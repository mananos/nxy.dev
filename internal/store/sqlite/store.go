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
	ErrConflict = errors.New("sqlite: feature changed concurrently")
	// ErrNotFound means no feature has that id.
	ErrNotFound = errors.New("sqlite: feature not found")
	// ErrActiveFeature means the repo already has a feature that is not done.
	ErrActiveFeature = errors.New("sqlite: repo already has an active feature")
)

// sqliteConstraintUnique is the extended result code SQLITE_CONSTRAINT_UNIQUE.
const sqliteConstraintUnique = 2067

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
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO features (id, version, state, created_at, updated_at, machine_id, repo, done) VALUES (?, 1, ?, ?, ?, ?, ?, ?)`,
		f.ID, string(b), ms, ms, s.machineID, f.Repo, isDone(f))
	var coded interface{ Code() int }
	if errors.As(err, &coded) && coded.Code() == sqliteConstraintUnique {
		return "", ErrActiveFeature
	}
	if err != nil {
		return "", err
	}
	return f.ID, nil
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

// ActiveFeature is the most recently updated feature of repo that is not done.
func (s *Store) ActiveFeature(ctx context.Context, repo string) (domain.Feature, bool, error) {
	f, err := s.readOne(ctx,
		`SELECT state FROM features WHERE repo=? AND done=0 ORDER BY updated_at DESC, id DESC LIMIT 1`, repo)
	if errors.Is(err, ErrNotFound) {
		return domain.Feature{}, false, nil
	}
	if err != nil {
		return domain.Feature{}, false, err
	}
	return f, true, nil
}

// LatestFeature is the most recently updated feature of repo, done or not.
func (s *Store) LatestFeature(ctx context.Context, repo string) (domain.Feature, bool, error) {
	f, err := s.readOne(ctx,
		`SELECT state FROM features WHERE repo=? ORDER BY updated_at DESC, id DESC LIMIT 1`, repo)
	if errors.Is(err, ErrNotFound) {
		return domain.Feature{}, false, nil
	}
	if err != nil {
		return domain.Feature{}, false, err
	}
	return f, true, nil
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
		return domain.Feature{}, fmt.Errorf("sqlite: decode feature: %w", err)
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
			return fmt.Errorf("sqlite: decode feature %s: %w", id, err)
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
		res, err := s.db.ExecContext(ctx,
			`UPDATE features SET state=?, version=version+1, updated_at=?, repo=?, done=? WHERE id=? AND version=?`,
			string(b), s.now().UTC().UnixMilli(), nf.Repo, isDone(nf), id, version)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 1 {
			return nil
		}
	}
	return ErrConflict
}
