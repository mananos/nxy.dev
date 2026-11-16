package design

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
)

func prop(fork string) ProposalInput {
	return ProposalInput{Fork: fork, Text: "usar cola", Why: "desacopla", Scope: domain.ScopeTask}
}

func TestSetAgendaKeepsDecided(t *testing.T) {
	d := &domain.Design{}
	if p := SetAgenda(d, []string{"a", "b"}); p != nil {
		t.Fatal(p)
	}
	Propose(d, prop("B1"))
	Approve(d)
	if p := SetAgenda(d, []string{"c"}); p != nil {
		t.Fatal(p)
	}
	if len(d.Agenda) != 2 || d.Agenda[0].Status != domain.ForkDecided || d.Agenda[1].ID != "B3" {
		t.Fatalf("agenda: %+v", d.Agenda)
	}
}

func TestValidateProposal(t *testing.T) {
	d := &domain.Design{}
	SetAgenda(d, []string{"a"})
	if p := ValidateProposal(d, prop("B9")); len(p) == 0 {
		t.Fatal("unknown fork accepted")
	}
	bad := prop("B1")
	bad.Why = ""
	bad.Text = "a\nb"
	bad.Scope = "x"
	if p := ValidateProposal(d, bad); len(p) != 3 {
		t.Fatalf("problems: %v", p)
	}
	Propose(d, prop("B1"))
	if p := ValidateProposal(d, prop("B1")); len(p) == 0 {
		t.Fatal("second pending accepted")
	}
}

func TestTextLimitCountsCharacters(t *testing.T) {
	d := &domain.Design{}
	ok := strings.Repeat("ñ", 200)
	if p := SetAgenda(d, []string{ok}); p != nil {
		t.Fatalf("200 characters rejected: %v", p)
	}
	if p := SetAgenda(&domain.Design{}, []string{ok + "ñ"}); len(p) == 0 {
		t.Fatal("201 characters accepted")
	}
	pr := prop("B1")
	pr.Text = ok
	if p := ValidateProposal(d, pr); len(p) != 0 {
		t.Fatalf("200-character text rejected: %v", p)
	}
	pr.Text = ok + "ñ"
	if p := ValidateProposal(d, pr); len(p) == 0 {
		t.Fatal("201-character text accepted")
	}
}

func TestApproveNumbers(t *testing.T) {
	d := &domain.Design{}
	SetAgenda(d, []string{"a", "b"})
	Propose(d, prop("B1"))
	x, _ := Approve(d)
	Propose(d, prop("B2"))
	y, _ := Approve(d)
	if x.ID != "D1" || y.ID != "D2" || d.Pending != nil || d.Agenda[1].Status != domain.ForkDecided {
		t.Fatalf("%+v", d)
	}
}

func TestCloseNeedsDelegateForOpenForks(t *testing.T) {
	d := &domain.Design{}
	SetAgenda(d, []string{"a"})
	c := domain.DesignClose{Criteria: []domain.Criterion{{ID: "R1", Text: "hace x"}}, D2: "a -> b"}
	if p := ValidateClose(d, c); len(p) == 0 {
		t.Fatal("open fork without delegate accepted")
	}
	c.Delegate = true
	if p := ValidateClose(d, c); len(p) != 0 {
		t.Fatal(p)
	}
	ApplyClose(d, c)
	if !d.Closed || d.D2 != "a -> b" || d.Agenda[0].Status != domain.ForkDelegated {
		t.Fatalf("%+v", d)
	}
}

func TestSetAgendaRejectedWhilePending(t *testing.T) {
	d := &domain.Design{}
	SetAgenda(d, []string{"a"})
	Propose(d, prop("B1"))
	if p := SetAgenda(d, []string{"b"}); len(p) == 0 {
		t.Fatal("agenda replaced with a pending proposal")
	}
	d.Pending = nil
	d.Close = &domain.DesignClose{}
	if p := SetAgenda(d, []string{"b"}); len(p) == 0 {
		t.Fatal("agenda replaced with a pending close")
	}
}

func TestValidateCloseRejectsFencesInD2(t *testing.T) {
	d := &domain.Design{}
	c := domain.DesignClose{Criteria: []domain.Criterion{{ID: "R1", Text: "hace x"}}, D2: "a -> b\n```\nmalo"}
	if p := ValidateClose(d, c); len(p) == 0 {
		t.Fatal("d2 with fence accepted")
	}
}

func TestGuideCarriesDecisionsAndPending(t *testing.T) {
	d := &domain.Design{}
	SetAgenda(d, []string{"a", "b"})
	Propose(d, ProposalInput{Fork: "B1", Text: "usar cola", Why: "desacopla", Dissent: "prefiero tabla", Scope: domain.ScopeTask})
	Approve(d)
	Propose(d, prop("B2"))
	g := Guide(domain.Feature{ID: "f1", Design: d}, "nxy")
	for _, w := range []string{"D1", "usar cola", "desacopla", "prefiero tabla", "esperando respuesta"} {
		if !strings.Contains(g, w) {
			t.Errorf("guide lacks %q", w)
		}
	}
}

func TestGuideCarriesForkQuestion(t *testing.T) {
	d := &domain.Design{}
	SetAgenda(d, []string{"a"})
	if p := SetForkOptions(d, ForkInput{Fork: "B1", Opinion: "o", Options: []domain.ForkOption{
		{Label: "En memoria", Summary: "s"}, {Label: "En disco", Summary: "s"}}}); p != nil {
		t.Fatal(p)
	}
	g := Guide(domain.Feature{ID: "f1", Design: d}, "nxy")
	if !strings.Contains(g, ForkQuestion("f1", "B1")) || !strings.Contains(g, "En disco") {
		t.Errorf("guide lacks the fork question: %s", g)
	}
}

func TestGuideCarriesLastRefs(t *testing.T) {
	d := &domain.Design{}
	for i := 1; i <= 12; i++ {
		d.Refs = append(d.Refs, fmt.Sprintf("mensaje-%02d-fin", i))
	}
	d.Refs = append(d.Refs, strings.Repeat("ñ", 500))
	g := Guide(domain.Feature{ID: "f1", Design: d}, "nxy")
	for _, w := range []string{"mensaje-04-fin", "mensaje-12-fin", strings.Repeat("ñ", 300) + "…"} {
		if !strings.Contains(g, w) {
			t.Errorf("guide lacks %q", w)
		}
	}
	for _, w := range []string{"mensaje-01-fin", "mensaje-03-fin", strings.Repeat("ñ", 301)} {
		if strings.Contains(g, w) {
			t.Errorf("guide has %q", w)
		}
	}
}

func TestGuideHasCommands(t *testing.T) {
	f := domain.Feature{ID: "f1", Request: "hacer x", Design: &domain.Design{}}
	g := Guide(f, `C:\bin\nxy.exe`)
	for _, w := range []string{"design agenda --feature f1", "design today --feature f1", "design propose --feature f1", "design close --feature f1", "salvo que", "C:/bin/nxy.exe", "una sola vez"} {
		if !strings.Contains(g, w) {
			t.Errorf("guide lacks %q", w)
		}
	}
}
