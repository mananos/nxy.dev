package app

import (
	"context"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/flow"
)

// subagentStop handles SubagentStop of a planner launched in the background: it clears the
// mark, leaves the result to be delivered to the main thread and, if the planner ended
// without a saved plan, applies PlannerReturned (retry or give up). A foreground planner is
// ignored: its PostToolUse follows. It never blocks the subagent.
func (h Hooks) subagentStop(ctx context.Context, ev HookEvent) HookResponse {
	if ev.AgentType != rolePlanner {
		return HookResponse{}
	}
	f, ok := h.activeFeature(ctx, ev)
	if !ok {
		return HookResponse{}
	}
	if f.Background == "" && f.State.Phase == domain.PhasePlanning {
		// The planner ended before its launch hook ran (or it is a foreground one, whose
		// PostToolUse clears this): remember it so a later async launch does not wait.
		_, _, _ = h.mutate(ctx, f.ID, func(g *domain.Feature) bool {
			if g.Background != "" || g.State.Phase != domain.PhasePlanning {
				return false
			}
			g.Background = backgroundEnded
			return true
		})
		return HookResponse{}
	}
	if f.Background != rolePlanner {
		return HookResponse{}
	}
	_, _, _ = h.advanceWith(ctx, f.ID, flow.Event{Kind: flow.PlannerReturned}, func(g *domain.Feature) error {
		if g.Background != rolePlanner {
			return errSkipEvent // another hook handled it
		}
		g.Background = ""
		g.Undelivered = true
		if g.State.Phase != domain.PhasePlanning {
			return errSkipEvent
		}
		return nil
	})
	return HookResponse{}
}
