package main

import (
	"context"
	"flag"
	"fmt"
	"io"
)

// runReview is `nxy review save --feature <id> --lens <name>` and `nxy review refute --feature <id>`:
// the JSON of the reviewer or the refuter comes on stdin.
func runReview(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	const usage = "usage: nxy review save --feature <id> --lens <name> | nxy review refute --feature <id>  (JSON on stdin) | nxy review docs --feature <id>"
	if len(args) == 0 || (args[0] != "save" && args[0] != "refute" && args[0] != "docs") {
		fmt.Fprintln(stderr, usage)
		return 1
	}
	sub := args[0]
	fs := flag.NewFlagSet("review "+sub, flag.ContinueOnError)
	fs.SetOutput(stderr)
	id := fs.String("feature", "", "feature id")
	lens := fs.String("lens", "", "lens name")
	if err := fs.Parse(args[1:]); err != nil {
		return 1
	}
	if *id == "" || (sub == "save" && *lens == "") {
		fmt.Fprintf(stderr, "nxy review %s: %s\n", sub, usage)
		return 1
	}
	h, st := newHooksStore()
	defer st.Close()
	ctx := context.Background()
	if sub == "docs" {
		text, err := h.DocsPacket(ctx, *id)
		if err != nil {
			fmt.Fprintf(stderr, "nxy review docs: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, text)
		return 0
	}
	if sub == "refute" {
		n, err := h.SaveRefute(ctx, *id, stdin)
		if err != nil {
			fmt.Fprintf(stderr, "nxy review refute: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "review refutación guardada (%d veredictos); reply with exactly this line, nothing else\n", n)
		return 0
	}
	res, err := h.SaveReview(ctx, *id, *lens, stdin)
	if err != nil {
		fmt.Fprintf(stderr, "nxy review save: %v\n", err)
		return 1
	}
	if len(res.Problems) > 0 {
		for _, p := range res.Problems {
			fmt.Fprintln(stderr, p.String())
		}
		return 1
	}
	fmt.Fprintf(stdout, "review %s guardada (%d de %d); reply with exactly this line, nothing else\n", *lens, res.Saved, res.Total)
	return 0
}
