package claudecode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeSubagentStopLastMessage(t *testing.T) {
	ev, err := Decode(strings.NewReader(`{"hook_event_name":"SubagentStop","agent_type":"nxy:implementer","agent_id":"a1","last_assistant_message":"ok\nlearned: x"}`))
	if err != nil || ev.AgentResult != "ok\nlearned: x" {
		t.Fatalf("result = %q, err = %v", ev.AgentResult, err)
	}
}

func TestDecodeSubagentStopFallsBackToTranscript(t *testing.T) {
	p := filepath.Join(t.TempDir(), "agent.jsonl")
	lines := `{"type":"assistant","message":{"content":[{"type":"text","text":"first"}]}}` + "\n" +
		`{"type":"user","message":{"content":"hi"}}` + "\n" +
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"x"},{"type":"text","text":"learned: y"}]}}` + "\n"
	if err := os.WriteFile(p, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	payload := `{"hook_event_name":"SubagentStop","agent_transcript_path":` + string(mustJSON(t, p)) + `}`
	ev, err := Decode(strings.NewReader(payload))
	if err != nil || ev.AgentResult != "learned: y" {
		t.Fatalf("result = %q, err = %v", ev.AgentResult, err)
	}
}

func TestDecodeAgentResultOnlyInSubagentStop(t *testing.T) {
	ev, _ := Decode(strings.NewReader(`{"hook_event_name":"Stop","last_assistant_message":"x"}`))
	if ev.AgentResult != "" {
		t.Errorf("result = %q", ev.AgentResult)
	}
	ev, _ = Decode(strings.NewReader(`{"hook_event_name":"SubagentStop"}`))
	if ev.AgentResult != "" {
		t.Errorf("result = %q", ev.AgentResult)
	}
}

func mustJSON(t *testing.T, s string) []byte {
	t.Helper()
	b, err := marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
