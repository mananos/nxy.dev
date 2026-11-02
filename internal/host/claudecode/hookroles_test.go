package claudecode

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/app"
)

func TestDecodeAgentModelEffort(t *testing.T) {
	ev, err := Decode(strings.NewReader(`{"hook_event_name":"PreToolUse","tool_name":"Agent","tool_input":{"subagent_type":"nxy:scout","model":"haiku","effort":"low"}}`))
	if err != nil || ev.AgentModel != "haiku" || ev.AgentEffort != "low" {
		t.Fatalf("%+v %v", ev, err)
	}
}

func TestEncodeMergesPromptModelEffort(t *testing.T) {
	enc := func(resp app.HookResponse) (map[string]any, string) {
		ev := app.HookEvent{Name: "PreToolUse", Subagent: "nxy:scout", ToolInput: []byte(`{"subagent_type":"nxy:scout","prompt":"old","description":"d"}`)}
		var b bytes.Buffer
		if err := Encode(&b, ev, resp); err != nil {
			t.Fatal(err)
		}
		var got struct {
			H struct {
				Updated  map[string]any `json:"updatedInput"`
				Decision string         `json:"permissionDecision"`
				Reason   string         `json:"permissionDecisionReason"`
			} `json:"hookSpecificOutput"`
		}
		if err := json.Unmarshal(b.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.H.Decision != "allow" {
			t.Fatalf("decision: %s", b.String())
		}
		return got.H.Updated, got.H.Reason
	}
	u, why := enc(app.HookResponse{Model: "haiku", Effort: "low"})
	if u["prompt"] != "old" || u["description"] != "d" || u["model"] != "haiku" || u["effort"] != "low" || why != "nxy: roles.scout.model/effort" {
		t.Errorf("role only: %v %q", u, why)
	}
	u, why = enc(app.HookResponse{Effort: "high"})
	if _, has := u["model"]; has || u["effort"] != "high" || why != "nxy: roles.scout.effort" {
		t.Errorf("effort only: %v %q", u, why)
	}
	u, why = enc(app.HookResponse{Prompt: "new", Model: "opus"})
	if u["prompt"] != "new" || u["model"] != "opus" || why != "nxy: prompt written by nxy" {
		t.Errorf("prompt + model: %v %q", u, why)
	}
}
