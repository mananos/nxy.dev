package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/mananos/nxy.dev/internal/report"
)

// Reader reads nxy.db without writing: it never creates the file, runs no migration and holds no
// write lock. For commands that only report (stats, trend).
type Reader struct {
	db *sql.DB
}

// OpenReadOnly opens the database at path read-only. A missing file gives false and is not created.
func OpenReadOnly(ctx context.Context, path string) (*Reader, bool, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	p := filepath.ToSlash(path)
	p = strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(p)
	db, err := sql.Open("sqlite", "file:"+p+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, false, err
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, false, err
	}
	return &Reader{db: db}, true, nil
}

// Close releases the database.
func (r *Reader) Close() error { return r.db.Close() }

// FilterGroups is the filter summary of a session; empty when the database predates the table.
func (r *Reader) FilterGroups(ctx context.Context, checkout, session string) ([]report.FilterGroup, error) {
	var one int
	err := r.db.QueryRowContext(ctx, `SELECT 1 FROM sqlite_master WHERE type='table' AND name='filter_sessions'`).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return queryFilterGroups(ctx, r.db, checkout, session)
}
