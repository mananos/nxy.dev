package app

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/flow"
	"github.com/mananos/nxy.dev/internal/plan"
)

// planCheckouts is the checkouts the plan of f works in.
func (h Hooks) planCheckouts(f domain.Feature) []string {
	if len(f.Repos) > 0 {
		out := make([]string, 0, len(f.Repos))
		for _, r := range f.Repos {
			co := r.Checkout
			if co == "" {
				co = r.Root
			}
			out = append(out, co)
		}
		return out
	}
	if h.Repos != nil {
		return []string{h.Repos.Checkout(f.ProjectDir)}
	}
	return []string{f.ProjectDir}
}

// remainingClashes is the clashes of f that still stand: the checkout is held by another feature.
func (h Hooks) remainingClashes(ctx context.Context, f domain.Feature) []domain.RepoClash {
	var out []domain.RepoClash
	for _, c := range f.Clash {
		other, found, err := h.Store.ActiveFeature(ctx, c.Checkout)
		if err != nil || !found || other.ID == f.ID {
			continue
		}
		c.FeatureID, c.Phase = other.ID, string(other.State.Phase)
		out = append(out, c)
	}
	return out
}

// askAgain keeps the feature in the repo-busy phase with the clashes that still stand.
func (h Hooks) askAgain(ctx context.Context, f domain.Feature, cs []domain.RepoClash, note string) HookResponse {
	nf, _, err := h.mutate(ctx, f.ID, func(g *domain.Feature) bool {
		if g.State.Phase != domain.PhaseRepoClash {
			return false
		}
		g.Clash = cs
		return true
	})
	if err != nil || nf.ID == "" {
		nf = f
	}
	return HookResponse{Next: &Next{Feature: nf, Note: note}}
}

// answerClash turns the answer to the repo-busy question into the next step: pausing the other
// feature or opening in a worktree applies the plan that was waiting (so it goes to the approval
// without another planner), dropping the repo re-plans, stopping ends the feature.
func (h Hooks) answerClash(ctx context.Context, f domain.Feature, answer string) HookResponse {
	retry := HookResponse{Next: &Next{Feature: f, Note: "nxy could not record your answer; ask again"}}
	choice := plan.ParseClash(answer)
	if choice == plan.ClashNone {
		return HookResponse{Next: &Next{Feature: f, Note: "pick one of the options"}}
	}
	if choice == plan.ClashStopFeature {
		nf, _, err := h.advance(ctx, f.ID, flow.Event{Kind: flow.Stop})
		if err != nil {
			return retry
		}
		h.deleteFeatureNotes(ctx, f.ID)
		return HookResponse{Next: &Next{Feature: nf}}
	}
	if choice == plan.ClashDrop {
		return h.dropClashRepos(ctx, f)
	}
	if f.PendingPlan == nil {
		return retry
	}
	if choice == plan.ClashPause {
		for _, id := range plan.ClashFeatures(f.Clash) {
			if _, err := h.PauseFeature(ctx, id); err != nil {
				return HookResponse{Next: &Next{Feature: f, Note: "nxy could not pause feature " + id + ": " + err.Error()}}
			}
		}
		if cs := h.remainingClashes(ctx, f); len(cs) > 0 {
			return h.askAgain(ctx, f, cs, "another feature holds the repo now; ask again")
		}
	}
	nf, err := h.applyPendingPlan(ctx, f, choice == plan.ClashOpenWorktree)
	if errors.Is(err, ErrActiveFeature) {
		return h.askAgain(ctx, f, h.remainingClashes(ctx, f), "another feature took the repo; ask again")
	}
	if err != nil || nf.ID == "" {
		return retry
	}
	return HookResponse{Next: &Next{Feature: nf}}
}

// applyPendingPlan stores the plan that waited for the clash and moves the feature on as a saved
// plan. With worktree the busy checkouts are not held (the worktrees are made when the plan is
// approved).
func (h Hooks) applyPendingPlan(ctx context.Context, f domain.Feature, worktree bool) (domain.Feature, error) {
	if f.PendingPlan == nil {
		return domain.Feature{}, errNoChange
	}
	busy := map[string]bool{}
	for _, c := range f.Clash {
		busy[c.Checkout] = true
	}
	nf, _, err := h.advanceSteps(ctx, f.ID, func(g *domain.Feature) ([]flow.Event, error) {
		if g.State.Phase != domain.PhaseRepoClash || g.PendingPlan == nil {
			return nil, errNoChange
		}
		p := *g.PendingPlan
		g.Plan = &p
		g.PlanRev++
		g.Holds = planHolds(*g, len(g.Repos) > 0, h.planCheckouts(*g))
		if worktree {
			g.InWorktree = true
			g.Holds = slices.DeleteFunc(g.Holds, func(co string) bool { return busy[co] })
		}
		g.PendingPlan, g.Clash = nil, nil
		return []flow.Event{{Kind: flow.ClashAnswered, Questions: len(p.Questions) > 0, NoTests: p.NoTests, Batches: len(p.Batches)}}, nil
	})
	if err != nil {
		return domain.Feature{}, err
	}
	return nf, nil
}

// dropClashRepos goes back to the planner with a note: the busy repos leave the plan.
func (h Hooks) dropClashRepos(ctx context.Context, f domain.Feature) HookResponse {
	names := plan.ClashRepos(f.Clash)
	nf, _, err := h.advanceWith(ctx, f.ID, flow.Event{Kind: flow.Answered}, func(g *domain.Feature) error {
		if g.State.Phase != domain.PhaseRepoClash {
			return errNoChange
		}
		g.ChangeNotes = append(append([]string(nil), g.ChangeNotes...), "sacá el repo "+strings.Join(names, ", ")+" del plan")
		g.PendingPlan, g.Clash = nil, nil
		return nil
	})
	if err != nil {
		return HookResponse{Next: &Next{Feature: f, Note: "nxy could not record your answer; ask again"}}
	}
	return HookResponse{Next: &Next{Feature: nf}}
}
