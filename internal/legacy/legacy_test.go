package legacy

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mananos/nxy.dev/internal/memory"
)

func TestConvertMemoryPrivate(t *testing.T) {
	mems := []Memory{
		{ID: "c1", Scope: "project", Type: "convention", Title: "Secreta", Private: true, Created: 1, Updated: 1},
		{ID: "h-done-1", Scope: "project", Type: "handoff", Title: "feature: x", Private: true, Created: 2, Updated: 2},
		{ID: "c2", Scope: "project", Type: "convention", Title: "Publica", Created: 3, Updated: 3},
		{ID: "handoff-a-1", Scope: "project", Type: "handoff", Title: "handoff: t", Private: true, Created: 4, Updated: 4, Body: "## Next\nx"},
	}
	imp := ConvertMemory(mems, nil, "r", "/f", 5)
	if imp.Counts.Private != 2 || len(imp.Learnings) != 1 || len(imp.Features) != 0 || len(imp.Notes) != 1 {
		t.Fatalf("imp = %+v", imp)
	}
	if got := imp.Counts.Render(); !strings.Contains(got, "2 privadas sin importar") {
		t.Errorf("render = %q", got)
	}
}

func TestConvertMemory(t *testing.T) {
	const day = int64(24 * 60 * 60 * 1000)
	now := 100 * day
	mems := []Memory{
		{ID: "c1", Scope: "project", Type: "convention", Title: "Usar tabs", Created: 1, Updated: 2},
		{ID: "c2", Scope: "area", Area: "api", Type: "bug", Title: "Falla el parser", Created: 3, Updated: 4},
		{ID: "d1", Scope: "project", Type: "decision", Title: "Viejo", Created: 5, Updated: 5},
		{ID: "p1", Scope: "project", Type: "preference", Title: "Sin emojis", Created: 6, Updated: 6},
		{ID: "g1", Scope: "global", Type: "convention", Title: "Global", Created: 7, Updated: 7},
		{ID: "x1", Scope: "project", Type: "lore", Title: "Otra cosa", Created: 8, Updated: 8},
		{ID: "handoff-a-1", Scope: "project", Type: "handoff", Title: "handoff: mi tarea", Created: 9, Updated: now - day,
			Body: "route: inline\n## Next\n- seguir\n## Done\n- hecho"},
		{ID: "handoff-b-2", Scope: "project", Type: "handoff", Title: "handoff: vieja", Created: 9, Updated: now - 31*day, Body: "x"},
		{ID: "handoff-c-3-done-zz", Scope: "project", Type: "handoff", Title: "feature: cerrada", Created: 10, Updated: 11, Body: "## Next\nnada"},
	}
	edges := []Edge{
		{Src: "c1", Dst: "file:src/a.go", Kind: "file"},
		{Src: "c1", Dst: "file:src/a.go", Kind: "file"},
		{Src: "c1", Dst: "file:src\\b.go", Kind: "file"},
		{Src: "p1", Dst: "d1", Kind: "supersedes"},
		{Src: "c1", Dst: "c2", Kind: "related"},
		{Src: "c1", Dst: "c2", Kind: "co_use"},
		{Src: "handoff-c-3-done-zz", Dst: "file:main.go", Kind: "file"},
	}
	imp := ConvertMemory(mems, edges, "github.com/o/r", "/work/r", now)
	c := imp.Counts
	if c.Conventions != 1 || c.Bugs != 1 || c.Preferences != 1 || c.Decisions != 0 || c.Learnings != 3 {
		t.Errorf("learning counts = %+v", c)
	}
	if c.Superseded != 1 || c.Globals != 1 || c.Other != 1 || c.EdgesDropped != 2 {
		t.Errorf("counts = %+v", c)
	}
	if c.Notes != 1 || c.NotesExpired != 1 || c.Features != 1 {
		t.Errorf("note/feature counts = %+v", c)
	}
	if len(imp.Learnings) != 4 || imp.Learnings[0].Text != "Usar tabs" || imp.Learnings[1].Text != "Falla el parser (área api)" {
		t.Fatalf("learnings = %+v", imp.Learnings)
	}
	l := imp.Learnings[0]
	if len(l.Files) != 2 || l.Files[0] != "src/a.go" || l.Files[1] != "src/b.go" || l.Repo != "github.com/o/r" {
		t.Errorf("files = %v", l.Files)
	}
	if l.Hash != memory.LearningHash(l.Text, l.Files) || l.ID != "" || l.CreatedAt != 1 || l.UpdatedAt != 2 {
		t.Errorf("learning = %+v", l)
	}
	n := imp.Notes[0]
	if n.Folder != "/work/r" || n.Title != "mi tarea" || !strings.HasPrefix(n.Body, "## Next") || n.UpdatedAt != now-day {
		t.Errorf("note = %+v", n)
	}
	f := imp.Features[0]
	if f.Title != "cerrada" || len(f.Files) != 1 || f.Files[0] != (memory.FileRef{Repo: "github.com/o/r", Path: "main.go"}) {
		t.Errorf("feature = %+v", f)
	}
	ids := map[string]string{}
	for _, s := range imp.Sources {
		ids[s.Kind+":"+s.LegacyID] = s.Kind
	}
	if g := imp.Learnings[3]; g.Repo != memory.GlobalRepo || g.Text != "Global" || g.Hash != memory.LearningHash("Global", nil) {
		t.Errorf("global learning = %+v", g)
	}
	if len(imp.Sources) != 6 || ids["global-learning:g1"] == "" || ids["note:handoff-a-1"] == "" || ids["feature:handoff-c-3-done-zz"] == "" {
		t.Errorf("sources = %+v", imp.Sources)
	}
}

func TestConvertMemoryGlobals(t *testing.T) {
	mems := []Memory{
		{ID: "g1", Scope: "global", Type: "preference", Title: "Sin emojis", Created: 1, Updated: 1},
		{ID: "g2", Scope: "global", Type: "convention", Title: "Privada", Private: true, Created: 2, Updated: 2},
		{ID: "g3", Scope: "global", Type: "decision", Title: "Vieja", Created: 3, Updated: 3},
		{ID: "g4", Scope: "global", Type: "decision", Title: "Nueva", Created: 4, Updated: 4},
		{ID: "g5", Scope: "global", Type: "lore", Title: "Otra", Created: 5, Updated: 5},
		{ID: "g6", Scope: "global", Type: "bug", Title: "Con archivo", Created: 6, Updated: 6},
	}
	edges := []Edge{{Src: "g4", Dst: "g3", Kind: "supersedes"}, {Src: "g6", Dst: "file:/srv/dev/a.go", Kind: "file"}}
	imp := ConvertMemory(mems, edges, "r", "/f", 10)
	c := imp.Counts
	if c.Globals != 3 || c.Private != 1 || c.Superseded != 1 || c.Other != 1 || c.Learnings != 0 || c.Preferences != 0 {
		t.Errorf("counts = %+v", c)
	}
	if len(imp.Learnings) != 3 || len(imp.Sources) != 3 || imp.Learnings[2].Files[0] != "/srv/dev/a.go" {
		t.Fatalf("imp = %+v", imp)
	}
	for _, l := range imp.Learnings {
		if l.Repo != memory.GlobalRepo {
			t.Errorf("repo = %q", l.Repo)
		}
	}
	got := Counts{Globals: 3, GlobalFirst: 13, GlobalLast: 15}.Render()
	if got != "3 globales → aprendizajes globales A13..A15" {
		t.Errorf("render = %q", got)
	}
	if got := (Counts{Globals: 1, GlobalFirst: 13, GlobalLast: 13}).Render(); got != "1 global → aprendizaje global A13" {
		t.Errorf("render = %q", got)
	}
}

func TestNoteBody(t *testing.T) {
	body := "route: implementer\n## Done\n- a\n## Next\n- b\n## Files\n- f\n## Decisions\n- d\n" +
		"## Plan\nGoal: llegar lejos\n### Batch 1 — x\ndetalle\n## Extra\n- e\n" +
		"Progress (recorded by nxy, not by the model): batch 1 ✔"
	got := NoteBody(body)
	want := "Goal: llegar lejos\n\n## Next\n- b\n\n## Done\n- a\n\n## Decisions\n- d\n\n## Files\n- f\n\n## Extra\n- e"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
	if strings.Contains(got, CutMark) {
		t.Error("mark on a body that fits")
	}
}

func TestNoteBodyNoSections(t *testing.T) {
	if got := NoteBody("texto viejo\r\nsin secciones\n"); got != "texto viejo\nsin secciones" {
		t.Errorf("got %q", got)
	}
	long := strings.Repeat("palabra ", 600)
	got := NoteBody(long)
	if len(got) > memory.NoteMaxBytes || !strings.HasSuffix(got, "\n"+CutMark) {
		t.Errorf("len %d, tail %q", len(got), got[len(got)-60:])
	}
}

func TestNoteBodyLimit(t *testing.T) {
	// exactly the limit: no mark
	exact := strings.Repeat("a", memory.NoteMaxBytes)
	if got := NoteBody(exact); got != exact {
		t.Errorf("exact body changed, len %d", len(got))
	}
	// one byte over: cut, and the result fits with the mark
	got := NoteBody(exact + "b")
	if len(got) > memory.NoteMaxBytes || !strings.HasSuffix(got, "\n"+CutMark) {
		t.Errorf("len %d", len(got))
	}
	// whole lines first: the line that does not fit is cut, the ones before stay
	lines := "## Next\n" + strings.Repeat("x", 1000) + "\n" + strings.Repeat("y", 1000) + "\n" + strings.Repeat("z", 1000)
	got = NoteBody(lines)
	if !strings.Contains(got, strings.Repeat("x", 1000)) || len(got) > memory.NoteMaxBytes || !strings.HasSuffix(got, CutMark) {
		t.Errorf("len %d", len(got))
	}
	if memory.ValidateNote(memory.Note{Title: "t", Body: got}) != nil {
		t.Error("cut body is not a valid note")
	}
}

func TestNoteBodyRunes(t *testing.T) {
	for _, r := range []string{"é", "€", "😀"} {
		long := strings.Repeat(r, 2000)
		got := NoteBody(long)
		if !utf8.ValidString(got) || len(got) > memory.NoteMaxBytes || !strings.HasSuffix(got, CutMark) {
			t.Errorf("%q: valid=%v len=%d", r, utf8.ValidString(got), len(got))
		}
	}
}

func TestConvertReviews(t *testing.T) {
	rows := []any{
		map[string]any{"kind": "review", "id": "aaa111", "ts": 10.0, "findings": []any{
			map[string]any{"id": "R1", "lens": "bugs", "severity": "high"},
			map[string]any{"id": "R2", "lens": "style", "severity": "low"},
		}},
		map[string]any{"kind": "review", "id": "aaa111", "ts": 20.0, "findings": []any{
			map[string]any{"id": "R1", "lens": "bugs", "severity": "high"},
			map[string]any{"id": "R2", "lens": "style", "severity": "low"},
			map[string]any{"id": "R3", "lens": "bugs", "severity": "medium"},
		}},
		map[string]any{"kind": "chosen", "id": "aaa111", "finding": "R1"},
		map[string]any{"kind": "chosen", "id": "zzz999", "finding": "R1"},
		map[string]any{"kind": "chosen", "id": "aaa111"},
		"basura",
	}
	picks, c := ConvertReviews(rows, "r")
	if len(picks) != 3 || c.Reviews != 1 || c.Picks != 3 || c.Chosen != 1 {
		t.Fatalf("picks = %+v counts = %+v", picks, c)
	}
	p := picks[0]
	if p.FeatureID != "1x:aaa111" || p.Finding != "R1" || !p.Chosen || p.Lens != "bugs" || p.Severity != "high" || p.CreatedAt != 20 || p.Repo != "r" {
		t.Errorf("pick = %+v", p)
	}
	if picks[1].Chosen || picks[2].Chosen {
		t.Errorf("unchosen marked: %+v", picks)
	}
}

func TestCountsRender(t *testing.T) {
	c := Counts{Conventions: 12, Learnings: 12, LearningFirst: 7, LearningLast: 18, Notes: 3, Features: 41, FilterSessions: 30, Reviews: 26, Chosen: 88}
	want := "12 convenciones → aprendizajes A7..A18 · 3 handoffs vivos → notas · 41 archivados → features cerradas · 30 sesiones del filtro · 26 reviews, 88 elegidos"
	if got := c.Render(); got != want {
		t.Errorf("got %q", got)
	}
	r := Counts{Conventions: 3, Learnings: 2, Repeated: 1, LearningFirst: 5, LearningLast: 6}
	if got, want := r.Render(), "2 aprendizajes → A5..A6 · 1 repetido sin importar"; got != want {
		t.Errorf("repeated: got %q want %q", got, want)
	}
	if got := (Counts{}).Render(); got != "" {
		t.Errorf("empty = %q", got)
	}
}
