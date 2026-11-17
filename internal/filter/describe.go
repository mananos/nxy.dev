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
	rtk := "no está en el PATH ni en las carpetas de instalación conocidas"
	if info.RtkPath != "" {
		rtk = info.RtkPath + " v" + info.RtkVersion
		if strings.ContainsAny(info.RtkPath, `/\`) {
			rtk += "  (no está en el PATH de este proceso — está en una carpeta de instalación conocida; una terminal nueva lo va a tener en el PATH)"
		}
	}
	rgLine := "no está (RTK lo necesita para algunos filtros)"
	switch {
	case rg.Available:
		rgLine = "disponible"
	case rg.InstalledAt != "":
		rgLine = "instalado en " + rg.InstalledAt + " pero no está en el PATH de este proceso — abrí una terminal nueva (RTK lanza rg él mismo para algunos filtros)"
	}
	hook := "no instalado (bien)"
	if info.RtkHookDetected {
		hook = "INSTALADO — nxy le cede la reescritura; corré `rtk init --uninstall` (o sacá el hook) para que la maneje nxy"
	}
	lines := []string{
		"motor:         " + info.Engine + " (" + info.Reason + "; config=" + s.Engine + ")",
		"rtk:           " + rtk,
		"ripgrep (rg):  " + rgLine,
		"hook de rtk:   " + hook,
	}
	if info.RtkPath == "" {
		lines = append(lines,
			"instalar:      corré /nxy:setup (muestra el comando exacto y lo corre cuando confirmás)   |   Windows → winget install rtk-ai.rtk   |   Linux/macOS → mirá https://github.com/rtk-ai/rtk#installation",
			"               NO corras `rtk init -g`: nxy instala su propio hook.",
		)
	}
	if !rg.Available && rg.InstalledAt == "" {
		lines = append(lines, "instalar rg:   corré /nxy:setup   |   Windows → winget install BurntSushi.ripgrep.MSVC   |   Linux → apt/dnf install ripgrep")
	}
	return strings.Join(lines, "\n")
}
