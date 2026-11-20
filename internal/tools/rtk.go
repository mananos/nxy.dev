package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/filter"
	"github.com/mananos/nxy.dev/v2/internal/platform"
)

// rewriteTimeout is a var so a test can force the timeout path.
var rewriteTimeout = 2 * time.Second

const (
	versionTimeout = 3 * time.Second
	warnWindow     = 23 * time.Hour
	positiveTTL    = 6 * time.Hour
	negativeTTL    = 2 * time.Minute
)

var rtkVersionPrefix = regexp.MustCompile(`(?i)^rtk\s+`)

func homeOf(getenv func(string) string) string {
	if h := getenv("HOME"); h != "" {
		return h
	}
	if h := getenv("USERPROFILE"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

// KnownRtkLocations are the well-known install locations, for processes whose PATH predates the install.
func KnownRtkLocations(goos, home string, getenv func(string) string) []string {
	var out []string
	if goos == "windows" {
		local := getenv("LOCALAPPDATA")
		if local == "" {
			local = filepath.Join(home, "AppData", "Local")
		}
		out = append(out, filepath.Join(local, "Microsoft", "WinGet", "Links", "rtk.exe"))
		pkgs := filepath.Join(local, "Microsoft", "WinGet", "Packages")
		if entries, err := os.ReadDir(pkgs); err == nil {
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), "rtk-ai.rtk") {
					out = append(out, filepath.Join(pkgs, e.Name(), "rtk.exe"))
				}
			}
		}
		out = append(out, filepath.Join(home, ".local", "bin", "rtk.exe"), filepath.Join(home, ".cargo", "bin", "rtk.exe"))
		return out
	}
	return append(out,
		filepath.Join(home, ".local", "bin", "rtk"),
		filepath.Join(home, ".cargo", "bin", "rtk"),
		"/usr/local/bin/rtk", "/opt/homebrew/bin/rtk", "/home/linuxbrew/.linuxbrew/bin/rtk")
}

// tryVersion runs `bin --version`; ok only on exit 0.
func tryVersion(bin string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), versionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--version")
	hideWindow(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if cmd.Run() != nil {
		return "", false
	}
	out := stdout.String()
	if out == "" {
		out = stderr.String()
	}
	return strings.TrimSpace(out), true
}

// FindRtk locates rtk: NXY_RTK_PATH, then `rtk` on PATH, then `rtk.exe` (Windows), then known install locations.
func FindRtk(getenv func(string) string) (path, version string, ok bool) {
	var candidates []string
	if p := getenv("NXY_RTK_PATH"); p != "" {
		candidates = append(candidates, p)
	}
	candidates = append(candidates, "rtk")
	if runtime.GOOS == "windows" {
		candidates = append(candidates, "rtk.exe")
	}
	for _, bin := range candidates {
		if v, found := tryVersion(bin); found {
			return bin, rtkVersionPrefix.ReplaceAllString(v, ""), true
		}
	}
	for _, bin := range KnownRtkLocations(runtime.GOOS, homeOf(getenv), getenv) {
		if _, err := os.Stat(bin); err != nil {
			continue
		}
		if v, found := tryVersion(bin); found {
			return strings.ReplaceAll(bin, `\`, "/"), rtkVersionPrefix.ReplaceAllString(v, ""), true
		}
	}
	return "", "", false
}

// Rewrite asks rtk for the filtered equivalent of cmd (argv, no shell). Exit codes: 0 rewritten+allow,
// 1 no equivalent, 2 deny, 3 rewritten but ask. A failure to run or a timeout is an error.
func Rewrite(rtkPath, cmd string) (filter.RewriteResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), rewriteTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, rtkPath, "rewrite", cmd)
	hideWindow(c)
	var stdout bytes.Buffer
	c.Stdout = &stdout
	err := c.Run()
	if ctx.Err() != nil {
		return filter.RewriteResult{}, ctx.Err()
	}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return filter.RewriteResult{Code: ee.ExitCode(), Stdout: stdout.String()}, nil
		}
		return filter.RewriteResult{}, err
	}
	return filter.RewriteResult{Code: 0, Stdout: stdout.String()}, nil
}

// gainTimeout bounds `rtk gain`.
const gainTimeout = 10 * time.Second

// Gain runs `rtk gain <args> --format json` in cwd and decodes the answer. False on a failure to
// run, a timeout, a non-zero exit, empty stdout or bad JSON (rtk.mjs rtkGain).
func Gain(rtkPath string, args []string, cwd string) (any, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), gainTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, rtkPath, append(append([]string{"gain"}, args...), "--format", "json")...)
	c.Dir = cwd
	hideWindow(c)
	var stdout bytes.Buffer
	c.Stdout = &stdout
	if c.Run() != nil || stdout.Len() == 0 {
		return nil, false
	}
	var v any
	if json.Unmarshal(stdout.Bytes(), &v) != nil {
		return nil, false
	}
	return v, true
}

// warnMarkerPath is rtk's `.hook_warn_last` under the per-OS data dir.
func warnMarkerPath(goos string, getenv func(string) string) string {
	home := homeOf(getenv)
	var data string
	switch goos {
	case "windows":
		data = getenv("LOCALAPPDATA")
		if data == "" {
			data = filepath.Join(home, "AppData", "Local")
		}
	case "darwin":
		data = filepath.Join(home, "Library", "Application Support")
	default:
		data = getenv("XDG_DATA_HOME")
		if data == "" {
			data = filepath.Join(home, ".local", "share")
		}
	}
	return filepath.Join(data, "rtk", ".hook_warn_last")
}

// SilenceHookWarning starts rtk's once-a-day warning window ourselves: touches the marker when it is
// missing or older than 23 h. Best effort; true when the marker was touched.
func SilenceHookWarning(getenv func(string) string, now time.Time) bool {
	p := warnMarkerPath(runtime.GOOS, getenv)
	st, err := os.Stat(p)
	if err == nil && now.Sub(st.ModTime()) < warnWindow {
		return false
	}
	if err != nil && !os.IsNotExist(err) {
		return false
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return false
	}
	if err != nil {
		if os.WriteFile(p, nil, 0o644) != nil {
			return false
		}
	}
	return os.Chtimes(p, now, now) == nil
}

// Engine resolves which tool rewrites commands, with a cache file separate from the 1.x one.
type Engine struct {
	CachePath        string // default <UserDir>/cache/engine-v2.json
	RewriteCachePath string // default <UserDir>/cache/rewrite-v2.json
	Now              func() time.Time
	Getenv           func(string) string
}

type engineCache struct {
	Engine          string `json:"engine"`
	RtkPath         string `json:"rtkPath"`
	RtkVersion      string `json:"rtkVersion"`
	RtkHookDetected bool   `json:"rtkHookDetected"`
	Reason          string `json:"reason"`
	ResolvedAt      int64  `json:"resolvedAt"`
	// What the verdict was resolved for: a change of either makes the entry stale (unlike 1.x,
	// whose cached verdict beat a changed config or env for up to 6 h).
	ConfigEngine string `json:"configEngine"`
	RtkPathEnv   string `json:"rtkPathEnv"`
}

func (e Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e Engine) getenv(k string) string {
	if e.Getenv != nil {
		return e.Getenv(k)
	}
	return os.Getenv(k)
}

func (e Engine) cachePath() string {
	if e.CachePath != "" {
		return e.CachePath
	}
	u, err := platform.UserDir()
	if err != nil {
		return ""
	}
	return filepath.Join(u, "cache", "engine-v2.json")
}

func (e Engine) readCache(path string, s filter.Settings) (filter.EngineInfo, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return filter.EngineInfo{}, false
	}
	var c engineCache
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &c) != nil || json.Unmarshal(data, &raw) != nil {
		return filter.EngineInfo{}, false
	}
	if _, ok := raw["resolvedAt"]; !ok {
		return filter.EngineInfo{}, false
	}
	if c.ConfigEngine != s.Engine || c.RtkPathEnv != e.getenv("NXY_RTK_PATH") {
		return filter.EngineInfo{}, false
	}
	ttl := negativeTTL
	if c.Engine == "rtk" || c.Reason == "config" {
		ttl = positiveTTL
	}
	if e.now().UnixMilli()-c.ResolvedAt > ttl.Milliseconds() {
		return filter.EngineInfo{}, false
	}
	return filter.EngineInfo{Engine: c.Engine, RtkPath: c.RtkPath, RtkVersion: c.RtkVersion,
		Reason: c.Reason, RtkHookDetected: c.RtkHookDetected, ResolvedAt: c.ResolvedAt}, true
}

// Info returns the cached engine if still fresh and resolved for the same engine setting and
// NXY_RTK_PATH, else resolves it (calling rtkHook once) and refreshes the cache.
func (e Engine) Info(s filter.Settings, rtkHook func() bool) filter.EngineInfo {
	path := e.cachePath()
	info, ok := filter.EngineInfo{}, false
	if path != "" {
		info, ok = e.readCache(path, s)
	}
	if !ok {
		info = e.resolve(s, rtkHook)
		if path != "" {
			writeFileAtomic(path, engineCache{info.Engine, info.RtkPath, info.RtkVersion, info.RtkHookDetected,
				info.Reason, info.ResolvedAt, s.Engine, e.getenv("NXY_RTK_PATH")})
		}
	}
	if info.Engine == "rtk" {
		SilenceHookWarning(e.getenv, e.now())
	}
	return info
}

// Refresh resolves the engine now and rewrites the cache, so a change (an install, a config edit)
// shows at once instead of when the cached verdict expires.
func (e Engine) Refresh(s filter.Settings, rtkHook func() bool) filter.EngineInfo {
	info := e.resolve(s, rtkHook)
	if path := e.cachePath(); path != "" {
		writeFileAtomic(path, engineCache{info.Engine, info.RtkPath, info.RtkVersion, info.RtkHookDetected,
			info.Reason, info.ResolvedAt, s.Engine, e.getenv("NXY_RTK_PATH")})
	}
	return info
}

func (e Engine) resolve(s filter.Settings, rtkHook func() bool) filter.EngineInfo {
	path, version, found := FindRtk(e.getenv)
	hook := false
	if rtkHook != nil {
		hook = rtkHook()
	}
	info := filter.EngineInfo{RtkPath: path, RtkVersion: version, RtkHookDetected: hook, ResolvedAt: e.now().UnixMilli()}
	switch {
	case s.Engine == "off":
		info.Engine, info.Reason = "off", "config"
	case !found:
		info.Engine, info.Reason = "off", "auto-no-rtk"
		if s.Engine == "rtk" {
			info.Reason = "rtk-missing"
		}
	default:
		info.Engine, info.Reason = "rtk", "rtk-found"
	}
	return info
}
