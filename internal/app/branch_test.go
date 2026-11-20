package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/domain"
	"github.com/mananos/nxy.dev/v2/internal/plan"
)

// fakeBranches answers List with info and records Switch and AddWorktree calls.
type fakeBranches struct {
	info     BranchInfo
	switchE  error
	wtErr    error
	switches []string
	added    []string
	removed  []string
}

func (b *fakeBranches) List(context.Context, string) BranchInfo { return b.info }
func (b *fakeBranches) Switch(_ context.Context, dir, branch, base string) (BranchChange, error) {
	b.switches = append(b.switches, dir+" "+branch+" "+base)
	if b.switchE != nil {
		return BranchChange{}, b.switchE
	}
	return BranchChange{Command: "git switch -c " + branch + " " + base, Branch: branch, Created: true}, nil
}
func (b *fakeBranches) AddWorktree(_ context.Context, repo, branch, base string) (BranchChange, error) {
	if b.wtErr != nil {
		return BranchChange{}, b.wtErr
	}
	p := repo + ".worktrees/" + strings.ReplaceAll(branch, "/", "_")
	b.added = append(b.added, p)
	return BranchChange{Path: p, Branch: branch, Created: true}, nil
}
func (b *fakeBranches) RemoveWorktree(_ context.Context, _, path string) error {
	b.removed = append(b.removed, path)
	return nil
}
func (b *fakeBranches) OpenCommand(p string) string { return "code " + p }
func (b *fakeBranches) MissingDeps(string) bool     { return false }

func branchHooks(t *testing.T) (Hooks, *memStore, *fakeBranches, domain.Feature) {
	t.Helper()
	h, st := flowHooks(false)
	fb := &fakeBranches{info: BranchInfo{Branches: []string{"main", "dev", "feature/old"}, Default: "main", Current: "main"}}
	h.Branches = fb
	h.Changes = fakeChanges{head: "sha1"}
	f := approvalFeature(t, h, validPlan())
	return h, st, fb, f
}

func branchAnswers(f domain.Feature, branch, base string) HookEvent {
	ev := askPost(f, plan.BranchQuestion(f.ID), branch)
	if base != "" {
		ev.Answers[plan.BaseQuestion(f.ID, f.BranchOffer.Repos[0].Name)] = base
	}
	return ev
}

func approveToBranch(t *testing.T, h Hooks, st *memStore, f domain.Feature) domain.Feature {
	t.Helper()
	h.Handle(context.Background(), askPost(f, plan.ApprovalQuestion(f.ID, 1), plan.Approve))
	g := st.feats[f.ID]
	if g.State.Phase != domain.PhaseBranch || g.Next.Kind != domain.AskBranch || g.BranchOffer == nil {
		t.Fatalf("after approve: %+v offer=%+v", g.State, g.BranchOffer)
	}
	return g
}

func TestApproveAsksBranch(t *testing.T) {
	h, st, _, f := branchHooks(t)
	g := approveToBranch(t, h, st, f)
	o := g.BranchOffer
	if len(o.Suggest) < 2 || len(o.Repos) != 1 || o.Repos[0].Bases[0] != "main" {
		t.Errorf("offer = %+v", o)
	}
	qs := plan.BranchQuestions(g)
	if len(qs) != 2 || qs[0].Question != plan.BranchQuestion(f.ID) || len(qs[0].Options) < 2 || len(qs[1].Options) != 3 {
		t.Errorf("questions = %+v", qs)
	}
	if !isNxyQuestion(g, qs[1].Question) {
		t.Errorf("%q is not an nxy question", qs[1].Question)
	}
}

func TestBranchAnswerSwitchesInTheSessionFolder(t *testing.T) {
	h, st, fb, f := branchHooks(t)
	g := approveToBranch(t, h, st, f)
	r := h.Handle(context.Background(), branchAnswers(g, "feature/mine", "dev"))
	got := st.feats[f.ID]
	if got.State.Phase != domain.PhaseRunningBatch || got.State.Batch != 1 || r.Next == nil || r.Next.Feature.Next.Kind != domain.DispatchImplementer {
		t.Fatalf("state = %+v response = %+v", got.State, r)
	}
	if len(fb.switches) != 1 || fb.switches[0] != "/p feature/mine dev" || len(fb.added) != 0 {
		t.Errorf("switches = %q added = %q", fb.switches, fb.added)
	}
	rs := got.RepoList()
	if len(rs) != 1 || rs[0].Branch != "feature/mine" || rs[0].Base != "sha1" || got.Base != "sha1" || got.BranchOffer != nil {
		t.Errorf("repos = %+v base = %q", rs, got.Base)
	}
	if got.UsesWorktrees() || len(got.Held()) != 1 || got.Held()[0] != "repo:/p" {
		t.Errorf("held = %v", got.Held())
	}
	if !strings.Contains(r.Next.Note, "feature/mine") || !strings.Contains(r.Next.Note, "dev") {
		t.Errorf("note = %q", r.Next.Note)
	}
}

func TestBranchAlreadyOnItDoesNotSwitch(t *testing.T) {
	h, st, fb, f := branchHooks(t)
	g := approveToBranch(t, h, st, f)
	h.Handle(context.Background(), branchAnswers(g, "main", "main"))
	if st.feats[f.ID].State.Phase != domain.PhaseRunningBatch || len(fb.switches) != 0 {
		t.Errorf("phase = %s switches = %q", st.feats[f.ID].State.Phase, fb.switches)
	}
}

func TestBranchPartialAnswerAsksOnlyTheRest(t *testing.T) {
	h, st, fb, f := branchHooks(t)
	g := approveToBranch(t, h, st, f)
	h.Handle(context.Background(), askPost(g, plan.BranchQuestion(f.ID), "feature/mine"))
	got := st.feats[f.ID]
	if got.State.Phase != domain.PhaseBranch || len(fb.switches) != 0 || got.BranchOffer.Branch != "feature/mine" {
		t.Fatalf("state = %+v offer = %+v", got.State, got.BranchOffer)
	}
	qs := plan.BranchQuestions(got)
	if len(qs) != 1 || qs[0].Question != plan.BaseQuestion(f.ID, got.BranchOffer.Repos[0].Name) {
		t.Errorf("questions = %+v", qs)
	}
	h.Handle(context.Background(), askPost(got, qs[0].Question, "main"))
	if st.feats[f.ID].State.Phase != domain.PhaseRunningBatch || len(fb.switches) != 1 {
		t.Errorf("phase = %s switches = %q", st.feats[f.ID].State.Phase, fb.switches)
	}
}

func TestBranchGitErrorAsksAgain(t *testing.T) {
	h, st, fb, f := branchHooks(t)
	fb.switchE = errors.New("tenés cambios sin commitear que se perderían")
	g := approveToBranch(t, h, st, f)
	h.Handle(context.Background(), branchAnswers(g, "feature/mine", "main"))
	got := st.feats[f.ID]
	if got.State.Phase != domain.PhaseBranch || got.BranchOffer.Error == "" || got.BranchOffer.Branch != "" {
		t.Fatalf("state = %+v offer = %+v", got.State, got.BranchOffer)
	}
	if len(plan.BranchQuestions(got)) != 2 {
		t.Errorf("questions = %+v", plan.BranchQuestions(got))
	}
}

func TestBranchInWorktreeHoldsOnlyTheWorktree(t *testing.T) {
	h, st, fb, f := branchHooks(t)
	g := st.feats[f.ID]
	g.InWorktree = true
	st.feats[f.ID] = g
	g = approveToBranch(t, h, st, g)
	h.Handle(context.Background(), branchAnswers(g, "feature/mine", "main"))
	got := st.feats[f.ID]
	if got.State.Phase != domain.PhaseRunningBatch || len(fb.added) != 1 || len(fb.switches) != 0 {
		t.Fatalf("state = %+v added = %q switches = %q", got.State, fb.added, fb.switches)
	}
	rs := got.RepoList()
	if rs[0].Checkout != fb.added[0] || rs[0].Branch != "feature/mine" {
		t.Errorf("repos = %+v", rs)
	}
	if h := got.Held(); len(h) != 1 || h[0] != "repo:"+fb.added[0] {
		t.Errorf("held = %v", h)
	}
}

func TestBranchWorktreeFailureAsksAgain(t *testing.T) {
	h, st, fb, f := branchHooks(t)
	g := st.feats[f.ID]
	g.InWorktree = true
	st.feats[f.ID] = g
	fb.wtErr = errors.New("la carpeta ya existe")
	g = approveToBranch(t, h, st, g)
	h.Handle(context.Background(), branchAnswers(g, "feature/mine", "main"))
	got := st.feats[f.ID]
	if got.State.Phase != domain.PhaseBranch || !strings.Contains(got.BranchOffer.Error, "ya existe") {
		t.Errorf("state = %+v offer = %+v", got.State, got.BranchOffer)
	}
}
