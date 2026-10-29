package app

import (
	"context"
	"errors"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/flow"
)

// advance applies ev to the feature through flow.Next, atomically, and returns the stored
// feature and the actions to perform. It is the only place in the package that calls flow.Next.
// The feature keeps in Next the first action the main thread has to do.
func (h Hooks) advance(ctx context.Context, id string, ev flow.Event) (domain.Feature, []domain.Action, error) {
	return h.advanceWith(ctx, id, ev, nil)
}

// errSkipEvent, returned by mutate, keeps the mutation but does not apply the event.
var errSkipEvent = errors.New("skip event")

// advanceWith is advance with a change to the feature (mutate may be nil) applied in the same
// transition, before the event; an error from mutate aborts it, except errSkipEvent, which
// stores the change and leaves the state as is.
func (h Hooks) advanceWith(ctx context.Context, id string, ev flow.Event, mutate func(*domain.Feature) error) (domain.Feature, []domain.Action, error) {
	var out domain.Feature
	var acts []domain.Action
	err := h.Store.Transition(ctx, id, func(f domain.Feature) (domain.Feature, error) {
		if mutate != nil {
			if err := mutate(&f); err != nil {
				if errors.Is(err, errSkipEvent) {
					out = f
					return f, nil
				}
				return f, err
			}
		}
		s, as, err := flow.Next(f.State, ev)
		if err != nil {
			return f, err
		}
		f.State = s
		f.Next = mainThreadAction(as)
		out, acts = f, as
		return f, nil
	})
	if err != nil {
		return domain.Feature{}, nil, err
	}
	return out, acts, nil
}

// mainThreadAction is the first action the main thread performs, or the zero Action.
func mainThreadAction(as []domain.Action) domain.Action {
	for _, a := range as {
		if a.MainThread() {
			return a
		}
	}
	return domain.Action{}
}
