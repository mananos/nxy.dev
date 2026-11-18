package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mananos/nxy.dev/v2/internal/filter"
	"github.com/mananos/nxy.dev/v2/internal/host/claudecode"
	"github.com/mananos/nxy.dev/v2/internal/platform"
	"github.com/mananos/nxy.dev/v2/internal/tools"
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
		fmt.Fprintf(stdout, "módulo de filtros: %s (escrito en %s)\n", strings.ToUpper(action), p)
	case "status":
	default:
		fmt.Fprintf(stdout, "no conozco la acción %q. Usá: status | on | off\n", action)
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
	on := "DESACTIVADO"
	if s.Enabled {
		on = "activado"
	}
	metrics := "desactivado"
	if v, present := modules["metrics"]; !present || v == true {
		metrics = "activado"
	}
	fmt.Fprintf(stdout, "módulo de filtros: %s (módulo de métricas: %s)\n", on, metrics)
	fmt.Fprintln(stdout, filter.DescribeEngine(info, rg, s))
	if len(s.ExcludeCommands) > 0 {
		fmt.Fprintf(stdout, "excluidos:     %s\n", strings.Join(s.ExcludeCommands, ", "))
	}
	if len(s.OnlyCommands) > 0 {
		fmt.Fprintf(stdout, "solo:          %s\n", strings.Join(s.OnlyCommands, ", "))
	}
	fmt.Fprintln(stdout, `salida de emergencia: poné NXY_RAW=1 antes de un comando (o agregale "# raw") para correrlo sin filtrar`)
	return 0
}
