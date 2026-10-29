// Command nxy is the nxy binary. Only this package wires adapters to use cases.
package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/matyy-a/nxy.dev/internal/host/claudecode"
	"github.com/matyy-a/nxy.dev/internal/version"
)

type command func(args []string, stdin io.Reader, stdout, stderr io.Writer) int

var commands = map[string]command{
	"bench":   runBench,
	"hook":    runHook,
	"version": runVersion,
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: nxy <hook|bench|version>")
		return 1
	}
	cmd, ok := commands[args[0]]
	if !ok {
		fmt.Fprintf(stderr, "nxy: unknown command %q\nusage: nxy <hook|bench|version>\n", args[0])
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
	resp := newHooks().Handle(context.Background(), ev)
	if err := claudecode.Encode(stdout, ev, resp); err != nil {
		fmt.Fprintf(stderr, "nxy hook: %v\n", err)
	}
	return 0
}
