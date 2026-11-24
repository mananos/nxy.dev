package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/report"
)

// sessionUsageTTL is how long a transcript's row is kept, by the mtime of the transcript.
const sessionUsageTTL = 90 * 24 * time.Hour

// SessionUsageRow is the stored summary of one session transcript and the signature it was read at.
type SessionUsageRow struct {
	Path      string
	SessionID string
	Stamp     string
	MtimeMs   int64
	Usage     report.SessionUsage
}

type rowsQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// SessionUsageRows gives the stored row of each path that has one, keyed by path.
func (s *Store) SessionUsageRows(ctx context.Context, paths []string) (map[string]SessionUsageRow, error) {
	return querySessionUsage(ctx, s.db, paths)
}

// SaveSessionUsage upserts the rows in one transaction and deletes those whose transcript is
// older than 90 days (by the Store's clock).
func (s *Store) SaveSessionUsage(ctx context.Context, rows []SessionUsageRow) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := s.nowMs()
	cutoff := now - sessionUsageTTL.Milliseconds()
	for _, r := range rows {
		if r.MtimeMs < cutoff {
			continue // it would be pruned below: never write what is deleted in the same transaction
		}
		data, err := json.Marshal(r.Usage)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO session_usage (path, session_id, stamp, mtime_ms, data, saved_at)
		  VALUES (?,?,?,?,?,?)
		  ON CONFLICT(path) DO UPDATE SET session_id=excluded.session_id, stamp=excluded.stamp,
		    mtime_ms=excluded.mtime_ms, data=excluded.data, saved_at=excluded.saved_at`,
			r.Path, r.SessionID, r.Stamp, r.MtimeMs, string(data), now); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM session_usage WHERE mtime_ms < ?`, cutoff); err != nil {
		return err
	}
	return tx.Commit()
}

func querySessionUsage(ctx context.Context, q rowsQuerier, paths []string) (map[string]SessionUsageRow, error) {
	out := map[string]SessionUsageRow{}
	const chunk = 500
	for i := 0; i < len(paths); i += chunk {
		part := paths[i:min(i+chunk, len(paths))]
		args := make([]any, len(part))
		for j, p := range part {
			args[j] = p
		}
		rows, err := q.QueryContext(ctx, `SELECT path, session_id, stamp, mtime_ms, data FROM session_usage
		  WHERE path IN (?`+strings.Repeat(",?", len(part)-1)+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var r SessionUsageRow
			var data string
			if err := rows.Scan(&r.Path, &r.SessionID, &r.Stamp, &r.MtimeMs, &data); err != nil {
				rows.Close()
				return nil, err
			}
			if err := json.Unmarshal([]byte(data), &r.Usage); err != nil {
				continue // a row nobody can read is a miss: the caller re-reads the transcript
			}
			out[r.Path] = r
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
