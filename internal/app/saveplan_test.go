package app

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/design"
	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/plan"
)

func validPlan() domain.Plan {
	return domain.Plan{
		Title:    "Flag",
		Criteria: []domain.Criterion{{ID: "R1", Text: "adds the flag", Test: domain.TestTDD}},
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

// planningEngineer returns an engineer feature whose design is closed (rule R1 "r", one decision,
// one delegated fork) and that waits for the planner.
func planningEngineer(t *testing.T) (Hooks, *memStore, string) {
	t.Helper()
	h, st, id := engineerFeature(t)
	ctx := context.Background()
	_, _ = h.SaveAgenda(ctx, id, strings.NewReader(`{"forks":["a","b"]}`))
	_, _ = h.ProposeDecision(ctx, id, strings.NewReader(`{"fork":"B1","text":"x","why":"y","scope":"task"}`))
	answer(h, design.ProposeQuestion(id, 1), design.LabelSave)
	if res, err := h.ProposeClose(ctx, id, strings.NewReader(`{"criteria":[{"id":"R1","text":"r"}],"delegate":true}`)); err != nil || len(res.Problems) > 0 {
		t.Fatalf("close: %+v %v", res, err)
	}
	answer(h, design.CloseQuestion(id), design.LabelClose)
	if st.feats[id].State.Phase != domain.PhasePlanning {
		t.Fatalf("phase = %s", st.feats[id].State.Phase)
	}
	return h, st, id
}

func TestSavePlanEngineerMergesDecisions(t *testing.T) {
	h, st, id := planningEngineer(t)
	p := validPlan()
	p.Criteria[0].Text = "r"
	p.Decisions = []domain.Decision{{Scope: domain.ScopeTask, Text: "pick z", Fork: "B2"}}
	res, err := h.SavePlan(context.Background(), id, planReader(t, p))
	if err != nil || len(res.Problems) != 0 {
		t.Fatalf("res = %+v err = %v", res, err)
	}
	ds := st.feats[id].Plan.Decisions
	if len(ds) != 2 || ds[0].ID != "D1" || ds[0].Why != "y" || ds[1].By != domain.ByPlanner || ds[1].ID != "D2" {
		t.Errorf("decisions = %+v", ds)
	}
}

func TestSavePlanEngineerRejects(t *testing.T) {
	h, st, id := planningEngineer(t)
	changed := validPlan()
	changed.Criteria[0].Text = "other"
	if res, _ := h.SavePlan(context.Background(), id, planReader(t, changed)); len(res.Problems) == 0 {
		t.Error("changed rule accepted")
	}
	asks := validPlan()
	asks.Criteria[0].Text = "r"
	asks.Questions = []domain.Question{{Question: "q?", Header: "h", Options: []domain.Option{{Label: "a", Recommended: true}, {Label: "b"}}}}
	if res, _ := h.SavePlan(context.Background(), id, planReader(t, asks)); len(res.Problems) == 0 {
		t.Error("questions accepted")
	}
	own := validPlan()
	own.Criteria[0].Text = "r"
	own.Decisions = []domain.Decision{{Scope: domain.ScopeTask, Text: "pick", Fork: "B1"}}
	if res, _ := h.SavePlan(context.Background(), id, planReader(t, own)); len(res.Problems) == 0 {
		t.Error("decision on a decided fork accepted")
	}
	forged := validPlan()
	forged.Criteria[0].Text = "r"
	forged.Decisions = []domain.Decision{{ID: "D9", Scope: domain.ScopeTask, Text: "pick", Fork: "B2"}}
	if res, _ := h.SavePlan(context.Background(), id, planReader(t, forged)); len(res.Problems) == 0 {
		t.Error("decision with id accepted")
	}
	if st.feats[id].Plan != nil {
		t.Error("a refused plan was stored")
	}
}

func TestSaveGapsBackToDesigning(t *testing.T) {
	h, st, id := planningEngineer(t)
	if res, _ := h.SaveGaps(context.Background(), id, strings.NewReader(`{"gaps":[]}`)); len(res.Problems) == 0 {
		t.Error("empty gaps accepted")
	}
	res, err := h.SaveGaps(context.Background(), id, strings.NewReader(`{"gaps":["where does the cache live?"]}`))
	if err != nil || len(res.Problems) > 0 {
		t.Fatalf("res = %+v err = %v", res, err)
	}
	f := st.feats[id]
	last := f.Design.Agenda[len(f.Design.Agenda)-1]
	if f.State.Phase != domain.PhaseDesigning || f.Design.Closed || last.ID != "B3" || last.Source != domain.SourcePlanner || last.Status != domain.ForkOpen {
		t.Errorf("feature = %+v", f)
	}
	if _, err := h.SaveGaps(context.Background(), id, strings.NewReader(`{"gaps":["x"]}`)); err == nil {
		t.Error("gaps outside planning accepted")
	}
}

func TestPlannerPromptEngineerHasNoConversation(t *testing.T) {
	_, st, id := planningEngineer(t)
	f := st.feats[id]
	f.Design.Refs = []string{"usá este ejemplo"}
	p := plan.PlannerPrompt(f, "/bin/nxy", "")
	for _, want := range []string{"D1", "R1: r", "usá este ejemplo", "plan gaps", "B2: b"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	if strings.Contains(p, "Answers to your earlier questions") || strings.Contains(p, "Ask a question only") {
		t.Error("engineer prompt has the delegate flow")
	}
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
	if got := st.feats[f.ID]; !reflect.DeepEqual(got.State, before.State) || got.Plan != nil || got.PlanRev != 0 || st.version[f.ID] != 1 {
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
