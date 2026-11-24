package views

import (
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/design"
	"github.com/mananos/nxy.dev/v2/internal/domain"
)

func engFeature() domain.Feature {
	f := baseFeature(domain.PhaseDesigning)
	f.Design = &domain.Design{
		Agenda: []domain.Fork{
			{ID: "B1", Title: "store", Status: domain.ForkDecided},
			{ID: "B2", Title: "format", Status: domain.ForkOpen, Question: "Which format?", Opinion: "A",
				Options: []domain.ForkOption{{Label: "json", Pros: []string{"easy"}, D2: "a -> b"}, {Key: "Z", Label: "csv"}}},
			{ID: "B3", Title: "name", Status: domain.ForkDelegated},
			{ID: "B4", Title: "later", Status: domain.ForkOpen},
		},
		Decisions: []domain.Decision{{Fork: "B1", Text: "sqlite"}},
		Current:   "B2",
		Today:     "x -> y",
	}
	return f
}

func TestBuildEngineerNoDesign(t *testing.T) {
	f := baseFeature(domain.PhasePlanning)
	if _, ok := BuildEngineer(EngineerIn{FeatureIn: FeatureIn{Feature: f, Now: testNow}}); ok {
		t.Fatal("no design must give false")
	}
}

func TestBuildEngineerAgendaAndOptions(t *testing.T) {
	var drawn []string
	e, ok := BuildEngineer(EngineerIn{
		FeatureIn: FeatureIn{Feature: engFeature(), Now: testNow},
		Draw: func(src string) Diagram {
			drawn = append(drawn, src)
			return Diagram{Source: src, Lines: []string{"[" + src + "]"}, Renderer: "d2 0.7.1 txt"}
		},
	})
	if !ok {
		t.Fatal("no engineer")
	}
	want := []EngFork{
		{ID: "B1", Title: "store", Status: ForkDecided, Choice: "sqlite"},
		{ID: "B2", Title: "format", Status: ForkCurrent},
		{ID: "B3", Title: "name", Status: ForkDecided, Choice: "la decide el planner"},
		{ID: "B4", Title: "later", Status: ForkPending},
	}
	if len(e.Agenda) != len(want) {
		t.Fatalf("agenda = %+v", e.Agenda)
	}
	for i := range want {
		if e.Agenda[i] != want[i] {
			t.Errorf("agenda[%d] = %+v, want %+v", i, e.Agenda[i], want[i])
		}
	}
	if e.Current != "B2" || e.Question != "Which format?" || e.Opinion != "A" || e.Other == "" {
		t.Errorf("current %q question %q opinion %q", e.Current, e.Question, e.Opinion)
	}
	if len(e.Options) != 2 || e.Options[0].Key != "A" || e.Options[1].Key != "Z" {
		t.Fatalf("options = %+v", e.Options)
	}
	o := e.Options[0]
	if len(o.Diagram.Lines) != 1 || o.Cons == nil || len(o.Pros) != 1 || o.Action.Planned {
		t.Errorf("option 0 = %+v", o)
	}
	if got := strings.Join(o.Action.Argv, " "); e.AskID != "" || got != "nxy action pick --feature F1 --fork B2 --option json" {
		t.Errorf("nothing waiting: ask %q argv %q", e.AskID, got)
	}
	// with the fork question waiting, enter answers with the label
	w, _ := BuildEngineer(EngineerIn{
		FeatureIn: FeatureIn{Feature: engFeature(), Now: testNow},
		Waiting: []domain.PendingAsk{
			{ID: "A9", FeatureID: "F1", Questions: []domain.AskedQuestion{{Question: design.ForkQuestion("F1", "B2")}}},
			{ID: "A8", FeatureID: "F1", Questions: []domain.AskedQuestion{{Question: "otra"}}},
		},
	})
	if got := strings.Join(w.Options[0].Action.Argv, " "); w.AskID != "A9" || got != "nxy action decide --ask A9 --answer json" {
		t.Errorf("waiting: ask %q argv %q", w.AskID, got)
	}
	if e.Options[1].Diagram.Lines == nil || len(e.Options[1].Diagram.Lines) != 0 {
		t.Errorf("empty d2 must give empty lines: %+v", e.Options[1].Diagram)
	}
	if len(drawn) != 2 || drawn[0] != "x -> y" || drawn[1] != "a -> b" {
		t.Errorf("Draw called with %q, want today and the one d2 only", drawn)
	}
	if len(e.Today.Lines) != 1 {
		t.Errorf("today = %+v", e.Today)
	}
}

func TestBuildEngineerNoCurrentNoToday(t *testing.T) {
	f := engFeature()
	f.Design.Current, f.Design.Today = "", ""
	called := false
	e, _ := BuildEngineer(EngineerIn{
		FeatureIn: FeatureIn{Feature: f, Now: testNow},
		Draw:      func(string) Diagram { called = true; return Diagram{} },
	})
	if len(e.Options) != 0 || e.Options == nil || e.Current != "" || e.Question != "" {
		t.Errorf("no current: %+v", e)
	}
	for _, a := range e.Agenda {
		if a.Status == ForkCurrent {
			t.Errorf("no fork can be current: %+v", a)
		}
	}
	if called || e.Today.Lines == nil || len(e.Today.Lines) != 0 || e.Today.Source != "" {
		t.Errorf("Draw called %v, today %+v", called, e.Today)
	}
}
