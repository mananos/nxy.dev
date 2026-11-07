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
	mrs := h.memRepos(f)
	def := 0 // the repo of the batch the implementer works on
	var allowed []string
	switch f.State.Phase {
	case domain.PhaseRunningBatch:
		n := batchOfPrompt(ev.AgentPrompt)
		if n == 0 {
			n = f.State.Batch
		}
		if f.Plan != nil && n >= 1 && n <= len(f.Plan.Batches) {
			allowed = batchFiles(f.Plan.Batches[n-1])
			def = max(0, memRepoIndex(mrs, f.Plan.Batches[n-1].Repo))
		}
	case domain.PhaseAffectedFix:
		allowed = h.changedPaths(ctx, f)
	case domain.PhaseFixing:
		allowed = findingFiles(f)
	}
	if len(mrs) > 1 || (f.Plan != nil && len(f.Plan.Repos) > 0) {
		// The implementer may report a path with or without the repo name.
		for _, a := range append([]string(nil), allowed...) {
			if i, rel := h.splitLabel(f, mrs, a); i >= 0 && rel != a {
				allowed = append(allowed, rel)
			}
		}
	}
	for _, l := range rep.Learnings {
		files := l.Files
		if len(allowed) > 0 {
			files = l.Within(allowed)
		}
		i, rels := h.placeFiles(f, mrs, files, def)
		_, _ = h.Memory.AddLearning(ctx, memory.Learning{Repo: mrs[i].Key, Text: strings.TrimSpace(l.Text), Files: rels, FeatureID: f.ID})
	}
	if len(rep.False) > 0 {
		now := time.Now().UnixMilli()
		for key, ids := range resolveFalse(mrs, rep.False, f.Delivered) {
			_ = h.Memory.MarkObsolete(ctx, key, ids, now)
		}
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
