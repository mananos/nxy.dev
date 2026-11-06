package sqlite

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mananos/nxy.dev/internal/memory"
)

var memT0 = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

// memStore opens a store whose clock is *clock.
func memStore(t *testing.T, path string, clock *time.Time) *Store {
	t.Helper()
	s, err := Open(ctx, path, func() time.Time { return *clock })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func newMem(t *testing.T) (*Store, *time.Time) {
	t.Helper()
	clock := memT0
	return memStore(t, dbPath(t), &clock), &clock
}

func rule(repo, text, q string) memory.Rule {
	return memory.Rule{Repo: repo, Text: text, Question: q, Origin: memory.OriginManual}
}

func TestMemFTSIgnoresAccents(t *testing.T) {
	s, _ := newMem(t)
	if _, err := s.AddRule(ctx, rule("r", "usar una conexión única", "how do we open the db?")); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"conexion", "conexión", "UNICA", `"quote`} {
		hits, err := s.Search(ctx, "r", false, q, 0)
		if err != nil {
			t.Fatalf("%q: %v", q, err)
		}
		if q != `"quote` && (len(hits) != 1 || hits[0].Short != "K1" || hits[0].Kind != "rule") {
			t.Errorf("%q: hits %+v", q, hits)
		}
	}
	if hits, _ := s.Search(ctx, "other", false, "conexion", 0); len(hits) != 0 {
		t.Errorf("other repo sees the rule: %+v", hits)
	}
	if hits, _ := s.Search(ctx, "other", true, "conexion", 0); len(hits) != 1 {
		t.Errorf("--all should find it: %+v", hits)
	}
}

func TestMemRuleNeedsQuestion(t *testing.T) {
	s, _ := newMem(t)
	if _, err := s.AddRule(ctx, rule("r", "x", "  ")); err == nil {
		t.Fatal("Go accepted a rule without a question")
	}
	_, err := s.db.Exec(`INSERT INTO mem_rules (id, short, repo, text, question, origin, created_at, updated_at)
		VALUES ('X','K9','r','t','  ','manual',1,1)`)
	if err == nil {
		t.Fatal("the table accepted a rule without a question")
	}
}

func TestMemSupersedesHidesOldRule(t *testing.T) {
	s, _ := newMem(t)
	a, _ := s.AddRule(ctx, rule("r", "old", "q"))
	b := rule("r", "new", "q")
	b.Supersedes = "K1"
	nb, err := s.AddRule(ctx, b)
	if err != nil || nb.Supersedes != a.ID {
		t.Fatalf("%+v %v", nb, err)
	}
	rs, _ := s.Rules(ctx, "r")
	if len(rs) != 1 || rs[0].Text != "new" {
		t.Errorf("rules %+v", rs)
	}
}

func TestMemLearningDuplicatesAndPromote(t *testing.T) {
	s, clock := newMem(t)
	l := memory.Learning{Repo: "r", Text: "Tests need TZ set.", Files: []string{"./a_test.go"}}
	first, err := s.AddLearning(ctx, l)
	if err != nil || first.Seen != 1 {
		t.Fatal(first, err)
	}
	*clock = clock.Add(time.Hour)
	l.Text, l.Files = "tests  need tz set", []string{"a_test.go"}
	again, err := s.AddLearning(ctx, l)
	if err != nil || again.ID != first.ID || again.Seen != 2 || again.LastSeenAt <= first.LastSeenAt {
		t.Fatalf("%+v %v", again, err)
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM mem_learnings`).Scan(&n)
	if n != 1 {
		t.Fatalf("%d rows", n)
	}
	ls, _ := s.Learnings(ctx, "r")
	if len(ls) != 1 {
		t.Fatal(ls)
	}
	if _, err := s.Promote(ctx, "A1", "", nil); err == nil {
		t.Error("promote without a question worked")
	}
	r, err := s.Promote(ctx, "A1", "what do tests need?", []string{"*_test.go"})
	if err != nil || r.Origin != memory.OriginManual || r.Short != "K1" {
		t.Fatalf("%+v %v", r, err)
	}
	if ls, _ := s.Learnings(ctx, "r"); len(ls) != 0 {
		t.Errorf("promoted learning still listed: %+v", ls)
	}
	if _, err := s.Promote(ctx, "A1", "q", nil); err == nil {
		t.Error("promoted twice")
	}
	if rs, _ := s.Rules(ctx, "r"); len(rs) != 1 || rs[0].Paths[0] != "*_test.go" {
		t.Errorf("rules %+v", rs)
	}
}

func TestMemResolve(t *testing.T) {
	s, _ := newMem(t)
	a, _ := s.AddRule(ctx, rule("r", "a", "q"))
	s.AddRule(ctx, rule("r", "b", "q"))
	if id, err := s.Resolve(ctx, KindRule, "k1"); err != nil || id != a.ID {
		t.Errorf("%q %v", id, err)
	}
	if id, err := s.Resolve(ctx, KindRule, a.ID[:12]); err != nil || id != a.ID {
		t.Errorf("prefix: %q %v", id, err)
	}
	if _, err := s.Resolve(ctx, KindRule, a.ID[:3]); !errors.Is(err, memory.ErrAmbiguous) {
		t.Errorf("want ambiguous, got %v", err)
	}
	if _, err := s.Resolve(ctx, KindRule, "K9"); !errors.Is(err, memory.ErrNotFound) {
		t.Errorf("want not found, got %v", err)
	}
	if _, err := s.Resolve(ctx, KindRule, "A1"); !errors.Is(err, memory.ErrNotFound) {
		t.Errorf("wrong kind: %v", err)
	}
}

func TestMemRelatedAcrossRepos(t *testing.T) {
	s, _ := newMem(t)
	f, err := s.SaveClosedFeature(ctx, memory.ClosedFeature{
		Title: "Sync de cuentas",
		Files: []memory.FileRef{{Repo: "api", Path: `pkg\sync\a.go`}, {Repo: "web", Path: "src/app.ts"}},
	})
	if err != nil || len(f.Repos) != 2 {
		t.Fatalf("%+v %v", f, err)
	}
	s.AddLearning(ctx, memory.Learning{Repo: "api", Text: "sync is idempotent", Files: []string{"pkg/sync/a.go"}})
	for _, c := range []struct {
		repo  string
		paths []string
		feats int
		learn int
	}{
		{"api", []string{"pkg/sync/a.go"}, 1, 1},
		{"api", []string{"pkg"}, 1, 1},
		{"api", []string{"pkg/syn"}, 0, 0},
		{"web", []string{"src"}, 1, 0},
		{"web", []string{"pkg"}, 0, 0},
		{"other", []string{"src"}, 0, 0},
	} {
		fs, ls, err := s.Related(ctx, c.repo, c.paths)
		if err != nil || len(fs) != c.feats || len(ls) != c.learn {
			t.Errorf("%v: %d feats %d learnings %v", c, len(fs), len(ls), err)
		}
	}
	// the feature is found by search from either repo, not from a third
	for repo, want := range map[string]int{"api": 1, "web": 1, "other": 0} {
		if hits, _ := s.Search(ctx, repo, false, "cuentas", 0); len(hits) != want {
			t.Errorf("%s: %+v", repo, hits)
		}
	}
	// saving again with the same id updates it
	f.Title = "Sync de cuentas v2"
	g, err := s.SaveClosedFeature(ctx, f)
	if err != nil || g.ID != f.ID || g.CreatedAt != f.CreatedAt {
		t.Fatalf("%+v %v", g, err)
	}
	if hits, _ := s.Search(ctx, "api", false, "v2", 0); len(hits) != 1 {
		t.Errorf("index not refreshed: %+v", hits)
	}
}

func TestMemNotesFoldersAndExpiry(t *testing.T) {
	s, clock := newMem(t)
	n, err := s.SaveNote(ctx, memory.Note{Folder: "/w/proj", Title: "wip", Body: "left off at step 3"})
	if err != nil || n.Short != "N1" {
		t.Fatalf("%+v %v", n, err)
	}
	if _, err := s.SaveNote(ctx, memory.Note{Folder: "/w/proj", Title: "big", Body: strings.Repeat("x", 2049)}); err == nil {
		t.Error("oversized note accepted")
	}
	for folder, want := range map[string]int{"/w/proj": 1, "/w/proj/sub": 1, "/w": 1, "/w/projx": 0, "/z": 0, "": 1} {
		got, err := s.Notes(ctx, folder, false, *clock)
		if err != nil || len(got) != want {
			t.Errorf("%q: %d notes %v", folder, len(got), err)
		}
	}
	if got, _ := s.Notes(ctx, "/W/PROJ", true, *clock); len(got) != 1 {
		t.Error("fold case did not match")
	}
	if got, _ := s.Notes(ctx, "/W/PROJ", false, *clock); len(got) != 0 {
		t.Error("case-sensitive matched")
	}
	// update by short ref moves the 30 days
	*clock = clock.Add(29 * 24 * time.Hour)
	if u, err := s.SaveNote(ctx, memory.Note{ID: "N1", Title: "wip", Body: "step 4"}); err != nil || u.Folder != "/w/proj" || u.Body != "step 4" {
		t.Fatalf("%+v %v", u, err)
	}
	*clock = clock.Add(29 * 24 * time.Hour)
	if got, _ := s.Notes(ctx, "", false, *clock); len(got) != 1 {
		t.Error("note expired too early")
	}
	*clock = clock.Add(24 * time.Hour)
	if got, _ := s.Notes(ctx, "", false, *clock); len(got) != 0 {
		t.Error("note did not expire at 30 days")
	}
	if c, err := s.SweepNotes(ctx, *clock); err != nil || c != 1 {
		t.Fatalf("sweep %d %v", c, err)
	}
	b, _ := s.ExportBundle(ctx, true)
	if len(b.Notes) != 1 || !b.Notes[0].Deleted() || b.Notes[0].Body != "" {
		t.Errorf("tombstone %+v", b.Notes)
	}
	if _, err := s.Resolve(ctx, KindNote, "N1"); !errors.Is(err, memory.ErrNotFound) {
		t.Errorf("deleted note resolves: %v", err)
	}
}

func TestNoteOfDeletedIsNotFound(t *testing.T) {
	s, _ := newMem(t)
	n, err := s.SaveNote(ctx, memory.Note{Folder: "/w", Title: "t", Body: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Note(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteNote(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Note(ctx, n.ID); err == nil {
		t.Error("deleted note must not be returned")
	}
}

func TestMemExportNotesOnlyOnRequest(t *testing.T) {
	s, _ := newMem(t)
	s.SaveNote(ctx, memory.Note{Folder: "/w", Title: "t", Body: "b"})
	s.AddRule(ctx, rule("r", "a", "q"))
	b, err := s.ExportBundle(ctx, false)
	if err != nil || len(b.Notes) != 0 || len(b.Rules) != 1 || b.Version != memory.BundleVersion || b.Machine != s.MachineID() {
		t.Fatalf("%+v %v", b, err)
	}
	if data, _ := memory.EncodeBundle(b); strings.Contains(string(data), `"notes"`) {
		t.Error("export without notes carries the key")
	}
	if b, _ := s.ExportBundle(ctx, true); len(b.Notes) != 1 {
		t.Errorf("notes %+v", b.Notes)
	}
}

// transfer exports src and imports it into dst through the JSON encoding.
func transfer(t *testing.T, src, dst *Store, notes bool) memory.Diff {
	t.Helper()
	b, err := src.ExportBundle(ctx, notes)
	if err != nil {
		t.Fatal(err)
	}
	data, err := memory.EncodeBundle(b)
	if err != nil {
		t.Fatal(err)
	}
	in, err := memory.DecodeBundle(data)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := dst.MemorySnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	d := memory.Merge(snap, in)
	if err := dst.ApplyImport(ctx, d); err != nil {
		t.Fatal(err)
	}
	return d
}

func seenOf(t *testing.T, s *Store, repo string) int {
	t.Helper()
	ls, err := s.Learnings(ctx, repo)
	if err != nil || len(ls) != 1 {
		t.Fatalf("learnings %+v %v", ls, err)
	}
	return ls[0].Seen
}

func TestMemImportTwiceChangesNothing(t *testing.T) {
	a, aclock := newMem(t)
	b, _ := newMem(t)
	a.AddRule(ctx, rule("r", "from a", "q"))
	b.AddRule(ctx, rule("r", "from b", "q")) // both are K1
	lt := memory.Learning{Repo: "r", Text: "shared lesson"}
	a.AddLearning(ctx, lt)
	a.AddLearning(ctx, lt) // seen 2 in a
	b.AddLearning(ctx, lt) // seen 1 in b: same hash, other ULID
	a.SaveClosedFeature(ctx, memory.ClosedFeature{Title: "done", Files: []memory.FileRef{{Repo: "r", Path: "x.go"}}})

	d := transfer(t, a, b, false)
	if d.Count(memory.Folded) != 1 || d.Count(memory.New) != 2 { // rule + feature new, learning folded
		t.Fatalf("first diff: new %d folded %d", d.Count(memory.New), d.Count(memory.Folded))
	}
	if !d.Rules[0].ReassignShort {
		t.Error("the K1 clash was not flagged")
	}
	rs, _ := b.Rules(ctx, "r")
	if len(rs) != 2 || rs[0].Short != "K1" || rs[1].Short != "K2" || rs[1].Text != "from a" || rs[1].Machine != a.MachineID() {
		t.Fatalf("rules %+v", rs)
	}
	if got := seenOf(t, b, "r"); got != 3 {
		t.Fatalf("seen %d, want 3", got)
	}

	// the same bundle again: nothing to write, seen untouched
	d = transfer(t, a, b, false)
	if d.Writes() != 0 {
		t.Fatalf("second pass writes %d: %+v", d.Writes(), d)
	}
	if got := seenOf(t, b, "r"); got != 3 {
		t.Errorf("seen changed to %d", got)
	}
	// b's export does not carry the alias, and a folds nothing back by accident
	out, _ := b.ExportBundle(ctx, false)
	alias := b.mustLearningID(t)
	for _, l := range out.Learnings {
		if l.ID == alias {
			t.Error("the alias travelled")
		}
	}
	// a sees it again later: only the growth is added
	*aclock = aclock.Add(time.Hour)
	a.AddLearning(ctx, lt)
	d = transfer(t, a, b, false)
	if d.Count(memory.Folded) != 1 || d.Writes() != 1 { // only a's updated learning folds again
		t.Fatalf("third diff: writes %d folded %d", d.Writes(), d.Count(memory.Folded))
	}
	if got := seenOf(t, b, "r"); got != 4 {
		t.Errorf("seen %d, want 4 (growth of one)", got)
	}
}

func TestMemSeenDoesNotMoveUpdatedAt(t *testing.T) {
	s, clock := newMem(t)
	l := memory.Learning{Repo: "r", Text: "counting is not editing"}
	first, _ := s.AddLearning(ctx, l)
	*clock = clock.Add(time.Hour)
	again, err := s.AddLearning(ctx, l)
	if err != nil || again.Seen != 2 || again.UpdatedAt != first.UpdatedAt || again.LastSeenAt <= first.LastSeenAt {
		t.Fatalf("%+v %v", again, err)
	}
}

func TestMemImportKeepsMaxCounters(t *testing.T) {
	a, _ := newMem(t)
	b, _ := newMem(t)
	r, _ := a.AddRule(ctx, rule("r", "shared", "q"))
	l, _ := a.AddLearning(ctx, memory.Learning{Repo: "r", Text: "lesson"})
	transfer(t, a, b, false)

	// a counted more: the row is Skipped but its counters travel, without touching updated_at
	a.db.Exec(`UPDATE mem_rules SET deliveries=5, citations=2, last_cited_at=77 WHERE id=?`, r.ID)
	a.db.Exec(`UPDATE mem_learnings SET seen=4 WHERE id=?`, l.ID)
	d := transfer(t, a, b, false)
	if !d.Rules[0].Counters || d.Rules[0].Kind != memory.Skipped || !d.Learnings[0].Counters || d.Writes() != 2 {
		t.Fatalf("diff %+v", d)
	}
	var del, cit, cited, upd int64
	b.db.QueryRow(`SELECT deliveries, citations, last_cited_at, updated_at FROM mem_rules WHERE id=?`, r.ID).Scan(&del, &cit, &cited, &upd)
	if del != 5 || cit != 2 || cited != 77 || upd != r.UpdatedAt {
		t.Errorf("rule counters %d %d %d updated %d", del, cit, cited, upd)
	}
	var seen int
	b.db.QueryRow(`SELECT seen FROM mem_learnings WHERE id=?`, l.ID).Scan(&seen)
	if seen != 4 {
		t.Errorf("seen %d", seen)
	}
	if d := transfer(t, a, b, false); d.Writes() != 0 {
		t.Errorf("second import writes %d", d.Writes())
	}

	// a newer row with smaller counters does not lower them
	b.db.Exec(`UPDATE mem_rules SET deliveries=9 WHERE id=?`, r.ID)
	a.db.Exec(`UPDATE mem_rules SET text='edited', updated_at=updated_at+1000 WHERE id=?`, r.ID)
	d = transfer(t, a, b, false)
	if d.Rules[0].Kind != memory.Changed {
		t.Fatalf("kind %s", d.Rules[0].Kind)
	}
	b.db.QueryRow(`SELECT deliveries FROM mem_rules WHERE id=?`, r.ID).Scan(&del)
	if del != 9 {
		t.Errorf("deliveries lowered to %d", del)
	}
}

func (s *Store) mustLearningID(t *testing.T) string {
	t.Helper()
	var id string
	if err := s.db.QueryRow(`SELECT alias_id FROM mem_learning_aliases LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestMemDeletionTravelsAndDoesNotRevive(t *testing.T) {
	a, clock := newMem(t)
	b, _ := newMem(t)
	a.SaveNote(ctx, memory.Note{Folder: "/w", Title: "t", Body: "b"})
	old, _ := a.ExportBundle(ctx, true)
	transfer(t, a, b, true)
	if got, _ := b.Notes(ctx, "", false, *clock); len(got) != 1 {
		t.Fatal("note did not arrive")
	}
	*clock = clock.Add(time.Minute)
	if err := a.DeleteNote(ctx, "N1"); err != nil {
		t.Fatal(err)
	}
	transfer(t, a, b, true)
	if got, _ := b.Notes(ctx, "", false, *clock); len(got) != 0 {
		t.Fatal("deletion did not travel")
	}
	// an old export (the live version) must not bring it back
	snap, _ := b.MemorySnapshot(ctx)
	d := memory.Merge(snap, old)
	if d.Writes() != 0 {
		t.Fatalf("old export writes %d", d.Writes())
	}
	// a tombstone for a note the destination never saw still lands, so it cannot revive later
	c, _ := newMem(t)
	transfer(t, a, c, true)
	if got, _ := c.Notes(ctx, "", false, *clock); len(got) != 0 {
		t.Error("tombstone-only import created a live note")
	}
}

func TestMemImportIsAtomic(t *testing.T) {
	a, _ := newMem(t)
	b, _ := newMem(t)
	a.AddRule(ctx, rule("r", "ok", "q"))
	bundle, _ := a.ExportBundle(ctx, false)
	snap, _ := b.MemorySnapshot(ctx)
	d := memory.Merge(snap, bundle)
	// a second row that cannot be written (empty question, not a tombstone) aborts the whole import
	bad := d.Rules[0]
	bad.Row.ID = "ZZZZZZZZZZZZZZZZZZZZZZZZZZ"
	bad.Row.Short = "K7"
	bad.Row.Question = ""
	d.Rules = append(d.Rules, bad)
	if err := b.ApplyImport(ctx, d); err == nil {
		t.Fatal("bad import accepted")
	}
	if rs, _ := b.Rules(ctx, "r"); len(rs) != 0 {
		t.Errorf("partial import: %+v", rs)
	}
}

func TestMemStaleImportDoesNotOverwriteNewerLocalRow(t *testing.T) {
	a, _ := newMem(t)
	b, _ := newMem(t)
	r, _ := a.AddRule(ctx, rule("r", "from a", "q"))
	bundle, _ := a.ExportBundle(ctx, false)
	snap, _ := b.MemorySnapshot(ctx)
	d := memory.Merge(snap, bundle) // decided on an empty snapshot: the rule is New
	if len(d.Rules) != 1 {
		t.Fatalf("diff %+v", d.Rules)
	}
	newer := r
	newer.Text, newer.UpdatedAt = "newer local", r.UpdatedAt+1000
	if err := upsertRule(ctx, b.db, newer); err != nil { // lands after the snapshot
		t.Fatal(err)
	}
	if err := b.ApplyImport(ctx, d); err != nil {
		t.Fatal(err)
	}
	var text string
	b.db.QueryRow(`SELECT text FROM mem_rules WHERE id=?`, r.ID).Scan(&text)
	if text != "newer local" {
		t.Errorf("stale import overwrote the row: %q", text)
	}
}

func TestMemNoteOnPromotedLearningSaysRule(t *testing.T) {
	s, _ := newMem(t)
	l := memory.Learning{Repo: "r", Text: "tests need tz", Files: []string{"a.go"}}
	s.AddLearning(ctx, l)
	rl, err := s.Promote(ctx, "A1", "what do tests need?", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.AddLearning(ctx, l)
	var are *memory.AlreadyRuleError
	if !errors.As(err, &are) || are.Rule != rl.Short || are.Learning != "A1" {
		t.Fatalf("err %v", err)
	}
	var seen int
	s.db.QueryRow(`SELECT seen FROM mem_learnings WHERE short='A1'`).Scan(&seen)
	if seen != 1 {
		t.Errorf("promoted learning bumped to %d", seen)
	}
}

func TestMemNoteOnObsoleteLearningCreatesFreshOne(t *testing.T) {
	s, clock := newMem(t)
	l := memory.Learning{Repo: "r", Text: "tests need tz"}
	first, _ := s.AddLearning(ctx, l)
	if _, err := s.db.Exec(`UPDATE mem_learnings SET obsolete_at=1 WHERE id=?`, first.ID); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(time.Hour)
	again, err := s.AddLearning(ctx, l)
	if err != nil || again.ID == first.ID || again.Seen != 1 {
		t.Fatalf("%+v %v", again, err)
	}
	if ls, _ := s.Learnings(ctx, "r"); len(ls) != 1 || ls[0].ID != again.ID {
		t.Errorf("learnings %+v", ls)
	}
}

func TestMemSearchSkipsSupersededAndPromoted(t *testing.T) {
	s, _ := newMem(t)
	s.AddRule(ctx, rule("r", "zebra old rule", "q"))
	b := rule("r", "zebra new rule", "q")
	b.Supersedes = "K1"
	s.AddRule(ctx, b)
	s.AddLearning(ctx, memory.Learning{Repo: "r", Text: "zebra learning"})
	if _, err := s.Promote(ctx, "A1", "q2", nil); err != nil {
		t.Fatal(err)
	}
	hits, err := s.Search(ctx, "r", false, "zebra", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.Short == "K1" || h.Short == "A1" {
			t.Errorf("hidden row returned: %+v", h)
		}
	}
	if len(hits) != 2 { // K2 (the replacement) and K3 (the promoted learning's rule)
		t.Errorf("hits %+v", hits)
	}
}

func TestMemSupersedesIndexExists(t *testing.T) {
	s, _ := newMem(t)
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='mem_rules_supersedes'`).Scan(&n)
	if n != 1 {
		t.Error("mem_rules_supersedes missing")
	}
}

func TestMigrate4To5KeepsFeatures(t *testing.T) {
	path := dbPath(t)
	migs, _ := embeddedMigrations()
	s, err := openWith(ctx, path, time.Now, migs[:4])
	if err != nil {
		t.Fatal(err)
	}
	id := rawOldFeature(t, s, "r", "keep")
	s.Close()
	s2 := mustOpen(t, path)
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Errorf("no backup: %v", err)
	}
	if f, err := s2.Feature(ctx, id); err != nil || f.Request != "keep" {
		t.Fatalf("%+v %v", f, err)
	}
	if v := version(t, s2); v != len(migs) {
		t.Errorf("version %d", v)
	}
	if _, err := s2.AddRule(ctx, rule("r", "t", "q")); err != nil {
		t.Fatal(err)
	}
}
