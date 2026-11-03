package plan

import (
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
)

func offerOf(n int) []domain.RuleCandidate {
	var out []domain.RuleCandidate
	for i := 1; i <= n; i++ {
		out = append(out, domain.RuleCandidate{ID: "S" + string(rune('0'+i%10)), Text: "rule", Question: "q"})
	}
	return out
}

func TestRuleGroupsSplitFourByFour(t *testing.T) {
	g := RuleGroups(offerOf(9))
	if len(g) != 3 || len(g[0].Options) != 4 || len(g[2].Options) != 2 {
		t.Fatalf("groups = %+v", g)
	}
	if g[2].Options[1].Label != RuleNoneLabel {
		t.Errorf("a group of one needs %q: %+v", RuleNoneLabel, g[2])
	}
	if g[0].Header != "Reglas 1/3" {
		t.Errorf("header = %q", g[0].Header)
	}
	if got := RuleGroups(offerOf(20)); len(got) != 4 {
		t.Errorf("more than 16 must still fit in 4 groups, got %d", len(got))
	}
}

func TestRuleGroupsDescribeWhatTheyReplace(t *testing.T) {
	g := RuleGroups([]domain.RuleCandidate{{ID: "S1", Text: "use slog", Question: "which logger", Supersedes: "K3", SupersedesText: "use log"}, {ID: "S2", Text: "b", Question: "c"}})
	d := g[0].Options[0].Description
	if !strings.Contains(d, "reemplaza a K3") || !strings.Contains(d, "which logger") {
		t.Errorf("description = %q", d)
	}
}

func TestChosenRuleIDs(t *testing.T) {
	offer := []domain.RuleCandidate{{ID: "S1", Text: "other"}, {ID: "S2", Text: "use slog"}, {ID: "S3", Text: "see S1 for context, ok"}}
	got := ChosenRuleIDs("S2 use slog, s1 other, S2 again, MS3 no, S10", offer)
	if strings.Join(got, ",") != "S1,S2" {
		t.Errorf("ids = %v", got)
	}
	if len(ChosenRuleIDs("Ninguna", offer)) != 0 || !IsNoneRules("ninguna") {
		t.Error("Ninguna must pick nothing")
	}
}

// An S<n> inside the text of a rule is not a pick.
func TestChosenRuleIDsIgnoresIDsInsideRuleText(t *testing.T) {
	offer := []domain.RuleCandidate{{ID: "S1", Text: "a"}, {ID: "S2", Text: "never mix S1 with S3 here"}, {ID: "S3", Text: "c"}}
	got := ChosenRuleIDs("S2 never mix S1 with S3 here", offer)
	if strings.Join(got, ",") != "S2" {
		t.Errorf("ids = %v", got)
	}
}

func TestRulesQuestionTextStartsWithItsPrefix(t *testing.T) {
	if q := RulesQuestion("abc", 1, 2); !strings.HasPrefix(q, RulesQuestionPrefix("abc")) || !strings.HasSuffix(q, "(1/2)") {
		t.Errorf("question = %q", q)
	}
}

func TestRepoDecisionNeedsItsQuestion(t *testing.T) {
	p := valid()
	p.Decisions = []domain.Decision{{Scope: domain.ScopeRepo, Text: "use slog"}}
	found := false
	for _, pr := range Validate(p) {
		found = found || strings.Contains(pr.Where, "question")
	}
	if !found {
		t.Error("a repo decision without question must be a problem")
	}
	p.Decisions[0].Question = "which logger?"
	for _, pr := range Validate(p) {
		if strings.Contains(pr.Where, "question") {
			t.Errorf("unexpected problem %v", pr)
		}
	}
	p.Decisions = []domain.Decision{{Scope: domain.ScopeTask, Text: "x"}}
	for _, pr := range Validate(p) {
		if strings.Contains(pr.Where, "question") {
			t.Errorf("a task decision needs no question: %v", pr)
		}
	}
}
