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
	case ev.Subagent == rolePlanner && f.State.Phase == domain.PhasePlanning:
		return HookResponse{Prompt: plan.PlannerPrompt(f, h.Exe)}
	case ev.Subagent == roleImplementer && f.State.Phase != domain.PhaseRunningBatch:
		return HookResponse{Deny: "plan not approved; next step " + f.Next.Label()}
	case ev.Subagent != roleFor[f.Next.Kind]:
		return HookResponse{Deny: "out of order: next step is " + f.Next.Label()}
	}
	return HookResponse{}
}

// postToolAgent handles PostToolUse Agent/Task for the planner: if it came back without a
// saved plan the flow retries or gives up; otherwise the stored step is repeated.
func (h Hooks) postToolAgent(ctx context.Context, ev HookEvent) HookResponse {
	if ev.AgentID != "" || ev.Subagent != rolePlanner {
		return HookResponse{}
	}
	f, ok := h.activeFeature(ctx, ev)
	if !ok {
		return HookResponse{}
	}
	if f.State.Phase == domain.PhasePlanning {
		nf, _, err := h.advance(ctx, f.ID, flow.Event{Kind: flow.PlannerReturned})
		if err != nil {
			return HookResponse{}
		}
		f = nf
	}
	return HookResponse{Next: &Next{Feature: f}}
}
