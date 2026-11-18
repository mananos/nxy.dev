package app

import (
	"context"

	"github.com/mananos/nxy.dev/v2/internal/legacy"
)

// halfPlans are the 1.x plans of this checkout that were approved and left half-finished and that
// the user has not answered yet. Every candidate that turns out to have nothing left is marked
// closed, so memory.db is only read while an unmarked candidate exists.
func (h Hooks) halfPlans(ctx context.Context, dir string) ([]legacy.HalfPlan, error) {
	d, s := h.LegacyData, h.LegacyStore
	if d == nil || s == nil || dir == "" {
		return nil, nil
	}
	files, err := d.PlanFiles(dir)
	if err != nil {
		return nil, err
	}
	if len(files.Approved) == 0 && len(files.Verify) == 0 {
		return nil, nil
	}
	marks, _, err := s.LegacyImports(ctx)
	if err != nil {
		return nil, err
	}
	checkout := d.Checkout(dir)
	done := map[string]bool{}
	for _, m := range marks {
		if m.Scope == legacy.ScopeHalfPlan {
			done[m.Key] = true
		}
	}
	cands := legacy.Candidates(files, func(hash string) bool { return done[legacy.HalfPlanKey(checkout, hash)] })
	if len(cands) == 0 {
		return nil, nil
	}
	mems, _, err := d.Memory(ctx, d.ProjectKey(dir))
	if err != nil {
		return nil, err
	}
	open, closed := legacy.FindHalfPlans(mems, files, cands)
	for _, hash := range closed {
		_, _ = s.MarkLegacyPlan(ctx, legacy.HalfPlanKey(checkout, hash), legacy.AnswerClosed, h.now().UnixMilli())
	}
	return open, nil
}

// offerLegacyPlan is the notice and the question for the newest half-finished 1.x plan, if any.
func (h Hooks) offerLegacyPlan(ctx context.Context, dir string) (string, *AskSpec) {
	open, err := h.halfPlans(ctx, dir)
	if err != nil || len(open) == 0 {
		return "", nil
	}
	hp := open[0]
	return legacy.HalfPlanNotice(hp), &AskSpec{
		Question: legacy.HalfPlanQuestion(hp),
		Header:   "Feature de la 1.x",
		Options: []AskOption{
			{Label: legacy.HalfPlanReplan + " (recomendada)", Description: "Arranca una feature de la v2 con ese pedido; el planner arma un plan nuevo y no se toca nada de la 1.x"},
			{Label: legacy.HalfPlanIgnore, Description: "No se vuelve a ofrecer; lo que faltaba queda sin hacer"},
		},
	}
}

// answerLegacyPlan acts on the answer to the half-finished 1.x plan question. ok is false when the
// answers are not to that question.
func (h Hooks) answerLegacyPlan(ctx context.Context, ev HookEvent) (HookResponse, bool) {
	hash, answer := "", ""
	for q, a := range ev.Answers {
		if got, ok := legacy.ParseHalfPlanHash(q); ok {
			hash, answer = got, a
			break
		}
	}
	if hash == "" {
		return HookResponse{}, false
	}
	if h.LegacyData == nil || h.LegacyStore == nil {
		return HookResponse{}, true
	}
	dir := ev.ProjectDir
	if dir == "" {
		dir = ev.Cwd
	}
	key := legacy.HalfPlanKey(h.LegacyData.Checkout(dir), hash)
	mark := func(a string) { _, _ = h.LegacyStore.MarkLegacyPlan(ctx, key, a, h.now().UnixMilli()) }
	open, err := h.halfPlans(ctx, dir)
	if err != nil {
		return HookResponse{}, true
	}
	var hp *legacy.HalfPlan
	for i := range open {
		if open[i].Hash == hash {
			hp = &open[i]
		}
	}
	if hp == nil {
		mark(legacy.AnswerClosed)
		return HookResponse{Context: "nxy: that 1.x feature is no longer open; carry on with the user's message"}, true
	}
	if trimLabel(answer) != legacy.HalfPlanReplan {
		if trimLabel(answer) != legacy.HalfPlanIgnore {
			return HookResponse{}, true
		}
		mark(legacy.AnswerIgnore)
		if spec, ok := h.notesAsk(ctx, sessionFolder(ev), ""); ok {
			return HookResponse{Ask: spec}, true
		}
		return HookResponse{}, true
	}
	if h.Store == nil || h.Repos == nil {
		return HookResponse{}, true
	}
	if f, ok := h.linkedFeature(ctx, ev); ok {
		return legacyBusy(f.ID), true
	}
	if f, found, err := h.Store.ActiveFeature(ctx, h.Repos.Checkout(dir)); err != nil {
		return HookResponse{}, true
	} else if found && !h.heldByOther(ctx, ev, f) {
		return legacyBusy(f.ID), true
	}
	r := h.beginFeature(ctx, ev, dir, hp.Request(), false)
	if r.Next != nil || r.Ask != nil {
		mark(legacy.AnswerReplan)
	}
	return r, true
}

func legacyBusy(id string) HookResponse {
	return HookResponse{Context: "nxy: feature " + id + " is in progress; the 1.x one is offered again in a later session; carry on with the user's message"}
}

// trimLabel drops the " (recomendada)" tail the offer adds to its first option.
func trimLabel(answer string) string {
	const tail = " (recomendada)"
	if n := len(answer) - len(tail); n >= 0 && answer[n:] == tail {
		return answer[:n]
	}
	return answer
}
