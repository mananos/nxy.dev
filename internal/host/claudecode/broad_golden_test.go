package claudecode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/goldentest"
	"github.com/mananos/nxy.dev/internal/searchscope"
)

// TestGoldenBroadHistory runs the history cases of testdata/golden/searchscope: each is materialized
// under a temp root (files under <root>/proj, @ROOT@ JSON-escaped in the transcript lines).
func TestGoldenBroadHistory(t *testing.T) {
	n := 0
	for _, c := range goldentest.Load(t, "searchscope") {
		if c.File != "history.json" {
			continue
		}
		n++
		t.Run(c.Name, func(t *testing.T) {
			var lines []json.RawMessage
			var files []string
			var threshold, sinceMs float64
			c.In(t, "lines", &lines)
			c.In(t, "files", &files)
			c.In(t, "threshold", &threshold)
			c.In(t, "sinceMs", &sinceMs)

			root := t.TempDir()
			if real, err := filepath.EvalSymlinks(root); err == nil {
				root = real
			}
			if err := os.MkdirAll(filepath.Join(root, "proj"), 0o755); err != nil {
				t.Fatal(err)
			}
			for _, rel := range files {
				p := filepath.Join(root, "proj", filepath.FromSlash(rel))
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			esc, _ := json.Marshal(root)
			escRoot := string(esc[1 : len(esc)-1])
			var b strings.Builder
			for _, l := range lines {
				b.WriteString(strings.ReplaceAll(string(l), "@ROOT@", escRoot))
				b.WriteByte('\n')
			}
			file := filepath.Join(root, "transcript.jsonl")
			if err := os.WriteFile(file, []byte(b.String()), 0o644); err != nil {
				t.Fatal(err)
			}

			rows := []map[string]any{}
			for _, r := range broadSearchesSince(file, threshold, sinceMs) {
				rows = append(rows, map[string]any{"ts": r.Ts, "tool": r.Tool, "kind": r.Kind, "contextTokens": r.ContextTokens})
			}
			goldentest.Check(t, c, map[string]any{"rows": rows})
		})
	}
	if n == 0 {
		t.Fatal("no history cases in testdata/golden/searchscope")
	}
}

// A path rooted without a volume takes the volume of the cwd, like path.resolve on Windows; it is
// not joined under the cwd. Elsewhere such a path is absolute and wins as is.
func TestIsFileInRootedPathTakesCwdVolume(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "src", "noext")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if searchscope.LooksLikeFile("noext") {
		t.Fatal("the probe must not look like a file, or the stat would not decide")
	}
	cwd := filepath.Join(dir, "proj")
	rooted := strings.TrimPrefix(file, filepath.VolumeName(file))
	for _, p := range []string{rooted, filepath.ToSlash(rooted)} {
		if !isFileIn(cwd, p) {
			t.Errorf("isFileIn(%q, %q) = false, want true (the file exists on the cwd volume)", cwd, p)
		}
	}
	if runtime.GOOS == "windows" {
		if got, want := resolveLikeNode(`C:\a\b`, `\src\x.go`), `C:\src\x.go`; got != want {
			t.Errorf("resolveLikeNode = %q, want %q", got, want)
		}
		if got, want := resolveLikeNode(`C:\a\b`, `src\x.go`), `C:\a\b\src\x.go`; got != want {
			t.Errorf("resolveLikeNode relative = %q, want %q", got, want)
		}
	}
}

func TestBroadSearchesInTranscriptMissingFile(t *testing.T) {
	if got := BroadSearchesInTranscript(filepath.Join(t.TempDir(), "nope.jsonl"), 1000); got != nil {
		t.Fatalf("unreadable file must give nil, got %v", got)
	}
}
