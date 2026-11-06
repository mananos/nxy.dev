package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitIn runs git in dir with a local identity and no signing, and returns its trimmed output.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-c", "user.name=nxy", "-c", "user.email=nxy@example.com", "-c", "commit.gpgsign=false"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// flowGitRepo is flowRepo with a real git repo (branch main, one commit).
func flowGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	repo := flowRepo(t)
	if err := os.RemoveAll(filepath.Join(repo, ".git")); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "add", "a.txt")
	gitIn(t, repo, "commit", "-m", "first")
	gitIn(t, repo, "branch", "dev") // a second base, so the base question is asked
	return repo
}

func TestBranchStepEndToEnd(t *testing.T) {
	needSh(t)
	repo := flowGitRepo(t)
	hookCall(t, map[string]any{"hook_event_name": "UserPromptSubmit", "cwd": repo, "prompt": "/nxy:feature add a flag"})
	id := featureID(t)
	agentPre(t, repo, "nxy:planner", "feature "+id)
	if code, out, errb := runCmd(t, batchPlan("true"), "plan", "save", "--feature", id); code != 0 {
		t.Fatalf("plan save: code %d, out %q, err %q", code, out, errb)
	}
	agentPost(t, repo, "nxy:planner", nil)
	out := answer(t, repo, "Approve nxy plan "+id+" r1?", "Approve")
	if !strings.Contains(out, "¿en qué rama trabajamos?") || !strings.Contains(out, "¿desde qué base") {
		t.Fatalf("no branch question after approve: %s", out)
	}
	if _, st, _ := runCmd(t, "", "status"); !strings.Contains(st, "Rama") {
		t.Fatalf("status = %q", st)
	}
	// A failed branch asks again, with git's words, and nothing moves.
	out = hookCall(t, map[string]any{"hook_event_name": "PostToolUse", "tool_name": "AskUserQuestion", "cwd": repo,
		"tool_response": map[string]any{"answers": map[string]any{
			"nxy feature " + id + ": ¿en qué rama trabajamos?":                                        "feature/bad..name",
			"nxy feature " + id + ": ¿desde qué base se crea la rama en " + filepath.Base(repo) + "?": "main"}}})
	if got := gitIn(t, repo, "branch", "--show-current"); got != "main" {
		t.Fatalf("branch after a bad name = %q", got)
	}
	if _, st, _ := runCmd(t, "", "status"); !strings.Contains(st, "Rama") {
		t.Fatalf("status after a bad name = %q (hook: %s)", st, out)
	}
	hookCall(t, map[string]any{"hook_event_name": "PostToolUse", "tool_name": "AskUserQuestion", "cwd": repo,
		"tool_response": map[string]any{"answers": map[string]any{
			"nxy feature " + id + ": ¿en qué rama trabajamos?":                                        "feature/mine",
			"nxy feature " + id + ": ¿desde qué base se crea la rama en " + filepath.Base(repo) + "?": "main"}}})
	if got := gitIn(t, repo, "branch", "--show-current"); got != "feature/mine" {
		t.Fatalf("branch = %q", got)
	}
	if _, st, _ := runCmd(t, "", "status"); !strings.Contains(st, "Lote 1") {
		t.Fatalf("status after the branch: %q", st)
	}
}
