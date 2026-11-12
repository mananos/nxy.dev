package tools

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// renderD2Timeout bounds one `d2` render; a var so a test can force the timeout path.
var renderD2Timeout = 5 * time.Second

// KnownD2Locations are the well-known install locations of d2, for processes whose PATH predates the install.
func KnownD2Locations(goos, home string, getenv func(string) string) []string {
	var out []string
	if goos == "windows" {
		local := getenv("LOCALAPPDATA")
		if local == "" {
			local = filepath.Join(home, "AppData", "Local")
		}
		out = append(out, filepath.Join(local, "Microsoft", "WinGet", "Links", "d2.exe"))
		pkgs := filepath.Join(local, "Microsoft", "WinGet", "Packages")
		if entries, err := os.ReadDir(pkgs); err == nil {
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), "Terrastruct.D2") {
					out = append(out, filepath.Join(pkgs, e.Name(), "d2.exe"))
				}
			}
		}
		out = append(out, filepath.Join(home, ".local", "bin", "d2.exe"), filepath.Join(home, "go", "bin", "d2.exe"))
		return out
	}
	return append(out,
		filepath.Join(home, ".local", "bin", "d2"),
		filepath.Join(home, "go", "bin", "d2"),
		"/usr/local/bin/d2", "/opt/homebrew/bin/d2")
}

// FindD2 locates d2: NXY_D2_PATH, then `d2` on PATH, then `d2.exe` (Windows), then known install
// locations. The version has a leading "v" stripped.
func FindD2(getenv func(string) string) (path, version string, ok bool) {
	var candidates []string
	if p := getenv("NXY_D2_PATH"); p != "" {
		candidates = append(candidates, p)
	}
	candidates = append(candidates, "d2")
	if runtime.GOOS == "windows" {
		candidates = append(candidates, "d2.exe")
	}
	for _, bin := range candidates {
		if v, found := tryVersion(bin); found {
			return bin, strings.TrimPrefix(v, "v"), true
		}
	}
	for _, bin := range KnownD2Locations(runtime.GOOS, homeOf(getenv), getenv) {
		if _, err := os.Stat(bin); err != nil {
			continue
		}
		if v, found := tryVersion(bin); found {
			return strings.ReplaceAll(bin, `\`, "/"), strings.TrimPrefix(v, "v"), true
		}
	}
	return "", "", false
}

// RenderD2 draws a D2 source as ASCII: `d2 --stdout-format txt - -` (source on stdin, text on
// stdout; d2 0.7.1 or later). Lines come back with trailing blanks trimmed. A non-zero exit or a
// timeout is an error.
func RenderD2(ctx context.Context, bin, src string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, renderD2Timeout)
	defer cancel()
	c := exec.CommandContext(ctx, bin, "--stdout-format", "txt", "-", "-")
	hideWindow(c)
	c.WaitDelay = time.Second // a killed wrapper's children must not hold the pipes open
	c.Stdin = strings.NewReader(src)
	var stdout bytes.Buffer
	c.Stdout = &stdout
	err := c.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.ReplaceAll(stdout.String(), "\r\n", "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines, nil
}
