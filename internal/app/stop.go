package app

import (
	"context"
	"errors"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/flow"
)

// ErrNoActiveFeature means there is no feature to act on in the repo.
var ErrNoActiveFeature = errors.New("no active feature")

// StopFeature ends the active feature of the repo dir belongs to and discards its plan file.
// A failed discard is returned as warning: the stop itself was applied.
func (h Hooks) StopFeature(ctx context.Context, dir string) (f domain.Feature, warning error, err error) {
	f, ok, err := h.activeIn(ctx, dir)
	if err != nil {
		return domain.Feature{}, nil, err
	}
	if !ok {
		return domain.Feature{}, nil, ErrNoActiveFeature
	}
	nf, _, err := h.advance(ctx, f.ID, flow.Event{Kind: flow.Stop})
	if err != nil {
		return domain.Feature{}, nil, err
	}
	if h.Viewer != nil {
		warning = h.Viewer.Discard(f.ID)
	}
	return nf, warning, nil
}
