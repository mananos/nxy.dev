package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mananos/nxy.dev/internal/filter"
	"github.com/mananos/nxy.dev/internal/host/claudecode"
	"github.com/mananos/nxy.dev/internal/platform"
	"github.com/mananos/nxy.dev/internal/tools"
)

// filterConfigPath is where `on`/`off` write: the project override, at the git root when there is one.
func filterConfigPath(cwd string) string {
	root := cwd
	if r, ok := platform.GitRoot(cwd); ok {
		root = r
	}
	return filepath.Join(root, ".nxy", "config.json")
}

// runFilter is `nxy filter [status|on|off [--repo]]`: on/off toggle modules.filter in the user config
// (~/.nxy/config.json) or, with --repo, in <git root>/.nxy/config.json (a few bytes of the user's,
// never deleted by nxy; a broken file is refused, not replaced), and every call
// refreshes the engine cache and prints the status like the 1.x /nxy:filter.
func runFilter(args []string, _ io.Reader, stdout, _ io.Writer) (code int) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(stdout, "error: %v\n", r)
			code = 1
		}
	}()
	a := parseSetupArgs(args)
	cwd := resolveCwd(a)
	repo, pos := splitRepo(a.positional)
	action := "status"
	if len(pos) > 0 {
		action = strings.ToLower(pos[0])
	}

	switch action {
	case "on", "off":
		userPath, repoPath, err := configFilePaths(cwd)
		if err != nil {
			fmt.Fprintf(stdout, "error: %v\n", err)
			return 1
		}
		p := userPath
		if repo {
			p = repoPath
		}
		doc, _, err := platform.ReadJSONDoc(p)
		if err != nil || doc == nil {
			fmt.Fprintf(stdout, "%s está roto; no se toca\n", filepath.ToSlash(p))
			return 1
		}
		if err := doc.Set([]string{"modules", "filter"}, action == "on"); err != nil {
			fmt.Fprintf(stdout, "error: %v\n", err)
			return 1
		}
		if _, err := platform.WriteJSONDoc(p, doc); err != nil {
			fmt.Fprintf(stdout, "error: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "filter module: %s (written to %s)\n", strings.ToUpper(action), p)
	case "status":
	default:
		fmt.Fprintf(stdout, "unknown action %q. Use: status | on | off\n", action)
	}

	cfg := platform.LoadConfig(cwd, os.Getenv)
	s := platform.LoadFilterSettings(cwd, os.Getenv)
	info := tools.Engine{Getenv: os.Getenv}.Refresh(s, func() bool {
		return filter.RtkHookIn(claudecode.PreToolUseHookCommands(claudecode.SettingsFiles(homeDir(), cwd)))
	})
	rgOnPath, rgAt := tools.DetectRg(os.Getenv)
	rg := filter.RgState{Available: rgOnPath}
	if !rgOnPath {
		rg.InstalledAt = strings.ReplaceAll(rgAt, `\`, "/")
	}

	modules, _ := cfg["modules"].(map[string]any)
	on := "DISABLED"
	if s.Enabled {
		on = "enabled"
	}
	metrics := "disabled"
	if v, present := modules["metrics"]; !present || v == true {
		metrics = "enabled"
	}
	fmt.Fprintf(stdout, "filter module: %s (metrics module: %s)\n", on, metrics)
	fmt.Fprintln(stdout, filter.DescribeEngine(info, rg, s))
	if len(s.ExcludeCommands) > 0 {
		fmt.Fprintf(stdout, "excluded:      %s\n", strings.Join(s.ExcludeCommands, ", "))
	}
	if len(s.OnlyCommands) > 0 {
		fmt.Fprintf(stdout, "only:          %s\n", strings.Join(s.OnlyCommands, ", "))
	}
	fmt.Fprintln(stdout, `escape hatch:  prefix a command with NXY_RAW=1 (or append "# raw") to run it unfiltered`)
	return 0
}
