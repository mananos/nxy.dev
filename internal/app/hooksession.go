package app

import (
	"context"
	"strings"

	"github.com/mananos/nxy.dev/internal/domain"
)

const legacyNotice = "nxy 1.x is still enabled: disable it while testing the v2 (both answer the same hooks)"

// sessionStart handles SessionStart: it re-injects the step of the active feature (so it
// survives /clear) and warns when the 1.x plugin is still on.
func (h Hooks) sessionStart(ctx context.Context, ev HookEvent) HookResponse {
	dir := ev.ProjectDir
	if dir == "" {
		dir = ev.Cwd
	}
	var r HookResponse
	if h.Store != nil && h.Repos != nil {
		if f, found, err := h.Store.ActiveFeature(ctx, h.Repos.Key(dir)); err == nil && found {
			fresh := ev.Source == "startup" || ev.Source == ""
			if fresh && f.Background != "" {
				// A new process cannot have a planner still running from the old one.
				if g, ok, err := h.mutate(ctx, f.ID, func(g *domain.Feature) bool {
					if g.Background == "" {
						return false
					}
					g.Background = ""
					return true
				}); err == nil && ok {
					f = g
				} else {
					f.Background = ""
				}
			}
			role, known := roles[f.Background]
			_, inPhase := role[f.State.Phase]
			running := known && inPhase
			if running {
				r.Context = backgroundWait(f.ID, f.Background)
			} else {
				r.Next = &Next{Feature: f}
			}
			if fresh || ev.Source == "clear" {
				if running {
					r.Notice = "nxy: feature " + f.ID + " — the " + strings.TrimPrefix(f.Background, "nxy:") + " is still running; its result arrives when it ends"
				} else {
					r.Notice = "nxy: feature " + f.ID + " pendiente (" + f.Next.Label() + ") — escribí cualquier cosa para seguir"
				}
			}
		}
	}
	if h.Legacy != nil && h.Legacy.Active(dir) {
		if r.Notice != "" {
			r.Notice += "\n"
		}
		r.Notice += legacyNotice
	}
	return r
}
