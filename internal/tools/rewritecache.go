package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"

	"github.com/mananos/nxy.dev/v2/internal/filter"
	"github.com/mananos/nxy.dev/v2/internal/platform"
)

// rewriteCacheMax is a var so a test can exercise eviction with few entries.
var rewriteCacheMax = 500

const rewriteCacheTTL = 7 * 24 * 60 * 60 * 1000 // ms

// rewriteCacheFile holds rtk's answers for one rtk install: a different binary, version or rtk
// config (its [hooks] section changes what gets rewritten) throws every entry away.
type rewriteCacheFile struct {
	RtkPath    string                  `json:"rtkPath"`
	RtkVersion string                  `json:"rtkVersion"`
	RtkConfig  string                  `json:"rtkConfig"`
	Entries    map[string]rewriteEntry `json:"entries"`
}

type rewriteEntry struct {
	Code   int    `json:"code"`
	Stdout string `json:"stdout"`
	At     int64  `json:"at"`
}

// rtkConfigPath is rtk's config.toml under the per-OS config dir (what `rtk config` prints).
func rtkConfigPath(goos string, getenv func(string) string) string {
	home := homeOf(getenv)
	var dir string
	switch goos {
	case "windows":
		dir = getenv("APPDATA")
		if dir == "" {
			dir = filepath.Join(home, "AppData", "Roaming")
		}
	case "darwin":
		dir = filepath.Join(home, "Library", "Application Support")
	default:
		dir = getenv("XDG_CONFIG_HOME")
		if dir == "" {
			dir = filepath.Join(home, ".config")
		}
	}
	return filepath.Join(dir, "rtk", "config.toml")
}

// rtkConfigStamp changes whenever rtk's config file is created, edited or removed.
func rtkConfigStamp(getenv func(string) string) string {
	st, err := os.Stat(rtkConfigPath(runtime.GOOS, getenv))
	if err != nil {
		return "absent"
	}
	return fmt.Sprintf("%d:%d", st.ModTime().UnixNano(), st.Size())
}

func (e Engine) rewriteCachePath() string {
	if e.RewriteCachePath != "" {
		return e.RewriteCachePath
	}
	u, err := platform.UserDir()
	if err != nil {
		return ""
	}
	return filepath.Join(u, "cache", "rewrite-v2.json")
}

// Rewrite asks rtk (info.RtkPath) for cmd, remembering its answer per exact command for 7 days.
// Only real answers (exit 0-3) are kept; errors and timeouts always go back to rtk.
// NXY_REWRITE_CACHE=0 skips the cache (bench, debugging).
func (e Engine) Rewrite(info filter.EngineInfo, cmd string) (filter.RewriteResult, error) {
	path := e.rewriteCachePath()
	if path == "" || e.getenv("NXY_REWRITE_CACHE") == "0" {
		return Rewrite(info.RtkPath, cmd)
	}
	now := e.now().UnixMilli()
	stamp := rtkConfigStamp(e.getenv)
	f := rewriteCacheFile{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &f)
	}
	if f.RtkPath != info.RtkPath || f.RtkVersion != info.RtkVersion || f.RtkConfig != stamp {
		f = rewriteCacheFile{RtkPath: info.RtkPath, RtkVersion: info.RtkVersion, RtkConfig: stamp}
	}
	if en, ok := f.Entries[cmd]; ok && now-en.At <= rewriteCacheTTL {
		return filter.RewriteResult{Code: en.Code, Stdout: en.Stdout}, nil
	}
	res, err := Rewrite(info.RtkPath, cmd)
	if err != nil || res.Code < 0 || res.Code > 3 {
		return res, err
	}
	if f.Entries == nil {
		f.Entries = map[string]rewriteEntry{}
	}
	f.Entries[cmd] = rewriteEntry{Code: res.Code, Stdout: res.Stdout, At: now}
	pruneRewriteCache(f.Entries, now)
	writeFileAtomic(path, f)
	return res, nil
}

// pruneRewriteCache drops expired entries, then the oldest ones beyond rewriteCacheMax.
func pruneRewriteCache(entries map[string]rewriteEntry, now int64) {
	for k, en := range entries {
		if now-en.At > rewriteCacheTTL {
			delete(entries, k)
		}
	}
	if len(entries) <= rewriteCacheMax {
		return
	}
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return entries[keys[i]].At < entries[keys[j]].At })
	for _, k := range keys[:len(keys)-rewriteCacheMax] {
		delete(entries, k)
	}
}

// writeFileAtomic writes v as JSON through a temp file and a rename, so a hook running in
// parallel reads either the old file or the new one. Best effort: a lost write is a later miss.
func writeFileAtomic(path string, v any) {
	data, err := json.Marshal(v)
	if err != nil || os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Rename(tmp.Name(), path) != nil {
		_ = os.Remove(tmp.Name())
	}
}
