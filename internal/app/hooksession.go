package app

import "context"

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
			r.Next = &Next{Feature: f}
		}
	}
	if h.Legacy != nil && h.Legacy.Active(dir) {
		r.Notice = legacyNotice
	}
	return r
}
