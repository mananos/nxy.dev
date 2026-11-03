package review

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
)

func offeredN(n int) []domain.Finding {
	var out []domain.Finding
	for i := 1; i <= n; i++ {
		out = append(out, domain.Finding{
			ID: fmt.Sprintf("H%d", i), Lens: "base", Severity: domain.SeverityHigh,
			File: "a.go", Line: i, Title: fmt.Sprintf("t%d", i), Cause: domain.CauseChange,
		})
	}
	return out
}

func TestGroupsLimits(t *testing.T) {
	gs, left := Groups(offeredN(17))
	if len(gs) != 4 || left != 1 {
		t.Fatalf("groups=%d left=%d, want 4 and 1", len(gs), left)
	}
	for _, g := range gs {
		if len(g.Options) != 4 {
			t.Errorf("group %q has %d options", g.Header, len(g.Options))
		}
	}
	if gs[0].Options[0].Label != "H1 t1" {
		t.Errorf("first label %q", gs[0].Options[0].Label)
	}
}

func TestGroupsSingleGetsNone(t *testing.T) {
	gs, left := Groups(offeredN(5))
	if left != 0 || len(gs) != 2 {
		t.Fatalf("groups=%d left=%d", len(gs), left)
	}
	last := gs[1].Options
	if len(last) != 2 || last[1].Label != NoneLabel {
		t.Errorf("single group options: %+v", last)
	}
	if gs, _ := Groups(nil); len(gs) != 0 {
		t.Errorf("no findings must give no groups")
	}
}

func TestChosenIDs(t *testing.T) {
	got := ChosenIDs("H1 x, H3 y, h1 again, Ninguno")
	if want := []string{"H1", "H3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := ChosenIDs("lo que sea"); len(got) != 0 {
		t.Errorf("free text gave %v", got)
	}
}

func testFeature() domain.Feature {
	return domain.Feature{
		ID: "f1", Request: "haceme un saludo",
		Plan: &domain.Plan{
			Decisions: []domain.Decision{
				{Scope: domain.ScopeRepo, Text: "usar tabs"},
				{Scope: domain.ScopeTask, Text: "en inglés"},
			},
			Criteria: []domain.Criterion{
				{ID: "R1", Text: "saluda", Test: domain.TestTDD},
				{ID: "R2", Text: "README", Test: domain.TestManual},
			},
			Batches: []domain.Batch{{N: 1, Covers: []string{"R1", "R2"}, Test: domain.BatchTest{Name: "TestGreet"}}},
		},
		Review: &domain.Review{
			Risk:     domain.RiskHigh,
			Criteria: []domain.CriterionCheck{{ID: "R1", OK: true}, {ID: "R2", OK: true}},
		},
	}
}

func TestReviewerPromptBaseHasRulesAndBothSaveForms(t *testing.T) {
	f := testFeature()
	p := ReviewerPrompt(f, Lens{Name: BaseLens, Title: "Base", Body: "cuerpo"}, []FileDiff{{Path: "a.go", Status: "M", Text: "@@ -1 +1 @@\n+x\n"}}, `C:\bin\nxy.exe`, "")
	for _, want := range []string{
		"haceme un saludo", "R1: saluda", "R2: README", "usar tabs", "a.go",
		`"C:/bin/nxy.exe" review save --feature f1 --lens base <<'JSON'`,
		`@'`, `'@ | & "C:/bin/nxy.exe" review save --feature f1 --lens base`,
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	if strings.Contains(p, "en inglés") {
		t.Error("task decisions are not repo conventions")
	}
}

func TestReviewerPromptOtherLensHasNoRules(t *testing.T) {
	p := ReviewerPrompt(testFeature(), Lens{Name: "api", Title: "API"}, nil, "nxy", "")
	if strings.Contains(p, "R1") || strings.Contains(p, "saluda") {
		t.Error("only the base pass gets the rules")
	}
	f := testFeature()
	f.Plan.Decisions = nil
	if p := ReviewerPrompt(f, Lens{Name: "api"}, nil, "nxy", ""); !strings.Contains(p, "ninguna registrada") {
		t.Error("no conventions must say so")
	}
}

func TestReviewerPromptListsCutFiles(t *testing.T) {
	big := FileDiff{Path: "big.go", Status: "M", Text: strings.Repeat("+x\n", MaxDiffLines+5)}
	p := ReviewerPrompt(testFeature(), Lens{Name: "api"}, []FileDiff{big, {Path: "s.go", Status: "M", Text: "+y\n"}}, "nxy", "")
	if !strings.Contains(p, "read them yourself") || !strings.Contains(p, "- big.go") {
		t.Error("cut file not listed")
	}
}

func TestRefuterAndFixPrompts(t *testing.T) {
	f := testFeature()
	f.Review.Findings = []domain.Finding{
		{ID: "H1", Severity: domain.SeverityHigh, Cause: domain.CauseChange, File: "a.go", Line: 3, Title: "boom", Scenario: "x -> y", Fix: "arreglar"},
		{ID: "H2", Severity: domain.SeverityMedium, Cause: domain.CauseChange, File: "b.go", Line: 4, Title: "meh", Scenario: "p -> q"},
	}
	f.Review.Chosen = []string{"H1", "H2"}
	r := RefuterPrompt(f, "nxy")
	if !strings.Contains(r, "H1") || strings.Contains(r, "### H2") || !strings.Contains(r, "review refute --feature f1 <<'JSON'") || !strings.Contains(r, "| & \"nxy\" review refute --feature f1") {
		t.Errorf("refuter prompt wrong:\n%s", r)
	}
	fx := FixPrompt(f, "nxy", "")
	for _, want := range []string{"H1", "H2", "a.go:3", "arreglar", `"nxy" affected --feature f1`} {
		if !strings.Contains(fx, want) {
			t.Errorf("fix prompt lacks %q", want)
		}
	}
	if strings.Contains(fx, "What failed") {
		t.Error("first attempt has no failure")
	}
	f.State.Attempt = 1
	f.Affected = &domain.AffectedRun{Failure: "FAIL TestX"}
	if fx := FixPrompt(f, "nxy", ""); !strings.Contains(fx, "FAIL TestX") {
		t.Error("retry must carry the failure")
	}
}

func TestCriterionLineAndSummary(t *testing.T) {
	f := testFeature()
	f.Review.Criteria = []domain.CriterionCheck{{ID: "R1", OK: true}, {ID: "R2", Why: "x"}}
	if got := CriterionLine(*f.Review, f.Plan.Criteria); got != "R1 ✔ · R2 ✘" {
		t.Errorf("CriterionLine = %q", got)
	}
	f.Review.Criteria = nil
	if got := CriterionLine(*f.Review, f.Plan.Criteria); got != "R1 ? · R2 ?" {
		t.Errorf("unanswered CriterionLine = %q", got)
	}
	f = testFeature()
	f.Review.Findings = []domain.Finding{{ID: "H1", Title: "boom"}}
	f.Review.Chosen = []string{"H1"}
	s := Summary(f)
	if !strings.HasPrefix(s, "Listo · R1 ✔ TestGreet · R2 ✔ manual") || !strings.Contains(s, "H1 boom") {
		t.Errorf("Summary = %q", s)
	}
}

func TestSummaryShowsAffectedVerdict(t *testing.T) {
	green := &domain.AffectedRun{Runs: []domain.CommandRun{{Command: "go test", Exit: 0}}}
	f := testFeature()
	f.Affected = green
	if s := Summary(f); strings.Contains(s, "rojo") || strings.Contains(s, "sin verificar") {
		t.Errorf("green Summary = %q", s)
	}
	f.Review.Findings = []domain.Finding{{ID: "H1", Title: "boom"}}
	f.Review.Chosen = []string{"H1"}
	if s := Summary(f); !strings.Contains(s, "Arreglos aplicados: H1 boom") {
		t.Errorf("verified fixes Summary = %q", s)
	}
	f.Affected = &domain.AffectedRun{Failure: "FAIL TestX", Runs: green.Runs}
	s := Summary(f)
	if !strings.Contains(s, "Afectados en rojo") || !strings.Contains(s, "Arreglos pedidos (sin verificar): H1 boom") || strings.Contains(s, "aplicados") {
		t.Errorf("red Summary = %q", s)
	}
	f.Affected = &domain.AffectedRun{Reason: "sin runner"}
	if s := Summary(f); !strings.Contains(s, "Afectados sin verificar") || !strings.Contains(s, "sin verificar): H1") {
		t.Errorf("unverified Summary = %q", s)
	}
}

func TestSummaryNeverEmpty(t *testing.T) {
	f := testFeature()
	f.Review = nil
	s := Summary(f)
	if !strings.Contains(s, "R1 ? · R2 ?") || !strings.Contains(s, "review no corrió") {
		t.Errorf("nil review Summary = %q", s)
	}
	f.Review = &domain.Review{}
	if s := Summary(f); !strings.Contains(s, "R1 ?") || !strings.Contains(s, "review no corrió") {
		t.Errorf("empty review Summary = %q", s)
	}
	f.Plan = nil
	f.Review = nil
	if Summary(f) == "" {
		t.Error("Summary must not be empty without plan")
	}
	f = testFeature()
	f.Review.Warning = "lente rota"
	if s := Summary(f); !strings.Contains(s, "Aviso: lente rota") {
		t.Errorf("warning missing: %q", s)
	}
}

func TestCheckpointAndMarkdown(t *testing.T) {
	f := testFeature()
	f.Review.FullPath = "/tmp/f1.md"
	f.Review.Findings = []domain.Finding{
		{ID: "H1", Lens: "base", Severity: domain.SeverityHigh, Cause: domain.CauseChange, File: "a.go", Line: 3, Title: "boom", Scenario: "x -> y", Fix: "arreglar"},
		{ID: "H2", Lens: "api", Severity: domain.SeverityLow, Cause: domain.CausePreexisting, File: "b.go", Line: 1, Title: "viejo"},
		{ID: "H3", Lens: "api", Severity: domain.SeverityMedium, Cause: domain.CauseChange, Title: "vago", Dropped: "sin path:línea"},
		{ID: "H4", Lens: "api", Severity: domain.SeverityHigh, Cause: domain.CauseChange, File: "c.go", Line: 2, Title: "falso", Scenario: "a -> b", Verdict: domain.VerdictRefuted, VerdictWhy: "no pasa"},
	}
	c := Checkpoint(f)
	for _, want := range []string{"Criterios: R1 ✔ · R2 ✔", "H1", "Escenario: x -> y", "Arreglo: arreglar", "Ya estaban", "H2", "1 descartados", "1 refutados", "Ver completo: /tmp/f1.md"} {
		if !strings.Contains(c, want) {
			t.Errorf("checkpoint lacks %q:\n%s", want, c)
		}
	}
	if strings.Contains(c, "falso") || strings.Contains(c, "vago") {
		t.Error("dropped and refuted findings must not be offered")
	}
	m := Markdown(f)
	for _, want := range []string{"Descartado: sin path:línea", "Refutador: refuted — no pasa", "falso", "vago"} {
		if !strings.Contains(m, want) {
			t.Errorf("markdown lacks %q", want)
		}
	}
}

func TestEngineerCheckpointAndPrompt(t *testing.T) {
	f := domain.Feature{
		ID: "f1", Mode: domain.ModeEngineer,
		Plan: &domain.Plan{Decisions: []domain.Decision{{ID: "D1", Scope: domain.ScopeTask, Text: "usar cola"}}},
		Review: &domain.Review{
			Decisions: []domain.DecisionCheck{{ID: "D1", File: "q.go", Line: 7, How: "la cola se crea acá"}},
			Findings: []domain.Finding{{ID: "H1", Lens: "api", Severity: domain.SeverityHigh, Title: "t",
				Cause: domain.CauseChange, Why: "rompe el request", Lesson: "validar antes de indexar"}},
		},
	}
	out := Checkpoint(f)
	for _, want := range []string{"### Diff por decisión", "D1 usar cola → q.go:7 — la cola se crea acá", "Por qué: rompe el request", "Regla general: validar antes de indexar"} {
		if !strings.Contains(out, want) {
			t.Errorf("checkpoint missing %q:\n%s", want, out)
		}
	}
	f.Mode = ""
	if strings.Contains(Checkpoint(f), "Diff por decisión") {
		t.Error("delegate checkpoint must not show the diff by decision")
	}
	f.Mode = domain.ModeEngineer
	p := ReviewerPrompt(f, Lens{Name: BaseLens, Title: "Base"}, nil, "nxy", "")
	if !strings.Contains(p, "Decisiones del diseño") || !strings.Contains(p, "D1: usar cola") || !strings.Contains(p, "lesson") {
		t.Errorf("base prompt of engineer:\n%s", p)
	}
}

func TestEngineerBaseExampleWithoutDecisions(t *testing.T) {
	f := domain.Feature{ID: "f1", Mode: domain.ModeEngineer, Plan: &domain.Plan{}}
	p := ReviewerPrompt(f, Lens{Name: BaseLens, Title: "Base"}, nil, "nxy", "")
	if strings.Contains(p, `"decisions"`) {
		t.Errorf("example lists decisions although the design has none:\n%s", p)
	}
}

func TestFixQuestion(t *testing.T) {
	q := FixQuestion("f1", 2, 3)
	if !strings.HasPrefix(q, FixQuestionPrefix("f1")) || !strings.Contains(q, "2/3") {
		t.Errorf("FixQuestion = %q", q)
	}
}
