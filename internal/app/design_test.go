package app

import (
	"context"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/design"
	"github.com/mananos/nxy.dev/v2/internal/domain"
)

func engineerFeature(t *testing.T) (Hooks, *memStore, string) {
	t.Helper()
	h, st := flowHooks(false)
	r := h.Handle(context.Background(), prompt("/nxy:engineer  add a cache\nplease"))
	if r.Deny != "" || r.Next == nil {
		t.Fatalf("response = %+v", r)
	}
	return h, st, r.Next.Feature.ID
}

func answer(h Hooks, q, a string) HookResponse {
	return h.Handle(context.Background(), HookEvent{Name: "PostToolUse", Tool: toolAskUser, ProjectDir: "/p", Cwd: "/p",
		Answers: map[string]string{q: a}})
}

func TestEngineerStartsDesigning(t *testing.T) {
	h, st, id := engineerFeature(t)
	f := st.feats[id]
	if f.Request != " add a cache\nplease" {
		t.Errorf("request = %q", f.Request)
	}
	if f.State.Phase != domain.PhaseDesigning || f.Next.Kind != domain.DesignDebate || f.Mode != domain.ModeEngineer || f.Design == nil {
		t.Errorf("feature = %+v", f)
	}
	r := h.Handle(context.Background(), prompt("/nxy:engineer other"))
	if !strings.Contains(r.Deny, "in progress") {
		t.Errorf("second feature: %+v", r)
	}
	r = h.Handle(context.Background(), prompt("/nxy:engineer "))
	if !strings.Contains(r.Deny, "uso: /nxy:engineer") {
		t.Errorf("empty: %+v", r)
	}
}

func TestDesignChangeOutsideDesigningFails(t *testing.T) {
	h, st, id := engineerFeature(t)
	f := st.feats[id]
	f.State.Phase = domain.PhasePlanning
	st.feats[id] = f
	res, err := h.SaveAgenda(context.Background(), id, strings.NewReader(`{"forks":["a"]}`))
	if err == nil || len(res.Problems) != 0 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

func TestDesignTodaySaves(t *testing.T) {
	h, st, id := engineerFeature(t)
	ctx := context.Background()
	if res, err := h.SaveToday(ctx, id, strings.NewReader(`{"d2":"a -> b"}`)); err != nil || len(res.Problems) > 0 {
		t.Fatalf("today: %+v %v", res, err)
	}
	if got := st.feats[id].Design.Today; got != "a -> b" {
		t.Errorf("Today = %q", got)
	}
	f := st.feats[id]
	f.State.Phase = domain.PhasePlanning
	st.feats[id] = f
	if _, err := h.SaveToday(ctx, id, strings.NewReader(`{"d2":"c -> d"}`)); err == nil {
		t.Error("today saved outside designing")
	}
}

func TestDesignProposeAndOK(t *testing.T) {
	h, st, id := engineerFeature(t)
	ctx := context.Background()
	if res, err := h.SaveAgenda(ctx, id, strings.NewReader(`{"forks":["donde va el cache","ttl"]}`)); err != nil || len(res.Problems) > 0 {
		t.Fatalf("agenda: %+v %v", res, err)
	}
	prop := `{"fork":"B1","text":"cache en memoria","why":"un solo proceso","dissent":"se pierde al reiniciar","scope":"task"}`
	if res, err := h.ProposeDecision(ctx, id, strings.NewReader(prop)); err != nil || len(res.Problems) > 0 {
		t.Fatalf("propose: %+v %v", res, err)
	}
	if res, _ := h.ProposeDecision(ctx, id, strings.NewReader(prop)); len(res.Problems) == 0 {
		t.Error("second pending proposal accepted")
	}
	r := answer(h, design.ProposeQuestion(id, 1), design.LabelSave)
	d := st.feats[id].Design
	if len(d.Decisions) != 1 || d.Decisions[0].ID != "D1" || d.Decisions[0].Why != "un solo proceso" ||
		d.Decisions[0].Dissent != "se pierde al reiniciar" || d.Pending != nil || d.Agenda[0].Status != domain.ForkDecided {
		t.Errorf("design = %+v", d)
	}
	if !strings.Contains(r.Context, "1 decisiones") {
		t.Errorf("context = %q", r.Context)
	}
}

func TestDesignFreeTextDoesNotSave(t *testing.T) {
	h, st, id := engineerFeature(t)
	ctx := context.Background()
	_, _ = h.SaveAgenda(ctx, id, strings.NewReader(`{"forks":["a"]}`))
	_, _ = h.ProposeDecision(ctx, id, strings.NewReader(`{"fork":"B1","text":"x","why":"y","scope":"repo"}`))
	r := answer(h, design.ProposeQuestion(id, 1), "mejor redis")
	d := st.feats[id].Design
	if len(d.Decisions) != 0 || d.Pending != nil {
		t.Errorf("design = %+v", d)
	}
	if !strings.Contains(r.Context, "mejor redis") {
		t.Errorf("context = %q", r.Context)
	}
}

func TestDesignForkQuestionAnswer(t *testing.T) {
	h, st, id := engineerFeature(t)
	ctx := context.Background()
	_, _ = h.SaveAgenda(ctx, id, strings.NewReader(`{"forks":["donde va el cache"]}`))
	fork := `{"fork":"B1","options":[{"key":"A","label":"En memoria","summary":"s"},{"key":"B","label":"En disco","summary":"s"}],"opinion":"memoria"}`
	if res, err := h.SaveForkOptions(ctx, id, strings.NewReader(fork)); err != nil || len(res.Problems) > 0 {
		t.Fatalf("fork: %+v %v", res, err)
	}
	f := st.feats[id]
	if !isNxyQuestion(f, design.ForkQuestion(id, "B1")) {
		t.Error("fork question is not an nxy question")
	}
	q := design.ForkQuestion(id, "B1")
	r := answer(h, q, "En disco")
	if !strings.Contains(r.Context, "chose En disco for B1") || !strings.Contains(r.Context, "design propose") {
		t.Errorf("picked: %+v", r)
	}
	r = answer(h, q, "mejor redis")
	if !strings.Contains(r.Context, "The user said: mejor redis") || !strings.Contains(r.Context, "keep debating") {
		t.Errorf("free: %+v", r)
	}
	if st.feats[id].State.Phase != domain.PhaseDesigning {
		t.Errorf("phase = %v", st.feats[id].State.Phase)
	}
}

func TestDesignRefsCaptured(t *testing.T) {
	h, st, id := engineerFeature(t)
	h.Handle(context.Background(), prompt("  usá  este ejemplo\n"))
	h.Handle(context.Background(), prompt("/clear"))
	refs := st.feats[id].Design.Refs
	if len(refs) != 1 || refs[0] != "  usá  este ejemplo\n" {
		t.Errorf("refs = %q", refs)
	}
}

func TestDesignCloseGoesToPlanning(t *testing.T) {
	h, st, id := engineerFeature(t)
	ctx := context.Background()
	_, _ = h.SaveAgenda(ctx, id, strings.NewReader(`{"forks":["a"]}`))
	if res, _ := h.ProposeClose(ctx, id, strings.NewReader(`{"criteria":[{"id":"R1","text":"r"}]}`)); len(res.Problems) == 0 {
		t.Error("close with an open fork accepted")
	}
	if res, err := h.ProposeClose(ctx, id, strings.NewReader("{\"criteria\":[{\"id\":\"R1\",\"text\":\"r\"}],\"d2\":\"a -> b\",\"delegate\":true}")); err != nil || len(res.Problems) > 0 {
		t.Fatalf("close: %+v %v", res, err)
	}
	r := answer(h, design.CloseQuestion(id), design.LabelClose)
	f := st.feats[id]
	if f.State.Phase != domain.PhasePlanning || !f.Design.Closed || f.Design.D2 != "a -> b" || f.Design.Agenda[0].Status != domain.ForkDelegated {
		t.Errorf("feature = %+v", f)
	}
	if r.Next == nil || !strings.Contains(r.Next.Note, "/clear is safe") {
		t.Errorf("response = %+v", r)
	}
	if _, err := h.SaveAgenda(ctx, id, strings.NewReader(`{"forks":["b"]}`)); err == nil {
		t.Error("agenda saved outside designing")
	}
}

func TestDesignScoutAndPlannerOrder(t *testing.T) {
	h, _, _ := engineerFeature(t)
	ev := HookEvent{Name: "PreToolUse", Tool: "Agent", Subagent: roleScout, ProjectDir: "/p", Cwd: "/p"}
	if r := h.Handle(context.Background(), ev); r.Deny != "" {
		t.Errorf("scout denied: %+v", r)
	}
	ev.Subagent = rolePlanner
	if r := h.Handle(context.Background(), ev); r.Deny == "" {
		t.Errorf("planner allowed while designing: %+v", r)
	}
}

func TestOwnStepDesign(t *testing.T) {
	h, _, id := engineerFeature(t)
	h.Exe = "/opt/nxy/nxy"
	cmd := func(sub, fid string) string {
		return "\"/opt/nxy/nxy\" design " + sub + " --feature " + fid + " <<'JSON'\n{}\nJSON"
	}
	ev := func(c, agent string) HookEvent {
		aid := ""
		if agent != "" {
			aid = "a1"
		}
		return HookEvent{Name: "PreToolUse", Tool: "Bash", Command: c, HasCommand: true, AgentID: aid, AgentType: agent, ProjectDir: "/p", Cwd: "/p"}
	}
	for _, sub := range []string{"agenda", "fork", "today", "propose", "close"} {
		if _, ok := h.ownStep(context.Background(), ev(cmd(sub, id), "")); !ok {
			t.Errorf("design %s not allowed", sub)
		}
	}
	if _, ok := h.ownStep(context.Background(), ev(cmd("agenda", "other"), "")); ok {
		t.Error("other feature allowed")
	}
	if _, ok := h.ownStep(context.Background(), ev(cmd("bogus", id), "")); ok {
		t.Error("unknown subcommand allowed")
	}
	if _, ok := h.ownStep(context.Background(), ev(cmd("agenda", id), rolePlanner)); ok {
		t.Error("planner allowed to design")
	}
}
