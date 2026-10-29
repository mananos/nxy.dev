package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/mananos/nxy.dev/internal/app"
)

// runPlan is `nxy plan save --feature <id>`: the plan JSON comes on stdin.
func runPlan(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "save" {
		fmt.Fprintln(stderr, "usage: nxy plan save --feature <id>  (plan JSON on stdin)")
		return 1
	}
	fs := flag.NewFlagSet("plan save", flag.ContinueOnError)
	fs.SetOutput(stderr)
	id := fs.String("feature", "", "feature id")
	if err := fs.Parse(args[1:]); err != nil {
		return 1
	}
	if *id == "" {
		fmt.Fprintln(stderr, "nxy plan save: --feature is required")
		return 1
	}
	h, st := newHooksStore()
	defer st.Close()
	res, err := h.SavePlan(context.Background(), *id, stdin)
	if err != nil {
		fmt.Fprintf(stderr, "nxy plan save: %v\n", err)
		return 1
	}
	if len(res.Problems) > 0 {
		for _, p := range res.Problems {
			fmt.Fprintln(stderr, p.String())
		}
		return 1
	}
	fmt.Fprintf(stdout, "plan r%d saved; reply with one line\n", res.Rev)
	return 0
}

// runStatus is `nxy status` for the repo of the current directory.
func runStatus(_ []string, _ io.Reader, stdout, stderr io.Writer) int {
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "nxy status: %v\n", err)
		return 1
	}
	h, st := newHooksStore()
	defer st.Close()
	v, err := h.Status(context.Background(), dir)
	if err != nil {
		fmt.Fprintf(stderr, "nxy status: %v\n", err)
		return 1
	}
	if v.FeatureID == "" {
		fmt.Fprintln(stdout, "no active feature")
		return 0
	}
	fmt.Fprintf(stdout, "feature: %s\nrequest: %s\nstate: %s\nstep: %s\nnext: %s\n",
		v.FeatureID, v.Request, v.Public, v.Phase, v.Next)
	return 0
}

// runFeature is `nxy feature stop`.
func runFeature(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] != "stop" {
		fmt.Fprintln(stderr, "usage: nxy feature stop")
		return 1
	}
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "nxy feature stop: %v\n", err)
		return 1
	}
	h, st := newHooksStore()
	defer st.Close()
	f, warning, err := h.StopFeature(context.Background(), dir)
	if errors.Is(err, app.ErrNoActiveFeature) {
		fmt.Fprintln(stderr, "nxy: no active feature in this repo")
		return 1
	}
	if err != nil {
		fmt.Fprintf(stderr, "nxy feature stop: %v\n", err)
		return 1
	}
	if warning != nil {
		fmt.Fprintf(stderr, "nxy feature stop: warning: could not delete the plan file: %v\n", warning)
	}
	fmt.Fprintf(stdout, "feature %s stopped\n", f.ID)
	return 0
}
