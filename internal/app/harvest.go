package app

import (
	"context"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/memory"
)

// harvest saves what an implementer reports when it stops: its `learned:` lines become learnings
// attached to the files of what it was working on, and `false: A<n>` marks that learning obsolete.
// Each agent is harvested once (Feature.Harvested). A store error never breaks the hook.
func (h Hooks) harvest(ctx context.Context, f domain.Feature, ev HookEvent) {
	if h.Memory == nil || h.Repos == nil || f.ProjectDir == "" {
		return
	}
	rep := memory.ParseReport(ev.AgentResult)
	if len(rep.Learnings) == 0 && len(rep.False) == 0 {
		return
	}
	// Without an agent id there is no way to dedupe: skip rather than count the same report twice.
	if ev.AgentID == "" {
		return
	}
	for _, id := range f.Harvested {
		if id == ev.AgentID {
			return
		}
	}
	repo := h.Repos.Key(f.ProjectDir)
	var allowed []string
	switch f.State.Phase {
	case domain.PhaseRunningBatch:
		if n := f.State.Batch; f.Plan != nil && n >= 1 && n <= len(f.Plan.Batches) {
			allowed = batchFiles(f.Plan.Batches[n-1])
		}
	case domain.PhaseAffectedFix:
		allowed = h.changedPaths(ctx, f)
	case domain.PhaseFixing:
		allowed = findingFiles(f)
	}
	for _, l := range rep.Learnings {
		files := l.Files
		if len(allowed) > 0 {
			files = l.Within(allowed)
		}
		_, _ = h.Memory.AddLearning(ctx, memory.Learning{Repo: repo, Text: strings.TrimSpace(l.Text), Files: files, FeatureID: f.ID})
	}
	if len(rep.False) > 0 {
		_ = h.Memory.MarkObsolete(ctx, repo, rep.False, time.Now().UnixMilli())
	}
	// Mark the agent as harvested only after its learnings were saved.
	_, _, _ = h.mutate(ctx, f.ID, func(g *domain.Feature) bool {
		for _, id := range g.Harvested {
			if id == ev.AgentID {
				return false
			}
		}
		g.Harvested = append(g.Harvested, ev.AgentID)
		return true
	})
}
