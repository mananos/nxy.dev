package design

import (
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/domain"
)

func TestAgendaAndProposal(t *testing.T) {
	d := &domain.Design{}
	SetAgenda(d, []string{"a", "b"})
	p := prop("B1")
	p.Dissent = "el N+1 sigue"
	Propose(d, p)
	if got := Proposal(d); !strings.Contains(got, "Claude mantiene su desacuerdo: el N+1 sigue") {
		t.Fatal(got)
	}
	Approve(d)
	if got := Agenda(d); !strings.Contains(got, "1 de 2 decididas") {
		t.Fatal(got)
	}
}

func TestCloseSummaryKeepsD2Source(t *testing.T) {
	d := &domain.Design{}
	SetAgenda(d, []string{"a"})
	ApplyClose(d, domain.DesignClose{Criteria: []domain.Criterion{{ID: "R1", Text: "hace x"}}, D2: "a -> b", Delegate: true})
	got := CloseSummary(domain.Feature{ID: "f", Design: d})
	for _, w := range []string{"```d2\na -> b\n```", "R1 hace x", "Las elige el planner", "B1 a"} {
		if !strings.Contains(got, w) {
			t.Errorf("summary lacks %q:\n%s", w, got)
		}
	}
}
