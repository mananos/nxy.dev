package claudecode

import (
	"bytes"
	"strings"
	"testing"

	"github.com/matyy-a/nxy.dev/internal/app"
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
	if err := Encode(&b, app.HookResponse{}); err != nil {
		t.Fatal(err)
	}
	if b.Len() != 0 {
		t.Fatalf("wrote %d bytes", b.Len())
	}
}
