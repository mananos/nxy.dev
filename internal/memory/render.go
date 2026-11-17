package memory

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// maxLine is the longest text shown on one line of a listing.
const maxLine = 120

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > maxLine {
		return string(r[:maxLine-3]) + "..."
	}
	return s
}

func age(now time.Time, at int64) string {
	d := now.Sub(ms(at))
	days := int(d.Hours() / 24)
	switch {
	case days <= 0:
		return "hoy"
	case days == 1:
		return "hace 1 día"
	}
	return fmt.Sprintf("hace %d días", days)
}

// times is for what the planner reads (RenderRelated); vecesES is the same count for a person.
func times(n int) string {
	if n == 1 {
		return "1 time"
	}
	return fmt.Sprintf("%d times", n)
}

func vecesES(n int) string {
	if n == 1 {
		return "1 vez"
	}
	return fmt.Sprintf("%d veces", n)
}

// RenderShow lists the rules of a repo and the learnings not yet promoted. Learnings
// seen more than once come first: they are the candidates for `nxy mem promote`.
func RenderShow(rules []Rule, learnings []Learning, now time.Time) string {
	var b strings.Builder
	if len(rules) == 0 {
		b.WriteString("todavía no hay reglas para este repo\n")
	} else {
		fmt.Fprintf(&b, "Reglas (%d)\n", len(rules))
		total := 0
		for _, r := range rules {
			total += len([]rune(r.Text))
			fmt.Fprintf(&b, "%s · %s\n", r.Short, oneLine(r.Text))
			fmt.Fprintf(&b, "   resuelve: %s · %s · entregada %d · citada %d", oneLine(r.Question), r.Origin, r.Deliveries, r.Citations)
			if NeedsReview(r, now) {
				b.WriteString(" · para revisar")
			}
			b.WriteString("\n")
		}
		if total > DispatchBudget {
			fmt.Fprintf(&b, "demasiadas reglas para un despacho (%d caracteres > %d): revisalas\n", total, DispatchBudget)
		}
	}
	if len(learnings) > 0 {
		ls := append([]Learning(nil), learnings...)
		sort.SliceStable(ls, func(i, j int) bool { return ls[i].Seen > ls[j].Seen })
		fmt.Fprintf(&b, "\nAprendizajes (%d)\n", len(ls))
		for _, l := range ls {
			fmt.Fprintf(&b, "%s · %s · visto %s", l.Short, oneLine(l.Text), vecesES(l.Seen))
			if l.Seen > 1 {
				fmt.Fprintf(&b, " · candidato: nxy mem promote %s --question \"...\"", l.Short)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// RenderRelated lists the closed features and learnings that touch the files asked about.
func RenderRelated(features []ClosedFeature, learnings []Learning) string {
	if len(features) == 0 && len(learnings) == 0 {
		return "nothing related to those files\n"
	}
	var b strings.Builder
	if len(features) > 0 {
		fmt.Fprintf(&b, "Closed features (%d)\n", len(features))
		for _, f := range features {
			fmt.Fprintf(&b, "%s · %s\n", f.ID, oneLine(f.Title))
		}
	}
	if len(learnings) > 0 {
		if len(features) > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "Learnings (%d)\n", len(learnings))
		for _, l := range learnings {
			fmt.Fprintf(&b, "%s · %s · seen %s\n", l.Short, oneLine(l.Text), times(l.Seen))
		}
	}
	return b.String()
}

// RenderSearch lists search hits.
func RenderSearch(hits []Hit) string {
	if len(hits) == 0 {
		return "sin resultados\n"
	}
	var b strings.Builder
	for _, h := range hits {
		ref := h.Short
		if ref == "" {
			ref = h.ID
		}
		if h.Repo == GlobalRepo && h.Kind != "feature" {
			fmt.Fprintf(&b, "%s · %s · global · %s\n", h.Kind, ref, oneLine(h.Text))
			continue
		}
		fmt.Fprintf(&b, "%s · %s · %s\n", h.Kind, ref, oneLine(h.Text))
	}
	return b.String()
}

// RenderGlobal lists the global rules and learnings; "" when there are none.
func RenderGlobal(rules []Rule, learnings []Learning, now time.Time) string {
	if len(rules) == 0 && len(learnings) == 0 {
		return ""
	}
	var b strings.Builder
	if len(rules) > 0 {
		fmt.Fprintf(&b, "Reglas globales (%d)\n", len(rules))
		for _, r := range rules {
			roles := "todos"
			if len(r.Roles) > 0 {
				roles = strings.Join(r.Roles, ", ")
			}
			fmt.Fprintf(&b, "%s · %s\n", r.Short, oneLine(r.Text))
			fmt.Fprintf(&b, "   resuelve: %s · roles: %s · entregada %d · citada %d", oneLine(r.Question), roles, r.Deliveries, r.Citations)
			if NeedsReview(r, now) {
				b.WriteString(" · para revisar")
			}
			b.WriteString("\n")
		}
	}
	if len(learnings) > 0 {
		if len(rules) > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "Aprendizajes globales (%d)\n", len(learnings))
		for _, l := range learnings {
			fmt.Fprintf(&b, "%s · %s · promover: nxy mem promote %s --question \"...\"\n", l.Short, oneLine(l.Text), l.Short)
		}
	}
	return b.String()
}

func diffLine(b *strings.Builder, label string, counts [6]int) {
	fmt.Fprintf(b, "%s: nuevos %d, cambiados %d, borrados %d, unificados %d, salteados %d", label, counts[0], counts[1], counts[2], counts[3], counts[4])
	if counts[5] > 0 {
		fmt.Fprintf(b, ", contadores actualizados %d", counts[5])
	}
	b.WriteString("\n")
}

// kindES says what an import does to a row, in the gender of its noun.
func kindES(k ChangeKind, fem bool) string {
	var w string
	switch k {
	case New:
		w = "nuev"
	case Changed:
		w = "cambiad"
	case Folded:
		w = "unificad"
	default:
		return string(k)
	}
	if fem {
		return w + "a"
	}
	return w + "o"
}

func tally[T any](cs []Change[T], text func(T) string, lines *[]string, noun string, fem bool) [6]int {
	var c [6]int
	for _, ch := range cs {
		switch {
		case ch.Kind == Skipped:
			c[4]++
			if ch.Counters {
				c[5]++
			}
			continue
		case ch.Kind == Folded:
			c[3]++
		case ch.Deleted:
			c[2]++
		case ch.Kind == New:
			c[0]++
		case ch.Kind == Changed:
			c[1]++
		}
		what := kindES(ch.Kind, fem)
		if ch.Deleted && ch.Kind != Folded {
			if fem {
				what += " (borrada)"
			} else {
				what += " (borrado)"
			}
		}
		line := fmt.Sprintf("  %s %s: %s", noun, what, oneLine(text(ch.Row)))
		if ch.ReassignShort {
			line += " (referencia corta reasignada)"
		}
		*lines = append(*lines, line)
	}
	return c
}

// RenderDiff describes what importing a bundle does. apply false adds the dry-run notice.
func RenderDiff(d Diff, apply bool) string {
	var b strings.Builder
	var lines []string
	diffLine(&b, "reglas", tally(d.Rules, func(r Rule) string { return r.Text }, &lines, "regla", true))
	diffLine(&b, "features", tally(d.Features, func(f ClosedFeature) string { return f.Title }, &lines, "feature", true))
	diffLine(&b, "aprendizajes", tally(d.Learnings, func(l Learning) string { return l.Text }, &lines, "aprendizaje", false))
	diffLine(&b, "notas", tally(d.Notes, func(n Note) string { return n.Title }, &lines, "nota", true))
	if len(lines) > 0 {
		b.WriteString(strings.Join(lines, "\n"))
		b.WriteString("\n")
	}
	switch {
	case d.Writes() == 0:
		b.WriteString("no hay nada para importar\n")
	case !apply:
		b.WriteString("simulacro: no escribí nada, corré con --apply\n")
	default:
		fmt.Fprintf(&b, "importé %d cambios\n", d.Writes())
	}
	return b.String()
}

// RenderNotes lists temporary notes with their age and reference.
func RenderNotes(notes []Note, now time.Time) string {
	if len(notes) == 0 {
		return "no hay notas para esta carpeta\n"
	}
	var b strings.Builder
	for _, n := range notes {
		fmt.Fprintf(&b, "%s · %s · %s\n", n.Short, oneLine(n.Title), age(now, n.UpdatedAt))
	}
	return b.String()
}
