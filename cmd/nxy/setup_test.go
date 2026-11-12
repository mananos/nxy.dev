package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/setup"
)

// noInstall replaces the runner so no test can install anything; it records the calls.
func noInstall(t *testing.T, result setup.RunResult) *[]setup.Plan {
	t.Helper()
	var calls []setup.Plan
	old := runSetupPlan
	runSetupPlan = func(p setup.Plan) setup.RunResult {
		calls = append(calls, p)
		return result
	}
	t.Cleanup(func() { runSetupPlan = old })
	return &calls
}

func TestSetupStatus(t *testing.T) {
	isolate(t)
	noInstall(t, setup.RunResult{})
	cwd := t.TempDir()

	code, out, _ := runCmd(t, "", "setup", "status", "--cwd", cwd)
	if code != 0 || !strings.HasPrefix(out, "nxy setup (") || !strings.Contains(out, "\nrtk: ") ||
		!strings.Contains(out, "\ncodegraph: ") || !strings.HasSuffix(out, "Para instalar: nxy setup run <herramienta> --yes\n") {
		t.Fatalf("status (%d):\n%s", code, out)
	}

	code, out, _ = runCmd(t, "", "setup", "status", "--json", "--cwd", cwd)
	var got struct {
		OK       bool
		Platform string
		Tools    map[string]struct {
			Plan struct{ Tool, Action string }
		}
	}
	if code != 0 || json.Unmarshal([]byte(out), &got) != nil || !got.OK || len(got.Tools) != 4 || got.Tools["rg"].Plan.Tool != "rg" {
		t.Fatalf("status --json (%d): %s", code, out)
	}
	if got.Platform != "win32" && got.Platform != "linux" && got.Platform != "darwin" {
		t.Errorf("platform %q is not a process.platform name", got.Platform)
	}
}

func TestSetupStatusReadsTheConfigFiles(t *testing.T) {
	isolate(t)
	noInstall(t, setup.RunResult{})
	home := homeDir()
	cwd := t.TempDir()
	write := func(path, text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(home, ".claude", "settings.json"),
		`{"hooks":{"PreToolUse":[{"hooks":[{"command":"rtk hook claude"}]}]}}`)
	write(filepath.Join(home, ".claude.json"), `{"mcpServers":{"codegraph":{"command":"codegraph"}}}`)
	write(filepath.Join(cwd, ".claude", "settings.json"), `{ broken`)

	_, out, _ := runCmd(t, "", "setup", "status", "--cwd", cwd)
	for _, want := range []string{
		"  choca: hook propio de rtk en tus settings\n",
		"  choca: codegraph ya registrado como servidor MCP\n",
		"quita:   claude mcp remove codegraph -s user   (claude mcp)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status lacks %q:\n%s", want, out)
		}
	}
}

func TestSetupRunWithoutYesDoesNotInstall(t *testing.T) {
	isolate(t)
	calls := noInstall(t, setup.RunResult{OK: true})
	code, out, _ := runCmd(t, "", "setup", "run", "rtk", "--cwd", t.TempDir())
	if code != 1 || !strings.HasPrefix(out, "Falta --yes: no se instala nada. Lo que correría para rtk:\n") {
		t.Fatalf("run without --yes (%d): %s", code, out)
	}
	if len(*calls) != 0 {
		t.Fatalf("ran %d plans without --yes", len(*calls))
	}
}

func TestSetupRunDryRunRunsNothing(t *testing.T) {
	isolate(t)
	calls := noInstall(t, setup.RunResult{OK: true})
	code, out, _ := runCmd(t, "", "setup", "run", "rtk", "--yes", "--dry-run", "--cwd", t.TempDir())
	// the action is reinstall on a machine that already has rtk
	if code != 0 || !strings.Contains(out, "install rtk (dry-run: no se ejecuta nada):\n  ") || !strings.Contains(out, "install: ") {
		t.Fatalf("dry-run (%d): %s", code, out)
	}
	if len(*calls) != 0 {
		t.Fatalf("dry-run ran %d plans", len(*calls))
	}
}

func TestSetupRunUnknownTool(t *testing.T) {
	isolate(t)
	calls := noInstall(t, setup.RunResult{OK: true})
	for _, args := range [][]string{{"setup", "run", "nope", "--yes"}, {"setup", "run"}} {
		code, out, _ := runCmd(t, "", args...)
		if code != 1 || out != setup.RunUsage+"\n" {
			t.Errorf("%v (%d): %q", args, code, out)
		}
	}
	code, out, _ := runCmd(t, "", "setup")
	if code != 1 || out != setup.StatusUsage+"\n" {
		t.Errorf("bare setup (%d): %q", code, out)
	}
	if len(*calls) != 0 {
		t.Fatal("ran a plan")
	}
}

func TestSetupRunReportsTheOutcome(t *testing.T) {
	isolate(t)
	calls := noInstall(t, setup.RunResult{OK: true, Ran: []string{"x"}, Tail: "all good"})
	code, out, _ := runCmd(t, "", "setup", "run", "rtk", "--yes", "--cwd", t.TempDir())
	if code != 0 || !strings.HasSuffix(out, "all good\nrtk: listo (1 paso).\n") || len(*calls) != 1 {
		t.Fatalf("ok (%d, %d calls): %s", code, len(*calls), out)
	}

	calls = noInstall(t, setup.RunResult{FailedAt: "step 2", Tail: "boom", Manual: []string{"sudo x"}})
	code, out, _ = runCmd(t, "", "setup", "run", "rtk", "--yes", "--cwd", t.TempDir())
	if code != 1 || out[strings.Index(out, "boom"):] != "boom\nFalló en: step 2\nPara correrlo en una terminal: sudo x\n" || len(*calls) != 1 {
		t.Fatalf("failed (%d): %s", code, out)
	}
}
