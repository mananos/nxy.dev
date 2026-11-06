package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
)

type discardViewer struct{ discarded []string }

func (*discardViewer) Show(context.Context, domain.Feature) (string, error) { return "", nil }
func (v *discardViewer) Discard(id string) error {
	v.discarded = append(v.discarded, id)
	return nil
}

func seed(t *testing.T, h Hooks, st *memStore, dir string, state domain.State, next domain.Action) string {
	t.Helper()
	id, err := st.CreateFeature(context.Background(), domain.Feature{
		Repo: h.Repos.Key(dir), ProjectDir: dir, Request: "first line\nsecond", State: state, Next: next, PlanRev: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

var (
	approvalState = domain.State{Phase: domain.PhaseApproval}
	approvalNext  = domain.Action{Kind: domain.AskApproval}
	runningState  = domain.State{Phase: domain.PhaseRunningBatch, Batch: 1, Batches: 2}
	runningNext   = domain.Action{Kind: domain.DispatchImplementer, Batch: 1}
)

func TestPreToolEdit(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	edit := func(file, agent string) HookEvent {
		return HookEvent{Name: "PreToolUse", Tool: "Write", FilePath: file, AgentID: agent, ProjectDir: root, Cwd: root}
	}
	cases := []struct {
		name string
		ev   HookEvent
		deny bool
	}{
		{"inside", edit(filepath.Join(root, "a", "b.go"), ""), true},
		{"outside", edit(filepath.Join(outside, "x.md"), ""), false},
		{"relative inside", edit("a/b.go", ""), true},
		{"relative escaping", edit("../elsewhere/x", ""), false},
		{"subagent", edit(filepath.Join(root, "a.go"), "agent-1"), false},
		{"sibling prefix", edit(root+"-other/a.go", ""), false},
	}
	for _, c := range cases {
		h, st := flowHooks(false)
		seed(t, h, st, root, approvalState, approvalNext)
		r := h.Handle(context.Background(), c.ev)
		if (r.Deny != "") != c.deny {
			t.Errorf("%s: deny = %q, want deny=%v", c.name, r.Deny, c.deny)
		}
		if c.deny && (!strings.Contains(r.Deny, "`nxy feature stop`") || !strings.Contains(r.Deny, "Aprobar plan")) {
			t.Errorf("%s: reason = %q", c.name, r.Deny)
		}
	}

	t.Run("no feature", func(t *testing.T) {
		h, _ := flowHooks(false)
		if r := h.Handle(context.Background(), edit(filepath.Join(root, "a.go"), "")); r.Deny != "" {
			t.Errorf("deny = %q", r.Deny)
		}
	})
	t.Run("stopped feature", func(t *testing.T) {
		h, st := flowHooks(false)
		seed(t, h, st, root, domain.State{Phase: domain.PhaseDone, Outcome: domain.OutcomeStopped}, domain.Action{})
		if r := h.Handle(context.Background(), edit(filepath.Join(root, "a.go"), "")); r.Deny != "" {
			t.Errorf("deny = %q", r.Deny)
		}
	})
}

func TestPreToolEditWindowsPaths(t *testing.T) {
	old := goos
	goos = "windows"
	defer func() { goos = old }()
	h, st := flowHooks(false)
	root := `C:\Repo\Proj`
	seed(t, h, st, root, approvalState, approvalNext)
	for _, p := range []string{`c:\repo\proj\A.go`, `C:/Repo/Proj/sub/a.go`} {
		ev := HookEvent{Name: "PreToolUse", Tool: "Edit", FilePath: p, ProjectDir: root, Cwd: root}
		if r := h.Handle(context.Background(), ev); r.Deny == "" {
			t.Errorf("%s: not denied", p)
		}
	}
	ev := HookEvent{Name: "PreToolUse", Tool: "Edit", FilePath: `C:\Users\me\.claude\x.md`, ProjectDir: root, Cwd: root}
	if r := h.Handle(context.Background(), ev); r.Deny != "" {
		t.Errorf("outside denied: %q", r.Deny)
	}
}

func TestPreToolEditImplementerAllow(t *testing.T) {
	root := t.TempDir()
	plan := &domain.Plan{Batches: []domain.Batch{{N: 1, Change: []domain.Change{{At: "calc.go:5", What: "x"}, {At: "sub/b.go", What: "y"}}}}}
	setup := func(state domain.State) (Hooks, *memStore) {
		h, st := flowHooks(false)
		if _, err := st.CreateFeature(context.Background(), domain.Feature{
			Repo: h.Repos.Key(root), ProjectDir: root, Request: "r", State: state, Next: runningNext, PlanRev: 1, Plan: plan,
		}); err != nil {
			t.Fatal(err)
		}
		return h, st
	}
	ev := func(file, agentType string) HookEvent {
		id := "agent-1"
		if agentType == "" {
			id = ""
		}
		return HookEvent{Name: "PreToolUse", Tool: "Edit", FilePath: file, AgentID: id, AgentType: agentType, ProjectDir: root, Cwd: root}
	}
	edits := func(h Hooks, st *memStore) int {
		f, _, _ := st.ActiveFeature(context.Background(), h.Repos.Key(root))
		if f.Proof == nil {
			return 0
		}
		return f.Proof.Edits
	}
	cases := []struct {
		name  string
		state domain.State
		ev    HookEvent
		allow bool
		edits int
	}{
		{"listed", runningState, ev(filepath.Join(root, "calc.go"), roleImplementer), true, 1},
		{"listed relative", runningState, ev("sub/b.go", roleImplementer), true, 1},
		{"unlisted", runningState, ev(filepath.Join(root, "other.go"), roleImplementer), false, 1},
		{"escapes", runningState, ev(root+"/sub/../../calc.go", roleImplementer), false, 0},
		{"other subagent", runningState, ev(filepath.Join(root, "calc.go"), "other:agent"), false, 0},
		{"other phase", domain.State{Phase: domain.PhaseApproval}, ev(filepath.Join(root, "calc.go"), roleImplementer), false, 0},
	}
	for _, c := range cases {
		h, st := setup(c.state)
		r := h.Handle(context.Background(), c.ev)
		if (r.AllowReason != "") != c.allow || r.Deny != "" {
			t.Errorf("%s: allow = %q deny = %q, want allow=%v", c.name, r.AllowReason, r.Deny, c.allow)
		}
		if got := edits(h, st); got != c.edits {
			t.Errorf("%s: edits = %d, want %d", c.name, got, c.edits)
		}
	}
	h, _ := setup(runningState)
	if r := h.Handle(context.Background(), ev(filepath.Join(root, "calc.go"), "")); r.Deny == "" || r.AllowReason != "" {
		t.Errorf("main thread: %+v", r)
	}

	old := goos
	goos = "windows"
	defer func() { goos = old }()
	wroot := `C:\Repo\Proj`
	h, st := flowHooks(false)
	if _, err := st.CreateFeature(context.Background(), domain.Feature{
		Repo: h.Repos.Key(wroot), ProjectDir: wroot, Request: "r", State: runningState, Next: runningNext, PlanRev: 1, Plan: plan,
	}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{`c:\repo\proj\CALC.go`, `C:/Repo/Proj/calc.go`, `C:\Repo\Proj\sub\b.go`} {
		e := HookEvent{Name: "PreToolUse", Tool: "Edit", FilePath: p, AgentID: "a", AgentType: roleImplementer, ProjectDir: wroot, Cwd: wroot}
		if r := h.Handle(context.Background(), e); r.AllowReason == "" {
			t.Errorf("%s: not allowed", p)
		}
	}
}

func TestNormPathUNC(t *testing.T) {
	old := goos
	goos = "windows"
	defer func() { goos = old }()
	root := normPath(`\\srv\Share\Proj`, "")
	if !within(root, normPath(`\\SRV\share\proj\sub\a.go`, "")) {
		t.Error("UNC file inside root not matched")
	}
	if within(root, normPath(`\\srv\share\other\a.go`, "")) {
		t.Error("UNC file outside root matched")
	}
	if got := normPath(`C:\Repo\..\Repo\a.go`, ""); got != "c:/repo/a.go" {
		t.Errorf("got %q", got)
	}
}

func TestStatus(t *testing.T) {
	dir := t.TempDir()
	h, st := flowHooks(false)
	if v, err := h.Status(context.Background(), dir); err != nil || v.FeatureID != "" {
		t.Fatalf("empty: %+v %v", v, err)
	}
	id := seed(t, h, st, dir, approvalState, approvalNext)
	v, err := h.Status(context.Background(), dir)
	if err != nil || v.FeatureID != id || v.Request != "first line" || v.Next != "Aprobar plan" || v.PlanRev != 1 {
		t.Errorf("approval: %+v %v", v, err)
	}
	if v.Public == "" || v.Phase == "" {
		t.Errorf("approval labels: %+v", v)
	}

	h, st = flowHooks(false)
	seed(t, h, st, dir, runningState, runningNext)
	if v, _ := h.Status(context.Background(), dir); v.Next != "Lote 1" {
		t.Errorf("running: %+v", v)
	}
}

func TestNoMainThreadStepIsNeverEmpty(t *testing.T) {
	dir := t.TempDir()
	h, st := flowHooks(false)
	seed(t, h, st, dir, domain.State{Phase: domain.PhaseReviewing}, domain.Action{})
	r := h.Handle(context.Background(), HookEvent{Name: "PreToolUse", Tool: "Write", FilePath: filepath.Join(dir, "a.go"), ProjectDir: dir, Cwd: dir})
	if r.Deny == "" || strings.Contains(r.Deny, "next step .") || !strings.Contains(r.Deny, "`nxy feature stop`") {
		t.Errorf("deny = %q", r.Deny)
	}
	v, _ := h.Status(context.Background(), dir)
	if v.Next == "" || !strings.Contains(v.Next, "Revisando") {
		t.Errorf("status next = %q", v.Next)
	}

	h, st = flowHooks(false)
	seed(t, h, st, dir, domain.State{Phase: domain.PhaseVerifyingBatch}, domain.Action{})
	if v, _ := h.Status(context.Background(), dir); !strings.Contains(v.Next, "Verificando lote") {
		t.Errorf("verifying status next = %q", v.Next)
	}
}

func TestStopFeature(t *testing.T) {
	dir := t.TempDir()
	for name, c := range map[string]struct {
		s domain.State
		n domain.Action
	}{"approval": {approvalState, approvalNext}, "running": {runningState, runningNext}} {
		h, st := flowHooks(false)
		v := &discardViewer{}
		h.Viewer = v
		id := seed(t, h, st, dir, c.s, c.n)
		f, _, err := h.StopFeature(context.Background(), dir)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if f.ID != id || f.State.Phase != domain.PhaseDone || f.State.Outcome != domain.OutcomeStopped {
			t.Errorf("%s: feature = %+v", name, f)
		}
		if _, ok, _ := st.ActiveFeature(context.Background(), h.Repos.Key(dir)); ok {
			t.Errorf("%s: still active", name)
		}
		if len(v.discarded) != 1 || v.discarded[0] != id {
			t.Errorf("%s: discarded = %v", name, v.discarded)
		}
		if _, _, err := h.StopFeature(context.Background(), dir); !errors.Is(err, ErrNoActiveFeature) {
			t.Errorf("%s: second stop err = %v", name, err)
		}
	}
	h, _ := flowHooks(false)
	if _, _, err := h.StopFeature(context.Background(), dir); !errors.Is(err, ErrNoActiveFeature) {
		t.Errorf("no feature: %v", err)
	}
}

type failViewer struct{ discardViewer }

func (*failViewer) Discard(string) error { return errors.New("locked") }

func TestStopFeatureDiscardFailureIsWarning(t *testing.T) {
	dir := t.TempDir()
	h, st := flowHooks(false)
	h.Viewer = &failViewer{}
	seed(t, h, st, dir, approvalState, approvalNext)
	f, warn, err := h.StopFeature(context.Background(), dir)
	if err != nil || warn == nil {
		t.Fatalf("err = %v, warning = %v", err, warn)
	}
	if f.State.Outcome != domain.OutcomeStopped {
		t.Errorf("feature = %+v", f)
	}
	if _, ok, _ := st.ActiveFeature(context.Background(), h.Repos.Key(dir)); ok {
		t.Error("still active")
	}
}

func multiRepoFeature(t *testing.T, root, wtAPI, wtWeb string) (Hooks, *memStore, string) {
	t.Helper()
	h, st := flowHooks(false)
	p := &domain.Plan{
		Repos: []domain.PlanRepo{{Path: "api", Name: "api"}, {Path: "web", Name: "web"}},
		Batches: []domain.Batch{{N: 1, Repo: "api", Change: []domain.Change{{At: "api/src/a.go:3", What: "x"}}},
			{N: 2, Repo: "web", Change: []domain.Change{{At: "web/b.go", What: "y"}}}},
	}
	id, err := st.CreateFeature(context.Background(), domain.Feature{
		Repo: "repo:" + root, Home: "repo:" + root, Holds: []string{"repo:" + wtAPI, "repo:" + wtWeb}, ProjectDir: root,
		State: runningState, Next: runningNext, Plan: p, InWorktree: true,
		Repos: []domain.FeatureRepo{
			{Name: "api", Root: filepath.Join(root, "api"), Checkout: wtAPI},
			{Name: "web", Root: filepath.Join(root, "web"), Checkout: wtWeb},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return h, st, id
}

func TestPreToolEditMultiRepoWorktrees(t *testing.T) {
	root, wtAPI, wtWeb := t.TempDir(), t.TempDir(), t.TempDir()
	h, _, _ := multiRepoFeature(t, root, wtAPI, wtWeb)
	ev := func(file string) HookEvent {
		return HookEvent{Name: "PreToolUse", Tool: "Edit", FilePath: file, AgentID: "a", AgentType: roleImplementer, ProjectDir: wtAPI, Cwd: wtAPI}
	}
	if r := h.Handle(context.Background(), ev(filepath.Join(wtAPI, "src", "a.go"))); r.AllowReason == "" {
		t.Errorf("worktree file named by batch 1: %+v", r)
	}
	if r := h.Handle(context.Background(), ev(filepath.Join(wtWeb, "src", "a.go"))); r.AllowReason != "" || r.Deny != "" {
		t.Errorf("file of another repo's worktree is not named by batch 1: %+v", r)
	}
	r := h.Handle(context.Background(), ev(filepath.Join(root, "api", "src", "a.go")))
	if r.Deny == "" || !strings.Contains(r.Deny, wtAPI) {
		t.Errorf("original checkout: %+v", r)
	}
}

func TestUndeclaredRepoEditAddsRepoOnce(t *testing.T) {
	root, wtAPI, wtWeb := t.TempDir(), t.TempDir(), t.TempDir()
	h, st, id := multiRepoFeature(t, root, wtAPI, wtWeb)
	other := t.TempDir()
	ev := HookEvent{Name: "PreToolUse", Tool: "Edit", FilePath: filepath.Join(other, "x.go"), AgentID: "a", AgentType: roleImplementer, ProjectDir: root, Cwd: root}
	r := h.Handle(context.Background(), ev)
	if !strings.Contains(r.AllowReason, "outside the plan") {
		t.Fatalf("first edit: %+v", r)
	}
	f := st.feats[id]
	if len(f.Repos) != 3 || !f.Repos[2].Undeclared || f.Repos[2].Checkout != other {
		t.Fatalf("repos = %+v", f.Repos)
	}
	if r := h.Handle(context.Background(), ev); r.AllowReason != "" {
		t.Errorf("second edit warns again: %+v", r)
	}
	if len(st.feats[id].Repos) != 3 {
		t.Errorf("repo added twice")
	}
}

func TestUndeclaredRepoDoesNotMakeWorktrees(t *testing.T) {
	root := t.TempDir()
	h, st := flowHooks(false)
	id, err := st.CreateFeature(context.Background(), domain.Feature{
		Repo: "repo:" + root, Home: "repo:" + root, Holds: []string{"repo:" + root}, ProjectDir: root,
		State: runningState, Next: runningNext,
		Plan: &domain.Plan{Batches: []domain.Batch{{N: 1, Change: []domain.Change{{At: "a.go", What: "x"}}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	ev := HookEvent{Name: "PreToolUse", Tool: "Edit", FilePath: filepath.Join(other, "x.go"), AgentID: "a", AgentType: roleImplementer, ProjectDir: root, Cwd: root}
	if r := h.Handle(context.Background(), ev); !strings.Contains(r.AllowReason, "outside the plan") {
		t.Fatalf("edit: %+v", r)
	}
	if st.feats[id].UsesWorktrees() {
		t.Errorf("an undeclared repo flipped UsesWorktrees: %+v", st.feats[id].Repos)
	}
}
