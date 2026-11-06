package app

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/plan"
	"github.com/mananos/nxy.dev/internal/repos"
	"github.com/mananos/nxy.dev/internal/review"
)

// planRepo is one repo of a plan once nxy resolved it against the disk.
type planRepo struct {
	Name string
	Root string
	Role string
}

// resolvePlanRepos finds the git root of each repo the plan declares (paths are relative to the
// session folder) and names them; "." is the session's own repo. Without declared repos it returns
// nil. A path that is not a git repo refuses the plan.
func (h Hooks) resolvePlanRepos(f domain.Feature, p *domain.Plan) ([]planRepo, []plan.Problem) {
	if len(p.Repos) == 0 {
		return nil, nil
	}
	var probs []plan.Problem
	roots := make([]string, len(p.Repos))
	for i, r := range p.Repos {
		path := strings.TrimSpace(r.Path)
		if path == "" {
			continue // ValidateRepos reports the empty path
		}
		abs := path
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(f.ProjectDir, path)
		}
		root, ok := "", false
		if h.Repos != nil {
			root, ok = h.Repos.Root(abs)
		}
		if !ok || root == "" {
			probs = append(probs, plan.Problem{Where: fmt.Sprintf("repo %d", i+1), Msg: fmt.Sprintf("repo %s: no es un repo git; el plan no se guarda", path)})
			continue
		}
		roots[i] = root
	}
	if len(probs) > 0 {
		return nil, probs
	}
	var list []string
	for _, r := range roots {
		if r != "" {
			list = append(list, r)
		}
	}
	names := repos.Names(list, runtime.GOOS == "windows")
	out := make([]planRepo, len(p.Repos))
	for i, r := range p.Repos {
		out[i] = planRepo{Name: names[roots[i]], Root: roots[i], Role: r.Role}
		p.Repos[i].Name = out[i].Name
	}
	return out, nil
}

// planRepoNames is the names of the resolved repos, in the plan's order.
func planRepoNames(rs []planRepo) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Name
	}
	return out
}

// tagBatchRepos sets Batch.Repo from the repo name that starts the path of each change.
func tagBatchRepos(p *domain.Plan, names []string) {
	for i := range p.Batches {
		p.Batches[i].Repo = ""
		for _, c := range p.Batches[i].Change {
			if name, _, ok := repos.Split(c.At, names); ok {
				p.Batches[i].Repo = name
				break
			}
		}
	}
}

// planRunners sets the repo's own runners of every repo, once each, for the checkpoint.
func (h Hooks) planRunners(dirs []string, p *domain.Plan) {
	p.RepoRunners, p.RepoRunnersWarn = nil, ""
	if h.Runners == nil {
		return
	}
	var warns []string
	for _, dir := range dirs {
		rows, warn := h.Runners.Rows(dir)
		if warn != "" && !slices.Contains(warns, warn) {
			warns = append(warns, warn)
		}
		for _, r := range rows {
			if !r.Repo {
				continue
			}
			rr := domain.RepoRunner{Name: r.Name, Command: r.Command}
			if !slices.Contains(p.RepoRunners, rr) {
				p.RepoRunners = append(p.RepoRunners, rr)
			}
		}
	}
	p.RepoRunnersWarn = strings.Join(warns, "; ")
}

// raiseRepoRisk is raisePlanRisk for a multi-repo plan: each batch's files are read in the root of
// its repo, by their path inside it (the lens globs see the repo's own paths).
func (h Hooks) raiseRepoRisk(p *domain.Plan, rs []planRepo) {
	names := planRepoNames(rs)
	lenses := map[string][]review.Lens{}
	for i := range p.Batches {
		bt := &p.Batches[i]
		var root string
		for _, r := range rs {
			if r.Name == bt.Repo {
				root = r.Root
			}
		}
		if root == "" {
			continue
		}
		if _, ok := lenses[root]; !ok {
			lenses[root] = h.reviewLenses(root)
		}
		var paths []string
		for _, c := range bt.Change {
			_, rel, ok := repos.Split(c.At, names)
			if !ok {
				continue
			}
			if rp := reviewPath(rel); rp != "" {
				paths = append(paths, rp)
			}
		}
		risk, why := review.Raise(bt.Risk, review.Select(lenses[root], h.reviewFiles(root, paths)))
		bt.Risk, bt.RiskWhy = risk, why
	}
}

// planClashes lists the checkouts the plan needs that another feature holds.
func (h Hooks) planClashes(ctx context.Context, f domain.Feature, names, checkouts []string) []domain.RepoClash {
	var out []domain.RepoClash
	seen := map[string]bool{}
	for i, co := range checkouts {
		if seen[co] {
			continue
		}
		seen[co] = true
		other, found, err := h.Store.ActiveFeature(ctx, co)
		if err != nil || !found || other.ID == f.ID {
			continue
		}
		out = append(out, domain.RepoClash{Repo: names[i], Checkout: co, FeatureID: other.ID, Phase: string(other.State.Phase)})
	}
	return out
}

// clashLine is the line the planner gets when its plan was not saved because of a clash.
func clashLine(cs []domain.RepoClash) string {
	var parts []string
	for _, c := range cs {
		parts = append(parts, fmt.Sprintf("repo %s is held by feature %s", c.Repo, c.FeatureID))
	}
	return "plan not saved: " + strings.Join(parts, "; ") + "; reply exactly `plan blocked`, nxy asks the user"
}
