package platform

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// noEnv has no NXY_* switches; NXY_HOME still comes from the process (setup sets it).
func noEnv(k string) string {
	if k == "NXY_HOME" {
		return os.Getenv(k)
	}
	return ""
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setup(t *testing.T) (nxyHome, project string) {
	t.Helper()
	nxyHome = t.TempDir()
	t.Setenv("NXY_HOME", nxyHome)
	project = t.TempDir()
	return nxyHome, project
}

func TestDefaults(t *testing.T) {
	_, project := setup(t)
	s := LoadFilterSettings(project, noEnv)
	if !s.Enabled || s.Engine != "auto" || !s.AutoAllow || len(s.ExcludeCommands) != 0 || len(s.OnlyCommands) != 0 {
		t.Fatalf("defaults: %+v", s)
	}
}

func TestLayering(t *testing.T) {
	home, project := setup(t)
	writeFile(t, filepath.Join(home, "config.json"), `{"filter":{"engine":"rtk","excludeCommands":["a","b"],"autoAllowWhenOriginalAllowed":false}}`)
	s := LoadFilterSettings(project, noEnv)
	if s.Engine != "rtk" || s.AutoAllow || !reflect.DeepEqual(s.ExcludeCommands, []string{"a", "b"}) || !s.Enabled {
		t.Fatalf("user layer: %+v", s)
	}
	// project wins; arrays replace; sibling keys survive
	writeFile(t, filepath.Join(project, ".nxy", "config.json"), `{"modules":{"filter":false},"filter":{"excludeCommands":["c"]}}`)
	s = LoadFilterSettings(project, noEnv)
	if s.Enabled || s.Engine != "rtk" || !reflect.DeepEqual(s.ExcludeCommands, []string{"c"}) {
		t.Fatalf("project layer: %+v", s)
	}
}

func TestBrokenFileIgnored(t *testing.T) {
	home, project := setup(t)
	writeFile(t, filepath.Join(home, "config.json"), `{not json`)
	writeFile(t, filepath.Join(project, ".nxy", "config.json"), `[1,2]`)
	s := LoadFilterSettings(project, noEnv)
	if !s.Enabled || s.Engine != "auto" {
		t.Fatalf("broken files must be ignored: %+v", s)
	}
}

func TestTruthiness(t *testing.T) {
	_, project := setup(t)
	writeFile(t, filepath.Join(project, ".nxy", "config.json"), `{"modules":{"filter":0},"filter":{"autoAllowWhenOriginalAllowed":"yes"}}`)
	s := LoadFilterSettings(project, noEnv)
	if s.Enabled || !s.AutoAllow {
		t.Fatalf("truthiness: %+v", s)
	}
}

func TestEnvOverrides(t *testing.T) {
	home, project := setup(t)
	writeFile(t, filepath.Join(home, "config.json"), `{"modules":{"filter":false}}`)
	env := map[string]string{"NXY_FILTER": "1", "NXY_ENGINE": "off"}
	s := LoadFilterSettings(project, func(k string) string { return env[k] })
	if !s.Enabled || s.Engine != "off" {
		t.Fatalf("env: %+v", s)
	}
	env = map[string]string{"NXY_FILTER": "0", "NXY_ENGINE": "bogus"}
	s = LoadFilterSettings(project, func(k string) string { return env[k] })
	if s.Enabled || s.Engine != "auto" {
		t.Fatalf("env: %+v", s)
	}
}

func TestGitRootFromSubdir(t *testing.T) {
	home, _ := setup(t)
	_ = home
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	got, ok := GitRoot(sub)
	if !ok || got != root {
		t.Fatalf("GitRoot = %q, %v; want %q", got, ok, root)
	}
	writeFile(t, filepath.Join(root, ".nxy", "config.json"), `{"filter":{"engine":"off"}}`)
	if s := LoadFilterSettings(sub, noEnv); s.Engine != "off" {
		t.Fatalf("config at git root not read from subdir: %+v", s)
	}
}

func TestLoadConfigMetricsGateLayering(t *testing.T) {
	home, project := setup(t)
	cfg := LoadConfig(project, noEnv)
	m := cfg["metrics"].(map[string]any)
	if m["subscription"] != true || m["projectsDir"] != nil || m["cacheBreakThreshold"] != float64(100000) {
		t.Fatalf("metrics defaults: %v", m)
	}
	writeFile(t, filepath.Join(home, "config.json"), `{"metrics":{"statusline":{"preset":"calm"}},"gate":{"contextTokens":5}}`)
	writeFile(t, filepath.Join(project, ".nxy", "config.json"), `{"metrics":{"statusline":{"brand":"x"},"projectsDir":"/p"}}`)
	cfg = LoadConfig(project, noEnv)
	sl := cfg["metrics"].(map[string]any)["statusline"].(map[string]any)
	if sl["preset"] != "calm" || sl["brand"] != "x" || sl["layout"] != "line" {
		t.Fatalf("statusline layering: %v", sl)
	}
	if cfg["metrics"].(map[string]any)["projectsDir"] != "/p" || cfg["gate"].(map[string]any)["contextTokens"] != float64(5) {
		t.Fatalf("layering: %v", cfg)
	}
}

func TestRuntimeDirFromSubdir(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got, want := RuntimeDir(sub), filepath.Join(root, ".nxy", "local"); got != want {
		t.Fatalf("RuntimeDir = %q want %q", got, want)
	}
}

func TestReadJSONLSkipsBroken(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.jsonl")
	writeFile(t, p, "{\"a\":1}\n   \n{broken\n\n[2]\n")
	rows := ReadJSONL(p)
	if len(rows) != 2 {
		t.Fatalf("rows = %v", rows)
	}
	if ReadJSONL(filepath.Join(t.TempDir(), "none")) != nil {
		t.Fatal("missing file must give no rows")
	}
}

func TestGitBranch(t *testing.T) {
	root := t.TempDir()
	if _, ok := GitBranch(root); ok {
		t.Fatal("no repo must be false")
	}
	writeFile(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/feat/x\n")
	sub := filepath.Join(root, "s")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if b, ok := GitBranch(sub); !ok || b != "feat/x" {
		t.Fatalf("branch = %q %v", b, ok)
	}
	writeFile(t, filepath.Join(root, ".git", "HEAD"), "0123456789abcdef\n")
	if b, ok := GitBranch(root); !ok || b != "0123456" {
		t.Fatalf("detached = %q %v", b, ok)
	}
	// worktree pointer file, relative target
	wt := t.TempDir()
	writeFile(t, filepath.Join(wt, ".git"), "gitdir: gd/wt1\n")
	writeFile(t, filepath.Join(wt, "gd", "wt1", "HEAD"), "ref: refs/heads/wt\n")
	if b, ok := GitBranch(wt); !ok || b != "wt" {
		t.Fatalf("worktree = %q %v", b, ok)
	}
}

func TestWriteAtomic(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.json")
	if err := WriteAtomic(p, []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(p, []byte("two")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "two" {
		t.Fatalf("got %q", b)
	}
	if e, _ := os.ReadDir(filepath.Dir(p)); len(e) != 1 {
		t.Fatalf("temp file left behind: %v", e)
	}
}
