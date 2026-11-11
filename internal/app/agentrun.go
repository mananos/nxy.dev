package app

import (
	"context"
	"strings"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/metrics"
	"github.com/mananos/nxy.dev/internal/pricing"
)

// recordAgentRun keeps what the subagent that stopped cost and how long it took, one row per
// (feature, agent). It runs before anything else of SubagentStop, so the lane of an implementer is
// still there. A missing port, agent id or active feature records nothing; the write error is
// dropped: it never blocks the subagent.
func (h Hooks) recordAgentRun(ctx context.Context, ev HookEvent) {
	if ev.AgentID == "" || h.AgentFiles == nil || h.Runs == nil {
		return
	}
	f, ok := h.featureFor(ctx, ev)
	if !ok {
		return
	}
	batch := 0
	role := strings.TrimPrefix(ev.AgentType, "nxy:")
	if ev.AgentType == roleImplementer {
		switch f.State.Phase {
		case domain.PhaseRunningBatch:
			if n, found := laneFor(f, ev); found {
				batch = n
			}
		case domain.PhaseAffectedFix, domain.PhaseFixing:
			role = "fixer"
		}
	}
	res, _ := h.AgentFiles.Read(ev.AgentTranscript, ev.AgentID)
	b := metrics.NewBucket()
	model, effort := "", ""
	for _, c := range res.Calls {
		b.Add(c)
		if c.Model != "" && !pricing.IsSyntheticModel(c.Model) {
			model = c.Model
		}
		if c.Effort != nil && *c.Effort != "" {
			effort = *c.Effort
		}
	}
	if effort == "" && h.Roles != nil {
		dir := ev.ProjectDir
		if dir == "" {
			dir = ev.Cwd
		}
		_, effort = h.Roles.Role(dir, role)
	}
	fin := b.Finish()
	now := h.now().UnixMilli()
	start, end := now, now
	if res.FirstTs != nil {
		start = *res.FirstTs
	}
	if res.LastTs != nil {
		end = *res.LastTs
	}
	_ = h.Runs.RecordAgentRun(ctx, domain.AgentRun{
		FeatureID:     f.ID,
		Batch:         batch,
		AgentID:       ev.AgentID,
		Role:          role,
		Model:         model,
		Effort:        effort,
		Calls:         fin.Calls,
		TokensIn:      int64(fin.Usage.Input),
		TokensOut:     int64(fin.Usage.Output),
		CacheRead:     int64(fin.Usage.CacheRead),
		CacheWrite:    int64(fin.Usage.CacheWrite5m + fin.Usage.CacheWrite1h),
		CostUSD:       fin.USD,
		CostEstimated: fin.USDEstimated || fin.USDPartial,
		StartedAt:     start,
		EndedAt:       end,
	})
}
