package app

import (
	"context"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/flow"
)

// subagentStop handles SubagentStop of a planner or implementer launched in the background: it
// clears the mark, leaves the result to be delivered to the main thread and applies what the
// return means (a planner without a saved plan retries or gives up; a batch is verified). A
// foreground subagent is ignored: its PostToolUse follows. It never blocks the subagent.
func (h Hooks) subagentStop(ctx context.Context, ev HookEvent) HookResponse {
	if isReviewerRole(ev.AgentType) {
		return h.reviewerStop(ctx, ev)
	}
	role, known := roles[ev.AgentType]
	if !known {
		return HookResponse{}
	}
	f, ok := h.activeFeature(ctx, ev)
	if !ok {
		return HookResponse{}
	}
	if ev.AgentType == roleImplementer {
		h.harvest(ctx, f, ev)
	}
	if _, in := role[f.State.Phase]; f.Background == "" && in {
		// It ended before its launch hook ran (or it is a foreground one, whose
		// PostToolUse clears this): remember it so a later async launch does not wait.
		_, _, _ = h.mutate(ctx, f.ID, func(g *domain.Feature) bool {
			if _, in := role[g.State.Phase]; g.Background != "" || !in {
				return false
			}
			g.Background = backgroundEnded
			return true
		})
		return HookResponse{}
	}
	if f.Background != ev.AgentType {
		return HookResponse{}
	}
	_, _, _ = h.advanceSteps(ctx, f.ID, func(g *domain.Feature) ([]flow.Event, error) {
		if g.Background != ev.AgentType {
			return nil, errSkipEvent // another hook handled it
		}
		g.Background = ""
		g.Undelivered = true
		returned, in := role[g.State.Phase]
		if !in {
			return nil, errSkipEvent
		}
		return h.withCandidates(g, returned(g)), nil
	})
	return HookResponse{}
}
