package plan

import (
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
)

func TestSuiteRenderedOnlyWhenSet(t *testing.T) {
	f := sampleFeature(t)
	want := "- Suite (sólo si no se pueden calcular los afectados): `go test ./...`"
	if !strings.Contains(Checkpoint(*f.Plan), want) || !strings.Contains(Markdown(f), want) {
		t.Fatal("suite line missing")
	}
	f.Plan.Suite = ""
	if strings.Contains(Checkpoint(*f.Plan), "Suite") || strings.Contains(Markdown(f), "Suite (") {
		t.Fatal("suite line rendered without a suite")
	}
}

func TestSuiteValidation(t *testing.T) {
	p := valid()
	p.Suite = "npm test"
	if ps := Validate(p); len(ps) != 0 {
		t.Fatalf("problems: %v", ps)
	}
	for _, bad := range []string{"a\nb", "x ```y"} {
		p.Suite = bad
		if ps := Validate(p); len(ps) == 0 {
			t.Errorf("suite %q accepted", bad)
		}
	}
}

func TestAffectedFixPrompt(t *testing.T) {
	f := sampleFeature(t)
	f.Affected = &domain.AffectedRun{Failure: "`go test ./x/` (exit 1):\nFAIL TestX"}
	got := AffectedFixPrompt(f, `C:\bin\nxy.exe`, "")
	for _, want := range []string{"feature f1", "add greeting", "Greeting stays English", "FAIL TestX", "C:/bin/nxy.exe\" affected --feature f1", "only reports and records nothing"} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt lacks %q:\n%s", want, got)
		}
	}
}

func TestAffectedQuestions(t *testing.T) {
	if !strings.Contains(AffectedFailedQuestion("f1"), "fallaron dos veces") || !strings.Contains(UnverifiedQuestion("f1"), "no se pudieron verificar") {
		t.Fatal("questions changed")
	}
}
