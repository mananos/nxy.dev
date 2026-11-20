package app

import (
	"context"
	"strings"

	"github.com/mananos/nxy.dev/v2/internal/domain"
)

// StatusView is what `nxy status` shows; the zero value means no active feature.
type StatusView struct {
	FeatureID string
	Request   string
	Public    domain.Public
	Phase     string
	Next      string
	PlanRev   int
	Progress  string   // "lote N de M" while batches run
	Failure   string   // what failed last, empty when nothing did
	Decisions []string // decisions of the plan and the design
	Repos     []string // "name → branch (checkout)" once the repos have a branch
	Paused    bool
}

// RepoLines is one line per repo of the feature that already has a branch: "name → branch (checkout)".
// A feature without repos or branches gives none.
func RepoLines(f domain.Feature) []string {
	var out []string
	for _, r := range f.Repos {
		if r.Branch == "" {
			continue
		}
		dir := r.Checkout
		if dir == "" {
			dir = r.Root
		}
		name := r.Name
		if name == "" {
			name = "repo"
		}
		out = append(out, name+" → "+r.Branch+" ("+dir+")")
	}
	return out
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
		Next:      nextSituation(f),
		PlanRev:   f.PlanRev,
		Progress:  batchProgress(f),
		Failure:   lastFailure(f),
		Decisions: featureDecisions(f),
		Repos:     RepoLines(f),
		Paused:    f.Paused,
	}, nil
}

// nextSituation says what comes next for the user: the main-thread step when there is one,
// otherwise what the feature is doing, never an empty string.
func nextSituation(f domain.Feature) string {
	if l := f.Next.Label(); l != "" && f.Next.MainThread() {
		return l
	}
	if l := f.State.Phase.Label(); l != "" {
		return "nada para el hilo principal (" + l + ")"
	}
	return "nada para el hilo principal"
}

// activeIn is the active feature of the repo dir belongs to.
func (h Hooks) activeIn(ctx context.Context, dir string) (domain.Feature, bool, error) {
	if h.Store == nil || h.Repos == nil {
		return domain.Feature{}, false, nil
	}
	f, ok := h.featureFor(ctx, HookEvent{ProjectDir: dir})
	return f, ok, nil
}
