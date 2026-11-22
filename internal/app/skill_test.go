package app

import (
	"context"
	"strings"
	"testing"
)

func TestPreToolSkillDeniesFeature(t *testing.T) {
	h, _ := flowHooks(false)
	for _, sk := range []string{"nxy:feature", "nxy:engineer"} {
		r := h.Handle(context.Background(), HookEvent{Name: "PreToolUse", Tool: "Skill", Skill: sk})
		if !strings.Contains(r.Deny, "/"+sk+" <pedido>") {
			t.Errorf("%s: deny = %q", sk, r.Deny)
		}
	}
	other := HookEvent{Name: "PreToolUse", Tool: "Skill", Skill: "nxy:planner"}
	if r := h.Handle(context.Background(), other); r != (HookResponse{}) {
		t.Errorf("other skill: %+v", r)
	}
	sub := HookEvent{Name: "PreToolUse", Tool: "Skill", Skill: "nxy:feature", AgentID: "a1"}
	if r := h.Handle(context.Background(), sub); r != (HookResponse{}) {
		t.Errorf("subagent: %+v", r)
	}
}
