package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/legacy"
)

const halfPlanBody = "## Next\n- seguir\n\n## Plan\nGoal: migrar el login\n### Batch 1 — modelo\nhacer x\n### Batch 2 — vista\nhacer y\n"

// halfPlanWorld is a 1.x folder with one approved plan whose batch 1 passed.
func halfPlanWorld(t *testing.T) (Hooks, *fakeLegacyData, *fakeLegacyStore, *noteMemory, string) {
	t.Helper()
	h, d, s, mem := legacyHooks(t)
	hash := legacy.PlanHash(legacy.ExtractPlan(halfPlanBody))
	now := time.Now().UnixMilli()
	d.hasMem = true
	d.mem = []legacy.Memory{{ID: "h1", Scope: "project", Project: "proj", Type: "handoff", Title: "handoff: migrar", Body: halfPlanBody, Created: now, Updated: now}}
	d.plans = legacy.PlanFiles{Approved: []string{hash}, Verify: map[string]map[int]string{hash: {1: "pass"}}}
	return h, d, s, mem, hash
}

func planAnswerEv(sid, q, a string) HookEvent {
	return HookEvent{Name: "PostToolUse", Tool: "AskUserQuestion", SessionID: sid, ProjectDir: "/p", Cwd: "/p",
		Answers: map[string]string{q: a}}
}

func TestLegacyPlanOfferedAtStartupUntilAnswered(t *testing.T) {
	h, _, _, _, hash := halfPlanWorld(t)
	for _, sid := range []string{"s1", "s2"} {
		r := h.Handle(context.Background(), startEv("startup", sid))
		if !strings.Contains(r.Notice, "migrar el login") || !strings.Contains(r.Notice, "falta el lote 2") {
			t.Fatalf("notice = %q", r.Notice)
		}
		if r.Ask == nil {
			t.Fatalf("no ask")
		}
		if got, ok := legacy.ParseHalfPlanHash(r.Ask.Question); !ok || got != hash {
			t.Errorf("question = %q", r.Ask.Question)
		}
	}
}

func TestLegacyPlanIgnoreIsMarkedAndNeverReturns(t *testing.T) {
	h, d, s, _, hash := halfPlanWorld(t)
	r := h.Handle(context.Background(), startEv("startup", "s1"))
	h.Handle(context.Background(), planAnswerEv("s1", r.Ask.Question, legacy.HalfPlanIgnore))
	m, ok := s.marks[legacy.ScopeHalfPlan+"/"+legacy.HalfPlanKey("/p/.nxy/local", hash)]
	if !ok || m.Answer != legacy.AnswerIgnore {
		t.Fatalf("mark = %+v ok=%v", m, ok)
	}
	reads := d.memReads
	r = h.Handle(context.Background(), startEv("startup", "s2"))
	if r.Ask != nil || strings.Contains(r.Notice, "1.x") || d.memReads != reads {
		t.Errorf("ask=%v notice=%q memReads %d -> %d", r.Ask, r.Notice, reads, d.memReads)
	}
}

func TestLegacyPlanFreeTextAnswerIsNotMarked(t *testing.T) {
	h, _, s, _, hash := halfPlanWorld(t)
	r := h.Handle(context.Background(), startEv("startup", "s1"))
	h.Handle(context.Background(), planAnswerEv("s1", r.Ask.Question, "mejor después"))
	if _, ok := s.marks[legacy.ScopeHalfPlan+"/"+legacy.HalfPlanKey("/p/.nxy/local", hash)]; ok {
		t.Fatalf("free text must not mark the plan")
	}
	if r = h.Handle(context.Background(), startEv("startup", "s2")); r.Ask == nil {
		t.Errorf("offer should come back")
	}
}

func TestLegacyPlanAllDoneIsClosedAndNotReadAgain(t *testing.T) {
	h, d, s, _, hash := halfPlanWorld(t)
	d.plans.Verify[hash] = map[int]string{1: "pass", 2: "manual"}
	r := h.Handle(context.Background(), startEv("startup", "s1"))
	if r.Ask != nil {
		t.Fatalf("offered a finished plan: %q", r.Notice)
	}
	if m := s.marks[legacy.ScopeHalfPlan+"/"+legacy.HalfPlanKey("/p/.nxy/local", hash)]; m.Answer != legacy.AnswerClosed {
		t.Fatalf("mark = %+v", m)
	}
	reads := d.memReads
	h.Handle(context.Background(), startEv("startup", "s2"))
	if d.memReads != reads {
		t.Errorf("memory read again: %d -> %d", reads, d.memReads)
	}
}

func TestLegacyPlanReplanStartsFeatureWithTheGoal(t *testing.T) {
	h, _, s, _, hash := halfPlanWorld(t)
	r := h.Handle(context.Background(), startEv("startup", "s1"))
	r = h.Handle(context.Background(), planAnswerEv("s1", r.Ask.Question, legacy.HalfPlanReplan+" (recomendada)"))
	if r.Next == nil {
		t.Fatalf("no feature step: %+v", r)
	}
	req := r.Next.Feature.Request
	if !strings.HasPrefix(req, "migrar el login\n") || !strings.Contains(req, "    ## Plan") || !strings.Contains(req, "    ### Batch 2") {
		t.Errorf("request = %q", req)
	}
	if m := s.marks[legacy.ScopeHalfPlan+"/"+legacy.HalfPlanKey("/p/.nxy/local", hash)]; m.Answer != legacy.AnswerReplan {
		t.Errorf("mark = %+v", m)
	}
	// with the feature active nothing is offered again
	if r := h.Handle(context.Background(), startEv("startup", "s2")); r.Ask != nil && strings.Contains(r.Ask.Question, "1.x") {
		t.Errorf("offered with an active feature")
	}
}

func TestLegacyPlanReplanWithActiveFeatureIsBusyAndNotMarked(t *testing.T) {
	h, _, s, _, hash := halfPlanWorld(t)
	r := h.Handle(context.Background(), startEv("startup", "s1"))
	q := r.Ask.Question
	fr := h.Handle(context.Background(), prompt("/nxy:feature algo"))
	if fr.Next == nil {
		t.Fatalf("no feature started: %+v", fr)
	}
	id := fr.Next.Feature.ID
	r = h.Handle(context.Background(), planAnswerEv("s1", q, legacy.HalfPlanReplan))
	if r.Next != nil || !strings.Contains(r.Context, id) || !strings.Contains(r.Context, "in progress") {
		t.Errorf("response = %+v, want busy context with %q", r, id)
	}
	if _, ok := s.marks[legacy.ScopeHalfPlan+"/"+legacy.HalfPlanKey("/p/.nxy/local", hash)]; ok {
		t.Errorf("busy answer must not mark the plan")
	}
}

func TestLegacyPlanAnswerForMarkedHashIsClosed(t *testing.T) {
	h, _, _, _, hash := halfPlanWorld(t)
	q := legacy.HalfPlanQuestion(legacy.HalfPlan{Hash: hash, Goal: "migrar el login"})
	h.Handle(context.Background(), planAnswerEv("s1", q, legacy.HalfPlanIgnore))
	r := h.Handle(context.Background(), planAnswerEv("s1", q, legacy.HalfPlanReplan))
	if r.Next != nil || !strings.Contains(r.Context, "no longer open") {
		t.Errorf("response = %+v", r)
	}
}

func TestLegacyPlanNotOfferedWithActiveFeatureOnResumeOrWithoutFiles(t *testing.T) {
	h, _, _, _, _ := halfPlanWorld(t)
	for _, src := range []string{"resume", "compact"} {
		if r := h.Handle(context.Background(), startEv(src, "s1")); r.Ask != nil {
			t.Errorf("%s offered", src)
		}
	}
	h.Handle(context.Background(), prompt("/nxy:feature algo"))
	if r := h.Handle(context.Background(), startEv("startup", "s2")); r.Ask != nil && strings.Contains(r.Ask.Question, "1.x") {
		t.Errorf("offered with an active feature")
	}

	h, d, _, _ := legacyHooks(t)
	d.hasMem = true
	if r := h.Handle(context.Background(), startEv("startup", "s3")); r.Ask != nil || d.memReads != 1 {
		t.Errorf("ask=%v memReads=%d: only the import reads memory", r.Ask, d.memReads)
	}
}

func TestLegacyPlanIgnoreAsksAboutTheNotes(t *testing.T) {
	h, _, _, mem, _ := halfPlanWorld(t)
	mem.notes = twoNotes()
	r := h.Handle(context.Background(), startEv("startup", "s1"))
	r = h.Handle(context.Background(), planAnswerEv("s1", r.Ask.Question, legacy.HalfPlanIgnore))
	if r.Ask == nil || !strings.Contains(r.Ask.Options[0].Label, "N1") {
		t.Errorf("no notes question: %+v", r.Ask)
	}
}
