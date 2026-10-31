package app

import (
	"context"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
)

func reviewingFeature(t *testing.T, h Hooks, st *memStore, quick bool, lenses ...string) string {
	t.Helper()
	rv := &domain.Review{Risk: domain.RiskMedium, Quick: quick}
	if quick {
		rv.Risk = domain.RiskLow
	}
	for _, n := range lenses {
		rv.Lenses = append(rv.Lenses, domain.LensRun{Name: n, Title: n})
	}
	id, err := st.CreateFeature(context.Background(), domain.Feature{
		Repo: h.Repos.Key("/p"), ProjectDir: "/p", Request: "do it", PlanRev: 1,
		Plan:   &domain.Plan{Rules: []domain.Rule{{ID: "R1", Text: "adds the flag", Test: domain.TestTDD}}},
		State:  domain.State{Phase: domain.PhaseReviewing},
		Next:   domain.Action{Kind: domain.DispatchReviewer},
		Review: rv,
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

const baseOK = `{"lens":"base","rules":[{"id":"R1","ok":true}],"findings":[]}`

func finding(lens, sev, line string) string {
	return `{"lens":"` + lens + `","findings":[{"severity":"` + sev + `","file":"a.go","line":` + line +
		`,"title":"t","scenario":"x -> boom","fix":"f"}]}`
}

func reviewerStopEv(agent, id string) HookEvent {
	return HookEvent{Name: "SubagentStop", AgentType: agent, AgentID: id, ProjectDir: "/p", Cwd: "/p"}
}

func TestReviewersInParallelAdvanceOnce(t *testing.T) {
	h, st := flowHooks(false)
	h.Exe = "/opt/nxy/nxy"
	ctx := context.Background()
	id := reviewingFeature(t, h, st, false, "api", "base", "security")
	for _, l := range []string{"api", "base", "security"} {
		ev := agentEv("PreToolUse", roleReviewer)
		ev.AgentPrompt = "feature " + id + " review " + l
		r := h.Handle(ctx, ev)
		if r.Deny != "" || r.Prompt == "" {
			t.Fatalf("%s: dispatch = %+v", l, r)
		}
		if again := h.Handle(ctx, ev); again.Deny == "" {
			t.Errorf("%s dispatched twice", l)
		}
	}
	if _, err := h.SaveReview(ctx, id, "api", strings.NewReader(finding("api", "medium", "5"))); err != nil {
		t.Fatal(err)
	}
	res, err := h.SaveReview(ctx, id, "base", strings.NewReader(baseOK))
	if err != nil || res.Saved != 2 || res.Total != 3 {
		t.Fatalf("base = %+v %v", res, err)
	}
	for _, a := range []string{"a1", "a2"} {
		h.Handle(ctx, reviewerStopEv(roleReviewer, a))
	}
	if got := st.feats[id]; got.State.Phase != domain.PhaseReviewing || got.Undelivered {
		t.Fatalf("advanced early: %+v", got.State)
	}
	// The third reviewer returns without saving: the review closes.
	h.Handle(ctx, reviewerStopEv(roleReviewer, "a3"))
	h.Handle(ctx, reviewerStopEv(roleReviewer, "a3"))
	got := st.feats[id]
	if got.State.Phase != domain.PhaseChoosingFindings || !got.Undelivered {
		t.Fatalf("stored = %+v", got)
	}
	if len(got.Review.Findings) != 1 || got.Review.Findings[0].ID != "H1" {
		t.Errorf("findings = %+v", got.Review.Findings)
	}
	if r := h.Handle(ctx, prompt("sigo")); r.Next == nil || r.Next.Feature.Next.Kind != domain.AskFindings {
		t.Fatalf("delivery = %+v", r)
	}
	if r := h.Handle(ctx, prompt("otra")); r.Next != nil {
		t.Errorf("second delivery = %+v", r)
	}
}

func TestUndispatchedLensIsDeliveredAgain(t *testing.T) {
	h, st := flowHooks(false)
	h.Exe = "/opt/nxy/nxy"
	ctx := context.Background()
	id := reviewingFeature(t, h, st, false, "api", "base", "security")
	for _, l := range []string{"api", "base"} {
		ev := agentEv("PreToolUse", roleReviewer)
		ev.AgentPrompt = "feature " + id + " review " + l
		if r := h.Handle(ctx, ev); r.Deny != "" {
			t.Fatalf("%s: %+v", l, r)
		}
	}
	if _, err := h.SaveReview(ctx, id, "api", strings.NewReader(finding("api", "medium", "5"))); err != nil {
		t.Fatal(err)
	}
	if _, err := h.SaveReview(ctx, id, "base", strings.NewReader(baseOK)); err != nil {
		t.Fatal(err)
	}
	for _, a := range []string{"a1", "a2"} {
		h.Handle(ctx, reviewerStopEv(roleReviewer, a))
	}
	got := st.feats[id]
	if got.State.Phase != domain.PhaseReviewing || !got.Undelivered {
		t.Fatalf("stalled: %+v undelivered=%v", got.State, got.Undelivered)
	}
	if r := h.Handle(ctx, prompt("sigo")); r.Next == nil || r.Next.Feature.Next.Kind != domain.DispatchReviewer {
		t.Fatalf("delivery = %+v", r)
	}
	ev := agentEv("PreToolUse", roleReviewer)
	ev.AgentPrompt = "feature " + id + " review security"
	if r := h.Handle(ctx, ev); r.Deny != "" {
		t.Fatalf("security: %+v", r)
	}
	h.Handle(ctx, reviewerStopEv(roleReviewer, "a3"))
	if got := st.feats[id]; got.State.Phase == domain.PhaseReviewing {
		t.Fatalf("did not close: %+v", got.State)
	}
}

func TestSaveReviewInvalidTouchesNothing(t *testing.T) {
	h, st := flowHooks(false)
	ctx := context.Background()
	id := reviewingFeature(t, h, st, false, "api", "base")
	v := st.version[id]
	bad := []string{
		`{"lens":"api","extra":1,"findings":[]}`,
		`not json`,
		`{"lens":"api","findings":[{"severity":"huge","title":"t"}]}`,
		`{"lens":"base","findings":[]}`, // base without the rules
	}
	for i, body := range bad {
		lens := "api"
		if i == 3 {
			lens = "base"
		}
		res, err := h.SaveReview(ctx, id, lens, strings.NewReader(body))
		if err == nil && len(res.Problems) == 0 {
			t.Errorf("%q accepted", body)
		}
	}
	if _, err := h.SaveReview(ctx, id, "nope", strings.NewReader(baseOK)); err == nil {
		t.Error("unknown lens accepted")
	}
	if st.version[id] != v {
		t.Errorf("store changed: version %d -> %d", v, st.version[id])
	}
}

func TestReviewForegroundDeliversOnce(t *testing.T) {
	h, st := flowHooks(false)
	ctx := context.Background()
	id := reviewingFeature(t, h, st, true, "base")
	pre := agentEv("PreToolUse", roleReviewerQuick)
	pre.AgentPrompt = "feature " + id + " review base"
	if r := h.Handle(ctx, pre); r.Prompt == "" {
		t.Fatalf("pre = %+v", r)
	}
	if _, err := h.SaveReview(ctx, id, "base", strings.NewReader(baseOK)); err != nil {
		t.Fatal(err)
	}
	h.Handle(ctx, reviewerStopEv(roleReviewerQuick, "a1"))
	post := agentEv("PostToolUse", roleReviewerQuick)
	r := h.Handle(ctx, post)
	if r.Next == nil || r.Next.Feature.State.Phase != domain.PhaseDone {
		t.Fatalf("first = %+v", r)
	}
	if r := h.Handle(ctx, post); r.Next != nil {
		t.Errorf("second = %+v", r)
	}
	if r := h.Handle(ctx, mainEv("Stop")); r.Next != nil {
		t.Errorf("stop = %+v", r)
	}
}

func TestReviewBackgroundWaitsThenDeliversOnce(t *testing.T) {
	h, st := flowHooks(false)
	ctx := context.Background()
	id := reviewingFeature(t, h, st, false, "api", "base")
	for _, l := range []string{"api", "base"} {
		pre := agentEv("PreToolUse", roleReviewer)
		pre.AgentPrompt = "feature " + id + " review " + l
		h.Handle(ctx, pre)
		post := agentEv("PostToolUse", roleReviewer)
		post.AgentAsync = true
		r := h.Handle(ctx, post)
		if r.Next != nil || !strings.Contains(r.Context, "running") {
			t.Fatalf("%s launch = %+v", l, r)
		}
	}
	h.SaveReview(ctx, id, "api", strings.NewReader(finding("api", "low", "1")))
	h.SaveReview(ctx, id, "base", strings.NewReader(baseOK))
	h.Handle(ctx, reviewerStopEv(roleReviewer, "a1"))
	h.Handle(ctx, reviewerStopEv(roleReviewer, "a2"))
	r := h.Handle(ctx, mainEv("Stop"))
	if r.Next == nil || r.Next.Feature.State.Phase != domain.PhaseChoosingFindings {
		t.Fatalf("delivery = %+v", r)
	}
	if r := h.Handle(ctx, mainEv("Stop")); r.Next != nil {
		t.Errorf("second = %+v", r)
	}
}

func TestFailedRuleBecomesHighFinding(t *testing.T) {
	h, st := flowHooks(false)
	ctx := context.Background()
	id := reviewingFeature(t, h, st, true, "base")
	body := `{"lens":"base","rules":[{"id":"R1","ok":false,"why":"no flag in README","file":"README.md","line":3}],"findings":[]}`
	if _, err := h.SaveReview(ctx, id, "base", strings.NewReader(body)); err != nil {
		t.Fatal(err)
	}
	got := st.feats[id]
	if got.State.Phase != domain.PhaseChoosingFindings || len(got.Review.Findings) != 1 {
		t.Fatalf("stored = %+v", got)
	}
	f := got.Review.Findings[0]
	if f.Severity != domain.SeverityHigh || f.Rule != "R1" || f.Lens != "base" || f.Dropped != "" {
		t.Errorf("finding = %+v", f)
	}
}

func TestReviewerDeniedOutOfOrder(t *testing.T) {
	h, st := flowHooks(false)
	ctx := context.Background()
	id := reviewingFeature(t, h, st, true, "base")
	pre := agentEv("PreToolUse", roleReviewer) // quick review needs the quick reviewer
	pre.AgentPrompt = "feature " + id + " review base"
	if r := h.Handle(ctx, pre); r.Deny == "" {
		t.Errorf("full reviewer on a quick review: %+v", r)
	}
	pre = agentEv("PreToolUse", roleReviewerQuick)
	pre.AgentPrompt = "feature " + id + " review other"
	if r := h.Handle(ctx, pre); r.Deny == "" {
		t.Errorf("unknown lens: %+v", r)
	}
	pre.AgentPrompt = "review base"
	if r := h.Handle(ctx, pre); r.Deny == "" {
		t.Errorf("free prompt: %+v", r)
	}
}

func TestSessionStartReviewersRunning(t *testing.T) {
	h, st := flowHooks(false)
	ctx := context.Background()
	id := reviewingFeature(t, h, st, false, "api", "base")
	pre := agentEv("PreToolUse", roleReviewer)
	pre.AgentPrompt = "feature " + id + " review api"
	h.Handle(ctx, pre)
	ev := HookEvent{Name: "SessionStart", Source: "resume", ProjectDir: "/p", Cwd: "/p"}
	if r := h.Handle(ctx, ev); r.Next != nil || !strings.Contains(r.Context, "reviewers are running") {
		t.Errorf("resume = %+v", r)
	}
	ev.Source = "startup"
	r := h.Handle(ctx, ev)
	if r.Next == nil || st.feats[id].Review.Lenses[0].Dispatched {
		t.Errorf("startup = %+v, lens %+v", r, st.feats[id].Review.Lenses[0])
	}
}
