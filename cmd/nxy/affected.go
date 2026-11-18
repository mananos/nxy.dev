package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/mananos/nxy.dev/v2/internal/app"
	"github.com/mananos/nxy.dev/v2/internal/domain"
	"github.com/mananos/nxy.dev/v2/internal/host/claudecode"
)

// runAffected is `nxy affected --feature <id>`: nxy works out the affected tests, runs them,
// records the verdict and prints the next step.
func runAffected(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("affected", flag.ContinueOnError)
	fs.SetOutput(stderr)
	id := fs.String("feature", "", "feature id")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *id == "" {
		fmt.Fprintln(stderr, "nxy affected: --feature is required")
		return 1
	}
	h, st := newHooksStore()
	defer st.Close()
	ctx, cancel := context.WithTimeout(context.Background(), acceptTimeout)
	defer cancel()
	res, err := h.RunAffected(ctx, *id)
	if err != nil {
		fmt.Fprintf(stderr, "nxy affected: %v\n", err)
		return 1
	}
	rec := res.Record
	fmt.Fprintf(stdout, "Afectados (fuente: %s, %d tests)\n", rec.Source, len(rec.Tests))
	if rec.Note != "" {
		fmt.Fprintln(stdout, rec.Note)
	}
	for _, r := range rec.Runs {
		tag := ""
		if r.RepoName != "" {
			tag = "[" + r.RepoName + "] "
		}
		if r.Repo {
			fmt.Fprintf(stdout, "%s$ %s (fila del repo: %s)\n", tag, r.Command, r.Row)
		} else {
			fmt.Fprintf(stdout, "%s$ %s\n", tag, r.Command)
		}
		if r.Tail != "" {
			fmt.Fprintln(stdout, r.Tail)
		}
	}
	code := 0
	switch res.Verdict {
	case app.AffectedGreen:
		fmt.Fprintln(stdout, "verde")
	case app.AffectedRed:
		fmt.Fprintf(stdout, "rojo (exit %d)\n", failingExit(rec.Runs))
		code = 1
	default:
		fmt.Fprintf(stdout, "sin verificar: %s\n", res.Reason)
	}
	if res.Check {
		fmt.Fprintln(stdout, "(chequeo durante el arreglo: no se registra nada)")
		return code
	}
	if res.Refused != "" {
		fmt.Fprintln(stdout, res.Refused)
		return 1
	}
	if res.Recorded {
		fmt.Fprintln(stdout, claudecode.InstructionFor(res.Feature, "", exePath()))
	}
	return code
}

func failingExit(runs []domain.CommandRun) int {
	for _, r := range runs {
		if r.TimedOut || r.Exit != 0 {
			return r.Exit
		}
	}
	return 1
}
