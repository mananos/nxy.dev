package claudecode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mananos/nxy.dev/internal/goldentest"
	"github.com/mananos/nxy.dev/internal/host/claudecode/transcript"
)

// materialize writes the golden texts under a temp dir and sets the discovery mtimes.
func materialize(t *testing.T, cases []goldentest.Case) string {
	t.Helper()
	root := t.TempDir()
	for _, c := range cases {
		if c.File != "texts.json" {
			continue
		}
		var texts map[string]string
		var mtimes map[string]float64
		if err := json.Unmarshal(c.Output["texts"], &texts); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(c.Output["mtimes"], &mtimes); err != nil {
			t.Fatal(err)
		}
		for rel, text := range texts {
			p := filepath.Join(root, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		for rel, sec := range mtimes {
			ts := time.Unix(int64(sec), 0)
			if err := os.Chtimes(filepath.Join(root, filepath.FromSlash(rel)), ts, ts); err != nil {
				t.Fatal(err)
			}
		}
		return root
	}
	t.Fatal("texts case not found in testdata/golden/transcripts")
	return ""
}

func TestGoldenSessions(t *testing.T) {
	cases := goldentest.Load(t, "transcripts")
	root := materialize(t, cases)
	n := 0
	for _, c := range cases {
		if c.File != "sessions.json" {
			continue
		}
		n++
		t.Run(c.Name, func(t *testing.T) {
			var in struct {
				ProjectsDir string                 `json:"projectsDir"`
				Project     string                 `json:"project"`
				ID          string                 `json:"id"`
				Opts        transcript.SessionOpts `json:"opts"`
			}
			c.In(t, "projectsDir", &in.ProjectsDir)
			c.In(t, "project", &in.Project)
			c.In(t, "id", &in.ID)
			c.In(t, "opts", &in.Opts)
			dir := filepath.Join(root, in.ProjectsDir, in.Project)
			ref := transcript.SessionRef{
				SessionID: in.ID,
				Path:      filepath.Join(dir, in.ID+".jsonl"),
				Project:   in.Project,
				AgentsDir: filepath.Join(dir, in.ID, "subagents"),
			}
			parsed := ParseSession(ref, in.Opts)
			session := goldentest.NormRoot(goldentest.Norm(t, parsed), root)
			order := append([]string{}, parsed.Tools.Calls.Keys...)
			subs := goldentest.NormRoot(goldentest.Norm(t, SubagentFiles(ref)), root)
			goldentest.Check(t, c, map[string]any{"session": session, "subagents": subs, "callsOrder": order})
		})
	}
	if n == 0 {
		t.Fatal("no sessions cases")
	}
}

func TestGoldenDiscovery(t *testing.T) {
	cases := goldentest.Load(t, "transcripts")
	root := materialize(t, cases)
	n := 0
	for _, c := range cases {
		if c.File != "discovery.json" {
			continue
		}
		n++
		t.Run(c.Name, func(t *testing.T) {
			var projectsDir, cwd string
			var all bool
			var since float64
			var selector *string
			c.In(t, "projectsDir", &projectsDir)
			c.In(t, "cwd", &cwd)
			c.In(t, "all", &all)
			c.In(t, "since", &since)
			c.In(t, "selector", &selector)
			opts := ListOpts{Cwd: cwd, All: all, ProjectsDir: filepath.Join(root, projectsDir), Since: int64(since)}
			if selector == nil {
				list := ListSessions(opts)
				goldentest.Check(t, c, map[string]any{"list": goldentest.NormRoot(goldentest.Norm(t, list), root)})
				return
			}
			ref := ResolveSession(*selector, opts)
			var got any // nil is the JS null
			if ref != nil {
				got = goldentest.NormRoot(goldentest.Norm(t, ref), root)
			}
			goldentest.Check(t, c, map[string]any{"ref": got})
		})
	}
	if n == 0 {
		t.Fatal("no discovery cases")
	}
}

func TestProjectsDir(t *testing.T) {
	if got := ProjectsDir("/x/y"); got != "/x/y" {
		t.Fatalf("override ignored: %q", got)
	}
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	if got := ProjectsDir(""); got != "" {
		t.Fatalf("no home must give no sessions dir, got %q", got)
	}
	if got := ListSessions(ListOpts{All: true}); len(got) != 0 {
		t.Fatalf("no home must list nothing: %v", got)
	}
}
