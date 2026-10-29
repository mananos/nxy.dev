package claudecode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/mananos/nxy.dev/internal/filter"
)

// SettingsFiles are the Claude Code settings files that can carry hooks and permissions, in precedence order.
func SettingsFiles(home, projectDir string) []string {
	return []string{
		filepath.Join(home, ".claude", "settings.json"),
		filepath.Join(projectDir, ".claude", "settings.json"),
		filepath.Join(projectDir, ".claude", "settings.local.json"),
	}
}

// LegacyActive reports whether a 1.x nxy plugin is enabled in the settings files and
// installed at version 1.x. Read-only; any error means false.
func LegacyActive(home, projectDir string) bool {
	enabled := map[string]bool{}
	for _, f := range SettingsFiles(home, projectDir) {
		ep, _ := readSettings(f)["enabledPlugins"].(map[string]any)
		for k, v := range ep {
			if on, ok := v.(bool); ok && strings.HasPrefix(k, "nxy@") {
				enabled[k] = on
			}
		}
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude", "plugins", "installed_plugins.json"))
	if err != nil {
		return false
	}
	var inst struct {
		Plugins map[string][]struct {
			Version string `json:"version"`
		} `json:"plugins"`
	}
	if json.Unmarshal(data, &inst) != nil {
		return false
	}
	for k, on := range enabled {
		if !on {
			continue
		}
		for _, e := range inst.Plugins[k] {
			if strings.HasPrefix(e.Version, "1.") {
				return true
			}
		}
	}
	return false
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
