package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/affected"
	"github.com/mananos/nxy.dev/v2/internal/domain"
	"github.com/mananos/nxy.dev/v2/internal/plan"
)

type fakeChanges struct {
	head    string
	changed []string
	files   []string
	err     error
}

func (c fakeChanges) Head(string) string { return c.head }
func (c fakeChanges) Changed(context.Context, string, string) ([]string, error) {
	return c.changed, c.err
}
func (c fakeChanges) Files(context.Context, string) ([]string, error) { return c.files, nil }

type fakeGraph struct{ res GraphResult }

func (g fakeGraph) Affected(context.Context, string, []string) GraphResult { return g.res }

type fakeRunners struct {
	rows []affected.Runner
	warn string
}

func (r fakeRunners) Rows(string) ([]affected.Runner, string) { return r.rows, r.warn }

type markerTree map[string]bool

func (m markerTree) Read(p string) ([]byte, bool) { return nil, m[p] }

type fakeTrees struct{ t markerTree }

func (f fakeTrees) Tree(string) affected.Tree { return f.t }

// cmdShell records every command and answers with exit codes by substring.
type cmdShell struct {
	ran  []string
	exit map[string]int
}

func (s *cmdShell) Run(_ context.Context, _, command string) (ShellResult, error) {
	s.ran = append(s.ran, command)
	for k, e := range s.exit {
		if strings.Contains(command, k) {
			return ShellResult{Exit: e, Output: "out of " + command}, nil
		}
	}
	return ShellResult{}, nil
}

// affectedHooks stores a feature at PhaseAffected with a Go repo layout.
func affectedHooks(t *testing.T, suite string, g GraphResult) (Hooks, *memStore, *cmdShell, string) {
	t.Helper()
	h, st := flowHooks(false)
	sh := &cmdShell{exit: map[string]int{}}
	h.Shell = sh
	h.Changes = fakeChanges{head: "abc", changed: []string{"pkg/a.go"}, files: []string{"pkg/a.go", "pkg/a_test.go"}}
	h.Graph = fakeGraph{g}
	h.Trees = fakeTrees{markerTree{"go.mod": true}}
	p := validPlan()
	p.Suite = suite
	id, err := st.CreateFeature(context.Background(), domain.Feature{
		Repo: "repo:/p", ProjectDir: "/p", Plan: &p, Base: "abc",
		State: domain.State{Phase: domain.PhaseAffected, Batch: 1, Batches: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	return h, st, sh, id
}

func TestAffectedCodegraphGreen(t *testing.T) {
	h, st, sh, id := affectedHooks(t, "", GraphResult{Status: GraphOK, Tests: []string{"pkg/a_test.go"}})
	r, err := h.RunAffected(context.Background(), id)
	if err != nil || r.Verdict != AffectedGreen || !r.Recorded {
		t.Fatalf("r = %+v err = %v", r, err)
	}
	f := st.feats[id]
	if f.State.Phase != domain.PhaseReviewing || f.Affected == nil || f.Affected.Source != "codegraph" {
		t.Errorf("feature = %+v", f)
	}
	if len(sh.ran) != 1 || sh.ran[0] != "go test ./pkg/" {
		t.Errorf("ran %q", sh.ran)
	}
}

func TestAffectedMissingGraphFallsBackToNames(t *testing.T) {
	h, _, sh, id := affectedHooks(t, "", GraphResult{Status: GraphMissing})
	r, err := h.RunAffected(context.Background(), id)
	if err != nil || r.Verdict != AffectedGreen || r.Record.Source != "nombres" {
		t.Fatalf("r = %+v err = %v", r, err)
	}
	if !strings.Contains(r.Record.Note, "/nxy:setup") || len(sh.ran) != 1 {
		t.Errorf("note = %q ran = %q", r.Record.Note, sh.ran)
	}
}

func TestAffectedInitTimeoutNote(t *testing.T) {
	h, _, _, id := affectedHooks(t, "", GraphResult{Status: GraphInitTimeout})
	r, _ := h.RunAffected(context.Background(), id)
	if !strings.Contains(r.Record.Note, "90 s") || r.Record.Source != "nombres" {
		t.Errorf("record = %+v", r.Record)
	}
}

func TestAffectedInitializedNote(t *testing.T) {
	h, _, _, id := affectedHooks(t, "", GraphResult{Status: GraphInitialized, Took: 12 * time.Second, Tests: []string{"pkg/a_test.go"}})
	r, _ := h.RunAffected(context.Background(), id)
	if r.Record.Source != "codegraph" || !strings.Contains(r.Record.Note, "creado en 12 s") {
		t.Errorf("record = %+v", r.Record)
	}
}

func TestAffectedNoRunnerRunsSuite(t *testing.T) {
	h, _, sh, id := affectedHooks(t, "make test", GraphResult{Status: GraphOK, Tests: []string{"zzz/x_test.py"}})
	r, _ := h.RunAffected(context.Background(), id)
	if r.Verdict != AffectedGreen || r.Record.Source != "suite" || len(sh.ran) != 1 || sh.ran[0] != "make test" {
		t.Errorf("r = %+v ran = %q", r, sh.ran)
	}
}

func TestAffectedNothingIsUnverified(t *testing.T) {
	h, st, sh, id := affectedHooks(t, "", GraphResult{Status: GraphOK})
	h.Changes = fakeChanges{head: "abc", changed: []string{"pkg/zzz.go"}, files: []string{"pkg/zzz.go", "pkg/a_test.go"}}
	r, _ := h.RunAffected(context.Background(), id)
	f := st.feats[id]
	if r.Verdict != AffectedUnverified || f.State.Phase != domain.PhaseAffectedUnverified || len(sh.ran) != 0 {
		t.Fatalf("r = %+v phase = %s", r, f.State.Phase)
	}
	if !strings.Contains(f.Affected.Reason, "no tiene Suite") || r.Reason != f.Affected.Reason {
		t.Errorf("reason = %q", f.Affected.Reason)
	}
	// the reason is shown once: it is not repeated in the note
	if strings.Contains(f.Affected.Note, "no tiene Suite") || strings.Contains(r.Record.Note, r.Reason) {
		t.Errorf("reason repeated in note %q", f.Affected.Note)
	}
}

func TestAffectedGraphZeroTestsTriesNames(t *testing.T) {
	h, _, sh, id := affectedHooks(t, "", GraphResult{Status: GraphOK})
	r, err := h.RunAffected(context.Background(), id)
	if err != nil || r.Verdict != AffectedGreen || r.Record.Source != "nombres" {
		t.Fatalf("r = %+v err = %v", r, err)
	}
	if !strings.Contains(r.Record.Note, "codegraph no encontró tests afectados; se buscaron por nombre") || len(sh.ran) != 1 {
		t.Errorf("note = %q ran = %q", r.Record.Note, sh.ran)
	}
}

func repoRow() affected.Runner {
	return affected.Runner{
		Name: "mine", Repo: true, Detect: []affected.Detect{{File: "go.mod"}}, Tests: []string{"*_test.go"},
		Command: "mytest {tests}", Item: "dir",
	}
}

func TestAffectedNamesTheRepoRow(t *testing.T) {
	h, st, sh, id := affectedHooks(t, "", GraphResult{Status: GraphOK, Tests: []string{"pkg/a_test.go"}})
	h.Runners = fakeRunners{rows: affected.Merge([]affected.Runner{repoRow()}, affected.Builtin())}
	approveRepoRow(t, st, id, "mine", "mytest {tests}")
	r, _ := h.RunAffected(context.Background(), id)
	if len(sh.ran) != 1 || !strings.HasPrefix(sh.ran[0], "mytest") {
		t.Fatalf("ran %q", sh.ran)
	}
	if run := r.Record.Runs[0]; run.Row != "mine" || !run.Repo {
		t.Errorf("run = %+v", run)
	}
}

func approveRepoRow(t *testing.T, st *memStore, id, name, command string) {
	t.Helper()
	f := st.feats[id]
	f.Plan.RepoRunners = []domain.RepoRunner{{Name: name, Command: command}}
	st.feats[id] = f
}

func TestAffectedIgnoresRepoRowsNotInTheApprovedPlan(t *testing.T) {
	for name, approved := range map[string][2]string{
		"not approved":    {"", ""},
		"command changed": {"mine", "other {tests}"},
	} {
		t.Run(name, func(t *testing.T) {
			h, st, sh, id := affectedHooks(t, "", GraphResult{Status: GraphOK, Tests: []string{"pkg/a_test.go"}})
			h.Runners = fakeRunners{rows: affected.Merge([]affected.Runner{repoRow()}, affected.Builtin())}
			if approved[0] != "" {
				approveRepoRow(t, st, id, approved[0], approved[1])
			}
			r, _ := h.RunAffected(context.Background(), id)
			if len(sh.ran) != 1 || sh.ran[0] != "go test ./pkg/" {
				t.Fatalf("ran %q", sh.ran)
			}
			if !strings.Contains(r.Record.Note, "filas de .nxy/runners.json que no estaban en el plan aprobado, no se usaron: mine") {
				t.Errorf("note = %q", r.Record.Note)
			}
		})
	}
}

func TestSavePlanStoresRepoRunnersForTheCheckpoint(t *testing.T) {
	h, st := flowHooks(false)
	h.Runners = fakeRunners{rows: affected.Merge([]affected.Runner{repoRow()}, affected.Builtin())}
	f := approvalFeature(t, h, validPlan())
	p := st.feats[f.ID].Plan
	if len(p.RepoRunners) != 1 || p.RepoRunners[0].Name != "mine" || p.RepoRunners[0].Command != "mytest {tests}" {
		t.Fatalf("runners = %+v", p.RepoRunners)
	}
	if cp := plan.Checkpoint(*p); !strings.Contains(cp, "- Runners del repo (.nxy/runners.json): `mine` → `mytest {tests}`") {
		t.Errorf("checkpoint = %q", cp)
	}
}

func TestSavePlanShowsInvalidRunnersWarning(t *testing.T) {
	h, st := flowHooks(false)
	h.Runners = fakeRunners{rows: affected.Builtin(), warn: ".nxy/runners.json inválido, se ignora: x"}
	f := approvalFeature(t, h, validPlan())
	if cp := plan.Checkpoint(*st.feats[f.ID].Plan); !strings.Contains(cp, ".nxy/runners.json inválido") {
		t.Errorf("checkpoint = %q", cp)
	}
}

func TestAffectedUnmatchedWithoutSuiteRunsTheRest(t *testing.T) {
	h, _, sh, id := affectedHooks(t, "", GraphResult{Status: GraphOK, Tests: []string{"pkg/a_test.go", "zzz/x_test.py"}})
	r, _ := h.RunAffected(context.Background(), id)
	if r.Verdict != AffectedGreen || len(sh.ran) != 1 || !strings.Contains(r.Record.Note, "zzz/x_test.py") {
		t.Errorf("r = %+v ran = %q", r, sh.ran)
	}
}

func TestAffectedRedStoresFailureAndFixes(t *testing.T) {
	h, st, sh, id := affectedHooks(t, "", GraphResult{Status: GraphOK, Tests: []string{"pkg/a_test.go"}})
	sh.exit["go test"] = 1
	r, _ := h.RunAffected(context.Background(), id)
	f := st.feats[id]
	if r.Verdict != AffectedRed || f.State.Phase != domain.PhaseAffectedFix || f.Next.Kind != domain.DispatchAffectedFix {
		t.Fatalf("r = %+v feature = %+v", r, f)
	}
	if !strings.Contains(f.Affected.Failure, "`go test ./pkg/` (exit 1)") || !strings.Contains(f.Affected.Failure, "out of go test") {
		t.Errorf("failure = %q", f.Affected.Failure)
	}
}

type outShell struct{ out string }

func (s outShell) Run(context.Context, string, string) (ShellResult, error) {
	return ShellResult{Exit: 1, Output: s.out}, nil
}

func TestAffectedRedTailKeepsFailingPackages(t *testing.T) {
	h, st, _, id := affectedHooks(t, "", GraphResult{Status: GraphOK, Tests: []string{"pkg/a_test.go"}})
	h.Shell = outShell{out: longGoFailOutput()}
	if _, err := h.RunAffected(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	f := st.feats[id]
	if !strings.Contains(f.Affected.Failure, "FAIL\tgithub.com/x/cmd/nxy") || !strings.Contains(f.Affected.Failure, "FAIL\tgithub.com/x/internal/app") {
		t.Errorf("failure = %q", f.Affected.Failure)
	}
	if len(f.Affected.Runs) == 0 || !strings.Contains(f.Affected.Runs[0].Tail, "FAIL\tgithub.com/x/cmd/nxy") {
		t.Errorf("runs = %+v", f.Affected.Runs)
	}
}

func TestAffectedCheckModeRecordsNothing(t *testing.T) {
	h, st, sh, id := affectedHooks(t, "", GraphResult{Status: GraphOK, Tests: []string{"pkg/a_test.go"}})
	f := st.feats[id]
	f.State.Phase = domain.PhaseAffectedFix
	st.feats[id] = f
	sh.exit["go test"] = 1
	r, err := h.RunAffected(context.Background(), id)
	if err != nil || !r.Check || r.Verdict != AffectedRed || r.Recorded {
		t.Fatalf("r = %+v err = %v", r, err)
	}
	if g := st.feats[id]; g.State.Phase != domain.PhaseAffectedFix || g.Affected != nil {
		t.Errorf("feature changed: %+v", g)
	}
}

func TestAffectedWrongPhaseErrors(t *testing.T) {
	h, st, _, id := affectedHooks(t, "", GraphResult{Status: GraphOK})
	f := st.feats[id]
	f.State.Phase = domain.PhaseRunningBatch
	st.feats[id] = f
	if _, err := h.RunAffected(context.Background(), id); err == nil {
		t.Error("no error")
	}
}

func TestAffectedGitFailureUsesPlanPaths(t *testing.T) {
	h, _, sh, id := affectedHooks(t, "", GraphResult{Status: GraphMissing})
	h.Changes = fakeChanges{err: errors.New("no git")}
	r, _ := h.RunAffected(context.Background(), id)
	// the plan changes cmd/main.go: no test of it is found by name
	if r.Verdict != AffectedUnverified || len(sh.ran) != 0 {
		t.Errorf("r = %+v ran = %q", r, sh.ran)
	}
}

func TestApproveSetsBaseOnce(t *testing.T) {
	h, st := flowHooks(false)
	h.Changes = fakeChanges{head: "sha1"}
	f := approvalFeature(t, h, validPlan())
	h.Handle(context.Background(), askPost(f, plan.ApprovalQuestion(f.ID, 1), plan.Approve))
	if got := st.feats[f.ID].Base; got != "sha1" {
		t.Fatalf("base = %q", got)
	}
	g := st.feats[f.ID]
	g.State = domain.State{Phase: domain.PhaseApproval, Batches: 1}
	st.feats[f.ID] = g
	h.Changes = fakeChanges{head: "sha2"}
	h.Handle(context.Background(), askPost(f, plan.ApprovalQuestion(f.ID, 1), plan.Approve))
	if got := st.feats[f.ID].Base; got != "sha1" {
		t.Errorf("base after re-approval = %q", got)
	}
}

func TestAffectedDecisionAnswers(t *testing.T) {
	for _, c := range []struct {
		phase  domain.Phase
		q      func(string) string
		answer string
		want   domain.Phase
	}{
		{domain.PhaseAffectedFailed, plan.AffectedFailedQuestion, plan.Retry, domain.PhaseAffectedFix},
		{domain.PhaseAffectedFailed, plan.AffectedFailedQuestion, plan.Continue, domain.PhaseReviewing},
		{domain.PhaseAffectedUnverified, plan.UnverifiedQuestion, plan.Retry, domain.PhaseAffected},
		{domain.PhaseAffectedUnverified, plan.UnverifiedQuestion, plan.Continue, domain.PhaseReviewing},
		{domain.PhaseAffectedUnverified, plan.UnverifiedQuestion, plan.Stop, domain.PhaseDone},
	} {
		h, st, _, id := affectedHooks(t, "", GraphResult{})
		f := st.feats[id]
		f.State.Phase = c.phase
		st.feats[id] = f
		h.Handle(context.Background(), askPost(f, c.q(id), c.answer))
		if got := st.feats[id].State.Phase; got != c.want {
			t.Errorf("%s/%s: phase = %s, want %s", c.phase, c.answer, got, c.want)
		}
	}
}

func TestAffectedFixPromptInjectedAndReturnMovesOn(t *testing.T) {
	h, st, _, id := affectedHooks(t, "", GraphResult{})
	h.Exe = "/opt/nxy/nxy"
	f := st.feats[id]
	f.State.Phase = domain.PhaseAffectedFix
	f.Next = domain.Action{Kind: domain.DispatchAffectedFix}
	f.Affected = &domain.AffectedRun{Failure: "`go test ./pkg/` (exit 1):\nBOOM"}
	st.feats[id] = f
	r := h.Handle(context.Background(), agentEv("PreToolUse", roleImplementer))
	if r.Deny != "" || !strings.Contains(r.Prompt, "BOOM") {
		t.Fatalf("pre = %+v", r)
	}
	r = h.Handle(context.Background(), agentEv("PostToolUse", roleImplementer))
	if g := st.feats[id]; g.State.Phase != domain.PhaseAffected || g.Next.Kind != domain.RunAffected {
		t.Errorf("after return: %+v (%+v)", g.State, r)
	}
}

// dirChanges answers Changed per checkout.
type dirChanges struct {
	by map[string][]string
}

func (c dirChanges) Head(string) string { return "abc" }
func (c dirChanges) Changed(_ context.Context, dir, _ string) ([]string, error) {
	return c.by[dir], nil
}
func (c dirChanges) Files(_ context.Context, dir string) ([]string, error) { return c.by[dir], nil }

// dirShell records the folder each command ran in.
type dirShell struct {
	cwds, ran []string
	exit      map[string]int
}

func (s *dirShell) Run(_ context.Context, cwd, command string) (ShellResult, error) {
	s.cwds, s.ran = append(s.cwds, cwd), append(s.ran, command)
	return ShellResult{Exit: s.exit[command], Output: "out"}, nil
}

func multiRepoAffected(t *testing.T, by map[string][]string, exit map[string]int) (Hooks, *dirShell, string) {
	t.Helper()
	h, st := flowHooks(false)
	sh := &dirShell{exit: exit}
	h.Shell = sh
	h.Changes = dirChanges{by}
	h.Graph = fakeGraph{GraphResult{Status: GraphMissing}}
	h.Trees = fakeTrees{markerTree{}}
	p := domain.Plan{
		Repos: []domain.PlanRepo{{Path: "api", Name: "api", Suite: "api-suite"}, {Path: "web", Name: "web", Suite: "web-suite"}},
		Batches: []domain.Batch{
			{N: 1, Repo: "api", Change: []domain.Change{{At: "api/src/a.go:1"}}},
			{N: 2, Repo: "web", Change: []domain.Change{{At: "web/src/b.go:1"}}},
		},
	}
	id, err := st.CreateFeature(context.Background(), domain.Feature{
		Repo: "repo:/w", ProjectDir: "/w", Plan: &p, Base: "abc",
		Repos: []domain.FeatureRepo{
			{Name: "api", Root: "/w/api", Checkout: "/w/api.worktrees/x", Base: "abc"},
			{Name: "web", Root: "/w/web", Checkout: "/w/web.worktrees/x", Base: "abc"},
		},
		State: domain.State{Phase: domain.PhaseAffected, Batch: 2, Batches: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	return h, sh, id
}

func TestAffectedRunsEachRepoSuiteInItsCheckout(t *testing.T) {
	h, sh, id := multiRepoAffected(t, map[string][]string{
		"/w/api.worktrees/x": {"src/a.go"}, "/w/web.worktrees/x": {"src/b.go"},
	}, nil)
	r, err := h.RunAffected(context.Background(), id)
	if err != nil || r.Verdict != AffectedGreen {
		t.Fatalf("r = %+v err = %v", r, err)
	}
	if len(sh.ran) != 2 || sh.ran[0] != "api-suite" || sh.ran[1] != "web-suite" {
		t.Fatalf("ran %q", sh.ran)
	}
	if filepath.ToSlash(sh.cwds[0]) != "/w/api.worktrees/x" || filepath.ToSlash(sh.cwds[1]) != "/w/web.worktrees/x" {
		t.Errorf("cwds %q", sh.cwds)
	}
	if len(r.Record.Runs) != 2 || r.Record.Runs[0].RepoName != "api" || r.Record.Runs[1].RepoName != "web" {
		t.Errorf("runs %+v", r.Record.Runs)
	}
}

func TestAffectedRepoWithoutChangesRunsNothing(t *testing.T) {
	h, sh, id := multiRepoAffected(t, map[string][]string{"/w/web.worktrees/x": {"src/b.go"}}, nil)
	r, err := h.RunAffected(context.Background(), id)
	if err != nil || r.Verdict != AffectedGreen {
		t.Fatalf("r = %+v err = %v", r, err)
	}
	if len(sh.ran) != 1 || sh.ran[0] != "web-suite" {
		t.Errorf("ran %q", sh.ran)
	}
	if !strings.Contains(r.Record.Note, "api: sin cambios") {
		t.Errorf("note %q", r.Record.Note)
	}
}

func TestAffectedOneRepoRedMakesItRed(t *testing.T) {
	h, _, id := multiRepoAffected(t, map[string][]string{
		"/w/api.worktrees/x": {"src/a.go"}, "/w/web.worktrees/x": {"src/b.go"},
	}, map[string]int{"web-suite": 1})
	r, err := h.RunAffected(context.Background(), id)
	if err != nil || r.Verdict != AffectedRed || !strings.Contains(r.Record.Failure, "[web]") {
		t.Fatalf("r = %+v err = %v", r, err)
	}
}

func TestAffectedNoRepoWithChangesIsUnverified(t *testing.T) {
	h, sh, id := multiRepoAffected(t, nil, nil)
	r, err := h.RunAffected(context.Background(), id)
	if err != nil || r.Verdict != AffectedUnverified || len(sh.ran) != 0 {
		t.Fatalf("r = %+v err = %v ran %q", r, err, sh.ran)
	}
}
