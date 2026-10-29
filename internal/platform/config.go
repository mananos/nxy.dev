package platform

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/matyy-a/nxy.dev/internal/filter"
)

// GitRoot is the first ancestor of dir (or dir itself) holding a `.git` (directory or worktree
// pointer file), looking up at most 64 levels. Not a repo: "", false.
func GitRoot(dir string) (string, bool) {
	d, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	for i := 0; i < 64; i++ {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return d, true
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", false
		}
		d = parent
	}
	return "", false
}

// defaultFilterConfig is modules.filter and filter.* of nxy.config.json (a test keeps them equal).
func defaultFilterConfig() map[string]any {
	return map[string]any{
		"modules": map[string]any{"filter": true},
		"filter": map[string]any{
			"engine":                       "auto",
			"excludeCommands":              []any{},
			"onlyCommands":                 []any{},
			"autoAllowWhenOriginalAllowed": true,
		},
	}
}

// deepMerge mirrors core/config.mjs: objects recurse, arrays and scalars replace.
func deepMerge(base, src map[string]any) map[string]any {
	out := make(map[string]any, len(base))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range src {
		sm, sok := v.(map[string]any)
		bm, bok := base[k].(map[string]any)
		if sok && bok {
			out[k] = deepMerge(bm, sm)
		} else {
			out[k] = v
		}
	}
	return out
}

// readJSONObject returns nil for a missing file, broken JSON or a non-object.
func readJSONObject(path string) map[string]any {
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

// truthy is JavaScript truthiness for a decoded JSON value.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		return x != ""
	default:
		return true
	}
}

func stringList(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// LoadFilterSettings resolves the filter settings: defaults, then <UserDir>/config.json, then
// <git root or projectDir>/.nxy/config.json, then NXY_FILTER=0|1 and NXY_ENGINE=rtk|off.
func LoadFilterSettings(projectDir string, getenv func(string) string) filter.Settings {
	cfg := defaultFilterConfig()
	if u, err := UserDir(); err == nil {
		cfg = deepMerge(cfg, readJSONObject(filepath.Join(u, "config.json")))
	}
	root := projectDir
	if r, ok := GitRoot(projectDir); ok {
		root = r
	}
	cfg = deepMerge(cfg, readJSONObject(filepath.Join(root, ".nxy", "config.json")))
	if v := getenv("NXY_FILTER"); v == "0" || v == "1" {
		cfg = deepMerge(cfg, map[string]any{"modules": map[string]any{"filter": v == "1"}})
	}
	if v := getenv("NXY_ENGINE"); v == "rtk" || v == "off" {
		cfg = deepMerge(cfg, map[string]any{"filter": map[string]any{"engine": v}})
	}
	modules, _ := cfg["modules"].(map[string]any)
	f, _ := cfg["filter"].(map[string]any)
	engine, _ := f["engine"].(string)
	return filter.Settings{
		Enabled:         truthy(modules["filter"]),
		Engine:          engine,
		ExcludeCommands: stringList(f["excludeCommands"]),
		OnlyCommands:    stringList(f["onlyCommands"]),
		AutoAllow:       truthy(f["autoAllowWhenOriginalAllowed"]),
	}
}
