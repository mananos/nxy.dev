package views

import (
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/domain"
)

func TestBuildCockpit(t *testing.T) {
	ms := testNow.UnixMilli()
	item := func(id string, phase domain.Phase, upd int64) CockpitItem {
		f := baseFeature(phase)
		f.ID = id
		return CockpitItem{FeatureIn: FeatureIn{Feature: f, CreatedAt: ms - 100_000, UpdatedAt: upd}}
	}
	appr := item("A", domain.PhaseApproval, ms-5000)
	appr.Feature.Next = domain.Action{Kind: domain.AskApproval}
	old := item("B", domain.PhaseBatchFailed, ms-9000)
	old.Feature.Next = domain.Action{Kind: domain.AskDecision}
	old.Failed = []int{2}
	w1 := item("C", domain.PhaseRunningBatch, ms-1000)
	w1.Feature.State.Steps = []domain.Step{{N: 2, Status: domain.StepRunning, StartedAt: ms - 1000}}
	w1.Feature.Next = domain.Action{Kind: domain.DispatchImplementer, Batch: 2}
	w1.Running = []int{2}
	w1.Tool = &ToolUse{Name: "Edit", Arg: "NotaService.java"}
	w2 := item("D", domain.PhaseReviewing, ms-3000)
	done := item("E", domain.PhaseDone, ms-500)
	done.Feature.State.Outcome = domain.OutcomeSuccess
	paused := item("F", domain.PhasePlanning, ms-8000)
	paused.Feature.Paused = true

	c := BuildCockpit(CockpitIn{
		Now:          testNow,
		Features:     []CockpitItem{done, w2, old, paused, w1, appr},
		CostTodayUSD: 3.25,
	})
	var ids []string
	for _, f := range c.Features {
		ids = append(ids, f.ID)
	}
	if got, want := join(ids), "A B C D F E"; got != want {
		t.Fatalf("order = %s, want %s", got, want)
	}
	if c.Totals.Active != 5 || c.Totals.NeedsYou != 2 || c.Totals.CostTodayUSD != 3.25 || c.GeneratedAt != "2026-11-11T12:00:00Z" {
		t.Errorf("totals = %+v", c.Totals)
	}
	a := c.Features[0]
	if a.Decision == nil || a.Decision.Header != "Plan" || a.LastEvent != "Te necesita: aprobar plan" || len(a.Actions) != 1 || a.Actions[0].ID != "pause" {
		t.Errorf("approval card = %+v", a)
	}
	// with its question waiting in the TUI the options come first, each with its decide argv
	appr.Waiting = waitingOf(t, appr.Feature, "A1")
	wc := BuildCockpit(CockpitIn{Now: testNow, Features: []CockpitItem{appr}}).Features[0]
	if len(wc.Actions) != 4 || wc.Actions[3].ID != "pause" || wc.Actions[0].Argv[4] != "A1" {
		t.Errorf("waiting card actions = %+v", wc.Actions)
	}
	b := c.Features[1]
	if b.LastEvent != "Te necesita: decidir, el lote 2 falló" {
		t.Errorf("failed card = %q", b.LastEvent)
	}
	w := c.Features[2]
	if w.LastEvent != "Lote 2 · Edit NotaService.java" || w.Decision != nil || len(w.Running) != 1 || w.Failed == nil || len(w.Actions) != 1 {
		t.Errorf("working card = %+v", w)
	}
	if p := c.Features[4]; len(p.Actions) != 1 || p.Actions[0].ID != "resume" || p.LastEvent != "Pausada" {
		t.Errorf("paused card = %+v", p)
	}
	if d := c.Features[5]; d.Status != StatusDone || d.LastEvent != "Listo" || len(d.Actions) != 0 || d.ClosedAt == "" {
		t.Errorf("done card = %+v", d)
	}
	if c.Worktrees == nil {
		t.Error("worktrees must be an empty list, not null")
	}
}

func join(s []string) string {
	out := ""
	for i, x := range s {
		if i > 0 {
			out += " "
		}
		out += x
	}
	return out
}
