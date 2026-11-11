package platform

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// IconsFrom decides whether the terminal can draw Nerd Font glyphs. Pure: the
// caller passes what it found (faces of the Windows Terminal settings, output of
// `fc-list : family`). The terminal never says which font it uses, so this is a
// heuristic that falls back to Unicode; NXY_ICONS=nerd|unicode always wins.
func IconsFrom(getenv func(string) string, wtFaces []string, fcFamilies string) bool {
	switch strings.ToLower(strings.TrimSpace(getenv("NXY_ICONS"))) {
	case "nerd":
		return true
	case "unicode":
		return false
	}
	if getenv("TERM_PROGRAM") == "WezTerm" {
		return true
	}
	if getenv("WT_SESSION") != "" {
		for _, f := range wtFaces {
			if isNerdFace(f) {
				return true
			}
		}
		return false
	}
	return strings.Contains(strings.ToLower(fcFamilies), "nerd")
}

func isNerdFace(f string) bool {
	if strings.Contains(strings.ToLower(f), "nerd") {
		return true
	}
	for _, suf := range []string{" NF", " NFM", " NFP"} {
		if strings.HasSuffix(f, suf) {
			return true
		}
	}
	return false
}

var faceRe = regexp.MustCompile(`"face"\s*:\s*"([^"]*)"`)

// DetectIcons gathers the inputs of IconsFrom from the machine. Any error means Unicode.
func DetectIcons(getenv func(string) string) bool {
	var faces []string
	if getenv("WT_SESSION") != "" {
		if local := getenv("LOCALAPPDATA"); local != "" {
			for _, pkg := range []string{"Microsoft.WindowsTerminal_8wekyb3d8bbwe", "Microsoft.WindowsTerminalPreview_8wekyb3d8bbwe"} {
				b, err := os.ReadFile(filepath.Join(local, "Packages", pkg, "LocalState", "settings.json"))
				if err != nil {
					continue
				}
				for _, m := range faceRe.FindAllSubmatch(b, -1) {
					faces = append(faces, string(m[1]))
				}
			}
		}
	}
	fc := ""
	if runtime.GOOS != "windows" {
		if path, err := exec.LookPath("fc-list"); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			if out, err := exec.CommandContext(ctx, path, ":", "family").Output(); err == nil {
				fc = string(out)
			}
		}
	}
	return IconsFrom(getenv, faces, fc)
}
