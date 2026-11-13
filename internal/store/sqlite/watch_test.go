package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestWatch(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "nxy.db")
	a, err := Open(ctx, path, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Open(ctx, path, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	w, err := a.Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	v1, s1, err := w.Poll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	v2, _, err := w.Poll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v1 != v2 {
		t.Errorf("sin escrituras data_version cambió: %d -> %d", v1, v2)
	}
	latest, err := LatestSchemaVersion()
	if err != nil || latest == 0 || s1 != latest {
		t.Fatalf("schema %d, latest %d, err %v", s1, latest, err)
	}

	if _, err := b.db.ExecContext(ctx, `INSERT OR REPLACE INTO meta (key, value) VALUES ('watch_test', 'x')`); err != nil {
		t.Fatal(err)
	}
	v3, _, err := w.Poll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v3 == v2 {
		t.Errorf("la escritura de otra conexión no cambió data_version (%d)", v3)
	}

	if _, err := b.db.ExecContext(ctx, `INSERT INTO schema_version (version) VALUES (?)`, latest+1); err != nil {
		t.Fatal(err)
	}
	_, s2, err := w.Poll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s2 <= latest {
		t.Errorf("schema %d no supera %d", s2, latest)
	}
}
