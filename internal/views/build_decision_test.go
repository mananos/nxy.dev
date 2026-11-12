package views

import (
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
)

func TestBuildDecision(t *testing.T) {
	// approval
	f := baseFeature(domain.PhaseApproval)
	f.Next = domain.Action{Kind: domain.AskApproval}
	d, ok := BuildDecision(f, testNow.UnixMilli())
	if !ok || d.Kind != "approval" || d.ID != "F1:approval:2" || d.FeatureID != "F1" || len(d.Options) != 3 || len(d.Rest) != 0 {
		t.Fatalf("approval = %+v, %v", d, ok)
	}
	if d.AskedAt != "2026-11-11T12:00:00Z" || len(d.Body) == 0 || d.Header != "Plan" {
		t.Errorf("approval header = %+v", d)
	}
	for _, o := range d.Options {
		if o.Recommended || !o.Action.Planned || o.Action.ID == "" || o.Action.Argv[1] != "action" {
			t.Errorf("option = %+v", o)
		}
	}

	// batch failed: title and the failure as body, Parar asks to confirm
	f = baseFeature(domain.PhaseBatchFailed)
	f.State.Batch = 3
	f.Next = domain.Action{Kind: domain.AskDecision}
	f.SetProof(domain.Proof{Batch: 3, Failure: "boom\nline 2"})
	d, ok = BuildDecision(f, 0)
	if !ok || d.Kind != "batch_failed" || d.Title != "Lote 3 fallido" || len(d.Body) != 2 || d.AskedAt != "" {
		t.Fatalf("batch failed = %+v, %v", d, ok)
	}
	last := d.Options[len(d.Options)-1]
	if last.Action.ID != "parar" || last.Action.Confirm == "" || d.Options[0].Action.Confirm != "" {
		t.Errorf("options = %+v", d.Options)
	}
	if got := strings.Join(d.Options[0].Action.Argv, " "); got != "nxy action decide --feature F1 --question 1 --option 1" {
		t.Errorf("argv = %q", got)
	}

	// branch with two questions: the second goes to Rest
	f = baseFeature(domain.PhaseBranch)
	f.Next = domain.Action{Kind: domain.AskBranch}
	f.BranchOffer = &domain.BranchOffer{
		Suggest: []string{"feat/x", "main"},
		Repos:   []domain.BranchRepoOffer{{Name: "api", Current: "main", Bases: []string{"main", "develop"}}},
	}
	d, ok = BuildDecision(f, 0)
	if !ok || d.Kind != "branch" || len(d.Rest) != 1 || d.Header != "Rama" {
		t.Fatalf("branch = %+v, %v", d, ok)
	}
	if !d.Options[0].Recommended || d.Rest[0].Options[0].Action.Argv[6] != "2" {
		t.Errorf("branch options = %+v / %+v", d.Options, d.Rest)
	}

	// a step that is no question
	f = baseFeature(domain.PhaseRunningBatch)
	f.Next = domain.Action{Kind: domain.DispatchImplementer, Batch: 1}
	if _, ok := BuildDecision(f, 0); ok {
		t.Error("a dispatch is not a decision")
	}
}
