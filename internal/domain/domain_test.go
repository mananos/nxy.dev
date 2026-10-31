package domain

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestEveryPhaseHasPublicAndLabel(t *testing.T) {
	for _, p := range Phases {
		if p.Public() == "" {
			t.Errorf("%s: no public status", p)
		}
		if p.Label() == "" {
			t.Errorf("%s: no label", p)
		}
	}
	if len(phaseTable) != len(Phases) {
		t.Errorf("table has %d phases, list has %d", len(phaseTable), len(Phases))
	}
	if OutcomeStopped.Label() != "Parada" {
		t.Errorf("stopped label %q", OutcomeStopped.Label())
	}
}

func TestEveryActionHasLabel(t *testing.T) {
	for k := range actionTable {
		if (Action{Kind: k}).Label() == "" {
			t.Errorf("%s: no label", k)
		}
	}
	if got := (Action{Kind: DispatchImplementer, Batch: 1}).Label(); got != "Lote 1" {
		t.Errorf("label %q", got)
	}
	if !(Action{Kind: RunAffected}).MainThread() {
		t.Error("RunAffected must be main-thread")
	}
	if !(Action{Kind: AskApproval}).MainThread() || (Action{Kind: Close}).MainThread() {
		t.Error("MainThread wrong")
	}
}

func TestFeatureJSONRoundTrip(t *testing.T) {
	f := Feature{
		ID: "f1", Repo: "github.com/o/r", ProjectDir: "/p", Request: "do it\nnow",
		State:       State{Phase: PhaseApproval, Batch: 1, Batches: 2, Attempt: 1, Outcome: OutcomeNone},
		Next:        Action{Kind: AskApproval, Batch: 1, Retry: true},
		PlanRev:     2,
		Answers:     []Answer{{Question: "q", Answer: "a"}},
		ChangeNotes: []string{"note"},
		Plan: &Plan{
			Title:     "T",
			Decisions: []Decision{{Scope: ScopeRepo, Text: "d", Question: "q"}},
			Rules:     []Rule{{ID: "R1", Text: "r", Test: TestManual, Check: "look"}},
			Batches: []Batch{{
				N: 1, Title: "b", Covers: []string{"R1"}, Read: []string{"a.go"},
				Change:  []Change{{At: "a.go:1", What: "w"}},
				Test:    BatchTest{Kind: BatchTestNew, Name: "TestX", Accept: "go test ./..."},
				Risk:    RiskHigh,
				Depends: []int{},
			}},
			Risks: []string{"r"},
			Questions: []Question{{Question: "q", Header: "h",
				Options: []Option{{Label: "l", Description: "d", Recommended: true}}}},
			NoTests: true,
		},
	}
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	var got Feature
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	f.Plan.Batches[0].Depends = nil // omitempty drops the empty slice
	if !reflect.DeepEqual(f, got) {
		t.Errorf("round trip differs:\nwant %+v\ngot  %+v", f, got)
	}
}
