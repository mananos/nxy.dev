package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/mananos/nxy.dev/v2/internal/domain"
)

// PauseFeature pauses an active feature: it holds no checkout while paused, so another feature can
// take them. Pausing a paused feature changes nothing; a finished one cannot be paused.
func (h Hooks) PauseFeature(ctx context.Context, id string) (domain.Feature, error) {
	if h.Store == nil {
		return domain.Feature{}, fmt.Errorf("no hay base de datos")
	}
	var finished bool
	f, _, err := h.mutate(ctx, id, func(g *domain.Feature) bool {
		if g.State.Phase == domain.PhaseDone {
			finished = true
			return false
		}
		if g.Paused {
			return false
		}
		g.Paused = true
		return true
	})
	if err != nil {
		return domain.Feature{}, err
	}
	if finished {
		return f, fmt.Errorf("la feature %s ya está terminada", id)
	}
	return f, nil
}

// ResumeFeature takes the checkouts of a paused feature back. If another feature holds one now it
// says which, and the feature stays paused.
func (h Hooks) ResumeFeature(ctx context.Context, id string) (domain.Feature, error) {
	if h.Store == nil {
		return domain.Feature{}, fmt.Errorf("no hay base de datos")
	}
	var finished bool
	f, _, err := h.mutate(ctx, id, func(g *domain.Feature) bool {
		if g.State.Phase == domain.PhaseDone {
			finished = true
			return false
		}
		if !g.Paused {
			return false
		}
		g.Paused = false
		return true
	})
	if errors.Is(err, ErrActiveFeature) {
		return domain.Feature{}, h.resumeBusy(ctx, id)
	}
	if err != nil {
		return domain.Feature{}, err
	}
	if finished {
		return f, fmt.Errorf("la feature %s ya está terminada", id)
	}
	return f, nil
}

// resumeBusy words the refusal to resume: the checkout and the feature that holds it now.
func (h Hooks) resumeBusy(ctx context.Context, id string) error {
	f, err := h.Store.Feature(ctx, id)
	if err != nil {
		return ErrActiveFeature
	}
	held := f.Holds
	if len(held) == 0 && f.Repo != "" {
		held = []string{f.Repo}
	}
	for _, co := range held {
		if other, found, err := h.Store.ActiveFeature(ctx, co); err == nil && found && other.ID != id {
			return fmt.Errorf("el checkout %s lo tiene ahora la feature %s: %w", co, other.ID, ErrActiveFeature)
		}
	}
	return ErrActiveFeature
}
