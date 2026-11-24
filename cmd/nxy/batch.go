package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/mananos/nxy.dev/v2/internal/app"
)

// runBatch is `nxy batch red|green --feature <id>`: nxy runs the Accept of the current batch.
func runBatch(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || (args[0] != "red" && args[0] != "green") {
		fmt.Fprintln(stderr, "usage: nxy batch red|green --feature <id> [--batch <n>]")
		return 1
	}
	kind := app.RunKind(args[0])
	fs := flag.NewFlagSet("batch "+args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	id := fs.String("feature", "", "feature id")
	batch := fs.Int("batch", 0, "batch number (needed when several batches run at once)")
	if err := fs.Parse(args[1:]); err != nil {
		return 1
	}
	if *id == "" {
		fmt.Fprintf(stderr, "nxy batch %s: --feature is required\n", kind)
		return 1
	}
	h, st := newHooksStore()
	defer st.Close()
	res, err := h.RunBatchCheck(context.Background(), *id, kind, *batch)
	if err != nil {
		fmt.Fprintf(stderr, "nxy batch %s: %v\n", kind, err)
		return 1
	}
	if res.Tail != "" {
		fmt.Fprintln(stdout, res.Tail)
	}
	switch {
	case res.Refused != "":
		fmt.Fprintf(stdout, "%s not recorded: %s\n", kind, res.Refused)
		return 1
	case kind == app.RunRed:
		if res.Warn != "" {
			fmt.Fprintln(stdout, "nxy: "+res.Warn)
		}
		fmt.Fprintln(stdout, "red recorded")
		return 0
	case res.Exit == 0:
		fmt.Fprintln(stdout, "green recorded")
		return 0
	default:
		fmt.Fprintf(stdout, "green failed (exit %d): fix and run green again\n", res.Exit)
		return 1
	}
}
