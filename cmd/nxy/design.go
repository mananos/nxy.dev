package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/mananos/nxy.dev/internal/app"
	"github.com/mananos/nxy.dev/internal/design"
	"github.com/mananos/nxy.dev/internal/host/claudecode"
)

// runDesign is `nxy design agenda|fork|today|propose|close --feature <id>`: the JSON comes on stdin.
func runDesign(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	const usage = "usage: nxy design agenda|fork|today|propose|close --feature <id>  (JSON on stdin)"
	if len(args) == 0 || (args[0] != "agenda" && args[0] != "fork" && args[0] != "today" && args[0] != "propose" && args[0] != "close") {
		fmt.Fprintln(stderr, usage)
		return 1
	}
	sub := args[0]
	fs := flag.NewFlagSet("design "+sub, flag.ContinueOnError)
	fs.SetOutput(stderr)
	id := fs.String("feature", "", "feature id")
	if err := fs.Parse(args[1:]); err != nil {
		return 1
	}
	if *id == "" {
		fmt.Fprintf(stderr, "nxy design %s: --feature is required\n", sub)
		return 1
	}
	h, st := newHooksStore()
	defer st.Close()
	ctx := context.Background()
	var (
		res app.DesignResult
		err error
	)
	switch sub {
	case "agenda":
		res, err = h.SaveAgenda(ctx, *id, stdin)
	case "fork":
		res, err = h.SaveForkOptions(ctx, *id, stdin)
	case "today":
		res, err = h.SaveToday(ctx, *id, stdin)
	case "propose":
		res, err = h.ProposeDecision(ctx, *id, stdin)
	default:
		res, err = h.ProposeClose(ctx, *id, stdin)
	}
	if err != nil {
		fmt.Fprintf(stderr, "nxy design %s: %v\n", sub, err)
		return 1
	}
	if len(res.Problems) > 0 {
		for _, p := range res.Problems {
			fmt.Fprintln(stderr, p.String())
		}
		return 1
	}
	switch sub {
	case "agenda", "fork", "today":
		fmt.Fprintln(stdout, design.Agenda(res.Feature.Design))
	case "propose":
		fmt.Fprintln(stdout, claudecode.ProposalInstruction(res.Feature))
	default:
		fmt.Fprintln(stdout, claudecode.CloseInstruction(res.Feature))
	}
	return 0
}

// runPlanGaps is `nxy plan gaps --feature <id>`: the planner of an engineer feature found holes.
func runPlanGaps(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("plan gaps", flag.ContinueOnError)
	fs.SetOutput(stderr)
	id := fs.String("feature", "", "feature id")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *id == "" {
		fmt.Fprintln(stderr, "nxy plan gaps: --feature is required")
		return 1
	}
	h, st := newHooksStore()
	defer st.Close()
	res, err := h.SaveGaps(context.Background(), *id, stdin)
	if err != nil {
		fmt.Fprintf(stderr, "nxy plan gaps: %v\n", err)
		return 1
	}
	if len(res.Problems) > 0 {
		for _, p := range res.Problems {
			fmt.Fprintln(stderr, p.String())
		}
		return 1
	}
	fmt.Fprintln(stdout, "design gaps saved; the feature went back to the design. Reply with exactly this line, nothing else")
	return 0
}
