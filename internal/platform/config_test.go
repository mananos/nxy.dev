package platform

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func noEnv(string) string { return "" }

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

func TestDefaultsMatchPluginConfig(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "nxy.config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var plugin map[string]any
	if err := json.Unmarshal(data, &plugin); err != nil {
		t.Fatal(err)
	}
	def := defaultFilterConfig()
	got := map[string]any{
		"modules": map[string]any{"filter": plugin["modules"].(map[string]any)["filter"]},
		"filter":  plugin["filter"],
	}
	// normalise through JSON so []any{} and number types compare alike
	a, _ := json.Marshal(def)
	b, _ := json.Marshal(got)
	if string(a) != string(b) {
		t.Fatalf("Go defaults differ from nxy.config.json\n go: %s\njson: %s", a, b)
	}
}
