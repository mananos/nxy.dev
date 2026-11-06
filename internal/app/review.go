package app

import (
	"context"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/flow"
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
	planned := domain.RiskLow
	if f.Plan != nil {
		for _, b := range f.Plan.Batches {
			planned = review.Max(planned, b.Risk)
		}
	}
	// Lenses are chosen per repo, with paths relative to it (so "**/api/**" does not catch a whole
	// repo called api), and put together by lens with "<name>/<path>" labels; the risk is the highest.
	multi := len(f.Repos) > 1
	var sel []review.Selected
	risk, why := planned, ""
	for _, r := range f.RepoList() {
		dir, base := repoDir(r), r.Base
		if dir == "" {
			dir = f.ProjectDir
		}
		if base == "" {
			base = f.Base
		}
		paths := planPathsOf(f, r.Name)
		for i, p := range paths {
			paths[i] = reviewPath(p)
		}
		if h.Changes != nil {
			if c, err := h.Changes.Changed(ctx, dir, base); err == nil {
				paths = nil
				for _, p := range c {
					paths = append(paths, filepath.ToSlash(p))
				}
			}
		}
		rsel := review.Select(h.reviewLenses(dir), h.reviewFiles(dir, paths))
		rrisk, rwhy := review.Raise(planned, rsel)
		if review.Max(risk, rrisk) != risk || (why == "" && rwhy != "") {
			risk, why = review.Max(risk, rrisk), rwhy
		}
		if !multi {
			sel = rsel
			continue
		}
		for _, s := range rsel {
			for i := range s.Files {
				s.Files[i] = labelPath(r.Name, s.Files[i], true)
			}
			j := slices.IndexFunc(sel, func(x review.Selected) bool { return x.Lens.Name == s.Lens.Name })
			if j < 0 {
				sel = append(sel, s)
			} else {
				sel[j].Files = append(sel[j].Files, s.Files...)
			}
		}
	}
	if !multi {
		risk, why = review.Raise(planned, sel)
	}
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
//
// It returns the feature and the actions of the step it stands in: when it looked for docs, the
// ones of the step after that (the docs question or the close), not CheckDocs.
func (h Hooks) afterAdvance(ctx context.Context, f domain.Feature, acts []domain.Action) (domain.Feature, []domain.Action) {
	f = h.ensureRules(ctx, h.ensureReview(ctx, f))
	if f.State.Phase == domain.PhaseDocsCheck {
		if nf, nacts, ok := h.checkDocs(ctx, f); ok {
			return nf, nacts
		}
		return f, acts
	}
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
			h.deleteFeatureNotes(ctx, f.ID)
			h.saveClosedFeature(ctx, f)
			if h.Viewer != nil {
				_ = h.Viewer.Discard(f.ID)
			}
			if h.ReviewFiles != nil {
				_ = h.ReviewFiles.Discard(f.ID)
			}
		}
	}
	return f, acts
}

// changedPaths are the files the feature changed since its base (git), or the ones its plan names.
func (h Hooks) changedPaths(ctx context.Context, f domain.Feature) []string {
	if h.Changes != nil {
		multi := len(f.Repos) > 1
		if !multi {
			r := f.RepoList()[0]
			dir, base := repoDir(r), r.Base
			if dir == "" {
				dir = f.ProjectDir
			}
			if base == "" {
				base = f.Base
			}
			if c, err := h.Changes.Changed(ctx, dir, base); err == nil {
				out := make([]string, 0, len(c))
				for _, p := range c {
					out = append(out, filepath.ToSlash(p))
				}
				return out
			}
		} else if rels, ok := h.changedByRepo(ctx, f); ok {
			var out []string
			for i, r := range f.Repos {
				for _, p := range rels[i] {
					out = append(out, labelPath(r.Name, p, true))
				}
			}
			return out
		}
	}
	paths := planChangePaths(f)
	for i, p := range paths {
		paths[i] = reviewPath(p)
	}
	return paths
}

// changedByRepo are the files each repo of f changed since its base, relative to the repo, in the
// order of f.Repos; ok is false when git could not answer for some repo.
func (h Hooks) changedByRepo(ctx context.Context, f domain.Feature) ([][]string, bool) {
	out := make([][]string, len(f.Repos))
	for i, r := range f.Repos {
		base := r.Base
		if base == "" {
			base = f.Base
		}
		c, err := h.Changes.Changed(ctx, repoDir(r), base)
		if err != nil {
			return nil, false
		}
		for _, p := range c {
			out[i] = append(out[i], filepath.ToSlash(p))
		}
	}
	return out, true
}

// findDocs looks for the docs that name what the feature changed. With several repos it searches
// each one by its own relative paths and labels the docs "<name>/<path>".
func (h Hooks) findDocs(ctx context.Context, f domain.Feature) domain.Docs {
	if len(f.Repos) <= 1 {
		dir := repoDir(f.RepoList()[0])
		if dir == "" {
			dir = f.ProjectDir
		}
		return h.Docs.Find(ctx, dir, h.changedPaths(ctx, f))
	}
	var found domain.Docs
	rels, ok := h.changedByRepo(ctx, f)
	for i, r := range f.Repos {
		var changed []string
		if ok {
			changed = rels[i]
		} else {
			changed = planPathsOf(f, r.Name)
			for j, p := range changed {
				changed[j] = reviewPath(p)
			}
		}
		d := h.Docs.Find(ctx, repoDir(r), changed)
		for _, t := range d.Terms {
			if !slices.Contains(found.Terms, t) {
				found.Terms = append(found.Terms, t)
			}
		}
		for _, df := range d.Docs {
			df.Path = labelPath(r.Name, df.Path, true)
			found.Docs = append(found.Docs, df)
		}
		if found.WikiDir == "" && d.WikiDir != "" {
			found.WikiDir = labelPath(r.Name, d.WikiDir, true)
		}
	}
	return found
}

// checkDocs is the CheckDocs step, done by nxy itself outside any transition: it looks for the
// docs that name what the feature changed, stores them and applies DocsChecked (no docs closes
// the feature, docs ask the user). ok is false when the transition did not happen.
func (h Hooks) checkDocs(ctx context.Context, f domain.Feature) (domain.Feature, []domain.Action, bool) {
	var found domain.Docs
	if h.Docs != nil && f.ProjectDir != "" {
		found = h.findDocs(ctx, f)
	}
	nf, acts, err := h.advanceSteps(ctx, f.ID, func(g *domain.Feature) ([]flow.Event, error) {
		if g.State.Phase != domain.PhaseDocsCheck {
			return nil, errSkipEvent // another hook handled it
		}
		if len(found.Docs) > 0 {
			g.Docs = &found
		}
		return []flow.Event{{Kind: flow.DocsChecked, Docs: len(found.Docs) > 0, Candidates: h.hasCandidates(*g)}}, nil
	})
	if err != nil {
		return f, nil, false
	}
	return nf, acts, true
}

// ensureDocs runs the docs check of a feature left in docs_check (the process died between the
// transition and the check); any hook that reads the feature calls it.
func (h Hooks) ensureDocs(ctx context.Context, f domain.Feature) domain.Feature {
	if f.State.Phase != domain.PhaseDocsCheck {
		return f
	}
	if nf, _, ok := h.checkDocs(ctx, f); ok {
		return nf
	}
	return f
}
