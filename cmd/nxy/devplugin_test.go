package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/version"
)

func pluginDir(t *testing.T) string {
	t.Helper()
	return repoRoot(t)
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

func TestPluginManifest(t *testing.T) {
	dir := filepath.Join(pluginDir(t), ".claude-plugin")
	b, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Name        string `json:"name"`
		Version     string `json:"version"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m.Name != "nxy" {
		t.Errorf("plugin name = %q, want nxy", m.Name)
	}
	if m.Version != version.Version {
		t.Errorf("plugin.json version = %q, want %q", m.Version, version.Version)
	}
	if strings.Contains(m.Description, "Dev only") {
		t.Errorf("plugin.json description says %q", "Dev only")
	}
	mb, err := os.ReadFile(filepath.Join(dir, "marketplace.json"))
	if err != nil {
		t.Fatal(err)
	}
	var mk struct {
		Plugins []struct {
			Version     string `json:"version"`
			Description string `json:"description"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(mb, &mk); err != nil {
		t.Fatal(err)
	}
	if len(mk.Plugins) == 0 {
		t.Fatal("marketplace.json lists no plugin")
	}
	for _, p := range mk.Plugins {
		if p.Version != version.Version {
			t.Errorf("marketplace.json version = %q, want %q", p.Version, version.Version)
		}
		if strings.Contains(p.Description, "Dev only") {
			t.Errorf("marketplace.json description says %q", "Dev only")
		}
	}
}

func TestPluginAgentAndCommand(t *testing.T) {
	dir := pluginDir(t)
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
	if _, err := os.Stat(filepath.Join(dir, "skills", "nxy-filter", "SKILL.md")); err != nil {
		t.Errorf("skills/nxy-filter/SKILL.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "nxy-workflow")); err == nil {
		t.Errorf("skills/nxy-workflow is a 1.x skill and must not exist")
	}
}

func TestHooksManifest(t *testing.T) {
	h, raw := loadHooks(t, filepath.Join(pluginDir(t), "hooks", "hooks.json"))
	have := map[string]bool{}
	for ev, entries := range h.Hooks {
		for _, e := range entries {
			for _, hk := range e.Hooks {
				if hk.Command != pluginHookLine {
					t.Errorf("%s %q: command %q is not the shim line", ev, e.Matcher, hk.Command)
				}
				have[ev+"|"+e.Matcher] = true
			}
		}
	}
	want := []string{
		"SessionStart|",
		"UserPromptSubmit|",
		"SubagentStop|",
		"SessionEnd|",
		"Stop|",
		"PreToolUse|Bash|PowerShell",
		"PreToolUse|Edit|Write|NotebookEdit|MultiEdit",
		"PreToolUse|Grep|Glob",
		"PreToolUse|Agent|Task",
		"PreToolUse|AskUserQuestion",
		"PreToolUse|Skill",
		"PostToolUse|Bash|PowerShell",
		"PostToolUse|Agent|Task",
		"PostToolUse|AskUserQuestion",
	}
	for _, k := range want {
		if !have[k] {
			t.Errorf("hooks miss %s with the shim line", k)
		}
	}
	if len(have) != len(want) {
		t.Errorf("hooks have %d event/matcher entries, want %d", len(have), len(want))
	}
	for _, e := range h.Hooks["PreToolUse"] {
		if e.Matcher != "AskUserQuestion" {
			continue
		}
		for _, hk := range e.Hooks {
			if hk.Timeout != 1800 || hk.StatusMessage != "nxy: si tenés la TUI abierta, contestá ahí" {
				t.Errorf("PreToolUse AskUserQuestion needs timeout 1800 and the statusMessage, got %d %q", hk.Timeout, hk.StatusMessage)
			}
		}
	}
	for _, bad := range []string{`C:\\`, `C:/`, `/Users/`, `/home/`, `/c/`} {
		if strings.Contains(raw, bad) {
			t.Errorf("hooks carry a personal path (%s)", bad)
		}
	}
}

// walkPlugin visits every file of the plugin folders (not the whole repo: testdata holds /home/ on purpose).
func walkPlugin(t *testing.T, fn func(p string, b []byte)) {
	t.Helper()
	root := pluginDir(t)
	for _, sub := range []string{".claude-plugin", "agents", "commands", "hooks", "skills"} {
		err := filepath.WalkDir(filepath.Join(root, sub), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			fn(p, b)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestPluginNoPersonalPaths(t *testing.T) {
	walkPlugin(t, func(p string, b []byte) {
		for _, bad := range []string{`C:\Users\`, `C:\\Users\\`, `C:/Users/`, `/Users/`, `/home/`} {
			if strings.Contains(string(b), bad) {
				t.Errorf("%s carries a personal path (%s)", p, bad)
			}
		}
	})
}

var fmKeyLine = regexp.MustCompile(`^[a-z-]+: `)

// checkFrontmatter returns the problems of a frontmatter block: every non-empty line must be a
// `key: value` line, and no value may hold a later key glued inside (a description that swallowed
// the next line).
func checkFrontmatter(fm string) []string {
	var bad []string
	for _, line := range strings.Split(fm, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !fmKeyLine.MatchString(line) && !regexp.MustCompile(`^[a-z-]+:$`).MatchString(line) {
			bad = append(bad, "not a key line: "+line)
			continue
		}
		_, val, _ := strings.Cut(line, ":")
		for _, k := range []string{"allowed-tools:", "argument-hint:", "model:", "description:"} {
			if strings.Contains(val, k) {
				bad = append(bad, "value holds "+k+": "+line)
			}
		}
	}
	return bad
}

func TestFrontmatterCheckBites(t *testing.T) {
	good := "description: Shows stats\nallowed-tools: Bash(x:*)\nargument-hint: [a]"
	if bad := checkFrontmatter(good); len(bad) != 0 {
		t.Errorf("good frontmatter flagged: %v", bad)
	}
	for name, fm := range map[string]string{
		"glued":     "description: Shows stats allowed-tools: Bash(x:*)\nmodel: haiku",
		"continued": "description: Shows stats\n  and more\nallowed-tools: Bash(x:*)",
	} {
		if len(checkFrontmatter(fm)) == 0 {
			t.Errorf("%s: not detected", name)
		}
	}
}

func TestCommandsFrontmatter(t *testing.T) {
	dir := filepath.Join(pluginDir(t), "commands")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		for _, bad := range checkFrontmatter(frontmatter(t, filepath.Join(dir, e.Name()))) {
			t.Errorf("%s: %s", e.Name(), bad)
		}
	}
}

// Every `!` block of a command runs the binary through the shim, and allowed-tools covers it:
// Claude Code runs a `!` command only when a Bash(<prefix>:*) rule matches.
func TestCommandsRunTheShim(t *testing.T) {
	const shim = `sh "${CLAUDE_PLUGIN_ROOT}/bin/nxy.cmd"`
	walkPlugin(t, func(p string, b []byte) {
		if strings.Contains(string(b), "../../bin") {
			t.Errorf("%s still points at ../../bin", p)
		}
	})
	dir := filepath.Join(pluginDir(t), "commands")
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
		if fm := frontmatter(t, p); strings.Contains(fm, "(nxy dev)") {
			t.Errorf("%s: description still says (nxy dev)", e.Name())
		}
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
	for _, name := range []string{"stats", "trend", "setup", "filter", "statusline", "config", "locate", "worktree", "mem", "review", "gate", "tui"} {
		if _, err := os.Stat(filepath.Join(dir, name+".md")); err != nil {
			t.Errorf("commands/%s.md missing", name)
		}
	}
	if seen < 7 {
		t.Errorf("only %d `!` blocks found", seen)
	}
}

// Claude Code rejects `.` in a `!` block ("evaluates arguments as shell code"): commands run the
// shim with sh. Hooks and the statusline keep their line.
func TestCommandsDoNotSourceTheShim(t *testing.T) {
	dir := filepath.Join(pluginDir(t), "commands")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, `. "`) {
				t.Errorf("%s:%d sources the shim with `.`: %s", e.Name(), i+1, line)
			}
		}
	}
	for _, name := range []string{"setup.md", "worktree.md"} {
		b, _ := os.ReadFile(filepath.Join(dir, name))
		body := string(b)
		if i := strings.Index(body, "\n---\n"); i >= 0 {
			body = body[i+5:]
		}
		// the text after the `!` block must tell the model to use sh too
		var text []string
		for _, line := range strings.Split(body, "\n") {
			if !strings.Contains(line, "!`") {
				text = append(text, line)
			}
		}
		if !strings.Contains(strings.Join(text, "\n"), `sh "${CLAUDE_PLUGIN_ROOT}/bin/nxy.cmd"`) {
			t.Errorf("%s: the text does not tell the model to run the shim with sh", name)
		}
	}
}
