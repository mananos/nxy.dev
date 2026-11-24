package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"slices"

	"github.com/mananos/nxy.dev/v2/internal/design"
	"github.com/mananos/nxy.dev/v2/internal/domain"
	"github.com/mananos/nxy.dev/v2/internal/flow"
	"github.com/mananos/nxy.dev/v2/internal/plan"
	"github.com/mananos/nxy.dev/v2/internal/repos"
)

// SaveResult is the outcome of SavePlan: Problems when the plan was refused (nothing stored),
// otherwise Rev is the stored revision.
type SaveResult struct {
	Problems []plan.Problem
	Rev      int
	// Blocked is the line for the planner when the plan was not saved because a repo's checkout is
	// held by another feature (nothing is stored but the plan waiting for the answer).
	Blocked string
}

// SavePlan decodes and validates the planner's JSON and stores it, moving the feature on.
// An invalid plan leaves the store untouched.
func (h Hooks) SavePlan(ctx context.Context, featureID string, r io.Reader) (SaveResult, error) {
	if h.Store == nil {
		return SaveResult{}, fmt.Errorf("no store")
	}
	f, err := h.Store.Feature(ctx, featureID)
	if err != nil {
		return SaveResult{}, fmt.Errorf("feature %s: %w", featureID, err)
	}
	if f.State.Phase != domain.PhasePlanning {
		return SaveResult{}, notPlanning(f)
	}
	p, err := plan.Decode(r)
	if err != nil {
		return SaveResult{}, err
	}
	rs, probs := h.resolvePlanRepos(f, &p)
	if len(probs) > 0 {
		return SaveResult{Problems: probs}, nil
	}
	names := planRepoNames(rs)
	probs = append(plan.ValidateRepos(p, names), plan.Validate(p)...)
	if len(probs) > 0 {
		return SaveResult{Problems: probs}, nil
	}
	tagBatchRepos(&p, names)
	if f.Mode == domain.ModeEngineer {
		d := f.Design
		if d == nil {
			d = &domain.Design{}
		}
		if probs := design.CheckPlan(d, p, len(f.ChangeNotes) > 0); len(probs) > 0 {
			return SaveResult{Problems: probs}, nil
		}
		design.Merge(d, &p)
	}
	// The repo's own runners run commands: the user sees them at the checkpoint (nxy sets them,
	// whatever the planner wrote).
	dirs := []string{f.ProjectDir}
	if len(rs) > 0 {
		dirs = nil
		for _, r := range rs {
			dirs = append(dirs, r.Root)
		}
	}
	h.planRunners(dirs, &p)
	// nxy raises each batch's risk by what its files show (security, database, API).
	if len(rs) > 0 {
		h.raiseRepoRisk(&p, rs)
	} else {
		h.raisePlanRisk(f.ProjectDir, &p)
	}

	// The repos the feature works in and the checkouts it holds: the session folder's if it holds
	// it (the home), plus each repo's.
	var frs []domain.FeatureRepo
	var cnames, checkouts []string
	if len(rs) > 0 {
		for _, r := range rs {
			co := r.Root
			key := r.Root
			if h.Repos != nil {
				co, key = h.Repos.Checkout(r.Root), h.Repos.Key(r.Root)
			}
			frs = append(frs, domain.FeatureRepo{Name: r.Name, Key: key, Root: r.Root, Checkout: co, Role: r.Role})
			cnames = append(cnames, r.Name)
			checkouts = append(checkouts, co)
		}
	} else {
		co := f.ProjectDir
		if h.Repos != nil {
			co = h.Repos.Checkout(f.ProjectDir)
		}
		name := repos.Names([]string{f.ProjectDir}, runtime.GOOS == "windows")[f.ProjectDir]
		cnames, checkouts = []string{name}, []string{co}
	}
	holds := planHolds(f, len(rs) > 0, checkouts)

	block := func(cs []domain.RepoClash) (SaveResult, error) {
		_, applied, err := h.mutate(ctx, featureID, func(g *domain.Feature) bool {
			if g.State.Phase != domain.PhasePlanning {
				return false
			}
			g.PendingPlan = &p
			g.Clash = cs
			g.Repos = frs
			return true
		})
		if err != nil {
			return SaveResult{}, err
		}
		if !applied {
			return SaveResult{}, notPlanning(f)
		}
		return SaveResult{Blocked: clashLine(cs)}, nil
	}
	if cs := h.planClashes(ctx, f, cnames, checkouts); len(cs) > 0 {
		return block(cs)
	}
	ev := flow.Event{Kind: flow.PlanSaved, Questions: len(p.Questions) > 0, NoTests: p.NoTests, Batches: len(p.Batches), Steps: plan.Steps(p)}
	nf, _, err := h.advanceWith(ctx, featureID, ev, func(f *domain.Feature) error {
		if f.State.Phase != domain.PhasePlanning {
			return notPlanning(*f)
		}
		f.Plan = &p
		f.PlanRev++
		f.Repos = frs
		f.Holds = holds
		f.PendingPlan, f.Clash = nil, nil
		return nil
	})
	if errors.Is(err, ErrActiveFeature) {
		// another feature took a checkout between the check and the store: same as a clash
		if cs := h.planClashes(ctx, f, cnames, checkouts); len(cs) > 0 {
			return block(cs)
		}
	}
	if err != nil {
		return SaveResult{}, err
	}
	return SaveResult{Rev: nf.PlanRev}, nil
}

// planHolds is the checkouts a feature holds once its plan is saved: with declared repos the home
// folder if it holds it, plus the checkout of each repo.
func planHolds(f domain.Feature, declared bool, checkouts []string) []string {
	var holds []string
	if declared && f.Home != "" && slices.Contains(f.Holds, f.Home) {
		holds = []string{f.Home}
	}
	for _, co := range checkouts {
		if !slices.Contains(holds, co) {
			holds = append(holds, co)
		}
	}
	return holds
}

// SaveGaps is `plan gaps`: the planner of an engineer feature found holes in the design. They
// come back as open forks and the feature returns to Diseñando. Invalid input leaves the store untouched.
func (h Hooks) SaveGaps(ctx context.Context, featureID string, r io.Reader) (DesignResult, error) {
	if h.Store == nil {
		return DesignResult{}, fmt.Errorf("no store")
	}
	f, err := h.Store.Feature(ctx, featureID)
	if err != nil {
		return DesignResult{}, fmt.Errorf("feature %s: %w", featureID, err)
	}
	if f.State.Phase != domain.PhasePlanning {
		return DesignResult{}, notPlanning(f)
	}
	if f.Mode != domain.ModeEngineer {
		return DesignResult{}, fmt.Errorf("feature %s is not an engineer feature: it has no design to go back to", f.ID)
	}
	titles, err := design.DecodeGaps(r)
	if err != nil {
		return DesignResult{}, err
	}
	if probs := design.ValidateGaps(titles); len(probs) > 0 {
		return DesignResult{Problems: probs}, nil
	}
	nf, _, err := h.advanceWith(ctx, featureID, flow.Event{Kind: flow.GapsFound}, func(g *domain.Feature) error {
		if g.State.Phase != domain.PhasePlanning {
			return notPlanning(*g)
		}
		src := g.Design
		if src == nil {
			src = &domain.Design{}
		}
		d := cloneDesign(src)
		design.AddGaps(&d, titles)
		g.Design = &d
		return nil
	})
	if err != nil {
		return DesignResult{}, err
	}
	return DesignResult{Feature: nf}, nil
}

func notPlanning(f domain.Feature) error {
	return fmt.Errorf("feature %s is not planning (state %s); next step %s", f.ID, f.State.Phase, f.Next.Label())
}
