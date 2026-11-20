package views

import (
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/ask"
	"github.com/mananos/nxy.dev/v2/internal/domain"
)

// waitingOf is the row the hook would hold for the ask of f.
func waitingOf(t *testing.T, f domain.Feature, id string) []domain.PendingAsk {
	t.Helper()
	a, ok := ask.For(f)
	if !ok {
		t.Fatal("no ask")
	}
	row := domain.PendingAsk{ID: id, FeatureID: f.ID}
	for _, q := range a.Questions {
		aq := domain.AskedQuestion{Question: q.Question, Header: q.Header, MultiSelect: q.MultiSelect}
		for _, o := range q.Options {
			aq.Options = append(aq.Options, o.Label)
		}
		row.Questions = append(row.Questions, aq)
	}
	return []domain.PendingAsk{row}
}

func TestBuildDecision(t *testing.T) {
	// approval, nothing waiting: options carry no argv and are not planned
	f := baseFeature(domain.PhaseApproval)
	f.Next = domain.Action{Kind: domain.AskApproval}
	d, ok := BuildDecision(f, testNow.UnixMilli(), nil)
	if !ok || d.Kind != "approval" || d.ID != "F1:approval:2" || d.FeatureID != "F1" || len(d.Options) != 3 || len(d.Rest) != 0 {
		t.Fatalf("approval = %+v, %v", d, ok)
	}
	if d.AskedAt != "2026-11-11T12:00:00Z" || len(d.Body) == 0 || d.Header != "Plan" || d.AskID != "" || len(d.Release.Argv) != 0 {
		t.Errorf("approval header = %+v", d)
	}
	for _, o := range d.Options {
		if o.Recommended || o.Action.Planned || o.Action.ID == "" || len(o.Action.Argv) != 0 {
			t.Errorf("option = %+v", o)
		}
	}

	// approval with its ask waiting: each option answers with decide --ask
	d, _ = BuildDecision(f, 0, waitingOf(t, f, "A1"))
	if d.AskID != "A1" || strings.Join(d.Release.Argv, " ") != "nxy action decide --ask A1 --release" {
		t.Fatalf("waiting = %+v", d)
	}
	if got := strings.Join(d.Options[0].Action.Argv, " "); got != "nxy action decide --ask A1 --answer "+d.Options[0].Label {
		t.Errorf("argv = %q", got)
	}
	// a row of another feature does not count
	other := waitingOf(t, f, "A2")
	other[0].FeatureID = "F9"
	if d, _ = BuildDecision(f, 0, other); d.AskID != "" {
		t.Errorf("ask of another feature = %q", d.AskID)
	}

	// batch failed: title and the failure as body, Parar asks to confirm
	f = baseFeature(domain.PhaseBatchFailed)
	f.State.Batch = 3
	f.Next = domain.Action{Kind: domain.AskDecision}
	f.SetProof(domain.Proof{Batch: 3, Failure: "boom\nline 2"})
	d, ok = BuildDecision(f, 0, nil)
	if !ok || d.Kind != "batch_failed" || d.Title != "Lote 3 fallido" || len(d.Body) != 2 || d.AskedAt != "" {
		t.Fatalf("batch failed = %+v, %v", d, ok)
	}
	last := d.Options[len(d.Options)-1]
	if last.Action.ID != "parar" || last.Action.Confirm == "" || d.Options[0].Action.Confirm != "" {
		t.Errorf("options = %+v", d.Options)
	}

	// branch with two questions: the second goes to Rest
	f = baseFeature(domain.PhaseBranch)
	f.Next = domain.Action{Kind: domain.AskBranch}
	f.BranchOffer = &domain.BranchOffer{
		Suggest: []string{"feat/x", "main"},
		Repos:   []domain.BranchRepoOffer{{Name: "api", Current: "main", Bases: []string{"main", "develop"}}},
	}
	d, ok = BuildDecision(f, 0, waitingOf(t, f, "A3"))
	if !ok || d.Kind != "branch" || len(d.Rest) != 1 || d.Header != "Rama" {
		t.Fatalf("branch = %+v, %v", d, ok)
	}
	if !d.Options[0].Recommended || d.Rest[0].Options[0].Action.Argv[4] != "A3" {
		t.Errorf("branch options = %+v / %+v", d.Options, d.Rest)
	}

	// a step that is no question
	f = baseFeature(domain.PhaseRunningBatch)
	f.Next = domain.Action{Kind: domain.DispatchImplementer, Batch: 1}
	if _, ok := BuildDecision(f, 0, nil); ok {
		t.Error("a dispatch is not a decision")
	}
}
