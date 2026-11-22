package app

import (
	"context"
	"testing"
)

type fakeRoles map[string][3]string

func (f fakeRoles) Role(_, role string) (string, string, string) {
	return f[role][0], f[role][1], f[role][2]
}

func TestRolesFillModelAndEffort(t *testing.T) {
	h, _ := flowHooks(false)
	h.Roles = fakeRoles{"scout": {"haiku", "low"}, "planner": {"opus", ""}}
	ctx := context.Background()

	// the scout has no feature and no flow step: it still gets its role
	r := h.Handle(ctx, agentEv("PreToolUse", roleScout))
	if r.Model != "haiku" || r.Effort != "low" || r.Prompt != "" || r.Deny != "" {
		t.Fatalf("scout = %+v", r)
	}
	// the dispatch wins over the config
	ev := agentEv("PreToolUse", roleScout)
	ev.AgentModel = "sonnet"
	if r := h.Handle(ctx, ev); r.Model != "" || r.Effort != "low" {
		t.Fatalf("dispatch model = %+v", r)
	}
	// no config for the role: nothing to say
	if r := h.Handle(ctx, agentEv("PreToolUse", roleRefuter)); r != (HookResponse{}) {
		t.Fatalf("refuter = %+v", r)
	}
	// not an nxy role
	if r := h.Handle(ctx, agentEv("PreToolUse", "general-purpose")); r != (HookResponse{}) {
		t.Fatalf("other = %+v", r)
	}
	// a step of the flow: the planner prompt and its model go together
	startedFeature(t, h, "go")
	r = h.Handle(ctx, agentEv("PreToolUse", rolePlanner))
	if r.Prompt == "" || r.Model != "opus" || r.Effort != "" {
		t.Fatalf("planner = %+v", r)
	}
}

func TestRolesInvalidConfigWarnsOnDispatch(t *testing.T) {
	h, _ := flowHooks(false)
	h.Roles = fakeRoles{"scout": {"haiku", "", "roles.scout.effort: valor inválido"}}
	r := h.Handle(context.Background(), agentEv("PreToolUse", roleScout))
	if r.Notice != "nxy: roles.scout.effort: valor inválido" || r.Model != "haiku" {
		t.Fatalf("scout = %+v", r)
	}
}

func TestRolesNilOrDeniedChangeNothing(t *testing.T) {
	h, _ := flowHooks(false)
	if r := h.Handle(context.Background(), agentEv("PreToolUse", roleScout)); r != (HookResponse{}) {
		t.Fatalf("nil roles = %+v", r)
	}
	h.Roles = fakeRoles{"implementer": {"opus", "high"}}
	startedFeature(t, h, "go")
	r := h.Handle(context.Background(), agentEv("PreToolUse", roleImplementer))
	if r.Deny == "" || r.Model != "" || r.Effort != "" {
		t.Fatalf("implementer before the plan = %+v", r)
	}
}
