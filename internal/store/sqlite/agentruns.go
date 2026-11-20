package sqlite

import (
	"context"

	"github.com/mananos/nxy.dev/v2/internal/domain"
)

// RecordAgentRun stores the totals of one subagent. A repeated SubagentStop (or a resumed agent
// whose transcript grew) leaves one row with the latest numbers.
func (s *Store) RecordAgentRun(ctx context.Context, r domain.AgentRun) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO agent_runs
	  (feature_id, agent_id, batch, role, model, effort, calls, tokens_in, tokens_out, cache_read, cache_write, cost_usd, cost_estimated, started_at, ended_at)
	  VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	  ON CONFLICT(feature_id, agent_id) DO UPDATE SET
	    batch=CASE WHEN excluded.batch=0 THEN agent_runs.batch ELSE excluded.batch END,
	    role=CASE WHEN excluded.batch=0 THEN agent_runs.role ELSE excluded.role END,
	    model=CASE WHEN excluded.calls=0 THEN agent_runs.model ELSE excluded.model END,
	    effort=CASE WHEN excluded.calls=0 THEN agent_runs.effort ELSE excluded.effort END,
	    calls=CASE WHEN excluded.calls=0 THEN agent_runs.calls ELSE excluded.calls END,
	    tokens_in=CASE WHEN excluded.calls=0 THEN agent_runs.tokens_in ELSE excluded.tokens_in END,
	    tokens_out=CASE WHEN excluded.calls=0 THEN agent_runs.tokens_out ELSE excluded.tokens_out END,
	    cache_read=CASE WHEN excluded.calls=0 THEN agent_runs.cache_read ELSE excluded.cache_read END,
	    cache_write=CASE WHEN excluded.calls=0 THEN agent_runs.cache_write ELSE excluded.cache_write END,
	    cost_usd=CASE WHEN excluded.calls=0 THEN agent_runs.cost_usd ELSE excluded.cost_usd END,
	    cost_estimated=CASE WHEN excluded.calls=0 THEN agent_runs.cost_estimated ELSE excluded.cost_estimated END,
	    started_at=CASE WHEN excluded.calls=0 THEN agent_runs.started_at ELSE excluded.started_at END,
	    ended_at=CASE WHEN excluded.calls=0 THEN agent_runs.ended_at ELSE excluded.ended_at END`,
		r.FeatureID, r.AgentID, r.Batch, r.Role, r.Model, r.Effort, r.Calls, r.TokensIn, r.TokensOut, r.CacheRead,
		r.CacheWrite, r.CostUSD, r.CostEstimated, r.StartedAt, r.EndedAt)
	return err
}

// AgentRuns lists the runs of a feature ordered by start and agent id.
func (s *Store) AgentRuns(ctx context.Context, featureID string) ([]domain.AgentRun, error) {
	return s.queryAgentRuns(ctx, `WHERE feature_id=?`, featureID)
}

// AgentRunsSince lists the runs of every feature that ended at or after since (ms).
func (s *Store) AgentRunsSince(ctx context.Context, since int64) ([]domain.AgentRun, error) {
	return s.queryAgentRuns(ctx, `WHERE ended_at>=?`, since)
}

func (s *Store) queryAgentRuns(ctx context.Context, where string, arg any) ([]domain.AgentRun, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT feature_id, agent_id, batch, role, model, effort, calls, tokens_in, tokens_out,
	  cache_read, cache_write, cost_usd, cost_estimated, started_at, ended_at
	  FROM agent_runs `+where+` ORDER BY started_at, agent_id`, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.AgentRun
	for rows.Next() {
		var r domain.AgentRun
		if err := rows.Scan(&r.FeatureID, &r.AgentID, &r.Batch, &r.Role, &r.Model, &r.Effort, &r.Calls, &r.TokensIn,
			&r.TokensOut, &r.CacheRead, &r.CacheWrite, &r.CostUSD, &r.CostEstimated, &r.StartedAt, &r.EndedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
