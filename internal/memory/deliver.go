package memory

import (
	"path"
	"strings"
)

// Packet is what one dispatch receives from the memory.
type Packet struct {
	Text       string   // the text to inject; empty when nothing was delivered
	Rules      []string // ULIDs of the rules delivered
	Learnings  []string // ULIDs of the learnings delivered
	Referenced []string // K<n> / A<n> of what was delivered
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
	var items []packItem
	for _, r := range rules {
		s := shortOf(r.Short, r.ID)
		items = append(items, packItem{r.ID, s, s + ": " + strings.TrimSpace(r.Text), false})
	}
	for _, l := range learnings {
		s := shortOf(l.Short, l.ID)
		items = append(items, packItem{l.ID, s, s + ": " + strings.TrimSpace(l.Text), true})
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
