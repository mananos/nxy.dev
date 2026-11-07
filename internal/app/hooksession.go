package app

import (
	"context"
	"strings"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/flow"
)

const legacyNotice = "nxy 1.x is still enabled: disable it while testing the v2 (both answer the same hooks)"

// shortError is the first line of err, cut to 80 runes.
func shortError(err error) string {
	s, _, _ := strings.Cut(strings.TrimSpace(err.Error()), "\n")
	if r := []rune(s); len(r) > 80 {
		s = string(r[:80]) + "…"
	}
	return s
}

// carryLink hands the feature of the session that just ended in a /clear to the new session of the
// same folder, so the feature follows it even when it holds no checkout of that folder.
func (h Hooks) carryLink(ctx context.Context, ev HookEvent) {
	if h.Sessions == nil || ev.SessionID == "" || ev.Source != "clear" {
		return
	}
	if s, ok, err := h.Sessions.Session(ctx, ev.SessionID); err == nil && ok && s.FeatureID != "" {
		return
	}
	since := h.now().Add(-clearPairWindow).UnixMilli()
	old, ok, err := h.Sessions.RecentEnd(ctx, sessionFolder(ev), "clear", since, foldNoteFolders)
	if err != nil || !ok || old.ID == ev.SessionID || old.FeatureID == "" {
		return
	}
	_ = h.Sessions.LinkFeature(ctx, ev.SessionID, old.FeatureID)
}

// sessionStart handles SessionStart: it re-injects the step of the active feature (so it
// survives /clear) and warns when the 1.x plugin is still on.
func (h Hooks) sessionStart(ctx context.Context, ev HookEvent) HookResponse {
	dir := ev.ProjectDir
	if dir == "" {
		dir = ev.Cwd
	}
	var r HookResponse
	// First thing, so the notes it brings already count for the "¿con cuál seguimos?" question.
	imported := ""
	c, first, ierr := h.ImportLegacy(ctx, dir)
	if first {
		// A repo the 1.x knew nothing about has nothing to announce.
		if text := c.Render(); text != "" {
			imported = "nxy: datos de la 1.x importados — " + text
		}
	}
	if ierr != nil {
		msg := "nxy: la importación de la 1.x falló (" + shortError(ierr) + "); probá `nxy legacy import`"
		if imported != "" {
			imported += "\n"
		}
		imported += msg
	}
	featureActive := false
	defer func() { h.startNotesSession(ctx, ev, featureActive) }()
	if h.Store != nil && h.Repos != nil {
		h.carryLink(ctx, ev)
		if f, found := h.featureFor(ctx, ev); found {
			featureActive = true
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
			if fresh && f.State.Phase == domain.PhaseRunningBatch && (len(f.Lanes) > 0 || len(launchedBatches(f)) > 0) {
				// The implementers of the old process are gone: the batches go out again.
				nf, _, err := h.advanceSteps(ctx, f.ID, func(g *domain.Feature) ([]flow.Event, error) {
					g.Lanes = nil
					if g.State.Phase != domain.PhaseRunningBatch || len(launchedBatches(*g)) == 0 {
						return nil, errSkipEvent
					}
					return []flow.Event{{Kind: flow.Relaunch}}, nil
				})
				if err == nil {
					f = nf
				}
			}
			f = h.ensureReview(ctx, f)
			if fresh && f.State.Phase == domain.PhaseReviewing && f.Review != nil {
				// The reviewers of the old process are gone: the lenses go out again.
				if g, ok, err := h.mutate(ctx, f.ID, func(g *domain.Feature) bool {
					if g.State.Phase != domain.PhaseReviewing || g.Review == nil {
						return false
					}
					changed := len(g.Review.Returned) > 0
					g.Review.Returned = nil
					for i := range g.Review.Lenses {
						if l := &g.Review.Lenses[i]; !l.Saved && l.Dispatched {
							l.Dispatched, changed = false, true
						}
					}
					return changed
				}); err == nil && ok {
					f = g
				}
			}
			role, known := roles[f.Background]
			_, inPhase := role[f.State.Phase]
			reviewing := reviewInFlight(f)
			// Batches launched by this very process are still running (a /clear or a resume).
			waiting := launchedBatches(f)
			batchesRunning := f.State.Phase == domain.PhaseRunningBatch && len(waiting) > 0 && len(unlaunchedBatches(f)) == 0
			running := (known && inPhase) || reviewing || batchesRunning
			if reviewing {
				r.Context = reviewWait(f)
			} else if batchesRunning {
				r.Context = batchesWait(f.ID, waiting)
			} else if running {
				r.Context = backgroundWait(f.ID, f.Background)
			} else {
				r.Next = &Next{Feature: f}
			}
			if resume := h.resumeContext(ctx, ev, f); resume != "" {
				if r.Context != "" {
					r.Context += "\n\n"
				}
				r.Context += resume
			}
			if fresh || ev.Source == "clear" {
				if reviewing {
					r.Notice = "nxy: feature " + f.ID + " — the reviewers are still running; their result arrives when they end"
				} else if batchesRunning {
					w := "el lote " + domain.JoinInts(waiting, "y") + " sigue"
					if len(waiting) > 1 {
						w = "los lotes " + domain.JoinInts(waiting, "y") + " siguen"
					}
					r.Notice = "nxy: feature " + f.ID + " — " + w + " corriendo; su resultado llega cuando terminan"
				} else if running {
					r.Notice = "nxy: feature " + f.ID + " — the " + strings.TrimPrefix(f.Background, "nxy:") + " is still running; its result arrives when it ends"
				} else {
					r.Notice = "nxy: feature " + f.ID + " pendiente (" + f.Next.Label() + ") — escribí cualquier cosa para seguir"
				}
			}
		}
	}
	if imported != "" {
		if r.Notice != "" {
			r.Notice += "\n"
		}
		r.Notice += imported
	}
	if h.Legacy != nil && h.Legacy.Active(dir) {
		if r.Notice != "" {
			r.Notice += "\n"
		}
		r.Notice += legacyNotice
	}
	return r
}
