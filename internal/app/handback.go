package app

import (
	"context"
	"slices"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/domain"
)

// handBackCut is what the main thread hears when the verdict of a background agent is late: it ends
// its turn without running anything, and the Stop hook brings the step.
const handBackCut = "nxy: el veredicto del subagente tarda; cortá el turno sin correr nada, ni nxy status: nxy trae el Next al terminar."

// handBackPending says the hand-back of agent is of an nxy agent whose SubagentStop was not applied
// yet: a running lane of that agent (or the only one with no id), a planner or other role in the
// background, or a review still open that agent did not close.
func handBackPending(f domain.Feature, agent string) bool {
	if f.State.Phase == domain.PhaseRunningBatch {
		var running []domain.BatchLane
		for _, l := range f.Lanes {
			if l.Background == laneRunning {
				running = append(running, l)
			}
		}
		for _, l := range running {
			if l.AgentID == agent {
				return true
			}
		}
		if len(running) == 1 && running[0].AgentID == "" {
			return true
		}
	}
	if f.Background != "" && f.Background != backgroundEnded {
		return true
	}
	return reviewInFlight(f) && !slices.Contains(f.Review.Returned, agent)
}

// awaitHandBack holds the hand-back of a background nxy agent until its SubagentStop is applied, so
// the step arrives with that hand-back and not after a nxy status. It rereads the feature every
// 200 ms for up to 5 s. Released, it leaves the delivery to deliverPending, except for a batch that
// only waits for others (its wait line). Late, it tells the main thread to end the turn: the Stop
// hook delivers. ok is true when the response is final.
func (h Hooks) awaitHandBack(ctx context.Context, ev HookEvent) (HookResponse, bool) {
	if ev.HandBackAgent == "" || ev.AgentID != "" || h.Store == nil {
		return HookResponse{}, false
	}
	start := time.Now()
	waited := false
	for {
		f, ok := h.activeFeature(ctx, ev)
		if !ok || !handBackPending(f, ev.HandBackAgent) {
			if waited && f.State.Phase == domain.PhaseRunningBatch && f.Next.Kind == domain.AwaitBatches {
				if ns := runningBatches(f); len(ns) > 0 && !f.Undelivered {
					return HookResponse{Context: batchesWait(f.ID, ns)}, true
				}
			}
			return HookResponse{}, false
		}
		if time.Since(start) >= h.HandBackWait.max() {
			return HookResponse{Context: handBackCut}, true
		}
		waited = true
		select {
		case <-ctx.Done():
			return HookResponse{}, false
		case <-time.After(h.HandBackWait.every()):
		}
	}
}
