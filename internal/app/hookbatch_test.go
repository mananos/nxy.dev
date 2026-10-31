package app

import (
	"context"
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
	f.Proof = &p
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
	if got.State.Attempt != 1 || !strings.Contains(got.Proof.Failure, "no red") {
		t.Fatalf("stored = %+v proof = %+v", got.State, got.Proof)
	}
	pre := h.Handle(ctx, agentEv("PreToolUse", roleImplementer))
	if !strings.Contains(pre.Prompt, "no red recorded") || !strings.Contains(pre.Prompt, "Previous attempt failed") {
		t.Errorf("retry prompt lacks the failure: %q", pre.Prompt)
	}
	if st.feats[id].Proof.Green != nil {
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
	if got := st.feats[id].Proof; got.Edits != 1 {
		t.Fatalf("proof = %+v", got)
	}
	h.Handle(ctx, agentEv("PostToolUse", roleImplementer))
	got := st.feats[id]
	if got.State.Attempt != 1 || !strings.Contains(got.Proof.Failure, "written after") {
		t.Errorf("stored = %+v proof = %+v", got.State, got.Proof)
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
	if st.feats[id].Proof != nil {
		t.Errorf("counted: %+v", st.feats[id].Proof)
	}
	if r := h.Handle(ctx, HookEvent{Name: "PreToolUse", Tool: "Write", FilePath: "/p/x.go", ProjectDir: "/p", Cwd: "/p"}); r.Deny == "" {
		t.Errorf("main thread not denied")
	}
}

func TestBackgroundImplementer(t *testing.T) {
	h, st := flowHooks(false)
	ctx := context.Background()
	id := runningFeature(t, h, 2)
	launch := agentEv("PostToolUse", roleImplementer)
	launch.AgentAsync = true
	r := h.Handle(ctx, launch)
	if r.Next != nil || !strings.Contains(r.Context, "implementer is running in the background") || st.feats[id].Background != roleImplementer {
		t.Fatalf("launch = %+v", r)
	}
	if r := h.Handle(ctx, agentEv("PreToolUse", roleImplementer)); !strings.Contains(r.Deny, "background") {
		t.Errorf("second dispatch = %+v", r)
	}
	ss := HookEvent{Name: "SessionStart", Source: "clear", ProjectDir: "/p", Cwd: "/p"}
	if r := h.Handle(ctx, ss); r.Next != nil || !strings.Contains(r.Context, "implementer") || !strings.Contains(r.Notice, "implementer is still running") {
		t.Errorf("session start = %+v", r)
	}
	setProof(st, id, domain.Proof{Batch: 1, Red: redRun, Green: greenRun})
	if r := h.Handle(ctx, implStop()); r != (HookResponse{}) {
		t.Fatalf("stop = %+v", r)
	}
	if got := st.feats[id]; got.Background != "" || !got.Undelivered || got.State.Batch != 2 {
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
