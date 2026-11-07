package legacy

import (
	"fmt"
	"strings"
)

// Counts is what an import saw and did; the numbers behind the summary the user reads.
type Counts struct {
	Conventions int `json:"conventions,omitempty"`
	Decisions   int `json:"decisions,omitempty"`
	Bugs        int `json:"bugs,omitempty"`
	Preferences int `json:"preferences,omitempty"`
	Other       int `json:"other,omitempty"` // memories of a type with no destination
	Learnings   int `json:"learnings,omitempty"`
	Repeated    int `json:"repeated,omitempty"` // learnings already in the repo, left as they were

	// The real short numbers (A<n>) of the learnings stored; set by the store, zero before.
	LearningFirst int `json:"learning_first,omitempty"`
	LearningLast  int `json:"learning_last,omitempty"`

	Notes        int `json:"notes,omitempty"`
	NotesExpired int `json:"notes_expired,omitempty"`
	NotesCut     int `json:"notes_cut,omitempty"`
	Features     int `json:"features,omitempty"`

	Superseded   int `json:"superseded,omitempty"`
	EdgesDropped int `json:"edges_dropped,omitempty"`
	Globals      int `json:"globals,omitempty"`
	Private      int `json:"private,omitempty"` // private learnings/archived, left out

	FilterRows     int `json:"filter_rows,omitempty"`
	FilterSessions int `json:"filter_sessions,omitempty"`
	FilterNoSess   int `json:"filter_no_session,omitempty"`
	FilterV2       int `json:"filter_v2_sessions,omitempty"`

	Reviews int `json:"reviews,omitempty"`
	Picks   int `json:"picks,omitempty"`
	Chosen  int `json:"chosen,omitempty"`
}

// Add sums two counts.
func (c Counts) Add(o Counts) Counts {
	return Counts{
		Conventions: c.Conventions + o.Conventions, Decisions: c.Decisions + o.Decisions,
		Bugs: c.Bugs + o.Bugs, Preferences: c.Preferences + o.Preferences, Other: c.Other + o.Other,
		Learnings: c.Learnings + o.Learnings, Repeated: c.Repeated + o.Repeated,
		LearningFirst: firstNonZero(c.LearningFirst, o.LearningFirst), LearningLast: max(c.LearningLast, o.LearningLast),
		Notes: c.Notes + o.Notes, NotesExpired: c.NotesExpired + o.NotesExpired, NotesCut: c.NotesCut + o.NotesCut,
		Features: c.Features + o.Features, Superseded: c.Superseded + o.Superseded,
		EdgesDropped: c.EdgesDropped + o.EdgesDropped, Globals: c.Globals + o.Globals,
		Private:    c.Private + o.Private,
		FilterRows: c.FilterRows + o.FilterRows, FilterSessions: c.FilterSessions + o.FilterSessions,
		FilterNoSess: c.FilterNoSess + o.FilterNoSess, FilterV2: c.FilterV2 + o.FilterV2,
		Reviews: c.Reviews + o.Reviews, Picks: c.Picks + o.Picks, Chosen: c.Chosen + o.Chosen,
	}
}

// Render is the one-line summary: parts at zero are left out.
func (c Counts) Render() string {
	var parts []string
	if c.Learnings > 0 {
		var kinds []string
		add := func(n int, one, many string) {
			if n == 1 {
				kinds = append(kinds, "1 "+one)
			} else if n > 1 {
				kinds = append(kinds, fmt.Sprintf("%d %s", n, many))
			}
		}
		add(c.Conventions, "convención", "convenciones")
		add(c.Decisions, "decisión", "decisiones")
		add(c.Bugs, "bug", "bugs")
		add(c.Preferences, "preferencia", "preferencias")
		rng := ""
		switch {
		case c.LearningFirst == 0:
		case c.LearningFirst == c.LearningLast:
			rng = fmt.Sprintf(" A%d", c.LearningFirst)
		default:
			rng = fmt.Sprintf(" A%d..A%d", c.LearningFirst, c.LearningLast)
		}
		if c.Conventions+c.Decisions+c.Bugs+c.Preferences == c.Learnings {
			parts = append(parts, fmt.Sprintf("%s → aprendizajes%s", strings.Join(kinds, ", "), rng))
		} else {
			parts = append(parts, fmt.Sprintf("%s →%s", plural(c.Learnings, "aprendizaje", "aprendizajes"), rng))
		}
	}
	if c.Repeated > 0 {
		parts = append(parts, plural(c.Repeated, "repetido", "repetidos")+" sin importar")
	}
	if c.Notes > 0 {
		parts = append(parts, plural(c.Notes, "handoff vivo", "handoffs vivos")+" → notas")
	}
	if c.NotesExpired > 0 {
		parts = append(parts, plural(c.NotesExpired, "handoff vencido", "handoffs vencidos")+" sin importar")
	}
	if c.Features > 0 {
		parts = append(parts, plural(c.Features, "archivado", "archivados")+" → features cerradas")
	}
	if c.Private > 0 {
		parts = append(parts, plural(c.Private, "privada", "privadas")+" sin importar")
	}
	if c.FilterSessions > 0 {
		parts = append(parts, plural(c.FilterSessions, "sesión del filtro", "sesiones del filtro"))
	}
	if c.Reviews > 0 {
		parts = append(parts, fmt.Sprintf("%s, %s", plural(c.Reviews, "review", "reviews"), plural(c.Chosen, "elegido", "elegidos")))
	}
	return strings.Join(parts, " · ")
}

func firstNonZero(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
