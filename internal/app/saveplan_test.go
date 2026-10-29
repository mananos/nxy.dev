package app

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
)

func validPlan() domain.Plan {
	return domain.Plan{
		Title: "Flag",
		Rules: []domain.Rule{{ID: "R1", Text: "adds the flag", Test: domain.TestTDD}},
		Batches: []domain.Batch{{
			N: 1, Title: "flag", Covers: []string{"R1"},
			Change: []domain.Change{{At: "cmd/main.go:10", What: "parse the flag"}},
			Test:   domain.BatchTest{Kind: domain.BatchTestNew, Name: "TestFlag", Accept: "go test ./cmd/"},
			Risk:   domain.RiskLow,
		}},
	}
}

func planReader(t *testing.T, p domain.Plan) *bytes.Reader {
	t.Helper()
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(b)
}

func startedFeature(t *testing.T, h Hooks, request string) domain.Feature {
	t.Helper()
	r := h.Handle(context.Background(), prompt("/nxy:feature "+request))
	if r.Next == nil {
		t.Fatalf("not started: %+v", r)
	}
	return r.Next.Feature
}

func agentEv(name, sub string) HookEvent {
	return HookEvent{Name: name, Tool: "Agent", Subagent: sub, ProjectDir: "/p", Cwd: "/p"}
}

func TestSavePlanInvalidLeavesStoreUntouched(t *testing.T) {
	h, st := flowHooks(false)
	f := startedFeature(t, h, "go")
	before := st.feats[f.ID]
	bad := validPlan()
	bad.Title = ""
	res, err := h.SavePlan(context.Background(), f.ID, planReader(t, bad))
	if err != nil || len(res.Problems) == 0 {
		t.Fatalf("res = %+v err = %v", res, err)
	}
	if got := st.feats[f.ID]; got.State != before.State || got.Plan != nil || got.PlanRev != 0 || st.version[f.ID] != 1 {
		t.Errorf("store changed: %+v", got)
	}
}

func TestSavePlanValidGoesToApproval(t *testing.T) {
	h, st := flowHooks(false)
	f := startedFeature(t, h, "go")
	res, err := h.SavePlan(context.Background(), f.ID, planReader(t, validPlan()))
	if err != nil || len(res.Problems) != 0 || res.Rev != 1 {
		t.Fatalf("res = %+v err = %v", res, err)
	}
	got := st.feats[f.ID]
	if got.State.Phase != domain.PhaseApproval || got.PlanRev != 1 || got.Plan == nil || got.Next.Kind != domain.AskApproval {
		t.Errorf("feature = %+v", got)
	}
}

func TestSavePlanQuestionsAndNoTests(t *testing.T) {
	q := validPlan()
	q.Questions = []domain.Question{{Question: "Which?", Header: "Pick", Options: []domain.Option{
		{Label: "A", Description: "a", Recommended: true}, {Label: "B", Description: "b"}}}}
	nt := validPlan()
	nt.NoTests = true
	for _, c := range []struct {
		p    domain.Plan
		want domain.Phase
	}{{q, domain.PhaseQuestions}, {nt, domain.PhaseNoTests}} {
		h, st := flowHooks(false)
		f := startedFeature(t, h, "go")
		if res, err := h.SavePlan(context.Background(), f.ID, planReader(t, c.p)); err != nil || len(res.Problems) != 0 {
			t.Fatalf("res = %+v err = %v", res, err)
		}
		if got := st.feats[f.ID].State.Phase; got != c.want {
			t.Errorf("phase = %s, want %s", got, c.want)
		}
	}
}

func TestSavePlanOutsidePlanningFails(t *testing.T) {
	h, _ := flowHooks(false)
	f := startedFeature(t, h, "go")
	if _, err := h.SavePlan(context.Background(), f.ID, planReader(t, validPlan())); err != nil {
		t.Fatal(err)
	}
	_, err := h.SavePlan(context.Background(), f.ID, planReader(t, validPlan()))
	if err == nil || !strings.Contains(err.Error(), "approval") {
		t.Errorf("err = %v", err)
	}
	if _, err := h.SavePlan(context.Background(), "nope", planReader(t, validPlan())); err == nil {
		t.Error("missing feature accepted")
	}
}

func TestPreToolPlannerGetsInjectedPrompt(t *testing.T) {
	h, _ := flowHooks(false)
	h.Exe = "C:/bin/nxy.exe"
	req := "add  a flag\n  verbatim"
	f := startedFeature(t, h, req)
	r := h.Handle(context.Background(), agentEv("PreToolUse", "nxy:planner"))
	if !strings.Contains(r.Prompt, req) || !strings.Contains(r.Prompt, "C:/bin/nxy.exe") || !strings.Contains(r.Prompt, f.ID) || r.Deny != "" {
		t.Errorf("response = %+v", r)
	}
}

func TestPreToolRolesOutOfOrder(t *testing.T) {
	h, _ := flowHooks(false)
	startedFeature(t, h, "go")
	r := h.Handle(context.Background(), agentEv("PreToolUse", "nxy:implementer"))
	if !strings.Contains(r.Deny, "plan not approved") || !strings.Contains(r.Deny, "Planificar") {
		t.Errorf("implementer: %+v", r)
	}
	r = h.Handle(context.Background(), agentEv("PreToolUse", "nxy:reviewer"))
	if !strings.Contains(r.Deny, "out of order") {
		t.Errorf("reviewer: %+v", r)
	}
	if r := h.Handle(context.Background(), agentEv("PreToolUse", "Explore")); r != (HookResponse{}) {
		t.Errorf("foreign agent: %+v", r)
	}
	sub := agentEv("PreToolUse", "nxy:implementer")
	sub.AgentID = "a1"
	if r := h.Handle(context.Background(), sub); r != (HookResponse{}) {
		t.Errorf("subagent call: %+v", r)
	}
	other := agentEv("PreToolUse", "nxy:implementer")
	other.ProjectDir, other.Cwd = "/q", "/q"
	if r := h.Handle(context.Background(), other); r != (HookResponse{}) {
		t.Errorf("no feature: %+v", r)
	}
}

func TestPostToolPlannerWithoutPlanRetriesOnce(t *testing.T) {
	h, st := flowHooks(false)
	f := startedFeature(t, h, "go")
	r := h.Handle(context.Background(), agentEv("PostToolUse", "nxy:planner"))
	if r.Next == nil || r.Next.Feature.Next.Kind != domain.DispatchPlanner || !r.Next.Feature.Next.Retry {
		t.Fatalf("first return: %+v", r)
	}
	if st.feats[f.ID].State.Phase != domain.PhasePlanning {
		t.Errorf("phase = %s", st.feats[f.ID].State.Phase)
	}
	r = h.Handle(context.Background(), agentEv("PostToolUse", "nxy:planner"))
	if r.Next == nil || r.Next.Feature.State.Phase != domain.PhasePlannerFailed || r.Next.Feature.Next.Kind != domain.AskDecision {
		t.Fatalf("second return: %+v", r)
	}
}

func TestPostToolPlannerAfterSaveRepeatsStep(t *testing.T) {
	h, st := flowHooks(false)
	f := startedFeature(t, h, "go")
	if _, err := h.SavePlan(context.Background(), f.ID, planReader(t, validPlan())); err != nil {
		t.Fatal(err)
	}
	r := h.Handle(context.Background(), agentEv("PostToolUse", "nxy:planner"))
	if r.Next == nil || r.Next.Feature.Next.Kind != domain.AskApproval || r.Next.Feature.Plan == nil {
		t.Errorf("response = %+v", r)
	}
	if st.feats[f.ID].State.Phase != domain.PhaseApproval {
		t.Errorf("phase = %s", st.feats[f.ID].State.Phase)
	}
}
