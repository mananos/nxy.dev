package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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
	doc := frontmatter(t, filepath.Join(dir, "agents", "documenter.md"))
	for _, want := range []string{"name: documenter\n", "description:", "tools: Bash, Read, Edit, Write, Grep, Glob", "model: sonnet", "effort: low"} {
		if !strings.Contains(doc+"\n", want) {
			t.Errorf("agents/documenter.md frontmatter misses %q", want)
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

// Every `!` block of a dev command runs the binary through the shim, and allowed-tools covers it:
// Claude Code runs a `!` command only when a Bash(<prefix>:*) rule matches.
func TestDevCommandsRunTheShim(t *testing.T) {
	const shim = `. "${CLAUDE_PLUGIN_ROOT}/../../bin/nxy.cmd"`
	dir := filepath.Join(devPluginDir(t), "commands")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	bang := regexp.MustCompile("!`([^`]+)`")
	allowed := regexp.MustCompile(`(?m)^allowed-tools: Bash\((.+):\*\)$`)
	seen := 0
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		b, _ := os.ReadFile(p)
		s := string(b)
		for _, m := range bang.FindAllStringSubmatch(s, -1) {
			seen++
			if !strings.HasPrefix(m[1], shim+" ") {
				t.Errorf("%s: `!` block %q does not run the shim", e.Name(), m[1])
			}
			a := allowed.FindStringSubmatch(frontmatter(t, p))
			if a == nil || !strings.HasPrefix(m[1], a[1]) {
				t.Errorf("%s: allowed-tools does not cover %q", e.Name(), m[1])
			}
		}
	}
	for _, name := range []string{"stats", "trend", "setup", "filter", "statusline", "config", "locate", "worktree", "mem", "review"} {
		if _, err := os.Stat(filepath.Join(dir, name+".md")); err != nil {
			t.Errorf("commands/%s.md missing", name)
		}
	}
	if seen < 7 {
		t.Errorf("only %d `!` blocks found", seen)
	}
}
