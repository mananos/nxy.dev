package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/mananos/nxy.dev/v2/internal/memory"
)

// BootstrapState is the bootstrap of one repo (local, never exported). Status is "", "scouting",
// "confirming", "asked" or "done".
type BootstrapState struct {
	Repo            string
	OfferedAt       int64
	Status          string
	Candidates      []memory.BootstrapCandidate
	PendingRequest  string
	PendingEngineer bool
}

// Bootstrap reads the state of a repo; ok is false when there is none.
func (s *Store) Bootstrap(ctx context.Context, repo string) (BootstrapState, bool, error) {
	var b BootstrapState
	var cands string
	var eng int
	err := s.db.QueryRowContext(ctx, `SELECT repo, offered_at, status, candidates, pending_request, pending_engineer
		FROM mem_bootstrap WHERE repo=?`, repo).Scan(&b.Repo, &b.OfferedAt, &b.Status, &cands, &b.PendingRequest, &eng)
	if errors.Is(err, sql.ErrNoRows) {
		return BootstrapState{}, false, nil
	}
	if err != nil {
		return BootstrapState{}, false, err
	}
	b.PendingEngineer = eng != 0
	if err := json.Unmarshal([]byte(cands), &b.Candidates); err != nil {
		return BootstrapState{}, false, err
	}
	return b, true, nil
}

// SaveBootstrap stores the state of a repo, replacing the row.
func (s *Store) SaveBootstrap(ctx context.Context, b BootstrapState) error {
	cands, err := json.Marshal(b.Candidates)
	if err != nil {
		return err
	}
	if b.Candidates == nil {
		cands = []byte("[]")
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO mem_bootstrap (repo, offered_at, status, candidates, pending_request, pending_engineer, updated_at)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(repo) DO UPDATE SET offered_at=excluded.offered_at, status=excluded.status,
		candidates=excluded.candidates, pending_request=excluded.pending_request,
		pending_engineer=excluded.pending_engineer, updated_at=excluded.updated_at`,
		b.Repo, b.OfferedAt, b.Status, string(cands), b.PendingRequest, b2i(b.PendingEngineer), s.nowMs())
	return err
}

// MarkOffered records that the bootstrap was offered for the repo; true only the first time.
func (s *Store) MarkOffered(ctx context.Context, repo string) (bool, error) {
	now := s.nowMs()
	res, err := s.db.ExecContext(ctx, `INSERT INTO mem_bootstrap (repo, offered_at, updated_at) VALUES (?,?,?)
		ON CONFLICT(repo) DO UPDATE SET offered_at=excluded.offered_at, updated_at=excluded.updated_at
		WHERE mem_bootstrap.offered_at=0`, repo, now, now)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
