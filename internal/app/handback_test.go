package app

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/domain"
)

// stopOnRead applies a hook (the SubagentStop the hand-back waits for) on the nth read of the active
// feature after it was armed, so the wait is tested with no goroutines.
type stopOnRead struct {
	Store
	at    int
	reads int
	fire  func()
	fired bool
}

func (s *stopOnRead) ActiveFeature(ctx context.Context, repo string) (domain.Feature, bool, error) {
	if s.fire != nil {
		s.reads++
		if s.reads >= s.at {
			fire := s.fire
			s.fire, s.fired = nil, true
			fire()
		}
	}
	return s.Store.ActiveFeature(ctx, repo)
}

func handBackHooks() (Hooks, *memStore, *stopOnRead) {
	h, st := flowHooks(false)
	w := &stopOnRead{Store: st}
	h.Store = w
	h.HandBackWait = HandBackWait{Every: time.Millisecond, Max: 30 * time.Millisecond}
	return h, st, w
}

func handBack(agent string) HookEvent {
	ev := prompt("<agent-message from=\"" + agent + "\">listo")
	ev.PromptFromAgent, ev.HandBackAgent = true, agent
	return ev
}

// armStop makes the stop happen on the 3rd read of the feature.
func (w *stopOnRead) armStop(h Hooks, ev HookEvent) {
	w.at, w.reads, w.fired = 3, 0, false
	w.fire = func() { h.Handle(context.Background(), ev) }
}

func backgroundImplementer(t *testing.T, h Hooks, st *memStore) string {
	t.Helper()
	ctx := context.Background()
	id := runningFeature(t, h, 2)
	h.Handle(ctx, agentEv("PreToolUse", roleImplementer))
	launch := agentEv("PostToolUse", roleImplementer)
	launch.AgentAsync, launch.AsyncAgentID = true, "a1"
	h.Handle(ctx, launch)
	setProof(st, id, domain.Proof{Batch: 1, Red: redRun, Green: greenRun})
	return id
}

func TestHandBackImplementerWaitsForStop(t *testing.T) {
	h, st, w := handBackHooks()
	backgroundImplementer(t, h, st)
	w.armStop(h, implStop())
	r := h.Handle(context.Background(), handBack("a1"))
	if !w.fired || r.Next == nil || r.Next.Feature.Next.Batch != 2 {
		t.Fatalf("fired=%v response = %+v", w.fired, r)
	}
}

func TestHandBackPlannerWaitsForStop(t *testing.T) {
	h, st, w := handBackHooks()
	ctx := context.Background()
	f := startedFeature(t, h, "go")
	h.Handle(ctx, asyncLaunch())
	if _, err := h.SavePlan(ctx, f.ID, planReader(t, validPlan())); err != nil {
		t.Fatal(err)
	}
	w.armStop(h, plannerStop())
	r := h.Handle(ctx, handBack("a1"))
	if !w.fired || r.Next == nil || r.Next.Feature.Next.Kind != domain.AskApproval {
		t.Fatalf("fired=%v response = %+v", w.fired, r)
	}
	if got := st.feats[f.ID]; got.Background != "" {
		t.Errorf("stored = %+v", got)
	}
}

func TestHandBackReviewerWaitsForStop(t *testing.T) {
	h, st, w := handBackHooks()
	h.Exe = "/opt/nxy/nxy"
	ctx := context.Background()
	id := reviewingFeature(t, h, st, false, "api", "base")
	for _, l := range []string{"api", "base"} {
		ev := agentEv("PreToolUse", roleReviewer)
		ev.AgentPrompt = "feature " + id + " review " + l
		h.Handle(ctx, ev)
	}
	h.Handle(ctx, reviewerStopEv(roleReviewer, "a1"))
	w.armStop(h, reviewerStopEv(roleReviewer, "a2"))
	r := h.Handle(ctx, handBack("a2"))
	if !w.fired || r.Next == nil || st.feats[id].State.Phase == domain.PhaseReviewing {
		t.Fatalf("fired=%v response = %+v state %+v", w.fired, r, st.feats[id].State)
	}
}

func TestHandBackTimeoutCutsTheTurnAndStopDelivers(t *testing.T) {
	h, st, _ := handBackHooks()
	ctx := context.Background()
	id := backgroundImplementer(t, h, st)
	r := h.Handle(ctx, handBack("a1"))
	if r.Next != nil || !strings.Contains(r.Context, "cortá el turno") || !strings.Contains(r.Context, "nxy status") {
		t.Fatalf("response = %+v", r)
	}
	h.Handle(ctx, implStop())
	if !st.feats[id].Undelivered {
		t.Fatalf("stop did not leave the result: %+v", st.feats[id])
	}
	r = h.Handle(ctx, mainEv("Stop"))
	if r.Next == nil || r.Next.Feature.Next.Batch != 2 {
		t.Fatalf("stop = %+v", r)
	}
}

func TestHandBackDoesNotWaitWithoutAnNxyAgent(t *testing.T) {
	h, st, _ := handBackHooks()
	h.HandBackWait = HandBackWait{Every: time.Millisecond, Max: time.Hour}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Nothing running at all.
	startedFeature(t, h, "go")
	if r := h.Handle(ctx, handBack("scout1")); r.Context != "" {
		t.Errorf("nothing pending = %+v", r)
	}
	// An implementer in the background with another id: the hand-back is a scout's.
	h, st, _ = handBackHooks()
	h.HandBackWait = HandBackWait{Every: time.Millisecond, Max: time.Hour}
	backgroundImplementer(t, h, st)
	if r := h.Handle(ctx, handBack("scout1")); r.Context != "" {
		t.Errorf("other agent = %+v", r)
	}
	// A hand-back typed by the user, or from a subagent thread, never waits.
	typed := prompt("sigo")
	sub := handBack("a1")
	sub.AgentID = "x"
	h.Handle(ctx, typed)
	h.Handle(ctx, sub)
	if ctx.Err() != nil {
		t.Fatalf("waited: %v", ctx.Err())
	}
}

func TestHandBackParallelLaneStopWhileAnotherRuns(t *testing.T) {
	h, st, w := handBackHooks()
	ctx := context.Background()
	id := parallelFeature(t, h, st)
	for _, n := range []int{2, 3} {
		tu := "t" + strconv.Itoa(n)
		h.Handle(ctx, laneEv("PreToolUse", id, tu, n))
		setProof(st, id, domain.Proof{Batch: n, Red: redRun, Green: greenRun})
		post := laneEv("PostToolUse", id, tu, n)
		post.AgentAsync, post.AsyncAgentID = true, "a"+strconv.Itoa(n)
		h.Handle(ctx, post)
	}
	stop := implStop()
	stop.AgentID = "a3"
	w.armStop(h, stop)
	r := h.Handle(ctx, handBack("a3"))
	if !w.fired || r.Next != nil || !strings.Contains(r.Context, "batch 2 is running") || !strings.Contains(r.Context, id) {
		t.Fatalf("fired=%v response = %+v", w.fired, r)
	}
}

func TestHandBackSingleLaneWithoutIDWaitsAndCuts(t *testing.T) {
	h, st, _ := handBackHooks()
	ctx := context.Background()
	id := runningFeature(t, h, 2)
	h.Handle(ctx, agentEv("PreToolUse", roleImplementer))
	launch := agentEv("PostToolUse", roleImplementer)
	launch.AgentAsync = true
	h.Handle(ctx, launch)
	if l, _ := st.feats[id].LaneOf(1); l.AgentID != "" || l.Background != laneRunning {
		t.Fatalf("lane = %+v", l)
	}
	r := h.Handle(ctx, handBack("other"))
	if r.Next != nil || !strings.Contains(r.Context, "cortá el turno") {
		t.Fatalf("response = %+v", r)
	}
}

func TestHandBackJudgedBatchDoesNotWait(t *testing.T) {
	h, st, _ := handBackHooks()
	h.HandBackWait = HandBackWait{Every: time.Millisecond, Max: time.Hour}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	backgroundImplementer(t, h, st)
	h.Handle(ctx, implStop())
	r := h.Handle(ctx, handBack("a1"))
	if ctx.Err() != nil || r.Next == nil {
		t.Fatalf("response = %+v err %v", r, ctx.Err())
	}
}
