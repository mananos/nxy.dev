package setup

import (
	"fmt"
	"strings"
)

// PathOpts feed PathPlan. ShimPath is the stable shim (~/.nxy/bin/nxy.cmd), Home the OS home.
type PathOpts struct {
	Platform string
	ShimPath string
	Home     string
}

// shq quotes s for sh with single quotes.
func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// psq quotes s for PowerShell with single quotes.
func psq(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// PathPlan is what makes `nxy` resolvable in the user's terminals. Linux and macOS: a 2-line script
// in ~/.local/bin that runs the stable shim with sh (rewritten every time; it does not follow the
// version, the shim updates itself). Windows: the shim's folder is added to the user's Path, read
// from the registry unexpanded so %VAR% entries survive.
func PathPlan(o PathOpts) Plan {
	var step Step
	if o.Platform == "win32" {
		dir := o.ShimPath
		if i := strings.LastIndexAny(dir, `\/`); i >= 0 {
			dir = dir[:i]
		}
		cmd := `$k=[Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment',$true); ` +
			`$p=[string]$k.GetValue('Path','',[Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames); ` +
			`$d=` + psq(dir) + `; ` +
			`if((($p -split ';') | ForEach-Object { $_.TrimEnd('\') }) -notcontains $d.TrimEnd('\')) { ` +
			`$n=$d; if($p){ $n=$p.TrimEnd(';')+';'+$d }; ` +
			`$k.SetValue('Path',$n,[Microsoft.Win32.RegistryValueKind]::ExpandString) }; ` +
			`$k.Close(); [Environment]::SetEnvironmentVariable('NXY_PATH_REFRESH',$null,'User')`
		step = Step{
			Display: `powershell -NoProfile -Command ` + psq(cmd),
			File:    "powershell",
			Args:    []string{"-NoProfile", "-Command", cmd},
			Source:  "nxy setup run path",
		}
	} else {
		dir := `"$HOME/.local/bin"`
		if o.Home != "" {
			dir = shq(strings.TrimRight(o.Home, "/") + "/.local/bin")
		}
		script := fmt.Sprintf(`mkdir -p %s && printf '%%s\n' '#!/bin/sh' %s > %s/nxy && chmod 755 %s/nxy`,
			dir, shq(`exec sh `+shq(o.ShimPath)+` "$@"`), dir, dir)
		step = Step{Display: script, File: "sh", Args: []string{"-c", script}, Source: "nxy setup run path"}
	}
	return Plan{Tool: "path", Action: "install", OS: o.Platform, Steps: []Step{step}, Removes: []Step{}, Manual: []string{}}
}
