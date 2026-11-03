package memory

import (
	"strings"
	"testing"
)

func TestPackBudgetAndOrder(t *testing.T) {
	rules := []Rule{
		{Meta: Meta{ID: "r1"}, Short: "K1", Text: "area rule"},
		{Meta: Meta{ID: "r2"}, Short: "K2", Text: strings.Repeat("x", 2980)},
		{Meta: Meta{ID: "r3"}, Short: "K3", Text: "small"},
	}
	ls := []Learning{{Meta: Meta{ID: "l1"}, Short: "A1", Text: "learnt"}}
	p := Pack(rules, ls, DispatchBudget)
	if len(p.Text) > DispatchBudget {
		t.Fatalf("over budget: %d", len(p.Text))
	}
	if !strings.HasPrefix(p.Text, "K1: area rule") || !strings.Contains(p.Text, "not shown (budget):") {
		t.Fatalf("text: %q", p.Text)
	}
	if strings.Contains(p.Text, "K2: x") {
		t.Fatal("K2 should not fit")
	}
	small := Pack(rules[:1], ls, DispatchBudget)
	if small.Text != "K1: area rule\nA1: learnt" || len(small.Rules) != 1 || len(small.Learnings) != 1 || len(small.Referenced) != 2 {
		t.Fatalf("small: %+v", small)
	}
	exact := Pack(rules[:1], nil, len("K1: area rule"))
	if exact.Text != "K1: area rule" {
		t.Fatalf("exact: %q", exact.Text)
	}
	full := Pack([]Rule{{Meta: Meta{ID: "f"}, Short: "K1", Text: strings.Repeat("y", DispatchBudget-4)}}, nil, DispatchBudget)
	if len(full.Text) != DispatchBudget || len(full.Rules) != 1 {
		t.Fatalf("exact 3000: %d", len(full.Text))
	}
}

func TestRulesFor(t *testing.T) {
	rs := []Rule{
		{Meta: Meta{ID: "a"}, Text: "all"},
		{Meta: Meta{ID: "b"}, Text: "go", Paths: []string{"cmd/*.go"}},
		{Meta: Meta{ID: "c"}, Text: "md", Paths: []string{"*.md"}},
	}
	got := RulesFor(rs, []string{`cmd\x.go`})
	if len(got) != 2 || got[0].ID != "b" || got[1].ID != "a" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseReport(t *testing.T) {
	r := ParseReport("done\nLearned: tests need X | files: a.go, .\\b.go\nnoise\nFALSE: a7\nlearned:   \nlearned: second")
	if len(r.Learnings) != 2 || r.Learnings[0].Text != "tests need X" || len(r.Learnings[0].Files) != 2 || r.Learnings[0].Files[1] != "b.go" {
		t.Fatalf("%+v", r)
	}
	if len(r.False) != 1 || r.False[0] != "A7" {
		t.Fatalf("%+v", r.False)
	}
	many := ""
	for i := 0; i < 8; i++ {
		many += "learned: " + strings.Repeat("é", 400) + "\n"
	}
	m := ParseReport(many)
	if len(m.Learnings) != 5 || len([]rune(m.Learnings[0].Text)) != 300 {
		t.Fatalf("caps: %d", len(m.Learnings))
	}
	l := LearningIn{Files: []string{"a.go", "z.go"}}
	if w := l.Within([]string{"a.go", "b.go"}); len(w) != 1 || w[0] != "a.go" {
		t.Fatalf("within %v", w)
	}
	if w := (LearningIn{}).Within([]string{"a.go"}); len(w) != 1 {
		t.Fatalf("within empty %v", w)
	}
}

func TestWithinKeepsNamedFilesOutsideAllowed(t *testing.T) {
	l := LearningIn{Files: []string{"./x/z.go", "q.go"}}
	w := l.Within([]string{"a.go", "b.go"})
	if len(w) != 2 || w[0] != "x/z.go" || w[1] != "q.go" {
		t.Fatalf("within %v", w)
	}
}

func TestRuleHint(t *testing.T) {
	yes := []string{"siempre usá tabs", "NUNCA hagas eso", "de ahora en adelante, tests", "Always run vet", "from now on", "no, nunca."}
	no := []string{"nuncamente", "/nxy:feature siempre", "hola", "neverland", "siempreviva"}
	for _, s := range yes {
		if !RuleHint(s) {
			t.Errorf("want hint: %q", s)
		}
	}
	for _, s := range no {
		if RuleHint(s) {
			t.Errorf("no hint: %q", s)
		}
	}
}

func TestSimilar(t *testing.T) {
	if q := SimilarQuery("use the big tabs, ok"); q != `"tabs"` {
		t.Fatalf("query %q", q)
	}
	if Overlap("use tabs always", "Use tabs always.") != 1 || Overlap("a b", "c d") != 0 {
		t.Fatal("overlap")
	}
	if Overlap("use tabs here", "use spaces here") < SimilarMin {
		t.Fatal("min")
	}
}
