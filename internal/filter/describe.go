package filter

import "strings"

// RgState is what `nxy filter status` knows about ripgrep: on this process PATH, or installed at a
// known place that is not on PATH yet (InstalledAt, with forward slashes).
type RgState struct {
	Available   bool
	InstalledAt string
}

// DescribeEngine is the status block of `nxy filter status`, line for line the 1.x describeEngine
// (core/filter/engine.mjs). Unlike the 1.x it also treats a backslash in the rtk path as "found in
// a known install dir" (the 1.x regex only looks for a slash).
func DescribeEngine(info EngineInfo, rg RgState, s Settings) string {
	rtk := "not found on PATH nor in known install dirs"
	if info.RtkPath != "" {
		rtk = info.RtkPath + " v" + info.RtkVersion
		if strings.ContainsAny(info.RtkPath, `/\`) {
			rtk += "  (not on this process PATH — found in a known install dir; a new terminal will have it on PATH)"
		}
	}
	rgLine := "not found (RTK needs it for some filters)"
	switch {
	case rg.Available:
		rgLine = "available"
	case rg.InstalledAt != "":
		rgLine = "installed at " + rg.InstalledAt + " but not on this process PATH — open a new terminal (RTK spawns rg itself for some filters)"
	}
	hook := "not installed (good)"
	if info.RtkHookDetected {
		hook = "INSTALLED — nxy yields the rewrite to it; run `rtk init --uninstall` (or remove the hook) to let nxy manage it"
	}
	lines := []string{
		"engine:        " + info.Engine + " (" + info.Reason + "; config=" + s.Engine + ")",
		"rtk:           " + rtk,
		"ripgrep (rg):  " + rgLine,
		"rtk own hook:  " + hook,
	}
	if info.RtkPath == "" {
		lines = append(lines,
			"install:       run /nxy:setup (shows the exact command, runs it after you confirm)   |   Windows → winget install rtk-ai.rtk   |   Linux/macOS → see https://github.com/rtk-ai/rtk#installation",
			"               Do NOT run `rtk init -g` — nxy installs its own hook.",
		)
	}
	if !rg.Available && rg.InstalledAt == "" {
		lines = append(lines, "install rg:    run /nxy:setup   |   Windows → winget install BurntSushi.ripgrep.MSVC   |   Linux → apt/dnf install ripgrep")
	}
	return strings.Join(lines, "\n")
}
