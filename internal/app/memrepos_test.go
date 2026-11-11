package app

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/memory"
	"github.com/mananos/nxy.dev/internal/review"
)

// keyedMemory serves rules per repo key and records which key each false reference went to.
type keyedMemory struct {
	*deliverMemory
	byKey    map[string][]memory.Rule
	obsolete map[string][]string
}

func (m *keyedMemory) Rules(_ context.Context, key string) ([]memory.Rule, error) {
	return m.byKey[key], nil
}

func (m *keyedMemory) MarkObsolete(_ context.Context, key string, refs []string, _ int64) error {
	if m.obsolete == nil {
		m.obsolete = map[string][]string{}
	}
	m.obsolete[key] = append(m.obsolete[key], refs...)
	return nil
}

func twoRepoFeature(t *testing.T) domain.Feature {
	t.Helper()
	return domain.Feature{
		ID:         "f1",
		ProjectDir: t.TempDir(),
		Plan:       &domain.Plan{Repos: []domain.PlanRepo{{Path: "api", Name: "api"}, {Path: "web", Name: "web"}}},
		Repos: []domain.FeatureRepo{
			{Name: "api", Key: "key-api", Root: t.TempDir()},
			{Name: "web", Key: "key-web", Root: t.TempDir()},
		},
	}
}

func TestMemDeliverMultiRepoImplementerGetsOnlyTheBatchRepo(t *testing.T) {
	mem := &keyedMemory{deliverMemory: &deliverMemory{}, byKey: map[string][]memory.Rule{
		"key-api": {deliverRule("U1", "K1", "api rule")},
		"key-web": {deliverRule("U2", "K1", "web rule")},
	}}
	h, _, _ := deliverSetup(t, mem.deliverMemory)
	h.Memory = mem
	f := twoRepoFeature(t)
	pkt := h.memoryFor(context.Background(), f, roleImplementer, []string{"api/x.go"})
	if !strings.Contains(pkt.Text, "api rule") || strings.Contains(pkt.Text, "web rule") || strings.Contains(pkt.Text, "[") {
		t.Fatalf("text = %q", pkt.Text)
	}
	both := h.memoryFor(context.Background(), f, roleReviewer, []string{"api/x.go", "web/y.go"})
	if !strings.Contains(both.Text, "[api] K1: api rule") || !strings.Contains(both.Text, "[web] K1: web rule") {
		t.Fatalf("reviewer text = %q", both.Text)
	}
	if got := both.Origins["K1"]; len(got) != 2 {
		t.Errorf("origins of K1 = %v, want both repos", got)
	}
}

type citeSpy struct {
	*keyedMemory
	cites [][]string
}

func (m *citeSpy) Cite(_ context.Context, _ string, shorts []string, _ int64) ([]string, error) {
	m.cites = append(m.cites, shorts)
	return shorts, nil
}

func TestReviewMemoryGlobalRuleCitedOnceInTwoRepos(t *testing.T) {
	km := &keyedMemory{deliverMemory: &deliverMemory{}, byKey: map[string][]memory.Rule{}}
	mem := &citeSpy{keyedMemory: km}
	h, _, _ := deliverSetup(t, km.deliverMemory)
	h.Memory = mem
	f := twoRepoFeature(t)
	sub := review.Submission{Findings: []domain.Finding{
		{Rule: "G2", File: "api/x.go"},
		{Rule: "G2", File: "web/y.go"},
	}}
	h.reviewMemory(context.Background(), f, "", sub, nil)
	if len(mem.cites) != 1 || len(mem.cites[0]) != 1 || mem.cites[0][0] != "G2" {
		t.Errorf("cites = %v, want one citation of G2", mem.cites)
	}
}

func TestReviewDiffsMultiRepoLabeled(t *testing.T) {
	h, _, _ := deliverSetup(t, nil)
	f := twoRepoFeature(t)
	f.Repos[0].Base, f.Repos[1].Base = "b1", "b2"
	h.Diffs = repoDiffs{f.Repos[0].Root: {{Path: "a.go", Status: "M"}}, f.Repos[1].Root: {{Path: "src/b.go", Status: "A"}}}
	var got []string
	for _, d := range h.reviewDiffs(context.Background(), f) {
		got = append(got, d.Path)
	}
	if strings.Join(got, ",") != "api/a.go,web/src/b.go" {
		t.Fatalf("paths = %v", got)
	}
}

type repoDiffs map[string][]review.FileDiff

func (d repoDiffs) Diff(_ context.Context, dir, _ string) ([]review.FileDiff, error) {
	return append([]review.FileDiff(nil), d[dir]...), nil
}

type keyedLog struct{ byKey map[string][]string }

func (l *keyedLog) RecordPicks(_ context.Context, _, key string, picks []domain.Pick) error {
	if l.byKey == nil {
		l.byKey = map[string][]string{}
	}
	for _, p := range picks {
		l.byKey[key] = append(l.byKey[key], p.Finding)
	}
	return nil
}

func TestReviewPicksGroupedByRepo(t *testing.T) {
	h, st, _, _, id := reviewedFeature(t, domain.PhaseChoosingFindings, domain.AskFindings,
		fixFinding("H1", "base", domain.SeverityHigh, "api/a.go"),
		fixFinding("H2", "base", domain.SeverityHigh, "web/b.go"))
	log := &keyedLog{}
	h.ReviewLog = log
	f := st.feats[id]
	f.Repos = twoRepoFeature(t).Repos
	st.feats[id] = f
	h.Handle(context.Background(), answerEv(id, map[int]string{1: "H1 title H1, H2 title H2"}, 1))
	if strings.Join(log.byKey["key-api"], ",") != "H1" || strings.Join(log.byKey["key-web"], ",") != "H2" {
		t.Fatalf("picks by repo = %v", log.byKey)
	}
}

func TestHarvestFalseAmbiguousAppliesToNone(t *testing.T) {
	mem := &keyedMemory{deliverMemory: &deliverMemory{}}
	h, _, id := deliverSetup(t, mem.deliverMemory)
	h.Memory = mem
	ctx := context.Background()
	tf := twoRepoFeature(t)
	nf, _, err := h.mutate(ctx, id, func(g *domain.Feature) bool {
		g.Repos, g.Plan.Repos = tf.Repos, tf.Plan.Repos
		g.Delivered = map[string][]string{"A3": {"api", "web"}, "A4": {"web"}, "A5": {"api"}}
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	ev := HookEvent{AgentID: "ag1", AgentResult: "done\nfalse: A3\nfalse: A4\nfalse: [api] A5\nfalse: web:A3\nfalse: api:A9"}
	h.harvest(ctx, nf, ev)
	var gotAPI, gotWeb []string
	gotAPI, gotWeb = mem.obsolete["key-api"], mem.obsolete["key-web"]
	sort.Strings(gotWeb)
	// A3 alone is ambiguous (none); A4 came only from web; [api] A5 was delivered from api;
	// web:A3 names the repo that delivered it; api:A9 was never delivered.
	if strings.Join(gotAPI, ",") != "A5" || strings.Join(gotWeb, ",") != "A3,A4" {
		t.Fatalf("api = %v web = %v", gotAPI, gotWeb)
	}
}
