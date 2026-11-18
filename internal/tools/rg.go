package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/jscompat"
	"github.com/mananos/nxy.dev/v2/internal/platform"
)

// RgRow is one line rg found: Path with forward slashes and no leading "./".
type RgRow struct {
	Path string
	Line int
	Text string // trimmed, at most 200 UTF-16 units
}

// RgOpts narrows RgSearch. Zero values mean the 1.x defaults: 30 rows, 3 per file, literal, 10 s.
type RgOpts struct {
	// Bin is the rg to run; empty finds it with FindRg.
	Bin        string
	MaxRows    int
	MaxPerFile int
	// Regex searches the pattern as a regex instead of a literal string.
	Regex   bool
	Word    bool
	Globs   []string
	Timeout time.Duration
	Getenv  func(string) string // for FindRg; nil is os.Getenv
}

// RgResult is the answer of RgSearch. Reason is "ok", "rg-missing", "spawn-failed" or "exit-N".
type RgResult struct {
	OK        bool
	Reason    string
	Rows      []RgRow
	Truncated bool // the row cap bit: the query was too vague
}

var rgLine = regexp.MustCompile(`^([^\n\r\x{2028}\x{2029}]+?):(\d+):([^\n\r\x{2028}\x{2029}]*)$`)

// RgSearch is a capped search with rg (core/rg.mjs rgSearch). It is capped on purpose: an uncapped
// grep is the file dump the scout exists to avoid. It never panics; a failure is OK=false with a reason.
func RgSearch(ctx context.Context, cwd, pattern string, o RgOpts) RgResult {
	bin := o.Bin
	if bin == "" {
		getenv := o.Getenv
		if getenv == nil {
			getenv = os.Getenv
		}
		var ok bool
		if bin, ok = FindRg(getenv); !ok {
			return RgResult{Reason: "rg-missing"}
		}
	}
	maxRows, perFile, timeout := o.MaxRows, o.MaxPerFile, o.Timeout
	if maxRows <= 0 {
		maxRows = 30
	}
	if perFile <= 0 {
		perFile = 3
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	argv := []string{bin, "--line-number", "--no-heading", "--color", "never", "--max-count", strconv.Itoa(perFile), "--max-filesize", "1M"}
	if !o.Regex {
		argv = append(argv, "--fixed-strings")
	}
	argv = append(argv, "--smart-case")
	if o.Word {
		argv = append(argv, "--word-regexp")
	}
	for _, g := range o.Globs {
		argv = append(argv, "--glob", g)
	}
	argv = append(argv, "--", pattern, ".")
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	exit, out, _, err := platform.RunArgvSplit(ctx, cwd, argv, "", 8<<20)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return RgResult{Reason: "exit-null"}
		}
		return RgResult{Reason: "spawn-failed"}
	}
	// rg exits 1 when there are simply no matches: that is an answer, not a failure.
	if exit != 0 && exit != 1 {
		return RgResult{Reason: "exit-" + strconv.Itoa(exit)}
	}
	rows := []RgRow{}
	for _, raw := range strings.Split(out, "\n") {
		if jscompat.Trim(raw) == "" {
			continue
		}
		m := rgLine.FindStringSubmatch(raw)
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[2])
		p := strings.TrimPrefix(strings.ReplaceAll(m[1], `\`, "/"), "./")
		rows = append(rows, RgRow{Path: p, Line: n, Text: jscompat.UTF16Prefix(jscompat.Trim(m[3]), 200)})
		if len(rows) >= maxRows {
			break
		}
	}
	return RgResult{OK: true, Reason: "ok", Rows: rows, Truncated: len(rows) >= maxRows}
}

// KnownRgLocations are the install locations to try when rg is not on PATH (core/rg.mjs knownRgLocations).
func KnownRgLocations(goos, home string, getenv func(string) string) []string {
	var out []string
	if goos == "windows" {
		local := getenv("LOCALAPPDATA")
		if local == "" {
			local = filepath.Join(home, "AppData", "Local")
		}
		out = append(out, filepath.Join(local, "Microsoft", "WinGet", "Links", "rg.exe"))
		pkgs := filepath.Join(local, "Microsoft", "WinGet", "Packages")
		if entries, err := os.ReadDir(pkgs); err == nil {
			for _, e := range entries {
				if !strings.HasPrefix(e.Name(), "BurntSushi.ripgrep") {
					continue
				}
				pkgDir := filepath.Join(pkgs, e.Name())
				out = append(out, filepath.Join(pkgDir, "rg.exe"))
				if subs, err := os.ReadDir(pkgDir); err == nil {
					for _, s := range subs {
						if strings.HasPrefix(s.Name(), "ripgrep-") {
							out = append(out, filepath.Join(pkgDir, s.Name(), "rg.exe"))
						}
					}
				}
			}
		}
		return append(out, filepath.Join(home, ".cargo", "bin", "rg.exe"), filepath.Join(home, "scoop", "shims", "rg.exe"))
	}
	return append(out, "/usr/bin/rg", "/usr/local/bin/rg", "/opt/homebrew/bin/rg",
		filepath.Join(home, ".cargo", "bin", "rg"), filepath.Join(home, ".local", "bin", "rg"))
}

// DetectRg says whether `rg` runs from the PATH and, when it does not, the first known location
// that holds a file (a terminal opened before the install does not see it yet). The path keeps the
// OS separators.
func DetectRg(getenv func(string) string) (onPath bool, installedAt string) {
	if _, ok := tryVersion("rg"); ok {
		return true, ""
	}
	for _, bin := range KnownRgLocations(runtime.GOOS, homeOf(getenv), getenv) {
		if _, err := os.Stat(bin); err == nil {
			return false, bin
		}
	}
	return false, ""
}

// FindRg is the path to a working rg ("rg" when the PATH has it), or "" and false. The PATH goes
// first so a user's own build wins (core/rg.mjs findRg).
func FindRg(getenv func(string) string) (string, bool) {
	if _, ok := tryVersion("rg"); ok {
		return "rg", true
	}
	for _, bin := range KnownRgLocations(runtime.GOOS, homeOf(getenv), getenv) {
		if _, err := os.Stat(bin); err != nil {
			continue
		}
		if _, ok := tryVersion(bin); ok {
			return bin, true
		}
	}
	return "", false
}
