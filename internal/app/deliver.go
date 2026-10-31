package app

import (
	"context"

	"github.com/mananos/nxy.dev/internal/domain"
)

// latestDone is the repo's most recent feature when it is done.
func (h Hooks) latestDone(ctx context.Context, ev HookEvent) (domain.Feature, bool) {
	if h.Store == nil || h.Repos == nil {
		return domain.Feature{}, false
	}
	dir := ev.ProjectDir
	if dir == "" {
		dir = ev.Cwd
	}
	f, found, err := h.Store.LatestFeature(ctx, h.Repos.Key(dir))
	if err != nil || !found || f.State.Phase != domain.PhaseDone {
		return domain.Feature{}, false
	}
	return f, true
}

// deliverPending adds to a main-thread response the step of a feature whose result arrived
// while the main thread was not looking (a background agent). It is delivered exactly once:
// the flag is claimed atomically, so two hooks never both carry it.
func (h Hooks) deliverPending(ctx context.Context, ev HookEvent, resp HookResponse) HookResponse {
	if ev.AgentID != "" || resp.Deny != "" {
		return resp
	}
	f, ok := h.activeFeature(ctx, ev)
	if !ok {
		// A feature that finished inside a subagent hook is done: its closing step still has to arrive.
		f, ok = h.latestDone(ctx, ev)
	}
	if !ok || !f.Undelivered {
		return resp
	}
	nf, claimed, err := h.mutate(ctx, f.ID, func(g *domain.Feature) bool {
		if !g.Undelivered {
			return false
		}
		g.Undelivered = false
		return true
	})
	if err != nil || !claimed {
		return resp
	}
	if resp.Next == nil {
		resp.Next = &Next{Feature: nf}
	}
	return resp
}
