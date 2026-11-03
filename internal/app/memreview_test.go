package app

import (
	"context"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/memory"
)

// reviewMemoryRec records what a saved review gives the memory.
type reviewMemoryRec struct {
	deliverMemory
	cited     []string
	learnings []memory.Learning
}

func (m *reviewMemoryRec) Cite(_ context.Context, _ string, shorts []string, _ int64) ([]string, error) {
	m.cited = append(m.cited, shorts...)
	return shorts, nil
}

func (m *reviewMemoryRec) AddLearning(_ context.Context, l memory.Learning) (memory.Learning, error) {
	m.learnings = append(m.learnings, l)
	return l, nil
}

func reviewMemorySetup(t *testing.T) (Hooks, *memStore, *reviewMemoryRec, string) {
	t.Helper()
	h, st := flowHooks(false)
	h.Exe = "/opt/nxy/nxy"
	mem := &reviewMemoryRec{deliverMemory: deliverMemory{
		rules:     []memory.Rule{deliverRule("U1", "K1", "always wrap errors")},
		learnings: []memory.Learning{{Meta: memory.Meta{ID: "L9"}, Short: "A9", Text: "calc is slow", Files: []string{"a.go"}}},
	}}
	h.Memory = mem
	id := reviewingFeature(t, h, st, false, "api")
	// The plan names a.go, so an empty diff falls back to it.
	st.feats[id].Plan.Batches = []domain.Batch{{N: 1, Change: []domain.Change{{At: "a.go:1", What: "x"}}}}
	return h, st, mem, id
}

func dispatchAPI(t *testing.T, h Hooks, id string) HookResponse {
	t.Helper()
	ev := agentEv("PreToolUse", roleReviewer)
	ev.AgentPrompt = "feature " + id + " review api"
	r := h.Handle(context.Background(), ev)
	if r.Deny != "" {
		t.Fatalf("dispatch: %+v", r)
	}
	return r
}

const ruleFinding = `{"lens":"api","findings":[{"severity":"low","file":"a.go","line":3,"title":"t","scenario":"x -> boom","fix":"f","rule":"K1"}],` +
	`"learnings":[{"text":"calc is slow","files":["a.go"]}],"false":["A9"]}`

func TestReviewMemoryRulesReachTheReviewer(t *testing.T) {
	h, st, _, id := reviewMemorySetup(t)
	r := dispatchAPI(t, h, id)
	if !strings.Contains(r.Prompt, "K1: always wrap errors") || !strings.Contains(r.Prompt, "Repo rules") {
		t.Errorf("prompt lacks the rules:\n%s", r.Prompt)
	}
	if got := st.feats[id].Review.Lenses[0].Delivered; len(got) != 2 || got[0] != "K1" || got[1] != "A9" {
		t.Errorf("delivered = %v", got)
	}
}

// R6: with an empty diff the reviewer still gets the learnings of the files the feature changed.
func TestReviewMemoryEmptyDiffFallsBackToChangedFiles(t *testing.T) {
	h, _, mem, id := reviewMemorySetup(t)
	r := dispatchAPI(t, h, id)
	if !strings.Contains(r.Prompt, "A9: calc is slow") {
		t.Errorf("prompt lacks the learning:\n%s", r.Prompt)
	}
	if len(mem.relatedFor) == 0 {
		t.Error("Related was not asked for any file")
	}
}

// R4: a false that names a learning not delivered to the reviewer is refused.
func TestReviewMemoryRejectsUndeliveredFalse(t *testing.T) {
	h, _, mem, id := reviewMemorySetup(t)
	dispatchAPI(t, h, id)
	body := `{"lens":"api","findings":[],"false":["A5"]}`
	res, err := h.SaveReview(context.Background(), id, "api", strings.NewReader(body))
	if err != nil || len(res.Problems) == 0 {
		t.Errorf("accepted: %+v (%v)", res, err)
	}
	if len(mem.obsolete) != 0 {
		t.Errorf("obsolete = %v", mem.obsolete)
	}
}

func TestReviewMemoryRejectsUndeliveredRuleAndBadLearnings(t *testing.T) {
	h, st, mem, id := reviewMemorySetup(t)
	dispatchAPI(t, h, id)
	v := st.version[id]
	long := strings.Repeat("x", memory.ReportMaxRunes+1)
	bad := []string{
		strings.Replace(ruleFinding, `"rule":"K1"`, `"rule":"K9"`, 1),
		`{"lens":"api","findings":[],"learnings":[{"text":"  "}]}`,
		`{"lens":"api","findings":[],"learnings":[{"text":"` + long + `"}]}`,
		`{"lens":"api","findings":[],"false":["K1"]}`,
		`{"lens":"api","findings":[],"learnings":[{"text":"a"},{"text":"b"},{"text":"c"},{"text":"d"},{"text":"e"},{"text":"f"}]}`,
	}
	for _, body := range bad {
		res, err := h.SaveReview(context.Background(), id, "api", strings.NewReader(body))
		if err != nil || len(res.Problems) == 0 {
			t.Errorf("accepted: %.80s (%v)", body, err)
		}
	}
	if st.version[id] != v || len(mem.cited) != 0 || len(mem.learnings) != 0 {
		t.Error("a refused submission changed something")
	}
}

func TestReviewMemoryCitesLearnsAndMarksFalse(t *testing.T) {
	h, st, mem, id := reviewMemorySetup(t)
	dispatchAPI(t, h, id)
	if res, err := h.SaveReview(context.Background(), id, "api", strings.NewReader(ruleFinding)); err != nil || len(res.Problems) > 0 {
		t.Fatalf("save = %+v %v", res, err)
	}
	if len(mem.cited) != 1 || mem.cited[0] != "K1" {
		t.Errorf("cited = %v", mem.cited)
	}
	if len(mem.learnings) != 1 || mem.learnings[0].Text != "calc is slow" || mem.learnings[0].FeatureID != id {
		t.Errorf("learnings = %+v", mem.learnings)
	}
	if len(mem.obsolete) != 1 || mem.obsolete[0] != "A9" {
		t.Errorf("obsolete = %v", mem.obsolete)
	}
	fs := st.feats[id].Review.Findings
	if len(fs) != 1 || fs[0].Rule != "K1" || fs[0].Severity != domain.SeverityMedium {
		t.Errorf("findings = %+v", fs)
	}
}
