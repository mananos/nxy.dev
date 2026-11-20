package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/host/claudecode/transcript"
	"github.com/mananos/nxy.dev/v2/internal/views"
)

func TestViewStatsOutsideRepo(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "userdir")
	nxyHome := filepath.Join(root, "nxyhome")
	outside := filepath.Join(root, "afuera") // not a repo, no sessions of its own
	other := filepath.Join(root, "otro")     // a project that has them
	for _, d := range []string{home, nxyHome, outside, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("NXY_HOME", nxyHome)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("CLAUDE_CONFIG_DIR", "")

	now := vgAt(t, "2026-11-11T12:00:00Z")
	newSrc := func() *viewSource {
		return newViewSource(nil, os.Getenv, outside, func() time.Time { return now }, time.UTC)
	}

	// no transcripts anywhere: nothing to show, and it says so with a typed error
	src := newSrc()
	_, err := src.Stats(context.Background(), "")
	src.Close()
	if !errors.Is(err, views.ErrNoSession) {
		t.Fatalf("sin transcripts: err = %v, quiero ErrNoSession", err)
	}

	// a transcript of another project: it is the one shown
	slug := transcript.ProjectSlug(other)
	vgCopyTranscripts(t, filepath.Join(home, ".claude", "projects", slug), map[string]time.Time{
		"sess-a": vgAt(t, "2026-11-11T10:56:00Z"),
		"sess-b": vgAt(t, "2026-11-11T11:59:00Z"),
	})
	src = newSrc()
	defer src.Close()
	st, err := src.Stats(context.Background(), "")
	if err != nil {
		t.Fatalf("con sesiones de otro proyecto: %v", err)
	}
	if st.SessionID != "sess-b" {
		t.Fatalf("sesión = %q, quiero la última (sess-b)", st.SessionID)
	}
}
