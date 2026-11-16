package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mananos/nxy.dev/internal/legacy"
	"github.com/mananos/nxy.dev/internal/report"
)

// fakeLegacyData is a 1.x folder: a memory.db with rows, a review.jsonl and a filter.jsonl whose
// rows weigh 10 bytes each.
type fakeLegacyData struct {
	mem        []legacy.Memory
	hasMem     bool
	memReads   int
	memErr     error
	review     []any
	hasReview  bool
	filter     []any
	hasFilter  bool
	head       string
	filterRead []int64 // offsets filter.jsonl was read from
	plans      legacy.PlanFiles
}

func (f *fakeLegacyData) PlanFiles(string) (legacy.PlanFiles, error) { return f.plans, nil }

func (f *fakeLegacyData) ProjectKey(string) string { return "proj" }
func (f *fakeLegacyData) RepoKey(string) string    { return "repo:/p" }
func (f *fakeLegacyData) Checkout(string) string   { return "/p/.nxy/local" }
func (f *fakeLegacyData) Folder(string) string     { return "/p" }
func (f *fakeLegacyData) HasMemory() bool          { return f.hasMem }
func (f *fakeLegacyData) Memory(context.Context, string) ([]legacy.Memory, []legacy.Edge, error) {
	f.memReads++
	return f.mem, nil, f.memErr
}
func (f *fakeLegacyData) HasReview(string) bool { return f.hasReview }
func (f *fakeLegacyData) Review(string) []any   { return f.review }
func (f *fakeLegacyData) FilterSize(string) (int64, bool) {
	return int64(len(f.filter)) * 10, f.hasFilter
}
func (f *fakeLegacyData) FilterHead(string, int64) string { return f.head }
func (f *fakeLegacyData) FilterFrom(_ string, offset int64) ([]any, legacy.Cursor, error) {
	f.filterRead = append(f.filterRead, offset)
	size := int64(len(f.filter)) * 10
	return f.filter[offset/10:], legacy.Cursor{Offset: size, Size: size, Head: f.head}, nil
}

type filterCall struct {
	from, to legacy.Cursor
	groups   map[string][]report.FilterGroup
	reset    bool
}

// fakeLegacyStore keeps marks and the cursor like the real one, and can drop notes in a memory.
type fakeLegacyStore struct {
	marks   map[string]legacy.Mark
	cursor  legacy.Cursor
	has     bool
	calls   []filterCall
	imports int // calls to LegacyImports
	v2      map[string]bool
	notes   *noteMemory
	added   []addedGroup // AddFilterGroup calls
}

type addedGroup struct {
	checkout, session string
	g                 report.FilterGroup
}

func newFakeLegacyStore() *fakeLegacyStore {
	return &fakeLegacyStore{marks: map[string]legacy.Mark{}, v2: map[string]bool{}}
}

func (s *fakeLegacyStore) MarkLegacyPlan(_ context.Context, key, answer string, at int64) (bool, error) {
	k := legacy.ScopeHalfPlan + "/" + key
	if _, ok := s.marks[k]; ok {
		return false, nil
	}
	s.marks[k] = legacy.Mark{Scope: legacy.ScopeHalfPlan, Key: key, ImportedAt: at, Answer: answer}
	return true, nil
}

func (s *fakeLegacyStore) mark(scope, key string, c legacy.Counts) bool {
	if _, ok := s.marks[scope+"/"+key]; ok {
		return false
	}
	s.marks[scope+"/"+key] = legacy.Mark{Scope: scope, Key: key, Counts: c}
	return true
}

func (s *fakeLegacyStore) ImportLegacyMemory(_ context.Context, repo string, imp legacy.Import) (legacy.Counts, bool, error) {
	repoNew := s.mark("memory", repo, imp.Counts)
	globalNew := s.mark(legacy.ScopeMemoryGlobal, "", legacy.Counts{Globals: imp.Counts.Globals})
	if !repoNew && !globalNew {
		return legacy.Counts{}, false, nil
	}
	c := imp.Counts
	if !globalNew {
		c.Globals = 0
	}
	if !repoNew {
		return legacy.Counts{Globals: c.Globals}, true, nil
	}
	if s.notes != nil {
		s.notes.notes = append(s.notes.notes, imp.Notes...)
	}
	return c, true, nil
}

func (s *fakeLegacyStore) ImportLegacyReviews(_ context.Context, checkout string, _ []legacy.Pick, c legacy.Counts) (bool, error) {
	return s.mark("review", checkout, c), nil
}

func (s *fakeLegacyStore) ImportLegacyFilter(_ context.Context, _ string, from, to legacy.Cursor, groups map[string][]report.FilterGroup, reset bool) (legacy.FilterResult, error) {
	s.calls = append(s.calls, filterCall{from, to, groups, reset})
	if from != s.cursor {
		return legacy.FilterResult{}, nil
	}
	s.cursor, s.has = to, true
	res := legacy.FilterResult{Applied: true}
	for id, gs := range groups {
		if s.v2[id] {
			res.V2++
			continue
		}
		res.Sessions++
		for _, g := range gs {
			res.Rows += g.Rows
		}
	}
	return res, nil
}

func (s *fakeLegacyStore) LegacyImports(context.Context) ([]legacy.Mark, []legacy.StoredCursor, error) {
	s.imports++
	var marks []legacy.Mark
	for _, m := range s.marks {
		marks = append(marks, m)
	}
	var curs []legacy.StoredCursor
	if s.has {
		curs = append(curs, legacy.StoredCursor{Checkout: "/p/.nxy/local", Cursor: s.cursor})
	}
	return marks, curs, nil
}

func (s *fakeLegacyStore) SetLegacyError(_ context.Context, repo string, at int64, msg string) error {
	if msg == "" {
		delete(s.marks, legacy.ScopeMemoryError+"/"+repo)
		return nil
	}
	s.marks[legacy.ScopeMemoryError+"/"+repo] = legacy.Mark{Scope: legacy.ScopeMemoryError, Key: repo, ImportedAt: at, Error: msg}
	return nil
}

func (s *fakeLegacyStore) AddFilterGroup(_ context.Context, checkout, session string, g report.FilterGroup) error {
	s.added = append(s.added, addedGroup{checkout, session, g})
	return nil
}

func frow(session, engine string) map[string]any {
	return map[string]any{"session_id": session, "engine": engine, "chars": 40.0, "tokens": 10.0}
}

func legacyHooks(t *testing.T) (Hooks, *fakeLegacyData, *fakeLegacyStore, *noteMemory) {
	t.Helper()
	h, mem, _ := notesHooks(t)
	mem.notes = nil
	d := &fakeLegacyData{}
	s := newFakeLegacyStore()
	s.notes = mem
	h.LegacyData, h.LegacyStore = d, s
	return h, d, s, mem
}

func TestSessionStartImportsLegacyOnceAndAsksAboutTheImportedNote(t *testing.T) {
	h, d, s, _ := legacyHooks(t)
	now := time.Now().UnixMilli()
	d.hasMem, d.hasReview, d.hasFilter = true, true, true
	d.mem = []legacy.Memory{
		{ID: "c1", Scope: "project", Project: "proj", Type: "convention", Title: "usar tabs", Created: now, Updated: now},
		{ID: "h1", Scope: "project", Project: "proj", Type: "handoff", Title: "handoff: migrar login", Body: "## Next\n- seguir", Created: now, Updated: now},
	}
	d.review = []any{
		map[string]any{"kind": "review", "id": "a1", "ts": 1.0, "findings": []any{map[string]any{"id": "R1", "lens": "bugs", "severity": "high"}}},
		map[string]any{"kind": "chosen", "id": "a1", "finding": "R1"},
	}
	d.filter = []any{frow("s-old", "rtk"), frow("s-old", "skip"), map[string]any{"engine": "rtk"}}

	r := h.Handle(context.Background(), startEv("startup", "s1"))
	if !strings.Contains(r.Notice, "datos de la 1.x importados") || !strings.Contains(r.Notice, "1 convención") ||
		!strings.Contains(r.Notice, "1 handoff vivo → notas") || !strings.Contains(r.Notice, "1 sesión del filtro") {
		t.Fatalf("notice = %q", r.Notice)
	}
	if len(d.filterRead) != 1 || d.filterRead[0] != 0 || len(s.calls) != 1 || len(s.calls[0].groups) != 1 {
		t.Fatalf("filter reads = %v calls = %+v", d.filterRead, s.calls)
	}

	r = h.Handle(context.Background(), startEv("startup", "s2"))
	if strings.Contains(r.Notice, "1.x") {
		t.Errorf("second start notice = %q", r.Notice)
	}
	if d.memReads != 1 || len(s.calls) != 1 || len(d.filterRead) != 1 {
		t.Errorf("memReads=%d calls=%d reads=%v: nothing should repeat", d.memReads, len(s.calls), d.filterRead)
	}
}

func TestSessionStartGlobalsComeOncePerMachineEvenForAMarkedRepo(t *testing.T) {
	h, d, s, _ := legacyHooks(t)
	now := time.Now().UnixMilli()
	d.hasMem = true
	d.mem = []legacy.Memory{
		{ID: "g1", Scope: "global", Type: "preference", Title: "sin emojis", Created: now, Updated: now},
		{ID: "g2", Scope: "global", Type: "decision", Title: "ingles en commits", Created: now, Updated: now},
	}
	// the repo was imported by a build that knew no globals
	s.marks["memory/"+d.RepoKey("/srv/dev/proj")] = legacy.Mark{Scope: "memory", Key: d.RepoKey("/srv/dev/proj")}

	r := h.Handle(context.Background(), startEv("startup", "s1"))
	if !strings.Contains(r.Notice, "2 globales → aprendizajes globales") {
		t.Fatalf("notice = %q", r.Notice)
	}
	if d.memReads != 1 {
		t.Fatalf("memReads = %d", d.memReads)
	}
	h.Handle(context.Background(), startEv("startup", "s2"))
	if d.memReads != 1 {
		t.Errorf("memReads = %d: the globals must not be read again", d.memReads)
	}
}

func TestSessionStartImportedNoteMakesTheNotesQuestionPending(t *testing.T) {
	h, d, _, _ := legacyHooks(t)
	ss := newFakeSessions()
	h.Sessions = ss
	now := time.Now().UnixMilli()
	d.hasMem = true
	d.mem = []legacy.Memory{{ID: "h1", Scope: "project", Type: "handoff", Title: "handoff: migrar", Body: "x", Created: now, Updated: now}}
	h.Handle(context.Background(), startEv("startup", "s1"))
	if !ss.rows["s1"].NotesPending {
		t.Errorf("row = %+v: the imported note should be asked about", ss.rows["s1"])
	}
}

func TestImportLegacyFilterIsIncrementalAndResets(t *testing.T) {
	h, d, s, _ := legacyHooks(t)
	d.hasFilter, d.head = true, "h1"
	d.filter = []any{frow("a", "rtk"), frow("a", "rtk")}
	ctx := context.Background()
	if _, first, err := h.ImportLegacy(ctx, "/p"); err != nil || first {
		t.Fatalf("first=%v err=%v: the filter alone is not a first import", first, err)
	}
	// Nothing new: nothing is read.
	h.ImportLegacy(ctx, "/p")
	if len(d.filterRead) != 1 {
		t.Fatalf("reads = %v, want one", d.filterRead)
	}
	// Two new rows are read from the stored offset only.
	d.filter = append(d.filter, frow("a", "skip"), frow("b", "rtk"))
	c, _, _ := h.ImportLegacy(ctx, "/p")
	if len(d.filterRead) != 2 || d.filterRead[1] != 20 || c.FilterRows != 2 || c.FilterSessions != 2 {
		t.Fatalf("reads=%v counts=%+v", d.filterRead, c)
	}
	if s.calls[1].reset || s.calls[1].from.Offset != 20 || s.calls[1].to.Offset != 40 {
		t.Fatalf("call = %+v", s.calls[1])
	}
	// Truncated: read again from 0 with reset.
	d.filter = []any{frow("a", "rtk")}
	h.ImportLegacy(ctx, "/p")
	last := s.calls[len(s.calls)-1]
	if !last.reset || d.filterRead[len(d.filterRead)-1] != 0 || last.to.Offset != 10 {
		t.Fatalf("truncate: reads=%v call=%+v", d.filterRead, last)
	}
	// Rotated: same size grown, different start.
	d.filter = append(d.filter, frow("c", "rtk"))
	d.head = "h2"
	h.ImportLegacy(ctx, "/p")
	last = s.calls[len(s.calls)-1]
	if !last.reset || d.filterRead[len(d.filterRead)-1] != 0 {
		t.Fatalf("rotate: reads=%v call=%+v", d.filterRead, last)
	}
}

func TestImportLegacySkipsRowsWithoutSessionAndSessionsOfTheV2(t *testing.T) {
	h, d, s, _ := legacyHooks(t)
	d.hasFilter = true
	d.filter = []any{frow("v2s", "rtk"), frow("old", "rtk"), map[string]any{"engine": "rtk"}, "junk"}
	s.v2["v2s"] = true
	c, _, err := h.ImportLegacy(context.Background(), "/p")
	if err != nil || c.FilterV2 != 1 || c.FilterSessions != 1 || c.FilterNoSess != 2 || c.FilterRows != 1 {
		t.Fatalf("counts=%+v err=%v", c, err)
	}
}

func TestImportLegacyStaleCursorWritesNothing(t *testing.T) {
	h, d, s, _ := legacyHooks(t)
	d.hasFilter = true
	d.filter = []any{frow("a", "rtk")}
	s.cursor, s.has = legacy.Cursor{Offset: 5}, true // another start-up already moved it
	h.LegacyStore = &staleCursorStore{fakeLegacyStore: s}
	c, _, _ := h.ImportLegacy(context.Background(), "/p")
	if c.FilterRows != 0 || c.FilterSessions != 0 {
		t.Errorf("counts = %+v", c)
	}
}

// staleCursorStore reports the cursor as it was before another process moved it.
type staleCursorStore struct{ *fakeLegacyStore }

func (s *staleCursorStore) LegacyImports(ctx context.Context) ([]legacy.Mark, []legacy.StoredCursor, error) {
	return nil, nil, nil
}

func TestImportLegacyWithoutFilesOpensNothing(t *testing.T) {
	h, d, s, _ := legacyHooks(t)
	c, first, err := h.ImportLegacy(context.Background(), "/p")
	if err != nil || first || c != (legacy.Counts{}) {
		t.Fatalf("counts=%+v first=%v err=%v", c, first, err)
	}
	if s.imports != 0 || d.memReads != 0 {
		t.Errorf("store imports=%d memReads=%d: nothing should be opened", s.imports, d.memReads)
	}
	r := h.Handle(context.Background(), startEv("startup", "s1"))
	if r.Notice != "" || s.imports != 0 {
		t.Errorf("notice=%q imports=%d", r.Notice, s.imports)
	}
	// Without the ports it is a no-op too.
	h.LegacyData = nil
	if _, first, err := h.ImportLegacy(context.Background(), "/p"); first || err != nil {
		t.Errorf("nil data: first=%v err=%v", first, err)
	}
}

func TestSessionStartWithoutLegacyDataSaysNothing(t *testing.T) {
	h, d, _, _ := legacyHooks(t)
	d.hasMem = true
	d.mem = nil // memory.db exists but holds nothing of this repo
	r := h.Handle(context.Background(), startEv("startup", "s1"))
	if strings.Contains(r.Notice, "1.x importados") {
		t.Errorf("notice = %q: an empty import has nothing to announce", r.Notice)
	}
}

func TestSessionStartLegacyFailureNoticesAndRetriesOncePerDay(t *testing.T) {
	h, d, s, _ := legacyHooks(t)
	d.hasMem = true
	d.memErr = errors.New("copy memory.db: boom")
	r := h.Handle(context.Background(), startEv("startup", "s1"))
	if !strings.Contains(r.Notice, "la importación de la 1.x falló (copy memory.db: boom)") || !strings.Contains(r.Notice, "nxy legacy import") {
		t.Fatalf("notice = %q", r.Notice)
	}
	if m := s.marks[legacy.ScopeMemoryError+"/repo:/p"]; m.Error == "" || m.ImportedAt == 0 {
		t.Fatalf("error mark = %+v", m)
	}
	r = h.Handle(context.Background(), startEv("startup", "s2"))
	if d.memReads != 1 || strings.Contains(r.Notice, "falló") {
		t.Errorf("memReads=%d notice=%q: a failure within a day is not retried", d.memReads, r.Notice)
	}
	// The user's own command retries, and a success clears the error.
	d.memErr = nil
	if _, first, err := h.ImportLegacyNow(context.Background(), "/p"); err != nil || d.memReads != 2 {
		t.Fatalf("first=%v err=%v memReads=%d", first, err, d.memReads)
	}
	if _, ok := s.marks[legacy.ScopeMemoryError+"/repo:/p"]; ok {
		t.Error("error mark not cleared after a good import")
	}
}

func TestImportLegacyMemoryImportedLaterWhenTheFileAppears(t *testing.T) {
	h, d, s, _ := legacyHooks(t)
	d.hasFilter = true
	d.filter = []any{frow("a", "rtk")}
	h.ImportLegacy(context.Background(), "/p")
	if len(s.marks) != 0 {
		t.Fatalf("marks = %v: no memory.db, no mark", s.marks)
	}
	d.hasMem = true
	d.mem = []legacy.Memory{{ID: "c1", Scope: "project", Type: "convention", Title: "x", Created: 1, Updated: 1}}
	_, first, _ := h.ImportLegacy(context.Background(), "/p")
	if !first || len(s.marks) != 2 { // the repo and the machine's global mark
		t.Errorf("first=%v marks=%v", first, s.marks)
	}
}
