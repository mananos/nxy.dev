package app

import (
	"context"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/memory"
	"github.com/mananos/nxy.dev/internal/repos"
)

// noRulesYet is what the planner reads when the memory is on but holds no rule of the repo.
const noRulesYet = "(no repo rules yet)"

// memoryFor builds what one dispatch receives from the memory and counts the delivery of its
// rules. The planner gets every rule; the others the rules that apply to files plus the learnings
// attached to them. A learning whose files are all missing in the checkout is not delivered (and not
// marked obsolete: the files may live on another branch).
// Without Hooks.Memory (or a repo) the packet is empty.
func (h Hooks) memoryFor(ctx context.Context, f domain.Feature, role string, files []string) memory.Packet {
	if h.Memory == nil || h.Repos == nil || f.ProjectDir == "" {
		return memory.Packet{}
	}
	mrs := h.memRepos(f)
	// An error loading the global rules only drops them.
	var globals []memory.Rule
	if mr := memRole(role); mr != "" {
		if gl, err := h.Memory.GlobalRules(ctx); err == nil {
			globals = gl
		}
	}
	if role == rolePlanner || role == roleMain {
		// The planner runs before the plan names its repos: it gets the rules of the session repo (or
		// of every repo of the feature once it has them).
		var groups []memory.RepoRules
		for _, r := range mrs {
			rules, err := h.Memory.Rules(ctx, r.Key)
			if err != nil {
				return memory.Packet{}
			}
			groups = append(groups, memory.RepoRules{Name: r.Name, Rules: rules})
		}
		pkt := memory.PackWith(groups, memory.GlobalFor(globals, memRole(role), nil, true), memory.DispatchBudget)
		if pkt.Text == "" {
			pkt.Text = noRulesYet
		}
		h.countDeliveries(ctx, pkt)
		return pkt
	}
	// Each file goes to its repo; a repo without files delivers nothing (with one repo, its general
	// rules still go).
	rels := make([][]string, len(mrs))
	touched := make([]bool, len(mrs))
	for _, p := range files {
		if i, rel := h.splitLabel(f, mrs, p); i >= 0 {
			rels[i] = append(rels[i], rel)
			touched[i] = true
		}
	}
	if len(mrs) == 1 || !slices.Contains(touched, true) {
		touched[0] = true
	}
	var groups []memory.RepoRules
	for i, r := range mrs {
		if !touched[i] {
			continue
		}
		rules, err := h.Memory.Rules(ctx, r.Key)
		if err != nil {
			return memory.Packet{}
		}
		g := memory.RepoRules{Name: r.Name, Rules: memory.RulesFor(rules, rels[i])}
		if len(rels[i]) > 0 {
			if _, ls, err := h.Memory.Related(ctx, r.Key, rels[i]); err == nil {
				root := h.gitRootOf(r.Dir)
				for _, l := range ls {
					if l.ObsoleteAt != 0 || l.PromotedTo != "" {
						continue
					}
					if h.allGone(root, l.Files) {
						// Missing here may only mean another branch is checked out: skip, never obsolete.
						continue
					}
					g.Learnings = append(g.Learnings, l)
				}
			}
		}
		groups = append(groups, g)
	}
	var allRels []string
	for _, r := range rels {
		allRels = append(allRels, r...)
	}
	pkt := memory.PackWith(groups, memory.GlobalFor(globals, memRole(role), allRels, false), memory.DispatchBudget)
	h.countDeliveries(ctx, pkt)
	return pkt
}

// memRepo is a repo of a feature as the memory sees it.
type memRepo struct{ Name, Key, Dir string }

// memRepos are the repos of f with their memory key: the repo of the session folder when the
// feature has none yet (or an old one), otherwise one per repo of the feature.
func (h Hooks) memRepos(f domain.Feature) []memRepo {
	if len(f.Repos) == 0 {
		return []memRepo{{Key: h.Repos.Key(f.ProjectDir), Dir: f.ProjectDir}}
	}
	out := make([]memRepo, 0, len(f.Repos))
	for _, r := range f.Repos {
		dir := repoDir(r)
		if dir == "" {
			dir = f.ProjectDir
		}
		key := r.Key
		if key == "" {
			key = h.Repos.Key(dir)
		}
		out = append(out, memRepo{Name: r.Name, Key: key, Dir: dir})
	}
	return out
}

// memKeys are the distinct memory keys of the repos of f.
func (h Hooks) memKeys(f domain.Feature) []string {
	var out []string
	for _, r := range h.memRepos(f) {
		if !slices.Contains(out, r.Key) {
			out = append(out, r.Key)
		}
	}
	return out
}

// implementerMemory is memoryFor for an implementer dispatch; with several repos it also records
// which repo each delivered id came from, so a later `false: A3` can be placed.
func (h Hooks) implementerMemory(ctx context.Context, f domain.Feature, files []string) string {
	pkt := h.memoryFor(ctx, f, roleImplementer, files)
	if len(f.Repos) > 1 && h.Store != nil {
		_, _, _ = h.mutate(ctx, f.ID, func(g *domain.Feature) bool {
			g.Delivered = pkt.Origins
			return true
		})
	}
	return pkt.Text
}

// resolveFalse maps the `false:` references of an agent to the memory key each one applies to. An id
// only counts for the repo it was delivered from (origins); "[api] A3" / "api:A3" names the repo when
// the same id came from two. An id that stays ambiguous, or was not delivered, applies to none.
// With one repo every id is that repo's.
func resolveFalse(mrs []memRepo, refs []string, origins map[string][]string) map[string][]string {
	out := map[string][]string{}
	add := func(key, id string) {
		if !slices.Contains(out[key], id) {
			out[key] = append(out[key], id)
		}
	}
	for _, ref := range refs {
		name, id, ok := memory.SplitFalse(ref)
		if !ok {
			id = strings.ToUpper(strings.TrimSpace(ref))
		}
		if len(mrs) == 1 {
			add(mrs[0].Key, id)
			continue
		}
		from := origins[id]
		if name != "" {
			i := memRepoIndex(mrs, name)
			if i >= 0 && slices.Contains(from, name) {
				add(mrs[i].Key, id)
			}
			continue
		}
		if len(from) == 1 {
			if i := memRepoIndex(mrs, from[0]); i >= 0 {
				add(mrs[i].Key, id)
			}
		}
	}
	return out
}

// memRepoIndex is the index of the repo called name in mrs, or -1.
func memRepoIndex(mrs []memRepo, name string) int {
	for i, r := range mrs {
		if r.Name == name && name != "" {
			return i
		}
	}
	return -1
}

// placeFiles picks the repo a learning belongs to (the one of its first labeled file, else def) and
// returns its files relative to that repo; files of other repos are dropped.
func (h Hooks) placeFiles(f domain.Feature, mrs []memRepo, files []string, def int) (int, []string) {
	type part struct {
		i   int
		rel string
	}
	target := -1
	var parts []part
	for _, p := range files {
		i, rel := h.splitLabel(f, mrs, p)
		if i < 0 {
			i = def
		}
		if target < 0 {
			target = i
		}
		parts = append(parts, part{i, rel})
	}
	if target < 0 {
		return def, nil
	}
	var rels []string
	for _, x := range parts {
		if x.i == target {
			rels = append(rels, x.rel)
		}
	}
	return target, rels
}

// splitLabel finds the repo (index in repos) a path belongs to and the path relative to it. Paths of
// a feature with several repos (or a plan with `repos`) carry "<name>/"; otherwise the path is the
// first repo's. The index is -1 when a multi-repo path names no repo.
func (h Hooks) splitLabel(f domain.Feature, mrs []memRepo, p string) (int, string) {
	labeled := len(mrs) > 1 || (f.Plan != nil && len(f.Plan.Repos) > 0)
	if !labeled {
		return 0, p
	}
	names := make([]string, len(mrs))
	for i, r := range mrs {
		names[i] = r.Name
	}
	if name, rel, ok := repos.Split(p, names); ok {
		for i, r := range mrs {
			if r.Name == name {
				return i, rel
			}
		}
	}
	if len(mrs) == 1 {
		return 0, p
	}
	return -1, p
}

func (h Hooks) countDeliveries(ctx context.Context, pkt memory.Packet) {
	if len(pkt.Rules) > 0 {
		_ = h.Memory.CountDeliveries(ctx, pkt.Rules)
	}
}

// gitRootOf is the first ancestor of dir that holds a .git (the paths of a learning are relative to
// it, and Claude Code may be open in a subfolder); dir itself when there is none.
func (h Hooks) gitRootOf(dir string) string {
	if h.Files == nil {
		return dir
	}
	for d := dir; ; {
		if h.Files.Exists(filepath.Join(d, ".git")) {
			return d
		}
		up := filepath.Dir(d)
		if up == d {
			return dir
		}
		d = up
	}
}

// allGone reports whether the learning names files and none of them exists any more.
func (h Hooks) allGone(root string, files []string) bool {
	if h.Files == nil || len(files) == 0 {
		return false
	}
	for _, p := range files {
		if h.Files.Exists(filepath.Join(root, filepath.FromSlash(p))) {
			return false
		}
	}
	return true
}

// batchFiles are the files a batch changes, as repo-relative paths.
func batchFiles(b domain.Batch) []string {
	var out []string
	for _, c := range b.Change {
		if p := reviewPath(c.At); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// findingFiles are the files of the findings the user chose to fix.
func findingFiles(f domain.Feature) []string {
	if f.Review == nil {
		return nil
	}
	chosen := map[string]bool{}
	for _, id := range f.Review.Chosen {
		chosen[id] = true
	}
	var out []string
	for _, fd := range f.Review.Findings {
		if chosen[fd.ID] && fd.File != "" {
			out = append(out, filepath.ToSlash(fd.File))
		}
	}
	return out
}
