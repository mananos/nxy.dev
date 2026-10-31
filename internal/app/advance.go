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

// errNoChange, returned inside a Transition, aborts it without writing; mutate swallows it.
var errNoChange = errors.New("no change")

// advanceWith is advance with a change to the feature (mutate may be nil) applied in the same
// transition, before the event; an error from mutate aborts it, except errSkipEvent, which
// stores the change and leaves the state as is.
func (h Hooks) advanceWith(ctx context.Context, id string, ev flow.Event, mutate func(*domain.Feature) error) (domain.Feature, []domain.Action, error) {
	return h.advanceSteps(ctx, id, func(f *domain.Feature) ([]flow.Event, error) {
		if mutate != nil {
			if err := mutate(f); err != nil {
				return nil, err
			}
		}
		return []flow.Event{ev}, nil
	})
}

// advanceSteps is advance for several events in one transition: fn changes the feature and
// returns the events to apply in order (the later ones may depend on what fn stored). The
// feature keeps in Next the first main-thread action of the last step. An error from fn aborts
// it, except errSkipEvent, which stores the change and applies no event.
func (h Hooks) advanceSteps(ctx context.Context, id string, fn func(*domain.Feature) ([]flow.Event, error)) (domain.Feature, []domain.Action, error) {
	var out domain.Feature
	var acts []domain.Action
	err := h.Store.Transition(ctx, id, func(f domain.Feature) (domain.Feature, error) {
		evs, err := fn(&f)
		if err != nil {
			if errors.Is(err, errSkipEvent) {
				out, acts = f, nil
				return f, nil
			}
			return f, err
		}
		var as []domain.Action
		for _, ev := range evs {
			s, a, err := flow.Next(f.State, ev)
			if err != nil {
				return f, err
			}
			f.State, as = s, a
			f.Next = mainThreadAction(a)
		}
		out, acts = f, as
		return f, nil
	})
	if err != nil {
		return domain.Feature{}, nil, err
	}
	return out, acts, nil
}

// mutate changes the feature atomically without moving the flow. fn returns false to store
// nothing; it runs again on a retry, so it must re-check what it claims. The bool is whether
// fn applied its change.
func (h Hooks) mutate(ctx context.Context, id string, fn func(*domain.Feature) bool) (domain.Feature, bool, error) {
	var out domain.Feature
	var applied bool
	err := h.Store.Transition(ctx, id, func(f domain.Feature) (domain.Feature, error) {
		applied = fn(&f)
		if !applied {
			out = f
			return f, errNoChange
		}
		out = f
		return f, nil
	})
	if errors.Is(err, errNoChange) {
		return out, false, nil
	}
	if err != nil {
		return domain.Feature{}, false, err
	}
	return out, applied, nil
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
