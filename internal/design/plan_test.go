package design

import (
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
)

func closedDesign() *domain.Design {
	return &domain.Design{
		Decisions: []domain.Decision{{ID: "D1", Scope: domain.ScopeTask, Text: "x", Why: "y"}},
		Agenda: []domain.Fork{
			{ID: "B1", Status: domain.ForkDecided},
			{ID: "B2", Status: domain.ForkDelegated},
		},
		Rules:  []domain.Rule{{ID: "R1", Text: "r"}},
		Closed: true,
	}
}

func TestCheckPlanEngineer(t *testing.T) {
	d := closedDesign()
	ok := domain.Plan{Rules: []domain.Rule{{ID: "R1", Text: "r", Test: domain.TestTDD}}}
	if probs := CheckPlan(d, ok); len(probs) != 0 {
		t.Errorf("problems = %v", probs)
	}
	bad := domain.Plan{
		Rules:     []domain.Rule{{ID: "R1", Text: "changed"}},
		Questions: []domain.Question{{Question: "q"}},
		Decisions: []domain.Decision{{Fork: "B1"}},
	}
	if probs := CheckPlan(d, bad); len(probs) != 3 {
		t.Errorf("problems = %v", probs)
	}
}

func TestMergeMarksPlannerDecisions(t *testing.T) {
	d := closedDesign()
	p := domain.Plan{Decisions: []domain.Decision{{Scope: domain.ScopeTask, Text: "z", Fork: "B2"}}}
	Merge(d, &p)
	if len(p.Decisions) != 2 || p.Decisions[0].ID != "D1" || p.Decisions[1].ID != "D2" || p.Decisions[1].By != domain.ByPlanner {
		t.Errorf("decisions = %+v", p.Decisions)
	}
	if len(d.Decisions) != 1 {
		t.Error("Merge changed the design")
	}
}

func TestGaps(t *testing.T) {
	titles, err := DecodeGaps(strings.NewReader(`{"gaps":["a"]}`))
	if err != nil || len(titles) != 1 {
		t.Fatalf("%v %v", titles, err)
	}
	if _, err := DecodeGaps(strings.NewReader(`{"gap":["a"]}`)); err == nil {
		t.Error("unknown field accepted")
	}
	if len(ValidateGaps(nil)) == 0 || len(ValidateGaps([]string{"a\nb"})) == 0 {
		t.Error("bad gaps accepted")
	}
	d := closedDesign()
	AddGaps(d, titles)
	if d.Closed || d.Agenda[2].ID != "B3" || d.Agenda[2].Source != domain.SourcePlanner {
		t.Errorf("design = %+v", d)
	}
}
