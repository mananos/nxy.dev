package configedit

import (
	"strings"
	"testing"
)

func TestProblemMessageIsQuotedAndShort(t *testing.T) {
	m := Problem{Key: "roles.planner.effort", Value: "\x1b[2J" + strings.Repeat("x", 500)}.Message()
	if strings.ContainsRune(m, 0x1b) || len(m) > 250 || !strings.Contains(m, `"\x1b[2J`) {
		t.Fatalf("message = %q", m)
	}
}

func TestInvalidRoleValues(t *testing.T) {
	cfg := map[string]any{"roles": map[string]any{
		"planner":  map[string]any{"model": "opus", "effort": "xhigh"},
		"scout":    map[string]any{"model": "gpt", "effort": "max"},
		"refuter":  map[string]any{"effort": 3.0},
		"reviewer": map[string]any{"effort": "ultra"},
	}}
	got := Invalid(cfg)
	if len(got) != 3 {
		t.Fatalf("Invalid = %+v", got)
	}
	keys := map[string]bool{}
	for _, p := range got {
		keys[p.Key] = true
		if !strings.Contains(p.Message(), p.Key) {
			t.Fatalf("message = %q", p.Message())
		}
	}
	for _, k := range []string{"roles.scout.model", "roles.refuter.effort", "roles.reviewer.effort"} {
		if !keys[k] {
			t.Fatalf("missing %s in %+v", k, got)
		}
	}
	if !strings.Contains(InvalidOf(cfg, "reviewer"), "low, medium, high, xhigh, max") {
		t.Fatalf("warn = %q", InvalidOf(cfg, "reviewer"))
	}
	if InvalidOf(cfg, "planner") != "" {
		t.Fatal("planner is valid")
	}
	if len(Invalid(nil)) != 0 {
		t.Fatal("nil config has no problems")
	}
}
