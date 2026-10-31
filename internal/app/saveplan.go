package app

import (
	"context"
	"fmt"
	"io"

	"github.com/mananos/nxy.dev/internal/design"
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
	if f.Mode == domain.ModeEngineer {
		d := f.Design
		if d == nil {
			d = &domain.Design{}
		}
		if probs := design.CheckPlan(d, p); len(probs) > 0 {
			return SaveResult{Problems: probs}, nil
		}
		design.Merge(d, &p)
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
	// nxy raises each batch's risk by what its files show (security, database, API).
	h.raisePlanRisk(f.ProjectDir, &p)
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
