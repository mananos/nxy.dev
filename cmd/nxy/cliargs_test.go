package main

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/platform"
)

func TestResolveCwdPrecedence(t *testing.T) {
	flagDir := t.TempDir()
	envDir := t.TempDir()
	t.Setenv("CLAUDE_PROJECT_DIR", envDir)

	if got := resolveCwd(parseSetupArgs([]string{"--cwd", flagDir})); got != platform.NativePath(flagDir) {
		t.Fatalf("--cwd must win, got %q", got)
	}
	if got := resolveCwd(parseSetupArgs(nil)); got != platform.NativePath(envDir) {
		t.Fatalf("env must come next, got %q", got)
	}
	// --cwd without a value is ignored, not an error.
	if got := resolveCwd(parseSetupArgs([]string{"--cwd"})); got != platform.NativePath(envDir) {
		t.Fatalf("--cwd without value must fall back to env, got %q", got)
	}
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	if got := resolveCwd(parseSetupArgs(nil)); got != platform.NativePath(osGetwd()) {
		t.Fatalf("must fall back to the process dir, got %q", got)
	}
}

func TestSplitRepo(t *testing.T) {
	repo, pos := splitRepo([]string{"set", "--repo", "a", "b"})
	if !repo || !reflect.DeepEqual(pos, []string{"set", "a", "b"}) {
		t.Fatalf("got %v %v", repo, pos)
	}
	repo, pos = splitRepo([]string{"show"})
	if repo || !reflect.DeepEqual(pos, []string{"show"}) {
		t.Fatalf("got %v %v", repo, pos)
	}
}

func TestConfigFilePaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cwd := t.TempDir()
	user, repo, err := configFilePaths(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(user) != "config.json" || filepath.Base(filepath.Dir(repo)) != ".nxy" {
		t.Fatalf("got %q %q", user, repo)
	}
}
