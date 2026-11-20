package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/domain"
)

// pendingAskTTL is how long a pending_asks row is kept even if nobody removed it.
const pendingAskTTL = 24 * time.Hour

// pendingAskStale is how long an unanswered row can go without its hook renewing it before it counts
// as orphaned (the hook rereads every 300 ms).
const pendingAskStale = 10 * time.Second

// CreatePendingAsk stores a waiting question and returns it with its id and creation time.
func (s *Store) CreatePendingAsk(ctx context.Context, a domain.PendingAsk) (domain.PendingAsk, error) {
	q, err := json.Marshal(a.Questions)
	if err != nil {
		return a, err
	}
	a.ID = newULID(s.now(), rand.Reader)
	a.CreatedAt = s.nowMs()
	a.Answers, a.AnsweredBy, a.AnsweredAt = nil, "", 0
	_, err = s.db.ExecContext(ctx, `INSERT INTO pending_asks (id, session_id, feature_id, tool_use_id, questions, created_at, touched_at)
	  VALUES (?,?,?,?,?,?,?)`, a.ID, a.SessionID, a.FeatureID, a.ToolUseID, string(q), a.CreatedAt, a.CreatedAt)
	return a, err
}

// TouchPendingAsk renews an unanswered row: the hook holding it is still alive.
func (s *Store) TouchPendingAsk(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE pending_asks SET touched_at=? WHERE id=? AND answer IS NULL`, s.nowMs(), id)
	return err
}

func (s *Store) staleBefore() int64 { return s.nowMs() - pendingAskStale.Milliseconds() }

const pendingAskCols = `id, session_id, feature_id, tool_use_id, questions, answer, answered_by, created_at, answered_at`

type rowScanner interface{ Scan(...any) error }

func scanPendingAsk(r rowScanner) (domain.PendingAsk, error) {
	var a domain.PendingAsk
	var q string
	var ans sql.NullString
	if err := r.Scan(&a.ID, &a.SessionID, &a.FeatureID, &a.ToolUseID, &q, &ans, &a.AnsweredBy, &a.CreatedAt, &a.AnsweredAt); err != nil {
		return a, err
	}
	if err := json.Unmarshal([]byte(q), &a.Questions); err != nil {
		return a, err
	}
	if ans.Valid {
		a.Answers = []string{}
		if err := json.Unmarshal([]byte(ans.String), &a.Answers); err != nil {
			return a, err
		}
	}
	return a, nil
}

// PendingAsk reads one row by id; sql.ErrNoRows when it is gone or when nobody answered it and its
// hook stopped renewing it (orphaned).
func (s *Store) PendingAsk(ctx context.Context, id string) (domain.PendingAsk, error) {
	return scanPendingAsk(s.db.QueryRowContext(ctx, `SELECT `+pendingAskCols+` FROM pending_asks
	  WHERE id=? AND (answer IS NOT NULL OR touched_at>=?)`, id, s.staleBefore()))
}

// OpenPendingAsks lists the rows nobody answered yet and whose hook is still renewing them, oldest first.
func (s *Store) OpenPendingAsks(ctx context.Context) ([]domain.PendingAsk, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+pendingAskCols+` FROM pending_asks
	  WHERE answer IS NULL AND touched_at>=? ORDER BY created_at, id`, s.staleBefore())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PendingAsk
	for rows.Next() {
		a, err := scanPendingAsk(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AnswerPendingAsk records the answers (one per question; empty = released to the terminal). It
// returns false when the row was already answered or is gone.
func (s *Store) AnswerPendingAsk(ctx context.Context, id string, answers []string, by string) (bool, error) {
	if answers == nil {
		answers = []string{}
	}
	b, err := json.Marshal(answers)
	if err != nil {
		return false, err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE pending_asks SET answer=?, answered_by=?, answered_at=? WHERE id=? AND answer IS NULL AND touched_at>=?`,
		string(b), by, s.nowMs(), id, s.staleBefore())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ReleaseFeatureAsks answers, with no answers and the given by, every open row of a feature.
func (s *Store) ReleaseFeatureAsks(ctx context.Context, featureID, by string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE pending_asks SET answer='[]', answered_by=?, answered_at=? WHERE feature_id=? AND answer IS NULL`,
		by, s.nowMs(), featureID)
	return err
}

// DeletePendingAsk removes a row.
func (s *Store) DeletePendingAsk(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM pending_asks WHERE id=?`, id)
	return err
}
