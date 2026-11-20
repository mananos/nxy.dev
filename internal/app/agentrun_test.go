package app

import (
	"context"
	"testing"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/domain"
	"github.com/mananos/nxy.dev/v2/internal/host/claudecode/transcript"
	"github.com/mananos/nxy.dev/v2/internal/metrics"
	"github.com/mananos/nxy.dev/v2/internal/pricing"
)

type fakeAgentFiles struct {
	res transcript.FileResult
	ok  bool
}

func (f fakeAgentFiles) Read(string, string) (transcript.FileResult, bool) { return f.res, f.ok }

type fakeRuns struct{ rows map[string]domain.AgentRun }

func (r *fakeRuns) RecordAgentRun(_ context.Context, x domain.AgentRun) error {
	r.rows[x.FeatureID+"/"+x.AgentID] = x
	return nil
}

func i64(v int64) *int64 { return &v }

func runCall(model string, effort *string, in, out float64) metrics.Call {
	u := pricing.EmptyUsage()
	u.Input, u.Output, u.CacheRead, u.CacheWrite5m, u.CacheWrite1h = in, out, 7, 3, 2
	return metrics.Call{Key: "k", Model: model, Usage: u, Effort: effort}
}

func runHooks(res transcript.FileResult) (Hooks, *memStore, *fakeRuns) {
	h, st := flowHooks(false)
	runs := &fakeRuns{rows: map[string]domain.AgentRun{}}
	h.AgentFiles, h.Runs = fakeAgentFiles{res: res, ok: true}, runs
	h.Roles = fakeRoles{"implementer": {"m", "high"}}
	h.Clock = &fixedClock{t: time.UnixMilli(5000)}
	return h, st, runs
}

func TestRecordAgentRunImplementerByLane(t *testing.T) {
	eff := "xhigh"
	res := transcript.FileResult{
		Calls:   []metrics.Call{runCall("mystery-model", nil, 10, 5), runCall("mystery-model", &eff, 20, 6)},
		FirstTs: i64(1000), LastTs: i64(2000),
	}
	h, st, runs := runHooks(res)
	id := runningFeature(t, h, 2)
	f := st.feats[id]
	f.State.Steps = []domain.Step{
		{N: 1, Status: domain.StepRunning, Launched: true},
		{N: 2, Status: domain.StepRunning, Launched: true},
	}
	f.Lanes = []domain.BatchLane{{Batch: 1, AgentID: "a1"}, {Batch: 2, AgentID: "a2"}}
	st.feats[id] = f
	ev := implStop()
	ev.AgentID = "a2"
	h.recordAgentRun(context.Background(), ev)
	got, ok := runs.rows[id+"/a2"]
	if !ok {
		t.Fatalf("no row: %+v", runs.rows)
	}
	if got.Batch != 2 || got.Role != "implementer" || got.Model != "mystery-model" || got.Effort != eff {
		t.Errorf("row = %+v", got)
	}
	if got.Calls != 2 || got.TokensIn != 30 || got.TokensOut != 11 || got.CacheRead != 14 || got.CacheWrite != 10 {
		t.Errorf("totals = %+v", got)
	}
	if !got.CostEstimated || got.StartedAt != 1000 || got.EndedAt != 2000 {
		t.Errorf("estimated/times = %+v", got)
	}
	// A repeated stop keeps one row.
	h.recordAgentRun(context.Background(), ev)
	if len(runs.rows) != 1 {
		t.Errorf("rows = %d", len(runs.rows))
	}
}

func TestRecordAgentRunCost(t *testing.T) {
	for _, c := range []struct {
		model     string
		estimated bool
	}{{"claude-sonnet-4-6", false}, {"claude-sonnet-4-9", true}} {
		res := transcript.FileResult{Calls: []metrics.Call{runCall(c.model, nil, 10, 5)}}
		h, _, runs := runHooks(res)
		id := runningFeature(t, h, 1)
		h.recordAgentRun(context.Background(), implStop())
		got := runs.rows[id+"/a1"]
		if got.CostUSD <= 0 || got.CostEstimated != c.estimated {
			t.Errorf("%s: cost %v estimated %v, want estimated %v", c.model, got.CostUSD, got.CostEstimated, c.estimated)
		}
		if !c.estimated {
			// 10 in, 5 out, 7 cache read, 3 + 2 cache write at claude-sonnet-4-6 prices, per million tokens.
			want := (10*3 + 5*15 + 7*0.3 + 3*3.75 + 2*6) / 1e6
			if d := got.CostUSD - want; d > 1e-9 || d < -1e-9 {
				t.Errorf("cost = %v, want %v", got.CostUSD, want)
			}
		}
	}
}

func TestRecordAgentRunImplementerByPrompt(t *testing.T) {
	h, st, runs := runHooks(transcript.FileResult{})
	id := runningFeature(t, h, 2)
	f := st.feats[id]
	f.State.Steps = []domain.Step{
		{N: 1, Status: domain.StepGreen},
		{N: 2, Status: domain.StepRunning, Launched: true},
	}
	st.feats[id] = f
	ev := implStop()
	ev.AgentPrompt = "feature " + id + " batch 2"
	h.recordAgentRun(context.Background(), ev)
	got := runs.rows[id+"/a1"]
	if got.Batch != 2 {
		t.Errorf("batch = %d", got.Batch)
	}
	// Missing usage: zero totals, now for both times, effort from the role table.
	if got.Calls != 0 || got.StartedAt != 5000 || got.EndedAt != 5000 || got.Effort != "high" {
		t.Errorf("row = %+v", got)
	}
}

func TestRecordAgentRunFixerReviewerScout(t *testing.T) {
	h, st, runs := runHooks(transcript.FileResult{})
	id := runningFeature(t, h, 1)
	f := st.feats[id]
	f.State.Phase = domain.PhaseFixing
	st.feats[id] = f
	ev := implStop()
	h.recordAgentRun(context.Background(), ev)
	if got := runs.rows[id+"/a1"]; got.Role != "fixer" || got.Batch != 0 {
		t.Errorf("fixer = %+v", got)
	}
	for _, c := range []struct{ typ, role string }{{"nxy:reviewer", "reviewer"}, {"nxy:scout", "scout"}} {
		e := HookEvent{Name: "SubagentStop", AgentType: c.typ, AgentID: c.role, ProjectDir: "/p", Cwd: "/p"}
		h.recordAgentRun(context.Background(), e)
		if got := runs.rows[id+"/"+c.role]; got.Role != c.role || got.Batch != 0 {
			t.Errorf("%s = %+v", c.role, got)
		}
	}
}

func TestRecordAgentRunNothingToRecord(t *testing.T) {
	h, _, runs := runHooks(transcript.FileResult{})
	h.recordAgentRun(context.Background(), implStop()) // no feature
	h2, _, runs2 := runHooks(transcript.FileResult{})
	runningFeature(t, h2, 1)
	noID := implStop()
	noID.AgentID = ""
	h2.recordAgentRun(context.Background(), noID)
	h2.Runs = nil
	h2.recordAgentRun(context.Background(), implStop())
	if len(runs.rows) != 0 || len(runs2.rows) != 0 {
		t.Errorf("rows = %v %v", runs.rows, runs2.rows)
	}
}
