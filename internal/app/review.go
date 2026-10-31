package app

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/review"
)

// reviewLenses is the lens set of a project: embedded < user < repo.
func (h Hooks) reviewLenses(dir string) []review.Lens {
	layers := [][]review.Lens{review.Builtin()}
	if h.Lenses != nil {
		u, _ := h.Lenses.User()
		r, _ := h.Lenses.Repo(dir)
		layers = append(layers, u, r)
	}
	return review.Merge(layers...)
}

// reviewWarning is the Spanish warning about invalid lens files of the user and the repo.
func (h Hooks) reviewWarning(dir string) string {
	if h.Lenses == nil {
		return ""
	}
	var ws []string
	if _, w := h.Lenses.User(); w != "" {
		ws = append(ws, w)
	}
	if _, w := h.Lenses.Repo(dir); w != "" {
		ws = append(ws, w)
	}
	return strings.Join(ws, "; ")
}

// reviewPath is the repo-relative path a plan change points at, without line or "(new)".
func reviewPath(at string) string {
	p := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(at), "(new)"))
	return filepath.ToSlash(changePath(p))
}

// reviewFiles reads the files with their current content; a file that cannot be read counts as deleted.
func (h Hooks) reviewFiles(dir string, paths []string) []review.File {
	var tree interface {
		Read(path string) ([]byte, bool)
	}
	if h.Trees != nil {
		tree = h.Trees.Tree(dir)
	}
	out := make([]review.File, 0, len(paths))
	for _, p := range paths {
		f := review.File{Path: p}
		if tree != nil {
			if b, ok := tree.Read(p); ok {
				f.Content = string(b)
			} else {
				f.Deleted = true
			}
		}
		out = append(out, f)
	}
	return out
}

// raisePlanRisk lifts each batch's risk by what its files show and records why (nxy sets it,
// whatever the planner wrote).
func (h Hooks) raisePlanRisk(dir string, p *domain.Plan) {
	lenses := h.reviewLenses(dir)
	for i := range p.Batches {
		bt := &p.Batches[i]
		var paths []string
		for _, c := range bt.Change {
			if rp := reviewPath(c.At); rp != "" {
				paths = append(paths, rp)
			}
		}
		risk, why := review.Raise(bt.Risk, review.Select(lenses, h.reviewFiles(dir, paths)))
		bt.Risk, bt.RiskWhy = risk, why
	}
}

// prepareReview decides the depth of the review of a finished feature: the risk of the plan
// raised again with the real diff; low risk gets one quick base pass, medium and high the lenses
// the changed files select (base always).
func (h Hooks) prepareReview(ctx context.Context, f domain.Feature) domain.Review {
	paths := planChangePaths(f)
	for i, p := range paths {
		paths[i] = reviewPath(p)
	}
	if h.Changes != nil {
		if c, err := h.Changes.Changed(ctx, f.ProjectDir, f.Base); err == nil {
			paths = nil
			for _, p := range c {
				paths = append(paths, filepath.ToSlash(p))
			}
		}
	}
	planned := domain.RiskLow
	if f.Plan != nil {
		for _, b := range f.Plan.Batches {
			planned = review.Max(planned, b.Risk)
		}
	}
	sel := review.Select(h.reviewLenses(f.ProjectDir), h.reviewFiles(f.ProjectDir, paths))
	risk, why := review.Raise(planned, sel)
	rv := domain.Review{Risk: risk, RiskWhy: why, Quick: risk == domain.RiskLow, Warning: h.reviewWarning(f.ProjectDir)}
	for _, s := range sel {
		if rv.Quick && !s.Lens.Always {
			continue
		}
		rv.Lenses = append(rv.Lenses, domain.LensRun{Name: s.Lens.Name, Title: s.Lens.Title, Files: s.Files})
	}
	return rv
}

// ensureReview prepares and stores the review of a feature in reviewing that has none (the process
// died between the transition and its preparation); any hook that reads the feature calls it.
func (h Hooks) ensureReview(ctx context.Context, f domain.Feature) domain.Feature {
	if f.State.Phase != domain.PhaseReviewing || f.Review != nil {
		return f
	}
	rv := h.prepareReview(ctx, f)
	if nf, _, err := h.mutate(ctx, f.ID, func(g *domain.Feature) bool {
		if g.State.Phase != domain.PhaseReviewing || g.Review != nil {
			return false
		}
		g.Review = &rv
		return true
	}); err == nil {
		f = nf
	}
	return f
}

// afterAdvance does the work a transition leaves for nxy itself: preparing the review when the
// feature enters reviewing, writing the full review document at the findings checkpoint and
// cleaning up when it closes. It runs after the transition, never inside it.
func (h Hooks) afterAdvance(ctx context.Context, f domain.Feature, acts []domain.Action) domain.Feature {
	f = h.ensureReview(ctx, f)
	for _, a := range acts {
		switch {
		case a.Kind == domain.AskFindings && h.ReviewFiles != nil && f.Review != nil:
			path, err := h.ReviewFiles.Write(f.ID, review.Markdown(f))
			if err != nil || path == "" {
				continue
			}
			if nf, ok, merr := h.mutate(ctx, f.ID, func(g *domain.Feature) bool {
				if g.Review == nil {
					return false
				}
				g.Review.FullPath = path
				return true
			}); merr == nil && ok {
				f = nf
			}
		case a.Kind == domain.Close && f.State.Outcome == domain.OutcomeSuccess:
			if h.Viewer != nil {
				_ = h.Viewer.Discard(f.ID)
			}
			if h.ReviewFiles != nil {
				_ = h.ReviewFiles.Discard(f.ID)
			}
		}
	}
	return f
}
