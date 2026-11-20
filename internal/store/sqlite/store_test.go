package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/domain"
)

var ctx = context.Background()

func dbPath(t *testing.T) string { return filepath.Join(t.TempDir(), "nxy.db") }

func mustOpen(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(ctx, path, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func version(t *testing.T, s *Store) int {
	t.Helper()
	v, err := currentVersion(ctx, s.db)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func extraMigs(t *testing.T) []migration {
	migs, err := embeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	return append(migs, migration{version: 99, sql: `CREATE TABLE extra (x INTEGER);`})
}

func TestFreshDB(t *testing.T) {
	path := dbPath(t)
	s := mustOpen(t, path)
	migs, _ := embeddedMigrations()
	if got := version(t, s); got != migs[len(migs)-1].version {
		t.Errorf("version %d", got)
	}
	if len(s.MachineID()) != 26 {
		t.Errorf("machine id %q", s.MachineID())
	}
	id := s.MachineID()
	s.Close()
	s2 := mustOpen(t, path)
	if s2.MachineID() != id {
		t.Error("machine id changed on reopen")
	}
	if _, err := os.Stat(path + ".bak"); err == nil {
		t.Error("reopen made a backup")
	}
}

func TestMigrateOlderMakesBackup(t *testing.T) {
	path := dbPath(t)
	mustOpen(t, path).Close()
	s, err := openWith(ctx, path, time.Now, extraMigs(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Errorf("no backup: %v", err)
	}
	if got := version(t, s); got != 99 {
		t.Errorf("version %d", got)
	}
}

func TestSchemaNewer(t *testing.T) {
	path := dbPath(t)
	s, err := openWith(ctx, path, time.Now, extraMigs(t))
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := Open(ctx, path, time.Now); !errors.Is(err, ErrSchemaNewer) {
		t.Fatalf("err = %v", err)
	}
}

func phase(t *testing.T, s *Store, id string) string {
	t.Helper()
	var got string
	err := s.Transition(ctx, id, func(f domain.Feature) (domain.Feature, error) {
		got = f.Request
		return f, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestTransition(t *testing.T) {
	s := mustOpen(t, dbPath(t))
	id, err := s.CreateFeature(ctx, domain.Feature{Request: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Transition(ctx, id, func(f domain.Feature) (domain.Feature, error) {
		f.Request += "b"
		return f, nil
	}); err != nil {
		t.Fatal(err)
	}
	var v int
	if err := s.db.QueryRow(`SELECT version FROM features WHERE id=?`, id).Scan(&v); err != nil || v != 2 {
		t.Errorf("version %d, %v", v, err)
	}
	if got := phase(t, s, id); got != "ab" {
		t.Errorf("phase %q", got)
	}
	err = s.Transition(ctx, "missing", func(f domain.Feature) (domain.Feature, error) { return f, nil })
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v", err)
	}
}

func TestTransitionConflictRetries(t *testing.T) {
	path := dbPath(t)
	a := mustOpen(t, path)
	b := mustOpen(t, path)
	id, _ := a.CreateFeature(ctx, domain.Feature{})
	first := true
	err := a.Transition(ctx, id, func(f domain.Feature) (domain.Feature, error) {
		if first {
			first = false
			if err := b.Transition(ctx, id, func(g domain.Feature) (domain.Feature, error) {
				g.Request += "B"
				return g, nil
			}); err != nil {
				t.Fatal(err)
			}
		}
		f.Request += "A"
		return f, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := phase(t, a, id); got != "BA" {
		t.Errorf("phase %q", got)
	}
}

func TestTransitionConflictExhausted(t *testing.T) {
	path := dbPath(t)
	a := mustOpen(t, path)
	b := mustOpen(t, path)
	a.maxAttempts = 1
	id, _ := a.CreateFeature(ctx, domain.Feature{})
	err := a.Transition(ctx, id, func(f domain.Feature) (domain.Feature, error) {
		if err := b.Transition(ctx, id, func(g domain.Feature) (domain.Feature, error) {
			g.Request += "B"
			return g, nil
		}); err != nil {
			t.Fatal(err)
		}
		f.Request += "A"
		return f, nil
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v", err)
	}
	if got := phase(t, a, id); got != "B" {
		t.Errorf("phase %q", got)
	}
}

func TestActiveFeaturePerRepo(t *testing.T) {
	s := mustOpen(t, dbPath(t))
	if _, ok, err := s.ActiveFeature(ctx, "r1"); err != nil || ok {
		t.Fatalf("empty: ok=%v err=%v", ok, err)
	}
	a, _ := s.CreateFeature(ctx, domain.Feature{Repo: "r1", Request: "a"})
	b, _ := s.CreateFeature(ctx, domain.Feature{Repo: "r2", Request: "b"})
	f, ok, err := s.ActiveFeature(ctx, "r1")
	if err != nil || !ok || f.ID != a {
		t.Fatalf("r1: %v %v %v", f.ID, ok, err)
	}
	f, ok, _ = s.ActiveFeature(ctx, "r2")
	if !ok || f.ID != b {
		t.Fatalf("r2: %v %v", f.ID, ok)
	}
	for _, out := range []domain.Outcome{domain.OutcomeSuccess, domain.OutcomeStopped} {
		id, _ := s.CreateFeature(ctx, domain.Feature{Repo: "r3"})
		if err := s.Transition(ctx, id, func(f domain.Feature) (domain.Feature, error) {
			f.State = domain.State{Phase: domain.PhaseDone, Outcome: out}
			return f, nil
		}); err != nil {
			t.Fatal(err)
		}
		if _, ok, _ := s.ActiveFeature(ctx, "r3"); ok {
			t.Errorf("%s: still active", out)
		}
		if got, err := s.Feature(ctx, id); err != nil || got.State.Outcome != out {
			t.Errorf("Feature: %+v %v", got.State, err)
		}
	}
}

func TestLatestFeatureReturnsDone(t *testing.T) {
	s := mustOpen(t, dbPath(t))
	if _, ok, err := s.LatestFeature(ctx, "r"); err != nil || ok {
		t.Fatalf("empty: ok=%v err=%v", ok, err)
	}
	id, _ := s.CreateFeature(ctx, domain.Feature{Repo: "r", Request: "a"})
	if err := s.Transition(ctx, id, func(f domain.Feature) (domain.Feature, error) {
		f.State = domain.State{Phase: domain.PhaseDone, Outcome: domain.OutcomeSuccess}
		return f, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.ActiveFeature(ctx, "r"); ok {
		t.Fatal("still active")
	}
	f, ok, err := s.LatestFeature(ctx, "r")
	if err != nil || !ok || f.ID != id || f.State.Phase != domain.PhaseDone {
		t.Fatalf("latest: %+v %v %v", f.State, ok, err)
	}
	if _, ok, _ := s.LatestFeature(ctx, "other"); ok {
		t.Error("other repo has a latest")
	}
}

func TestFeatureRows(t *testing.T) {
	clock := memT0
	s := memStore(t, dbPath(t), &clock)
	finish := func(id string) {
		t.Helper()
		if err := s.Transition(ctx, id, func(f domain.Feature) (domain.Feature, error) {
			f.State = domain.State{Phase: domain.PhaseDone, Outcome: domain.OutcomeSuccess}
			return f, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	oldDone, _ := s.CreateFeature(ctx, domain.Feature{Repo: "r1", Request: "old"})
	finish(oldDone)
	clock = clock.Add(48 * time.Hour)
	since := clock.UnixMilli()
	active, _ := s.CreateFeature(ctx, domain.Feature{Repo: "r1", Request: "active"})
	paused, _ := s.CreateFeature(ctx, domain.Feature{Repo: "r2", Request: "paused", Paused: true})
	clock = clock.Add(time.Hour)
	newDone, _ := s.CreateFeature(ctx, domain.Feature{Repo: "r3", Request: "new"})
	finish(newDone)
	other, _ := s.CreateFeature(ctx, domain.Feature{Repo: "r4", Request: "other"})

	ids := func(rows []FeatureRow) []string {
		var out []string
		for _, r := range rows {
			out = append(out, r.Feature.ID)
		}
		return out
	}
	has := func(rows []FeatureRow, id string) bool {
		for _, x := range ids(rows) {
			if x == id {
				return true
			}
		}
		return false
	}
	rows, err := s.FeatureRows(ctx, FeatureFilter{Active: true}, since)
	if err != nil {
		t.Fatal(err)
	}
	if !has(rows, active) || !has(rows, newDone) || !has(rows, other) {
		t.Errorf("active+done since: %v", ids(rows))
	}
	if has(rows, oldDone) || has(rows, paused) {
		t.Errorf("done before since or paused listed: %v", ids(rows))
	}
	for i := 1; i < len(rows); i++ {
		if rows[i].UpdatedAt > rows[i-1].UpdatedAt {
			t.Errorf("not newest first: %v", ids(rows))
		}
	}
	for _, r := range rows {
		if r.CreatedAt == 0 || r.UpdatedAt < r.CreatedAt {
			t.Errorf("times of %s: %d %d", r.Feature.ID, r.CreatedAt, r.UpdatedAt)
		}
	}
	rows, _ = s.FeatureRows(ctx, FeatureFilter{Active: true}, 0)
	if has(rows, newDone) || has(rows, oldDone) || len(rows) != 2 {
		t.Errorf("doneSince 0 lists done: %v", ids(rows))
	}
	rows, _ = s.FeatureRows(ctx, FeatureFilter{Paused: true}, since)
	if has(rows, newDone) || has(rows, oldDone) || !has(rows, paused) || len(rows) != 1 {
		t.Errorf("paused+doneSince lists done: %v", ids(rows))
	}
	rows, _ = s.FeatureRows(ctx, FeatureFilter{Active: true}, since)
	if !has(rows, newDone) {
		t.Errorf("active+doneSince lacks done: %v", ids(rows))
	}
	rows, _ = s.FeatureRows(ctx, FeatureFilter{Home: "r1"}, 0)
	if len(rows) != 2 {
		t.Errorf("home r1: %v", ids(rows))
	}
	if all, _ := s.Features(ctx, FeatureFilter{}); len(all) != 5 {
		t.Errorf("Features all = %d", len(all))
	}
}

func TestRecordPicksIdempotent(t *testing.T) {
	s := mustOpen(t, dbPath(t))
	picks := []domain.Pick{
		{Finding: "H1", Lens: "base", Severity: domain.Severity("high"), Chosen: true},
		{Finding: "H2", Lens: "api", Severity: domain.Severity("low"), Chosen: false},
	}
	for range 2 {
		if err := s.RecordPicks(ctx, "f1", "r", picks); err != nil {
			t.Fatal(err)
		}
	}
	var n, chosen int
	if err := s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(chosen),0) FROM review_picks WHERE feature_id='f1'`).Scan(&n, &chosen); err != nil {
		t.Fatal(err)
	}
	if n != 2 || chosen != 1 {
		t.Errorf("rows %d chosen %d", n, chosen)
	}
}

func TestMigrate3To4KeepsFeatures(t *testing.T) {
	path := dbPath(t)
	migs, _ := embeddedMigrations()
	s, err := openWith(ctx, path, time.Now, migs[:3])
	if err != nil {
		t.Fatal(err)
	}
	id := rawOldFeature(t, s, "r", "keep")
	s.Close()
	s2 := mustOpen(t, path)
	f, err := s2.Feature(ctx, id)
	if err != nil || f.Request != "keep" {
		t.Fatalf("feature: %+v %v", f, err)
	}
	if err := s2.RecordPicks(ctx, id, "r", []domain.Pick{{Finding: "H1", Lens: "base"}}); err != nil {
		t.Fatal(err)
	}
}

func TestOneActiveFeaturePerRepo(t *testing.T) {
	s := mustOpen(t, dbPath(t))
	a, err := s.CreateFeature(ctx, domain.Feature{Repo: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateFeature(ctx, domain.Feature{Repo: "r1"}); !errors.Is(err, ErrActiveFeature) {
		t.Fatalf("second in same repo: err = %v", err)
	}
	if _, err := s.CreateFeature(ctx, domain.Feature{Repo: "r2"}); err != nil {
		t.Fatalf("other repo: %v", err)
	}
	if err := s.Transition(ctx, a, func(f domain.Feature) (domain.Feature, error) {
		f.State = domain.State{Phase: domain.PhaseDone, Outcome: domain.OutcomeStopped}
		return f, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateFeature(ctx, domain.Feature{Repo: "r1"}); err != nil {
		t.Fatalf("after done: %v", err)
	}
}

// rawOldFeature inserts an active feature at a schema before 0009 (no home column).
func rawOldFeature(t *testing.T, s *Store, repo, request string) string {
	t.Helper()
	id := "OLD" + request
	if _, err := s.db.Exec(`INSERT INTO features (id, version, state, created_at, updated_at, machine_id, repo, done) VALUES (?, 1, ?, 1, 1, 'm', ?, 0)`,
		id, `{"id":"`+id+`","repo":"`+repo+`","request":"`+request+`"}`, repo); err != nil {
		t.Fatal(err)
	}
	return id
}

func setPhase(t *testing.T, s *Store, id string, fn func(*domain.Feature)) error {
	t.Helper()
	return s.Transition(ctx, id, func(f domain.Feature) (domain.Feature, error) {
		fn(&f)
		return f, nil
	})
}

func TestTwoActiveFeaturesSameRepoDifferentCheckouts(t *testing.T) {
	s := mustOpen(t, dbPath(t))
	a, err := s.CreateFeature(ctx, domain.Feature{Repo: "r", Home: "h1", Holds: []string{"c1"}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateFeature(ctx, domain.Feature{Repo: "r", Home: "h2", Holds: []string{"c2"}})
	if err != nil {
		t.Fatalf("other checkout: %v", err)
	}
	for c, want := range map[string]string{"c1": a, "c2": b} {
		if f, ok, _ := s.ActiveFeature(ctx, c); !ok || f.ID != want {
			t.Errorf("%s: %q %v", c, f.ID, ok)
		}
	}
	if _, ok, _ := s.ActiveFeature(ctx, "r"); ok {
		t.Error("a feature with Home must not hold its repo key")
	}
	if f, ok, _ := s.LatestFeature(ctx, "h1"); !ok || f.ID != a {
		t.Errorf("latest by home: %q %v", f.ID, ok)
	}
}

func TestCheckoutClash(t *testing.T) {
	s := mustOpen(t, dbPath(t))
	if _, err := s.CreateFeature(ctx, domain.Feature{Home: "h", Holds: []string{"c1", "c2"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateFeature(ctx, domain.Feature{Home: "h2", Holds: []string{"c3", "c2"}}); !errors.Is(err, ErrActiveFeature) {
		t.Fatalf("clash: %v", err)
	}
	if _, ok, _ := s.ActiveFeature(ctx, "c3"); ok {
		t.Error("failed create left a hold")
	}
	b, err := s.CreateFeature(ctx, domain.Feature{Home: "h3", Holds: []string{"c3"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := setPhase(t, s, b, func(f *domain.Feature) { f.Holds = []string{"c3", "c1"} }); !errors.Is(err, ErrActiveFeature) {
		t.Fatalf("transition clash: %v", err)
	}
	if f, _ := s.Feature(ctx, b); len(f.Holds) != 1 {
		t.Errorf("failed transition changed the feature: %v", f.Holds)
	}
}

func TestRaceTwoCreateFeature(t *testing.T) {
	path := dbPath(t)
	stores := []*Store{mustOpen(t, path), mustOpen(t, path)}
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = stores[i].CreateFeature(ctx, domain.Feature{Home: "h", Holds: []string{"c"}})
		}()
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		if err == nil {
			ok++
		} else if !errors.Is(err, ErrActiveFeature) {
			t.Fatalf("err %v", err)
		}
	}
	if ok != 1 {
		t.Errorf("%d creates won, want 1", ok)
	}
}

func TestFeatureWithoutHolds(t *testing.T) {
	s := mustOpen(t, dbPath(t))
	for range 2 {
		if _, err := s.CreateFeature(ctx, domain.Feature{Repo: "r", Home: "h"}); err != nil {
			t.Fatalf("no holds: %v", err)
		}
	}
	if _, ok, _ := s.ActiveFeature(ctx, "r"); ok {
		t.Error("held without Holds")
	}
	fs, err := s.Features(ctx, FeatureFilter{Home: "h", Active: true})
	if err != nil || len(fs) != 2 {
		t.Errorf("features: %d %v", len(fs), err)
	}
}

func TestPauseReleasesCheckouts(t *testing.T) {
	s := mustOpen(t, dbPath(t))
	a, _ := s.CreateFeature(ctx, domain.Feature{Home: "h", Holds: []string{"c"}})
	if err := setPhase(t, s, a, func(f *domain.Feature) { f.Paused = true }); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.ActiveFeature(ctx, "c"); ok {
		t.Error("paused feature still holds")
	}
	if _, err := s.CreateFeature(ctx, domain.Feature{Home: "h2", Holds: []string{"c"}}); err != nil {
		t.Fatalf("take freed checkout: %v", err)
	}
	if fs, _ := s.Features(ctx, FeatureFilter{Paused: true}); len(fs) != 1 || fs[0].ID != a {
		t.Errorf("paused list: %+v", fs)
	}
	if fs, _ := s.Features(ctx, FeatureFilter{Active: true}); len(fs) != 1 {
		t.Errorf("active list: %d", len(fs))
	}
	// resuming while another feature holds it fails and leaves it paused
	if err := setPhase(t, s, a, func(f *domain.Feature) { f.Paused = false }); !errors.Is(err, ErrActiveFeature) {
		t.Fatalf("resume clash: %v", err)
	}
	if f, _ := s.Feature(ctx, a); !f.Paused {
		t.Error("failed resume changed the feature")
	}
}

func TestMigrate8To9KeepsActiveHold(t *testing.T) {
	path := dbPath(t)
	migs, _ := embeddedMigrations()
	s, err := openWith(ctx, path, time.Now, migs[:8])
	if err != nil {
		t.Fatal(err)
	}
	id := rawOldFeature(t, s, "r", "old")
	s.Close()
	s2 := mustOpen(t, path)
	f, ok, err := s2.ActiveFeature(ctx, "r")
	if err != nil || !ok || f.ID != id {
		t.Fatalf("active after migration: %v %v %v", f.ID, ok, err)
	}
	if f, ok, _ := s2.LatestFeature(ctx, "r"); !ok || f.ID != id {
		t.Error("latest by repo for a row without home")
	}
	if err := s2.LinkFeature(ctx, "s1", id); err != nil {
		t.Fatal(err)
	}
	if x, ok, _ := s2.Session(ctx, "s1"); !ok || x.FeatureID != id {
		t.Errorf("session link: %+v", x)
	}
}

func TestMigrate2To3KeepsNewestActive(t *testing.T) {
	path := dbPath(t)
	migs, _ := embeddedMigrations()
	s, err := openWith(ctx, path, time.Now, migs[:2])
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []struct {
		id   string
		upd  int
		repo string
	}{{"a", 1, "r"}, {"b", 3, "r"}, {"c", 2, "r"}, {"d", 1, "other"}} {
		if _, err := s.db.Exec(`INSERT INTO features (id, version, state, created_at, updated_at, machine_id, repo, done) VALUES (?, 1, '{}', 1, ?, 'm', ?, 0)`,
			r.id, r.upd, r.repo); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	s2 := mustOpen(t, path)
	rows, err := s2.db.Query(`SELECT id FROM features WHERE done=0 ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		got = append(got, id)
	}
	if len(got) != 2 || got[0] != "b" || got[1] != "d" {
		t.Errorf("active after migration = %v, want [b d]", got)
	}
}

func TestFeatureNotFound(t *testing.T) {
	s := mustOpen(t, dbPath(t))
	if _, err := s.Feature(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestMigrate1To2(t *testing.T) {
	path := dbPath(t)
	migs, _ := embeddedMigrations()
	s, err := openWith(ctx, path, time.Now, migs[:1])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO features (id, version, state, created_at, updated_at, machine_id) VALUES ('old', 1, '{"request":"x"}', 1, 1, 'm')`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s2 := mustOpen(t, path)
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Errorf("no backup: %v", err)
	}
	var repo string
	var done int
	if err := s2.db.QueryRow(`SELECT repo, done FROM features WHERE id='old'`).Scan(&repo, &done); err != nil || repo != "" || done != 0 {
		t.Errorf("repo %q done %d err %v", repo, done, err)
	}
	if _, ok, _ := s2.ActiveFeature(ctx, ""); !ok {
		t.Error("old row not readable as active for repo ''")
	}
}

func TestParallelOpen(t *testing.T) {
	path := dbPath(t)
	const n = 8
	stores := make([]*Store, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stores[i], errs[i] = Open(ctx, path, nil)
		}()
	}
	wg.Wait()
	for i := range n {
		if errs[i] != nil {
			t.Fatalf("open %d: %v", i, errs[i])
		}
		defer stores[i].Close()
	}
	for i := 1; i < n; i++ {
		if stores[i].MachineID() != stores[0].MachineID() {
			t.Errorf("machine id differs: %s vs %s", stores[i].MachineID(), stores[0].MachineID())
		}
	}
	migs, _ := embeddedMigrations()
	var rows int
	if err := stores[0].db.QueryRow(`SELECT COUNT(*) FROM schema_version`).Scan(&rows); err != nil || rows != len(migs) {
		t.Errorf("schema_version rows %d, %v", rows, err)
	}
	if _, err := os.Stat(path + ".bak"); err == nil {
		t.Error("fresh parallel open must not write a backup")
	}
}

func TestTransitionConcurrent(t *testing.T) {
	path := dbPath(t)
	stores := []*Store{mustOpen(t, path), mustOpen(t, path)}
	id, _ := stores[0].CreateFeature(ctx, domain.Feature{})
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := stores[i%2].Transition(ctx, id, func(f domain.Feature) (domain.Feature, error) {
				f.Request += "x"
				return f, nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got := phase(t, stores[0], id); len(got) != 10 {
		t.Errorf("phase length %d", len(got))
	}
}
