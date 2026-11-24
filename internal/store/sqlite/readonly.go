package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/mananos/nxy.dev/v2/internal/report"
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

// FilterSessionKey is a session of a checkout, as FilterGroupsMany keys its groups.
type FilterSessionKey struct{ Checkout, Session string }

// FilterGroupsMany is the filter groups of every given session in one query, by checkout and
// session, each in order of seq; empty when the database predates the table.
func (r *Reader) FilterGroupsMany(ctx context.Context, sessions []FilterSessionKey) (map[FilterSessionKey][]report.FilterGroup, error) {
	out := map[FilterSessionKey][]report.FilterGroup{}
	if len(sessions) == 0 {
		return out, nil
	}
	var one int
	err := r.db.QueryRowContext(ctx, `SELECT 1 FROM sqlite_master WHERE type='table' AND name='filter_sessions'`).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	pairs := make([][2]string, len(sessions))
	for i, k := range sessions {
		pairs[i] = [2]string{k.Checkout, k.Session}
	}
	list, err := json.Marshal(pairs)
	if err != nil {
		return nil, err
	}
	// by (checkout, session_id) pairs, so each one seeks on the index that leads with checkout
	rows, err := r.db.QueryContext(ctx,
		`SELECT checkout, session_id, engine, reason, kind, rows, chars, tokens FROM filter_sessions
		 WHERE (checkout, session_id) IN (SELECT value->>0, value->>1 FROM json_each(?))
		 ORDER BY checkout, session_id, seq`, string(list))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k FilterSessionKey
		var g report.FilterGroup
		var kind string
		if err := rows.Scan(&k.Checkout, &k.Session, &g.Engine, &g.Reason, &kind, &g.Rows, &g.Chars, &g.Tokens); err != nil {
			return nil, err
		}
		g.Kind = kindValue(kind)
		out[k] = append(out[k], g)
	}
	return out, rows.Err()
}

// SessionUsageRows gives the stored row of each path that has one; empty when the database
// predates the table.
func (r *Reader) SessionUsageRows(ctx context.Context, paths []string) (map[string]SessionUsageRow, error) {
	var one int
	err := r.db.QueryRowContext(ctx, `SELECT 1 FROM sqlite_master WHERE type='table' AND name='session_usage'`).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]SessionUsageRow{}, nil
	}
	if err != nil {
		return nil, err
	}
	return querySessionUsage(ctx, r.db, paths)
}
