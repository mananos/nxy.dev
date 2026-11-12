package views

import (
	"testing"
	"time"

	"github.com/mananos/nxy.dev/internal/domain"
)

var testNow = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

func baseFeature(phase domain.Phase) domain.Feature {
	return domain.Feature{
		ID:         "F1",
		Repo:       "api",
		ProjectDir: "/srv/dev/api",
		Request:    "export the data\nwith details",
		State:      domain.State{Phase: phase, Batch: 2, Batches: 4},
		Plan:       &domain.Plan{Title: "Export command", Batches: []domain.Batch{{N: 1}, {N: 2}, {N: 3}, {N: 4}}},
		PlanRev:    2,
	}
}

func TestHeaderStageAndStatusPerPhase(t *testing.T) {
	stage := map[domain.Phase]string{
		domain.PhaseDesigning: StageDesign,
		domain.PhasePlanning:  StagePlan, domain.PhaseQuestions: StagePlan, domain.PhaseNoTests: StagePlan,
		domain.PhaseApproval: StagePlan, domain.PhaseChangeNote: StagePlan, domain.PhasePlannerFailed: StagePlan,
		domain.PhaseRepoClash: StagePlan, domain.PhaseBranch: StagePlan,
		domain.PhaseRunningBatch: StageBatches, domain.PhaseVerifyingBatch: StageBatches, domain.PhaseBatchFailed: StageBatches,
		domain.PhaseAffected: StageBatches, domain.PhaseAffectedFix: StageBatches, domain.PhaseAffectedFailed: StageBatches,
		domain.PhaseAffectedUnverified: StageBatches,
		domain.PhaseReviewing:          StageReview, domain.PhaseRefuting: StageReview, domain.PhaseChoosingFindings: StageReview,
		domain.PhaseFixing: StageReview, domain.PhaseFixVerify: StageReview, domain.PhaseFixesFailed: StageReview,
		domain.PhaseDocsCheck: StageClose, domain.PhaseDocs: StageClose, domain.PhaseDocumenting: StageClose,
		domain.PhaseRules: StageClose, domain.PhaseDone: StageClose,
	}
	for _, p := range domain.Phases {
		want, ok := stage[p]
		if !ok {
			t.Errorf("phase %s has no expected stage in the test", p)
			continue
		}
		h := Header(FeatureIn{Feature: baseFeature(p), CreatedAt: testNow.UnixMilli() - 1000, Now: testNow})
		if h.Stage != want {
			t.Errorf("%s: stage = %q, want %q", p, h.Stage, want)
		}
		if h.Phase != p.Label() {
			t.Errorf("%s: phase = %q", p, h.Phase)
		}
		var st Status
		switch p.Public() {
		case domain.Working:
			st = StatusWorking
		case domain.Verifying:
			st = StatusVerifying
		case domain.NeedsYou:
			st = StatusNeedsYou
		case domain.Finished:
			st = StatusDone
		}
		if h.Status != st {
			t.Errorf("%s: status = %q, want %q", p, h.Status, st)
		}
		if (h.StageDetail != "") != (want == StageBatches) {
			t.Errorf("%s: stage_detail = %q", p, h.StageDetail)
		}
	}
}

func TestHeaderFields(t *testing.T) {
	f := baseFeature(domain.PhaseRunningBatch)
	f.Repos = []domain.FeatureRepo{
		{Name: "api", Root: "/srv/dev/api", Checkout: "/srv/dev/api.worktrees/x", Branch: "feat/x", Role: "api"},
		{Name: "web", Root: "/srv/dev/web", Checkout: "/srv/dev/web"},
	}
	created := testNow.UnixMilli() - 90_000
	h := Header(FeatureIn{Feature: f, CreatedAt: created, CostUSD: 1.5, CostEstimated: true, Now: testNow})
	if h.Title != "Export command" || h.StageDetail != "2/4" || h.ElapsedMs != 90_000 || h.CostUSD != 1.5 || !h.CostEstimated || h.ClosedAt != "" {
		t.Errorf("header = %+v", h)
	}
	if len(h.Repos) != 2 || !h.Repos[0].Worktree || h.Repos[1].Worktree || h.Repos[0].Branch != "feat/x" {
		t.Errorf("repos = %+v", h.Repos)
	}

	f.Plan = nil
	if h = Header(FeatureIn{Feature: f, Now: testNow}); h.Title != "export the data" || h.ElapsedMs != UnknownMs {
		t.Errorf("no plan / no creation: title %q elapsed %d", h.Title, h.ElapsedMs)
	}

	f.State.Phase = domain.PhaseDone
	upd := created + 60_000
	h = Header(FeatureIn{Feature: f, CreatedAt: created, UpdatedAt: upd, Now: testNow})
	if h.ElapsedMs != 60_000 || h.ClosedAt != time.UnixMilli(upd).UTC().Format(time.RFC3339) {
		t.Errorf("done header = %+v", h)
	}
	// the implicit repo of an old feature has no name: it takes the folder name, also with backslashes
	g := domain.Feature{ID: "F2", ProjectDir: `C:\work\billing`, State: domain.State{Phase: domain.PhasePlanning}}
	if r := Header(FeatureIn{Feature: g, Now: testNow}).Repos; len(r) != 1 || r[0].Name != "billing" {
		t.Errorf("implicit repo = %+v", r)
	}
}

func TestStepElapsed(t *testing.T) {
	now := testNow
	cases := []struct {
		name string
		st   domain.Step
		want int64
	}{
		{"legacy with no start", domain.Step{N: 1, Status: domain.StepRunning}, UnknownMs},
		{"legacy ended with no start", domain.Step{N: 1, Status: domain.StepGreen, EndedAt: 5}, UnknownMs},
		{"running", domain.Step{N: 1, Status: domain.StepRunning, StartedAt: now.UnixMilli() - 5000}, 5000},
		{"ended", domain.Step{N: 1, Status: domain.StepGreen, StartedAt: 1000, EndedAt: 4000}, 3000},
	}
	for _, c := range cases {
		if got := StepElapsed(c.st, now); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
}

func TestDoing(t *testing.T) {
	run := func(ns ...int) domain.Feature {
		f := baseFeature(domain.PhaseRunningBatch)
		for _, n := range ns {
			f.State.Steps = append(f.State.Steps, domain.Step{N: n, Status: domain.StepRunning})
		}
		return f
	}
	cases := []struct {
		name string
		f    domain.Feature
		tool *ToolUse
		want string
	}{
		{"approval", func() domain.Feature {
			f := baseFeature(domain.PhaseApproval)
			f.Next = domain.Action{Kind: domain.AskApproval}
			return f
		}(), nil, "Te necesita: aprobar plan"},
		{"batch failed", func() domain.Feature {
			f := baseFeature(domain.PhaseBatchFailed)
			f.State.Batch = 3
			f.Next = domain.Action{Kind: domain.AskDecision}
			return f
		}(), nil, "Te necesita: decidir, el lote 3 falló"},
		{"two running", func() domain.Feature {
			f := run(2, 3)
			f.Next = domain.Action{Kind: domain.AwaitBatches, Batches: []int{2, 3}}
			return f
		}(), &ToolUse{Name: "Edit", Arg: "x.go"}, "Esperando lotes 2 y 3"},
		{"one running with a tool", func() domain.Feature {
			f := run(2)
			f.Next = domain.Action{Kind: domain.DispatchImplementer, Batch: 2}
			return f
		}(), &ToolUse{Name: "Edit", Arg: "NotaService.java"}, "Lote 2 · Edit NotaService.java"},
		{"one running with no tool", run(2), nil, "Lote 2 en curso"},
		{"paused", func() domain.Feature {
			f := run(2)
			f.Paused = true
			return f
		}(), nil, "Pausada"},
		{"paused but needs you", func() domain.Feature {
			f := baseFeature(domain.PhaseApproval)
			f.Paused = true
			f.Next = domain.Action{Kind: domain.AskApproval}
			return f
		}(), nil, "Te necesita: aprobar plan"},
		{"done", func() domain.Feature {
			f := baseFeature(domain.PhaseDone)
			f.State.Outcome = domain.OutcomeSuccess
			return f
		}(), nil, "Listo"},
	}
	for _, c := range cases {
		if got := Doing(c.f, c.tool); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
