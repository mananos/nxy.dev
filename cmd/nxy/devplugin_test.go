package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func devPluginDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "dev", "plugin")
}

func frontmatter(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		t.Fatalf("%s has no frontmatter", path)
	}
	rest := s[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		t.Fatalf("%s frontmatter is not closed", path)
	}
	return rest[:end]
}

func TestDevPluginManifest(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(devPluginDir(t), ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m.Name != "nxy" {
		t.Errorf("plugin name = %q, want nxy", m.Name)
	}
}

func TestDevPluginAgentAndCommand(t *testing.T) {
	dir := devPluginDir(t)
	agent := frontmatter(t, filepath.Join(dir, "agents", "planner.md"))
	for _, want := range []string{"name: planner", "description:", "tools:", "model:"} {
		if !strings.Contains(agent, want) {
			t.Errorf("agents/planner.md frontmatter misses %q", want)
		}
	}
	impl := frontmatter(t, filepath.Join(dir, "agents", "implementer.md"))
	for _, want := range []string{"name: implementer", "description:", "tools:", "model:"} {
		if !strings.Contains(impl, want) {
			t.Errorf("agents/implementer.md frontmatter misses %q", want)
		}
	}
	for _, name := range []string{"reviewer", "reviewer-quick", "refuter"} {
		fm := frontmatter(t, filepath.Join(dir, "agents", name+".md"))
		for _, want := range []string{"name: " + name + "\n", "description:", "tools: Read, Grep, Glob, Bash", "model: sonnet"} {
			if !strings.Contains(fm+"\n", want) {
				t.Errorf("agents/%s.md frontmatter misses %q", name, want)
			}
		}
	}
	scout := frontmatter(t, filepath.Join(dir, "agents", "scout.md"))
	for _, want := range []string{"name: scout\n", "description:", "tools: Read, Grep, Glob, Bash", "model: sonnet"} {
		if !strings.Contains(scout+"\n", want) {
			t.Errorf("agents/scout.md frontmatter misses %q", want)
		}
	}
	for _, name := range []string{"feature", "engineer"} {
		cmd := frontmatter(t, filepath.Join(dir, "commands", name+".md"))
		if !strings.Contains(cmd, "description:") {
			t.Errorf("commands/%s.md frontmatter misses description", name)
		}
	}
}

func TestDevHooksManifestF21(t *testing.T) {
	dev, _ := loadHooks(t, filepath.Join(devPluginDir(t), "hooks", "hooks.json"))
	have := map[string]bool{}
	for ev, entries := range dev.Hooks {
		for _, e := range entries {
			for _, h := range e.Hooks {
				if h.Command == devHookLine {
					have[ev+"|"+e.Matcher] = true
				}
			}
		}
	}
	for _, k := range []string{
		"SessionStart|",
		"UserPromptSubmit|",
		"PreToolUse|Bash|PowerShell",
		"PreToolUse|Edit|Write|NotebookEdit|MultiEdit",
		"PreToolUse|Agent|Task",
		"PreToolUse|AskUserQuestion",
		"PostToolUse|Agent|Task",
		"PostToolUse|AskUserQuestion",
	} {
		if !have[k] {
			t.Errorf("dev hooks miss %s with the shim line", k)
		}
	}
}

func TestDevPluginNoPersonalPaths(t *testing.T) {
	dir := devPluginDir(t)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, bad := range []string{`C:\Users\`, `C:\\Users\\`, `C:/Users/`, `/Users/`, `/home/`} {
			if strings.Contains(string(b), bad) {
				t.Errorf("%s carries a personal path (%s)", p, bad)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
