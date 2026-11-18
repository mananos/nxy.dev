package tools

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/mananos/nxy.dev/internal/filter"
)

var (
	stubOnce sync.Once
	stubPath string
	stubErr  error
	stubDir  string
)

// buildStub builds internal/goldentest/rtkstub once per test run.
func buildStub(t *testing.T) string {
	t.Helper()
	stubOnce.Do(func() {
		dir, err := os.MkdirTemp("", "rtkstub")
		if err != nil {
			stubErr = err
			return
		}
		stubDir = dir
		name := "rtk"
		if runtime.GOOS == "windows" {
			name = "rtk.exe"
		}
		stubPath = filepath.Join(dir, name)
		out, err := exec.Command("go", "build", "-o", stubPath, "github.com/mananos/nxy.dev/internal/goldentest/rtkstub").CombinedOutput()
		if err != nil {
			stubErr = &buildError{err, string(out)}
		}
	})
	if stubErr != nil {
		t.Fatal(stubErr)
	}
	return stubPath
}

type buildError struct {
	err error
	out string
}

func (b *buildError) Error() string { return b.err.Error() + ": " + b.out }

func TestMain(m *testing.M) {
	code := m.Run()
	if stubDir != "" {
		os.RemoveAll(stubDir)
	}
	os.Exit(code)
}

// isolate keeps the real rtk and the real user dirs out: empty PATH, temp home.
func isolate(t *testing.T) (home string, getenv func(string) string) {
	t.Helper()
	home = t.TempDir()
	empty := t.TempDir()
	t.Setenv("PATH", empty)
	t.Setenv("Path", empty)
	env := map[string]string{
		"HOME": home, "USERPROFILE": home, "LOCALAPPDATA": filepath.Join(home, "AppData", "Local"),
		"XDG_DATA_HOME": filepath.Join(home, "xdg"),
	}
	return home, func(k string) string { return env[k] }
}

func withEnv(base func(string) string, extra map[string]string) func(string) string {
	return func(k string) string {
		if v, ok := extra[k]; ok {
			return v
		}
		return base(k)
	}
}

func writeTable(t *testing.T, table map[string]map[string]any) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "table.json")
	data, _ := json.Marshal(table)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NXY_GOLDEN_RTK_TABLE", p)
}

func TestFindRtkEnvPathWins(t *testing.T) {
	stub := buildStub(t)
	_, getenv := isolate(t)
	path, version, ok := FindRtk(withEnv(getenv, map[string]string{"NXY_RTK_PATH": stub}))
	if !ok || path != stub || version != "0.0.0-golden" {
		t.Fatalf("FindRtk = %q %q %v", path, version, ok)
	}
}

func TestFindRtkNone(t *testing.T) {
	_, getenv := isolate(t)
	if p, _, ok := FindRtk(withEnv(getenv, map[string]string{"NXY_RTK_PATH": "/nope/rtk"})); ok {
		t.Fatalf("found %q", p)
	}
}

func TestFindRtkKnownLocation(t *testing.T) {
	stub := buildStub(t)
	home, getenv := isolate(t)
	var dir string
	if runtime.GOOS == "windows" {
		dir = filepath.Join(home, ".cargo", "bin")
	} else {
		dir = filepath.Join(home, ".local", "bin")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(stub)
	dst := filepath.Join(dir, filepath.Base(stub))
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		t.Fatal(err)
	}
	path, _, ok := FindRtk(getenv)
	if !ok || path != filepath.ToSlash(dst) {
		t.Fatalf("FindRtk = %q %v, want %q", path, ok, filepath.ToSlash(dst))
	}
}

func TestKnownRtkLocations(t *testing.T) {
	get := func(string) string { return "" }
	got := KnownRtkLocations("linux", "/h", get)
	want := []string{filepath.Join("/h", ".local", "bin", "rtk"), filepath.Join("/h", ".cargo", "bin", "rtk"),
		"/usr/local/bin/rtk", "/opt/homebrew/bin/rtk", "/home/linuxbrew/.linuxbrew/bin/rtk"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("linux: %v", got)
	}

	home := t.TempDir()
	local := filepath.Join(home, "AppData", "Local")
	pkg := filepath.Join(local, "Microsoft", "WinGet", "Packages", "rtk-ai.rtk_x")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(local, "Microsoft", "WinGet", "Packages", "other.pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	got = KnownRtkLocations("windows", home, get)
	want = []string{
		filepath.Join(local, "Microsoft", "WinGet", "Links", "rtk.exe"),
		filepath.Join(pkg, "rtk.exe"),
		filepath.Join(home, ".local", "bin", "rtk.exe"),
		filepath.Join(home, ".cargo", "bin", "rtk.exe"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("windows: %v want %v", got, want)
	}
}

func TestRewrite(t *testing.T) {
	stub := buildStub(t)
	writeTable(t, map[string]map[string]any{
		"git status": {"code": 0, "stdout": "rtk git status"},
		"npm test":   {"code": 3, "stdout": "rtk npm run test"},
	})
	for _, c := range []struct {
		cmd  string
		want filter.RewriteResult
	}{
		{"git status", filter.RewriteResult{Code: 0, Stdout: "rtk git status"}},
		{"npm test", filter.RewriteResult{Code: 3, Stdout: "rtk npm run test"}},
		{"echo hi", filter.RewriteResult{Code: 1}},
	} {
		got, err := Rewrite(stub, c.cmd)
		if err != nil || got != c.want {
			t.Errorf("Rewrite(%q) = %+v, %v; want %+v", c.cmd, got, err, c.want)
		}
	}
}

func TestRewriteErrors(t *testing.T) {
	if _, err := Rewrite(filepath.Join(t.TempDir(), "missing"), "x"); err == nil {
		t.Fatal("missing binary must be an error")
	}
}

func TestRewriteTimeout(t *testing.T) {
	stub := buildStub(t)
	writeTable(t, map[string]map[string]any{"a": {"code": 0, "stdout": "x"}})
	old := rewriteTimeout
	rewriteTimeout = time.Nanosecond
	defer func() { rewriteTimeout = old }()
	if _, err := Rewrite(stub, "a"); err == nil {
		t.Fatal("timeout must be an error")
	}
}

func TestEngineCache(t *testing.T) {
	stub := buildStub(t)
	_, getenv := isolate(t)
	now := time.Now()
	e := Engine{CachePath: filepath.Join(t.TempDir(), "cache", "engine-v2.json"), Now: func() time.Time { return now },
		Getenv: withEnv(getenv, map[string]string{"NXY_RTK_PATH": stub})}
	calls := 0
	hook := func() bool { calls++; return true }

	info := e.Info(filter.Settings{Engine: "auto"}, hook)
	if info.Engine != "rtk" || info.Reason != "rtk-found" || info.RtkPath != stub || info.RtkVersion != "0.0.0-golden" || !info.RtkHookDetected || calls != 1 {
		t.Fatalf("resolve: %+v calls=%d", info, calls)
	}
	now = now.Add(5*time.Hour + 59*time.Minute)
	if info = e.Info(filter.Settings{Engine: "auto"}, hook); calls != 1 || info.Engine != "rtk" {
		t.Fatalf("cache hit must skip rtkHook: %+v calls=%d", info, calls)
	}
	now = now.Add(2 * time.Minute)
	e.Info(filter.Settings{Engine: "auto"}, hook)
	if calls != 2 {
		t.Fatalf("6h entry must expire, calls=%d", calls)
	}
	// the 1.x cache file is never touched
	if _, err := os.Stat(filepath.Join(filepath.Dir(e.CachePath), "engine.json")); err == nil {
		t.Fatal("engine.json must not be written")
	}
}

func TestEngineNegativeAndReasons(t *testing.T) {
	_, getenv := isolate(t)
	now := time.Now()
	e := Engine{CachePath: filepath.Join(t.TempDir(), "e.json"), Now: func() time.Time { return now },
		Getenv: withEnv(getenv, map[string]string{"NXY_RTK_PATH": "/nope/rtk"})}
	calls := 0
	hook := func() bool { calls++; return false }

	if info := e.Info(filter.Settings{Engine: "rtk"}, hook); info.Engine != "off" || info.Reason != "rtk-missing" {
		t.Fatalf("rtk-missing: %+v", info)
	}
	now = now.Add(time.Minute)
	e.Info(filter.Settings{Engine: "rtk"}, hook)
	if calls != 1 {
		t.Fatalf("negative entry fresh at 1 min, calls=%d", calls)
	}
	now = now.Add(61 * time.Second)
	if info := e.Info(filter.Settings{Engine: "rtk"}, hook); calls != 2 || info.Reason != "rtk-missing" {
		t.Fatalf("negative entry expires after 2 min: %+v calls=%d", info, calls)
	}
	if info := e.Info(filter.Settings{Engine: "auto"}, hook); calls != 3 || info.Reason != "auto-no-rtk" {
		t.Fatalf("auto without rtk: %+v calls=%d", info, calls)
	}

	e.CachePath = filepath.Join(t.TempDir(), "off.json")
	info := e.Info(filter.Settings{Engine: "off"}, hook)
	if info.Engine != "off" || info.Reason != "config" {
		t.Fatalf("config off: %+v", info)
	}
	now = now.Add(5 * time.Hour)
	before := calls
	e.Info(filter.Settings{Engine: "off"}, hook)
	if calls != before {
		t.Fatal("config-off entry is trusted for 6 h")
	}
}

// Unlike 1.x (engine.mjs:136-148), a cached verdict does not outlive a change of the engine
// setting (config or NXY_ENGINE) or of NXY_RTK_PATH.
func TestEngineCacheFollowsConfig(t *testing.T) {
	stub := buildStub(t)
	_, getenv := isolate(t)
	now := time.Now()
	calls := 0
	hook := func() bool { calls++; return false }

	e := Engine{CachePath: filepath.Join(t.TempDir(), "a.json"), Now: func() time.Time { return now },
		Getenv: withEnv(getenv, map[string]string{"NXY_RTK_PATH": stub})}
	if info := e.Info(filter.Settings{Engine: "auto"}, hook); info.Engine != "rtk" {
		t.Fatalf("seed: %+v", info)
	}
	if info := e.Info(filter.Settings{Engine: "off"}, hook); info.Engine != "off" || info.Reason != "config" || calls != 2 {
		t.Fatalf("engine off must win over cached rtk: %+v calls=%d", info, calls)
	}
	if info := e.Info(filter.Settings{Engine: "rtk"}, hook); info.Engine != "rtk" || calls != 3 {
		t.Fatalf("engine rtk must win over cached off: %+v calls=%d", info, calls)
	}
	if e.Info(filter.Settings{Engine: "rtk"}, hook); calls != 3 {
		t.Fatalf("same setting must hit the cache, calls=%d", calls)
	}

	e.Getenv = withEnv(getenv, map[string]string{"NXY_RTK_PATH": "/nope/rtk"})
	if info := e.Info(filter.Settings{Engine: "rtk"}, hook); info.Engine != "off" || info.Reason != "rtk-missing" || calls != 4 {
		t.Fatalf("a new NXY_RTK_PATH must resolve again: %+v calls=%d", info, calls)
	}

	// an entry written before these fields existed is stale
	old := filepath.Join(t.TempDir(), "old.json")
	data, _ := json.Marshal(map[string]any{"engine": "off", "reason": "config", "resolvedAt": now.UnixMilli()})
	if err := os.WriteFile(old, data, 0o644); err != nil {
		t.Fatal(err)
	}
	e = Engine{CachePath: old, Now: func() time.Time { return now }, Getenv: withEnv(getenv, map[string]string{"NXY_RTK_PATH": stub})}
	if info := e.Info(filter.Settings{Engine: "auto"}, hook); info.Engine != "rtk" {
		t.Fatalf("old entry must be re-resolved: %+v", info)
	}
}

func TestSilenceHookWarning(t *testing.T) {
	home, getenv := isolate(t)
	_ = home
	marker := warnMarkerPath(runtime.GOOS, getenv)
	now := time.Now()
	if !SilenceHookWarning(getenv, now) {
		t.Fatal("missing marker must be touched")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal(err)
	}
	if SilenceHookWarning(getenv, now.Add(time.Hour)) {
		t.Fatal("fresh marker must not be touched")
	}
	if !SilenceHookWarning(getenv, now.Add(24*time.Hour)) {
		t.Fatal("marker older than 23 h must be touched")
	}
}

func TestWarnMarkerPaths(t *testing.T) {
	get := func(k string) string {
		return map[string]string{"HOME": "/h", "LOCALAPPDATA": "/la", "XDG_DATA_HOME": "/xdg"}[k]
	}
	cases := map[string]string{
		"windows": filepath.Join("/la", "rtk", ".hook_warn_last"),
		"linux":   filepath.Join("/xdg", "rtk", ".hook_warn_last"),
		"darwin":  filepath.Join("/h", "Library", "Application Support", "rtk", ".hook_warn_last"),
	}
	for goos, want := range cases {
		if got := warnMarkerPath(goos, get); got != want {
			t.Errorf("%s: %q want %q", goos, got, want)
		}
	}
}
