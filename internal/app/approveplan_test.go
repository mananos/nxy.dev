package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/design"
	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/plan"
)

func offerPlan() domain.Plan {
	p := validPlan()
	opts := []domain.Option{{Label: "A", Description: "a", Recommended: true}, {Label: "B", Description: "b"}}
	p.Questions = []domain.Question{
		{Question: "First?", Header: "One", Options: opts},
		{Question: "Second?", Header: "Two", Options: opts},
	}
	return p
}

func TestOfferAcceptedGoesToDesigning(t *testing.T) {
	h, st := flowHooks(false)
	f := approvalFeature(t, h, offerPlan())
	h.Handle(context.Background(), askPost(f, design.OfferQuestion(f.ID), design.LabelToEngineer))
	got := st.feats[f.ID]
	if got.State.Phase != domain.PhaseDesigning || got.Mode != domain.ModeEngineer || !got.OfferAnswered {
		t.Fatalf("feature = %+v", got.State)
	}
	if got.Design == nil || len(got.Design.Agenda) != 2 || got.Design.Agenda[0].ID != "B1" ||
		got.Design.Agenda[1].Title != "Second?" || got.Design.Agenda[0].Source != domain.SourceOffer ||
		len(got.Design.Agenda[0].Options) != 2 {
		t.Errorf("agenda = %+v", got.Design)
	}
}

func TestOfferDeclinedKeepsQuestions(t *testing.T) {
	h, st := flowHooks(false)
	f := approvalFeature(t, h, offerPlan())
	h.Handle(context.Background(), askPost(f, design.OfferQuestion(f.ID), design.LabelAnswerHere))
	got := st.feats[f.ID]
	if got.State.Phase != domain.PhaseQuestions || !got.OfferAnswered || len(got.Answers) != 0 || got.Mode != domain.ModeDelegate {
		t.Fatalf("feature = %+v answers=%v", got.State, got.Answers)
	}
	h.Handle(context.Background(), askPost(f, "First?", "A"))
	h.Handle(context.Background(), askPost(f, "Second?", "B"))
	if st.feats[f.ID].State.Phase != domain.PhasePlanning {
		t.Errorf("phase = %s", st.feats[f.ID].State.Phase)
	}
}

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
	if r.Next != nil && (!strings.Contains(r.Next.Note, "/plans/"+f.ID+".md") || !strings.Contains(r.Next.Note, "editor")) {
		t.Errorf("note = %q", r.Next.Note)
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

func failedBatchFeature(t *testing.T, h Hooks, st *memStore) domain.Feature {
	t.Helper()
	f := approvalFeature(t, h, validPlan())
	f = st.feats[f.ID]
	f.State = domain.State{Phase: domain.PhaseBatchFailed, Batch: 1, Batches: 1, Attempt: 1}
	f.Next = domain.Action{Kind: domain.AskDecision}
	st.feats[f.ID] = f
	return f
}

func TestDecisionAnswersMoveTheFlow(t *testing.T) {
	for _, c := range []struct {
		answer string
		phase  domain.Phase
		out    domain.Outcome
	}{
		{plan.Retry, domain.PhaseRunningBatch, domain.OutcomeNone},
		{plan.Continue, domain.PhaseAffected, domain.OutcomeNone},
		{plan.Stop, domain.PhaseDone, domain.OutcomeStopped},
	} {
		h, st := flowHooks(false)
		f := failedBatchFeature(t, h, st)
		r := h.Handle(context.Background(), askPost(f, plan.DecisionQuestion(f.ID, 1), c.answer))
		got := st.feats[f.ID].State
		if r.Next == nil || got.Phase != c.phase || got.Outcome != c.out || (c.answer == plan.Retry && got.Attempt != 0) {
			t.Errorf("%s: state = %+v response = %+v", c.answer, got, r)
		}
	}
}

func TestDecisionFreeTextAsksAgain(t *testing.T) {
	h, st := flowHooks(false)
	f := failedBatchFeature(t, h, st)
	r := h.Handle(context.Background(), askPost(f, plan.DecisionQuestion(f.ID, 1), "no se"))
	if r.Next == nil || !strings.Contains(r.Next.Note, "pick one of the options") || st.feats[f.ID].State.Phase != domain.PhaseBatchFailed {
		t.Errorf("response = %+v state = %+v", r, st.feats[f.ID].State)
	}
	ev := HookEvent{Name: "PreToolUse", Tool: "AskUserQuestion", ProjectDir: "/p", Cwd: "/p",
		Questions: []string{plan.DecisionQuestion(f.ID, 1)}, InputHasAnswers: true}
	if r := h.Handle(context.Background(), ev); r.Deny == "" {
		t.Errorf("pre-filled decision should be denied: %+v", r)
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
