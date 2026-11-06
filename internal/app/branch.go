package app

import (
	"context"
	"fmt"
	"runtime"
	"strings"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/flow"
	"github.com/mananos/nxy.dev/internal/plan"
	"github.com/mananos/nxy.dev/internal/repos"
	"github.com/mananos/nxy.dev/internal/worktree"
)

// maxBaseOptions is how many bases a base question offers.
const maxBaseOptions = 3

// branchRepos is the repos of f with a name (a one-repo feature has none stored: it is named by its folder).
func branchRepos(f domain.Feature) []domain.FeatureRepo {
	rs := append([]domain.FeatureRepo(nil), f.RepoList()...)
	for i := range rs {
		if rs[i].Name == "" {
			rs[i].Name = repos.Names([]string{rs[i].Root}, runtime.GOOS == "windows")[rs[i].Root]
		}
	}
	return rs
}

func repoDir(r domain.FeatureRepo) string {
	if r.Checkout != "" {
		return r.Checkout
	}
	return r.Root
}

// branchOffer is the branch question of f: nil when nothing can be asked (no Branches port, or none of
// its repos is a git repo with a branch), in which case the feature starts its first batch where it is.
func (h Hooks) branchOffer(ctx context.Context, f domain.Feature) *domain.BranchOffer {
	if h.Branches == nil {
		return nil
	}
	o := &domain.BranchOffer{}
	var pool []string
	for _, r := range branchRepos(f) {
		info := h.Branches.List(ctx, repoDir(r))
		if len(info.Branches) == 0 {
			continue
		}
		if pool == nil {
			pool = info.Branches
		}
		bases := info.Branches
		if info.Default != "" {
			bases = append([]string{info.Default}, info.Branches...)
		}
		ro := domain.BranchRepoOffer{Name: r.Name, Current: info.Current, Bases: worktree.BaseOptions(bases, maxBaseOptions)}
		if len(ro.Bases) == 1 {
			ro.Base = ro.Bases[0] // nothing to choose
		}
		o.Repos = append(o.Repos, ro)
	}
	if len(o.Repos) == 0 {
		return nil
	}
	title := f.Request
	if f.Plan != nil && f.Plan.Title != "" {
		title = f.Plan.Title
	}
	o.Suggest = worktree.SuggestBranch(title, pool)
	if cur := o.Repos[0].Current; cur != "" && len(o.Suggest) < 3 && !hasString(o.Suggest, cur) {
		o.Suggest = append(o.Suggest, cur)
	}
	for _, fallback := range []string{"feature/" + f.ID, "feature/nxy_" + f.ID} {
		if len(o.Suggest) < 2 && !hasString(o.Suggest, fallback) {
			o.Suggest = append(o.Suggest, fallback)
		}
	}
	return o
}

func hasString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// hasBranchAnswer is true when answers has an answer to a question of the branch step.
func hasBranchAnswer(f domain.Feature, answers map[string]string) bool {
	for q := range answers {
		if q == plan.BranchQuestion(f.ID) || strings.HasPrefix(q, plan.BasePrefix(f.ID)) {
			return true
		}
	}
	return false
}

// branchResult is what the git of the branch step did for one repo.
type branchResult struct {
	Checkout string
	Branch   string
	Base     string // HEAD sha once on the branch
	Note     string
	worktree string // path of the worktree it created, for undoing
	root     string
}

// answerBranch collects the answers to the branch step. While one is missing it keeps what it has
// and asks the rest; with all of them it makes the branch (in the session's folder, or a worktree per
// repo) and starts the first batch. A git failure asks again with git's words.
func (h Hooks) answerBranch(ctx context.Context, f domain.Feature, answers map[string]string) HookResponse {
	retry := HookResponse{Next: &Next{Feature: f, Note: "nxy could not record your answer; ask again"}}
	if f.BranchOffer == nil {
		return retry
	}
	o := *f.BranchOffer
	o.Repos = append([]domain.BranchRepoOffer(nil), o.Repos...)
	o.Error = ""
	if a := strings.TrimSpace(answers[plan.BranchQuestion(f.ID)]); a != "" {
		o.Branch = a
	}
	complete := o.Branch != ""
	for i := range o.Repos {
		if a := strings.TrimSpace(answers[plan.BaseQuestion(f.ID, o.Repos[i].Name)]); a != "" {
			o.Repos[i].Base = a
		}
		complete = complete && o.Repos[i].Base != ""
	}
	store := func(note string) HookResponse {
		nf, _, err := h.mutate(ctx, f.ID, func(g *domain.Feature) bool {
			if g.State.Phase != domain.PhaseBranch {
				return false
			}
			g.BranchOffer = &o
			return true
		})
		if err != nil || nf.ID == "" {
			return retry
		}
		return HookResponse{Next: &Next{Feature: nf, Note: note}}
	}
	if !complete {
		return store("")
	}
	results, err := h.makeBranches(ctx, f, o)
	if err != nil {
		o.Branch = ""
		for i := range o.Repos {
			if len(o.Repos[i].Bases) != 1 {
				o.Repos[i].Base = ""
			}
		}
		o.Error = err.Error()
		return store("")
	}
	worktrees := f.WantsWorktrees()
	nf, _, err := h.advanceWith(ctx, f.ID, flow.Event{Kind: flow.BranchReady}, func(x *domain.Feature) error {
		if x.State.Phase != domain.PhaseBranch {
			return errNoChange
		}
		rs := branchRepos(*x)
		for i := range rs {
			if res, ok := results[rs[i].Name]; ok {
				if res.Checkout != "" {
					rs[i].Checkout = res.Checkout
				}
				rs[i].Branch, rs[i].Base = res.Branch, res.Base
			}
		}
		x.Repos = rs
		x.Base = rs[0].Base
		if worktrees {
			x.InWorktree = true
			x.Holds = nil
			for _, r := range rs {
				co := repoDir(r)
				if h.Repos != nil {
					co = h.Repos.Checkout(co)
				}
				if !hasString(x.Holds, co) {
					x.Holds = append(x.Holds, co)
				}
			}
		}
		x.BranchOffer = nil
		return nil
	})
	if err != nil {
		h.undoWorktrees(ctx, results)
		return retry
	}
	var notes []string
	for _, r := range branchRepos(f) {
		if res, ok := results[r.Name]; ok && res.Note != "" {
			notes = append(notes, res.Note)
		}
	}
	note := ""
	if len(notes) > 0 {
		note = "Tell the user, briefly: " + strings.Join(notes, " ")
	}
	return HookResponse{Next: &Next{Feature: nf, Note: note}}
}

// makeBranches runs the git of the branch step for every repo of the offer, by repo name. With worktrees
// a failure removes the ones created before it.
func (h Hooks) makeBranches(ctx context.Context, f domain.Feature, o domain.BranchOffer) (map[string]branchResult, error) {
	out := map[string]branchResult{}
	worktrees := f.WantsWorktrees()
	multi := len(o.Repos) > 1
	for _, r := range branchRepos(f) {
		var base string
		offered := false
		for _, ro := range o.Repos {
			if ro.Name == r.Name {
				base, offered = ro.Base, true
			}
		}
		if !offered {
			continue
		}
		res, err := h.makeBranch(ctx, r, o.Branch, base, worktrees)
		if err != nil {
			h.undoWorktrees(ctx, out)
			if multi {
				return nil, fmt.Errorf("%s: %w", r.Name, err)
			}
			return nil, err
		}
		out[r.Name] = res
	}
	return out, nil
}

func (h Hooks) makeBranch(ctx context.Context, r domain.FeatureRepo, branch, base string, worktrees bool) (branchResult, error) {
	dir := repoDir(r)
	res := branchResult{Branch: branch, root: r.Root}
	if worktrees {
		added, err := h.Branches.AddWorktree(ctx, r.Root, branch, base)
		if err != nil {
			return res, err
		}
		res.worktree, res.Branch = added.Path, added.Branch
		res.Checkout = added.Path // the real path, for display; Holds get the normalized key
		how := "creada desde " + base
		if !added.Created {
			how = "ya existía"
		}
		res.Note = fmt.Sprintf("%s: worktree en %s (rama %s, %s). Para abrirlo: %s.", r.Name, added.Path, added.Branch, how, h.Branches.OpenCommand(added.Path))
		if h.Branches.MissingDeps(added.Path) {
			res.Note += fmt.Sprintf(" Ojo: %s tiene package.json pero no node_modules; el primer Accept puede fallar hasta instalar las dependencias (nxy no las instala).", added.Path)
		}
		dir = added.Path
	} else {
		cur := h.Branches.List(ctx, dir).Current
		switch {
		case cur == branch:
			res.Note = fmt.Sprintf("%s: ya estabas en la rama %s, sigue ahí.", r.Name, branch)
		default:
			sw, err := h.Branches.Switch(ctx, dir, branch, base)
			if err != nil {
				return res, err
			}
			if sw.Created {
				res.Note = fmt.Sprintf("%s: rama %s creada desde %s en la carpeta de la sesión (%s).", r.Name, sw.Branch, base, sw.Command)
			} else {
				res.Note = fmt.Sprintf("%s: pasó a la rama existente %s (%s).", r.Name, sw.Branch, sw.Command)
			}
		}
	}
	if h.Changes != nil {
		res.Base = h.Changes.Head(dir)
	}
	return res, nil
}

// undoWorktrees removes the worktrees a failed branch step created (the branches stay).
func (h Hooks) undoWorktrees(ctx context.Context, results map[string]branchResult) {
	for _, r := range results {
		if r.worktree != "" {
			_ = h.Branches.RemoveWorktree(ctx, r.root, r.worktree)
		}
	}
}
