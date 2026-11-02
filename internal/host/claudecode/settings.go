package claudecode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/mananos/nxy.dev/internal/filter"
	"github.com/mananos/nxy.dev/internal/setup"
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

// SetupInfo is what the Claude Code config files say about rtk's hook and the codegraph MCP server
// (hosts/claude-code/setup-state.mjs). Broken or missing files are ignored; nothing is written.
type SetupInfo struct {
	Settings   []any    // parsed settings.json files that exist
	MCP        []any    // parsed ~/.claude.json and .mcp.json
	MCPScope   string   // where the codegraph MCP lives as `claude mcp remove -s` names it: user, local, project or ""
	HasMCP     bool     // codegraph registered as MCP anywhere
	HookScopes []string // where rtk's hook lives: user and/or project; ["user"] when nowhere
}

// SetupFacts reads the config files for home and the project dir cwd.
func SetupFacts(home, cwd string) SetupInfo {
	var info SetupInfo
	loose := func(path string) map[string]any { return readSettings(path) }
	for _, f := range SettingsFiles(home, cwd) {
		if o := loose(f); o != nil {
			info.Settings = append(info.Settings, o)
		}
	}
	userJSON := loose(filepath.Join(home, ".claude.json"))
	projectMCP := loose(filepath.Join(cwd, ".mcp.json"))
	for _, o := range []map[string]any{userJSON, projectMCP} {
		if o != nil {
			info.MCP = append(info.MCP, o)
		}
	}
	projects, _ := userJSON["projects"].(map[string]any)
	local, _ := projects[cwd].(map[string]any)
	switch {
	case setup.DetectCodegraphMcp(userJSON):
		info.MCPScope = "user"
	case setup.DetectCodegraphMcp(local):
		info.MCPScope = "local"
	case setup.DetectCodegraphMcp(projectMCP):
		info.MCPScope = "project"
	}
	info.HasMCP = setup.DetectCodegraphMcp(local)
	for _, o := range append(append([]any{}, info.Settings...), info.MCP...) {
		info.HasMCP = info.HasMCP || setup.DetectCodegraphMcp(o)
	}
	userSettings, _ := filepath.Abs(filepath.Join(home, ".claude", "settings.json"))
	for _, f := range SettingsFiles(home, cwd) {
		o := loose(f)
		if o == nil || !setup.DetectRtkConflict([]any{o}) {
			continue
		}
		scope := "project"
		if abs, _ := filepath.Abs(f); abs == userSettings {
			scope = "user"
		}
		if !contains(info.HookScopes, scope) {
			info.HookScopes = append(info.HookScopes, scope)
		}
	}
	if len(info.HookScopes) == 0 {
		info.HookScopes = []string{"user"}
	}
	return info
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
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
