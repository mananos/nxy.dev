package app

import (
	"context"
	"path/filepath"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/memory"
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
	repo := h.Repos.Key(f.ProjectDir)
	rules, err := h.Memory.Rules(ctx, repo)
	if err != nil {
		return memory.Packet{}
	}
	var learnings []memory.Learning
	if role == rolePlanner {
		pkt := memory.Pack(rules, nil, memory.DispatchBudget)
		if pkt.Text == "" {
			pkt.Text = noRulesYet
		}
		h.countDeliveries(ctx, pkt)
		return pkt
	}
	rules = memory.RulesFor(rules, files)
	if len(files) > 0 {
		if _, ls, err := h.Memory.Related(ctx, repo, files); err == nil {
			root := h.gitRootOf(f.ProjectDir)
			for _, l := range ls {
				if l.ObsoleteAt != 0 || l.PromotedTo != "" {
					continue
				}
				if h.allGone(root, l.Files) {
					// Missing here may only mean another branch is checked out: skip, never obsolete.
					continue
				}
				learnings = append(learnings, l)
			}
		}
	}
	pkt := memory.Pack(rules, learnings, memory.DispatchBudget)
	h.countDeliveries(ctx, pkt)
	return pkt
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
