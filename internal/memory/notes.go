package memory

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// NotesQuestionText is the question nxy asks at the start of a session that has temporary notes;
// the hook finds the answer by it.
const NotesQuestionText = "nxy: ¿Con cuál seguimos?"

// NotesNone is the label of the option that picks no note.
const NotesNone = "Ninguna, es otra cosa"

// notesShown is how many notes fit in the question (the host allows 4 options, one is "none").
const notesShown = 3

// NoteOption is one option of the notes question.
type NoteOption struct {
	Label       string
	Description string
}

// NotesAsk is the notes question, ready for the host to ask.
type NotesAsk struct {
	Question string
	Header   string
	Options  []NoteOption
}

// NotesQuestion builds the "which note do we continue" question: up to 3 notes (the recommended one,
// a short ref like "N3", first and marked), then "none". When more notes exist the last option says how
// many are left.
func NotesQuestion(notes []Note, recommended string, now time.Time) NotesAsk {
	ordered := make([]Note, 0, len(notes))
	for _, n := range notes {
		if recommended != "" && n.Short == recommended {
			ordered = append(ordered, n)
		}
	}
	for _, n := range notes {
		if recommended != "" && n.Short == recommended {
			continue
		}
		ordered = append(ordered, n)
	}
	q := NotesAsk{Question: NotesQuestionText, Header: "Notas"}
	for i, n := range ordered {
		if i >= notesShown {
			break
		}
		label := fmt.Sprintf("%s · %s (%s)", n.Short, clip(n.Title, 40), ago(n.UpdatedAt, now))
		if recommended != "" && n.Short == recommended {
			label += " (recomendada)"
		}
		desc := clip(firstLine(n.Body), 100)
		if desc == "" {
			desc = "Sin detalle"
		}
		q.Options = append(q.Options, NoteOption{Label: label, Description: desc})
	}
	none := NoteOption{Label: NotesNone, Description: "No sigo ninguna nota"}
	if extra := len(ordered) - notesShown; extra > 0 {
		none.Description = fmt.Sprintf("No sigo ninguna nota; y %d más: `nxy note list`", extra)
	}
	q.Options = append(q.Options, none)
	return q
}

var noteChoice = regexp.MustCompile(`^\s*(N\d+)\b`)

// NoteChoice returns the note ref (N3) an answer to the notes question picked, or "" for "none" or
// anything else.
func NoteChoice(answer string) string {
	m := noteChoice.FindStringSubmatch(answer)
	if m == nil {
		return ""
	}
	return m[1]
}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}

func clip(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:max-1])) + "…"
}

// ago words the age of a time (ms) in Spanish: "hace 5 min", "hace 3 h", "hace 2 d".
func ago(ms int64, now time.Time) string {
	d := now.Sub(time.UnixMilli(ms))
	switch {
	case d < time.Minute:
		return "hace un momento"
	case d < time.Hour:
		return fmt.Sprintf("hace %d min", int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("hace %d h", int(d/time.Hour))
	}
	return fmt.Sprintf("hace %d d", int(d/(24*time.Hour)))
}
