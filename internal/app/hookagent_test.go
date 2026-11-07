package app

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
)

func asyncLaunch() HookEvent {
	ev := agentEv("PostToolUse", rolePlanner)
	ev.AgentAsync = true
	return ev
}

func plannerStop() HookEvent {
	return HookEvent{Name: "SubagentStop", AgentType: rolePlanner, AgentID: "a1", ProjectDir: "/p", Cwd: "/p"}
}

func mainEv(name string) HookEvent {
	return HookEvent{Name: name, ProjectDir: "/p", Cwd: "/p"}
}

func TestBackgroundLaunchWaits(t *testing.T) {
	h, st := flowHooks(false)
	f := startedFeature(t, h, "go")
	r := h.Handle(context.Background(), asyncLaunch())
	if r.Next != nil || !strings.Contains(r.Context, "background") || !strings.Contains(r.Context, f.ID) {
		t.Fatalf("response = %+v", r)
	}
	got := st.feats[f.ID]
	if got.State.Phase != domain.PhasePlanning || got.State.Attempt != 0 || got.Background != rolePlanner {
		t.Errorf("stored = %+v", got)
	}
}

func TestBackgroundPlanDeliveredOnce(t *testing.T) {
	h, st := flowHooks(false)
	ctx := context.Background()
	f := startedFeature(t, h, "go")
	h.Handle(ctx, asyncLaunch())
	if _, err := h.SavePlan(ctx, f.ID, planReader(t, validPlan())); err != nil {
		t.Fatal(err)
	}
	if r := h.Handle(ctx, plannerStop()); r != (HookResponse{}) {
		t.Fatalf("subagent stop = %+v", r)
	}
	if got := st.feats[f.ID]; got.Background != "" || !got.Undelivered {
		t.Errorf("stored = %+v", got)
	}
	r := h.Handle(ctx, prompt("sigo"))
	if r.Next == nil || r.Next.Feature.Next.Kind != domain.AskApproval {
		t.Fatalf("delivery = %+v", r)
	}
	if r := h.Handle(ctx, prompt("otra")); r != (HookResponse{}) {
		t.Errorf("second prompt = %+v", r)
	}
	if r := h.Handle(ctx, mainEv("Stop")); r != (HookResponse{}) {
		t.Errorf("stop = %+v", r)
	}
}

func TestBackgroundPlannerWithoutPlanRetries(t *testing.T) {
	h, st := flowHooks(false)
	ctx := context.Background()
	f := startedFeature(t, h, "go")
	h.Handle(ctx, asyncLaunch())
	h.Handle(ctx, plannerStop())
	if got := st.feats[f.ID]; got.State.Attempt != 1 {
		t.Errorf("attempt = %d", got.State.Attempt)
	}
	r := h.Handle(ctx, mainEv("Stop"))
	if r.Next == nil || r.Next.Feature.Next.Kind != domain.DispatchPlanner || !r.Next.Feature.Next.Retry {
		t.Fatalf("delivery = %+v", r)
	}
}

func TestForegroundSubagentStopIgnored(t *testing.T) {
	h, st := flowHooks(false)
	ctx := context.Background()
	f := startedFeature(t, h, "go")
	before := st.feats[f.ID]
	if r := h.Handle(ctx, plannerStop()); r != (HookResponse{}) {
		t.Fatalf("response = %+v", r)
	}
	if !reflect.DeepEqual(st.feats[f.ID].State, before.State) || st.feats[f.ID].Undelivered {
		t.Errorf("store changed: %+v", st.feats[f.ID])
	}
	if _, err := h.SavePlan(ctx, f.ID, planReader(t, validPlan())); err != nil {
		t.Fatal(err)
	}
	r := h.Handle(ctx, agentEv("PostToolUse", rolePlanner))
	if r.Next == nil || r.Next.Feature.Next.Kind != domain.AskApproval {
		t.Errorf("foreground checkpoint = %+v", r)
	}
}

func TestSubagentStopOfStoppedFeature(t *testing.T) {
	h, st := flowHooks(false)
	ctx := context.Background()
	f := startedFeature(t, h, "go")
	h.Handle(ctx, asyncLaunch())
	if _, _, err := h.StopFeature(ctx, "/p"); err != nil {
		t.Fatal(err)
	}
	v := st.version[f.ID]
	if r := h.Handle(ctx, plannerStop()); r != (HookResponse{}) {
		t.Fatalf("response = %+v", r)
	}
	if st.version[f.ID] != v {
		t.Errorf("store changed")
	}
}

func TestPlannerEndedBeforeAsyncLaunch(t *testing.T) {
	h, st := flowHooks(false)
	ctx := context.Background()
	f := startedFeature(t, h, "go")
	h.Handle(ctx, plannerStop())
	r := h.Handle(ctx, asyncLaunch())
	if strings.Contains(r.Context, "background") || r.Next == nil {
		t.Fatalf("response = %+v", r)
	}
	got := st.feats[f.ID]
	if got.Background != "" || got.State.Attempt != 1 {
		t.Errorf("stored = %+v", got)
	}
}

func TestSubagentStopOfOtherTypeIgnored(t *testing.T) {
	h, st := flowHooks(false)
	ctx := context.Background()
	f := startedFeature(t, h, "go")
	h.Handle(ctx, asyncLaunch())
	v := st.version[f.ID]
	ev := plannerStop()
	ev.AgentType = roleImplementer
	if r := h.Handle(ctx, ev); r != (HookResponse{}) {
		t.Fatalf("response = %+v", r)
	}
	if st.version[f.ID] != v || st.feats[f.ID].Background != rolePlanner {
		t.Errorf("store changed: %+v", st.feats[f.ID])
	}
}

func TestDeliverPendingGuards(t *testing.T) {
	h, st := flowHooks(false)
	ctx := context.Background()
	f := startedFeature(t, h, "go")
	if _, _, err := h.mutate(ctx, f.ID, func(g *domain.Feature) bool { g.Undelivered = true; return true }); err != nil {
		t.Fatal(err)
	}
	sub := prompt("x")
	sub.AgentID = "a1"
	if r := h.deliverPending(ctx, sub, HookResponse{}); r.Next != nil || !st.feats[f.ID].Undelivered {
		t.Errorf("subagent thread claimed: %+v", r)
	}
	if r := h.deliverPending(ctx, prompt("x"), HookResponse{Deny: "no"}); r.Next != nil || !st.feats[f.ID].Undelivered {
		t.Errorf("deny claimed: %+v", r)
	}
}

type panicStore struct{ Store }

func (panicStore) ActiveFeature(context.Context, string) (domain.Feature, bool, error) {
	panic("store used")
}

func TestPostToolBashSkipsStore(t *testing.T) {
	h, _ := flowHooks(false)
	h.Store = panicStore{h.Store}
	for _, tool := range []string{"Bash", "PowerShell"} {
		ev := HookEvent{Name: "PostToolUse", Tool: tool, ProjectDir: "/p", Cwd: "/p"}
		if r := h.Handle(context.Background(), ev); r != (HookResponse{}) {
			t.Errorf("%s: %+v", tool, r)
		}
	}
}
