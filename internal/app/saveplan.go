package app

import (
	"context"
	"fmt"
	"io"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/flow"
	"github.com/mananos/nxy.dev/internal/plan"
)

// SaveResult is the outcome of SavePlan: Problems when the plan was refused (nothing stored),
// otherwise Rev is the stored revision.
type SaveResult struct {
	Problems []plan.Problem
	Rev      int
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
	if probs := plan.Validate(p); len(probs) > 0 {
		return SaveResult{Problems: probs}, nil
	}
	// The repo's own runners run commands: the user sees them at the checkpoint (nxy sets them,
	// whatever the planner wrote).
	p.RepoRunners, p.RepoRunnersWarn = nil, ""
	if h.Runners != nil {
		rows, warn := h.Runners.Rows(f.ProjectDir)
		p.RepoRunnersWarn = warn
		for _, r := range rows {
			if r.Repo {
				p.RepoRunners = append(p.RepoRunners, domain.RepoRunner{Name: r.Name, Command: r.Command})
			}
		}
	}
	ev := flow.Event{Kind: flow.PlanSaved, Questions: len(p.Questions) > 0, NoTests: p.NoTests, Batches: len(p.Batches)}
	nf, _, err := h.advanceWith(ctx, featureID, ev, func(f *domain.Feature) error {
		if f.State.Phase != domain.PhasePlanning {
			return notPlanning(*f)
		}
		f.Plan = &p
		f.PlanRev++
		return nil
	})
	if err != nil {
		return SaveResult{}, err
	}
	return SaveResult{Rev: nf.PlanRev}, nil
}

func notPlanning(f domain.Feature) error {
	return fmt.Errorf("feature %s is not planning (state %s); next step %s", f.ID, f.State.Phase, f.Next.Label())
}
