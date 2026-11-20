package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitFeatureRepo makes a real git repo <tmp>/repo (main, with branches dev and exist), enters it and
// returns its directory as the process sees it.
func gitFeatureRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	isolate(t)
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = repo
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.name", "t")
	run("config", "user.email", "t@example.com")
	run("config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "init")
	run("branch", "dev")
	run("branch", "exist")
	enter(t, repo)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

func enter(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(filepath.FromSlash(dir)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })
}

// created pulls the worktree path out of the "new" answer.
func created(t *testing.T, out string) string {
	t.Helper()
	const pre = "Listo: worktree creado en "
	rest, ok := strings.CutPrefix(out, pre)
	if !ok {
		t.Fatalf("unexpected answer: %q", out)
	}
	path, _, _ := strings.Cut(rest, " (rama ")
	return path
}

func TestWorktreeListFromParentFolder(t *testing.T) {
	needSh(t)
	parent := mrParent(t, "api", "web")
	mrIn(t, parent, func() {
		if code, _, _ := runCmd(t, "", "feature", "list"); code == 0 {
			t.Fatalf("a parent folder without features should still fail")
		}
	})
	hookCall(t, map[string]any{"hook_event_name": "UserPromptSubmit", "session_id": "s1", "cwd": parent, "prompt": "/nxy:feature add a thing"})
	id := featureID(t)
	mrIn(t, parent, func() {
		code, out, errb := runCmd(t, "", "feature", "list")
		if code != 0 || !strings.Contains(out, id+" (activa)") {
			t.Fatalf("list: code %d, out %q, err %q", code, out, errb)
		}
	})
}

func TestFeatureWorktreeLifecycle(t *testing.T) {
	gitFeatureRepo(t)

	code, out, errb := runCmd(t, "", "feature", "new", "--name", "Login Google", "--base", "dev", "--issue", "#14")
	if code != 0 || !strings.Contains(out, "feature/#14_login_google") || !strings.Contains(out, "git worktree add -b") || !strings.Contains(out, "claude") {
		t.Fatalf("new: code %d, out %q, err %q", code, out, errb)
	}
	wt := created(t, out)

	code, out, _ = runCmd(t, "", "feature")
	if code != 0 || !strings.Contains(out, "Worktrees de repo (2):") || !strings.Contains(out, "(principal, esta sesión)") ||
		!strings.Contains(out, "[feature/#14_login_google]") || !strings.Contains(out, "limpio") || strings.Contains(out, "pausado") {
		t.Fatalf("list: code %d, out %q", code, out)
	}
	if strings.Contains(out, "sin handoff") {
		t.Fatalf("the list must not talk about handoffs: %q", out)
	}

	code, out, errb = runCmd(t, "", "feature", "pause", "--path", wt)
	if code != 0 || !strings.Contains(out, "marcado como pausado") || !strings.Contains(out, "cd ") {
		t.Fatalf("pause: code %d, out %q, err %q", code, out, errb)
	}
	if _, out, _ = runCmd(t, "", "feature", "list"); !strings.Contains(out, "pausado hace menos de 1 min") {
		t.Fatalf("list does not show the pause: %q", out)
	}
	if _, err := os.Stat(filepath.Join(filepath.FromSlash(wt), ".nxy", "local", "features", "paused.json")); err != nil {
		t.Fatalf("pause mark missing: %v", err)
	}

	if code, out, errb = runCmd(t, "", "feature", "resume", "--path", wt); code != 0 || !strings.Contains(out, "ya no figura como pausado") {
		t.Fatalf("resume: code %d, out %q, err %q", code, out, errb)
	}
	if _, out, _ = runCmd(t, "", "feature", "list"); strings.Contains(out, "pausado") {
		t.Fatalf("list still shows the pause: %q", out)
	}

	code, out, errb = runCmd(t, "", "feature", "close", "--path", wt)
	if code != 0 || !strings.Contains(out, "git worktree remove") || !strings.Contains(out, "La rama no se borra") {
		t.Fatalf("close: code %d, out %q, err %q", code, out, errb)
	}
	if _, err := os.Stat(filepath.FromSlash(wt)); !os.IsNotExist(err) {
		t.Fatalf("folder still there: %v", err)
	}
}

func TestFeaturePauseAsksForNoteAndCloseDeletesNotes(t *testing.T) {
	gitFeatureRepo(t)
	code, out, errb := runCmd(t, "", "feature", "new", "--name", "notas", "--base", "dev")
	if code != 0 {
		t.Fatalf("new: code %d, out %q, err %q", code, out, errb)
	}
	wt := created(t, out)

	code, out, errb = runCmd(t, "", "feature", "pause", "--path", wt)
	if code != 0 || !strings.Contains(out, "¿Algo para la próxima sesión?") || !strings.Contains(out, "note save") || !strings.Contains(out, "--cwd") {
		t.Fatalf("pause: code %d, out %q, err %q", code, out, errb)
	}
	if strings.Contains(out, "--feature") {
		t.Fatalf("no feature is active in the worktree: %q", out)
	}

	if code, out, errb = runCmd(t, "falta el test", "note", "save", "retomar", "--cwd", wt); code != 0 {
		t.Fatalf("note save: code %d, out %q, err %q", code, out, errb)
	}
	if _, out, _ = runCmd(t, "", "note", "list", "--cwd", wt); !strings.Contains(out, "retomar") {
		t.Fatalf("note not listed: %q", out)
	}

	code, out, errb = runCmd(t, "", "feature", "close", "--path", wt)
	if code != 0 || !strings.Contains(out, "Se borraron 1 notas temporales") {
		t.Fatalf("close: code %d, out %q, err %q", code, out, errb)
	}
	if _, out, _ = runCmd(t, "", "note", "list", "--all"); strings.Contains(out, "retomar") {
		t.Fatalf("note survived the close: %q", out)
	}
}

func TestFeatureNewExistingBranch(t *testing.T) {
	gitFeatureRepo(t)
	code, out, errb := runCmd(t, "", "feature", "new", "--branch", "exist")
	if code != 0 || !strings.Contains(out, "(rama exist)") || strings.Contains(out, " -b ") {
		t.Fatalf("code %d, out %q, err %q", code, out, errb)
	}
	if code, _, errb = runCmd(t, "", "feature", "new", "--branch", "exist"); code != 1 || !strings.Contains(errb, "ya existe") {
		t.Fatalf("second open: code %d, err %q", code, errb)
	}
}

func TestFeatureNewRejections(t *testing.T) {
	gitFeatureRepo(t)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"new", "--name", "x", "--branch", "dev"}, "no los dos"},
		{[]string{"new"}, "falta --name con --base, o --branch"},
		{[]string{"new", "--name", "x"}, "falta --base"},
		{[]string{"new", "--branch", "dev", "--base", "main"}, "sólo van con --name"},
		{[]string{"new", "--branch", "dev", "--issue", "3"}, "sólo van con --name"},
		{[]string{"new", "--name", "x", "--base", "nope"}, "la base nope no existe"},
		{[]string{"pause"}, "falta --path"},
		{[]string{"close"}, "falta --path"},
		{[]string{"pause", "--path", "nowhere"}, "no está en la lista"},
		{[]string{"close", "--path", "nowhere"}, "no está en la lista"},
		{[]string{"bogus"}, "uso: nxy feature"},
		{[]string{"list", "extra"}, "argumento inesperado"},
	} {
		code, out, errb := runCmd(t, "", append([]string{"feature"}, c.args...)...)
		if code != 1 || out != "" || !strings.Contains(errb, c.want) {
			t.Errorf("%v: code %d, out %q, err %q; want %q", c.args, code, out, errb, c.want)
		}
	}
}

func TestFeatureCloseRefusals(t *testing.T) {
	repo := gitFeatureRepo(t)
	_, out, _ := runCmd(t, "", "feature", "new", "--name", "wip", "--base", "main")
	wt := created(t, out)
	if err := os.WriteFile(filepath.Join(filepath.FromSlash(wt), "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if code, _, errb := runCmd(t, "", "feature", "close", "--path", wt); code != 1 || !strings.Contains(errb, "1 archivo sin commitear") {
		t.Fatalf("dirty: code %d, err %q", code, errb)
	}
	if code, _, errb := runCmd(t, "", "feature", "close", "--path", repo); code != 1 || !strings.Contains(errb, "principal") {
		t.Fatalf("main: code %d, err %q", code, errb)
	}
	if _, err := os.Stat(filepath.FromSlash(wt)); err != nil {
		t.Fatal("refused closes must leave the folder")
	}

	// a feature of the v2 that is not done lives in the worktree: closing it, even with --force, is refused
	hookCall(t, map[string]any{"hook_event_name": "UserPromptSubmit", "cwd": wt, "prompt": "/nxy:feature add a flag"})
	code, _, errb := runCmd(t, "", "feature", "close", "--path", wt, "--force")
	if code != 1 || !strings.Contains(errb, "feature de nxy sin terminar") {
		t.Fatalf("active feature: code %d, err %q", code, errb)
	}
	enter(t, wt)
	if code, _, _ := runCmd(t, "", "feature", "stop"); code != 0 {
		t.Fatalf("stop: code %d", code)
	}
	enter(t, repo)
	if code, out, errb := runCmd(t, "", "feature", "close", "--path", wt, "--force"); code != 0 || !strings.Contains(out, "--force") {
		t.Fatalf("after stop: code %d, out %q, err %q", code, out, errb)
	}
}
