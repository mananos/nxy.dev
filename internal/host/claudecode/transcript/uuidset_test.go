package transcript

import "testing"

func TestZeroValueUUIDSet(t *testing.T) {
	line := `{"type":"assistant","uuid":"u1","requestId":"r1","message":{"model":"claude-opus-5","usage":{"input_tokens":1,"output_tokens":1}}}` + "\n"
	seen := &UUIDSet{}
	if seen.Has("u1") {
		t.Fatal("empty set has u1")
	}
	first := ParseFile("p", []byte(line), true, FileOpts{Seen: seen})
	second := ParseFile("p", []byte(line), true, FileOpts{Seen: seen})
	if len(first.Calls) != 1 || len(second.Calls) != 0 {
		t.Fatalf("shared zero-value set must count the line once: %d then %d", len(first.Calls), len(second.Calls))
	}
	if !seen.Has("u1") {
		t.Fatal("u1 not recorded")
	}
}

func TestLastCacheTTLNone(t *testing.T) {
	if ttl, ok := LastCacheTTL([]byte(`{"type":"user"}`+"\n"), false); ok || ttl != "" {
		t.Fatalf("no cache write must be ok=false, got %q %v", ttl, ok)
	}
}
