package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/mananos/nxy.dev/internal/report"
)

func TestOpenQuickDropsWriteOnBusyDB(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "nxy.db")
	s, err := Open(ctx, path, nil) // creates and migrates
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Another connection holds the write lock.
	other, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	conn, err := other.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(ctx, "ROLLBACK")

	start := time.Now()
	q, err := OpenQuick(ctx, path, nil)
	if err != nil {
		t.Fatalf("OpenQuick on an up-to-date locked db: %v", err)
	}
	defer q.Close()
	err = q.AddFilterGroup(ctx, "co", "s1", report.FilterGroup{Engine: "rtk", Rows: 1})
	if err == nil {
		t.Fatal("write succeeded under a held write lock")
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("hook path waited %v, want < 1s", d)
	}
}

func TestOpenQuickMigratesMissingDB(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "sub", "nxy.db")
	q, err := OpenQuick(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	if err := q.AddFilterGroup(ctx, "co", "s1", report.FilterGroup{Engine: "rtk", Rows: 1}); err != nil {
		t.Fatal(err)
	}
	gs, err := q.FilterGroups(ctx, "co", "s1")
	if err != nil || len(gs) != 1 {
		t.Fatalf("groups = %v, err = %v", gs, err)
	}
}
