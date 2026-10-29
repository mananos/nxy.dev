package claudecode

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/matyy-a/nxy.dev/internal/filter"
)

// SettingsFiles are the Claude Code settings files that can carry hooks and permissions, in precedence order.
func SettingsFiles(home, projectDir string) []string {
	return []string{
		filepath.Join(home, ".claude", "settings.json"),
		filepath.Join(projectDir, ".claude", "settings.json"),
		filepath.Join(projectDir, ".claude", "settings.local.json"),
	}
}

// readSettings returns nil for a missing or broken file. Settings files are only ever read.
func readSettings(path string) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return nil
	}
	return m
}

func strList(v any) []string {
	arr, _ := v.([]any)
	var out []string
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// LoadPermissionRules reads permissions.allow / permissions.deny from the files (missing or broken skipped).
func LoadPermissionRules(files []string) filter.Rules {
	var out filter.Rules
	for _, f := range files {
		p, _ := readSettings(f)["permissions"].(map[string]any)
		out.Allow = append(out.Allow, strList(p["allow"])...)
		out.Deny = append(out.Deny, strList(p["deny"])...)
	}
	return out
}

// PreToolUseHookCommands lists every hooks.PreToolUse[].hooks[].command string in the files.
func PreToolUseHookCommands(files []string) []string {
	var out []string
	for _, f := range files {
		hooks, _ := readSettings(f)["hooks"].(map[string]any)
		groups, _ := hooks["PreToolUse"].([]any)
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			hs, _ := gm["hooks"].([]any)
			for _, h := range hs {
				hm, _ := h.(map[string]any)
				if c, ok := hm["command"].(string); ok {
					out = append(out, c)
				}
			}
		}
	}
	return out
}
