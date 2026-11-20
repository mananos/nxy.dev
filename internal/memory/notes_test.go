package memory

import (
	"strings"
	"testing"
	"time"
)

func noteAt(short, title, body string, now time.Time, age time.Duration) Note {
	return Note{Meta: Meta{UpdatedAt: now.Add(-age).UnixMilli()}, Short: short, Title: title, Body: body}
}

func TestNotesQuestionRecommendedFirstAndNone(t *testing.T) {
	now := time.Now()
	notes := []Note{
		noteAt("N1", "uno", "qué falta\nmás", now, 5*time.Minute),
		noteAt("N2", "dos", "", now, 3*time.Hour),
		noteAt("N3", "tres", "x", now, 72*time.Hour),
	}
	q := NotesQuestion(notes, "N3", now)
	if q.Question != NotesQuestionText || len(q.Options) != 4 {
		t.Fatalf("ask = %+v", q)
	}
	if got := q.Options[0].Label; got != "N3 · tres (hace 3 d) (recomendada)" {
		t.Errorf("first = %q", got)
	}
	if got := q.Options[1].Label; got != "N1 · uno (hace 5 min)" || q.Options[1].Description != "qué falta" {
		t.Errorf("second = %+v", q.Options[1])
	}
	if q.Options[2].Description != "Sin detalle" || !strings.Contains(q.Options[2].Label, "hace 3 h") {
		t.Errorf("third = %+v", q.Options[2])
	}
	if q.Options[3].Label != NotesNone || strings.Contains(q.Options[3].Description, "más") {
		t.Errorf("none = %+v", q.Options[3])
	}
}

func TestNotesQuestionCountsTheRest(t *testing.T) {
	now := time.Now()
	var notes []Note
	for _, s := range []string{"N1", "N2", "N3", "N4", "N5"} {
		notes = append(notes, noteAt(s, "t", "", now, time.Hour))
	}
	q := NotesQuestion(notes, "", now)
	if len(q.Options) != 4 {
		t.Fatalf("options = %d", len(q.Options))
	}
	if d := q.Options[3].Description; !strings.Contains(d, "y 2 más") || !strings.Contains(d, "nxy note list") {
		t.Errorf("none = %q", d)
	}
}

func TestNoteChoice(t *testing.T) {
	cases := map[string]string{
		"N3 · tres (hace 3 d) (recomendada)": "N3",
		"N12 · x (hace 1 h)":                 "N12",
		NotesNone:                            "",
		"N":                                  "",
		"":                                   "",
	}
	for in, want := range cases {
		if got := NoteChoice(in); got != want {
			t.Errorf("NoteChoice(%q) = %q, want %q", in, got, want)
		}
	}
}
