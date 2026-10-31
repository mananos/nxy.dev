package plan

import (
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
)

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
	for _, bad := range []string{"Leer","internal/greet/greet.go:1-40", "add greeting"} {
		if strings.Contains(cp, bad) {
			t.Errorf("checkpoint contains %q:\n%s", bad, cp)
		}
	}
	for _, want := range []string{"R1", "Lote 1", "Cubre: R1, R2", "internal/greet/greet.go:12 — ", "TestGreet (nuevo)", "Accept: `go test ./internal/greet/`", "Riesgo: low", "mirar: README usage section"} {
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

func TestApprovalQuestion(t *testing.T) {
	if got := ApprovalQuestion("abc", 2); got != "Approve nxy plan abc r2?" {
		t.Fatalf("got %q", got)
	}
}

func TestImplementerPrompt(t *testing.T) {
	f := sampleFeature(t)
	f.Next = domain.Action{Kind: domain.DispatchImplementer, Batch: 1}
	got := ImplementerPrompt(f, `C:\tools\nxy.exe`)
	for _, want := range []string{
		f.Request,
		"batch 1 of 1",
		"Accept: `go test ./internal/greet/`",
		"nxy runs it, do not run it yourself",
		`"C:/tools/nxy.exe" batch red --feature f1`,
		`"C:/tools/nxy.exe" batch green --feature f1`,
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
	if got := ImplementerPrompt(f, "nxy"); !strings.Contains(got, "Previous attempt failed") || !strings.Contains(got, "boom") {
		t.Error("retry prompt lacks the failure")
	}
	if got := ImplementerPrompt(f, "nxy"); !strings.Contains(got, `"nxy" batch red`) {
		t.Error("retry without a recorded red lost the red step")
	}
	f.Proof.Red = &domain.Run{Exit: 1}
	if got := ImplementerPrompt(f, "nxy"); strings.Contains(got, "batch red") || !strings.Contains(got, "batch green") {
		t.Errorf("retry with a recorded red must skip the red step:\n%s", got)
	}
	f.Proof.Red = nil
	f.Proof.Batch = 2
	if strings.Contains(ImplementerPrompt(f, "nxy"), "Previous attempt failed") {
		t.Error("failure of another batch shown")
	}

	f.Plan.Rules[0].Test = domain.TestExisting
	if got := ImplementerPrompt(f, "nxy"); strings.Contains(got, "batch red") || !strings.Contains(got, "batch green") {
		t.Errorf("non-TDD prompt has the wrong steps:\n%s", got)
	}
	f.Plan.Batches[0].Test.Accept = ""
	if got := ImplementerPrompt(f, "nxy"); strings.Contains(got, `"nxy" batch`) {
		t.Errorf("manual prompt has a batch command:\n%s", got)
	}
}

func TestDecisionQuestions(t *testing.T) {
	if got := DecisionQuestion("abc", 2); got != "nxy feature abc: batch 2 failed twice — what now?" {
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
	got := PlannerPrompt(f, `C:\tools\nxy.exe`)
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
	if strings.Contains(PlannerPrompt(f, "nxy"), "Previous plan") {
		t.Error("first prompt mentions a previous plan")
	}
}
