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
	for _, bad := range []string{"Accept", "go test ./internal/greet/", "Leer", "internal/greet/greet.go:1-40", "add greeting"} {
		if strings.Contains(cp, bad) {
			t.Errorf("checkpoint contains %q:\n%s", bad, cp)
		}
	}
	for _, want := range []string{"R1", "Lote 1", "Cubre: R1, R2", "internal/greet/greet.go:12 — ", "TestGreet (nuevo)", "Riesgo: low", "mirar: README usage section"} {
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
