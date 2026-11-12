package views

import (
	"fmt"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/internal/domain"
)

// FeatureIn is what the builders need to know of one feature: the domain value, its db times (epoch
// ms), its cost (the main thread plus its subagents) and the clock.
type FeatureIn struct {
	Feature       domain.Feature
	CreatedAt     int64
	UpdatedAt     int64
	CostUSD       float64
	CostEstimated bool
	Now           time.Time
}

// Header is the summary line of a feature.
func Header(in FeatureIn) FeatureHeader {
	f := in.Feature
	stage := StageOf(f.State.Phase)
	h := FeatureHeader{
		ID:            f.ID,
		Title:         titleOf(f),
		Status:        StatusOf(f.State.Phase),
		Phase:         f.State.Phase.Label(),
		Stage:         stage,
		Repos:         reposOf(f),
		Batch:         f.State.Batch,
		Batches:       f.State.Batches,
		ElapsedMs:     elapsedOf(in),
		CostUSD:       in.CostUSD,
		CostEstimated: in.CostEstimated,
	}
	if stage == StageBatches && f.State.Batches > 0 {
		h.StageDetail = fmt.Sprintf("%d/%d", f.State.Batch, f.State.Batches)
	}
	if f.State.Phase == domain.PhaseDone && in.UpdatedAt > 0 {
		h.ClosedAt = time.UnixMilli(in.UpdatedAt).UTC().Format(time.RFC3339)
	}
	return h
}

// StatusOf maps the public status of a phase to the stable key of the view.
func StatusOf(p domain.Phase) Status {
	switch p.Public() {
	case domain.Verifying:
		return StatusVerifying
	case domain.NeedsYou:
		return StatusNeedsYou
	case domain.Finished:
		return StatusDone
	}
	return StatusWorking
}

// StageOf is the stage of the flow a phase belongs to.
func StageOf(p domain.Phase) string {
	switch p {
	case domain.PhaseDesigning:
		return StageDesign
	case domain.PhaseRunningBatch, domain.PhaseVerifyingBatch, domain.PhaseBatchFailed,
		domain.PhaseAffected, domain.PhaseAffectedFix, domain.PhaseAffectedFailed, domain.PhaseAffectedUnverified:
		return StageBatches
	case domain.PhaseReviewing, domain.PhaseRefuting, domain.PhaseChoosingFindings,
		domain.PhaseFixing, domain.PhaseFixVerify, domain.PhaseFixesFailed:
		return StageReview
	case domain.PhaseDocsCheck, domain.PhaseDocs, domain.PhaseDocumenting, domain.PhaseRules, domain.PhaseDone:
		return StageClose
	}
	return StagePlan // planning, questions, no_tests, approval, change_note, planner_failed, repo_clash, branch
}

func titleOf(f domain.Feature) string {
	if f.Plan != nil && strings.TrimSpace(f.Plan.Title) != "" {
		return strings.TrimSpace(f.Plan.Title)
	}
	first, _, _ := strings.Cut(strings.TrimSpace(f.Request), "\n")
	return strings.TrimSpace(first)
}

func reposOf(f domain.Feature) []RepoRef {
	list := f.RepoList()
	out := make([]RepoRef, 0, len(list))
	for _, r := range list {
		name := r.Name
		if name == "" {
			name = baseName(r.Root)
		}
		path := r.Checkout
		if path == "" {
			path = r.Root
		}
		out = append(out, RepoRef{Name: name, Path: path, Role: r.Role, Branch: r.Branch, Worktree: r.Checkout != "" && r.Checkout != r.Root})
	}
	return out
}

// baseName is the last element of a path written with / or \.
func baseName(p string) string {
	p = strings.TrimRight(p, `/\`)
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

func elapsedOf(in FeatureIn) int64 {
	if in.CreatedAt <= 0 {
		return UnknownMs
	}
	end := in.Now.UnixMilli()
	if in.Feature.State.Phase == domain.PhaseDone && in.UpdatedAt > 0 {
		end = in.UpdatedAt
	}
	return max(end-in.CreatedAt, 0)
}

// StepElapsed is how long a batch has been going (or took); UnknownMs when it has no start.
func StepElapsed(st domain.Step, now time.Time) int64 {
	if st.StartedAt <= 0 {
		return UnknownMs
	}
	end := st.EndedAt
	if end <= 0 {
		end = now.UnixMilli()
	}
	return max(end-st.StartedAt, 0)
}

// Doing is "what is going on" in Spanish, in one line. tool is the last tool of the only agent running, if known.
func Doing(f domain.Feature, tool *ToolUse) string {
	if f.State.Phase == domain.PhaseDone {
		return f.State.Outcome.Label()
	}
	if f.State.Phase.Public() == domain.NeedsYou {
		s := "Te necesita: " + strings.ToLower(f.Next.Label())
		if f.Next.Kind == domain.AskDecision {
			if why := failureOf(f); why != "" {
				s += ", " + why
			}
		}
		return s
	}
	if f.Paused {
		return "Pausada"
	}
	running := f.State.Running()
	if f.Next.Kind == domain.AwaitBatches || len(running) >= 2 {
		ns := running
		if len(ns) == 0 {
			ns = f.Next.Batches
			if len(ns) == 0 && f.Next.Batch > 0 {
				ns = []int{f.Next.Batch}
			}
		}
		if len(ns) == 1 {
			return fmt.Sprintf("Esperando lote %d", ns[0])
		}
		return "Esperando lotes " + domain.JoinInts(ns, "y")
	}
	label := f.Next.Label()
	if len(running) == 1 {
		label = fmt.Sprintf("Lote %d", running[0])
	}
	if t := toolText(tool); t != "" {
		return label + " · " + t
	}
	if len(running) == 1 {
		return label + " en curso"
	}
	return f.State.Phase.Label()
}

func failureOf(f domain.Feature) string {
	switch f.State.Phase {
	case domain.PhaseBatchFailed:
		return fmt.Sprintf("el lote %d falló", f.State.Batch)
	case domain.PhaseAffectedFailed:
		return "los afectados fallaron"
	case domain.PhaseAffectedUnverified:
		return "los afectados no se verificaron"
	case domain.PhaseFixesFailed:
		return "los arreglos fallaron"
	case domain.PhasePlannerFailed:
		return "el planner falló"
	}
	return ""
}

func toolText(t *ToolUse) string {
	if t == nil || t.Name == "" {
		return ""
	}
	return strings.TrimSpace(t.Name + " " + t.Arg)
}
