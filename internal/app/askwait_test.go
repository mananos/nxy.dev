package app

import (
	"context"
	"database/sql"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/domain"
	"github.com/mananos/nxy.dev/v2/internal/plan"
)

type fakeAsks struct {
	mu      sync.Mutex
	rows    map[string]*domain.PendingAsk
	n       int
	created int
	touches int
}

func newFakeAsks() *fakeAsks { return &fakeAsks{rows: map[string]*domain.PendingAsk{}} }

func (a *fakeAsks) CreatePendingAsk(_ context.Context, r domain.PendingAsk) (domain.PendingAsk, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.n++
	a.created++
	r.ID = "ASK" + strconv.Itoa(a.n)
	r.Answers = nil
	c := r
	a.rows[r.ID] = &c
	return r, nil
}

func (a *fakeAsks) PendingAsk(_ context.Context, id string) (domain.PendingAsk, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	r, ok := a.rows[id]
	if !ok {
		return domain.PendingAsk{}, sql.ErrNoRows
	}
	return *r, nil
}

func (a *fakeAsks) TouchPendingAsk(_ context.Context, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.touches++
	return nil
}

func (a *fakeAsks) touched() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.touches
}

func (a *fakeAsks) OpenPendingAsks(context.Context) ([]domain.PendingAsk, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []domain.PendingAsk
	for _, r := range a.rows {
		if r.Answers == nil {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (a *fakeAsks) AnswerPendingAsk(_ context.Context, id string, answers []string, by string) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	r, ok := a.rows[id]
	if !ok || r.Answers != nil {
		return false, nil
	}
	if answers == nil {
		answers = []string{}
	}
	r.Answers, r.AnsweredBy = answers, by
	return true, nil
}

func (a *fakeAsks) ReleaseFeatureAsks(_ context.Context, featureID, by string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, r := range a.rows {
		if r.FeatureID == featureID && r.Answers == nil {
			r.Answers, r.AnsweredBy = []string{}, by
		}
	}
	return nil
}

func (a *fakeAsks) DeletePendingAsk(_ context.Context, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.rows, id)
	return nil
}

func (a *fakeAsks) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.rows)
}

// firstOpen waits for the hook to write a question down.
func (a *fakeAsks) firstOpen(t *testing.T) domain.PendingAsk {
	t.Helper()
	for range 2000 {
		if rows, _ := a.OpenPendingAsks(context.Background()); len(rows) > 0 {
			return rows[0]
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the hook never held the question")
	return domain.PendingAsk{}
}

type fakeTUI struct{ alive atomic.Bool }

func (f *fakeTUI) Alive() bool { return f.alive.Load() }

func waitHooks(t *testing.T) (Hooks, *memStore, *fakeAsks, *fakeTUI, domain.Feature) {
	t.Helper()
	h, st := flowHooks(false)
	asks, tui := newFakeAsks(), &fakeTUI{}
	tui.alive.Store(true)
	h.Asks, h.TUI = asks, tui
	h.AskWait = AskWait{Every: time.Millisecond, Max: 5 * time.Second}
	f := approvalFeature(t, h, validPlan())
	return h, st, asks, tui, st.feats[f.ID]
}

func approvalAsk(f domain.Feature) HookEvent {
	var asked []domain.AskedQuestion
	for _, q := range tuiQuestions(f) {
		asked = append(asked, q)
	}
	var qs []string
	for _, q := range asked {
		qs = append(qs, q.Question)
	}
	return HookEvent{Name: "PreToolUse", Tool: toolAskUser, ProjectDir: "/p", Cwd: "/p", SessionID: "s1", ToolUseID: "tu1",
		Questions: qs, Asked: asked}
}

func TestWaitInTUIAnswers(t *testing.T) {
	h, _, asks, _, f := waitHooks(t)
	ev := approvalAsk(f)
	if len(ev.Asked) != 1 || ev.Asked[0].Question != plan.ApprovalQuestion(f.ID, f.PlanRev) {
		t.Fatalf("asked = %+v", ev.Asked)
	}
	go func() {
		row := asks.firstOpen(t)
		if err := h.Decide(context.Background(), row.ID, []string{plan.Approve}, false); err != nil {
			t.Error(err)
		}
	}()
	r := h.Handle(context.Background(), ev)
	if r.Answers == nil || r.Answers.ByQuestion[ev.Asked[0].Question] != plan.Approve || r.Deny != "" {
		t.Fatalf("response = %+v", r)
	}
	if asks.count() != 0 {
		t.Errorf("the row stays after answering: %d", asks.count())
	}
}

func TestWaitInTUIFreeTextAnswer(t *testing.T) {
	h, _, asks, _, f := waitHooks(t)
	ev := approvalAsk(f)
	go func() {
		row := asks.firstOpen(t)
		if err := h.Decide(context.Background(), row.ID, []string{"mi propia respuesta"}, false); err != nil {
			t.Error(err)
		}
	}()
	r := h.Handle(context.Background(), ev)
	if r.Answers == nil || r.Answers.ByQuestion[ev.Asked[0].Question] != "mi propia respuesta" || r.Deny != "" {
		t.Fatalf("response = %+v", r)
	}
}

func TestWaitInTUIRenewsTheRow(t *testing.T) {
	h, _, asks, _, f := waitHooks(t)
	ev := approvalAsk(f)
	go func() {
		row := asks.firstOpen(t)
		for range 5000 {
			if asks.touched() >= 3 {
				break
			}
			time.Sleep(time.Millisecond)
		}
		if asks.touched() < 3 {
			t.Error("the hook did not renew the row on every reread")
		}
		_ = h.Decide(context.Background(), row.ID, []string{plan.Approve}, false)
	}()
	if r := h.Handle(context.Background(), ev); r.Answers == nil {
		t.Fatalf("response = %+v", r)
	}
}

func TestWaitInTUINoTUIWritesNothing(t *testing.T) {
	h, _, asks, tui, f := waitHooks(t)
	tui.alive.Store(false)
	if r := h.Handle(context.Background(), approvalAsk(f)); r.Answers != nil || r.Deny != "" {
		t.Fatalf("response = %+v", r)
	}
	if asks.created != 0 {
		t.Errorf("rows written without a TUI: %d", asks.created)
	}
}

func TestWaitInTUIClosedTUIFallsBack(t *testing.T) {
	h, _, asks, tui, f := waitHooks(t)
	go func() {
		asks.firstOpen(t)
		tui.alive.Store(false)
	}()
	if r := h.Handle(context.Background(), approvalAsk(f)); r.Answers != nil || r.Deny != "" {
		t.Fatalf("response = %+v", r)
	}
	if asks.count() != 0 {
		t.Errorf("rows left: %d", asks.count())
	}
}

func TestWaitInTUIRelease(t *testing.T) {
	h, _, asks, _, f := waitHooks(t)
	go func() {
		row := asks.firstOpen(t)
		if err := h.Decide(context.Background(), row.ID, nil, true); err != nil {
			t.Error(err)
		}
	}()
	if r := h.Handle(context.Background(), approvalAsk(f)); r.Answers != nil || r.Deny != "" {
		t.Fatalf("response = %+v", r)
	}
}

func TestWaitInTUIMaxFallsBack(t *testing.T) {
	h, _, _, _, f := waitHooks(t)
	h.AskWait = AskWait{Every: time.Millisecond, Max: 20 * time.Millisecond}
	if r := h.Handle(context.Background(), approvalAsk(f)); r.Answers != nil || r.Deny != "" {
		t.Fatalf("response = %+v", r)
	}
}

func TestWaitInTUIStoppedDenies(t *testing.T) {
	h, st, asks, _, f := waitHooks(t)
	go func() {
		asks.firstOpen(t)
		if _, _, err := h.StopFeatureByID(context.Background(), f.ID); err != nil {
			t.Error(err)
		}
	}()
	r := h.Handle(context.Background(), approvalAsk(f))
	if r.Deny == "" || r.Answers != nil {
		t.Fatalf("response = %+v", r)
	}
	if st.feats[f.ID].State.Phase != domain.PhaseDone {
		t.Errorf("phase = %s", st.feats[f.ID].State.Phase)
	}
}

func TestWaitInTUISkipsWhatItCannotAnswer(t *testing.T) {
	h, _, asks, _, f := waitHooks(t)
	sub := approvalAsk(f)
	sub.AgentID = "a1"
	if r := h.Handle(context.Background(), sub); r.Answers != nil || r.Deny != "" {
		t.Errorf("subagent: %+v", r)
	}
	foreign := approvalAsk(f)
	foreign.Asked = []domain.AskedQuestion{{Question: "Tabs or spaces?", Options: []string{"Tabs", "Spaces"}}}
	if r := h.Handle(context.Background(), foreign); r.Answers != nil || r.Deny != "" {
		t.Errorf("foreign question: %+v", r)
	}
	changed := approvalAsk(f)
	changed.Asked[0].Options = []string{plan.Approve, "Otra cosa"}
	if r := h.Handle(context.Background(), changed); r.Answers != nil || r.Deny != "" {
		t.Errorf("changed labels: %+v", r)
	}
	if asks.created != 0 {
		t.Errorf("rows written: %d", asks.created)
	}
}

func TestWaitInTUIModelAnswersStillDenied(t *testing.T) {
	h, _, asks, _, f := waitHooks(t)
	ev := approvalAsk(f)
	ev.InputHasAnswers = true
	if r := h.Handle(context.Background(), ev); r.Deny == "" {
		t.Fatalf("response = %+v", r)
	}
	if asks.created != 0 {
		t.Errorf("rows written: %d", asks.created)
	}
}
