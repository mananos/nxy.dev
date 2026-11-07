package sqlite

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mananos/nxy.dev/internal/jscompat"
	"github.com/mananos/nxy.dev/internal/legacy"
	"github.com/mananos/nxy.dev/internal/memory"
	"github.com/mananos/nxy.dev/internal/report"
)

// legacyDDL is the schema of the 1.x memory.db (core/memory/store.mjs openStore), as is.
const legacyDDL = `
    CREATE TABLE IF NOT EXISTS memories (
      rowid    INTEGER PRIMARY KEY,
      id       TEXT NOT NULL UNIQUE,
      scope    TEXT NOT NULL,
      project  TEXT,
      area     TEXT,
      type     TEXT NOT NULL,
      title    TEXT NOT NULL,
      body     TEXT NOT NULL,
      keywords TEXT NOT NULL DEFAULT '',
      private  INTEGER NOT NULL DEFAULT 0,
      created  INTEGER NOT NULL,
      updated  INTEGER NOT NULL
    );
    CREATE INDEX IF NOT EXISTS memories_scope ON memories(scope, project, area);
    CREATE TABLE IF NOT EXISTS edges (
      src     TEXT NOT NULL,
      dst     TEXT NOT NULL,
      kind    TEXT NOT NULL,
      weight  REAL NOT NULL DEFAULT 1,
      by      TEXT NOT NULL DEFAULT 'code',
      created INTEGER NOT NULL,
      PRIMARY KEY (src, dst, kind)
    );
`

func legacyDB(t *testing.T, ms int64) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "memory.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(legacyDDL); err != nil {
		t.Fatal(err)
	}
	ins := func(id, scope, project, typ, title, body string, updated int64) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO memories (id, scope, project, area, type, title, body, created, updated) VALUES (?,?,?,?,?,?,?,?,?)`,
			id, scope, project, nil, typ, title, body, updated-1000, updated); err != nil {
			t.Fatal(err)
		}
	}
	ins("project-use-tabs-aaaaa", "project", "github.com/o/r", "convention", "usar tabs", "x", ms)
	ins("project-other-bbbbb", "project", "github.com/o/other", "convention", "de otro repo", "x", ms)
	ins("global-pref-ccccc", "global", "", "preference", "global", "x", ms)
	ins("project-handoff-ddddd", "project", "github.com/o/r", "handoff", "handoff: arreglar login", "Goal: arreglar login\n## Next\n- probar", ms)
	ins("project-handoff-done-eeeee", "project", "github.com/o/r", "handoff", "feature: cerrada", "listo", ms)
	for _, e := range [][3]string{
		{"project-use-tabs-aaaaa", "file:src/a.go", "file"},
		{"project-handoff-done-eeeee", "file:src/login.go", "file"},
		{"project-use-tabs-aaaaa", "project-other-bbbbb", "related"},
	} {
		if _, err := db.Exec(`INSERT INTO edges (src, dst, kind, created) VALUES (?,?,?,0)`, e[0], e[1], e[2]); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func dirState(t *testing.T, dir string) map[string]string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range ents {
		b, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		fi, _ := e.Info()
		out[e.Name()] = string(b) + "|" + fi.ModTime().String()
	}
	return out
}

func TestLegacyMemoryImport(t *testing.T) {
	s, clock := newMem(t)
	ms := clock.UnixMilli()
	src := legacyDB(t, ms)
	before := dirState(t, filepath.Dir(src))

	mems, edges, err := ReadLegacyMemory(ctx, src, "github.com/o/r")
	if err != nil {
		t.Fatal(err)
	}
	after := dirState(t, filepath.Dir(src))
	if len(before) != len(after) {
		t.Fatalf("files next to memory.db changed: %v -> %v", before, after)
	}
	for k, v := range before {
		if after[k] != v {
			t.Fatalf("%s changed", k)
		}
	}
	if len(mems) != 5-1 { // the other repo's convention stays out; the global one comes
		t.Fatalf("memories: %d", len(mems))
	}
	imp := legacy.ConvertMemory(mems, edges, "github.com/o/r", "/work/r", ms)
	got, ok, err := s.ImportLegacyMemory(ctx, "github.com/o/r", imp)
	if err != nil || !ok {
		t.Fatalf("first import: %v %v", ok, err)
	}
	if got.Learnings != 1 || got.LearningFirst != 1 || got.LearningLast != 1 || got.Repeated != 0 {
		t.Fatalf("counts %+v", got)
	}
	if _, ok, err := s.ImportLegacyMemory(ctx, "github.com/o/r", imp); err != nil || ok {
		t.Fatalf("second import: %v %v", ok, err)
	}
	ls, _ := s.Learnings(ctx, "github.com/o/r")
	if len(ls) != 1 || ls[0].Short != "A1" || len(ls[0].Files) != 1 {
		t.Fatalf("learnings %+v", ls)
	}
	notes, err := s.Notes(ctx, "/work/r", false, *clock)
	if err != nil || len(notes) != 1 || !strings.Contains(notes[0].Body, "Goal: arreglar login") {
		t.Fatalf("notes %+v %v", notes, err)
	}
	feats, _, err := s.Related(ctx, "github.com/o/r", []string{"src/login.go"})
	if err != nil || len(feats) != 1 || feats[0].Title != "cerrada" {
		t.Fatalf("related %+v %v", feats, err)
	}
	// a second database gets the same ids
	s2, _ := newMem(t)
	other := legacy.Import{
		Learnings: []memory.Learning{{Repo: "github.com/o/other", Text: "otra cosa", Hash: memory.LearningHash("otra cosa", nil)}},
		Sources:   []legacy.Source{{Kind: legacy.SourceLearning, Index: 0, LegacyID: "zzz-other"}},
	}
	if _, _, err := s2.ImportLegacyMemory(ctx, "github.com/o/other", other); err != nil {
		t.Fatal(err)
	}
	// with learnings already stored the range starts at the real short
	c2, _, err := s2.ImportLegacyMemory(ctx, "github.com/o/r", imp)
	if err != nil {
		t.Fatal(err)
	}
	if c2.Learnings != 1 || c2.LearningFirst != 2 || c2.LearningLast != 2 {
		t.Errorf("counts after previous learnings: %+v", c2)
	}
	// a learning that is already there does not count
	c3, ok3, err := s2.ImportLegacyMemory(ctx, "again", imp)
	if err != nil || !ok3 || c3.Learnings != 0 || c3.Repeated != 1 || c3.LearningFirst != 0 {
		t.Errorf("repeated: %+v %v %v", c3, ok3, err)
	}
	ls2, _ := s2.Learnings(ctx, "github.com/o/r")
	if ls2[0].ID != ls[0].ID {
		t.Errorf("ids differ: %s %s", ls2[0].ID, ls[0].ID)
	}
	marks, _, err := s.LegacyImports(ctx)
	if err != nil || len(marks) != 1 || marks[0].Counts.Notes != 1 {
		t.Fatalf("marks %+v %v", marks, err)
	}
	if mems, _, err := ReadLegacyMemory(ctx, filepath.Join(t.TempDir(), "none.db"), "x"); err != nil || mems != nil {
		t.Errorf("missing memory.db: %v %v", mems, err)
	}
}

func TestLegacyReviewImport(t *testing.T) {
	s, clock := newMem(t)
	picks := []legacy.Pick{
		{FeatureID: "1x:a", Finding: "F1", Repo: "r", Lens: "bugs", Severity: "high", Chosen: true, CreatedAt: clock.UnixMilli()},
		{FeatureID: "1x:a", Finding: "F2", Repo: "r", Lens: "bugs", Severity: "low", CreatedAt: clock.UnixMilli()},
		{FeatureID: "1x:a", Finding: "F3", Repo: "r", Lens: "style", Severity: "low", CreatedAt: clock.UnixMilli()},
	}
	if ok, err := s.ImportLegacyReviews(ctx, "co", picks, legacy.Counts{Reviews: 1}); err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	if ok, err := s.ImportLegacyReviews(ctx, "co", picks, legacy.Counts{}); err != nil || ok {
		t.Fatalf("again: %v %v", ok, err)
	}
	st, err := s.ReviewStats(ctx, "r", clock.Add(-time.Hour).UnixMilli())
	if err != nil || len(st) != 2 || st[0].Lens != "bugs" || st[0].Chosen != 1 || st[0].Ignored != 1 || st[1].Ignored != 1 {
		t.Fatalf("stats %+v %v", st, err)
	}
}

func fg(engine, reason string, kind any, rows int, chars float64) report.FilterGroup {
	return report.FilterGroup{Engine: engine, Reason: reason, Kind: kind, Rows: rows, Chars: chars, Tokens: chars / 4}
}

func TestLegacyFilterIncremental(t *testing.T) {
	s, _ := newMem(t)
	c0 := legacy.Cursor{}
	c1 := legacy.Cursor{Offset: 100, Size: 100, Head: "h"}
	c2 := legacy.Cursor{Offset: 200, Size: 200, Head: "h"}
	one := []report.FilterGroup{fg("rtk", "", "git", 2, 40), fg("skip", "short", jscompat.Undefined, 1, 8)}
	two := []report.FilterGroup{fg("rtk", "", "git", 1, 10), fg("rtk", "", 7.0, 1, 4)}

	r, err := s.ImportLegacyFilter(ctx, "co", c0, c1, map[string][]report.FilterGroup{"s1": one}, false)
	if err != nil || !r.Applied || r.Sessions != 1 || r.Rows != 3 {
		t.Fatalf("%+v %v", r, err)
	}
	// an old cursor writes nothing
	r, err = s.ImportLegacyFilter(ctx, "co", c0, c2, map[string][]report.FilterGroup{"s1": two}, false)
	if err != nil || r.Applied {
		t.Fatalf("stale cursor applied: %+v %v", r, err)
	}
	if cur, _ := s.FilterCursor(ctx, "co"); cur != c1 {
		t.Fatalf("cursor %+v", cur)
	}
	if _, err := s.ImportLegacyFilter(ctx, "co", c1, c2, map[string][]report.FilterGroup{"s1": two}, false); err != nil {
		t.Fatal(err)
	}
	got, err := s.FilterGroups(ctx, "co", "s1")
	if err != nil {
		t.Fatal(err)
	}
	want := report.MergeFilterGroups(one, two)
	if report.SummarizeFilterGroups(got).FilteredChars != report.SummarizeFilterGroups(want).FilteredChars || len(got) != len(want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
	for i := range want {
		if got[i].Engine != want[i].Engine || got[i].Rows != want[i].Rows || got[i].Chars != want[i].Chars ||
			jscompat.String(got[i].Kind) != jscompat.String(want[i].Kind) {
			t.Errorf("group %d: %+v vs %+v", i, got[i], want[i])
		}
	}
	if !jscompat.IsUndefined(got[1].Kind) {
		t.Errorf("kind of skip group: %#v", got[1].Kind)
	}
	// the v2 owns s2: the 1.x rows are skipped
	if err := s.AddFilterGroup(ctx, "co", "s2", fg("rtk", "", "go", 1, 5)); err != nil {
		t.Fatal(err)
	}
	if err := s.AddFilterGroup(ctx, "co", "s2", fg("rtk", "", "go", 1, 5)); err != nil {
		t.Fatal(err)
	}
	c3 := legacy.Cursor{Offset: 300, Size: 300, Head: "h"}
	r, err = s.ImportLegacyFilter(ctx, "co", c2, c3, map[string][]report.FilterGroup{"s2": one, "s3": two}, false)
	if err != nil || r.V2 != 1 || r.Sessions != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	g2, _ := s.FilterGroups(ctx, "co", "s2")
	if len(g2) != 1 || g2[0].Rows != 2 || g2[0].Chars != 10 {
		t.Fatalf("v2 session: %+v", g2)
	}
	// reset: the file was read again from 0; no duplicates, s1 not in the file stays
	c4 := legacy.Cursor{Offset: 50, Size: 50, Head: "new"}
	if _, err := s.ImportLegacyFilter(ctx, "co", c3, c4, map[string][]report.FilterGroup{"s3": two}, true); err != nil {
		t.Fatal(err)
	}
	g3, _ := s.FilterGroups(ctx, "co", "s3")
	if len(g3) != 2 || g3[0].Rows != 1 {
		t.Fatalf("after reset: %+v", g3)
	}
	if g1, _ := s.FilterGroups(ctx, "co", "s1"); len(g1) != 3 {
		t.Fatalf("s1 groups: %+v", g1)
	}
	if g, _ := s.FilterGroups(ctx, "other", "s1"); len(g) != 0 {
		t.Errorf("another checkout sees rows: %+v", g)
	}
	_, curs, err := s.LegacyImports(ctx)
	if err != nil || len(curs) != 1 || curs[0].Cursor != c4 {
		t.Fatalf("cursors %+v %v", curs, err)
	}
}

func TestSweepFilterSessions(t *testing.T) {
	s, clock := newMem(t)
	c1 := legacy.Cursor{Offset: 100, Size: 100, Head: "h"}
	one := []report.FilterGroup{fg("rtk", "", "git", 2, 40)}
	if _, err := s.ImportLegacyFilter(ctx, "co", legacy.Cursor{}, c1, map[string][]report.FilterGroup{"old": one, "live": one}, false); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(80 * 24 * time.Hour)
	// a new row keeps the whole "live" session
	if err := s.AddFilterGroup(ctx, "co", "live", fg("skip", "short", jscompat.Undefined, 1, 4)); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(11 * 24 * time.Hour)
	if _, err := s.SweepNotes(ctx, *clock); err != nil {
		t.Fatal(err)
	}
	if g, _ := s.FilterGroups(ctx, "co", "old"); len(g) != 0 {
		t.Errorf("old session kept: %+v", g)
	}
	if g, _ := s.FilterGroups(ctx, "co", "live"); len(g) != 2 {
		t.Errorf("live session cut: %+v", g)
	}
	if cur, _ := s.FilterCursor(ctx, "co"); cur != c1 {
		t.Errorf("cursor touched: %+v", cur)
	}
	// the same cursor: the swept session does not come back
	r, err := s.ImportLegacyFilter(ctx, "co", legacy.Cursor{}, c1, map[string][]report.FilterGroup{"old": one}, false)
	if err != nil || r.Applied {
		t.Fatalf("%+v %v", r, err)
	}
	if g, _ := s.FilterGroups(ctx, "co", "old"); len(g) != 0 {
		t.Errorf("old session came back: %+v", g)
	}
}

func TestOpenReadOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "nxy.db")
	r, ok, err := OpenReadOnly(ctx, path)
	if err != nil || ok || r != nil {
		t.Fatalf("missing file: %v %v", ok, err)
	}
	if _, err := os.Stat(filepath.Dir(path)); err == nil {
		t.Fatal("OpenReadOnly created the folder")
	}

	// a db from before the table
	old := filepath.Join(dir, "old.db")
	raw, err := sql.Open("sqlite", old)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE x (a INTEGER)`); err != nil {
		t.Fatal(err)
	}
	raw.Close()
	r, ok, err = OpenReadOnly(ctx, old)
	if err != nil || !ok {
		t.Fatalf("old db: %v %v", ok, err)
	}
	if g, err := r.FilterGroups(ctx, "co", "s"); err != nil || len(g) != 0 {
		t.Errorf("old db groups: %+v %v", g, err)
	}
	r.Close()

	s := mustOpen(t, filepath.Join(dir, "nxy.db"))
	if err := s.AddFilterGroup(ctx, "co", "s", fg("rtk", "", "git", 3, 30)); err != nil {
		t.Fatal(err)
	}
	r, ok, err = OpenReadOnly(ctx, filepath.Join(dir, "nxy.db"))
	if err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	defer r.Close()
	g, err := r.FilterGroups(ctx, "co", "s")
	if err != nil || len(g) != 1 || g[0].Rows != 3 {
		t.Fatalf("%+v %v", g, err)
	}
	if _, err := r.db.Exec(`DELETE FROM filter_sessions`); err == nil {
		t.Error("the reader can write")
	}
}

func TestBundleExcludesLegacyTables(t *testing.T) {
	s, _ := newMem(t)
	if err := s.AddFilterGroup(ctx, "co", "session-secret", fg("rtk", "", "git", 1, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ImportLegacyReviews(ctx, "co-secret", nil, legacy.Counts{}); err != nil {
		t.Fatal(err)
	}
	b, err := s.ExportBundle(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"session-secret", "co-secret", "filter_sessions"} {
		if strings.Contains(string(raw), bad) {
			t.Errorf("bundle carries %q", bad)
		}
	}
}
