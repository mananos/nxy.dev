package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/mananos/nxy.dev/internal/memory"
)

// Session is the local per-session state of the hooks. Times are in ms; zero means "never".
type Session struct {
	ID              string
	Folder          string
	Source          string
	StartedAt       int64
	NotesPending    bool
	RecommendedNote string
	NoteID          string
	StopAsked       bool
	LastEditAt      int64
	ReadHintAt      int64
	EndedAt         int64
	EndReason       string
	FeatureID       string
}

const sessionCols = `session_id, folder, source, started_at, notes_pending, recommended_note, note_id,
	stop_asked, last_edit_at, read_hint_at, ended_at, end_reason, feature_id`

func scanSession(r interface{ Scan(...any) error }) (Session, error) {
	var s Session
	var pending, asked int
	err := r.Scan(&s.ID, &s.Folder, &s.Source, &s.StartedAt, &pending, &s.RecommendedNote, &s.NoteID,
		&asked, &s.LastEditAt, &s.ReadHintAt, &s.EndedAt, &s.EndReason, &s.FeatureID)
	s.NotesPending, s.StopAsked = pending != 0, asked != 0
	return s, err
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// StartSession stores (or restarts) a session row.
func (s *Store) StartSession(ctx context.Context, x Session) error {
	now := s.nowMs()
	if x.StartedAt == 0 {
		x.StartedAt = now
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (`+sessionCols+`, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(session_id) DO UPDATE SET folder=excluded.folder, source=excluded.source,
		feature_id=CASE WHEN excluded.feature_id<>'' THEN excluded.feature_id ELSE sessions.feature_id END,
		started_at=excluded.started_at, notes_pending=excluded.notes_pending,
		recommended_note=excluded.recommended_note, note_id=excluded.note_id, stop_asked=excluded.stop_asked,
		last_edit_at=excluded.last_edit_at, read_hint_at=excluded.read_hint_at, ended_at=0, end_reason='',
		updated_at=excluded.updated_at`,
		x.ID, x.Folder, x.Source, x.StartedAt, b2i(x.NotesPending), x.RecommendedNote, x.NoteID,
		b2i(x.StopAsked), x.LastEditAt, x.ReadHintAt, x.EndedAt, x.EndReason, x.FeatureID, now)
	return err
}

// LinkFeature ties the session to the feature it works on, creating the row if needed.
func (s *Store) LinkFeature(ctx context.Context, sessionID, featureID string) error {
	now := s.nowMs()
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (session_id, started_at, feature_id, updated_at)
		VALUES (?,?,?,?) ON CONFLICT(session_id) DO UPDATE SET feature_id=excluded.feature_id,
		updated_at=excluded.updated_at`, sessionID, now, featureID, now)
	return err
}

// SessionsOfFeature lists the sessions linked to the feature, oldest start first.
func (s *Store) SessionsOfFeature(ctx context.Context, featureID string) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+sessionCols+` FROM sessions WHERE feature_id=? ORDER BY started_at, session_id`, featureID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		x, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// OtherLive is true when a session other than exceptID, not ended, is linked to the feature.
func (s *Store) OtherLive(ctx context.Context, featureID, exceptID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE feature_id=? AND session_id<>? AND ended_at=0`,
		featureID, exceptID).Scan(&n)
	return n > 0, err
}

// Session reads one session; ok is false when there is none.
func (s *Store) Session(ctx context.Context, id string) (Session, bool, error) {
	x, err := scanSession(s.db.QueryRowContext(ctx, `SELECT `+sessionCols+` FROM sessions WHERE session_id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, false, nil
	}
	return x, err == nil, err
}

// ConsumeNotesPending clears notes_pending and reports true only the one time it was set.
func (s *Store) ConsumeNotesPending(ctx context.Context, id string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET notes_pending=0, updated_at=? WHERE session_id=? AND notes_pending=1`, s.nowMs(), id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ChainNote records the note (N3 or ULID as given) the session works on.
func (s *Store) ChainNote(ctx context.Context, id, noteID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET note_id=?, updated_at=? WHERE session_id=?`, noteID, s.nowMs(), id)
	return err
}

// MarkEdit records an edit at `at` (ms), creating the row if no SessionStart came before.
// A new row starts one ms before the edit so that this first edit counts as after the start.
func (s *Store) MarkEdit(ctx context.Context, id, folder string, at int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (session_id, folder, started_at, last_edit_at, updated_at)
		VALUES (?,?,?,?,?) ON CONFLICT(session_id) DO UPDATE SET last_edit_at=excluded.last_edit_at,
		updated_at=excluded.updated_at`, id, folder, at-1, at, s.nowMs())
	return err
}

// MarkStopAsked records that the Stop already asked for a note.
func (s *Store) MarkStopAsked(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET stop_asked=1, updated_at=? WHERE session_id=?`, s.nowMs(), id)
	return err
}

// MarkReadHint reports true only the first time it is called for the session; at is in ms.
func (s *Store) MarkReadHint(ctx context.Context, id string, at int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO sessions (session_id, started_at, read_hint_at, updated_at)
		VALUES (?,?,?,?) ON CONFLICT(session_id) DO UPDATE SET read_hint_at=excluded.read_hint_at,
		updated_at=excluded.updated_at WHERE sessions.read_hint_at=0`, id, at, at, s.nowMs())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// EndSession records how a session ended; at is in ms.
func (s *Store) EndSession(ctx context.Context, id, reason string, at int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET ended_at=?, end_reason=?, updated_at=? WHERE session_id=?`, at, reason, s.nowMs(), id)
	return err
}

// RecentEnd returns the newest session of folder that ended with reason at or after since (ms).
func (s *Store) RecentEnd(ctx context.Context, folder, reason string, since int64, foldCase bool) (Session, bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+sessionCols+` FROM sessions
		WHERE ended_at>0 AND ended_at>=? AND end_reason=? ORDER BY ended_at DESC, session_id`, since, reason)
	if err != nil {
		return Session{}, false, err
	}
	defer rows.Close()
	for rows.Next() {
		x, err := scanSession(rows)
		if err != nil {
			return Session{}, false, err
		}
		if memory.FolderMatch(x.Folder, folder, foldCase) {
			return x, true, nil
		}
	}
	return Session{}, false, rows.Err()
}

// PruneSessions deletes the sessions last updated before the given time and returns how many.
func (s *Store) PruneSessions(ctx context.Context, before time.Time) (int, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE updated_at<?`, before.UTC().UnixMilli())
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}
