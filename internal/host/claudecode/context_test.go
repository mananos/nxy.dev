package claudecode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMainContextTokens(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "t.jsonl")
	body := `{"type":"user","message":{"content":"hi"}}` + "\n" +
		`{"type":"assistant","message":{"usage":{"input_tokens":10,"cache_read_input_tokens":2000,"cache_creation_input_tokens":300}}}` + "\n" +
		`{"type":"user","message":{"content":"more"}}` + "\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := MainContextTokens(p)
	if !ok || got != 2310 {
		t.Fatalf("got %v %v, want 2310 true", got, ok)
	}
}

func TestMainContextTokensBigFileUsesTail(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "t.jsonl")
	pad := `{"type":"user","message":{"content":"` + strings.Repeat("x", 1000) + `"}}` + "\n"
	body := strings.Repeat(pad, 400) +
		`{"type":"assistant","message":{"usage":{"input_tokens":5,"cache_read_input_tokens":95}}}` + "\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, ok := MainContextTokens(p); !ok || got != 100 {
		t.Fatalf("got %v %v, want 100 true", got, ok)
	}
}

func TestMainContextTokensUnknown(t *testing.T) {
	if _, ok := MainContextTokens(""); ok {
		t.Fatal("empty path must be unknown")
	}
	if _, ok := MainContextTokens(filepath.Join(t.TempDir(), "missing.jsonl")); ok {
		t.Fatal("missing file must be unknown")
	}
}
