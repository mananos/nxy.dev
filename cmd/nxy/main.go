// Command nxy is the nxy binary. Only this package wires adapters to use cases.
package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/mananos/nxy.dev/internal/host/claudecode"
	"github.com/mananos/nxy.dev/internal/version"
)

type command func(args []string, stdin io.Reader, stdout, stderr io.Writer) int

var commands = map[string]command{
	"affected":   runAffected,
	"batch":      runBatch,
	"bench":      runBench,
	"design":     runDesign,
	"feature":    runFeature,
	"hook":       runHook,
	"plan":       runPlan,
	"review":     runReview,
	"stats":      runStats,
	"status":     runStatus,
	"statusline": runStatusline,
	"trend":      runTrend,
	"version":    runVersion,
}

const usage = "usage: nxy <hook|bench|design agenda|propose|close|plan save|gaps|review save|refute|batch red|green|affected|status|stats|trend|statusline|feature stop|version>"

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
	h, st := newHooksStore()
	defer st.Close()
	resp := h.Handle(context.Background(), ev)
	if err := claudecode.Encode(stdout, ev, resp); err != nil {
		fmt.Fprintf(stderr, "nxy hook: %v\n", err)
	}
	return 0
}
