package app

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
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

func TestRunBatchRedExitZeroRefused(t *testing.T) {
	h, st, sh, id := batchFeature(t, domain.BatchTestNew, "go test")
	sh.res = ShellResult{Exit: 0, Output: "ok"}
	r, err := h.RunBatchCheck(context.Background(), id, RunRed)
	if err != nil || r.Recorded || !strings.Contains(r.Refused, "passes without it") {
		t.Fatalf("r = %+v err = %v", r, err)
	}
	if st.feats[id].Proof != nil {
		t.Errorf("proof stored: %+v", st.feats[id].Proof)
	}
}

func TestRunBatchRedCommandNotFound(t *testing.T) {
	for _, code := range []int{127, 9009} {
		h, _, sh, id := batchFeature(t, domain.BatchTestNew, "nope")
		sh.res = ShellResult{Exit: code}
		r, _ := h.RunBatchCheck(context.Background(), id, RunRed)
		if r.Recorded || !strings.Contains(r.Refused, "not found") {
			t.Errorf("exit %d: %+v", code, r)
		}
	}
}

func TestRunBatchRedFailureRecorded(t *testing.T) {
	h, st, sh, id := batchFeature(t, domain.BatchTestNew, "go test")
	sh.res = ShellResult{Exit: 1, Output: "FAIL"}
	r, err := h.RunBatchCheck(context.Background(), id, RunRed)
	if err != nil || !r.Recorded || r.Exit != 1 {
		t.Fatalf("r = %+v err = %v", r, err)
	}
	pr := st.feats[id].Proof
	if pr == nil || pr.Batch != 1 || pr.Red == nil || pr.Red.Exit != 1 || pr.Red.Tail != "FAIL" {
		t.Errorf("proof = %+v", pr)
	}
}

func TestRunBatchGreenBeforeRedRefused(t *testing.T) {
	h, st, sh, id := batchFeature(t, domain.BatchTestNew, "go test")
	r, err := h.RunBatchCheck(context.Background(), id, RunGreen)
	if err != nil || r.Recorded || r.Refused == "" {
		t.Fatalf("r = %+v err = %v", r, err)
	}
	if sh.calls != 0 || st.feats[id].Proof != nil {
		t.Errorf("ran %d times, proof %+v", sh.calls, st.feats[id].Proof)
	}
}

func TestRunBatchGreenAfterRed(t *testing.T) {
	h, st, sh, id := batchFeature(t, domain.BatchTestNew, "go test")
	sh.res = ShellResult{Exit: 1}
	h.RunBatchCheck(context.Background(), id, RunRed)
	sh.res = ShellResult{Exit: 0, Output: "ok"}
	r, err := h.RunBatchCheck(context.Background(), id, RunGreen)
	if err != nil || !r.Recorded {
		t.Fatalf("r = %+v err = %v", r, err)
	}
	pr := st.feats[id].Proof
	if pr.Red == nil || pr.Green == nil || pr.Green.Exit != 0 {
		t.Errorf("proof = %+v", pr)
	}
}

func TestRunBatchGreenOnExistingBatch(t *testing.T) {
	h, st, sh, id := batchFeature(t, domain.BatchTestExisting, "go test")
	sh.res = ShellResult{Exit: 0}
	r, err := h.RunBatchCheck(context.Background(), id, RunGreen)
	if err != nil || !r.Recorded {
		t.Fatalf("r = %+v err = %v", r, err)
	}
	if pr := st.feats[id].Proof; pr == nil || pr.Green == nil {
		t.Errorf("proof = %+v", pr)
	}
	r, _ = h.RunBatchCheck(context.Background(), id, RunRed)
	if r.Recorded || !strings.Contains(r.Refused, "not TDD") {
		t.Errorf("red on non-TDD: %+v", r)
	}
}

func TestRunBatchFailedGreenRecorded(t *testing.T) {
	h, st, sh, id := batchFeature(t, domain.BatchTestExisting, "go test")
	sh.res = ShellResult{Exit: 2, Output: "boom"}
	r, err := h.RunBatchCheck(context.Background(), id, RunGreen)
	if err != nil || !r.Recorded || r.Exit != 2 {
		t.Fatalf("r = %+v err = %v", r, err)
	}
	if pr := st.feats[id].Proof; pr == nil || pr.Green == nil || pr.Green.Exit != 2 {
		t.Errorf("proof = %+v", pr)
	}
}

func TestRunBatchTimedOutGreenRecorded(t *testing.T) {
	h, st, sh, id := batchFeature(t, domain.BatchTestExisting, "go test")
	sh.res = ShellResult{Exit: -1, Output: "slow\nnxy: Accept timed out", TimedOut: true}
	r, err := h.RunBatchCheck(context.Background(), id, RunGreen)
	if err != nil || !r.Recorded || r.Exit != -1 {
		t.Fatalf("r = %+v err = %v", r, err)
	}
	if pr := st.feats[id].Proof; pr == nil || pr.Green == nil || pr.Green.Exit != -1 || !strings.Contains(pr.Green.Tail, "timed out") {
		t.Errorf("proof = %+v", pr)
	}
}

func TestRunBatchTimedOutRedNotRecorded(t *testing.T) {
	h, st, sh, id := batchFeature(t, domain.BatchTestNew, "go test")
	sh.res = ShellResult{Exit: -1, Output: "nxy: Accept timed out", TimedOut: true}
	r, err := h.RunBatchCheck(context.Background(), id, RunRed)
	if err != nil || r.Recorded || !strings.Contains(r.Refused, "timed out") {
		t.Fatalf("r = %+v err = %v", r, err)
	}
	if pr := st.feats[id].Proof; pr != nil && pr.Red != nil {
		t.Errorf("red recorded: %+v", pr)
	}
}

func TestRunBatchWrongPhaseRefused(t *testing.T) {
	h, st, _, id := batchFeature(t, domain.BatchTestExisting, "go test")
	f := st.feats[id]
	f.State.Phase = domain.PhasePlanning
	st.feats[id] = f
	if _, err := h.RunBatchCheck(context.Background(), id, RunGreen); err == nil || !strings.Contains(err.Error(), "not running a batch") {
		t.Errorf("err = %v", err)
	}
}

func TestRunBatchNoAccept(t *testing.T) {
	h, _, _, id := batchFeature(t, domain.BatchTestExisting, "")
	if _, err := h.RunBatchCheck(context.Background(), id, RunGreen); err == nil || !strings.Contains(err.Error(), "no Accept") {
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
	r, err := h.RunBatchCheck(context.Background(), id, RunGreen)
	if err != nil || r.Recorded || r.Refused == "" {
		t.Fatalf("r = %+v err = %v", r, err)
	}
	if st.feats[id].Proof != nil {
		t.Errorf("proof stored: %+v", st.feats[id].Proof)
	}
}

func TestRunBatchTailCut(t *testing.T) {
	h, _, sh, id := batchFeature(t, domain.BatchTestExisting, "go test")
	var b strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	sh.res = ShellResult{Exit: 1, Output: b.String()}
	r, _ := h.RunBatchCheck(context.Background(), id, RunGreen)
	lines := strings.Split(r.Tail, "\n")
	if len(lines) != acceptTailLines || lines[len(lines)-1] != "line 100" || lines[0] != "line 61" {
		t.Errorf("tail has %d lines: first %q last %q", len(lines), lines[0], lines[len(lines)-1])
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
	if _, err := h.RunBatchCheck(context.Background(), id, RunRed); err != nil {
		t.Fatal(err)
	}
	if sh.dir != "/w/web.worktrees/x" {
		t.Errorf("dir = %q", sh.dir)
	}
}
