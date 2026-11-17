package app

import (
	"context"
	"errors"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/flow"
)

// ErrNoActiveFeature means there is no feature to act on in the repo.
var ErrNoActiveFeature = errors.New("no hay una feature activa")

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
	return h.stopFeature(ctx, f)
}

// StopFeatureByID ends the feature with that id (the TUI's stop). Stopping also lets go of the
// questions the hook holds for it: a hook waiting for the answer sees it was stopped and refuses it.
func (h Hooks) StopFeatureByID(ctx context.Context, id string) (f domain.Feature, warning error, err error) {
	if h.Store == nil {
		return domain.Feature{}, nil, ErrNoActiveFeature
	}
	f, err = h.Store.Feature(ctx, id)
	if err != nil {
		return domain.Feature{}, nil, err
	}
	return h.stopFeature(ctx, f)
}

func (h Hooks) stopFeature(ctx context.Context, f domain.Feature) (nf domain.Feature, warning error, err error) {
	nf, _, err = h.advance(ctx, f.ID, flow.Event{Kind: flow.Stop})
	if err != nil {
		return domain.Feature{}, nil, err
	}
	if h.Asks != nil {
		_ = h.Asks.ReleaseFeatureAsks(ctx, f.ID, answeredByStopped)
	}
	h.deleteFeatureNotes(ctx, f.ID)
	if h.Viewer != nil {
		warning = h.Viewer.Discard(f.ID)
	}
	if h.ReviewFiles != nil {
		if err := h.ReviewFiles.Discard(f.ID); err != nil && warning == nil {
			warning = err
		}
	}
	return nf, warning, nil
}
