package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/flow"
	"github.com/mananos/nxy.dev/internal/memory"
	"github.com/mananos/nxy.dev/internal/plan"
)

// ruleMemory records the rules and the closed feature the close saves.
type ruleMemory struct {
	deliverMemory
	added   []memory.Rule
	closed  []memory.ClosedFeature
	similar map[string]memory.Rule // by candidate text
}

func (m *ruleMemory) AddRule(_ context.Context, r memory.Rule) (memory.Rule, error) {
	m.added = append(m.added, r)
	return r, nil
}
func (m *ruleMemory) SaveClosedFeature(_ context.Context, f memory.ClosedFeature) (memory.ClosedFeature, error) {
	m.closed = append(m.closed, f)
	return f, nil
}
func (m *ruleMemory) SimilarRules(_ context.Context, _, text string, _ int) ([]memory.Rule, error) {
	if r, ok := m.similar[text]; ok {
		return []memory.Rule{r}, nil
	}
	return nil, nil
}

func repoDecisions() []domain.Decision {
	return []domain.Decision{
		{Scope: domain.ScopeRepo, Text: "Wrap errors with %w", Question: "How do we wrap errors?"},
		{Scope: domain.ScopeRepo, Text: "Tests use t.TempDir", Question: "Where do tests write?"},
		{Scope: domain.ScopeRepo, Text: "wrap errors with %w.", Question: "dup"},
		{Scope: domain.ScopeRepo, Text: "No question here"},
		{Scope: domain.ScopeTask, Text: "Greeting stays English", Question: "Which language?"},
	}
}

// reviewedWithDecisions is a feature whose review just finished clean, with repo decisions in its plan.
func reviewedWithDecisions(t *testing.T, mem *ruleMemory) (Hooks, *memStore, string) {
	t.Helper()
	h, st, id := reviewedClean(t, nil)
	if mem != nil {
		h.Memory = mem
	}
	f := st.feats[id]
	p := validPlan()
	p.Title = "Greet"
	p.Decisions = repoDecisions()
	f.Plan = &p
	st.feats[id] = f
	return h, st, id
}

func TestRuleCandidatesByMode(t *testing.T) {
	p := validPlan()
	p.Decisions = repoDecisions()
	got := ruleCandidates(domain.Feature{Plan: &p})
	if len(got) != 2 || got[0].Origin != memory.OriginPlan || got[1].Text != "Tests use t.TempDir" {
		t.Fatalf("delegate candidates = %+v", got)
	}
	f := domain.Feature{
		Mode: domain.ModeEngineer, Plan: &p,
		Design: &domain.Design{
			Agenda:    []domain.Fork{{ID: "B1", Title: "How to wrap errors?"}},
			Decisions: []domain.Decision{{ID: "D1", Scope: domain.ScopeRepo, Text: "Wrap with %w", Why: "traceable", Fork: "B1"}, {ID: "D2", Scope: domain.ScopeRepo, Text: "Use slog", Why: "structured", Fork: "B9"}, {ID: "D3", Scope: domain.ScopeTask, Text: "x", Why: "y"}},
		},
		Candidates: []domain.RuleCandidate{{Text: "use slog", Question: "which logger", Origin: memory.OriginReview}},
	}
	got = ruleCandidates(f)
	if len(got) != 2 {
		t.Fatalf("engineer candidates = %+v", got)
	}
	if got[0].Origin != memory.OriginEngineer || got[0].Question != "How to wrap errors?" || got[1].Question != "structured" {
		t.Errorf("engineer questions = %+v", got)
	}
}

func TestRuleCandidatesAreCapped(t *testing.T) {
	f := domain.Feature{}
	for i := 0; i < 30; i++ {
		f.Candidates = append(f.Candidates, domain.RuleCandidate{Text: "rule " + strings.Repeat("x", i), Question: "q"})
	}
	if got := ruleCandidates(f); len(got) != maxRuleCandidates {
		t.Errorf("len = %d, want %d", len(got), maxRuleCandidates)
	}
}

func TestCloseWithoutMemoryOrCandidatesFinishes(t *testing.T) {
	h, st, id := reviewedWithDecisions(t, nil)
	nf, _, err := h.advance(context.Background(), id, flow.Event{Kind: flow.ReviewDone})
	if err != nil {
		t.Fatal(err)
	}
	if nf.State.Phase != domain.PhaseDone || st.feats[id].RuleOffer != nil {
		t.Errorf("without memory: %+v", nf.State)
	}
	mem := &ruleMemory{}
	h, st, id = reviewedClean(t, nil)
	h.Memory = mem
	f := st.feats[id]
	p := validPlan()
	f.Plan = &p
	st.feats[id] = f
	nf, _, _ = h.advance(context.Background(), id, flow.Event{Kind: flow.ReviewDone})
	if nf.State.Phase != domain.PhaseDone || len(mem.closed) != 1 {
		t.Errorf("no candidates: %+v closed=%d", nf.State, len(mem.closed))
	}
}

func TestCloseAsksAndSavesTheChosenRules(t *testing.T) {
	mem := &ruleMemory{similar: map[string]memory.Rule{"Tests use t.TempDir": {Short: "K3", Text: "Tests live in tmp dirs"}}}
	h, st, id := reviewedWithDecisions(t, mem)
	nf, acts, err := h.advance(context.Background(), id, flow.Event{Kind: flow.ReviewDone})
	if err != nil {
		t.Fatal(err)
	}
	if nf.State.Phase != domain.PhaseRules || nf.Next.Kind != domain.AskRules || len(acts) != 2 {
		t.Fatalf("feature = %+v %+v acts=%+v", nf.State, nf.Next, acts)
	}
	offer := st.feats[id].RuleOffer
	if len(offer) != 2 || offer[0].ID != "S1" || offer[1].Supersedes != "K3" || offer[1].SupersedesText == "" {
		t.Fatalf("offer = %+v", offer)
	}
	if len(mem.closed) != 0 || len(mem.added) != 0 {
		t.Fatal("nothing is saved before the answer")
	}
	// Free text asks again.
	r := answer(h, plan.RulesQuestion(id, 1, 1), "las dos")
	if r.Next == nil || !strings.Contains(r.Next.Note, "S number") || st.feats[id].State.Phase != domain.PhaseRules {
		t.Fatalf("free text: %+v", r)
	}
	// Pick S2 only: one rule with its origin, question, feature and the rule it replaces.
	r = answer(h, plan.RulesQuestion(id, 1, 1), "S2 Tests use t.TempDir")
	if r.Next == nil || st.feats[id].State.Phase != domain.PhaseDone || st.feats[id].State.Outcome != domain.OutcomeSuccess {
		t.Fatalf("after pick: %+v %+v", st.feats[id].State, r)
	}
	if len(mem.added) != 1 {
		t.Fatalf("added = %+v", mem.added)
	}
	a := mem.added[0]
	if a.Text != "Tests use t.TempDir" || a.Question != "Where do tests write?" || a.Origin != memory.OriginPlan || a.FeatureID != id || a.Supersedes != "K3" || a.Repo == "" {
		t.Errorf("rule = %+v", a)
	}
	if len(mem.closed) != 1 {
		t.Fatalf("closed = %+v", mem.closed)
	}
	c := mem.closed[0]
	if c.ID != id || c.Title != "Greet" || len(c.Decisions) != 1 || c.Decisions[0] != "Greeting stays English" {
		t.Errorf("closed feature = %+v", c)
	}
	if len(c.Files) != 2 || c.Files[0].Path != "pkg/greet.go" {
		t.Errorf("closed files = %+v", c.Files)
	}
}

func TestCloseChoosingTwoOfThreeSavesTwo(t *testing.T) {
	mem := &ruleMemory{}
	h, st, id := reviewedWithDecisions(t, mem)
	f := st.feats[id]
	f.Candidates = []domain.RuleCandidate{{Text: "Never log tokens", Question: "What goes in logs?", Origin: memory.OriginReview}}
	st.feats[id] = f
	_, _, _ = h.advance(context.Background(), id, flow.Event{Kind: flow.ReviewDone})
	if n := len(st.feats[id].RuleOffer); n != 3 {
		t.Fatalf("offer = %d", n)
	}
	answer(h, plan.RulesQuestion(id, 1, 1), "S1 Wrap errors, S3 Never log tokens")
	if len(mem.added) != 2 || mem.added[1].Origin != memory.OriginReview || mem.added[1].Question != "What goes in logs?" {
		t.Errorf("added = %+v", mem.added)
	}
}

func TestStopAtTheRulesQuestionSavesNothing(t *testing.T) {
	mem := &ruleMemory{}
	h, st, id := reviewedWithDecisions(t, mem)
	_, _, _ = h.advance(context.Background(), id, flow.Event{Kind: flow.ReviewDone})
	if st.feats[id].State.Phase != domain.PhaseRules {
		t.Fatalf("phase = %v", st.feats[id].State.Phase)
	}
	if _, _, err := h.StopFeature(context.Background(), "/p"); err != nil {
		t.Fatal(err)
	}
	if st.feats[id].State.Outcome != domain.OutcomeStopped || len(mem.added) != 0 || len(mem.closed) != 0 {
		t.Errorf("state = %+v added=%d closed=%d", st.feats[id].State, len(mem.added), len(mem.closed))
	}
}

// R1: an S<n> inside the text of an option is not a pick.
func TestRulesAnswerIgnoresIDsInsideRuleText(t *testing.T) {
	mem := &ruleMemory{}
	h, st, id := reviewedWithDecisions(t, mem)
	f := st.feats[id]
	f.Candidates = []domain.RuleCandidate{{Text: "Do not mix S1 with S2", Question: "q", Origin: memory.OriginChat}}
	st.feats[id] = f
	_, _, _ = h.advance(context.Background(), id, flow.Event{Kind: flow.ReviewDone})
	offer := st.feats[id].RuleOffer
	if len(offer) != 3 {
		t.Fatalf("offer = %+v", offer)
	}
	answer(h, plan.RulesQuestion(id, 1, 1), offer[2].ID+" "+offer[2].Text)
	if len(mem.added) != 1 || mem.added[0].Text != "Do not mix S1 with S2" {
		t.Errorf("added = %+v", mem.added)
	}
}

// R2: a stale answer (the feature already left the rules phase) saves nothing.
func TestStaleRulesAnswerSavesNothing(t *testing.T) {
	mem := &ruleMemory{}
	h, st, id := reviewedWithDecisions(t, mem)
	_, _, _ = h.advance(context.Background(), id, flow.Event{Kind: flow.ReviewDone})
	stale := st.feats[id]
	if _, _, err := h.StopFeature(context.Background(), "/p"); err != nil {
		t.Fatal(err)
	}
	h.answerRules(context.Background(), stale, map[string]string{plan.RulesQuestion(id, 1, 1): "S1 Wrap errors with %w"})
	if len(mem.added) != 0 {
		t.Errorf("added = %+v", mem.added)
	}
}

// failSupersedes fails a rule whose Supersedes no longer resolves.
type failSupersedes struct{ *ruleMemory }

func (m failSupersedes) AddRule(ctx context.Context, r memory.Rule) (memory.Rule, error) {
	if r.Supersedes != "" {
		return memory.Rule{}, errors.New("unknown rule " + r.Supersedes)
	}
	return m.ruleMemory.AddRule(ctx, r)
}

// R2: a Supersedes that no longer resolves does not fail the answer; a live equal rule is not repeated.
func TestRulesAnswerSkipsDeadSupersedesAndDuplicates(t *testing.T) {
	mem := &ruleMemory{similar: map[string]memory.Rule{"Tests use t.TempDir": {Short: "K3", Text: "x"}}}
	mem.rules = []memory.Rule{{Text: "wrap errors with %w"}}
	h, st, id := reviewedWithDecisions(t, mem)
	h.Memory = failSupersedes{mem}
	_, _, _ = h.advance(context.Background(), id, flow.Event{Kind: flow.ReviewDone})
	answer(h, plan.RulesQuestion(id, 1, 1), "S1 Wrap errors with %w, S2 Tests use t.TempDir")
	if st.feats[id].State.Phase != domain.PhaseDone {
		t.Fatalf("phase = %v", st.feats[id].State.Phase)
	}
	if len(mem.added) != 1 || mem.added[0].Text != "Tests use t.TempDir" || mem.added[0].Supersedes != "" {
		t.Errorf("added = %+v", mem.added)
	}
}

// R3: with the closing question open, an escape is saved now as a rule and recorded.
func TestReviewEscapeInRulesPhaseSavesTheRule(t *testing.T) {
	mem := &ruleMemory{}
	h, st, id := reviewedWithDecisions(t, mem)
	_, _, _ = h.advance(context.Background(), id, flow.Event{Kind: flow.ReviewDone})
	if st.feats[id].State.Phase != domain.PhaseRules {
		t.Fatalf("phase = %v", st.feats[id].State.Phase)
	}
	res, err := h.ReviewEscape(context.Background(), "/p", "Never log tokens", "What goes in logs?", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Candidate || len(mem.added) != 1 || mem.added[0].Origin != memory.OriginReview {
		t.Errorf("res = %+v added = %+v", res, mem.added)
	}
}

// R9: the candidates flag is read off the stored feature, not the copy the hook read.
func TestDocsAnswerReadsCandidatesFromTheStoredFeature(t *testing.T) {
	mem := &ruleMemory{}
	h, st, id := reviewedWithDecisions(t, mem)
	f := st.feats[id]
	f.State = domain.State{Phase: domain.PhaseDocs, Batch: 1, Batches: 1}
	st.feats[id] = f
	stale := f
	stale.Plan = nil
	h.answerDocs(context.Background(), stale, "Dejar")
	if got := st.feats[id].State.Phase; got != domain.PhaseRules {
		t.Errorf("phase = %v, want rules", got)
	}
}

func TestRulesPhaseLeftHalfwayBuildsItsOffer(t *testing.T) {
	mem := &ruleMemory{}
	h, st, id := reviewedWithDecisions(t, mem)
	f := st.feats[id]
	f.State = domain.State{Phase: domain.PhaseRules, Batch: 1, Batches: 1}
	f.Next = domain.Action{Kind: domain.AskRules}
	st.feats[id] = f
	got := h.ensureRules(context.Background(), st.feats[id])
	if len(got.RuleOffer) != 2 || len(st.feats[id].RuleOffer) != 2 {
		t.Errorf("offer = %+v", got.RuleOffer)
	}
}

func TestAddRuleCandidate(t *testing.T) {
	mem := &ruleMemory{}
	h, st, id := reviewedWithDecisions(t, mem)
	ctx := context.Background()
	if _, err := h.AddRuleCandidate(ctx, "/nowhere", "t", "q", ""); err != ErrNoActiveFeature {
		t.Errorf("no feature: err = %v", err)
	}
	if _, err := h.AddRuleCandidate(ctx, "/p", "text", "", ""); err == nil {
		t.Error("a candidate without a question was accepted")
	}
	if _, err := h.AddRuleCandidate(ctx, "/p", "Use slog", "Which logger?", memory.OriginChat); err != nil {
		t.Fatal(err)
	}
	if _, err := h.AddRuleCandidate(ctx, "/p", "use slog.", "Which logger?", memory.OriginChat); err != nil {
		t.Fatal(err)
	}
	if c := st.feats[id].Candidates; len(c) != 1 || c[0].Origin != memory.OriginChat {
		t.Errorf("candidates = %+v", c)
	}
	if len(mem.added) != 0 {
		t.Error("a candidate is not a rule")
	}
}
