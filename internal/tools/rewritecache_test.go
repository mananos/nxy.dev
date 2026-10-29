package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mananos/nxy.dev/internal/filter"
)

// rewriteEngine is an Engine with a temp rewrite cache and a fake clock; rtk answers from the stub table.
func rewriteEngine(t *testing.T) (*Engine, *time.Time, filter.EngineInfo, func(string) string) {
	t.Helper()
	stub := buildStub(t)
	_, getenv := isolate(t)
	now := time.Now()
	e := &Engine{RewriteCachePath: filepath.Join(t.TempDir(), "cache", "rewrite-v2.json"),
		Now: func() time.Time { return now }, Getenv: getenv}
	return e, &now, filter.EngineInfo{Engine: "rtk", RtkPath: stub, RtkVersion: "0.0.0-golden"}, getenv
}

func mustRewrite(t *testing.T, e *Engine, info filter.EngineInfo, cmd string, want filter.RewriteResult) {
	t.Helper()
	got, err := e.Rewrite(info, cmd)
	if err != nil || got != want {
		t.Fatalf("Rewrite(%q) = %+v, %v; want %+v", cmd, got, err, want)
	}
}

func TestRewriteCacheHit(t *testing.T) {
	e, _, info, _ := rewriteEngine(t)
	writeTable(t, map[string]map[string]any{
		"git status": {"code": 0, "stdout": "rtk git status"},
		"npm test":   {"code": 3, "stdout": "rtk npm run test"},
	})
	mustRewrite(t, e, info, "git status", filter.RewriteResult{Code: 0, Stdout: "rtk git status"})
	mustRewrite(t, e, info, "npm test", filter.RewriteResult{Code: 3, Stdout: "rtk npm run test"})
	mustRewrite(t, e, info, "echo hi", filter.RewriteResult{Code: 1})

	// rtk now answers differently: the cached answers win, per exact command
	writeTable(t, map[string]map[string]any{
		"git status":  {"code": 0, "stdout": "changed"},
		"git status ": {"code": 0, "stdout": "other spelling"},
		"echo hi":     {"code": 0, "stdout": "changed"},
	})
	mustRewrite(t, e, info, "git status", filter.RewriteResult{Code: 0, Stdout: "rtk git status"})
	mustRewrite(t, e, info, "npm test", filter.RewriteResult{Code: 3, Stdout: "rtk npm run test"})
	mustRewrite(t, e, info, "echo hi", filter.RewriteResult{Code: 1})
	mustRewrite(t, e, info, "git status ", filter.RewriteResult{Code: 0, Stdout: "other spelling"})

	// NXY_REWRITE_CACHE=0 always asks rtk
	e.Getenv = withEnv(e.Getenv, map[string]string{"NXY_REWRITE_CACHE": "0"})
	mustRewrite(t, e, info, "git status", filter.RewriteResult{Code: 0, Stdout: "changed"})
}

func TestRewriteCacheInvalidation(t *testing.T) {
	e, now, info, getenv := rewriteEngine(t)
	writeTable(t, map[string]map[string]any{"git status": {"code": 0, "stdout": "v1"}})
	mustRewrite(t, e, info, "git status", filter.RewriteResult{Code: 0, Stdout: "v1"})
	writeTable(t, map[string]map[string]any{"git status": {"code": 0, "stdout": "v2"}})

	*now = now.Add(7*24*time.Hour - time.Minute)
	mustRewrite(t, e, info, "git status", filter.RewriteResult{Code: 0, Stdout: "v1"})
	*now = now.Add(2 * time.Minute)
	mustRewrite(t, e, info, "git status", filter.RewriteResult{Code: 0, Stdout: "v2"})

	writeTable(t, map[string]map[string]any{"git status": {"code": 0, "stdout": "v3"}})
	info.RtkVersion = "0.0.1"
	mustRewrite(t, e, info, "git status", filter.RewriteResult{Code: 0, Stdout: "v3"})

	writeTable(t, map[string]map[string]any{"git status": {"code": 1}})
	cfg := rtkConfigPath(runtime.GOOS, getenv)
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte("[hooks]\nexclude_commands = [\"git\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRewrite(t, e, info, "git status", filter.RewriteResult{Code: 1})
}

func TestRewriteCacheSkipsErrors(t *testing.T) {
	e, _, info, _ := rewriteEngine(t)
	writeTable(t, map[string]map[string]any{"a": {"code": 0, "stdout": "x"}})
	old := rewriteTimeout
	rewriteTimeout = time.Nanosecond
	_, err := e.Rewrite(info, "a")
	rewriteTimeout = old
	if err == nil {
		t.Fatal("timeout must be an error")
	}
	mustRewrite(t, e, info, "a", filter.RewriteResult{Code: 0, Stdout: "x"})

	missing := info
	missing.RtkPath = filepath.Join(t.TempDir(), "missing")
	if _, err := e.Rewrite(missing, "a"); err == nil {
		t.Fatal("missing binary must be an error")
	}
}

func TestRewriteCacheEviction(t *testing.T) {
	e, now, info, _ := rewriteEngine(t)
	old := rewriteCacheMax
	rewriteCacheMax = 2
	defer func() { rewriteCacheMax = old }()
	writeTable(t, map[string]map[string]any{})
	for _, c := range []string{"a", "b", "c"} {
		*now = now.Add(time.Second)
		mustRewrite(t, e, info, c, filter.RewriteResult{Code: 1})
	}
	data, err := os.ReadFile(e.RewriteCachePath)
	if err != nil {
		t.Fatal(err)
	}
	var f rewriteCacheFile
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.Entries["a"]; ok || len(f.Entries) != 2 {
		t.Fatalf("oldest entry must be evicted: %v", f.Entries)
	}
	tmps, _ := filepath.Glob(filepath.Join(filepath.Dir(e.RewriteCachePath), "*.tmp"))
	if len(tmps) != 0 {
		t.Fatalf("temp files left: %v", tmps)
	}
}

func TestRewriteCacheBrokenFile(t *testing.T) {
	e, _, info, _ := rewriteEngine(t)
	if err := os.MkdirAll(filepath.Dir(e.RewriteCachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.RewriteCachePath, []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTable(t, map[string]map[string]any{"a": {"code": 0, "stdout": "x"}})
	mustRewrite(t, e, info, "a", filter.RewriteResult{Code: 0, Stdout: "x"})
	data, _ := os.ReadFile(e.RewriteCachePath)
	if !strings.Contains(string(data), `"a"`) {
		t.Fatalf("broken file must be replaced: %s", data)
	}
}
