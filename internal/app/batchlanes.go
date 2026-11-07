package app

import (
	"context"
	"slices"
	"strconv"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/flow"
	"github.com/mananos/nxy.dev/internal/plan"
)

// The implementers of running_batch go in parallel, one per batch whose dependencies are done:
// each launch is tied to its batch (Feature.Lanes) so that every return is judged on its own batch.

const laneRunning = "running"

// runningBatches lists the batches in running status; a state saved before parallel batches has
// none in Steps and works on the batch in focus.
func runningBatches(f domain.Feature) []int {
	if len(f.State.Steps) > 0 {
		return f.State.Running()
	}
	if f.State.Phase == domain.PhaseRunningBatch && f.State.Batch > 0 {
		return []int{f.State.Batch}
	}
	return nil
}

// unlaunchedBatches lists the running batches whose implementer was not launched yet.
func unlaunchedBatches(f domain.Feature) []int {
	if len(f.State.Steps) > 0 {
		return f.State.Unlaunched()
	}
	if f.State.Phase == domain.PhaseRunningBatch && f.Next.Kind == domain.DispatchImplementer && f.State.Batch > 0 {
		return []int{f.State.Batch}
	}
	return nil
}

// launchedBatches lists the running batches whose implementer is up.
func launchedBatches(f domain.Feature) []int {
	if len(f.State.Steps) == 0 {
		return nil
	}
	var out []int
	for _, n := range f.State.Running() {
		if !slices.Contains(f.State.Unlaunched(), n) {
			out = append(out, n)
		}
	}
	return out
}

// batchesWait is the line telling the main thread that launched batches are still running.
func batchesWait(id string, ns []int) string {
	w := "batch " + domain.JoinInts(ns, "and") + " is"
	if len(ns) > 1 {
		w = "batches " + domain.JoinInts(ns, "and") + " are"
	}
	return "nxy: feature " + id + " — " + w + " running; wait and do not dispatch again: nxy brings each result when it ends."
}

// laneFor is the running batch an implementer event is about: the lane of its tool_use id; the
// batch its prompt names; or, with no sign, the only batch running. A prompt that names a batch
// that is not running judges nothing.
func laneFor(f domain.Feature, ev HookEvent) (int, bool) {
	running := runningBatches(f)
	for _, l := range f.Lanes {
		if !slices.Contains(running, l.Batch) {
			continue
		}
		if (ev.AgentID != "" && l.AgentID == ev.AgentID) || (ev.ToolUseID != "" && l.ToolUse == ev.ToolUseID) {
			return l.Batch, true
		}
	}
	if n := batchOfPrompt(ev.AgentPrompt); n > 0 {
		return n, slices.Contains(running, n)
	}
	cands := launchedBatches(f)
	if len(cands) == 0 {
		cands = running
	}
	if len(cands) == 1 {
		return cands[0], true
	}
	return 0, false
}

// preImplementer answers PreToolUse of an implementer in running_batch: its prompt must name a
// batch ready to launch (or none when only one is); it clears the green of that batch, ties the
// launch to it and writes its prompt.
func (h Hooks) preImplementer(ctx context.Context, ev HookEvent, f domain.Feature) HookResponse {
	unl, running := unlaunchedBatches(f), runningBatches(f)
	n := batchOfPrompt(ev.AgentPrompt)
	switch {
	case n == 0 && len(unl) == 1:
		n = unl[0]
	case n == 0 && len(unl) == 0:
		return HookResponse{Deny: "no batch to launch; next step " + f.Next.Label()}
	case n == 0:
		return HookResponse{Deny: "name the batch: prompt `feature " + f.ID + " batch N` with N in " + domain.JoinInts(unl, "and")}
	case slices.Contains(unl, n):
	case slices.Contains(running, n):
		return HookResponse{Deny: "batch " + strconv.Itoa(n) + " is already running; wait: nxy brings its result when it ends"}
	default:
		return HookResponse{Deny: "batch " + strconv.Itoa(n) + " is not ready; next step " + f.Next.Label()}
	}
	claimed := false
	nf, _, err := h.advanceSteps(ctx, f.ID, func(g *domain.Feature) ([]flow.Event, error) {
		claimed = false
		if g.State.Phase != domain.PhaseRunningBatch || !slices.Contains(unlaunchedBatches(*g), n) {
			return nil, errSkipEvent
		}
		// A new attempt starts on a clean green; the red and the failure stay for the retry.
		p := g.ProofOf(n)
		p.Green = nil
		g.SetProof(p)
		g.SetLane(domain.BatchLane{Batch: n, ToolUse: ev.ToolUseID})
		claimed = true
		return []flow.Event{{Kind: flow.Launched, Batch: n}}, nil
	})
	if err != nil {
		return HookResponse{}
	}
	if !claimed {
		return HookResponse{Deny: "batch " + strconv.Itoa(n) + " is already running; wait: nxy brings its result when it ends"}
	}
	var files []string
	if nf.Plan != nil && n >= 1 && n <= len(nf.Plan.Batches) {
		files = batchFiles(nf.Plan.Batches[n-1])
	}
	return HookResponse{Prompt: plan.ImplementerPromptFor(nf, n, h.Exe, h.implementerMemory(ctx, nf, files))}
}

// postImplementer answers PostToolUse of an implementer in running_batch: a background launch
// waits for its SubagentStop (unless it already came); a return is judged on its own batch.
func (h Hooks) postImplementer(ctx context.Context, ev HookEvent, f domain.Feature) HookResponse {
	n, ok := laneFor(f, ev)
	if !ok {
		return HookResponse{Next: &Next{Feature: f}}
	}
	lane, _ := f.LaneOf(n)
	if ev.AgentAsync && lane.Background != backgroundEnded {
		nf, _, err := h.mutate(ctx, f.ID, func(g *domain.Feature) bool {
			l, _ := g.LaneOf(n)
			if l.Background == backgroundEnded || !slices.Contains(runningBatches(*g), n) {
				return false
			}
			l.Batch, l.Background = n, laneRunning
			if ev.AsyncAgentID != "" {
				l.AgentID = ev.AsyncAgentID
			}
			g.SetLane(l)
			return true
		})
		if err == nil && slices.Contains(runningBatches(nf), n) {
			if l, _ := nf.LaneOf(n); l.Background == laneRunning {
				return HookResponse{Context: "nxy: feature " + nf.ID + " — batch " + strconv.Itoa(n) + " is running in the background; wait for it and do not dispatch it again: nxy brings its result when it ends."}
			}
		}
		// It ended in between: judge it now.
	}
	nf, _, err := h.judgeBatch(ctx, f.ID, n, false)
	if err != nil {
		return HookResponse{}
	}
	return HookResponse{Next: &Next{Feature: nf}}
}

// implementerStop handles SubagentStop of an implementer in running_batch: one that did not go to
// the background is remembered as ended (its PostToolUse judges it); a background one is judged
// now and its result left for the main thread, unless it only has to keep waiting.
func (h Hooks) implementerStop(ctx context.Context, ev HookEvent, f domain.Feature) {
	n, ok := laneFor(f, ev)
	if !ok {
		return
	}
	if l, _ := f.LaneOf(n); l.Background != laneRunning {
		_, _, _ = h.mutate(ctx, f.ID, func(g *domain.Feature) bool {
			l, _ := g.LaneOf(n)
			if l.Background == laneRunning || !slices.Contains(runningBatches(*g), n) {
				return false
			}
			l.Batch, l.Background = n, backgroundEnded
			g.SetLane(l)
			return true
		})
		return
	}
	_, _, _ = h.judgeBatch(ctx, f.ID, n, true)
}

// judgeBatch judges batch n from its proof and drops its lane. With deliver the result is left for
// the main thread, except when what comes next is only to keep waiting for other batches.
func (h Hooks) judgeBatch(ctx context.Context, id string, n int, deliver bool) (domain.Feature, []domain.Action, error) {
	return h.advanceSteps(ctx, id, func(g *domain.Feature) ([]flow.Event, error) {
		if g.State.Phase != domain.PhaseRunningBatch || !slices.Contains(runningBatches(*g), n) {
			return nil, errSkipEvent // another hook handled it
		}
		var lanes []domain.BatchLane
		for _, l := range g.Lanes {
			if l.Batch != n {
				lanes = append(lanes, l)
			}
		}
		g.Lanes = lanes
		evs := h.withCandidates(g, batchReturnedN(g, n))
		if deliver {
			st, as := g.State, []domain.Action(nil)
			for _, e := range evs {
				var err error
				if st, as, err = flow.Next(st, e); err != nil {
					break
				}
			}
			if mainThreadAction(as).Kind != domain.AwaitBatches {
				g.Undelivered = true
			}
		}
		return evs, nil
	})
}
