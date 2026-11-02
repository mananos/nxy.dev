package main

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/mananos/nxy.dev/internal/filter"
	"github.com/mananos/nxy.dev/internal/host/claudecode"
	"github.com/mananos/nxy.dev/internal/platform"
	"github.com/mananos/nxy.dev/internal/setup"
	"github.com/mananos/nxy.dev/internal/tools"
)

// runSetupPlan is the only call that installs anything; a var so a test can prove it does not run.
var runSetupPlan = tools.RunPlan

// jsPlatform names the OS as the 1.x does (process.platform), which the plans and the status print.
func jsPlatform(goos string) string {
	if goos == "windows" {
		return "win32"
	}
	return goos
}

// setupState detects the three tools: what is installed, what collides, and the plan for each.
// It reads files and runs `--version`; it never installs.
func setupState(cwd string, getenv func(string) string) setup.State {
	goos := jsPlatform(runtime.GOOS)
	facts := claudecode.SetupFacts(homeDir(), cwd)
	pm := setup.PackageManager(goos, tools.HasBin)

	rtkPath, rtkVersion, rtkFound := tools.FindRtk(getenv)
	rgOnPath, rgAt := tools.DetectRg(getenv)
	_, cgVersion, cgPath, cgFound := tools.FindCodegraphLabeled(getenv)

	rtk := setup.ToolInfo{Found: rtkFound, Path: rtkPath, Version: rtkVersion,
		OnPath: rtkFound && !strings.ContainsAny(rtkPath, `\/`)}
	if setup.DetectRtkConflict(facts.Settings) {
		rtk.Conflict = setup.ConflictYes
	}
	rg := setup.ToolInfo{Found: rgOnPath || rgAt != "", OnPath: rgOnPath, Path: rgAt}
	if rgOnPath {
		rg.Path = "rg"
	}
	cg := setup.ToolInfo{Found: cgFound, Path: cgPath, Version: cgVersion, OnPath: cgFound && !strings.ContainsAny(cgPath, `\/`)}
	switch {
	case facts.HasMCP:
		cg.Conflict = setup.ConflictMCP
	case setup.DetectCodegraphWiring(facts.Settings, facts.MCP):
		cg.Conflict = setup.ConflictHooks
	}

	for _, t := range []struct {
		name string
		info *setup.ToolInfo
	}{{"rtk", &rtk}, {"rg", &rg}, {"codegraph", &cg}} {
		t.info.Plan = setup.InstallPlan(t.name, setup.Opts{
			Platform: goos, PM: pm, Found: t.info.Found, Conflict: t.info.Conflict,
			RtkPath: &rtk.Path, HookScopes: facts.HookScopes, MCPScope: facts.MCPScope,
		})
	}
	return setup.State{Platform: goos, Rtk: rtk, Rg: rg, Codegraph: cg}
}

type setupArgs struct {
	cwd        string
	json, yes  bool
	dryRun     bool
	positional []string
}

func parseSetupArgs(args []string) setupArgs {
	var a setupArgs
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--cwd":
			if i+1 < len(args) {
				i++
				a.cwd = args[i]
			}
		case "--json":
			a.json = true
		case "--yes":
			a.yes = true
		case "--dry-run":
			a.dryRun = true
		default:
			a.positional = append(a.positional, args[i])
		}
	}
	return a
}

// runSetup is `nxy setup status [--json] | run <rtk|rg|codegraph> --yes [--dry-run]`. It only ever
// installs by the user's explicit choice (/nxy:setup or the panel's Config button); no hook calls it.
// Like the 1.x it answers on stdout, and exits 1 when it did not install what was asked.
func runSetup(args []string, _ io.Reader, stdout, _ io.Writer) (code int) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(stdout, "error: %v\n", r)
			code = 1
		}
	}()
	a := parseSetupArgs(args)
	cwd := resolveCwd(a)
	action := ""
	if len(a.positional) > 0 {
		action = strings.ToLower(a.positional[0])
	}
	switch action {
	case "status":
		st := setupState(cwd, os.Getenv)
		if a.json {
			fmt.Fprintln(stdout, setup.StatusJSON(st))
		} else {
			fmt.Fprintln(stdout, setup.StatusText(st))
		}
		return 0
	case "run":
		tool := ""
		if len(a.positional) > 1 {
			tool = strings.ToLower(a.positional[1])
		}
		return setupRun(cwd, tool, a, stdout)
	}
	fmt.Fprintln(stdout, setup.StatusUsage)
	return 1
}

func setupRun(cwd, tool string, a setupArgs, stdout io.Writer) int {
	known := false
	for _, t := range setup.Tools {
		known = known || t == tool
	}
	if !known {
		fmt.Fprintln(stdout, setup.RunUsage)
		return 1
	}
	plan := setupState(cwd, os.Getenv).Tool(tool).Plan
	if !a.yes {
		fmt.Fprintln(stdout, setup.MissingYesText(tool, plan))
		return 1
	}
	if len(plan.Steps) == 0 {
		fmt.Fprintln(stdout, strings.Join(setup.DescribePlan(plan, false), "\n"))
		return 1
	}
	fmt.Fprintln(stdout, setup.RunHeader(tool, plan, a.dryRun))
	if a.dryRun {
		return 0
	}
	r := runSetupPlan(plan)
	if r.OK {
		refreshEngineCache(cwd)
	}
	text, code := setup.RunOutcome(tool, plan, r)
	fmt.Fprintln(stdout, text)
	return code
}

// refreshEngineCache makes the filter see a freshly installed rtk now; failing is fine, the cache
// refreshes itself on the next session.
func refreshEngineCache(cwd string) {
	defer func() { _ = recover() }()
	settings := platform.LoadFilterSettings(cwd, os.Getenv)
	tools.Engine{Getenv: os.Getenv}.Refresh(settings, func() bool {
		return filter.RtkHookIn(claudecode.PreToolUseHookCommands(claudecode.SettingsFiles(homeDir(), cwd)))
	})
}
