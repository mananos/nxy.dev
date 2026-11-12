// Command nxy is the nxy binary. Only this package wires adapters to use cases.
package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/mananos/nxy.dev/internal/host/claudecode"
	"github.com/mananos/nxy.dev/internal/platform"
	"github.com/mananos/nxy.dev/internal/version"
)

type command func(args []string, stdin io.Reader, stdout, stderr io.Writer) int

var commands = map[string]command{
	"affected":   runAffected,
	"batch":      runBatch,
	"bench":      runBench,
	"config":     runConfig,
	"design":     runDesign,
	"feature":    runFeature,
	"filter":     runFilter,
	"gate":       runGate,
	"hook":       runHook,
	"legacy":     runLegacy,
	"locate":     runLocate,
	"mem":        runMem,
	"note":       runNote,
	"plan":       runPlan,
	"review":     runReview,
	"setup":      runSetup,
	"stats":      runStats,
	"status":     runStatus,
	"statusline": runStatusline,
	"trend":      runTrend,
	"tui":        runTui,
	"version":    runVersion,
	"view":       runView,
}

const usage = "usage: nxy <hook|bench|design agenda|fork|today|propose|close|plan save|gaps|review save|refute|docs|batch red|green|affected|status|stats|trend|statusline|setup status|run|filter status|on|off|gate status|once|reads|config show|set|cache-ttl|locate|legacy import|status|mem rule|edit|rm|restore|show|search|promote|related|export|import|note|note save|list|done|feature list|new|pause|resume|close|stop|tui --demo [--dump <pantalla> --width N --plain]|view <cockpit|agents|decision|memory|stats|trend|config|plan|file|engineer> --json|version>"

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return 1
	}
	cmd, ok := commands[args[0]]
	if !ok {
		fmt.Fprintf(stderr, "nxy: unknown command %q\n%s\n", args[0], usage)
		return 1
	}
	return cmd(args[1:], stdin, stdout, stderr)
}

func runVersion(_ []string, _ io.Reader, stdout, _ io.Writer) int {
	fmt.Fprintln(stdout, version.Version)
	return 0
}

// runHook never blocks the host by accident: any failure exits 0 with nothing on stdout.
func runHook(_ []string, stdin io.Reader, stdout, stderr io.Writer) (code int) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(stderr, "nxy hook: panic: %v\n", r)
			code = 0
		}
	}()
	ev, err := claudecode.Decode(stdin)
	if err != nil {
		fmt.Fprintf(stderr, "nxy hook: %v\n", err)
		return 0
	}
	if ev.Name == "SessionStart" {
		if u, err := platform.UserDir(); err == nil {
			refreshStableShim(u)
		}
	}
	h, st := newHooksStore()
	defer st.Close()
	resp := h.Handle(context.Background(), ev)
	if err := claudecode.Encode(stdout, ev, resp); err != nil {
		fmt.Fprintf(stderr, "nxy hook: %v\n", err)
	}
	return 0
}
