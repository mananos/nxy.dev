package main

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/domain"
	"github.com/mananos/nxy.dev/v2/internal/platform"
	"github.com/mananos/nxy.dev/v2/internal/store/sqlite"
)

func TestBatchCommand(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	repo := flowRepo(t)
	path, err := platform.DBPath()
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	s, err := sqlite.Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := &domain.Plan{
		Criteria: []domain.Criterion{{ID: "R1", Text: "x", Test: domain.TestTDD}},
		Batches: []domain.Batch{{N: 1, Covers: []string{"R1"},
			Test: domain.BatchTest{Kind: domain.BatchTestNew, Accept: "exit 3"}}},
	}
	id, err := s.CreateFeature(ctx, domain.Feature{
		Repo: platform.RepoKey(repo), ProjectDir: repo, Plan: p,
		State: domain.State{Phase: domain.PhaseRunningBatch, Batch: 1, Batches: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	code, out, errb := runCmd(t, "", "batch", "red", "--feature", id)
	if code != 0 || !strings.Contains(out, "red recorded") {
		t.Fatalf("red: code %d, out %q, err %q", code, out, errb)
	}
	code, out, errb = runCmd(t, "", "batch", "green", "--feature", id)
	if code != 1 || !strings.Contains(out, "green failed (exit 3)") {
		t.Fatalf("green: code %d, out %q, err %q", code, out, errb)
	}
	if code, _, _ := runCmd(t, "", "batch", "blue", "--feature", id); code != 1 {
		t.Errorf("unknown kind: code %d", code)
	}
}
