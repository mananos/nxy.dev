package claudecode

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/matyy-a/nxy.dev/internal/app"
	"github.com/matyy-a/nxy.dev/internal/platform"
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

func TestDecodeProjectDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_PROJECT_DIR", dir)
	ev, _ := Decode(strings.NewReader(`{"cwd":"/elsewhere"}`))
	if ev.ProjectDir != platform.NativePath(dir) {
		t.Errorf("got %q", ev.ProjectDir)
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
