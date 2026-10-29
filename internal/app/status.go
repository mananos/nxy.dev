package app

import (
	"context"
	"strings"

	"github.com/mananos/nxy.dev/internal/domain"
)

// StatusView is what `nxy status` shows; the zero value means no active feature.
type StatusView struct {
	FeatureID string
	Request   string
	Public    domain.Public
	Phase     string
	Next      string
	PlanRev   int
}

// Status describes the active feature of the repo dir belongs to.
func (h Hooks) Status(ctx context.Context, dir string) (StatusView, error) {
	f, ok, err := h.activeIn(ctx, dir)
	if err != nil || !ok {
		return StatusView{}, err
	}
	first, _, _ := strings.Cut(f.Request, "\n")
	return StatusView{
		FeatureID: f.ID,
		Request:   strings.TrimSpace(first),
		Public:    f.State.Phase.Public(),
		Phase:     f.State.Phase.Label(),
		Next:      f.Next.Label(),
		PlanRev:   f.PlanRev,
	}, nil
}

// activeIn is the active feature of the repo dir belongs to.
func (h Hooks) activeIn(ctx context.Context, dir string) (domain.Feature, bool, error) {
	if h.Store == nil || h.Repos == nil {
		return domain.Feature{}, false, nil
	}
	return h.Store.ActiveFeature(ctx, h.Repos.Key(dir))
}
