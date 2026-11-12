package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/platform"
	"github.com/mananos/nxy.dev/internal/store/sqlite"
	"github.com/mananos/nxy.dev/internal/tools"
)

func vsGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func vsWrite(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestViewSourcePlanFileEngineer(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "userdir"))
	t.Setenv("USERPROFILE", filepath.Join(root, "userdir"))
	t.Setenv("NXY_HOME", filepath.Join(root, "nxyhome"))
	t.Setenv("CLAUDE_PROJECT_DIR", filepath.Join(root, "app"))
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	repo := filepath.Join(root, "app")
	vsGit(t, root, "init", "-q", repo)
	for _, kv := range [][]string{{"user.name", "t"}, {"user.email", "t@example.com"}, {"commit.gpgsign", "false"}, {"core.autocrlf", "false"}} {
		vsGit(t, repo, "config", kv[0], kv[1])
	}
	vsWrite(t, filepath.Join(repo, "src", "foo.go"), "package src\n\nfunc Foo() int {\n\treturn 1\n}\n")
	vsGit(t, repo, "add", ".")
	vsGit(t, repo, "commit", "-q", "-m", "init")
	base := platform.GitHead(repo)
	vsWrite(t, filepath.Join(repo, "src", "foo.go"), "package src\n\nfunc Foo() int {\n\treturn 2\n}\n")
	vsWrite(t, filepath.Join(repo, "src", "new.go"), "package src\n")

	ctx := context.Background()
	clk := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	st, err := sqlite.Open(ctx, filepath.Join(root, "nxy.db"), func() time.Time { return clk })
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	key := platform.RepoKey(repo)
	plan := &domain.Plan{
		Title:    "Foo devuelve 2",
		Criteria: []domain.Criterion{{ID: "R1", Text: "Foo devuelve 2", Test: domain.TestTDD}},
		Batches: []domain.Batch{{
			N: 1, Title: "Foo", Covers: []string{"R1"}, Risk: domain.RiskLow,
			Change: []domain.Change{{At: "src/foo.go:4", What: "devuelve 2"}, {At: "src/new.go (new)", What: "nuevo"}},
			Test:   domain.BatchTest{Kind: domain.BatchTestNew, Name: "TestFoo", Accept: "go test ./src/"},
		}},
	}
	id, err := st.CreateFeature(ctx, domain.Feature{
		Repo: key, ProjectDir: repo, Home: "ck", Holds: []string{"ck"}, Request: "foo", PlanRev: 1, Plan: plan, Base: base,
		State: domain.State{Phase: domain.PhaseRunningBatch, Batch: 1, Batches: 1, Steps: []domain.Step{{N: 1, Status: domain.StepRunning, Launched: true}}},
		Next:  domain.Action{Kind: domain.AskDecision},
	})
	if err != nil {
		t.Fatal(err)
	}
	eid, err := st.CreateFeature(ctx, domain.Feature{
		Repo: key, ProjectDir: repo, Home: "ck2", Request: "diseño", Mode: domain.ModeEngineer,
		State: domain.State{Phase: domain.PhaseDesigning},
		Design: &domain.Design{
			Current: "B1", Today: "a -> b",
			Agenda: []domain.Fork{{ID: "B1", Title: "forma", Status: domain.ForkOpen, Options: []domain.ForkOption{{Label: "uno", D2: "a -> b"}, {Label: "dos", D2: "c -> d"}}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	old := viewTools
	t.Cleanup(func() { viewTools = old })
	affectedCalls, grepCalls := 0, 0
	stub := func(indexed, rg bool) {
		viewTools = viewToolset{
			Indexed: func(string) bool { return indexed },
			Affected: func(_ context.Context, _ string, files []string) ([]string, bool) {
				affectedCalls++
				return []string{"src/foo_test.go"}, true
			},
			RgFound: func() bool { return rg },
			Grep: func(_ context.Context, _, word string) []tools.RgRow {
				grepCalls++
				return []tools.RgRow{{Path: "src/foo.go", Line: 3}, {Path: "src/bar.go", Line: 7}}
			},
			FindD2:   func() (string, string, bool) { return "", "", false },
			RenderD2: func(context.Context, string, string) ([]string, error) { return nil, errors.New("no d2") },
		}
	}
	newSrc := func() *viewSource {
		return newViewSource(st, os.Getenv, repo, func() time.Time { return clk }, time.UTC)
	}

	// codegraph when the repo is indexed
	stub(true, true)
	src := newSrc()
	defer src.Close()
	p, ok, err := src.Plan(ctx, id)
	if err != nil || !ok {
		t.Fatalf("plan: %v %v", ok, err)
	}
	if p.Impact.Source != "codegraph" || len(p.Impact.Files) != 2 {
		t.Fatalf("impact %+v", p.Impact)
	}
	foo, nw := p.Impact.Files[0], p.Impact.Files[1]
	if foo.Kind != "edit" || nw.Kind != "new" {
		t.Fatalf("kinds %q %q", foo.Kind, nw.Kind)
	}
	if !reflect.DeepEqual(foo.ImportedBy, []string{"bar.go"}) || len(foo.Tests) != 1 || foo.Tests[0].Path != "src/foo_test.go" {
		t.Fatalf("foo impact %+v", foo)
	}
	if grepCalls != 2 {
		t.Fatalf("grep once per file: %d", grepCalls)
	}
	calls := affectedCalls
	if _, _, err := src.Plan(ctx, id); err != nil || affectedCalls != calls {
		t.Fatalf("the impact must be cached: %d -> %d (%v)", calls, affectedCalls, err)
	}

	// by name when there is no index but rg is there
	stub(false, true)
	if p, _, _ = newSrc().Plan(ctx, id); p.Impact.Source != "name" {
		t.Fatalf("source %q", p.Impact.Source)
	}
	// nothing when neither is there
	stub(false, false)
	grepCalls = 0
	if p, _, _ = newSrc().Plan(ctx, id); p.Impact.Source != "none" || len(p.Impact.Files[0].Tests) != 0 {
		t.Fatalf("none: %+v", p.Impact)
	}
	if grepCalls != 0 {
		t.Fatalf("grep must not run without a source: %d", grepCalls)
	}

	// a feature without a plan
	if _, ok, err := newSrc().Plan(ctx, eid); err != nil || ok {
		t.Fatalf("no plan: %v %v", ok, err)
	}

	// file: the diff against the base
	stub(true, true)
	f, err := newSrc().File(ctx, id, "src/foo.go")
	if err != nil {
		t.Fatal(err)
	}
	if f.Source != "codegraph" || len(f.Hunks) != 1 || f.EditedBy != "lo edita el lote 1" || len(f.Content) == 0 {
		t.Fatalf("file %+v", f)
	}
	f, err = newSrc().File(ctx, id, "src/new.go")
	if err != nil || len(f.Hunks) != 1 || f.Hunks[0].OldStart != 0 {
		t.Fatalf("new file %+v %v", f, err)
	}

	// paths that leave the repo or exist nowhere are refused
	for _, bad := range []string{"../x", "src/../../x", filepath.Join(repo, "src", "foo.go"), "src/nothing.go"} {
		if _, err := newSrc().File(ctx, id, bad); err == nil {
			t.Fatalf("path %q must be refused", bad)
		}
	}

	// a file over the cap gets no diff (it would be false) and says so
	oldMax := maxFileView
	maxFileView = 10
	f, err = newSrc().File(ctx, id, "src/foo.go")
	maxFileView = oldMax
	if err != nil || len(f.Hunks) != 0 || f.DiffLabel != "archivo demasiado grande para mostrar el diff" {
		t.Fatalf("too big: %+v %v", f, err)
	}

	// engineer: d2 renders once for equal sources, and its absence leaves the source alone
	stub(true, true)
	e, ok, err := newSrc().Engineer(ctx, eid)
	if err != nil || !ok {
		t.Fatalf("engineer: %v %v", ok, err)
	}
	if e.Today.Source != "a -> b" || e.Today.Renderer != "" || len(e.Today.Lines) != 0 || e.Today.Lines == nil {
		t.Fatalf("without d2: %+v", e.Today)
	}
	renders := 0
	viewTools.FindD2 = func() (string, string, bool) { return "d2bin", "0.7.1", true }
	viewTools.RenderD2 = func(_ context.Context, bin, s string) ([]string, error) {
		renders++
		return []string{"[" + s + "]"}, nil
	}
	s2 := newSrc()
	e, _, err = s2.Engineer(ctx, eid)
	if err != nil {
		t.Fatal(err)
	}
	if e.Today.Renderer != "d2 0.7.1 txt" || !reflect.DeepEqual(e.Today.Lines, []string{"[a -> b]"}) {
		t.Fatalf("with d2: %+v", e.Today)
	}
	if !strings.Contains(e.Options[0].Diagram.Lines[0], "a -> b") || renders != 2 {
		t.Fatalf("renders %d, option %+v", renders, e.Options[0].Diagram)
	}
	if _, _, err := s2.Engineer(ctx, eid); err != nil || renders != 2 {
		t.Fatalf("second read must come from the cache: %d (%v)", renders, err)
	}
}

func TestD2CacheCachesFailure(t *testing.T) {
	old := viewTools
	t.Cleanup(func() { viewTools = old })
	calls := 0
	viewTools.FindD2 = func() (string, string, bool) { return "d2bin", "0.7.1", true }
	viewTools.RenderD2 = func(context.Context, string, string) ([]string, error) {
		calls++
		return nil, errors.New("boom")
	}
	c := &d2Cache{}
	for i := 0; i < 2; i++ {
		d := c.draw(context.Background(), "a -> b")
		if d.Source != "a -> b" || d.Lines == nil || len(d.Lines) != 0 || d.Renderer != "" {
			t.Fatalf("failed draw: %+v", d)
		}
	}
	if calls != 1 {
		t.Fatalf("a broken source is tried once: %d", calls)
	}
}

func TestD2CacheDoesNotCacheCancel(t *testing.T) {
	old := viewTools
	t.Cleanup(func() { viewTools = old })
	calls := 0
	viewTools.FindD2 = func() (string, string, bool) { return "d2bin", "0.7.1", true }
	viewTools.RenderD2 = func(ctx context.Context, _, s string) ([]string, error) {
		calls++
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return []string{s}, nil
	}
	c := &d2Cache{}
	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	if d := c.draw(cctx, "a -> b"); len(d.Lines) != 0 || d.Renderer != "" {
		t.Fatalf("cancelled draw: %+v", d)
	}
	d := c.draw(context.Background(), "a -> b")
	if calls != 2 || len(d.Lines) != 1 {
		t.Fatalf("a cancelled render is tried again: calls %d, %+v", calls, d)
	}
}

func TestTestRefsWholeWord(t *testing.T) {
	f := domain.Feature{Affected: &domain.AffectedRun{Failure: "--- FAIL: TestX\n    src/NotaTestHelper.java:12: boom\n"}}
	got := testRefs(f, []string{"src/NotaTest.java", "src/NotaTestHelper.java"})
	if got[0].Status != "none" || got[1].Status != "fail" {
		t.Fatalf("only the helper fails: %+v", got)
	}
	f.Affected.Failure = "FAIL src/NotaTest.java:3 x"
	got = testRefs(f, []string{"src/NotaTest.java", "src/NotaTestHelper.java"})
	if got[0].Status != "fail" || got[1].Status != "none" {
		t.Fatalf("only NotaTest fails: %+v", got)
	}
}
