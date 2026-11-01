package transcript

import "testing"

func TestBroadSearches(t *testing.T) {
	text := `{"type":"assistant","timestamp":"2026-01-01T10:00:00.000Z","cwd":"/w","requestId":"r","message":{"usage":{"input_tokens":5000},"content":[` +
		`{"type":"tool_use","id":"a","name":"Grep","input":{"pattern":"x"}},` +
		`{"type":"tool_use","id":"b","name":"Grep","input":{"pattern":"x","path":"known"}},` +
		`{"type":"tool_use","id":"c","name":"Grep","input":{"pattern":"x"}}]}}` + "\n" +
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"c","content":"nxy read gate: no"}]}}` + "\n" +
		`{"type":"assistant","requestId":"r","message":{"usage":{"input_tokens":0},"content":[{"type":"tool_use","id":"d","name":"Glob","input":{"pattern":"*"}}]}}` + "\n"
	var cwds []string
	isFile := func(cwd, p string) bool {
		cwds = append(cwds, cwd+"|"+p)
		return p == "known"
	}
	rows := BroadSearches(text, 1000, 0, isFile)
	if len(rows) != 2 || rows[0].Tool != "Grep" || rows[1].Tool != "Glob" || rows[1].ContextTokens != 5000 {
		t.Fatalf("rows: %+v", rows)
	}
	if rows[0].Ts == 0 || rows[1].Ts != 0 {
		t.Fatalf("ts: %+v", rows)
	}
	if len(cwds) != 1 || cwds[0] != "/w|known" {
		t.Fatalf("isFile calls: %v", cwds)
	}
	if got := BroadSearches(text, 0, 0, isFile); got != nil {
		t.Fatalf("threshold 0 must give nil: %v", got)
	}
}
