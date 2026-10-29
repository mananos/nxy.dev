package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/plan"
)

type fakeViewer struct{ shown []string }

func (v *fakeViewer) Show(_ context.Context, f domain.Feature) (string, error) {
	v.shown = append(v.shown, f.ID)
	return "/plans/" + f.ID + ".md", nil
}
func (v *fakeViewer) Discard(string) error { return nil }

func askPost(f domain.Feature, q, a string) HookEvent {
	return HookEvent{Name: "PostToolUse", Tool: "AskUserQuestion", ProjectDir: "/p", Cwd: "/p",
		Questions: []string{q}, Answers: map[string]string{q: a}}
}

func approvalFeature(t *testing.T, h Hooks, p domain.Plan) domain.Feature {
	t.Helper()
	f := startedFeature(t, h, "go")
	if _, err := h.SavePlan(context.Background(), f.ID, planReader(t, p)); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestApproveStartsBatchOne(t *testing.T) {
	h, st := flowHooks(false)
	f := approvalFeature(t, h, validPlan())
	r := h.Handle(context.Background(), askPost(f, plan.ApprovalQuestion(f.ID, 1), plan.Approve))
	if r.Next == nil || r.Next.Feature.State.Phase != domain.PhaseRunningBatch || r.Next.Feature.Next.Kind != domain.DispatchImplementer || r.Next.Feature.Next.Batch != 1 {
		t.Fatalf("response = %+v", r)
	}
	if st.feats[f.ID].State.Batch != 1 {
		t.Errorf("state = %+v", st.feats[f.ID].State)
	}
}

func TestChangeThenNoteReachesPlanner(t *testing.T) {
	h, st := flowHooks(false)
	f := approvalFeature(t, h, validPlan())
	r := h.Handle(context.Background(), askPost(f, plan.ApprovalQuestion(f.ID, 1), plan.Change))
	if r.Next == nil || r.Next.Feature.State.Phase != domain.PhaseChangeNote || r.Next.Feature.Next.Kind != domain.AskChangeNote {
		t.Fatalf("change: %+v", r)
	}
	note := "use  two\nbatches "
	r = h.Handle(context.Background(), prompt(note))
	if r.Next == nil || r.Next.Feature.State.Phase != domain.PhasePlanning || r.Next.Feature.Next.Kind != domain.DispatchPlanner {
		t.Fatalf("note: %+v", r)
	}
	if got := st.feats[f.ID].ChangeNotes; len(got) != 1 || got[0] != note {
		t.Errorf("notes = %q", got)
	}
	r = h.Handle(context.Background(), agentEv("PreToolUse", "nxy:planner"))
	if !strings.Contains(r.Prompt, note) {
		t.Errorf("planner prompt lacks the note: %q", r.Prompt)
	}
}

func TestFreeTextAnswerIsTheNote(t *testing.T) {
	h, st := flowHooks(false)
	f := approvalFeature(t, h, validPlan())
	r := h.Handle(context.Background(), askPost(f, plan.ApprovalQuestion(f.ID, 1), "split batch 1"))
	if r.Next == nil || r.Next.Feature.State.Phase != domain.PhasePlanning {
		t.Fatalf("response = %+v", r)
	}
	if got := st.feats[f.ID].ChangeNotes; len(got) != 1 || got[0] != "split batch 1" {
		t.Errorf("notes = %q", got)
	}
}

func TestViewFullShowsAndAsksAgain(t *testing.T) {
	h, st := flowHooks(false)
	v := &fakeViewer{}
	h.Viewer = v
	f := approvalFeature(t, h, validPlan())
	r := h.Handle(context.Background(), askPost(f, plan.ApprovalQuestion(f.ID, 1), plan.ViewFull))
	if len(v.shown) != 1 || v.shown[0] != f.ID {
		t.Errorf("shown = %v", v.shown)
	}
	if r.Next == nil || r.Next.Feature.Next.Kind != domain.AskApproval || st.feats[f.ID].State.Phase != domain.PhaseApproval {
		t.Errorf("response = %+v", r)
	}
}

func TestPrefilledNxyAnswersDenied(t *testing.T) {
	h, _ := flowHooks(false)
	f := approvalFeature(t, h, validPlan())
	ev := HookEvent{Name: "PreToolUse", Tool: "AskUserQuestion", ProjectDir: "/p", Cwd: "/p",
		Questions: []string{plan.ApprovalQuestion(f.ID, 1)}, InputHasAnswers: true}
	if r := h.Handle(context.Background(), ev); !strings.Contains(r.Deny, "answered by the user") {
		t.Errorf("response = %+v", r)
	}
	ev.InputHasAnswers = false
	if r := h.Handle(context.Background(), ev); r != (HookResponse{}) {
		t.Errorf("clean ask: %+v", r)
	}
	ev.InputHasAnswers, ev.Questions = true, []string{"Tabs or spaces?"}
	if r := h.Handle(context.Background(), ev); r != (HookResponse{}) {
		t.Errorf("foreign question: %+v", r)
	}
}

func TestSubagentAnswerDoesNotCount(t *testing.T) {
	h, st := flowHooks(false)
	f := approvalFeature(t, h, validPlan())
	ev := askPost(f, plan.ApprovalQuestion(f.ID, 1), plan.Approve)
	ev.AgentID = "a1"
	if r := h.Handle(context.Background(), ev); r != (HookResponse{}) {
		t.Errorf("response = %+v", r)
	}
	if st.feats[f.ID].State.Phase != domain.PhaseApproval {
		t.Errorf("phase = %s", st.feats[f.ID].State.Phase)
	}
}

func TestOldRevisionIsIgnoredWithNote(t *testing.T) {
	h, st := flowHooks(false)
	f := approvalFeature(t, h, validPlan())
	r := h.Handle(context.Background(), askPost(f, plan.ApprovalQuestion(f.ID, 7), plan.Approve))
	if r.Next == nil || r.Next.Note != "that was plan r7; current is r1" {
		t.Fatalf("response = %+v", r)
	}
	if st.feats[f.ID].State.Phase != domain.PhaseApproval {
		t.Errorf("phase = %s", st.feats[f.ID].State.Phase)
	}
}

type failStore struct{ Store }

func (failStore) Transition(context.Context, string, func(domain.Feature) (domain.Feature, error)) error {
	return errors.New("db down")
}

func TestFailedApprovalAsksAgain(t *testing.T) {
	h, _ := flowHooks(false)
	f := approvalFeature(t, h, validPlan())
	h.Store = failStore{h.Store}
	r := h.Handle(context.Background(), askPost(f, plan.ApprovalQuestion(f.ID, 1), plan.Approve))
	if r.Next == nil || !strings.Contains(r.Next.Note, "ask again") || r.Next.Feature.ID != f.ID {
		t.Fatalf("response = %+v", r)
	}
}

func TestChangeNoteIgnoresSlashCommands(t *testing.T) {
	h, st := flowHooks(false)
	f := approvalFeature(t, h, validPlan())
	h.Handle(context.Background(), askPost(f, plan.ApprovalQuestion(f.ID, 1), plan.Change))
	if r := h.Handle(context.Background(), prompt("/clear")); r.Next != nil {
		t.Errorf("slash prompt consumed: %+v", r)
	}
	if got := st.feats[f.ID]; got.State.Phase != domain.PhaseChangeNote || len(got.ChangeNotes) != 0 {
		t.Errorf("feature = %+v", got)
	}
}

func TestPlanQuestionsAnsweredInTwoRounds(t *testing.T) {
	p := validPlan()
	opts := []domain.Option{{Label: "A", Description: "a", Recommended: true}, {Label: "B", Description: "b"}}
	p.Questions = []domain.Question{
		{Question: "First?", Header: "One", Options: opts},
		{Question: "Second?", Header: "Two", Options: opts},
	}
	h, st := flowHooks(false)
	f := approvalFeature(t, h, p)
	if st.feats[f.ID].State.Phase != domain.PhaseQuestions {
		t.Fatalf("phase = %s", st.feats[f.ID].State.Phase)
	}
	r := h.Handle(context.Background(), askPost(f, "First?", "A"))
	if r.Next == nil || r.Next.Feature.State.Phase != domain.PhaseQuestions || len(r.Next.Feature.Answers) != 1 {
		t.Fatalf("first round: %+v", r)
	}
	if r.Next.Feature.Next.Kind != domain.AskQuestions || st.feats[f.ID].State.Phase != domain.PhaseQuestions {
		t.Errorf("incomplete round must keep asking: %+v", st.feats[f.ID])
	}
	r = h.Handle(context.Background(), askPost(f, "First?", "B"))
	if len(st.feats[f.ID].Answers) != 1 || st.feats[f.ID].Answers[0].Answer != "B" {
		t.Errorf("answers = %+v", st.feats[f.ID].Answers)
	}
	r = h.Handle(context.Background(), askPost(f, "Second?", "A"))
	if r.Next == nil || r.Next.Feature.State.Phase != domain.PhasePlanning || len(st.feats[f.ID].Answers) != 2 {
		t.Fatalf("second round: %+v", r)
	}
}
