// Package sqlite persists nxy state in a single per-user SQLite file.
package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	mrand "math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/internal/domain"
	_ "modernc.org/sqlite"
)

var (
	// ErrConflict means Transition lost the version race on every attempt.
	ErrConflict = errors.New("la feature cambió al mismo tiempo desde otro lado")
	// ErrNotFound means no feature has that id.
	ErrNotFound = errors.New("no existe esa feature")
	// ErrActiveFeature means the repo already has a feature that is not done.
	ErrActiveFeature = errors.New("el repo ya tiene una feature activa")
)

// sqliteConstraintUnique is the extended result code SQLITE_CONSTRAINT_UNIQUE.
const (
	sqliteConstraintUnique     = 2067
	sqliteConstraintPrimaryKey = 1555
)

// Store is the SQLite-backed feature store.
type Store struct {
	db          *sql.DB
	now         func() time.Time
	machineID   string
	maxAttempts int
}

// Open opens (creating and migrating if needed) the database at path.
func Open(ctx context.Context, path string, now func() time.Time) (*Store, error) {
	migs, err := embeddedMigrations()
	if err != nil {
		return nil, err
	}
	return openWith(ctx, path, now, migs)
}

// quickBusyMs is how long OpenQuick waits for a lock held by someone else.
const quickBusyMs = 200

// OpenQuick is Open for hot hooks that must not wait: when the file exists and is already at the
// latest schema it opens with a short busy timeout, no busy retry, no migration and no machine id
// lookup (a write then fails fast with SQLITE_BUSY and the caller drops it). A missing or older
// database goes through Open, which migrates as usual (once).
func OpenQuick(ctx context.Context, path string, now func() time.Time) (*Store, error) {
	migs, err := embeddedMigrations()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err == nil && len(migs) > 0 {
		if now == nil {
			now = time.Now
		}
		db, err := sql.Open("sqlite", fmt.Sprintf("%s?_pragma=busy_timeout(%d)&_pragma=foreign_keys(1)&_txlock=immediate", path, quickBusyMs))
		if err != nil {
			return nil, err
		}
		cur, err := currentVersion(ctx, db)
		if err == nil && cur == migs[len(migs)-1].version {
			return &Store{db: db, now: now, maxAttempts: 40}, nil
		}
		db.Close()
	}
	return Open(ctx, path, now)
}

func openWith(ctx context.Context, path string, now func() time.Time, migs []migration) (*Store, error) {
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db, now: now, maxAttempts: 40}
	// The first connections of a fresh file race to switch it to WAL, which busy_timeout does not cover.
	err = retryBusy(ctx, func() error {
		if err := db.PingContext(ctx); err != nil {
			return err
		}
		return migrate(ctx, db, path, migs)
	})
	if err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO meta (key, value) VALUES ('machine_id', ?)`,
		newULID(now(), rand.Reader)); err != nil {
		db.Close()
		return nil, err
	}
	if err := db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key='machine_id'`).Scan(&s.machineID); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// retryBusy runs fn again, with a short jittered pause, while it fails with SQLITE_BUSY/locked.
func retryBusy(ctx context.Context, fn func() error) error {
	var err error
	for range 100 {
		if err = fn(); err == nil {
			return nil
		}
		msg := err.Error()
		if !strings.Contains(msg, "SQLITE_BUSY") && !strings.Contains(msg, "database is locked") {
			return err
		}
		d := time.Duration(5+mrand.IntN(20)) * time.Millisecond
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

// MachineID is the random id of this user directory.
func (s *Store) MachineID() string { return s.machineID }

// CreateFeature stores f under a fresh ULID and returns it.
func (s *Store) CreateFeature(ctx context.Context, f domain.Feature) (string, error) {
	t := s.now()
	f.ID = newULID(t, rand.Reader)
	b, err := json.Marshal(f)
	if err != nil {
		return "", err
	}
	ms := t.UTC().UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx,
		`INSERT INTO features (id, version, state, created_at, updated_at, machine_id, repo, done, home) VALUES (?, 1, ?, ?, ?, ?, ?, ?, ?)`,
		f.ID, string(b), ms, ms, s.machineID, f.Repo, isDone(f), f.Home); err != nil {
		return "", mapUnique(err)
	}
	if err := syncHolds(ctx, tx, f); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return f.ID, nil
}

// mapUnique turns a UNIQUE violation into ErrActiveFeature.
func mapUnique(err error) error {
	var coded interface{ Code() int }
	if errors.As(err, &coded) && (coded.Code() == sqliteConstraintUnique || coded.Code() == sqliteConstraintPrimaryKey) {
		return ErrActiveFeature
	}
	return err
}

// syncHolds rewrites the checkouts the feature holds to f.Held().
func syncHolds(ctx context.Context, tx *sql.Tx, f domain.Feature) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM feature_holds WHERE feature_id=?`, f.ID); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, c := range f.Held() {
		if seen[c] {
			continue
		}
		seen[c] = true
		if _, err := tx.ExecContext(ctx, `INSERT INTO feature_holds (checkout, feature_id) VALUES (?, ?)`, c, f.ID); err != nil {
			return mapUnique(err)
		}
	}
	return nil
}

func isDone(f domain.Feature) int {
	if f.State.Phase == domain.PhaseDone {
		return 1
	}
	return 0
}

// Feature reads one feature without changing it.
func (s *Store) Feature(ctx context.Context, id string) (domain.Feature, error) {
	return s.readOne(ctx, `SELECT state FROM features WHERE id=?`, id)
}

// ActiveFeature is the feature that holds the checkout (one per checkout).
func (s *Store) ActiveFeature(ctx context.Context, checkout string) (domain.Feature, bool, error) {
	f, err := s.readOne(ctx,
		`SELECT f.state FROM features f JOIN feature_holds h ON h.feature_id=f.id WHERE h.checkout=?`, checkout)
	if errors.Is(err, ErrNotFound) {
		return domain.Feature{}, false, nil
	}
	if err != nil {
		return domain.Feature{}, false, err
	}
	return f, true, nil
}

// LatestFeature is the most recently updated feature started from the folder key (Home), or, for
// rows from before the migration (no home), of that repo key; done or not.
func (s *Store) LatestFeature(ctx context.Context, key string) (domain.Feature, bool, error) {
	f, err := s.readOne(ctx,
		`SELECT state FROM features WHERE home=? OR (home='' AND repo=?) ORDER BY updated_at DESC, id DESC LIMIT 1`, key, key)
	if errors.Is(err, ErrNotFound) {
		return domain.Feature{}, false, nil
	}
	if err != nil {
		return domain.Feature{}, false, err
	}
	return f, true, nil
}

// FeatureFilter narrows Features. With neither Active nor Paused every feature matches; Active is
// not done and not paused; Paused is not done and paused.
type FeatureFilter struct {
	Home   string // folder key; empty matches any
	Active bool
	Paused bool
}

// Features lists features, newest first.
func (s *Store) Features(ctx context.Context, flt FeatureFilter) ([]domain.Feature, error) {
	rows, err := s.FeatureRows(ctx, flt, 0)
	if err != nil {
		return nil, err
	}
	var out []domain.Feature
	for _, r := range rows {
		out = append(out, r.Feature)
	}
	return out, nil
}

// FeatureRow is a feature with its stored creation and last update (ms).
type FeatureRow struct {
	Feature   domain.Feature
	CreatedAt int64
	UpdatedAt int64
}

// FeatureRows is Features with the stored times. With doneSince not 0 the features that finished
// at or after it (ms) are listed too under the Active filter (not under Paused, which asks for paused
// features only). Newest first.
func (s *Store) FeatureRows(ctx context.Context, flt FeatureFilter, doneSince int64) ([]FeatureRow, error) {
	q := `SELECT state, created_at, updated_at, done FROM features`
	var args []any
	var where []string
	if flt.Home != "" {
		where = append(where, `(home=? OR (home='' AND repo=?))`)
		args = append(args, flt.Home, flt.Home)
	}
	if flt.Active || flt.Paused {
		if doneSince != 0 && !flt.Paused {
			where = append(where, `(done=0 OR updated_at>=?)`)
			args = append(args, doneSince)
		} else {
			where = append(where, `done=0`)
		}
	}
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, ` AND `)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY updated_at DESC, id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FeatureRow
	for rows.Next() {
		var state string
		var r FeatureRow
		var done int
		if err := rows.Scan(&state, &r.CreatedAt, &r.UpdatedAt, &done); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(state), &r.Feature); err != nil {
			return nil, fmt.Errorf("no pude leer la feature: %w", err)
		}
		if (flt.Active || flt.Paused) && done == 0 && r.Feature.Paused != flt.Paused {
			continue
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RecordPicks stores which review findings were offered and which were chosen, in one transaction.
// It is idempotent: recording the same finding again replaces its row.
func (s *Store) RecordPicks(ctx context.Context, featureID, repo string, picks []domain.Pick) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ms := s.now().UTC().UnixMilli()
	for _, p := range picks {
		chosen := 0
		if p.Chosen {
			chosen = 1
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR REPLACE INTO review_picks (feature_id, finding, repo, lens, severity, chosen, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			featureID, p.Finding, repo, p.Lens, string(p.Severity), chosen, ms); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) readOne(ctx context.Context, query string, args ...any) (domain.Feature, error) {
	var state string
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Feature{}, ErrNotFound
	}
	if err != nil {
		return domain.Feature{}, err
	}
	var f domain.Feature
	if err := json.Unmarshal([]byte(state), &f); err != nil {
		return domain.Feature{}, fmt.Errorf("no pude leer la feature: %w", err)
	}
	return f, nil
}

// Transition applies fn to the stored feature with optimistic versioning, retrying when another
// writer got in between. fn must be pure: it can run more than once.
func (s *Store) Transition(ctx context.Context, id string, fn func(domain.Feature) (domain.Feature, error)) error {
	for attempt := range s.maxAttempts {
		if attempt > 0 {
			// jittered, growing pause (capped) so contending writers spread out
			d := time.Duration(1<<min(attempt, 6)) * time.Millisecond
			d += time.Duration(mrand.Int64N(int64(d)))
			select {
			case <-time.After(d):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		var state string
		var version int
		err := s.db.QueryRowContext(ctx, `SELECT state, version FROM features WHERE id=?`, id).Scan(&state, &version)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		var f domain.Feature
		if err := json.Unmarshal([]byte(state), &f); err != nil {
			return fmt.Errorf("no pude leer la feature %s: %w", id, err)
		}
		nf, err := fn(f)
		if err != nil {
			return err
		}
		nf.ID = id
		b, err := json.Marshal(nf)
		if err != nil {
			return err
		}
		done, err := s.commitTransition(ctx, nf, string(b), version)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
	return ErrConflict
}

// commitTransition writes nf and its holds in one transaction; false means the version moved.
func (s *Store) commitTransition(ctx context.Context, nf domain.Feature, state string, version int) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx,
		`UPDATE features SET state=?, version=version+1, updated_at=?, repo=?, done=?, home=? WHERE id=? AND version=?`,
		state, s.now().UTC().UnixMilli(), nf.Repo, isDone(nf), nf.Home, nf.ID, version)
	if err != nil {
		return false, mapUnique(err)
	}
	n, err := res.RowsAffected()
	if err != nil || n != 1 {
		return false, err
	}
	if err := syncHolds(ctx, tx, nf); err != nil {
		return false, err
	}
	return true, tx.Commit()
}
