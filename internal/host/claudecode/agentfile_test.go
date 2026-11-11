package claudecode

import (
	"path/filepath"
	"testing"
)

func TestAgentFileReadsCallsAndTimes(t *testing.T) {
	path := filepath.Join("testdata", "agent_transcript.jsonl")
	res, ok := AgentFile{}.Read(path, "a1")
	if !ok || len(res.Calls) != 1 {
		t.Fatalf("ok=%v calls=%d", ok, len(res.Calls))
	}
	if c := res.Calls[0]; c.Model != "claude-sonnet-4-6" || c.Usage.Input != 10 || c.Usage.Output != 5 {
		t.Errorf("call = %+v", c)
	}
	if res.FirstTs == nil || res.LastTs == nil || *res.LastTs <= *res.FirstTs {
		t.Errorf("times = %v %v", res.FirstTs, res.LastTs)
	}
}

func TestAgentFileMissingIsEmpty(t *testing.T) {
	res, ok := AgentFile{}.Read(filepath.Join(t.TempDir(), "none.jsonl"), "a1")
	if ok || len(res.Calls) != 0 {
		t.Errorf("ok=%v calls=%d", ok, len(res.Calls))
	}
	if _, ok := (AgentFile{}).Read("", "a1"); ok {
		t.Error("empty path read")
	}
}
