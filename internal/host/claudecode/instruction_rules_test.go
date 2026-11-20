package claudecode

import (
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/domain"
	"github.com/mananos/nxy.dev/v2/internal/plan"
)

func rulesFeature(n int) domain.Feature {
	f := domain.Feature{
		ID:    "f1",
		State: domain.State{Phase: domain.PhaseRules},
		Next:  domain.Action{Kind: domain.AskRules},
	}
	for i := 1; i <= n; i++ {
		c := domain.RuleCandidate{ID: "S" + string(rune('0'+i)), Text: "rule " + string(rune('a'+i)), Question: "what " + string(rune('a'+i))}
		if i == 2 {
			c.Supersedes, c.SupersedesText = "K3", "old rule"
		}
		f.RuleOffer = append(f.RuleOffer, c)
	}
	return f
}

func TestRulesAskIsAMultiSelectWithWhatItReplaces(t *testing.T) {
	s := InstructionFor(rulesFeature(5), "", "nxy")
	for _, want := range []string{
		plan.RulesQuestion("f1", 1, 2), plan.RulesQuestion("f1", 2, 2),
		`"multiSelect":true`, "reemplaza a K3", "Reglas 1/2", "S1 rule b", "Ninguna",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("instruction lacks %q:\n%s", want, s)
		}
	}
	if !strings.HasPrefix(s, "nxy: feature f1") {
		t.Errorf("instruction = %q", s)
	}
}

func TestRulesAskStopVariantAsksWithoutRepeating(t *testing.T) {
	s := StopInstruction(rulesFeature(2), "", "nxy")
	if !strings.Contains(s, "Input:\n") || strings.Contains(s, "nxy saves the rules") {
		t.Errorf("stop instruction = %s", s)
	}
}
