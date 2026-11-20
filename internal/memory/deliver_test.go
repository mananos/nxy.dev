package memory

import (
	"strings"
	"testing"
)

func TestPackReposNamesLinesOnlyWithSeveralGroups(t *testing.T) {
	a := []Rule{{Meta: Meta{ID: "01A"}, Short: "K1", Text: "use tabs"}}
	b := []Rule{{Meta: Meta{ID: "01B"}, Short: "K1", Text: "use spaces"}}
	pkt := PackRepos([]RepoRules{{Name: "api", Rules: a}, {Name: "web", Rules: b}}, DispatchBudget)
	if !strings.Contains(pkt.Text, "[api] K1: use tabs") || !strings.Contains(pkt.Text, "[web] K1: use spaces") {
		t.Fatalf("text = %q", pkt.Text)
	}
	one := PackRepos([]RepoRules{{Name: "api", Rules: a}}, DispatchBudget)
	if one.Text != Pack(a, nil, DispatchBudget).Text || strings.Contains(one.Text, "[") {
		t.Fatalf("one group must equal Pack, got %q", one.Text)
	}
}

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

func TestGlobalFor(t *testing.T) {
	rules := []Rule{
		{Meta: Meta{ID: "g1"}, Short: "G1"},
		{Meta: Meta{ID: "g2"}, Short: "G2", Roles: []string{RoleReviewer}},
		{Meta: Meta{ID: "g3"}, Short: "G3", Paths: []string{"*.go"}},
		{Meta: Meta{ID: "g4"}, Short: "G4", Roles: []string{RolePlanner, RoleMain}, Paths: []string{"*.go"}},
	}
	shorts := func(rs []Rule) string {
		var s []string
		for _, r := range rs {
			s = append(s, r.Short)
		}
		return strings.Join(s, ",")
	}
	if got := shorts(GlobalFor(rules, RoleReviewer, []string{"a.md"}, false)); got != "G1,G2" {
		t.Errorf("reviewer md = %s", got)
	}
	if got := shorts(GlobalFor(rules, RoleImplementer, []string{"a.go"}, false)); got != "G3,G1" {
		t.Errorf("implementer go = %s", got)
	}
	if got := shorts(GlobalFor(rules, RolePlanner, nil, true)); got != "G1,G3,G4" {
		t.Errorf("planner all = %s", got)
	}
}

func TestPackWithGlobals(t *testing.T) {
	ks := []Rule{{Meta: Meta{ID: "r1"}, Short: "K1", Text: "k rule"}}
	gs := []Rule{{Meta: Meta{ID: "g1"}, Short: "G1", Text: "g rule"}}
	ls := []Learning{{Meta: Meta{ID: "l1"}, Short: "A1", Text: "learnt"}}
	p := PackWith([]RepoRules{{Name: "a", Rules: ks, Learnings: ls}, {Name: "b"}}, gs, DispatchBudget)
	if want := "[a] K1: k rule\nG1: g rule\n[a] A1: learnt\n" + GlobalPrecedence; p.Text != want {
		t.Fatalf("text: %q", p.Text)
	}
	if len(p.Rules) != 2 || p.Origins["G1"][0] != GlobalRepo {
		t.Fatalf("packet: %+v", p)
	}
	if no := PackWith([]RepoRules{{Rules: ks}}, nil, DispatchBudget); no.Text != "K1: k rule" {
		t.Fatalf("no globals: %q", no.Text)
	}
	// a budget that cuts a K drops every G first
	big := []Rule{
		{Meta: Meta{ID: "r1"}, Short: "K1", Text: strings.Repeat("a", 1400)},
		{Meta: Meta{ID: "r2"}, Short: "K2", Text: strings.Repeat("b", 1400)},
		{Meta: Meta{ID: "r3"}, Short: "K3", Text: strings.Repeat("c", 1400)},
	}
	tiny := []Rule{{Meta: Meta{ID: "g1"}, Short: "G1", Text: "tiny"}}
	cut := PackWith([]RepoRules{{Rules: big}}, tiny, DispatchBudget)
	if len(cut.Text) > DispatchBudget || strings.Contains(cut.Text, "G1: tiny") || strings.Contains(cut.Text, GlobalPrecedence) {
		t.Fatalf("G survived a K cut: %q", cut.Text)
	}
	if !strings.Contains(cut.Text, "K1: a") || !strings.Contains(cut.Text, "K2: b") || !strings.Contains(cut.Text, "not shown (budget): K3, G1") {
		t.Fatalf("cut text tail: %q", cut.Text[len(cut.Text)-60:])
	}
	// the precedence line counts toward the budget
	for b := len("K1: k rule"); b < len("K1: k rule\nG1: g rule\n"+GlobalPrecedence)+2; b++ {
		if g := PackWith([]RepoRules{{Rules: ks}}, gs, b); len(g.Text) > b {
			t.Fatalf("budget %d: %d chars", b, len(g.Text))
		}
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
