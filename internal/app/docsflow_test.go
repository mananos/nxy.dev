package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/docs"
	"github.com/mananos/nxy.dev/v2/internal/domain"
	"github.com/mananos/nxy.dev/v2/internal/flow"
	"github.com/mananos/nxy.dev/v2/internal/review"
)

type fakeDocs struct {
	res   domain.Docs
	calls *[][]string
}

func (d fakeDocs) Find(_ context.Context, _ string, changed []string) domain.Docs {
	if d.calls != nil {
		*d.calls = append(*d.calls, changed)
	}
	return d.res
}

var readmeDocs = domain.Docs{
	Terms: []string{"greet"},
	Docs:  []domain.DocFile{{Path: "README.md", Lines: []domain.DocLine{{Line: 4, Text: "call greet()"}}}},
}

// reviewedClean is a feature whose review just finished with nothing to fix.
func reviewedClean(t *testing.T, d Docs) (Hooks, *memStore, string) {
	t.Helper()
	h, st := flowHooks(false)
	h.Docs = d
	h.Changes = fakeChanges{head: "abc", changed: []string{"pkg/greet.go", "README.md"}}
	h.ReviewFiles = nil
	dir := "/p"
	id, err := st.CreateFeature(context.Background(), domain.Feature{
		Repo: h.Repos.Key(dir), ProjectDir: dir, Request: "add greet", PlanRev: 1, Base: "abc",
		State:  domain.State{Phase: domain.PhaseReviewing, Batch: 1, Batches: 1},
		Next:   domain.Action{Kind: domain.DispatchReviewer},
		Review: &domain.Review{Risk: domain.RiskLow, Quick: true, Lenses: []domain.LensRun{{Name: "base", Saved: true}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return h, st, id
}

func TestCleanReviewWithoutDocsClosesWithoutAsking(t *testing.T) {
	for name, d := range map[string]Docs{"no port": nil, "nothing found": fakeDocs{}} {
		h, st, id := reviewedClean(t, d)
		nf, acts, err := h.advance(context.Background(), id, flow.Event{Kind: flow.ReviewDone})
		if err != nil {
			t.Fatal(err)
		}
		if nf.State.Phase != domain.PhaseDone || nf.State.Outcome != domain.OutcomeSuccess {
			t.Errorf("%s: state = %+v", name, nf.State)
		}
		if len(acts) != 1 || acts[0].Kind != domain.Close {
			t.Errorf("%s: acts = %+v, want Close", name, acts)
		}
		if st.feats[id].Docs != nil {
			t.Errorf("%s: docs stored = %+v", name, st.feats[id].Docs)
		}
	}
}

func TestCleanReviewWithDocsAsks(t *testing.T) {
	var calls [][]string
	h, st, id := reviewedClean(t, fakeDocs{res: readmeDocs, calls: &calls})
	nf, acts, err := h.advance(context.Background(), id, flow.Event{Kind: flow.ReviewDone})
	if err != nil {
		t.Fatal(err)
	}
	if nf.State.Phase != domain.PhaseDocs || nf.Next.Kind != domain.AskDocs {
		t.Fatalf("feature = %+v %+v", nf.State, nf.Next)
	}
	if len(acts) != 2 || acts[0].Kind != domain.AskDocs || acts[1].Kind != domain.Notify {
		t.Errorf("acts = %+v, want the docs question", acts)
	}
	if got := st.feats[id]; got.Docs == nil || len(got.Docs.Docs) != 1 {
		t.Errorf("stored docs = %+v", got.Docs)
	}
	if len(calls) != 1 || len(calls[0]) != 2 || calls[0][0] != "pkg/greet.go" {
		t.Errorf("Find got %v, want the changed files since the base", calls)
	}
}

func TestDocsAnswerLeaveCloses(t *testing.T) {
	h, st, id := reviewedClean(t, fakeDocs{res: readmeDocs})
	_, _, _ = h.advance(context.Background(), id, flow.Event{Kind: flow.ReviewDone})
	r := answer(h, docs.QuestionText(id), docs.LabelLeave)
	if r.Next == nil || st.feats[id].State.Phase != domain.PhaseDone || st.feats[id].State.Outcome != domain.OutcomeSuccess {
		t.Fatalf("stored = %+v resp = %+v", st.feats[id].State, r)
	}
}

func TestDocsAnswerFreeTextAsksAgain(t *testing.T) {
	h, st, id := reviewedClean(t, fakeDocs{res: readmeDocs})
	_, _, _ = h.advance(context.Background(), id, flow.Event{Kind: flow.ReviewDone})
	r := answer(h, docs.QuestionText(id), "mejor no sé")
	if r.Next == nil || !strings.Contains(r.Next.Note, "pick one") || st.feats[id].State.Phase != domain.PhaseDocs {
		t.Errorf("stored = %+v resp = %+v", st.feats[id].State, r)
	}
}

func TestDocsUpdateDispatchesTheDocumenterAndClosesOnReturn(t *testing.T) {
	h, st, id := reviewedClean(t, fakeDocs{res: readmeDocs})
	h.Exe = "/opt/nxy/nxy"
	ctx := context.Background()
	_, _, _ = h.advance(ctx, id, flow.Event{Kind: flow.ReviewDone})
	answer(h, docs.QuestionText(id), docs.LabelUpdate)
	if got := st.feats[id]; got.State.Phase != domain.PhaseDocumenting || got.Next.Kind != domain.DispatchDocumenter {
		t.Fatalf("stored = %+v %+v", got.State, got.Next)
	}
	pre := agentEv("PreToolUse", roleDocumenter)
	r := h.Handle(ctx, pre)
	if r.Deny != "" || !strings.Contains(r.Prompt, "README.md:4: call greet()") || !strings.Contains(r.Prompt, `"/opt/nxy/nxy" review docs --feature `+id) {
		t.Fatalf("pre = %+v", r)
	}
	post := agentEv("PostToolUse", roleDocumenter)
	if r := h.Handle(ctx, post); r.Next == nil {
		t.Fatalf("post = %+v", r)
	}
	if got := st.feats[id]; got.State.Phase != domain.PhaseDone || got.State.Outcome != domain.OutcomeSuccess {
		t.Errorf("stored = %+v", got.State)
	}
}

func TestDocumenterInBackgroundReturnsThroughSubagentStop(t *testing.T) {
	h, st, id := reviewedClean(t, fakeDocs{res: readmeDocs})
	ctx := context.Background()
	_, _, _ = h.advance(ctx, id, flow.Event{Kind: flow.ReviewDone})
	answer(h, docs.QuestionText(id), docs.LabelUpdate)
	post := agentEv("PostToolUse", roleDocumenter)
	post.AgentAsync = true
	if r := h.Handle(ctx, post); r.Next != nil || !strings.Contains(r.Context, "background") {
		t.Fatalf("launch = %+v", r)
	}
	h.Handle(ctx, HookEvent{Name: "SubagentStop", AgentType: roleDocumenter, AgentID: "d1", ProjectDir: "/p", Cwd: "/p"})
	if got := st.feats[id]; got.State.Phase != domain.PhaseDone || !got.Undelivered {
		t.Fatalf("stored = %+v undelivered = %v", got.State, got.Undelivered)
	}
}

func TestDocumenterOutOfOrderIsRefused(t *testing.T) {
	h, _, _ := reviewedClean(t, fakeDocs{res: readmeDocs})
	if r := h.Handle(context.Background(), agentEv("PreToolUse", roleDocumenter)); r.Deny == "" {
		t.Errorf("a documenter before the docs step was allowed: %+v", r)
	}
}

func TestDocsCheckLeftHalfwayIsFinishedByTheNextHook(t *testing.T) {
	h, st, id := reviewedClean(t, fakeDocs{res: readmeDocs})
	f := st.feats[id]
	f.State = domain.State{Phase: domain.PhaseDocsCheck, Batch: 1, Batches: 1}
	f.Next = domain.Action{}
	st.feats[id] = f
	h.Handle(context.Background(), mainEv("Stop"))
	if got := st.feats[id]; got.State.Phase != domain.PhaseDocs || got.Next.Kind != domain.AskDocs {
		t.Errorf("stored = %+v %+v", got.State, got.Next)
	}
}

func TestFindingsPickedNoneAlsoLooksForDocs(t *testing.T) {
	h, st, id := reviewedClean(t, fakeDocs{res: readmeDocs})
	f := st.feats[id]
	f.State = domain.State{Phase: domain.PhaseChoosingFindings, Batch: 1, Batches: 1}
	f.Next = domain.Action{Kind: domain.AskFindings}
	f.Review = &domain.Review{Risk: domain.RiskHigh, Lenses: []domain.LensRun{{Name: "base", Saved: true}},
		Findings: []domain.Finding{fixFinding("H1", "api", domain.SeverityMedium, "pkg/a.go")}}
	st.feats[id] = f
	ev := answerEv(id, map[int]string{1: review.NoneLabel}, 1)
	r := h.Handle(context.Background(), ev)
	if r.Next == nil || st.feats[id].State.Phase != domain.PhaseDocs {
		t.Errorf("stored = %+v resp = %+v", st.feats[id].State, r)
	}
}

func TestStopFromDocsPhasesEndsStopped(t *testing.T) {
	for _, p := range []domain.Phase{domain.PhaseDocs, domain.PhaseDocumenting} {
		h, st, id := reviewedClean(t, fakeDocs{res: readmeDocs})
		f := st.feats[id]
		f.State.Phase = p
		st.feats[id] = f
		nf, _, err := h.advance(context.Background(), id, flow.Event{Kind: flow.Stop})
		if err != nil || nf.State.Outcome != domain.OutcomeStopped {
			t.Errorf("%s: %+v %v", p, nf.State, err)
		}
	}
}

// existsFiles is a Files port that also knows which paths exist.
type existsFiles struct{ exist map[string]bool }

func (existsFiles) NativePath(p string) string { return p }
func (existsFiles) IsDir(string) bool          { return false }
func (e existsFiles) Exists(p string) bool     { return e.exist[p] }

func TestDocumenterEditsOnlyTheListedDocs(t *testing.T) {
	root := t.TempDir()
	h, st := flowHooks(false)
	h.Files = existsFiles{exist: map[string]bool{normPath(filepath.Join(root, "handbook", "old.md"), ""): true}}
	d := domain.Docs{WikiDir: "handbook", Docs: []domain.DocFile{{Path: "README.md"}, {Path: "docs/guide.md"}}}
	if _, err := st.CreateFeature(context.Background(), domain.Feature{
		Repo: h.Repos.Key(root), ProjectDir: root, Request: "r", PlanRev: 1, Docs: &d,
		State: domain.State{Phase: domain.PhaseDocumenting}, Next: domain.Action{Kind: domain.DispatchDocumenter},
	}); err != nil {
		t.Fatal(err)
	}
	ev := func(file, agent string) HookEvent {
		return HookEvent{Name: "PreToolUse", Tool: "Edit", FilePath: file, AgentID: "d1", AgentType: agent, ProjectDir: root, Cwd: root}
	}
	cases := []struct {
		name  string
		ev    HookEvent
		allow bool
	}{
		{"listed doc", ev(filepath.Join(root, "README.md"), roleDocumenter), true},
		{"listed relative", ev("docs/guide.md", roleDocumenter), true},
		{"code file", ev(filepath.Join(root, "main.go"), roleDocumenter), false},
		{"unlisted doc", ev(filepath.Join(root, "docs", "other.md"), roleDocumenter), false},
		{"new wiki page", ev(filepath.Join(root, "handbook", "new.md"), roleDocumenter), true},
		{"existing wiki page", ev(filepath.Join(root, "handbook", "old.md"), roleDocumenter), false},
		{"wiki non markdown", ev(filepath.Join(root, "handbook", "x.go"), roleDocumenter), false},
		{"escapes", ev(root+"/docs/../main.go", roleDocumenter), false},
	}
	for _, c := range cases {
		r := h.Handle(context.Background(), c.ev)
		if (r.AllowReason != "") != c.allow || (r.Deny != "") == c.allow {
			t.Errorf("%s: allow = %q deny = %q, want allow=%v", c.name, r.AllowReason, r.Deny, c.allow)
		}
	}
	main := ev(filepath.Join(root, "README.md"), "")
	main.AgentID = ""
	if r := h.Handle(context.Background(), main); r.Deny == "" {
		t.Errorf("main thread edit allowed: %+v", r)
	}
	// Outside the repo nothing is decided.
	if r := h.Handle(context.Background(), ev(filepath.Join(t.TempDir(), "x.md"), roleDocumenter)); r.Deny != "" || r.AllowReason != "" {
		t.Errorf("outside the repo: %+v", r)
	}
}

func TestDocumenterShellWritesAreDenied(t *testing.T) {
	root := t.TempDir()
	h, st := flowHooks(false)
	h.Config = fakeConfig{}
	h.Exe = "/opt/nxy/nxy"
	d := domain.Docs{Docs: []domain.DocFile{{Path: "README.md"}}}
	if _, err := st.CreateFeature(context.Background(), domain.Feature{
		Repo: h.Repos.Key(root), ProjectDir: root, Request: "r", PlanRev: 1, Docs: &d,
		State: domain.State{Phase: domain.PhaseDocumenting}, Next: domain.Action{Kind: domain.DispatchDocumenter},
	}); err != nil {
		t.Fatal(err)
	}
	ev := func(cmd string) HookEvent {
		return HookEvent{Name: "PreToolUse", Tool: "Bash", Command: cmd, HasCommand: true, AgentID: "d1", AgentType: roleDocumenter, ProjectDir: root, Cwd: root}
	}
	if r := h.Handle(context.Background(), ev("echo x > README.md")); r.Deny == "" {
		t.Errorf("shell write allowed: %+v", r)
	}
	if r := h.Handle(context.Background(), ev("git status")); r.Deny != "" {
		t.Errorf("read-only command denied: %+v", r)
	}
}

func TestDocsPacketOfAFeature(t *testing.T) {
	h, st, id := reviewedClean(t, fakeDocs{res: readmeDocs})
	h.Diffs = fakeDiffs{{Path: "pkg/greet.go", Status: "M", Text: "diff --git a/pkg/greet.go b/pkg/greet.go\n--- a/pkg/greet.go\n+++ b/pkg/greet.go\n@@ -1 +1,2 @@\n-old\n+new\n+newer\n"}}
	ctx := context.Background()
	text, err := h.DocsPacket(ctx, id)
	if err != nil || text != NoDocsText {
		t.Fatalf("no docs: %q %v", text, err)
	}
	f := st.feats[id]
	f.Docs = &readmeDocs
	st.feats[id] = f
	text, err = h.DocsPacket(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# nxy docs packet · review " + id, "add greet", "- README.md", "README.md:4: call greet()", "- pkg/greet.go (M, +2 \u22121)", "+newer"} {
		if !strings.Contains(text, want) {
			t.Errorf("packet misses %q:\n%s", want, text)
		}
	}
	if _, err := h.DocsPacket(ctx, "nope"); err == nil {
		t.Error("unknown feature accepted")
	}
}

// perDirDocs answers Find by the folder it searches.
type perDirDocs struct {
	by    map[string]domain.Docs
	calls map[string][]string
}

func (d perDirDocs) Find(_ context.Context, dir string, changed []string) domain.Docs {
	d.calls[dir] = changed
	return d.by[dir]
}

func TestDocsOfSeveralReposAreSearchedPerRepoAndLabeled(t *testing.T) {
	h, _ := flowHooks(false)
	pd := perDirDocs{
		by: map[string]domain.Docs{
			"/w/api.wt": {Terms: []string{"a"}, Docs: []domain.DocFile{{Path: "docs/api.md"}}, WikiDir: "wiki"},
			"/w/web.wt": {Terms: []string{"a", "b"}},
		},
		calls: map[string][]string{},
	}
	h.Docs = pd
	h.Changes = dirChanges{map[string][]string{"/w/api.wt": {"src/a.go"}, "/w/web.wt": {"src/b.go"}}}
	f := domain.Feature{
		ProjectDir: "/w", Base: "abc",
		Repos: []domain.FeatureRepo{
			{Name: "api", Root: "/w/api", Checkout: "/w/api.wt", Base: "abc"},
			{Name: "web", Root: "/w/web", Checkout: "/w/web.wt", Base: "abc"},
		},
	}
	got := h.findDocs(context.Background(), f)
	if len(got.Docs) != 1 || got.Docs[0].Path != "api/docs/api.md" || got.WikiDir != "api/wiki" || len(got.Terms) != 2 {
		t.Fatalf("docs = %+v", got)
	}
	if strings.Join(pd.calls["/w/api.wt"], ",") != "src/a.go" || strings.Join(pd.calls["/w/web.wt"], ",") != "src/b.go" {
		t.Errorf("calls %v", pd.calls)
	}
	f.Docs = &got
	if want := normPath("/w/api.wt/docs/api.md", ""); h.docsAbs(f, "api/docs/api.md") != want {
		t.Errorf("docsAbs = %q want %q", h.docsAbs(f, "api/docs/api.md"), want)
	}
	if af := h.docsAbsolute(f); af.Docs.Docs[0].Path != normPath("/w/api.wt/docs/api.md", "") || f.Docs.Docs[0].Path != "api/docs/api.md" {
		t.Errorf("absolute = %+v", af.Docs.Docs)
	}
}

type fakeDiffs []review.FileDiff

func (d fakeDiffs) Diff(context.Context, string, string) ([]review.FileDiff, error) { return d, nil }
