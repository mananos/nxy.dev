package app

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
)

// runningFeature stores a feature in running_batch (batch 1 of n, all TDD) and returns its id.
func runningFeature(t *testing.T, h Hooks, n int) string {
	t.Helper()
	p := validPlan()
	for len(p.Batches) < n {
		b := p.Batches[0]
		b.N = len(p.Batches) + 1
		p.Batches = append(p.Batches, b)
	}
	id, err := h.Store.CreateFeature(context.Background(), domain.Feature{
		Repo: "repo:/p", ProjectDir: "/p", Request: "do it", Plan: &p, PlanRev: 1,
		State: domain.State{Phase: domain.PhaseRunningBatch, Batch: 1, Batches: n},
		Next:  domain.Action{Kind: domain.DispatchImplementer, Batch: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func setProof(st *memStore, id string, p domain.Proof) {
	f := st.feats[id]
	f.SetProof(p)
	st.feats[id] = f
}

var (
	redRun   = &domain.Run{Exit: 1}
	greenRun = &domain.Run{Exit: 0}
)

func implStop() HookEvent {
	return HookEvent{Name: "SubagentStop", AgentType: roleImplementer, AgentID: "a1", ProjectDir: "/p", Cwd: "/p"}
}

func implWrite() HookEvent {
	return HookEvent{Name: "PreToolUse", Tool: "Write", FilePath: "/p/x.go", AgentID: "a1", AgentType: roleImplementer, ProjectDir: "/p", Cwd: "/p"}
}

func TestImplementerReturnTDDGoesToNextBatch(t *testing.T) {
	h, st := flowHooks(false)
	id := runningFeature(t, h, 2)
	setProof(st, id, domain.Proof{Batch: 1, Red: redRun, Green: greenRun})
	r := h.Handle(context.Background(), agentEv("PostToolUse", roleImplementer))
	if r.Next == nil || r.Next.Feature.Next.Kind != domain.DispatchImplementer || r.Next.Feature.Next.Batch != 2 {
		t.Fatalf("response = %+v", r)
	}
	if got := st.feats[id]; got.State.Batch != 2 || got.State.Phase != domain.PhaseRunningBatch || got.State.Attempt != 0 {
		t.Errorf("stored = %+v", got.State)
	}
}

func TestImplementerLastBatchGoesToAffected(t *testing.T) {
	h, st := flowHooks(false)
	id := runningFeature(t, h, 1)
	setProof(st, id, domain.Proof{Batch: 1, Red: redRun, Green: greenRun})
	h.Handle(context.Background(), agentEv("PostToolUse", roleImplementer))
	if got := st.feats[id]; got.State.Phase != domain.PhaseAffected {
		t.Errorf("phase = %s", got.State.Phase)
	}
}

func TestImplementerWithoutRedRetriesOnceThenFails(t *testing.T) {
	h, st := flowHooks(false)
	ctx := context.Background()
	id := runningFeature(t, h, 1)
	h.Handle(ctx, agentEv("PreToolUse", roleImplementer))
	setProof(st, id, domain.Proof{Batch: 1, Green: greenRun})
	r := h.Handle(ctx, agentEv("PostToolUse", roleImplementer))
	if r.Next == nil || !r.Next.Feature.Next.Retry || r.Next.Feature.Next.Kind != domain.DispatchImplementer {
		t.Fatalf("response = %+v", r)
	}
	got := st.feats[id]
	if len(got.State.Steps) != 1 || got.State.Steps[0].Attempt != 1 || !strings.Contains(got.ProofOf(1).Failure, "no red") {
		t.Fatalf("stored = %+v proof = %+v", got.State, got.ProofOf(1))
	}
	pre := h.Handle(ctx, agentEv("PreToolUse", roleImplementer))
	if !strings.Contains(pre.Prompt, "no red recorded") || !strings.Contains(pre.Prompt, "Previous attempt failed") {
		t.Errorf("retry prompt lacks the failure: %q", pre.Prompt)
	}
	if g := st.feats[id]; g.ProofOf(1).Green != nil {
		t.Errorf("green kept across attempts")
	}
	r = h.Handle(ctx, agentEv("PostToolUse", roleImplementer))
	if got := st.feats[id]; got.State.Phase != domain.PhaseBatchFailed || r.Next == nil || r.Next.Feature.Next.Kind != domain.AskDecision {
		t.Errorf("stored = %+v response = %+v", got.State, r)
	}
}

func TestImplementerEditAfterGreenIsRed(t *testing.T) {
	h, st := flowHooks(false)
	ctx := context.Background()
	id := runningFeature(t, h, 1)
	setProof(st, id, domain.Proof{Batch: 1, Red: redRun, Green: greenRun})
	if r := h.Handle(ctx, implWrite()); r != (HookResponse{}) {
		t.Fatalf("write = %+v", r)
	}
	if g := st.feats[id]; g.ProofOf(1).Edits != 1 {
		got := g.ProofOf(1)
		t.Fatalf("proof = %+v", got)
	}
	h.Handle(ctx, agentEv("PostToolUse", roleImplementer))
	got := st.feats[id]
	if len(got.State.Steps) != 1 || got.State.Steps[0].Attempt != 1 || !strings.Contains(got.ProofOf(1).Failure, "written after") {
		t.Errorf("stored = %+v proof = %+v", got.State, got.ProofOf(1))
	}
}

func TestWriteCountingScope(t *testing.T) {
	h, st := flowHooks(false)
	ctx := context.Background()
	id := runningFeature(t, h, 1)
	outside := implWrite()
	outside.FilePath = "/elsewhere/x.go"
	other := implWrite()
	other.AgentType = "nxy:planner"
	h.Handle(ctx, outside)
	h.Handle(ctx, other)
	if g := st.feats[id]; g.Proof != nil || len(g.Proofs) > 0 {
		t.Errorf("counted: %+v %+v", g.Proof, g.Proofs)
	}
	if r := h.Handle(ctx, HookEvent{Name: "PreToolUse", Tool: "Write", FilePath: "/p/x.go", ProjectDir: "/p", Cwd: "/p"}); r.Deny == "" {
		t.Errorf("main thread not denied")
	}
}

func TestBackgroundImplementer(t *testing.T) {
	h, st := flowHooks(false)
	ctx := context.Background()
	id := runningFeature(t, h, 2)
	if r := h.Handle(ctx, agentEv("PreToolUse", roleImplementer)); r.Prompt == "" {
		t.Fatalf("pre = %+v", r)
	}
	launch := agentEv("PostToolUse", roleImplementer)
	launch.AgentAsync = true
	r := h.Handle(ctx, launch)
	if l, _ := st.feats[id].LaneOf(1); r.Next != nil || !strings.Contains(r.Context, "batch 1 is running in the background") || l.Background != laneRunning {
		t.Fatalf("launch = %+v", r)
	}
	if r := h.Handle(ctx, agentEv("PreToolUse", roleImplementer)); r.Deny == "" {
		t.Errorf("second dispatch = %+v", r)
	}
	ss := HookEvent{Name: "SessionStart", Source: "clear", ProjectDir: "/p", Cwd: "/p"}
	if r := h.Handle(ctx, ss); r.Next != nil || !strings.Contains(r.Context, "batch 1") || !strings.Contains(r.Notice, "el lote 1 sigue corriendo") {
		t.Errorf("session start = %+v", r)
	}
	setProof(st, id, domain.Proof{Batch: 1, Red: redRun, Green: greenRun})
	if r := h.Handle(ctx, implStop()); r != (HookResponse{}) {
		t.Fatalf("stop = %+v", r)
	}
	if got := st.feats[id]; len(got.Lanes) != 0 || !got.Undelivered || got.State.Batch != 2 {
		t.Errorf("stored = %+v", got)
	}
	r = h.Handle(ctx, prompt("sigo"))
	if r.Next == nil || r.Next.Feature.Next.Batch != 2 {
		t.Fatalf("delivery = %+v", r)
	}
	if r := h.Handle(ctx, prompt("otra")); r != (HookResponse{}) {
		t.Errorf("second prompt = %+v", r)
	}
}

// parallelFeature is a running feature with batch 1 green and batches 2 and 3 ready to launch.
func parallelFeature(t *testing.T, h Hooks, st *memStore) string {
	t.Helper()
	id := runningFeature(t, h, 3)
	f := st.feats[id]
	f.State = domain.State{Phase: domain.PhaseRunningBatch, Batch: 2, Batches: 3, Steps: []domain.Step{
		{N: 1, Status: domain.StepGreen},
		{N: 2, After: []int{1}, Status: domain.StepRunning},
		{N: 3, After: []int{1}, Status: domain.StepRunning},
	}}
	f.Next = domain.Action{Kind: domain.DispatchImplementer, Batch: 2, Batches: []int{2, 3}}
	st.feats[id] = f
	return id
}

func laneEv(name, id, toolUse string, n int) HookEvent {
	e := agentEv(name, roleImplementer)
	e.ToolUseID = toolUse
	e.AgentPrompt = "feature " + id + " batch " + strconv.Itoa(n)
	return e
}

func TestParallelBatchesDispatchAndReturnInAnyOrder(t *testing.T) {
	h, st := flowHooks(false)
	ctx := context.Background()
	id := parallelFeature(t, h, st)
	if r := h.Handle(ctx, laneEv("PreToolUse", id, "t2", 2)); !strings.Contains(r.Prompt, "batch 2 of") {
		t.Fatalf("pre 2 = %+v", r)
	}
	if r := h.Handle(ctx, laneEv("PreToolUse", id, "tx", 2)); !strings.Contains(r.Deny, "already running") {
		t.Errorf("pre 2 again = %+v", r)
	}
	if r := h.Handle(ctx, laneEv("PreToolUse", id, "ty", 4)); !strings.Contains(r.Deny, "not ready") {
		t.Errorf("pre 4 = %+v", r)
	}
	if r := h.Handle(ctx, laneEv("PreToolUse", id, "t3", 3)); !strings.Contains(r.Prompt, "batch 3 of") {
		t.Fatalf("pre 3 = %+v", r)
	}
	setProof(st, id, domain.Proof{Batch: 2, Red: redRun, Green: greenRun})
	setProof(st, id, domain.Proof{Batch: 3, Red: redRun, Green: greenRun})
	r := h.Handle(ctx, laneEv("PostToolUse", id, "t3", 3))
	if r.Next == nil || r.Next.Feature.Next.Kind != domain.AwaitBatches || !reflect.DeepEqual(st.feats[id].State.Running(), []int{2}) {
		t.Fatalf("post 3 = %+v state %+v", r, st.feats[id].State)
	}
	r = h.Handle(ctx, laneEv("PostToolUse", id, "t2", 2))
	if got := st.feats[id]; r.Next == nil || got.State.Phase != domain.PhaseAffected || len(got.Lanes) != 0 {
		t.Fatalf("post 2 = %+v state %+v", r, got.State)
	}
}

func TestParallelBatchesBackgroundStopByPrompt(t *testing.T) {
	h, st := flowHooks(false)
	ctx := context.Background()
	id := parallelFeature(t, h, st)
	h.Handle(ctx, laneEv("PreToolUse", id, "t2", 2))
	h.Handle(ctx, laneEv("PreToolUse", id, "t3", 3))
	setProof(st, id, domain.Proof{Batch: 2, Red: redRun, Green: greenRun})
	setProof(st, id, domain.Proof{Batch: 3, Red: redRun, Green: greenRun})
	for _, n := range []int{2, 3} {
		post := laneEv("PostToolUse", id, "t"+strconv.Itoa(n), n)
		post.AgentAsync = true
		if r := h.Handle(ctx, post); !strings.Contains(r.Context, "running in the background") {
			t.Fatalf("launch %d = %+v", n, r)
		}
	}
	stop := func(n int) HookEvent {
		e := implStop()
		e.AgentID = "a" + strconv.Itoa(n)
		e.AgentPrompt = "feature " + id + " batch " + strconv.Itoa(n)
		return e
	}
	h.Handle(ctx, stop(3))
	if got := st.feats[id]; got.Undelivered || !reflect.DeepEqual(got.State.Running(), []int{2}) {
		t.Fatalf("after stop 3: undelivered=%v state %+v", got.Undelivered, got.State)
	}
	h.Handle(ctx, stop(2))
	if got := st.feats[id]; !got.Undelivered || got.State.Phase != domain.PhaseAffected {
		t.Fatalf("after stop 2: undelivered=%v state %+v", got.Undelivered, got.State)
	}
}

func TestParallelBatchesUnnumberedStop(t *testing.T) {
	launch := func(t *testing.T, ns ...int) (Hooks, *memStore, string) {
		h, st := flowHooks(false)
		ctx := context.Background()
		id := parallelFeature(t, h, st)
		for _, n := range ns {
			tu := "t" + strconv.Itoa(n)
			h.Handle(ctx, laneEv("PreToolUse", id, tu, n))
			setProof(st, id, domain.Proof{Batch: n, Red: redRun, Green: greenRun})
			post := laneEv("PostToolUse", id, tu, n)
			post.AgentAsync, post.AsyncAgentID = true, "a"+strconv.Itoa(n)
			h.Handle(ctx, post)
		}
		return h, st, id
	}
	stop := func(agent string) HookEvent {
		e := implStop()
		e.AgentID, e.AgentPrompt = agent, ""
		return e
	}
	ctx := context.Background()

	// Two launched and no prompt: an unknown agent judges nothing; a known agent id judges its batch.
	h, st, id := launch(t, 2, 3)
	h.Handle(ctx, stop("zzz"))
	if got := st.feats[id]; !reflect.DeepEqual(got.State.Running(), []int{2, 3}) || got.Undelivered {
		t.Fatalf("unknown stop judged: %+v", got.State)
	}
	h.Handle(ctx, stop("a3"))
	if got := st.feats[id]; !reflect.DeepEqual(got.State.Running(), []int{2}) {
		t.Fatalf("stop by agent id: %+v", got.State)
	}

	// One launched and no prompt: it is judged.
	h, st, id = launch(t, 2)
	h.Handle(ctx, stop("zzz"))
	if got := st.feats[id]; reflect.DeepEqual(got.State.Running(), []int{2, 3}) {
		t.Fatalf("single launched not judged: %+v", got.State)
	}
}

func TestParallelBatchesRelaunchOnNewProcess(t *testing.T) {
	h, st := flowHooks(false)
	ctx := context.Background()
	id := parallelFeature(t, h, st)
	h.Handle(ctx, laneEv("PreToolUse", id, "t2", 2))
	h.Handle(ctx, laneEv("PreToolUse", id, "t3", 3))
	h.Handle(ctx, HookEvent{Name: "SessionStart", Source: "startup", ProjectDir: "/p", Cwd: "/p"})
	got := st.feats[id]
	if len(got.Lanes) != 0 || !reflect.DeepEqual(got.State.Unlaunched(), []int{2, 3}) || got.Next.Kind != domain.DispatchImplementer {
		t.Fatalf("stored = %+v next %+v", got.State, got.Next)
	}
}
