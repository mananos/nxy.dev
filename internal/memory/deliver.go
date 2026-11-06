package memory

import (
	"path"
	"slices"
	"strings"
)

// Packet is what one dispatch receives from the memory.
type Packet struct {
	Text       string   // the text to inject; empty when nothing was delivered
	Rules      []string // ULIDs of the rules delivered
	Learnings  []string // ULIDs of the learnings delivered
	Referenced []string // K<n> / A<n> of what was delivered
	// Origins says from which repo (by group name) each referenced id came; the same id can come from
	// two repos.
	Origins map[string][]string
}

// RulesFor returns the rules that apply to files: those without Paths always,
// those with a glob when it matches some file. The ones of the area go first.
func RulesFor(rules []Rule, files []string) []Rule {
	var area, general []Rule
	for _, r := range rules {
		if len(r.Paths) == 0 {
			general = append(general, r)
			continue
		}
		if matchesAny(r.Paths, files) {
			area = append(area, r)
		}
	}
	return append(area, general...)
}

func matchesAny(globs, files []string) bool {
	for _, g := range globs {
		for _, f := range files {
			if ok, err := path.Match(g, CleanPath(f)); err == nil && ok {
				return true
			}
		}
	}
	return false
}

type packItem struct {
	id, short, line string
	learning        bool
	repo            string
}

func shortOf(short, id string) string {
	if short != "" {
		return short
	}
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// Pack renders rules and then learnings in full ("K3: text", "A7: text"), never
// only the title, within budget chars. What does not fit is named in one line.
func Pack(rules []Rule, learnings []Learning, budget int) Packet {
	return PackRepos([]RepoRules{{Rules: rules, Learnings: learnings}}, budget)
}

// RepoRules are the rules and learnings of one repo of a feature.
type RepoRules struct {
	Name      string // the repo name shown in front of each line when there are several groups
	Rules     []Rule
	Learnings []Learning
}

// PackRepos is Pack over the rules of several repos: every rule first, then every learning, each
// line starting with "[name] " when there is more than one group. One group is the same as Pack.
func PackRepos(groups []RepoRules, budget int) Packet {
	prefix := func(name string) string {
		if len(groups) > 1 && name != "" {
			return "[" + name + "] "
		}
		return ""
	}
	var items []packItem
	for _, g := range groups {
		for _, r := range g.Rules {
			s := shortOf(r.Short, r.ID)
			items = append(items, packItem{r.ID, s, prefix(g.Name) + s + ": " + strings.TrimSpace(r.Text), false, g.Name})
		}
	}
	for _, g := range groups {
		for _, l := range g.Learnings {
			s := shortOf(l.Short, l.ID)
			items = append(items, packItem{l.ID, s, prefix(g.Name) + s + ": " + strings.TrimSpace(l.Text), true, g.Name})
		}
	}
	var kept []packItem
	var skipped []string
	used := 0
	for _, it := range items {
		n := len(it.line)
		if len(kept) > 0 {
			n++ // newline
		}
		if used+n > budget {
			skipped = append(skipped, it.short)
			continue
		}
		used += n
		kept = append(kept, it)
	}
	notShown := func() string {
		return "not shown (budget): " + strings.Join(skipped, ", ") + " - run nxy mem show"
	}
	if len(skipped) > 0 {
		// make room for the line, dropping from the end
		for len(kept) > 0 && used+1+len(notShown()) > budget {
			last := kept[len(kept)-1]
			kept = kept[:len(kept)-1]
			used -= len(last.line)
			if len(kept) > 0 {
				used--
			}
			skipped = append([]string{last.short}, skipped...)
		}
	}
	var p Packet
	lines := make([]string, 0, len(kept)+1)
	for _, it := range kept {
		lines = append(lines, it.line)
		p.Referenced = append(p.Referenced, it.short)
		if p.Origins == nil {
			p.Origins = map[string][]string{}
		}
		if !slices.Contains(p.Origins[it.short], it.repo) {
			p.Origins[it.short] = append(p.Origins[it.short], it.repo)
		}
		if it.learning {
			p.Learnings = append(p.Learnings, it.id)
		} else {
			p.Rules = append(p.Rules, it.id)
		}
	}
	if len(skipped) > 0 {
		lines = append(lines, notShown())
	}
	p.Text = strings.Join(lines, "\n")
	if len(p.Text) > budget {
		p.Text = p.Text[:budget]
	}
	return p
}
