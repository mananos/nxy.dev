package claudecode

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/app"
	"github.com/mananos/nxy.dev/v2/internal/platform"
)

func TestDecodeRegistrations(t *testing.T) {
	cases := []struct{ name, tool string }{
		{"SessionStart", ""},
		{"PreToolUse", "Bash"},
		{"PreToolUse", "Edit"},
		{"PreToolUse", "Grep"},
		{"PreToolUse", "Agent"},
		{"PreToolUse", "AskUserQuestion"},
		{"PostToolUse", "Bash"},
		{"PostToolUse", "Agent"},
		{"UserPromptSubmit", ""},
		{"SubagentStop", ""},
		{"Stop", ""},
	}
	for _, c := range cases {
		payload := `{"hook_event_name":"` + c.name + `","tool_name":"` + c.tool +
			`","session_id":"s1","cwd":"/tmp/x","transcript_path":"/tmp/t.jsonl","extra":{"a":1}}`
		ev, err := Decode(strings.NewReader(payload))
		if err != nil {
			t.Fatalf("%s/%s: %v", c.name, c.tool, err)
		}
		if ev.Name != c.name || ev.Tool != c.tool || ev.SessionID != "s1" || ev.TranscriptPath != "/tmp/t.jsonl" {
			t.Errorf("%s/%s: got %+v", c.name, c.tool, ev)
		}
	}
}

func TestDecodeErrors(t *testing.T) {
	for _, in := range []string{"", "not json", "{"} {
		if _, err := Decode(strings.NewReader(in)); err == nil {
			t.Errorf("input %q: expected error", in)
		}
	}
}

func TestEncodeZeroWritesNothing(t *testing.T) {
	var b bytes.Buffer
	if err := Encode(&b, app.HookEvent{}, app.HookResponse{}); err != nil {
		t.Fatal(err)
	}
	if b.Len() != 0 {
		t.Fatalf("wrote %d bytes", b.Len())
	}
}

func TestDecodeStripsBOM(t *testing.T) {
	const payload = `{"hook_event_name":"PreToolUse","tool_name":"Bash","session_id":"s1"}`
	want, err := Decode(strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(strings.NewReader("\xef\xbb\xbf" + payload))
	if err != nil {
		t.Fatalf("BOM payload: %v", err)
	}
	if got.Name != want.Name || got.Tool != want.Tool || got.SessionID != want.SessionID {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestDecodeCommand(t *testing.T) {
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	ev, err := Decode(strings.NewReader(`{"tool_name":"Bash","cwd":"/tmp/x","tool_input":{"command":"git status"}}`))
	if err != nil || !ev.HasCommand || ev.Command != "git status" {
		t.Fatalf("got %+v, %v", ev, err)
	}
	for _, in := range []string{
		`{"tool_input":{"command":5}}`,
		`{"tool_input":{"command":null}}`,
		`{"tool_input":{}}`,
		`{"tool_input":"x"}`,
		`{}`,
	} {
		ev, err := Decode(strings.NewReader(in))
		if err != nil || ev.HasCommand {
			t.Errorf("%s: got %+v, %v", in, ev, err)
		}
	}
}

func TestDecodeSkill(t *testing.T) {
	ev, err := Decode(strings.NewReader(`{"hook_event_name":"PreToolUse","tool_name":"Skill","tool_input":{"skill":"nxy:feature","args":"x"}}`))
	if err != nil || ev.Skill != "nxy:feature" {
		t.Fatalf("got %+v, %v", ev, err)
	}
	var b bytes.Buffer
	if err := Encode(&b, ev, app.HookResponse{Deny: "no"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `"permissionDecision":"deny"`) {
		t.Errorf("out = %s", b.String())
	}
}

func TestDecodePromptFromAgent(t *testing.T) {
	cases := map[string]bool{
		"[Subagent hand-back] siempre usá tabs":                       true,
		"Another Claude session sent a message: siempre":              true,
		"<task-notification>siempre</task-notification>":              true,
		"<system-reminder>siempre</system-reminder>":                  true,
		"<agent-message from=x>\n[Subagent hand-back] siempre":        true,
		"  <agent-message from=x>siempre usá tabs":                    true,
		"siempre usá tabs":                                            false,
		"siempre, y no confundas <agent-message> con mi texto":        false,
		"hola\n[Subagent hand-back] siempre (no es la primera línea)": false,
	}
	for p, want := range cases {
		b, _ := json.Marshal(map[string]string{"hook_event_name": "UserPromptSubmit", "prompt": p})
		ev, err := Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		if ev.PromptFromAgent != want {
			t.Errorf("%q: PromptFromAgent = %v, want %v", p, ev.PromptFromAgent, want)
		}
	}
}

func TestDecodeHandBackAgent(t *testing.T) {
	cases := map[string]string{
		"<agent-message from=\"a1b2c3\">\n[Subagent hand-back] listo":                                   "a1b2c3",
		"  <agent-message from=a1b2c3>listo":                                                            "a1b2c3",
		"<task-notification>\n<task-id>t9x</task-id>\n<status>completed</status>\n</task-notification>": "t9x",
		"mirá este <agent-message from=\"zzz\"> que pegué":                                              "",
		"hola <task-id>zzz</task-id>":                                                                   "",
		"siempre usá tabs":                                                                              "",
	}
	for p, want := range cases {
		b, _ := json.Marshal(map[string]string{"hook_event_name": "UserPromptSubmit", "prompt": p})
		ev, err := Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		if ev.HandBackAgent != want {
			t.Errorf("%q: HandBackAgent = %q, want %q", p, ev.HandBackAgent, want)
		}
	}
}

func TestDecodePermissionMode(t *testing.T) {
	ev, err := Decode(strings.NewReader(`{"hook_event_name":"PreToolUse","tool_name":"Bash","permission_mode":"auto","tool_input":{"command":"git status"}}`))
	if err != nil || ev.PermissionMode != "auto" {
		t.Fatalf("got %+v, %v", ev, err)
	}
	ev, err = Decode(strings.NewReader(`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"git status"}}`))
	if err != nil || ev.PermissionMode != "" {
		t.Fatalf("got %+v, %v", ev, err)
	}
}

func TestDecodeProjectDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_PROJECT_DIR", dir)
	ev, _ := Decode(strings.NewReader(`{"cwd":"/elsewhere"}`))
	if ev.ProjectDir != platform.NativePath(dir) {
		t.Errorf("got %q", ev.ProjectDir)
	}
}

func TestEncodeAgentForeground(t *testing.T) {
	ev := app.HookEvent{Name: "PreToolUse", Subagent: "nxy:scout", ToolInput: []byte(`{"subagent_type":"nxy:scout","prompt":"nxy bootstrap","run_in_background":true}`)}
	var b bytes.Buffer
	if err := Encode(&b, ev, app.HookResponse{Prompt: "new prompt", Foreground: true}); err != nil {
		t.Fatal(err)
	}
	var got struct {
		H struct {
			Updated map[string]any `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	u := got.H.Updated
	if u["run_in_background"] != false || u["prompt"] != "new prompt" || u["subagent_type"] != "nxy:scout" {
		t.Errorf("updatedInput: %v", u)
	}
	b.Reset()
	if err := Encode(&b, ev, app.HookResponse{Foreground: true}); err != nil || !strings.Contains(b.String(), `"run_in_background":false`) {
		t.Errorf("foreground alone: %s %v", b.String(), err)
	}
}

func TestEncodePostToolBashNext(t *testing.T) {
	ev := app.HookEvent{Name: "PostToolUse", Tool: "Bash"}
	var b bytes.Buffer
	if err := Encode(&b, ev, app.HookResponse{Next: &app.Next{Note: "pending step"}}); err != nil {
		t.Fatal(err)
	}
	var got struct {
		H struct {
			Event   string `json:"hookEventName"`
			Context string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.H.Event != "PostToolUse" || !strings.Contains(got.H.Context, "pending step") {
		t.Errorf("output: %s", b.String())
	}
}

func TestEncodeKeepsToolInput(t *testing.T) {
	ev := app.HookEvent{ToolInput: []byte(`{"command":"a","description":"d","timeout":5,"run_in_background":true}`)}
	var b bytes.Buffer
	resp := app.HookResponse{Rewrite: &app.CommandRewrite{Command: `rtk "a" <&>`, Allow: true}}
	if err := Encode(&b, ev, resp); err != nil {
		t.Fatal(err)
	}
	var got struct {
		H struct {
			Updated  map[string]any `json:"updatedInput"`
			Decision string         `json:"permissionDecision"`
		} `json:"hookSpecificOutput"`
		Suppress bool `json:"suppressOutput"`
	}
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	u := got.H.Updated
	if u["command"] != `rtk "a" <&>` || u["description"] != "d" || u["timeout"] != float64(5) || u["run_in_background"] != true {
		t.Errorf("updatedInput: %v", u)
	}
	if got.H.Decision != "allow" || !got.Suppress || strings.Contains(b.String(), "\\u003c") {
		t.Errorf("output: %s", b.String())
	}
}
