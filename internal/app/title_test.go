package app

import (
	"context"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/domain"
	"github.com/mananos/nxy.dev/v2/internal/plan"
)

func postApproval(f domain.Feature, answer string) HookEvent {
	ev := approvalAsk(f)
	ev.Name = "PostToolUse"
	ev.Answers = map[string]string{ev.Asked[0].Question: answer}
	return ev
}

func TestTitleRestoredAfterAnsweringANxyQuestion(t *testing.T) {
	h, _, _, _, f := waitHooks(t)
	r := h.Handle(context.Background(), postApproval(f, plan.Approve))
	if r.Title != TitleRestore {
		t.Fatalf("title = %v, response = %+v", r.Title, r)
	}
}

func TestTitleNoneForAForeignQuestion(t *testing.T) {
	h, _, _, _, f := waitHooks(t)
	ev := postApproval(f, "Tabs")
	ev.Questions = []string{"Tabs or spaces?"}
	ev.Answers = map[string]string{"Tabs or spaces?": "Tabs"}
	if r := h.Handle(context.Background(), ev); r.Title != TitleNone {
		t.Fatalf("title = %v", r.Title)
	}
}

func TestTitlePendingWhenTheNextStepAsks(t *testing.T) {
	h, st, _, _, f := waitHooks(t)
	g := st.feats[f.ID]
	g.State.Phase = domain.PhasePlannerFailed
	g.Undelivered = true
	st.feats[f.ID] = g
	ev := HookEvent{Name: "Stop", ProjectDir: "/p", Cwd: "/p", SessionID: "s1"}
	r := h.Handle(context.Background(), ev)
	if r.Next == nil || r.Title != TitlePending {
		t.Fatalf("title = %v, response = %+v", r.Title, r)
	}
}

func TestTitlePendingBeatsRestore(t *testing.T) {
	h, st, _, _, f := waitHooks(t)
	// An answer nxy cannot read asks the same question again: the title stays pending.
	g := st.feats[f.ID]
	g.State.Phase = domain.PhasePlannerFailed
	st.feats[f.ID] = g
	ev := postApproval(f, "no es una opcion")
	ev.Asked = nil
	ev.Questions = []string{plan.PlannerFailedQuestion(f.ID)}
	ev.Answers = map[string]string{plan.PlannerFailedQuestion(f.ID): "no es una opcion"}
	r := h.Handle(context.Background(), ev)
	if r.Next == nil || r.Title != TitlePending {
		t.Fatalf("title = %v, response = %+v", r.Title, r)
	}
}
