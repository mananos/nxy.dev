package app

import (
	"context"
	"strings"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/flow"
	"github.com/mananos/nxy.dev/internal/plan"
)

const (
	rolePlanner     = "nxy:planner"
	roleImplementer = "nxy:implementer"
)

// roleFor is the nxy subagent each dispatch action expects.
var roleFor = map[domain.ActionKind]string{
	domain.DispatchPlanner:     rolePlanner,
	domain.DispatchImplementer: roleImplementer,
	domain.DispatchAffectedFix: roleImplementer,
	domain.DispatchReviewer:    "nxy:reviewer",
	domain.DispatchRefuter:     "nxy:refuter",
	domain.DispatchFixer:       roleImplementer,
}

// roleInfo is what nxy does with a subagent that can run in the background: the phase it works
// in and the events its return stands for (it may change the feature, e.g. the verdict).
type roleInfo map[domain.Phase]func(f *domain.Feature) []flow.Event

var roles = map[string]roleInfo{
	rolePlanner: {domain.PhasePlanning: func(*domain.Feature) []flow.Event {
		return []flow.Event{{Kind: flow.PlannerReturned}}
	}},
	roleImplementer: {
		domain.PhaseRunningBatch: batchReturned,
		domain.PhaseAffectedFix: func(*domain.Feature) []flow.Event {
			return []flow.Event{{Kind: flow.FixesReturned}}
		},
	},
}

// batchReturned judges the batch from the proof the binary recorded: BatchReturned, then Green
// or Red. The reason of a red stays in Proof.Failure for the retry prompt and the question.
func batchReturned(f *domain.Feature) []flow.Event {
	n := f.State.Batch
	p := proofOf(f, n)
	ok, why := false, "the feature has no plan"
	if f.Plan != nil {
		ok, why = plan.Verdict(plan.NeedOf(*f.Plan, n), f.Proof, n)
	}
	if ok {
		p.Failure = ""
	} else {
		p.Failure = why
	}
	f.Proof = &p
	kind := flow.Red
	if ok {
		kind = flow.Green
	}
	return []flow.Event{{Kind: flow.BatchReturned}, {Kind: kind}}
}

// proofOf is a copy of the proof of batch n (empty if the feature holds another one).
func proofOf(f *domain.Feature, n int) domain.Proof {
	if f.Proof != nil && f.Proof.Batch == n {
		return *f.Proof
	}
	return domain.Proof{Batch: n}
}

// noteWrite counts a subagent write in the repo during the running batch: a green recorded
// before it no longer vouches for the code.
func (h Hooks) noteWrite(ctx context.Context, id string) {
	_, _, _ = h.mutate(ctx, id, func(g *domain.Feature) bool {
		if g.State.Phase != domain.PhaseRunningBatch {
			return false
		}
		p := proofOf(g, g.State.Batch)
		p.Edits++
		g.Proof = &p
		return true
	})
}

// backgroundEnded marks that a background-capable subagent's SubagentStop arrived before any
// background launch.
const backgroundEnded = "ended"

// backgroundWait is the line telling the main thread a background subagent is still running.
func backgroundWait(id, role string) string {
	return "nxy: feature " + id + " — the " + strings.TrimPrefix(role, "nxy:") + " is running in the background; wait for it and do not dispatch it again: nxy brings its result when it ends."
}

func isAgentTool(tool string) bool { return tool == "Agent" || tool == "Task" }

// activeFeature is the active feature of the event's repo, if any.
func (h Hooks) activeFeature(ctx context.Context, ev HookEvent) (domain.Feature, bool) {
	if h.Store == nil || h.Repos == nil {
		return domain.Feature{}, false
	}
	dir := ev.ProjectDir
	if dir == "" {
		dir = ev.Cwd
	}
	f, found, err := h.Store.ActiveFeature(ctx, h.Repos.Key(dir))
	if err != nil || !found {
		return domain.Feature{}, false
	}
	return f, true
}

// preToolAgent handles PreToolUse Agent/Task from the main thread: it injects the planner's
// prompt and refuses nxy roles that are not the next step.
func (h Hooks) preToolAgent(ctx context.Context, ev HookEvent) HookResponse {
	if ev.AgentID != "" || !strings.HasPrefix(ev.Subagent, "nxy:") {
		return HookResponse{}
	}
	f, ok := h.activeFeature(ctx, ev)
	if !ok {
		return HookResponse{}
	}
	switch {
	case f.Background == ev.Subagent:
		return HookResponse{Deny: backgroundWait(f.ID, ev.Subagent)}
	case ev.Subagent == rolePlanner && f.State.Phase == domain.PhasePlanning:
		return HookResponse{Prompt: plan.PlannerPrompt(f, h.Exe)}
	case ev.Subagent == roleImplementer && f.State.Phase != domain.PhaseRunningBatch && f.State.Phase != domain.PhaseAffectedFix:
		return HookResponse{Deny: "plan not approved; next step " + f.Next.Label()}
	case ev.Subagent == roleImplementer && f.Next.Kind == domain.DispatchImplementer:
		// A new attempt starts on a clean green; the red and the failure stay for the retry.
		nf, _, err := h.mutate(ctx, f.ID, func(g *domain.Feature) bool {
			p := proofOf(g, g.State.Batch)
			p.Green = nil
			g.Proof = &p
			return true
		})
		if err != nil {
			return HookResponse{}
		}
		return HookResponse{Prompt: plan.ImplementerPrompt(nf, h.Exe)}
	case ev.Subagent == roleImplementer && f.Next.Kind == domain.DispatchAffectedFix:
		return HookResponse{Prompt: plan.AffectedFixPrompt(f, h.Exe)}
	case ev.Subagent != roleFor[f.Next.Kind]:
		return HookResponse{Deny: "out of order: next step is " + f.Next.Label()}
	}
	return HookResponse{}
}

// postToolAgent handles PostToolUse Agent/Task for the planner and the implementer: a
// background launch waits for SubagentStop; otherwise the return is judged (the planner
// without a saved plan retries or gives up, a batch is verified) and the stored step repeated.
func (h Hooks) postToolAgent(ctx context.Context, ev HookEvent) HookResponse {
	role, known := roles[ev.Subagent]
	if ev.AgentID != "" || !known {
		return HookResponse{}
	}
	f, ok := h.activeFeature(ctx, ev)
	if !ok {
		return HookResponse{}
	}
	ended := f.Background == backgroundEnded
	returned, inPhase := role[f.State.Phase]
	if ev.AgentAsync && !ended && inPhase {
		nf, _, err := h.mutate(ctx, f.ID, func(g *domain.Feature) bool {
			g.Background = ev.Subagent
			return true
		})
		if err != nil {
			return HookResponse{}
		}
		return HookResponse{Context: backgroundWait(nf.ID, ev.Subagent)}
	}
	if inPhase {
		nf, _, err := h.advanceSteps(ctx, f.ID, func(g *domain.Feature) ([]flow.Event, error) {
			if g.Background == backgroundEnded {
				g.Background = ""
			}
			return returned(g), nil
		})
		if err != nil {
			return HookResponse{}
		}
		f = nf
	} else if ended {
		if nf, _, err := h.mutate(ctx, f.ID, func(g *domain.Feature) bool {
			if g.Background != backgroundEnded {
				return false
			}
			g.Background = ""
			return true
		}); err == nil {
			f = nf
		}
	}
	return HookResponse{Next: &Next{Feature: f}}
}
