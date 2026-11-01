package claudecode

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mananos/nxy.dev/internal/host/claudecode/transcript"
)

func TestListSessionsSkipsDanglingEntry(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "-p")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ok.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "missing-target"), filepath.Join(dir, "dangling.jsonl")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	got := ListSessions(ListOpts{All: true, ProjectsDir: root})
	if len(got) != 1 || got[0].SessionID != "ok" {
		t.Fatalf("want only ok, got %+v", got)
	}
}

type failingReaderAt struct{}

func (failingReaderAt) ReadAt([]byte, int64) (int, error) { return 0, errors.New("disk error") }

func TestFoldNewKeepsOffsetOnReadError(t *testing.T) {
	st := transcript.NewIncrementalState()
	foldNew(failingReaderAt{}, 100, st, nil)
	if st.Offset != 0 {
		t.Fatalf("offset moved to %d after a failed read", st.Offset)
	}
}
