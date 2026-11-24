package plan

import (
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/domain"
)

func TestImplementerPromptNamesRepoCheckout(t *testing.T) {
	f := domain.Feature{
		ID: "f1", Request: "r",
		Plan: &domain.Plan{
			Repos:   []domain.PlanRepo{{Path: "api", Name: "api"}, {Path: "web", Name: "web"}},
			Batches: []domain.Batch{{N: 1, Title: "t", Repo: "web", Change: []domain.Change{{At: "web/x.go", What: "w"}}}},
		},
		Repos: []domain.FeatureRepo{{Name: "api", Checkout: "C:/wt/api"}, {Name: "web", Checkout: `C:\wt\web`}},
		Next:  domain.Action{Batch: 1},
	}
	got := ImplementerPrompt(f, "nxy", "")
	for _, want := range []string{"works in repo web, checkout `C:/wt/web`", "- api — checkout `C:/wt/api`", "`<name>/x` is `<checkout>/x`"} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt lacks %q:\n%s", want, got)
		}
	}
	f.Plan.Repos = nil
	if strings.Contains(ImplementerPrompt(f, "nxy", ""), "checkout") {
		t.Error("one-repo prompt mentions a checkout")
	}
}

func TestEngineerExampleValid(t *testing.T) {
	p, err := Decode(strings.NewReader(engineerExample))
	if err != nil {
		t.Fatal(err)
	}
	if probs := Validate(p); len(probs) != 0 {
		t.Errorf("problems = %v", probs)
	}
}

func TestValidateRejectsDesignOwnedFields(t *testing.T) {
	p, _ := Decode(strings.NewReader(engineerExample))
	p.Decisions = []domain.Decision{{ID: "D1", Scope: domain.ScopeTask, Text: "x"}}
	if probs := Validate(p); len(probs) == 0 {
		t.Error("decision id accepted")
	}
}

func TestPlannerPromptEngineerUsesDesignTemplate(t *testing.T) {
	f := domain.Feature{ID: "f1", Request: "r", Mode: domain.ModeEngineer, Design: &domain.Design{
		Decisions: []domain.Decision{{ID: "D1", Scope: domain.ScopeTask, Text: "x", Why: "y"}},
		Criteria:  []domain.Criterion{{ID: "R1", Text: "rule"}},
	}}
	p := PlannerPrompt(f, "nxy", "")
	if !strings.Contains(p, "D1 [task] x -- why: y") || !strings.Contains(p, "plan gaps") {
		t.Errorf("prompt = %s", p)
	}
}

func TestPlannerPromptEngineerPrevPlanUsesDesignRules(t *testing.T) {
	f := sampleFeature(t)
	f.Mode = domain.ModeEngineer
	f.Plan.Questions = []domain.Question{{Question: "ZZQUESTIONZZ", Header: "h"}}
	f.Plan.Criteria = []domain.Criterion{{ID: "R1", Text: "ZZDELEGATERULEZZ"}}
	f.Design = &domain.Design{Criteria: []domain.Criterion{{ID: "R1", Text: "ZZDESIGNRULEZZ"}}}
	p := PlannerPrompt(f, "nxy", "")
	if strings.Contains(p, "ZZQUESTIONZZ") || strings.Contains(p, "ZZDELEGATERULEZZ") {
		t.Errorf("prev plan leaks delegate rules/questions:\n%s", p)
	}
	if !strings.Contains(p, "ZZDESIGNRULEZZ") {
		t.Errorf("prompt lacks design rules:\n%s", p)
	}
}

func sampleFeature(t *testing.T) domain.Feature {
	t.Helper()
	p, err := Decode(strings.NewReader(plannerExample))
	if err != nil {
		t.Fatal(err)
	}
	return domain.Feature{ID: "f1", Request: "add greeting\n  with  spaces\n", Plan: &p, PlanRev: 1}
}

func TestExampleIsValid(t *testing.T) {
	f := sampleFeature(t)
	if ps := Validate(*f.Plan); len(ps) != 0 {
		t.Fatalf("example has problems: %v", ps)
	}
}

func TestCheckpointOmitsMachineFields(t *testing.T) {
	f := sampleFeature(t)
	cp := Checkpoint(*f.Plan)
	for _, bad := range []string{"Leer", "internal/greet/greet.go:1-40", "add greeting"} {
		if strings.Contains(cp, bad) {
			t.Errorf("checkpoint contains %q:\n%s", bad, cp)
		}
	}
	for _, want := range []string{"R1", "Lote 1", "Cubre: R1, R2", "internal/greet/greet.go:12 — ", "TestGreet (nuevo)", "Accept: `go test ./internal/greet/`", "Riesgo: bajo", "mirar: README usage section"} {
		if !strings.Contains(cp, want) {
			t.Errorf("checkpoint lacks %q:\n%s", want, cp)
		}
	}
}

func TestMarkdownHasEverything(t *testing.T) {
	f := sampleFeature(t)
	md := Markdown(f)
	for _, want := range []string{"add greeting", "Accept: `go test ./internal/greet/`", "Leer: internal/greet/greet.go:1-40", "[task] Greeting stays English"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	if md != Markdown(f) {
		t.Error("markdown is not deterministic")
	}
}

func engineerSample(t *testing.T) domain.Feature {
	f := sampleFeature(t)
	f.Mode = domain.ModeEngineer
	f.Plan.Decisions = []domain.Decision{
		{ID: "D1", Scope: domain.ScopeTask, Text: "usar cola", Why: "evita carrera", Dissent: "sigo creyendo que un lock alcanza"},
		{ID: "D2", Scope: domain.ScopeTask, Text: "nombre del campo", By: domain.ByPlanner, Fork: "B2"},
	}
	f.Design = &domain.Design{
		D2:     "a -> b",
		Agenda: []domain.Fork{{ID: "B2", Title: "nombre del campo", Status: domain.ForkDelegated}},
	}
	return f
}

func TestCheckpointOfEngineerOmitsRules(t *testing.T) {
	f := engineerSample(t)
	cp := CheckpointOf(f)
	if strings.Contains(cp, "### Criterios") {
		t.Errorf("engineer checkpoint has rules:\n%s", cp)
	}
	for _, want := range []string{"Decisiones que eligió el planner", "D2 nombre del campo", "Lote 1"} {
		if !strings.Contains(cp, want) {
			t.Errorf("checkpoint lacks %q:\n%s", want, cp)
		}
	}
	d := sampleFeature(t)
	if CheckpointOf(d) != Checkpoint(*d.Plan) {
		t.Error("delegate checkpoint changed")
	}
}

func TestMarkdownEngineerHasDesign(t *testing.T) {
	md := Markdown(engineerSample(t))
	for _, want := range []string{"### Diseño", "D1 usar cola — evita carrera", "Claude mantiene su desacuerdo: sigo creyendo", "(la eligió el planner)", "```d2\na -> b\n```"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "B2 nombre del campo") {
		t.Errorf("fork resolved by the planner listed twice:\n%s", md)
	}
}

func TestRiskLineShowsWhyNxyRaisedIt(t *testing.T) {
	f := sampleFeature(t)
	f.Plan.Batches[0].Risk = domain.RiskHigh
	f.Plan.Batches[0].RiskWhy = "lente security: auth/x.go"
	want := "Riesgo: alto (subido por nxy: lente security: auth/x.go)"
	if cp := Checkpoint(*f.Plan); !strings.Contains(cp, want) {
		t.Errorf("checkpoint lacks %q:\n%s", want, cp)
	}
	if md := Markdown(f); !strings.Contains(md, want) {
		t.Errorf("markdown lacks %q", want)
	}
}

func TestApprovalQuestion(t *testing.T) {
	if got := ApprovalQuestion("abc", 2); got != "¿Aprobás el plan abc r2 de nxy?" {
		t.Fatalf("got %q", got)
	}
}

func TestPromptsPrintMemoryOnlyWhenGiven(t *testing.T) {
	f := sampleFeature(t)
	f.Next = domain.Action{Kind: domain.DispatchImplementer, Batch: 1}
	mem := "K1: always wrap errors"
	if got := ImplementerPrompt(f, "nxy", mem); !strings.Contains(got, "## Repo rules and learnings (nxy memory)") || !strings.Contains(got, mem) || !strings.Contains(got, "learned:") {
		t.Errorf("implementer prompt lacks the memory:\n%s", got)
	}
	if got := ImplementerPrompt(f, "nxy", ""); strings.Contains(got, "nxy memory") || strings.Contains(got, "learned:") {
		t.Error("implementer prompt prints an empty memory section")
	}
	if got := PlannerPrompt(f, "nxy", mem); !strings.Contains(got, "## Repo rules (nxy memory)") || !strings.Contains(got, `"nxy" mem related`) {
		t.Errorf("planner prompt lacks the memory:\n%s", got)
	}
	if got := PlannerPrompt(f, "nxy", ""); strings.Contains(got, "nxy memory") {
		t.Error("planner prompt prints an empty memory section")
	}
	if got := AffectedFixPrompt(f, "nxy", mem); !strings.Contains(got, mem) {
		t.Error("affected fix prompt lacks the memory")
	}
}

func TestImplementerPrompt(t *testing.T) {
	f := sampleFeature(t)
	f.Next = domain.Action{Kind: domain.DispatchImplementer, Batch: 1}
	got := ImplementerPrompt(f, `C:\tools\nxy.exe`, "")
	for _, want := range []string{
		f.Request,
		"batch 1 of 1",
		"Accept: `go test ./internal/greet/`",
		"nxy runs it, do not run it yourself",
		`"C:/tools/nxy.exe" batch red --feature f1`,
		`"C:/tools/nxy.exe" batch green --feature f1 --batch 1`,
		"timeout 600000",
		"R1: greet(name)",
		"internal/greet/greet.go:12",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	if strings.Contains(got, "Previous attempt failed") {
		t.Error("first prompt has a failure section")
	}

	f.Proof = &domain.Proof{Batch: 1, Failure: "green failed (exit 2): boom"}
	if got := ImplementerPrompt(f, "nxy", ""); !strings.Contains(got, "Previous attempt failed") || !strings.Contains(got, "boom") {
		t.Error("retry prompt lacks the failure")
	}
	if got := ImplementerPrompt(f, "nxy", ""); !strings.Contains(got, `"nxy" batch red`) {
		t.Error("retry without a recorded red lost the red step")
	}
	f.Proof.Red = &domain.Run{Exit: 1}
	if got := ImplementerPrompt(f, "nxy", ""); strings.Contains(got, "batch red") || !strings.Contains(got, "batch green") {
		t.Errorf("retry with a recorded red must skip the red step:\n%s", got)
	}
	f.Proof.Red = nil
	f.Proof.Batch = 2
	if strings.Contains(ImplementerPrompt(f, "nxy", ""), "Previous attempt failed") {
		t.Error("failure of another batch shown")
	}

	f.Plan.Criteria[0].Test = domain.TestExisting
	if got := ImplementerPrompt(f, "nxy", ""); strings.Contains(got, "batch red") || !strings.Contains(got, "batch green") {
		t.Errorf("non-TDD prompt has the wrong steps:\n%s", got)
	}
	f.Plan.Batches[0].Test.Accept = ""
	if got := ImplementerPrompt(f, "nxy", ""); strings.Contains(got, `"nxy" batch`) {
		t.Errorf("manual prompt has a batch command:\n%s", got)
	}
}

func TestImplementerPromptSharedCriterion(t *testing.T) {
	f := sampleFeature(t)
	b1 := f.Plan.Batches[0]
	b1.Covers = []string{"R1", "R2"}
	b2 := b1
	b2.N = 2
	b2.Covers = []string{"R2"}
	b1.N = 1
	f.Plan.Batches = []domain.Batch{b1, b2}
	f.Plan.Criteria = []domain.Criterion{
		{ID: "R1", Text: "solo uno", Test: domain.TestExisting},
		{ID: "R2", Text: "compartido", Test: domain.TestManual},
	}
	const line = "se cumple entre todos los lotes"
	got1 := ImplementerPromptFor(f, 1, "nxy", "")
	if !strings.Contains(got1, "also covered by batches 2") || !strings.Contains(got1, line) {
		t.Errorf("prompt 1 lacks the shared marker:\n%s", got1)
	}
	got2 := ImplementerPromptFor(f, 2, "nxy", "")
	if !strings.Contains(got2, "also covered by batches 1") || !strings.Contains(got2, line) {
		t.Errorf("prompt 2 lacks the shared marker:\n%s", got2)
	}
	if strings.Contains(got1, "R1: solo uno (test: existing; also") {
		t.Error("unshared criterion is marked as shared")
	}
	b2.Covers = []string{"R1"}
	f.Plan.Batches = []domain.Batch{b1, b2}
	b1.Covers = []string{"R2"}
	f.Plan.Batches[0] = b1
	if got := ImplementerPromptFor(f, 1, "nxy", ""); strings.Contains(got, line) {
		t.Error("no shared criterion but the line is present")
	}
}

func TestImplementerPromptForbidsStubRed(t *testing.T) {
	f := sampleFeature(t)
	f.Next = domain.Action{Kind: domain.DispatchImplementer, Batch: 1}
	const ban = "Never stub, comment out or revert code to make the test fail"
	const warn = "say so in your reply"
	got := ImplementerPrompt(f, "nxy", "")
	if !strings.Contains(got, ban) || !strings.Contains(got, warn) {
		t.Errorf("TDD prompt lacks the stub ban or the warning:\n%s", got)
	}
	f.Plan.Criteria[0].Test = domain.TestExisting
	if got := ImplementerPrompt(f, "nxy", ""); strings.Contains(got, ban) {
		t.Error("green-only prompt carries the stub ban")
	}
}

func TestDecisionQuestions(t *testing.T) {
	if got := DecisionQuestion("abc", 2); got != "nxy feature abc: el lote 2 falló dos veces — ¿qué hacemos?" {
		t.Fatalf("got %q", got)
	}
	if PlannerFailedQuestion("abc") == "" {
		t.Fatal("empty")
	}
}

func TestPlannerPrompt(t *testing.T) {
	f := sampleFeature(t)
	f.Answers = []domain.Answer{{Question: "Where?", Answer: "internal/greet"}}
	f.ChangeNotes = []string{"split batch 1"}
	got := PlannerPrompt(f, `C:\tools\nxy.exe`, "")
	for _, want := range []string{
		f.Request,
		`"C:/tools/nxy.exe" plan save --feature f1 <<'JSON'`,
		"Where? -> internal/greet",
		"split batch 1",
		"Previous plan",
		"$OutputEncoding = [System.Text.UTF8Encoding]::new($false)",
		`| & "C:/tools/nxy.exe" plan save --feature f1`,
		"your final reply is exactly the line `plan save` printed (plan rN saved...) and nothing else",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	f.Plan, f.Answers, f.ChangeNotes = nil, nil, nil
	if strings.Contains(PlannerPrompt(f, "nxy", ""), "Previous plan") {
		t.Error("first prompt mentions a previous plan")
	}
}
