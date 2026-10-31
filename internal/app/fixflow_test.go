package app

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/plan"
	"github.com/mananos/nxy.dev/internal/review"
)

type fakeLog struct{ calls [][]domain.Pick }

func (l *fakeLog) RecordPicks(_ context.Context, _, _ string, picks []domain.Pick) error {
	l.calls = append(l.calls, picks)
	return nil
}

func fixFinding(id, lens string, sev domain.Severity, file string) domain.Finding {
	return domain.Finding{ID: id, Lens: lens, Severity: sev, File: file, Line: 3, Title: "title " + id,
		Scenario: "x -> boom", Fix: "do it", Cause: domain.CauseChange}
}

// reviewedFeature moves the affected-tests feature into the given review phase with findings.
func reviewedFeature(t *testing.T, phase domain.Phase, next domain.ActionKind, fs ...domain.Finding) (Hooks, *memStore, *cmdShell, *fakeLog, string) {
	t.Helper()
	h, st, sh, id := affectedHooks(t, "", GraphResult{Status: GraphOK, Tests: []string{"pkg/a_test.go"}})
	log := &fakeLog{}
	h.ReviewLog = log
	f := st.feats[id]
	f.State = domain.State{Phase: phase, Batch: 1, Batches: 1}
	f.Next = domain.Action{Kind: next}
	f.Review = &domain.Review{Risk: domain.RiskHigh, Lenses: []domain.LensRun{{Name: "base", Saved: true}}, Findings: fs}
	st.feats[id] = f
	return h, st, sh, log, id
}

func answerEv(id string, parts map[int]string, n int) HookEvent {
	ev := HookEvent{Name: "PostToolUse", Tool: toolAskUser, ProjectDir: "/p", Cwd: "/p", Answers: map[string]string{}}
	for part, a := range parts {
		q := review.FixQuestion(id, part, n)
		ev.Questions = append(ev.Questions, q)
		ev.Answers[q] = a
	}
	return ev
}

func TestRefutedHighNeverReachesCheckpoint(t *testing.T) {
	h, st, _, _, id := reviewedFeature(t, domain.PhaseRefuting, domain.DispatchRefuter,
		fixFinding("H1", "security", domain.SeverityHigh, "pkg/a.go"),
		fixFinding("H2", "api", domain.SeverityMedium, "pkg/b.go"))
	ctx := context.Background()
	pre := agentEv("PreToolUse", roleRefuter)
	pre.AgentPrompt = "feature " + id + " refute"
	if r := h.Handle(ctx, pre); r.Prompt == "" || !strings.Contains(r.Prompt, "H1") {
		t.Fatalf("pre = %+v", r)
	}
	n, err := h.SaveRefute(ctx, id, strings.NewReader(`{"verdicts":[{"id":"H1","verdict":"refuted","why":"guarded"}]}`))
	if err != nil || n != 1 {
		t.Fatalf("save = %d %v", n, err)
	}
	got := st.feats[id]
	if got.State.Phase != domain.PhaseChoosingFindings || !got.Undelivered {
		t.Fatalf("stored = %+v", got.State)
	}
	if off := review.Offered(*got.Review); len(off) != 1 || off[0].ID != "H2" {
		t.Errorf("offered = %+v", off)
	}
	if !strings.Contains(review.Markdown(got), "H1") {
		t.Error("the full review lost the refuted finding")
	}
	if _, err := h.SaveRefute(ctx, id, strings.NewReader(`{"verdicts":[]}`)); err == nil {
		t.Error("second refute accepted")
	}
}

func TestRefuterAllRefutedCloses(t *testing.T) {
	h, st, _, _, id := reviewedFeature(t, domain.PhaseRefuting, domain.DispatchRefuter,
		fixFinding("H1", "security", domain.SeverityHigh, "pkg/a.go"))
	if _, err := h.SaveRefute(context.Background(), id, strings.NewReader(`{"verdicts":[{"id":"H1","verdict":"refuted"}]}`)); err != nil {
		t.Fatal(err)
	}
	if got := st.feats[id]; got.State.Phase != domain.PhaseDone || got.State.Outcome != domain.OutcomeSuccess {
		t.Errorf("stored = %+v", got.State)
	}
}

func TestRefuterBadVerdictsTouchNothing(t *testing.T) {
	h, st, _, _, id := reviewedFeature(t, domain.PhaseRefuting, domain.DispatchRefuter,
		fixFinding("H1", "security", domain.SeverityHigh, "pkg/a.go"))
	v := st.version[id]
	for _, body := range []string{`nope`, `{"verdicts":[{"id":"H9","verdict":"refuted"}]}`, `{"verdicts":[{"id":"H1","verdict":"maybe"}]}`} {
		if _, err := h.SaveRefute(context.Background(), id, strings.NewReader(body)); err == nil {
			t.Errorf("%q accepted", body)
		}
	}
	if st.version[id] != v {
		t.Error("store changed")
	}
}

func TestRefuterBackgroundWithoutSavingIsInconclusive(t *testing.T) {
	h, st, _, _, id := reviewedFeature(t, domain.PhaseRefuting, domain.DispatchRefuter,
		fixFinding("H1", "security", domain.SeverityHigh, "pkg/a.go"))
	ctx := context.Background()
	post := agentEv("PostToolUse", roleRefuter)
	post.AgentAsync = true
	if r := h.Handle(ctx, post); r.Next != nil || !strings.Contains(r.Context, "background") {
		t.Fatalf("launch = %+v", r)
	}
	h.Handle(ctx, HookEvent{Name: "SubagentStop", AgentType: roleRefuter, AgentID: "r1", ProjectDir: "/p", Cwd: "/p"})
	got := st.feats[id]
	if got.State.Phase != domain.PhaseChoosingFindings || got.Review.Findings[0].Verdict != domain.VerdictInconclusive {
		t.Fatalf("stored = %+v", got)
	}
	if r := h.Handle(ctx, mainEv("Stop")); r.Next == nil || r.Next.Feature.Next.Kind != domain.AskFindings {
		t.Fatalf("delivery = %+v", r)
	}
}

func TestChooseTwoFindingsOneImplementerThenGreen(t *testing.T) {
	h, st, sh, log, id := reviewedFeature(t, domain.PhaseChoosingFindings, domain.AskFindings,
		fixFinding("H1", "security", domain.SeverityHigh, "pkg/a.go"),
		fixFinding("H2", "api", domain.SeverityMedium, "pkg/b.go"),
		fixFinding("H3", "base", domain.SeverityLow, "pkg/c.go"))
	ctx := context.Background()

	// Free text without ids asks again and records nothing.
	r := h.Handle(ctx, answerEv(id, map[int]string{1: "arreglá lo que puedas"}, 1))
	if r.Next == nil || r.Next.Note == "" || st.feats[id].State.Phase != domain.PhaseChoosingFindings || len(log.calls) != 0 {
		t.Fatalf("free text = %+v", r)
	}

	r = h.Handle(ctx, answerEv(id, map[int]string{1: "H1 title H1, H3 title H3"}, 1))
	got := st.feats[id]
	if r.Next == nil || got.State.Phase != domain.PhaseFixing || r.Next.Feature.Next.Kind != domain.DispatchFixer {
		t.Fatalf("choose = %+v state %+v", r, got.State)
	}
	if strings.Join(got.Review.Chosen, ",") != "H1,H3" {
		t.Errorf("chosen = %v", got.Review.Chosen)
	}
	if len(log.calls) != 1 || len(log.calls[0]) != 3 {
		t.Fatalf("picks = %+v", log.calls)
	}
	for _, p := range log.calls[0] {
		if p.Chosen != (p.Finding != "H2") {
			t.Errorf("pick = %+v", p)
		}
	}

	pre := agentEv("PreToolUse", roleImplementer)
	pre.AgentPrompt = "feature " + id + " review fix"
	if p := h.Handle(ctx, pre); p.Deny != "" || !strings.Contains(p.Prompt, "H1") || !strings.Contains(p.Prompt, "H3") || strings.Contains(p.Prompt, "H2 ") {
		t.Fatalf("fix prompt = %+v", p)
	}
	edit := HookEvent{Name: "PreToolUse", Tool: "Edit", FilePath: "/p/pkg/a.go", AgentID: "i1", AgentType: roleImplementer, ProjectDir: "/p", Cwd: "/p"}
	if e := h.Handle(ctx, edit); e.AllowReason == "" {
		t.Errorf("edit of a chosen file = %+v", e)
	}
	edit.FilePath = "/p/pkg/b.go"
	if e := h.Handle(ctx, edit); e.AllowReason != "" {
		t.Errorf("edit of an ignored file = %+v", e)
	}

	// While fixing, nxy affected only checks.
	res, err := h.RunAffected(ctx, id)
	if err != nil || !res.Check || res.Recorded || st.feats[id].State.Phase != domain.PhaseFixing {
		t.Fatalf("check = %+v %v", res, err)
	}

	post := agentEv("PostToolUse", roleImplementer)
	if r := h.Handle(ctx, post); r.Next == nil || st.feats[id].State.Phase != domain.PhaseFixVerify || r.Next.Feature.Next.Kind != domain.RunAffected {
		t.Fatalf("return = %+v", r)
	}
	res, err = h.RunAffected(ctx, id)
	if err != nil || res.Verdict != AffectedGreen || !res.Recorded {
		t.Fatalf("verify = %+v %v", res, err)
	}
	if got := st.feats[id]; got.State.Phase != domain.PhaseDone || got.State.Outcome != domain.OutcomeSuccess {
		t.Errorf("stored = %+v", got.State)
	}
	if sh.ran == nil || len(log.calls) != 1 {
		t.Errorf("ran %q picks %d", sh.ran, len(log.calls))
	}
}

func TestChooseNoneClosesAndRecordsPicks(t *testing.T) {
	h, st, _, log, id := reviewedFeature(t, domain.PhaseChoosingFindings, domain.AskFindings,
		fixFinding("H1", "security", domain.SeverityHigh, "pkg/a.go"))
	h.Handle(context.Background(), answerEv(id, map[int]string{1: review.NoneLabel}, 1))
	if got := st.feats[id]; got.State.Phase != domain.PhaseDone || len(got.Review.Chosen) != 0 {
		t.Errorf("stored = %+v", got.State)
	}
	if len(log.calls) != 1 || len(log.calls[0]) != 1 || log.calls[0][0].Chosen {
		t.Errorf("picks = %+v", log.calls)
	}
}

func TestPicksOnlyForShownFindings(t *testing.T) {
	var fs []domain.Finding
	for i := 1; i <= 17; i++ {
		fs = append(fs, fixFinding("H"+strconv.Itoa(i), "base", domain.SeverityMedium, "pkg/a.go"))
	}
	h, _, _, log, id := reviewedFeature(t, domain.PhaseChoosingFindings, domain.AskFindings, fs...)
	h.Handle(context.Background(), answerEv(id, map[int]string{1: "H1 title H1"}, 4))
	if len(log.calls) != 1 || len(log.calls[0]) != 16 {
		t.Fatalf("picks = %+v", log.calls)
	}
	for _, p := range log.calls[0] {
		if p.Finding == "H17" {
			t.Errorf("recorded a finding the user never saw: %+v", p)
		}
	}
}

func fixingFeature(t *testing.T) (Hooks, *memStore, *cmdShell, string) {
	t.Helper()
	h, st, sh, _, id := reviewedFeature(t, domain.PhaseFixing, domain.DispatchFixer,
		fixFinding("H1", "security", domain.SeverityHigh, "pkg/a.go"))
	f := st.feats[id]
	f.Review.Chosen = []string{"H1"}
	st.feats[id] = f
	sh.exit["go test"] = 1
	return h, st, sh, id
}

func TestFixRedRetriesOnceThenFails(t *testing.T) {
	h, st, _, id := fixingFeature(t)
	ctx := context.Background()
	h.Handle(ctx, agentEv("PostToolUse", roleImplementer))
	res, err := h.RunAffected(ctx, id)
	if err != nil || res.Verdict != AffectedRed || !res.Recorded {
		t.Fatalf("first = %+v %v", res, err)
	}
	if got := st.feats[id]; got.State.Phase != domain.PhaseFixing || got.State.Attempt != 1 {
		t.Fatalf("after first red = %+v", got.State)
	}
	pre := agentEv("PreToolUse", roleImplementer)
	if p := h.Handle(ctx, pre); !strings.Contains(p.Prompt, "exit 1") {
		t.Errorf("retry prompt lacks the failure: %q", p.Prompt)
	}
	h.Handle(ctx, agentEv("PostToolUse", roleImplementer))
	if _, err := h.RunAffected(ctx, id); err != nil {
		t.Fatal(err)
	}
	got := st.feats[id]
	if got.State.Phase != domain.PhaseFixesFailed || got.Next.Kind != domain.AskDecision {
		t.Fatalf("after second red = %+v next %+v", got.State, got.Next)
	}
}

func TestFixesFailedDecisions(t *testing.T) {
	cases := []struct {
		answer  string
		phase   domain.Phase
		outcome domain.Outcome
	}{
		{plan.Retry, domain.PhaseFixing, domain.OutcomeNone},
		{plan.Continue, domain.PhaseDone, domain.OutcomeSuccess},
		{plan.Stop, domain.PhaseDone, domain.OutcomeStopped},
	}
	for _, c := range cases {
		t.Run(c.answer, func(t *testing.T) {
			h, st, _, id := fixingFeature(t)
			f := st.feats[id]
			f.State.Phase, f.State.Attempt, f.Next = domain.PhaseFixesFailed, 1, domain.Action{Kind: domain.AskDecision}
			st.feats[id] = f
			q := plan.FixesFailedQuestion(id)
			pre := HookEvent{Name: "PreToolUse", Tool: toolAskUser, ProjectDir: "/p", Cwd: "/p", Questions: []string{q}, InputHasAnswers: true}
			if r := h.Handle(context.Background(), pre); r.Deny == "" {
				t.Error("pre-filled answer accepted")
			}
			ev := HookEvent{Name: "PostToolUse", Tool: toolAskUser, ProjectDir: "/p", Cwd: "/p",
				Questions: []string{q}, Answers: map[string]string{q: c.answer}}
			h.Handle(context.Background(), ev)
			got := st.feats[id]
			if got.State.Phase != c.phase || got.State.Outcome != c.outcome {
				t.Errorf("state = %+v", got.State)
			}
		})
	}
}
