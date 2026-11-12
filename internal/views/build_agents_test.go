package views

import (
	"reflect"
	"testing"
	"time"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/metrics"
	"github.com/mananos/nxy.dev/internal/pricing"
)

func TestBuildAgentsUnplacedRuns(t *testing.T) {
	now := time.UnixMilli(1_000_000_000)
	nowMs := now.UnixMilli()
	f := domain.Feature{ID: "F1", Request: "x", State: domain.State{Phase: domain.PhaseRunningBatch, Steps: []domain.Step{
		{N: 1, Status: domain.StepGreen, Attempt: 1, StartedAt: nowMs - 9000, EndedAt: nowMs - 5000},
	}}}
	runs := []domain.AgentRun{
		{AgentID: "try1", Role: "implementer", Batch: 1, StartedAt: nowMs - 9000, EndedAt: nowMs - 8000, CostUSD: 1},
		{AgentID: "try2", Role: "implementer", Batch: 1, StartedAt: nowMs - 7000, EndedAt: nowMs - 5000, CostUSD: 2},
		{AgentID: "orphan", Role: "implementer", Batch: 9, StartedAt: nowMs - 4000, EndedAt: nowMs - 3000, CostUSD: 4},
	}
	a := BuildAgents(AgentsIn{FeatureIn: FeatureIn{Feature: f, CreatedAt: nowMs - 30000, Now: now}, Runs: runs})
	if a.Feature.CostUSD != 7 {
		t.Fatalf("header cost = %v", a.Feature.CostUSD)
	}
	var ids []string
	for _, c := range a.Root.Children {
		ids = append(ids, c.ID)
	}
	if !reflect.DeepEqual(ids, []string{"try2", "orphan"}) {
		t.Fatalf("children = %v", ids)
	}
	if ch := a.Root.Children[0].Children; len(ch) != 1 || ch[0].ID != "try1" {
		t.Fatalf("earlier attempt = %+v", ch)
	}
}

func TestBuildAgents(t *testing.T) {
	now := time.UnixMilli(1_000_000_000)
	nowMs := now.UnixMilli()
	f := domain.Feature{
		ID:      "F1",
		Request: "hacer algo",
		Plan: &domain.Plan{Title: "Dos repos", Batches: []domain.Batch{
			{N: 1, Title: "Uno", Repo: "api"},
			{N: 2, Title: "Dos", Repo: "web"},
			{N: 3, Title: "Tres", Repo: "web", Test: domain.BatchTest{Accept: "npm test"}},
			{N: 4, Title: "Cuatro", Repo: "api"},
			{N: 5, Title: "Cinco", Repo: "api"},
		}},
		Lanes: []domain.BatchLane{{Batch: 2, AgentID: "ag-live"}},
		State: domain.State{Phase: domain.PhaseRunningBatch, Batch: 2, Batches: 5, Steps: []domain.Step{
			{N: 1, Repo: "api", Status: domain.StepGreen, StartedAt: nowMs - 9000, EndedAt: nowMs - 5000},
			{N: 2, Repo: "web", Status: domain.StepRunning, Launched: true, StartedAt: nowMs - 2000},
			{N: 3, Repo: "web", Status: domain.StepFailed, Attempt: 1, After: []int{1}, StartedAt: nowMs - 8000, EndedAt: nowMs - 4000},
			{N: 4, Repo: "api", After: []int{3}},
			{N: 5, Repo: "api", Status: domain.StepRunning, StartedAt: 0},
		}},
		Proofs: []domain.Proof{{Batch: 3, Failure: "a\n\n1\n2\n3\n4\n5\n6\n7\n"}},
	}
	runs := []domain.AgentRun{
		{AgentID: "ag-plan", Role: "planner", Model: "opus", StartedAt: nowMs - 20000, EndedAt: nowMs - 15000, TokensIn: 100, TokensOut: 10, CostUSD: 1},
		{AgentID: "ag-scout", Role: "scout", StartedAt: nowMs - 19000, EndedAt: nowMs - 16000, CostUSD: 0.5},
		{AgentID: "ag-b1", Role: "implementer", Batch: 1, StartedAt: nowMs - 9000, EndedAt: nowMs - 5000, TokensIn: 50, CacheRead: 150, CostUSD: 2},
		{AgentID: "ag-fix", Role: "fixer", Batch: 3, StartedAt: nowMs - 4500, EndedAt: nowMs - 4200, CostUSD: 0.25},
	}
	live := []LiveAgent{{
		AgentID: "ag-live", Role: "implementer", Batch: 2, StartedAt: nowMs - 2000,
		Usage: metrics.Finished{USD: 3, Usage: pricing.Usage{Input: 10, Output: 5}},
		Tool:  &ToolUse{Name: "Edit", Arg: "A.java"},
	}}
	main := &MainThread{Model: "sonnet", Usage: metrics.Finished{USD: 4}, Tool: &ToolUse{Name: "Bash", Arg: "go test"}}
	a := BuildAgents(AgentsIn{
		FeatureIn: FeatureIn{Feature: f, CreatedAt: nowMs - 30000, Now: now},
		Runs:      runs, Live: live, Main: main,
		Received: map[string][]MemRef{"ag-b1": {{ID: "K1", Kind: "rule", Text: "x", Cited: true}}},
		Learned:  map[string][]string{"ag-b1": {"learned: algo nuevo"}},
	})

	if a.Root.ID != "main" || a.Root.CurrentTool == nil || a.Root.CurrentTool.Name != "Bash" {
		t.Fatalf("root = %+v", a.Root)
	}
	var ids []string
	for _, c := range a.Root.Children {
		ids = append(ids, c.ID)
	}
	want := []string{"ag-plan", "ag-b1", "ag-live", "batch-3", "batch-4", "batch-5"}
	// the failed batch 3 has no lane/implementer run: it keeps its batch id
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("children = %v, want %v", ids, want)
	}
	if got := a.Root.Children[0].Children; len(got) != 1 || got[0].ID != "ag-scout" {
		t.Fatalf("scouts = %+v", got)
	}
	b1 := a.Root.Children[1]
	if b1.Title != "Uno" || b1.Status != StepGreen || b1.ElapsedMs != 4000 || b1.Attempt != 1 || b1.Attempts != 2 {
		t.Fatalf("b1 = %+v", b1)
	}
	if b1.CacheHitPct == nil || *b1.CacheHitPct != 75 {
		t.Fatalf("cache = %v", b1.CacheHitPct)
	}
	if len(b1.Received) != 1 || b1.Received[0].ID != "K1" || !reflect.DeepEqual(b1.Learned, []string{"algo nuevo"}) {
		t.Fatalf("memory = %+v %+v", b1.Received, b1.Learned)
	}
	b2 := a.Root.Children[2]
	if b2.Status != StepRunning || b2.CurrentTool == nil || b2.CurrentTool.Name != "Edit" || b2.ElapsedMs != 2000 || b2.CostUSD != 3 {
		t.Fatalf("b2 = %+v", b2)
	}
	b3 := a.Root.Children[3]
	if b3.Status != StepFailed || b3.Attempt != 2 || len(b3.Children) != 1 || b3.Children[0].ID != "ag-fix" {
		t.Fatalf("b3 = %+v", b3)
	}
	if b3.Failure == nil || b3.Failure.Test != "npm test" ||
		!reflect.DeepEqual(b3.Failure.Lines, []string{"2", "3", "4", "5", "6", "7"}) {
		t.Fatalf("failure = %+v", b3.Failure)
	}
	if !reflect.DeepEqual(b3.WaitingOn, []int{}) && len(b3.WaitingOn) != 0 {
		t.Fatalf("b3 waiting = %v", b3.WaitingOn)
	}
	b4 := a.Root.Children[4]
	if b4.Status != StepPending || !reflect.DeepEqual(b4.WaitingOn, []int{3}) || b4.CostUSD != 0 || b4.Model != "" {
		t.Fatalf("b4 = %+v", b4)
	}
	if b5 := a.Root.Children[5]; b5.ElapsedMs != UnknownMs {
		t.Fatalf("legacy step elapsed = %d", b5.ElapsedMs)
	}
	if a.Feature.CostUSD != 10.75 { // 4 + 1 + 0.5 + 2 + 3 + 0.25
		t.Fatalf("header cost = %v", a.Feature.CostUSD)
	}
}
