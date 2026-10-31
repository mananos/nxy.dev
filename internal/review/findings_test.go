package review

import (
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
)

func TestPrecision(t *testing.T) {
	ok := domain.Finding{File: "a.go", Line: 3, Scenario: "input vacío → panic"}
	tests := []struct {
		name string
		mod  func(f *domain.Finding)
		want string
	}{
		{"ok", func(f *domain.Finding) {}, ""},
		{"ascii arrow", func(f *domain.Finding) { f.Scenario = "x -> y" }, ""},
		{"sin línea", func(f *domain.Finding) { f.Line = 0 }, "sin path:línea"},
		{"sin archivo", func(f *domain.Finding) { f.File = "" }, "sin path:línea"},
		{"sin escenario", func(f *domain.Finding) { f.Scenario = "podría fallar" }, "sin escenario entrada → resultado"},
		{"flecha sin resultado", func(f *domain.Finding) { f.Scenario = "entrada →" }, "sin escenario entrada → resultado"},
		{"flecha sin entrada", func(f *domain.Finding) { f.Scenario = "-> resultado" }, "sin escenario entrada → resultado"},
	}
	for _, tc := range tests {
		f := ok
		tc.mod(&f)
		if got := Precision(f); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}

func TestHunks(t *testing.T) {
	text := "diff --git a/x b/x\n@@ -1,2 +1,3 @@\n a\n@@ -10 +12 @@\n+b\n@@ -20,2 +30,0 @@\n"
	got := Hunks(text)
	want := [][2]int{{1, 3}, {12, 12}, {30, 30}}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("hunk %d: got %v want %v", i, got[i], want[i])
		}
	}
}

func TestClassify(t *testing.T) {
	hunks := map[string][][2]int{
		"pkg/a.go": {{10, 12}},
		"new.go":   {},
	}
	tests := []struct {
		file string
		line int
		want domain.Cause
	}{
		{"pkg/a.go", 11, domain.CauseChange},
		{"pkg/a.go", 15, domain.CauseChange},
		{"pkg/a.go", 16, domain.CausePreexisting},
		{"pkg/a.go", 6, domain.CausePreexisting},
		{"PKG/A.go", 7, domain.CauseChange},
		{"a.go", 10, domain.CauseChange},
		{"new.go", 999, domain.CauseChange},
		{"other.go", 1, domain.CausePreexisting},
	}
	for _, tc := range tests {
		got := Classify([]domain.Finding{{File: tc.file, Line: tc.line}}, hunks)[0].Cause
		if got != tc.want {
			t.Errorf("%s:%d got %s want %s", tc.file, tc.line, got, tc.want)
		}
	}
}

func TestAssembleNumbersAndRules(t *testing.T) {
	good := "in → out"
	rv := domain.Review{
		Rules: []domain.RuleCheck{{ID: "R1", OK: true}, {ID: "R2", OK: false, Why: "no valida", File: "a.go", Line: 4}},
		Findings: []domain.Finding{
			{Lens: "api", Severity: domain.SeverityLow, File: "a.go", Line: 1, Scenario: good, Cause: domain.CauseChange},
			{Lens: "api", Severity: domain.SeverityHigh, File: "a.go", Line: 1, Scenario: good, Cause: domain.CausePreexisting},
			{Lens: "api", Severity: domain.SeverityMedium, Title: "sin línea", Cause: domain.CauseChange},
			{Lens: "api", Severity: domain.SeverityMedium, File: "a.go", Line: 2, Scenario: good, Cause: domain.CauseChange},
		},
	}
	rules := []domain.Rule{{ID: "R1", Text: "uno"}, {ID: "R2", Text: "dos"}}
	got := Assemble(rv, rules).Findings
	if len(got) != 5 {
		t.Fatalf("got %d findings", len(got))
	}
	if got[0].Rule != "R2" || got[0].Lens != "base" || got[0].Severity != domain.SeverityHigh || got[0].Dropped != "" || got[0].ID != "H1" {
		t.Errorf("first must be the failed rule as high base finding: %+v", got[0])
	}
	if !strings.Contains(got[0].Title, "dos") {
		t.Errorf("title: %q", got[0].Title)
	}
	if got[4].Cause != domain.CausePreexisting || got[4].ID != "H5" {
		t.Errorf("preexisting last: %+v", got[4])
	}
	dropped := 0
	for _, f := range got {
		if f.Dropped != "" {
			dropped++
		}
	}
	if dropped != 1 {
		t.Errorf("dropped = %d", dropped)
	}
	rv = Assemble(rv, rules)
	if n := len(Offered(rv)); n != 3 {
		t.Errorf("offered = %d", n)
	}
}

func TestHighUnproven(t *testing.T) {
	rv := domain.Review{Risk: domain.RiskHigh, Findings: []domain.Finding{
		{ID: "H1", Severity: domain.SeverityHigh, Cause: domain.CauseChange},
	}}
	if !HighUnproven(rv) {
		t.Error("high risk with unproven high finding")
	}
	rv.Findings[0].Verdict = domain.VerdictConfirmed
	if HighUnproven(rv) {
		t.Error("already has a verdict")
	}
	rv.Findings[0].Verdict = ""
	rv.Risk = domain.RiskMedium
	if HighUnproven(rv) {
		t.Error("medium risk skips the refuter")
	}
}

func TestValidate(t *testing.T) {
	rules := []domain.Rule{{ID: "R1"}, {ID: "R2"}}
	good := Submission{Lens: "base", Rules: []domain.RuleCheck{{ID: "R1", OK: true}, {ID: "R2", OK: false, Why: "x"}}}
	if p := Validate(good, "base", rules, true); len(p) != 0 {
		t.Fatalf("good: %v", p)
	}
	tests := []struct {
		name string
		sub  Submission
		base bool
		want string
	}{
		{"lens", Submission{Lens: "api"}, false, "must be"},
		{"rules fuera de base", Submission{Lens: "api", Rules: good.Rules}, false, "only the base"},
		{"falta regla", Submission{Lens: "base", Rules: good.Rules[:1]}, true, "R2 is missing"},
		{"repetida", Submission{Lens: "base", Rules: []domain.RuleCheck{{ID: "R1", OK: true}, {ID: "R1", OK: true}, {ID: "R2", OK: true}}}, true, "more than once"},
		{"ajena", Submission{Lens: "base", Rules: []domain.RuleCheck{{ID: "R1", OK: true}, {ID: "R2", OK: true}, {ID: "R9", OK: true}}}, true, "not a rule"},
		{"x sin why", Submission{Lens: "base", Rules: []domain.RuleCheck{{ID: "R1", OK: true}, {ID: "R2"}}}, true, "needs a why"},
		{"severidad", Submission{Lens: "api", Findings: []domain.Finding{{Severity: "bad", Title: "t"}}}, false, "severity"},
		{"id", Submission{Lens: "api", Findings: []domain.Finding{{ID: "H9", Severity: "high", Title: "t"}}}, false, "findings[0].id"},
		{"rule", Submission{Lens: "api", Findings: []domain.Finding{{Rule: "R1", Severity: "high", Title: "t"}}}, false, "findings[0].rule"},
		{"dropped", Submission{Lens: "api", Findings: []domain.Finding{{Dropped: "x", Severity: "high", Title: "t"}}}, false, "findings[0].dropped"},
		{"verdict", Submission{Lens: "api", Findings: []domain.Finding{{Verdict: "refuted", Severity: "high", Title: "t"}}}, false, "findings[0].verdict"},
		{"verdict_why", Submission{Lens: "api", Findings: []domain.Finding{{VerdictWhy: "x", Severity: "high", Title: "t"}}}, false, "findings[0].verdict_why"},
	}
	for _, tc := range tests {
		lens := tc.sub.Lens
		if tc.name == "lens" {
			lens = "base"
		}
		p := Validate(tc.sub, lens, rules, tc.base)
		found := false
		for _, x := range p {
			if strings.Contains(x.String(), tc.want) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: want %q in %v", tc.name, tc.want, p)
		}
	}
}

func TestDecodeStrict(t *testing.T) {
	if _, err := Decode(strings.NewReader(`{"lens":"api","findings":[],"extra":1}`)); err == nil {
		t.Error("unknown field must fail")
	}
	if _, err := Decode(strings.NewReader(`{"lens":"api","findings":[]} x`)); err == nil {
		t.Error("trailing content must fail")
	}
	if _, err := Decode(strings.NewReader(`{"lens":"api","findings":[]}`)); err != nil {
		t.Error(err)
	}
}

func TestVerdicts(t *testing.T) {
	if _, err := DecodeVerdicts(strings.NewReader(`{"verdicts":[{"id":"H1","verdict":"refuted"},{"id":"H1","verdict":"confirmed"}]}`)); err == nil {
		t.Error("duplicate id must fail")
	}
	if _, err := DecodeVerdicts(strings.NewReader(`{"verdicts":[{"id":"H1","verdict":"maybe"}]}`)); err == nil {
		t.Error("invalid verdict must fail")
	}
	rv := domain.Review{Findings: []domain.Finding{
		{ID: "H1", Severity: domain.SeverityHigh, Cause: domain.CauseChange},
		{ID: "H2", Severity: domain.SeverityHigh, Cause: domain.CauseChange},
		{ID: "H3", Severity: domain.SeverityLow, Cause: domain.CauseChange},
	}}
	vs, err := DecodeVerdicts(strings.NewReader(`{"verdicts":[{"id":"H1","verdict":"refuted","why":"guard"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	out, err := ApplyVerdicts(rv, vs)
	if err != nil {
		t.Fatal(err)
	}
	if out.Findings[0].Verdict != domain.VerdictRefuted || out.Findings[1].Verdict != domain.VerdictInconclusive || out.Findings[2].Verdict != "" || !out.Refuted {
		t.Errorf("%+v", out.Findings)
	}
	if rv.Findings[0].Verdict != "" {
		t.Error("input mutated")
	}
	if n := len(Offered(out)); n != 2 {
		t.Errorf("offered = %d", n)
	}
	if _, err := ApplyVerdicts(rv, Verdicts{Verdicts: []VerdictEntry{{ID: "H3", Verdict: domain.VerdictRefuted}}}); err == nil {
		t.Error("verdict for a low finding must fail")
	}
	m := MissingInconclusive(rv)
	if m.Findings[0].Verdict != domain.VerdictInconclusive || m.Findings[1].Verdict != domain.VerdictInconclusive || !m.Refuted {
		t.Errorf("%+v", m.Findings)
	}
}

func TestCut(t *testing.T) {
	mk := func(p string, n int) FileDiff { return FileDiff{Path: p, Text: strings.Repeat("x\n", n)} }
	kept, cut := Cut([]FileDiff{mk("a_test.go", 5), mk("a.go", 5), mk("big.go", 50)}, 12)
	if len(kept) != 2 || kept[0].Path != "a.go" || kept[1].Path != "a_test.go" {
		t.Errorf("kept %v", kept)
	}
	if len(cut) != 1 || cut[0] != "big.go" {
		t.Errorf("cut %v", cut)
	}
}
