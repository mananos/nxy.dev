package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/matyy-a/nxy.dev/internal/domain"
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
		got = f.Phase
		return f, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestTransition(t *testing.T) {
	s := mustOpen(t, dbPath(t))
	id, err := s.CreateFeature(ctx, domain.Feature{Phase: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Transition(ctx, id, func(f domain.Feature) (domain.Feature, error) {
		f.Phase += "b"
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
	id, _ := a.CreateFeature(ctx, domain.Feature{Phase: ""})
	first := true
	err := a.Transition(ctx, id, func(f domain.Feature) (domain.Feature, error) {
		if first {
			first = false
			if err := b.Transition(ctx, id, func(g domain.Feature) (domain.Feature, error) {
				g.Phase += "B"
				return g, nil
			}); err != nil {
				t.Fatal(err)
			}
		}
		f.Phase += "A"
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
			g.Phase += "B"
			return g, nil
		}); err != nil {
			t.Fatal(err)
		}
		f.Phase += "A"
		return f, nil
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v", err)
	}
	if got := phase(t, a, id); got != "B" {
		t.Errorf("phase %q", got)
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
	var rows int
	if err := stores[0].db.QueryRow(`SELECT COUNT(*) FROM schema_version`).Scan(&rows); err != nil || rows != 1 {
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
				f.Phase += "x"
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
