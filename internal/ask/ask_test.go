package ask

import (
	"strconv"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/domain"
)

func feat(kind domain.ActionKind, phase domain.Phase) domain.Feature {
	return domain.Feature{
		ID:      "F1",
		Request: "export the data",
		State:   domain.State{Phase: phase},
		Next:    domain.Action{Kind: kind},
		Plan: &domain.Plan{
			Title:    "Add the export command",
			Criteria: []domain.Criterion{{ID: "R1", Text: "The export writes a file", Test: domain.TestTDD}},
			Batches:  []domain.Batch{{N: 1, Title: "Write the exporter", Covers: []string{"R1"}}},
		},
		PlanRev: 1,
	}
}

func labels(q Question) []string {
	var out []string
	for _, o := range q.Options {
		out = append(out, o.Label)
	}
	return out
}

func TestForQuestions(t *testing.T) {
	f := feat(domain.AskQuestions, domain.PhaseQuestions)
	for i := 1; i <= 6; i++ {
		n := strconv.Itoa(i)
		f.Plan.Questions = append(f.Plan.Questions, domain.Question{
			Question: "Which " + n + "?", Header: "H" + n,
			Options: []domain.Option{{Label: "A" + n, Description: "a", Recommended: i == 1}, {Label: "B" + n}},
		})
	}
	a, ok := For(f)
	if !ok || a.Kind != KindQuestions || a.FeatureID != "F1" {
		t.Fatalf("For = %+v, %v", a, ok)
	}
	if len(a.Questions) != 4 || a.Questions[0].Header != "Engineer" {
		t.Fatalf("questions = %+v", a.Questions)
	}
	if !a.Questions[1].Options[0].Recommended || a.Questions[1].Options[1].Recommended {
		t.Errorf("Recommended not copied: %+v", a.Questions[1].Options)
	}
	if !strings.Contains(a.Body, "Add the export command") || len(a.Lines()) < 2 {
		t.Errorf("body = %q", a.Body)
	}
	f.Mode = domain.ModeEngineer
	if a, _ = For(f); len(a.Questions) != 4 || a.Questions[0].Header != "H1" {
		t.Errorf("engineer mode questions = %+v", a.Questions)
	}
}

func TestForApproval(t *testing.T) {
	a, ok := For(feat(domain.AskApproval, domain.PhaseApproval))
	if !ok || a.Kind != KindApproval || len(a.Questions) != 1 || a.Questions[0].Header != "Plan" || a.Questions[0].MultiSelect {
		t.Fatalf("For = %+v, %v", a, ok)
	}
	for _, o := range a.Questions[0].Options {
		if o.Recommended || o.Argv != nil {
			t.Errorf("no recommendation or argv expected: %+v", o)
		}
	}
	a, ok = For(feat(domain.AskChangeNote, domain.PhaseApproval))
	if !ok || a.Kind != KindChangeNote || a.Questions != nil {
		t.Errorf("change note = %+v, %v", a, ok)
	}
}

func TestForClashAndBranch(t *testing.T) {
	f := feat(domain.AskClash, domain.PhaseRepoClash)
	f.Clash = []domain.RepoClash{{Repo: "api", FeatureID: "F0", Phase: "running_batch"}, {Repo: "web", FeatureID: "F2"}}
	a, ok := For(f)
	if !ok || a.Kind != KindRepoClash || a.Questions[0].Header != "Repo ocupado" {
		t.Fatalf("clash = %+v, %v", a, ok)
	}
	if got := strings.Join(a.Context, "|"); got != "repo api: feature F0 (running_batch)|repo web: feature F2" {
		t.Errorf("context = %q", got)
	}
	if !a.Questions[0].Options[0].Recommended {
		t.Errorf("pause must be recommended: %+v", a.Questions[0].Options)
	}

	f = feat(domain.AskBranch, domain.PhaseBranch)
	f.BranchOffer = &domain.BranchOffer{Suggest: []string{"feat/x"}, Error: "boom"}
	a, ok = For(f)
	if !ok || a.Kind != KindBranch || len(a.Context) != 1 || a.Context[0] != "boom" || a.Questions == nil {
		t.Errorf("branch = %+v, %v", a, ok)
	}
}

func TestForDecisions(t *testing.T) {
	cases := []struct {
		phase  domain.Phase
		kind   Kind
		header string
		title  string
	}{
		{domain.PhaseBatchFailed, KindBatchFailed, "Lote", "Lote 0 fallido"},
		{domain.PhaseAffectedFailed, KindAffectedFailed, "Afectados", "Afectados fallidos"},
		{domain.PhaseAffectedUnverified, KindAffectedUnverified, "Afectados", "Afectados sin verificar"},
		{domain.PhaseFixesFailed, KindFixesFailed, "Arreglos", "Arreglos fallidos"},
		{domain.PhasePlannerFailed, KindPlannerFailed, "Planner", ""},
	}
	for _, c := range cases {
		a, ok := For(feat(domain.AskDecision, c.phase))
		if !ok || a.Kind != c.kind || a.Title != c.title || a.Questions[0].Header != c.header {
			t.Errorf("%s: %+v, %v", c.phase, a, ok)
			continue
		}
		for _, o := range a.Questions[0].Options {
			if o.Recommended {
				t.Errorf("%s: no recommendation expected", c.phase)
			}
		}
	}
	if _, ok := For(feat(domain.AskDecision, domain.PhaseDone)); ok {
		t.Error("unknown decision phase must give false")
	}
}

func TestForFindingsRulesDocs(t *testing.T) {
	f := feat(domain.AskRules, domain.PhaseDone)
	for i := 1; i <= 20; i++ {
		f.RuleOffer = append(f.RuleOffer, domain.RuleCandidate{ID: "S" + strconv.Itoa(i), Text: "r", Question: "q"})
	}
	a, ok := For(f)
	if !ok || a.Kind != KindRules || a.Left != 4 || len(a.Questions) != 4 || !a.Questions[0].MultiSelect {
		t.Errorf("rules = %d questions, left %d, %v", len(a.Questions), a.Left, ok)
	}

	a, ok = For(feat(domain.AskDocs, domain.PhaseDone))
	if !ok || a.Kind != KindDocs || a.Questions[0].Header != "Docs" || a.Questions[0].MultiSelect || len(a.Questions[0].Options) != 2 {
		t.Errorf("docs = %+v, %v", a, ok)
	}

	f = feat(domain.AskFindings, domain.PhaseDone)
	f.Review = &domain.Review{}
	a, ok = For(f)
	if !ok || a.Kind != KindFindings || a.Left != 0 || a.Questions == nil {
		t.Errorf("findings = %+v, %v", a, ok)
	}
}

func TestForNotAQuestion(t *testing.T) {
	for _, k := range []domain.ActionKind{domain.DispatchPlanner, domain.DispatchImplementer, domain.AwaitBatches, domain.RunAffected, domain.DispatchReviewer, domain.AskNoTests, domain.Close} {
		if a, ok := For(feat(k, domain.PhaseApproval)); ok {
			t.Errorf("%v gave an ask: %+v", k, a)
		}
	}
}

func TestDesign(t *testing.T) {
	f := feat(domain.DesignDebate, domain.PhaseApproval)
	f.Design = &domain.Design{}
	p := DesignProposal(f)
	if p.Kind != KindDesignProposal || p.Title != "decisión" || p.Questions[0].Header != "Diseño" || len(p.Questions[0].Options) != 2 {
		t.Errorf("proposal = %+v", p)
	}
	c := DesignClose(f)
	if c.Kind != KindDesignClose || c.Title != "resumen del diseño" || len(c.Questions[0].Options) != 2 {
		t.Errorf("close = %+v", c)
	}
	if got := labels(c.Questions[0]); len(got) != 2 {
		t.Errorf("labels = %v", got)
	}
}
