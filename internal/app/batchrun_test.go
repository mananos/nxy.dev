package app

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/domain"
)

type fakeShell struct {
	res   ShellResult
	calls int
	dir   string
	// during runs while the command "executes".
	during func()
}

func (s *fakeShell) Run(_ context.Context, dir, _ string) (ShellResult, error) {
	s.calls++
	s.dir = dir
	if s.during != nil {
		s.during()
	}
	return s.res, nil
}

// batchFeature stores a feature running batch 1 of a plan with one TDD rule.
func batchFeature(t *testing.T, kind domain.BatchTestKind, accept string) (Hooks, *memStore, *fakeShell, string) {
	t.Helper()
	h, st := flowHooks(false)
	sh := &fakeShell{}
	h.Shell = sh
	p := &domain.Plan{
		Criteria: []domain.Criterion{{ID: "R1", Text: "x", Test: domain.TestTDD}},
		Batches:  []domain.Batch{{N: 1, Covers: []string{"R1"}, Test: domain.BatchTest{Kind: kind, Accept: accept}}},
	}
	id, err := st.CreateFeature(context.Background(), domain.Feature{
		Repo: "r", ProjectDir: "/p", Plan: p,
		State: domain.State{Phase: domain.PhaseRunningBatch, Batch: 1, Batches: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	return h, st, sh, id
}

// runBatch runs `nxy batch` of the only running batch.
func runBatch(h Hooks, id string, kind RunKind) (BatchRun, error) {
	return h.RunBatchCheck(context.Background(), id, kind, 0)
}

// stored is the proof of batch 1 of the stored feature and whether the feature holds any proof.
func stored(st *memStore, id string) (domain.Proof, bool) {
	f := st.feats[id]
	return f.ProofOf(1), f.Proof != nil || len(f.Proofs) > 0
}

func TestRunBatchRedExitZeroRefused(t *testing.T) {
	h, st, sh, id := batchFeature(t, domain.BatchTestNew, "go test")
	sh.res = ShellResult{Exit: 0, Output: "ok"}
	r, err := runBatch(h, id, RunRed)
	if err != nil || r.Recorded || !strings.Contains(r.Refused, "passes without it") {
		t.Fatalf("r = %+v err = %v", r, err)
	}
	if pr, any := stored(st, id); any {
		t.Errorf("proof stored: %+v", pr)
	}
}

func TestRunBatchRedCommandNotFound(t *testing.T) {
	for _, code := range []int{127, 9009} {
		h, _, sh, id := batchFeature(t, domain.BatchTestNew, "nope")
		sh.res = ShellResult{Exit: code}
		r, _ := runBatch(h, id, RunRed)
		if r.Recorded || !strings.Contains(r.Refused, "not found") {
			t.Errorf("exit %d: %+v", code, r)
		}
	}
}

func TestRunBatchRedFailureRecorded(t *testing.T) {
	h, st, sh, id := batchFeature(t, domain.BatchTestNew, "go test")
	sh.res = ShellResult{Exit: 1, Output: "FAIL"}
	r, err := runBatch(h, id, RunRed)
	if err != nil || !r.Recorded || r.Exit != 1 {
		t.Fatalf("r = %+v err = %v", r, err)
	}
	pr, _ := stored(st, id)
	if pr.Batch != 1 || pr.Red == nil || pr.Red.Exit != 1 || pr.Red.Tail != "FAIL" {
		t.Errorf("proof = %+v", pr)
	}
}

func TestRunBatchGreenBeforeRedRefused(t *testing.T) {
	h, st, sh, id := batchFeature(t, domain.BatchTestNew, "go test")
	r, err := runBatch(h, id, RunGreen)
	if err != nil || r.Recorded || r.Refused == "" {
		t.Fatalf("r = %+v err = %v", r, err)
	}
	if pr, any := stored(st, id); sh.calls != 0 || any {
		t.Errorf("ran %d times, proof %+v", sh.calls, pr)
	}
}

func TestRunBatchGreenAfterRed(t *testing.T) {
	h, st, sh, id := batchFeature(t, domain.BatchTestNew, "go test")
	sh.res = ShellResult{Exit: 1}
	runBatch(h, id, RunRed)
	sh.res = ShellResult{Exit: 0, Output: "ok"}
	r, err := runBatch(h, id, RunGreen)
	if err != nil || !r.Recorded {
		t.Fatalf("r = %+v err = %v", r, err)
	}
	pr, _ := stored(st, id)
	if pr.Red == nil || pr.Green == nil || pr.Green.Exit != 0 {
		t.Errorf("proof = %+v", pr)
	}
}

type fakeFormat struct {
	bad   []string
	files []string
}

func (f *fakeFormat) Unformatted(_ context.Context, _ string, files []string) ([]string, error) {
	f.files = files
	return f.bad, nil
}

// formatBatch is a batch feature on an existing-test batch with a base and a changed .go and .md file.
func formatBatch(t *testing.T, bad []string) (Hooks, *memStore, *fakeFormat, string) {
	t.Helper()
	h, st, sh, id := batchFeature(t, domain.BatchTestExisting, "go test")
	sh.res = ShellResult{Exit: 0}
	f := st.feats[id]
	f.Base = "abc"
	st.feats[id] = f
	h.Changes = fakeChanges{changed: []string{"a/x.go", "README.md"}}
	ff := &fakeFormat{bad: bad}
	h.Format = ff
	return h, st, ff, id
}

func TestRunBatchGreenRefusesUnformatted(t *testing.T) {
	h, st, ff, id := formatBatch(t, []string{"a/x.go"})
	r, err := runBatch(h, id, RunGreen)
	if err != nil || r.Recorded || !strings.Contains(r.Refused, "gofmt -w a/x.go") {
		t.Fatalf("r = %+v err = %v", r, err)
	}
	if len(ff.files) != 1 || ff.files[0] != "a/x.go" {
		t.Errorf("checked %v, want only the .go", ff.files)
	}
	if pr, any := stored(st, id); any {
		t.Errorf("proof stored: %+v", pr)
	}
}

func TestRunBatchGreenFormattedRecorded(t *testing.T) {
	h, st, _, id := formatBatch(t, nil)
	r, err := runBatch(h, id, RunGreen)
	if err != nil || !r.Recorded {
		t.Fatalf("r = %+v err = %v", r, err)
	}
	if pr, _ := stored(st, id); pr.Green == nil {
		t.Errorf("proof = %+v", pr)
	}
}

func TestRunBatchGreenOnExistingBatch(t *testing.T) {
	h, st, sh, id := batchFeature(t, domain.BatchTestExisting, "go test")
	sh.res = ShellResult{Exit: 0}
	r, err := runBatch(h, id, RunGreen)
	if err != nil || !r.Recorded {
		t.Fatalf("r = %+v err = %v", r, err)
	}
	if pr, _ := stored(st, id); pr.Green == nil {
		t.Errorf("proof = %+v", pr)
	}
	r, _ = runBatch(h, id, RunRed)
	if r.Recorded || !strings.Contains(r.Refused, "not TDD") {
		t.Errorf("red on non-TDD: %+v", r)
	}
}

func TestRunBatchFailedGreenRecorded(t *testing.T) {
	h, st, sh, id := batchFeature(t, domain.BatchTestExisting, "go test")
	sh.res = ShellResult{Exit: 2, Output: "boom"}
	r, err := runBatch(h, id, RunGreen)
	if err != nil || !r.Recorded || r.Exit != 2 {
		t.Fatalf("r = %+v err = %v", r, err)
	}
	if pr, _ := stored(st, id); pr.Green == nil || pr.Green.Exit != 2 {
		t.Errorf("proof = %+v", pr)
	}
}

func TestRunBatchTimedOutGreenRecorded(t *testing.T) {
	h, st, sh, id := batchFeature(t, domain.BatchTestExisting, "go test")
	sh.res = ShellResult{Exit: -1, Output: "slow\nnxy: Accept timed out", TimedOut: true}
	r, err := runBatch(h, id, RunGreen)
	if err != nil || !r.Recorded || r.Exit != -1 {
		t.Fatalf("r = %+v err = %v", r, err)
	}
	if pr, _ := stored(st, id); pr.Green == nil || pr.Green.Exit != -1 || !strings.Contains(pr.Green.Tail, "timed out") {
		t.Errorf("proof = %+v", pr)
	}
}

func TestRunBatchTimedOutRedNotRecorded(t *testing.T) {
	h, st, sh, id := batchFeature(t, domain.BatchTestNew, "go test")
	sh.res = ShellResult{Exit: -1, Output: "nxy: Accept timed out", TimedOut: true}
	r, err := runBatch(h, id, RunRed)
	if err != nil || r.Recorded || !strings.Contains(r.Refused, "timed out") {
		t.Fatalf("r = %+v err = %v", r, err)
	}
	if pr, _ := stored(st, id); pr.Red != nil {
		t.Errorf("red recorded: %+v", pr)
	}
}

func TestRunBatchWrongPhaseRefused(t *testing.T) {
	h, st, _, id := batchFeature(t, domain.BatchTestExisting, "go test")
	f := st.feats[id]
	f.State.Phase = domain.PhasePlanning
	st.feats[id] = f
	if _, err := runBatch(h, id, RunGreen); err == nil || !strings.Contains(err.Error(), "not running a batch") {
		t.Errorf("err = %v", err)
	}
}

func TestRunBatchNoAccept(t *testing.T) {
	h, _, _, id := batchFeature(t, domain.BatchTestExisting, "")
	if _, err := runBatch(h, id, RunGreen); err == nil || !strings.Contains(err.Error(), "no Accept") {
		t.Errorf("err = %v", err)
	}
}

func TestRunBatchFeatureMovedOn(t *testing.T) {
	h, st, sh, id := batchFeature(t, domain.BatchTestExisting, "go test")
	sh.res = ShellResult{Exit: 0}
	sh.during = func() {
		f := st.feats[id]
		f.State.Phase = domain.PhaseBatchFailed
		st.feats[id] = f
	}
	r, err := runBatch(h, id, RunGreen)
	if err != nil || r.Recorded || r.Refused == "" {
		t.Fatalf("r = %+v err = %v", r, err)
	}
	if pr, any := stored(st, id); any {
		t.Errorf("proof stored: %+v", pr)
	}
}

func TestRunBatchTailCut(t *testing.T) {
	h, _, sh, id := batchFeature(t, domain.BatchTestExisting, "go test")
	var b strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	sh.res = ShellResult{Exit: 1, Output: b.String()}
	r, _ := runBatch(h, id, RunGreen)
	lines := strings.Split(r.Tail, "\n")
	if len(lines) != acceptTailLines+1 || lines[len(lines)-1] != "line 100" || lines[0] != "… 60 líneas anteriores recortadas" || lines[1] != "line 61" {
		t.Errorf("tail has %d lines: first %q last %q", len(lines), lines[0], lines[len(lines)-1])
	}
}

func TestTailLinesMarksCut(t *testing.T) {
	if got := tailLines("a\nb\nc", 5); got != "a\nb\nc" {
		t.Errorf("no cut: %q", got)
	}
	var b strings.Builder
	for i := 1; i <= 43; i++ {
		fmt.Fprintf(&b, "l%d\n", i)
	}
	got := strings.Split(tailLines(b.String(), 40), "\n")
	if len(got) != 41 || got[0] != "… 3 líneas anteriores recortadas" || got[1] != "l4" || got[40] != "l43" {
		t.Errorf("cut: %q", got)
	}
}

func longGoFailOutput() string {
	var b strings.Builder
	b.WriteString("FAIL\tgithub.com/x/cmd/nxy\t0.5s\n--- FAIL: TestA (0.00s)\nFAIL\tgithub.com/x/internal/app\t1.2s\n")
	for i := 1; i <= 60; i++ {
		fmt.Fprintf(&b, "ok  \tgithub.com/x/pkg%d\t0.1s\n", i)
	}
	return b.String()
}

func TestFailTail(t *testing.T) {
	out := failTail(longGoFailOutput(), acceptTailLines)
	for _, want := range []string{"FAIL\tgithub.com/x/cmd/nxy", "--- FAIL: TestA", "FAIL\tgithub.com/x/internal/app", "pkg60"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if !strings.HasPrefix(out, "… 23 líneas anteriores recortadas\n") {
		t.Errorf("no cut line:\n%s", out)
	}
	if strings.Contains(out, "pkg1\t") {
		t.Errorf("ok lines outside the tail kept:\n%s", out)
	}
	if got := failTail("a\nb\nc", 2); got != "… 1 líneas anteriores recortadas\nb\nc" {
		t.Errorf("no failures: %q", got)
	}
}

func TestRunBatchRedCodeFirst(t *testing.T) {
	ctx := context.Background()
	h, st, sh, id := batchFeature(t, domain.BatchTestNew, "go test")
	for _, p := range []string{"x_test.go", "testdata/golden/a.txt", "docs/plan.md", "internal/x/testdata/b.golden"} {
		h.noteWrite(ctx, id, 1, p)
	}
	sh.res = ShellResult{Exit: 1, Output: "FAIL"}
	r, err := runBatch(h, id, RunRed)
	if err != nil || !r.Recorded || r.Warn != "" {
		t.Fatalf("r = %+v err = %v", r, err)
	}
	if pr, _ := stored(st, id); pr.Red == nil || len(pr.Red.CodeFirst) != 0 {
		t.Errorf("marked without code: %+v", pr.Red)
	}

	h, st, sh, id = batchFeature(t, domain.BatchTestNew, "go test")
	h.noteWrite(ctx, id, 1, "x.go")
	h.noteWrite(ctx, id, 1, "x.go")
	sh.res = ShellResult{Exit: 1, Output: "FAIL"}
	r, err = runBatch(h, id, RunRed)
	if err != nil || !r.Recorded || !strings.Contains(r.Warn, "x.go") {
		t.Fatalf("r = %+v err = %v", r, err)
	}
	pr, _ := stored(st, id)
	if pr.Red == nil || len(pr.Red.CodeFirst) != 1 || pr.Red.CodeFirst[0] != "x.go" {
		t.Errorf("red = %+v", pr.Red)
	}
	h.noteWrite(ctx, id, 1, "y.go")
	if pr, _ := stored(st, id); len(pr.CodeFirst) != 1 {
		t.Errorf("code after the red counted: %+v", pr.CodeFirst)
	}
}

func TestRunBatchRunsInRepoCheckout(t *testing.T) {
	h, st, sh, id := batchFeature(t, domain.BatchTestNew, "go test")
	f := st.feats[id]
	f.Plan.Repos = []domain.PlanRepo{{Path: "api"}, {Path: "web"}}
	f.Plan.Batches[0].Repo = "web"
	f.Repos = []domain.FeatureRepo{
		{Name: "api", Root: "/w/api", Checkout: "/w/api.worktrees/x"},
		{Name: "web", Root: "/w/web", Checkout: "/w/web.worktrees/x"},
	}
	st.feats[id] = f
	sh.res = ShellResult{Exit: 1, Output: "FAIL"}
	if _, err := runBatch(h, id, RunRed); err != nil {
		t.Fatal(err)
	}
	if sh.dir != "/w/web.worktrees/x" {
		t.Errorf("dir = %q", sh.dir)
	}
}

func TestRunBatchSeveralRunningNeedsFlag(t *testing.T) {
	h, st, sh, id := batchFeature(t, domain.BatchTestExisting, "go test")
	f := st.feats[id]
	f.Plan.Batches = append(f.Plan.Batches, domain.Batch{N: 2, Test: domain.BatchTest{Kind: domain.BatchTestExisting, Accept: "go test"}})
	f.State.Batches = 2
	f.State.Steps = []domain.Step{{N: 1, Status: domain.StepRunning}, {N: 2, Status: domain.StepRunning}}
	st.feats[id] = f
	sh.res = ShellResult{Exit: 0}
	if _, err := runBatch(h, id, RunGreen); err == nil || !strings.Contains(err.Error(), "pass --batch") {
		t.Fatalf("err = %v", err)
	}
	r, err := h.RunBatchCheck(context.Background(), id, RunGreen, 2)
	if err != nil || !r.Recorded {
		t.Fatalf("r = %+v err = %v", r, err)
	}
	g := st.feats[id]
	if g.ProofOf(2).Green == nil || g.ProofOf(1).Green != nil {
		t.Errorf("proofs = %+v", g.Proofs)
	}
}
