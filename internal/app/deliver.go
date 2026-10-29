package app

import (
	"context"

	"github.com/mananos/nxy.dev/internal/domain"
)

// deliverPending adds to a main-thread response the step of a feature whose result arrived
// while the main thread was not looking (a background agent). It is delivered exactly once:
// the flag is claimed atomically, so two hooks never both carry it.
func (h Hooks) deliverPending(ctx context.Context, ev HookEvent, resp HookResponse) HookResponse {
	if ev.AgentID != "" || resp.Deny != "" {
		return resp
	}
	f, ok := h.activeFeature(ctx, ev)
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
