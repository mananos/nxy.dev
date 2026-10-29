package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// ErrSchemaNewer means the database was written by a newer nxy; this binary must not touch it.
var ErrSchemaNewer = errors.New("sqlite: database schema is newer than this binary")

type migration struct {
	version int
	sql     string
}

// embeddedMigrations reads migrations/NNNN_name.sql sorted by numeric prefix.
func embeddedMigrations() ([]migration, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		name := e.Name()
		prefix, _, _ := strings.Cut(name, "_")
		v, err := strconv.Atoi(prefix)
		if err != nil {
			return nil, fmt.Errorf("sqlite: bad migration name %q", name)
		}
		b, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return nil, err
		}
		out = append(out, migration{version: v, sql: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

func currentVersion(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) (int, error) {
	var v sql.NullInt64
	if err := q.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_version`).Scan(&v); err != nil {
		return 0, err
	}
	return int(v.Int64), nil
}

// migrate brings the database at path up to the latest migration, backing it up first when it
// already holds data from an older schema.
func migrate(ctx context.Context, db *sql.DB, path string, migs []migration) error {
	latest := 0
	if len(migs) > 0 {
		latest = migs[len(migs)-1].version
	}
	// Fast path without a write lock; an error here just means the table does not exist yet.
	if cur, err := currentVersion(ctx, db); err == nil {
		if cur > latest {
			return fmt.Errorf("%w (db %d, binary %d)", ErrSchemaNewer, cur, latest)
		}
		if cur == latest {
			return nil
		}
	}
	tx, err := db.BeginTx(ctx, nil) // _txlock=immediate: takes the write lock now
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return err
	}
	cur, err := currentVersion(ctx, tx)
	if err != nil {
		return err
	}
	if cur > latest {
		return fmt.Errorf("%w (db %d, binary %d)", ErrSchemaNewer, cur, latest)
	}
	if cur == latest {
		return nil // another process migrated while we waited; its .bak stays untouched
	}
	if cur > 0 {
		// Under the write lock: no other migrator can run, and a reader connection (WAL) sees the
		// committed pre-migration state, so the backup is always of the old schema.
		bak := path + ".bak"
		_ = os.Remove(bak)
		if _, err := db.ExecContext(ctx, `VACUUM INTO `+quote(bak)); err != nil {
			return fmt.Errorf("sqlite: backup: %w", err)
		}
	}
	for _, m := range migs {
		if m.version <= cur {
			continue
		}
		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			return fmt.Errorf("sqlite: migration %d: %w", m.version, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_version (version) VALUES (?)`, m.version); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
