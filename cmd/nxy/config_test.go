package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/configedit"
)

func TestConfigShowAndSet(t *testing.T) {
	isolate(t)
	t.Setenv("NXY_FILTER", "")
	t.Setenv("NXY_ENGINE", "")
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	userCfg := filepath.Join(os.Getenv("NXY_HOME"), "config.json")
	repoCfg := filepath.Join(repo, ".nxy", "config.json")

	code, out, _ := runCmd(t, "", "config", "--cwd", repo)
	if code != 0 || !strings.Contains(out, "modules.filter") || !strings.Contains(out, "(predeterminado)") ||
		!strings.Contains(out, "roles.reviewer-quick.model") || !strings.Contains(out, "no existe") ||
		!strings.Contains(out, "nxy config set <clave>") {
		t.Fatalf("show (%d):\n%s", code, out)
	}

	// default scope is the user's file
	code, out, _ = runCmd(t, "", "config", "set", "roles.planner.model", "opus", "--cwd", repo)
	if code != 0 || !strings.Contains(out, "roles.planner.model = opus en ") || !strings.Contains(out, " bytes)") {
		t.Fatalf("set (%d):\n%s", code, out)
	}
	if _, err := os.Stat(repoCfg); err == nil {
		t.Error("the repo file was written without --repo")
	}
	_, out, _ = runCmd(t, "", "config", "show", "--cwd", repo)
	if !strings.Contains(out, "opus") || !strings.Contains(out, "(usuario)") {
		t.Errorf("show after set:\n%s", out)
	}

	// --repo wins and keeps the other keys of the file
	if err := os.MkdirAll(filepath.Dir(repoCfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(repoCfg, []byte("{\n  \"zeta\": 1,\n  \"modules\": {\n    \"metrics\": false\n  }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ = runCmd(t, "", "config", "set", "modules.filter", "false", "--repo", "--cwd", repo)
	if code != 0 || !strings.Contains(out, "modules.filter = false en ") {
		t.Fatalf("set --repo (%d):\n%s", code, out)
	}
	got, _ := os.ReadFile(repoCfg)
	if s := string(got); !strings.Contains(s, `"zeta": 1`) || !strings.Contains(s, `"metrics": false`) || !strings.Contains(s, `"filter": false`) {
		t.Errorf("repo config:\n%s", s)
	}
	_, out, _ = runCmd(t, "", "config", "--cwd", repo)
	if !strings.Contains(out, "(repo)") {
		t.Errorf("source repo missing:\n%s", out)
	}

	// default removes the key and prunes what is left empty
	code, out, _ = runCmd(t, "", "config", "set", "roles.planner.model", "default", "--cwd", repo)
	if code != 0 || !strings.Contains(out, "roles.planner.model vuelve al predeterminado en ") {
		t.Fatalf("default (%d):\n%s", code, out)
	}
	got, _ = os.ReadFile(userCfg)
	if strings.Contains(string(got), "roles") {
		t.Errorf("user config:\n%s", got)
	}

	// invalid keys and values write nothing
	for _, c := range [][]string{{"nope", "x"}, {"roles.planner.model", "gpt"}, {"modules.filter", "3"}} {
		before, _ := os.ReadFile(userCfg)
		code, out, _ = runCmd(t, "", append([]string{"config", "set"}, append(c, "--cwd", repo)...)...)
		after, _ := os.ReadFile(userCfg)
		if code != 1 || !strings.Contains(out, "campo desconocido") && !strings.Contains(out, "valor inválido") || string(before) != string(after) {
			t.Errorf("%v (%d): %s", c, code, out)
		}
	}
	if code, _, _ := runCmd(t, "", "config", "set", "modules.filter"); code != 1 {
		t.Errorf("set without value: %d", code)
	}
}

// `config set` writes through JSONDoc (to keep the key order) but must land where configedit.SetPath,
// the tested and golden logic, lands: set, overwrite and delete with prune.
func TestConfigSetMatchesConfigeditSetPath(t *testing.T) {
	isolate(t)
	userCfg := filepath.Join(os.Getenv("NXY_HOME"), "config.json")
	if err := os.WriteFile(userCfg, []byte("{\n  \"zeta\": 1,\n  \"roles\": {\n    \"planner\": {\n      \"model\": \"opus\"\n    }\n  }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	decode := func() map[string]any {
		b, err := os.ReadFile(userCfg)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	steps := []struct {
		key, raw string
		value    any
	}{
		{"roles.planner.effort", "high", "high"},
		{"roles.planner.model", "sonnet", "sonnet"},
		{"modules.filter", "false", false},
		{"roles.planner.model", "default", nil},
		{"roles.planner.effort", "default", nil},
		{"modules.filter", "default", nil},
		{"roles.scout.model", "default", nil},
	}
	for _, s := range steps {
		f, ok := configedit.Lookup(s.key)
		if !ok {
			t.Fatalf("%s is not a field", s.key)
		}
		want := configedit.SetPath(decode(), f.Path, s.value)
		if code, out, _ := runCmd(t, "", "config", "set", s.key, s.raw, "--cwd", t.TempDir()); code != 0 {
			t.Fatalf("%s %s (%d): %s", s.key, s.raw, code, out)
		}
		if got := decode(); !reflect.DeepEqual(got, want) {
			t.Errorf("%s %s: wrote %v, SetPath gives %v", s.key, s.raw, got, want)
		}
	}
	if got := decode(); !reflect.DeepEqual(got, map[string]any{"zeta": float64(1)}) {
		t.Errorf("final = %v", got)
	}
}

func TestConfigSetBrokenFileUntouched(t *testing.T) {
	isolate(t)
	userCfg := filepath.Join(os.Getenv("NXY_HOME"), "config.json")
	if err := os.WriteFile(userCfg, []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runCmd(t, "", "config", "set", "filter.engine", "rtk", "--cwd", t.TempDir())
	if code != 1 || !strings.Contains(out, "está roto; no se toca") {
		t.Fatalf("(%d): %s", code, out)
	}
	if b, _ := os.ReadFile(userCfg); string(b) != "{ not json" {
		t.Errorf("file changed: %s", b)
	}
	_, out, _ = runCmd(t, "", "config", "--cwd", t.TempDir())
	if !strings.Contains(out, "(roto)") {
		t.Errorf("show:\n%s", out)
	}
}

func TestConfigCacheTTL(t *testing.T) {
	isolate(t)
	settings := filepath.Join(os.Getenv("HOME"), ".claude", "settings.json")

	code, out, _ := runCmd(t, "", "config", "cache-ttl", "1h")
	if code != 0 || !strings.Contains(out, "promptCacheTtl = 1h en ") || !strings.Contains(out, "backup: ninguno") ||
		!strings.Contains(out, "Claude Code lo toma en la próxima sesión.") {
		t.Fatalf("1h, no file (%d):\n%s", code, out)
	}

	if err := os.WriteFile(settings, []byte("{\n  \"zeta\": 1,\n  \"alpha\": {\"x\": true}\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ = runCmd(t, "", "config", "cache-ttl", "1h")
	got, _ := os.ReadFile(settings)
	s := string(got)
	if code != 0 || !strings.Contains(out, "backup: ") || !strings.Contains(out, ".bak-nxy-") || !strings.Contains(out, "(se conservan 1)") ||
		strings.Index(s, `"zeta"`) > strings.Index(s, `"alpha"`) || !strings.Contains(s, `"promptCacheTtl": "1h"`) {
		t.Fatalf("1h (%d):\n%s\n%s", code, out, s)
	}

	for i := 0; i < 5; i++ {
		if code, _, _ := runCmd(t, "", "config", "cache-ttl", "off"); code != 0 {
			t.Fatal("off failed")
		}
	}
	got, _ = os.ReadFile(settings)
	if strings.Contains(string(got), "promptCacheTtl") || !strings.Contains(string(got), `"zeta": 1`) {
		t.Errorf("after off:\n%s", got)
	}
	baks, _ := filepath.Glob(settings + ".bak-nxy-*")
	if len(baks) != 3 {
		t.Errorf("backups kept = %d, want 3", len(baks))
	}

	if code, out, _ := runCmd(t, "", "config", "cache-ttl", "2h"); code != 1 || !strings.Contains(out, "uso: cache-ttl 1h|off") {
		t.Errorf("bad arg (%d): %s", code, out)
	}
	if err := os.WriteFile(settings, []byte("[1]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := runCmd(t, "", "config", "cache-ttl", "off"); code != 1 || !strings.Contains(out, "está roto; no se toca") {
		t.Errorf("broken (%d): %s", code, out)
	}
}
