package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/plan"
)

// mrAnswers answers several questions of one AskUserQuestion call.
func mrAnswers(t *testing.T, cwd string, answers map[string]any) string {
	t.Helper()
	return hookCall(t, map[string]any{"hook_event_name": "PostToolUse", "tool_name": "AskUserQuestion", "cwd": cwd,
		"tool_response": map[string]any{"answers": answers}})
}

// mrGitRepo makes a git repo (branch main, one commit, a second branch dev) at dir.
func mrGitRepo(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "a.txt")
	gitIn(t, dir, "commit", "-m", "first")
	gitIn(t, dir, "branch", "dev")
}

// mrParent makes a folder (not a repo) with the git repos `names` inside and enters it.
func mrParent(t *testing.T, names ...string) string {
	t.Helper()
	flowRepo(t) // isolates NXY_HOME and the project dir; its repo is not used
	parent := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(parent); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("NXY_CODEGRAPH_PATH", filepath.Join(wd, "no-such-codegraph"))
	for _, n := range names {
		mrGitRepo(t, filepath.Join(wd, n))
	}
	return wd
}

func mrWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// mrBatch is the JSON of batch n (1 to 9), which touches repoAt and proves itself with accept.
func mrBatch(n int, repoAt, accept string) string {
	return `{"n":` + string(rune('0'+n)) + `,"title":"b` + string(rune('0'+n)) + `","covers":["R1"],"change":[{"at":"` + repoAt + `:1","what":"change it"}],` +
		`"test":{"kind":"new","name":"TestX","accept":` + strconvQuote(accept) + `},"risk":"low"}`
}

const mrCriteria = `"criteria":[{"id":"R1","text":"works","test":"tdd"}]`

// mrStart starts a feature from cwd, saves the plan and shows the checkpoint; returns the feature id.
func mrStart(t *testing.T, cwd, planJSON string) string {
	t.Helper()
	hookCall(t, map[string]any{"hook_event_name": "UserPromptSubmit", "cwd": cwd, "prompt": "/nxy:feature add a thing"})
	id := featureID(t)
	agentPre(t, cwd, "nxy:planner", "feature "+id)
	if code, out, errb := runCmd(t, planJSON, "plan", "save", "--feature", id); code != 0 {
		t.Fatalf("plan save: code %d, out %q, err %q", code, out, errb)
	}
	return id
}

func TestMultiRepoOneRepoBranchInTheFolder(t *testing.T) {
	needSh(t)
	repo := flowGitRepo(t)
	t.Setenv("NXY_CODEGRAPH_PATH", filepath.Join(repo, "no-such-codegraph"))
	p := strings.Replace(batchPlan("test -f done.txt"), `"risk":"low"}]}`, `"risk":"low"}],"suite":"test -f done.txt"}`, 1)
	id := mrStart(t, repo, p)
	agentPost(t, repo, "nxy:planner", nil)
	out := answer(t, repo, "Approve nxy plan "+id+" r1?", "Approve")
	if !strings.Contains(out, "¿en qué rama trabajamos?") {
		t.Fatalf("no branch question: %s", out)
	}
	base := plan.BaseQuestion(id, filepath.Base(repo))
	mrAnswers(t, repo, map[string]any{plan.BranchQuestion(id): "feature/thing", base: "main"})
	if got := gitIn(t, repo, "branch", "--show-current"); got != "feature/thing" {
		t.Fatalf("branch in the session's folder = %q", got)
	}
	if _, err := os.Stat(repo + ".worktrees"); !os.IsNotExist(err) {
		t.Fatalf("a worktree folder was made for a free checkout: %v", err)
	}
	_, st, _ := runCmd(t, "", "status")
	if !strings.Contains(st, "Lote 1") || !strings.Contains(st, "repo: ") || !strings.Contains(st, "feature/thing") {
		t.Fatalf("status: %q", st)
	}

	agentPre(t, repo, "nxy:implementer", "feature "+id+" batch 1")
	if code, o, errb := runCmd(t, "", "batch", "red", "--feature", id); code != 0 {
		t.Fatalf("red: code %d, out %q, err %q", code, o, errb)
	}
	mrWrite(t, filepath.Join(repo, "done.txt"), "x")
	mrWrite(t, filepath.Join(repo, "cmd", "main.go"), "package main\n")
	if code, o, errb := runCmd(t, "", "batch", "green", "--feature", id); code != 0 {
		t.Fatalf("green: code %d, out %q, err %q", code, o, errb)
	}
	if o := agentPost(t, repo, "nxy:implementer", nil); !strings.Contains(o, "affected --feature "+id) {
		t.Fatalf("no affected step: %s", o)
	}
	code, o, errb := runCmd(t, "", "affected", "--feature", id)
	if code != 0 || !strings.Contains(o, "nxy:reviewer-quick") {
		t.Fatalf("affected: code %d, out %q, err %q", code, o, errb)
	}
	agentPre(t, repo, "nxy:reviewer-quick", "feature "+id+" review base")
	if code, o, errb := runCmd(t, baseClean, "review", "save", "--feature", id, "--lens", "base"); code != 0 {
		t.Fatalf("review save: code %d, out %q, err %q", code, o, errb)
	}
	if o := agentPost(t, repo, "nxy:reviewer-quick", nil); !strings.Contains(o, "Listo") {
		t.Fatalf("no Listo: %s", o)
	}
	if got := gitIn(t, repo, "branch", "--show-current"); got != "feature/thing" {
		t.Fatalf("branch after the review = %q", got)
	}
}

// mrHook sends a hook payload as the session sid.
func mrHook(t *testing.T, sid string, payload map[string]any) string {
	t.Helper()
	payload["session_id"] = sid
	return hookCall(t, payload)
}

func mrAgentPre(t *testing.T, sid, cwd, role, prompt string) string {
	t.Helper()
	return mrHook(t, sid, map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Agent", "cwd": cwd,
		"tool_input": map[string]any{"subagent_type": role, "prompt": prompt}})
}

func mrAgentPost(t *testing.T, sid, cwd, role string) string {
	t.Helper()
	return mrHook(t, sid, map[string]any{"hook_event_name": "PostToolUse", "tool_name": "Agent", "cwd": cwd,
		"tool_input": map[string]any{"subagent_type": role, "prompt": "x"}})
}

func mrAnswersAs(t *testing.T, sid, cwd string, answers map[string]any) string {
	t.Helper()
	return mrHook(t, sid, map[string]any{"hook_event_name": "PostToolUse", "tool_name": "AskUserQuestion", "cwd": cwd,
		"tool_response": map[string]any{"answers": answers}})
}

// mrFeatureIDs is the ids `nxy feature list` shows, in order.
func mrFeatureIDs(t *testing.T) []string {
	t.Helper()
	code, out, errb := runCmd(t, "", "feature", "list")
	if code != 0 {
		t.Fatalf("feature list: code %d, err %q", code, errb)
	}
	var ids []string
	for _, l := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(l, "- "); ok && (strings.Contains(l, "(activa)") || strings.Contains(l, "(pausada)")) {
			id, _, _ := strings.Cut(rest, " ")
			ids = append(ids, id)
		}
	}
	return ids
}

// mrOtherSession starts a feature from cwd as session sid (another session holds the checkout) and
// returns its id.
func mrOtherSession(t *testing.T, sid, cwd string, known ...string) string {
	t.Helper()
	mrHook(t, sid, map[string]any{"hook_event_name": "UserPromptSubmit", "cwd": cwd, "prompt": "/nxy:feature add another thing"})
	for _, id := range mrFeatureIDs(t) {
		seen := false
		for _, k := range known {
			seen = seen || k == id
		}
		if !seen {
			return id
		}
	}
	t.Fatalf("no new feature for session %s", sid)
	return ""
}

// mrMultiPlan is a plan over api and web with one batch each.
func mrMultiPlan() string {
	return `{"title":"Contract","repos":[{"path":"api","role":"backend","suite":"test -f done.txt"},` +
		`{"path":"web","role":"front","suite":"test -f done.txt"}],` + mrCriteria + `,"batches":[` +
		mrBatch(1, "api/src/x.go", "test -f done.txt") + `,` + mrBatch(2, "web/src/y.js", "test -f done.txt") + `]}`
}

// mrIn runs fn with the process in dir (the commands read the working directory).
func mrIn(t *testing.T, dir string, fn func()) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	fn()
}

// mrWorktree is the only worktree folder of the repo at repo (<repo>.worktrees/<slug>).
func mrWorktree(t *testing.T, repo string) string {
	t.Helper()
	wts, err := os.ReadDir(repo + ".worktrees")
	if err != nil || len(wts) != 1 {
		t.Fatalf("worktrees of %s: %v, %v", repo, wts, err)
	}
	return filepath.Join(repo+".worktrees", wts[0].Name())
}

func TestMultiRepoParentFolderWorktreesEndToEnd(t *testing.T) {
	needSh(t)
	parent := mrParent(t, "api", "web")
	api, web := filepath.Join(parent, "api"), filepath.Join(parent, "web")
	hookCall(t, map[string]any{"hook_event_name": "UserPromptSubmit", "session_id": "s1", "cwd": parent, "prompt": "/nxy:feature add a thing"})
	id := featureID(t)
	mrAgentPre(t, "s1", parent, "nxy:planner", "feature "+id)
	if code, out, errb := runCmd(t, mrMultiPlan(), "plan", "save", "--feature", id); code != 0 {
		t.Fatalf("plan save: code %d, out %q, err %q", code, out, errb)
	}
	out := mrAgentPost(t, "s1", parent, "nxy:planner")
	if !strings.Contains(out, "api") || !strings.Contains(out, "web") || !strings.Contains(out, "backend") {
		t.Fatalf("checkpoint without the repos: %s", out)
	}
	out = mrAnswersAs(t, "s1", parent, map[string]any{"Approve nxy plan " + id + " r1?": "Approve"})
	if !strings.Contains(out, "¿en qué rama trabajamos?") || !strings.Contains(out, "¿desde qué base se crea la rama en api?") || !strings.Contains(out, "web?") {
		t.Fatalf("no branch and base questions: %s", out)
	}
	mrAnswersAs(t, "s1", parent, map[string]any{
		plan.BranchQuestion(id): "feature/contract", plan.BaseQuestion(id, "api"): "main", plan.BaseQuestion(id, "web"): "dev"})
	wtAPI, wtWeb := mrWorktree(t, api), mrWorktree(t, web)
	if got := gitIn(t, wtAPI, "branch", "--show-current"); got != "feature/contract" {
		t.Fatalf("api worktree branch = %q", got)
	}
	if got := gitIn(t, wtWeb, "branch", "--show-current"); got != "feature/contract" {
		t.Fatalf("web worktree branch = %q", got)
	}
	for _, r := range []string{api, web} {
		if got := gitIn(t, r, "branch", "--show-current"); got != "main" {
			t.Fatalf("original checkout %s moved to %q", r, got)
		}
	}
	if _, st, _ := runCmd(t, "", "status"); !strings.Contains(st, "Lote 1") || !strings.Contains(st, "api → feature/contract") || !strings.Contains(st, "web → feature/contract") {
		t.Fatalf("status: %q", st)
	}
	mrIn(t, api, func() {
		_, list, _ := runCmd(t, "", "feature", "list")
		if !strings.Contains(list, id+" (activa)") || !strings.Contains(list, "api → feature/contract") || !strings.Contains(list, "web → feature/contract") {
			t.Fatalf("list: %q", list)
		}
	})

	// A second feature can start from the same folder once the first one has its worktrees.
	hookCall(t, map[string]any{"hook_event_name": "UserPromptSubmit", "session_id": "s2", "cwd": parent, "prompt": "/nxy:feature another thing"})
	second := featureID(t)
	if second == id {
		t.Fatalf("the second feature was not created: status still shows %s", id)
	}
	if code, o, _ := runCmd(t, "", "feature", "stop"); code != 0 || !strings.Contains(o, "stopped") {
		t.Fatalf("stop of the second: code %d, out %q", code, o)
	}

	// Batch 1 is api's: the original checkout is refused, its worktree is the place.
	mrAgentPre(t, "s1", parent, "nxy:implementer", "feature "+id+" batch 1")
	deny := func(file string) string {
		return mrHook(t, "s1", map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Write", "cwd": parent,
			"agent_id": "a1", "agent_type": "nxy:implementer", "tool_input": map[string]any{"file_path": file}})
	}
	// The checkout keys are normalized (slashes, case on Windows), so compare them that way.
	norm := func(s string) string { return strings.ToLower(filepath.ToSlash(strings.ReplaceAll(s, `\\`, "/"))) }
	if o := deny(filepath.Join(api, "src", "x.go")); !strings.Contains(o, "deny") || !strings.Contains(norm(o), norm(wtAPI)) {
		t.Fatalf("edit in the original checkout not refused with the worktree: %q", o)
	}
	if o := deny(filepath.Join(wtAPI, "src", "x.go")); strings.Contains(o, "deny") {
		t.Fatalf("edit in the worktree refused: %q", o)
	}
	if code, o, errb := runCmd(t, "", "batch", "red", "--feature", id); code != 0 {
		t.Fatalf("red 1: code %d, out %q, err %q", code, o, errb)
	}
	mrWrite(t, filepath.Join(wtAPI, "done.txt"), "x")
	mrWrite(t, filepath.Join(wtAPI, "src", "x.go"), "package src\n")
	if code, o, errb := runCmd(t, "", "batch", "green", "--feature", id); code != 0 {
		t.Fatalf("green 1: code %d, out %q, err %q", code, o, errb)
	}
	mrAgentPost(t, "s1", parent, "nxy:implementer")

	mrAgentPre(t, "s1", parent, "nxy:implementer", "feature "+id+" batch 2")
	if code, o, errb := runCmd(t, "", "batch", "red", "--feature", id); code != 0 {
		t.Fatalf("red 2: code %d, out %q, err %q", code, o, errb)
	}
	mrWrite(t, filepath.Join(wtWeb, "done.txt"), "x")
	mrWrite(t, filepath.Join(wtWeb, "src", "y.js"), "x\n")
	if code, o, errb := runCmd(t, "", "batch", "green", "--feature", id); code != 0 {
		t.Fatalf("green 2: code %d, out %q, err %q", code, o, errb)
	}
	if o := mrAgentPost(t, "s1", parent, "nxy:implementer"); !strings.Contains(o, "affected --feature "+id) {
		t.Fatalf("no affected step: %s", o)
	}

	code, o, errb := runCmd(t, "", "affected", "--feature", id)
	if code != 0 || !strings.Contains(o, "nxy:reviewer-quick") {
		t.Fatalf("affected: code %d, out %q, err %q", code, o, errb)
	}
	rev := mrAgentPre(t, "s1", parent, "nxy:reviewer-quick", "feature "+id+" review base")
	if !strings.Contains(rev, "api/src/x.go") || !strings.Contains(rev, "web/src/y.js") {
		t.Fatalf("reviewer prompt without the files of both repos: %s", rev)
	}
	if code, o, errb := runCmd(t, baseClean, "review", "save", "--feature", id, "--lens", "base"); code != 0 {
		t.Fatalf("review save: code %d, out %q, err %q", code, o, errb)
	}
	if o := mrAgentPost(t, "s1", parent, "nxy:reviewer-quick"); !strings.Contains(o, "Listo") {
		t.Fatalf("no Listo: %s", o)
	}

	// Close: both worktrees go away (they are committed, so nothing is lost).
	for _, wt := range []string{wtAPI, wtWeb} {
		gitIn(t, wt, "add", "-A")
		gitIn(t, wt, "commit", "-m", "work")
	}
	code, o, errb = runCmd(t, "", "feature", "close", "--feature", id)
	if code != 0 {
		t.Fatalf("feature close: code %d, out %q, err %q", code, o, errb)
	}
	for _, wt := range []string{wtAPI, wtWeb} {
		if _, err := os.Stat(wt); !os.IsNotExist(err) {
			t.Fatalf("worktree %s still there: %v", wt, err)
		}
	}
}

func TestMultiRepoBusyReposPauseTheOtherFeature(t *testing.T) {
	needSh(t)
	parent := mrParent(t, "api", "web")
	api := filepath.Join(parent, "api")
	// s1 plans over api and web and holds both.
	hookCall(t, map[string]any{"hook_event_name": "UserPromptSubmit", "session_id": "s1", "cwd": parent, "prompt": "/nxy:feature add a thing"})
	first := featureID(t)
	mrAgentPre(t, "s1", parent, "nxy:planner", "feature "+first)
	if code, out, errb := runCmd(t, mrMultiPlan(), "plan", "save", "--feature", first); code != 0 {
		t.Fatalf("plan save 1: code %d, out %q, err %q", code, out, errb)
	}
	// s2 asks for the same repos.
	o2 := hookCall(t, map[string]any{"hook_event_name": "UserPromptSubmit", "session_id": "s2", "cwd": parent, "prompt": "/nxy:feature add another thing"})
	var id string
	mrIn(t, parent, func() {
		for _, x := range mrFeatureIDs(t) {
			if x != first {
				id = x
			}
		}
	})
	if id == "" {
		t.Fatalf("the second feature was not created: %s", o2)
	}
	mrAgentPre(t, "s2", parent, "nxy:planner", "feature "+id)
	code, out, errb := runCmd(t, mrMultiPlan(), "plan", "save", "--feature", id)
	if code != 1 || !strings.Contains(out+errb, first) {
		t.Fatalf("plan save on busy repos: code %d, out %q, err %q", code, out, errb)
	}
	out = mrAgentPost(t, "s2", parent, "nxy:planner")
	if !strings.Contains(out, "Pausar la feature "+first) || !strings.Contains(out, "Parar esta feature") {
		t.Fatalf("no repo-busy question: %s", out)
	}
	if strings.Contains(out, "Abrir en un worktree") {
		t.Fatalf("two repos offer the single-repo worktree option: %s", out)
	}
	// Pausing frees the repos and the waiting plan goes to the approval, with no other planner.
	out = mrAnswersAs(t, "s2", parent, map[string]any{plan.ClashQuestion(id): "Pausar la feature " + first})
	if !strings.Contains(out, "Approve nxy plan "+id) || strings.Contains(out, "nxy:planner") {
		t.Fatalf("the waiting plan did not reach the approval: %s", out)
	}
	mrIn(t, api, func() {
		_, list, _ := runCmd(t, "", "feature", "list")
		if !strings.Contains(list, first+" (pausada)") || !strings.Contains(list, id+" (activa)") {
			t.Fatalf("list: %q", list)
		}
	})
	// The paused one cannot come back while the other holds the repos.
	code, out, errb = runCmd(t, "", "feature", "resume", "--feature", first)
	if code != 1 || !strings.Contains(errb, "lo tiene ahora la feature "+id) {
		t.Fatalf("resume: code %d, out %q, err %q", code, out, errb)
	}
}

func TestMultiRepoBusyCheckoutOpensAWorktree(t *testing.T) {
	needSh(t)
	repo := flowGitRepo(t)
	t.Setenv("NXY_CODEGRAPH_PATH", filepath.Join(repo, "no-such-codegraph"))
	// Session s1 holds the checkout.
	mrHook(t, "s1", map[string]any{"hook_event_name": "UserPromptSubmit", "cwd": repo, "prompt": "/nxy:feature add a thing"})
	first := featureID(t)
	// Session s2 plans the same repo.
	id := mrOtherSession(t, "s2", repo, first)
	mrAgentPre(t, "s2", repo, "nxy:planner", "feature "+id)
	code, out, errb := runCmd(t, batchPlan("test -f done.txt"), "plan", "save", "--feature", id)
	if code != 1 || !strings.Contains(out+errb, first) {
		t.Fatalf("plan save on a busy repo: code %d, out %q, err %q", code, out, errb)
	}
	out = mrAgentPost(t, "s2", repo, "nxy:planner")
	if !strings.Contains(out, "Abrir en un worktree") || !strings.Contains(out, "Pausar la feature "+first) {
		t.Fatalf("no repo-busy question: %s", out)
	}
	out = mrAnswersAs(t, "s2", repo, map[string]any{plan.ClashQuestion(id): plan.ClashWorktree})
	if !strings.Contains(out, "Approve nxy plan "+id) {
		t.Fatalf("the waiting plan did not reach the approval: %s", out)
	}
	out = mrAnswersAs(t, "s2", repo, map[string]any{"Approve nxy plan " + id + " r1?": "Approve"})
	if !strings.Contains(out, "¿en qué rama trabajamos?") {
		t.Fatalf("no branch question: %s", out)
	}
	mrAnswersAs(t, "s2", repo, map[string]any{plan.BranchQuestion(id): "feature/other", plan.BaseQuestion(id, filepath.Base(repo)): "main"})

	wts, err := os.ReadDir(repo + ".worktrees")
	if err != nil || len(wts) != 1 {
		t.Fatalf("worktrees folder: %v, %v", wts, err)
	}
	wt := filepath.Join(repo+".worktrees", wts[0].Name())
	if got := gitIn(t, wt, "branch", "--show-current"); got != "feature/other" {
		t.Fatalf("branch of the worktree = %q", got)
	}
	if got := gitIn(t, repo, "branch", "--show-current"); got != "main" {
		t.Fatalf("the busy checkout moved to %q", got)
	}
	// The first feature keeps the checkout; the second retains only its worktree.
	if _, st, _ := runCmd(t, "", "status"); !strings.Contains(st, "feature: "+first) {
		t.Fatalf("status of the folder: %q", st)
	}
	_, list, _ := runCmd(t, "", "feature", "list")
	if !strings.Contains(list, id+" (activa)") || !strings.Contains(list, "feature/other") || !strings.Contains(list, ".worktrees") {
		t.Fatalf("list: %q", list)
	}
}
