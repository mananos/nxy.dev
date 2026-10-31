package app

import (
	"context"
	"fmt"
	"io"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/flow"
	"github.com/mananos/nxy.dev/internal/review"
)

// refuterReturned is the return of a refuter that stopped without saving: the high findings
// still waiting for a verdict stay inconclusive (and are offered).
func refuterReturned(g *domain.Feature) []flow.Event {
	if g.Review == nil {
		return []flow.Event{{Kind: flow.RefuterDone}}
	}
	rv := review.MissingInconclusive(*g.Review)
	g.Review = &rv
	return []flow.Event{{Kind: flow.RefuterDone, Findings: len(review.Offered(rv)) > 0}}
}

// SaveRefute decodes the refuter's verdicts and stores them; it closes the refuting step in the
// same transition. It returns how many verdicts were saved. An invalid answer stores nothing.
func (h Hooks) SaveRefute(ctx context.Context, featureID string, r io.Reader) (int, error) {
	if h.Store == nil {
		return 0, fmt.Errorf("no store")
	}
	f, err := h.Store.Feature(ctx, featureID)
	if err != nil {
		return 0, fmt.Errorf("feature %s: %w", featureID, err)
	}
	if err := refutable(f); err != nil {
		return 0, err
	}
	vs, err := review.DecodeVerdicts(r)
	if err != nil {
		return 0, err
	}
	if _, err := review.ApplyVerdicts(*f.Review, vs); err != nil {
		return 0, err
	}
	_, _, err = h.advanceSteps(ctx, featureID, func(g *domain.Feature) ([]flow.Event, error) {
		if err := refutable(*g); err != nil {
			return nil, err
		}
		rv, err := review.ApplyVerdicts(*g.Review, vs)
		if err != nil {
			return nil, err
		}
		g.Review = &rv
		g.Undelivered = true
		return []flow.Event{{Kind: flow.RefuterDone, Findings: len(review.Offered(rv)) > 0}}, nil
	})
	if err != nil {
		return 0, err
	}
	return len(vs.Verdicts), nil
}

func refutable(f domain.Feature) error {
	if f.State.Phase != domain.PhaseRefuting || f.Review == nil {
		return fmt.Errorf("feature %s is not refuting (state %s); next step %s", f.ID, f.State.Phase, f.Next.Label())
	}
	return nil
}
