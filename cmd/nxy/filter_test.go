package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilterOnOffKeepsOtherKeys(t *testing.T) {
	isolate(t)
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(repo, ".nxy", "config.json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("{\n  \"zeta\": 1,\n  \"modules\": {\n    \"metrics\": false\n  },\n  \"alpha\": true\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(cfgPath)

	code, out, _ := runCmd(t, "", "filter", "--cwd", repo)
	if code != 0 || !strings.Contains(out, "filter module: enabled (metrics module: disabled)") ||
		!strings.Contains(out, "engine:") || !strings.Contains(out, "escape hatch:") {
		t.Fatalf("status (%d):\n%s", code, out)
	}
	if after, _ := os.ReadFile(cfgPath); string(after) != string(before) {
		t.Fatalf("status wrote the config:\n%s", after)
	}

	code, out, _ = runCmd(t, "", "filter", "off", "--repo", "--cwd", repo)
	if code != 0 || !strings.HasPrefix(out, "filter module: OFF (written to ") || !strings.Contains(out, "filter module: DISABLED") {
		t.Fatalf("off (%d):\n%s", code, out)
	}
	got, _ := os.ReadFile(cfgPath)
	text := string(got)
	iz, im, ia := strings.Index(text, `"zeta"`), strings.Index(text, `"modules"`), strings.Index(text, `"alpha"`)
	if iz < 0 || !(iz < im && im < ia) || !strings.Contains(text, `"filter": false`) || !strings.Contains(text, `"metrics": false`) {
		t.Fatalf("keys or order lost:\n%s", text)
	}

	code, out, _ = runCmd(t, "", "filter", "on", "--repo", "--cwd", repo)
	if code != 0 || !strings.Contains(out, "filter module: ON") || !strings.Contains(out, "filter module: enabled") {
		t.Fatalf("on (%d):\n%s", code, out)
	}
	if got, _ := os.ReadFile(cfgPath); !strings.Contains(string(got), `"filter": true`) {
		t.Fatalf("on:\n%s", got)
	}
}

func TestFilterOnOffWritesUserConfigByDefault(t *testing.T) {
	isolate(t)
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	userCfg := filepath.Join(os.Getenv("NXY_HOME"), "config.json")
	code, out, _ := runCmd(t, "", "filter", "off", "--cwd", repo)
	if code != 0 || !strings.HasPrefix(out, "filter module: OFF (written to ") {
		t.Fatalf("off (%d):\n%s", code, out)
	}
	got, err := os.ReadFile(userCfg)
	if err != nil || !strings.Contains(string(got), `"filter": false`) {
		t.Fatalf("user config not written (%v):\n%s", err, got)
	}
	if _, err := os.Stat(filepath.Join(repo, ".nxy", "config.json")); err == nil {
		t.Fatal("repo config written without --repo")
	}
}

func TestFilterRefusesBrokenConfig(t *testing.T) {
	for _, repoFlag := range []bool{false, true} {
		isolate(t)
		repo := t.TempDir()
		if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(os.Getenv("NXY_HOME"), "config.json")
		args := []string{"filter", "on", "--cwd", repo}
		if repoFlag {
			path = filepath.Join(repo, ".nxy", "config.json")
			args = append(args, "--repo")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		const broken = "{ not json"
		if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
			t.Fatal(err)
		}
		code, out, _ := runCmd(t, "", args...)
		if code != 1 || !strings.Contains(out, "está roto; no se toca") {
			t.Fatalf("repo=%v (%d):\n%s", repoFlag, code, out)
		}
		if after, _ := os.ReadFile(path); string(after) != broken {
			t.Fatalf("repo=%v: broken config modified:\n%s", repoFlag, after)
		}
	}
}

func TestFilterWritesAtGitRootAndUnknownAction(t *testing.T) {
	isolate(t)
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := runCmd(t, "", "filter", "off", "--repo", "--cwd", sub); code != 0 {
		t.Fatal("off failed")
	}
	if _, err := os.Stat(filepath.Join(repo, ".nxy", "config.json")); err != nil {
		t.Fatalf("not written at the git root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sub, ".nxy")); err == nil {
		t.Fatal("written in the subdirectory")
	}

	_, out, _ := runCmd(t, "", "filter", "bogus", "--cwd", repo)
	if !strings.HasPrefix(out, `unknown action "bogus". Use: status | on | off`+"\n") {
		t.Fatalf("unknown action:\n%s", out)
	}
}
