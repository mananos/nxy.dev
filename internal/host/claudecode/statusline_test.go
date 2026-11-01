package claudecode

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/mananos/nxy.dev/internal/host/claudecode/transcript"
)

func asst(ts, id string, out int) string {
	return `{"type":"assistant","timestamp":"` + ts + `","requestId":"` + id + `","message":{"id":"m` + id +
		`","model":"claude-sonnet-4-5","usage":{"input_tokens":10,"output_tokens":` + strconv.Itoa(out) + `}}}` + "\n"
}

func appendTo(t *testing.T, p, s string) {
	t.Helper()
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

func TestFoldStatusline(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "s1.jsonl")
	subs := filepath.Join(dir, "s1", "subagents")
	if err := os.MkdirAll(subs, 0o755); err != nil {
		t.Fatal(err)
	}
	appendTo(t, main, asst("2026-03-01T10:00:00Z", "a", 5))
	c := transcript.NewSessionCache()
	if n := FoldStatusline(main, &c); n != 0 {
		t.Fatalf("agents = %d", n)
	}
	if c.Main.Calls != 1 {
		t.Fatalf("main calls = %d", c.Main.Calls)
	}
	appendTo(t, filepath.Join(subs, "agent-b.jsonl"), asst("2026-03-01T10:05:00Z", "b", 7))
	appendTo(t, filepath.Join(subs, "notes.txt"), "x")
	appendTo(t, main, asst("2026-03-01T10:01:00Z", "c", 5))
	if n := FoldStatusline(main, &c); n != 1 {
		t.Fatalf("agents = %d", n)
	}
	st, ok := c.Agents.Get("agent-b.jsonl")
	if !ok || st.Calls != 1 || c.Main.Calls != 2 {
		t.Fatalf("agent=%+v main calls=%d", st, c.Main.Calls)
	}
	if st.LastTs == nil || c.Main.LastTs == nil || *c.Main.LastTs != *st.LastTs {
		t.Errorf("lastTs not propagated: %v vs %v", c.Main.LastTs, st.LastTs)
	}
	off := st.Offset
	FoldStatusline(main, &c) // nothing changed: the agent is skipped
	if st.Offset != off || st.Calls != 1 || c.Main.Calls != 2 {
		t.Errorf("refold changed state: %+v", st)
	}
}
