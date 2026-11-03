package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/mananos/nxy.dev/internal/app"
)

// reviewUser is `nxy review show|status|escape`: what the user runs (/nxy:review).
func reviewUser(sub string, args []string, stdout, stderr io.Writer) int {
	opts, pos, _ := memArgs(args)
	dir := memCwd(opts)
	h, st := newHooksStore()
	defer st.Close()
	ctx := context.Background()
	switch sub {
	case "show":
		md, ok, err := h.ReviewShow(ctx, dir)
		if err != nil {
			fmt.Fprintf(stderr, "nxy review show: %v\n", err)
			return 1
		}
		if !ok {
			fmt.Fprintln(stdout, "no review yet")
			return 0
		}
		fmt.Fprintln(stdout, strings.TrimRight(md, "\n"))
	case "status":
		stats, err := h.ReviewStatus(ctx, dir)
		if err != nil {
			fmt.Fprintf(stderr, "nxy review status: %v\n", err)
			return 1
		}
		if len(stats) == 0 {
			fmt.Fprintf(stdout, "no review activity in the last %d days\n", app.ReviewStatsDays)
			return 0
		}
		fmt.Fprintf(stdout, "review by lens, last %d days\n", app.ReviewStatsDays)
		for _, s := range stats {
			fmt.Fprintf(stdout, "%s: %d chosen, %d ignored, %d escapes\n", s.Lens, s.Chosen, s.Ignored, s.Escapes)
		}
	case "escape":
		question := optVal(opts, "question")
		if len(pos) == 0 || question == "" {
			fmt.Fprintln(stderr, `usage: nxy review escape "<text>" --question "<what it settles>" [--lens base]`)
			return 1
		}
		res, err := h.ReviewEscape(ctx, dir, strings.Join(pos, " "), question, optVal(opts, "lens"))
		if err != nil {
			fmt.Fprintf(stderr, "nxy review escape: %v\n", err)
			return 1
		}
		if res.Candidate {
			fmt.Fprintf(stdout, "escape noted for feature %s: nxy asks about it as a rule when the feature closes\n", res.FeatureID)
			return 0
		}
		fmt.Fprintf(stdout, "escape saved as rule %s\n", res.Rule.Short)
		if res.Similar != nil {
			fmt.Fprintf(stdout, "similar to %s: %s\n", res.Similar.Short, res.Similar.Text)
		}
	}
	return 0
}

// runReview is `nxy review save --feature <id> --lens <name>` and `nxy review refute --feature <id>`:
// the JSON of the reviewer or the refuter comes on stdin.
func runReview(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	const usage = "usage: nxy review save --feature <id> --lens <name> | nxy review refute --feature <id>  (JSON on stdin) | nxy review docs --feature <id> | nxy review show | nxy review status | nxy review escape \"<text>\" --question \"<q>\" [--lens base]"
	if len(args) > 0 && (args[0] == "show" || args[0] == "status" || args[0] == "escape") {
		return reviewUser(args[0], args[1:], stdout, stderr)
	}
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
