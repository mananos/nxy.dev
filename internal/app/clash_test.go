package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/plan"
)

// clashedFeature is a feature whose plan was held back because another feature holds the api
// checkout, and whose planner already returned: it waits at "Repo ocupado".
func clashedFeature(t *testing.T, paths ...string) (Hooks, *memStore, domain.Feature, string) {
	t.Helper()
	h, st := flowHooks(false)
	ctx := context.Background()
	f := startedFeature(t, h, "x")
	other, err := st.CreateFeature(ctx, domain.Feature{Home: "o", Holds: []string{repoCo("api")}})
	if err != nil {
		t.Fatal(err)
	}
	res, err := h.SavePlan(ctx, f.ID, planReader(t, multiPlan(paths...)))
	if err != nil || res.Blocked == "" {
		t.Fatalf("res = %+v err = %v", res, err)
	}
	r := h.Handle(ctx, agentEv("PostToolUse", "nxy:planner"))
	g := st.feats[f.ID]
	if r.Next == nil || g.State.Phase != domain.PhaseRepoClash || g.Next.Kind != domain.AskClash {
		t.Fatalf("after the planner: %+v phase %s next %s", r, g.State.Phase, g.Next.Kind)
	}
	return h, st, g, other
}

func clashAnswer(h Hooks, f domain.Feature, a string) HookResponse {
	return h.Handle(context.Background(), askPost(f, plan.ClashQuestion(f.ID), a))
}

func TestClashPauseAppliesThePlanWithoutAnotherPlanner(t *testing.T) {
	h, st, f, other := clashedFeature(t, "api", "web")
	opts := plan.ClashOptions(f)
	if !strings.HasPrefix(opts[0].Label, plan.ClashPauseWord) || !opts[0].Recommended || !strings.Contains(opts[0].Label, other) {
		t.Fatalf("options = %+v", opts)
	}
	r := clashAnswer(h, f, opts[0].Label)
	if r.Next == nil {
		t.Fatal("no response")
	}
	g := st.feats[f.ID]
	if !st.feats[other].Paused {
		t.Error("the other feature was not paused")
	}
	if g.State.Phase != domain.PhaseApproval || g.Plan == nil || g.PlanRev != 1 || g.PendingPlan != nil || g.Clash != nil {
		t.Errorf("feature = %+v", g)
	}
	if !slices.Contains(g.Holds, repoCo("api")) || !slices.Contains(g.Holds, repoCo("web")) {
		t.Errorf("holds = %v", g.Holds)
	}
	if g.Next.Kind != domain.AskApproval {
		t.Errorf("next = %s", g.Next.Kind)
	}
}

func TestClashWorktreeLetsGoOfTheBusyCheckout(t *testing.T) {
	h, st, f, other := clashedFeature(t, "api")
	opts := plan.ClashOptions(f)
	if len(opts) != 3 || opts[1].Label != plan.ClashWorktree {
		t.Fatalf("options = %+v", opts)
	}
	clashAnswer(h, f, plan.ClashWorktree)
	g := st.feats[f.ID]
	if st.feats[other].Paused {
		t.Error("the other feature was paused")
	}
	if !g.InWorktree || g.State.Phase != domain.PhaseApproval || g.Plan == nil {
		t.Errorf("feature = %+v", g)
	}
	if slices.Contains(g.Holds, repoCo("api")) {
		t.Errorf("holds = %v", g.Holds)
	}
}

func TestClashDropReplansWithANote(t *testing.T) {
	h, st, f, _ := clashedFeature(t, "api", "web")
	opts := plan.ClashOptions(f)
	if len(opts) != 3 || opts[1].Label != "Sacar api del plan" {
		t.Fatalf("options = %+v", opts)
	}
	clashAnswer(h, f, opts[1].Label)
	g := st.feats[f.ID]
	if g.State.Phase != domain.PhasePlanning || g.Next.Kind != domain.DispatchPlanner || g.PendingPlan != nil || g.Clash != nil || g.Plan != nil {
		t.Errorf("feature = %+v", g)
	}
	if len(g.ChangeNotes) != 1 || g.ChangeNotes[0] != "sacá el repo api del plan" {
		t.Errorf("notes = %v", g.ChangeNotes)
	}
}

func TestClashStopEndsTheFeature(t *testing.T) {
	h, st, f, other := clashedFeature(t, "api", "web")
	clashAnswer(h, f, plan.ClashStopLabel)
	g := st.feats[f.ID]
	if g.State.Phase != domain.PhaseDone || g.State.Outcome != domain.OutcomeStopped {
		t.Errorf("state = %+v", g.State)
	}
	if st.feats[other].Paused {
		t.Error("the other feature was paused")
	}
}

func TestClashUnknownAnswerAsksAgain(t *testing.T) {
	h, st, f, _ := clashedFeature(t, "api")
	r := clashAnswer(h, f, "no sé")
	if r.Next == nil || !strings.Contains(r.Next.Note, "pick one") || st.feats[f.ID].State.Phase != domain.PhaseRepoClash {
		t.Errorf("response = %+v phase %s", r, st.feats[f.ID].State.Phase)
	}
}

func TestClashAnswerOutsideThePhaseIsIgnored(t *testing.T) {
	h, st := flowHooks(false)
	f := startedFeature(t, h, "x")
	if r := clashAnswer(h, f, plan.ClashWorktree); r.Next != nil && st.feats[f.ID].InWorktree {
		t.Errorf("response = %+v", r)
	}
	if st.feats[f.ID].State.Phase != domain.PhasePlanning {
		t.Errorf("phase = %s", st.feats[f.ID].State.Phase)
	}
}

func TestClashIsAnNxyQuestion(t *testing.T) {
	f := domain.Feature{ID: "F1"}
	if !isNxyQuestion(f, plan.ClashQuestion("F1")) {
		t.Error("the repo-busy question is not an nxy question")
	}
}

func TestPauseReleasesTheCheckouts(t *testing.T) {
	h, st := flowHooks(false)
	f := startedFeature(t, h, "x")
	ctx := context.Background()
	if _, found, _ := st.ActiveFeature(ctx, "repo:/p"); !found {
		t.Fatal("the feature holds nothing")
	}
	g, err := h.PauseFeature(ctx, f.ID)
	if err != nil || !g.Paused {
		t.Fatalf("g = %+v err = %v", g, err)
	}
	if _, found, _ := st.ActiveFeature(ctx, "repo:/p"); found {
		t.Error("the paused feature still holds its checkout")
	}
	if _, err := h.PauseFeature(ctx, f.ID); err != nil {
		t.Errorf("pausing twice: %v", err)
	}
}

func TestPauseOfADoneFeatureFails(t *testing.T) {
	h, _ := flowHooks(false)
	f := startedFeature(t, h, "x")
	ctx := context.Background()
	if _, _, err := h.StopFeature(ctx, "/p"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.PauseFeature(ctx, f.ID); err == nil {
		t.Error("a done feature was paused")
	}
}

// strictStore refuses a Transition that makes a feature hold a checkout another one holds, as the
// real store does.
type strictStore struct{ *memStore }

func (s strictStore) Transition(ctx context.Context, id string, fn func(domain.Feature) (domain.Feature, error)) error {
	return s.memStore.Transition(ctx, id, func(f domain.Feature) (domain.Feature, error) {
		nf, err := fn(f)
		if err != nil {
			return nf, err
		}
		for _, co := range nf.Held() {
			for _, g := range s.memStore.feats {
				if g.ID != id && slices.Contains(g.Held(), co) {
					return f, ErrActiveFeature
				}
			}
		}
		return nf, nil
	})
}

func TestResumeTakesTheCheckoutsBack(t *testing.T) {
	h, st := flowHooks(false)
	h.Store = strictStore{st}
	f := startedFeature(t, h, "x")
	ctx := context.Background()
	if _, err := h.PauseFeature(ctx, f.ID); err != nil {
		t.Fatal(err)
	}
	g, err := h.ResumeFeature(ctx, f.ID)
	if err != nil || g.Paused {
		t.Fatalf("g = %+v err = %v", g, err)
	}
	if got, found, _ := st.ActiveFeature(ctx, "repo:/p"); !found || got.ID != f.ID {
		t.Error("the feature does not hold its checkout again")
	}
}

func TestResumeWhenAnotherFeatureTookTheCheckout(t *testing.T) {
	h, st := flowHooks(false)
	h.Store = strictStore{st}
	f := startedFeature(t, h, "x")
	ctx := context.Background()
	if _, err := h.PauseFeature(ctx, f.ID); err != nil {
		t.Fatal(err)
	}
	other, err := st.CreateFeature(ctx, domain.Feature{Home: "repo:/p", Holds: []string{"repo:/p"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.ResumeFeature(ctx, f.ID)
	if !errors.Is(err, ErrActiveFeature) || !strings.Contains(err.Error(), "repo:/p") || !strings.Contains(err.Error(), other) {
		t.Fatalf("err = %v", err)
	}
	if !st.feats[f.ID].Paused {
		t.Error("the feature was resumed")
	}
}
