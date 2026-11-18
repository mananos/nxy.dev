package claudecode

import (
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/domain"
	"github.com/mananos/nxy.dev/v2/internal/plan"
)

func clashFeature(repos int) domain.Feature {
	f := domain.Feature{
		ID:    "f1",
		State: domain.State{Phase: domain.PhaseRepoClash},
		Next:  domain.Action{Kind: domain.AskClash},
		Clash: []domain.RepoClash{{Repo: "api", Checkout: "/w/api", FeatureID: "f0", Phase: "running_batch"}},
	}
	for _, n := range []string{"api", "web"}[:repos] {
		f.Repos = append(f.Repos, domain.FeatureRepo{Name: n})
	}
	return f
}

func TestClashAskNamesTheHolderAndOffersTheWayOut(t *testing.T) {
	s := Instruction(clashFeature(1), "")
	for _, want := range []string{
		plan.ClashQuestion("f1"), "repo api: feature f0", "Pausar la feature f0", plan.ClashWorktree, plan.ClashStopLabel,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("instruction lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "del plan") {
		t.Errorf("one repo cannot be dropped:\n%s", s)
	}
}

func TestClashAskWithTwoReposOffersToDropTheBusyOne(t *testing.T) {
	s := Instruction(clashFeature(2), "")
	if !strings.Contains(s, "Sacar api del plan") || strings.Contains(s, plan.ClashWorktree) {
		t.Errorf("instruction = %s", s)
	}
}
