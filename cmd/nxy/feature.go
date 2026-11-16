package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mananos/nxy.dev/internal/app"
	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/memory"
)

// runPlan is `nxy plan save --feature <id>`: the plan JSON comes on stdin.
// `nxy plan gaps --feature <id>` sends design holes back to the design.
func runPlan(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "gaps" {
		return runPlanGaps(args[1:], stdin, stdout, stderr)
	}
	if len(args) == 0 || args[0] != "save" {
		fmt.Fprintln(stderr, "usage: nxy plan save|gaps --feature <id>  (JSON on stdin)")
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
	if res.Blocked != "" {
		fmt.Fprintln(stderr, res.Blocked)
		return 1
	}
	fmt.Fprintf(stdout, "plan r%d saved; reply with exactly this line, nothing else\n", res.Rev)
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
	if v.Paused {
		fmt.Fprintln(stdout, "paused: yes")
	}
	for _, r := range v.Repos {
		fmt.Fprintf(stdout, "repo: %s\n", r)
	}
	if v.Progress != "" {
		fmt.Fprintf(stdout, "batch: %s\n", v.Progress)
	}
	if v.Failure != "" {
		fmt.Fprintf(stdout, "last failure: %s\n", v.Failure)
	}
	for _, d := range v.Decisions {
		fmt.Fprintf(stdout, "decision: %s\n", d)
	}
	return 0
}

const featureRuleUsage = `usage: nxy feature rule "<text>" --question "<what it settles>"`

// featureRule is `nxy feature rule`: it raises a rule candidate (origin chat) on the active feature;
// the closing question offers it, nothing is saved now.
func featureRule(args []string, stdout, stderr io.Writer) int {
	opts, pos, _ := memArgs(args)
	if len(pos) == 0 || optVal(opts, "question") == "" {
		fmt.Fprintln(stderr, featureRuleUsage)
		return 1
	}
	h, st := newHooksStore()
	defer st.Close()
	f, err := h.AddRuleCandidate(context.Background(), memCwd(opts), strings.Join(pos, " "), optVal(opts, "question"), memory.OriginChat)
	if errors.Is(err, app.ErrNoActiveFeature) {
		fmt.Fprintln(stderr, "no active feature: use nxy mem rule")
		return 1
	}
	if err != nil {
		fmt.Fprintf(stderr, "nxy feature rule: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "rule candidate noted for feature %s: nxy asks about it when the feature closes\n", f.ID)
	return 0
}

// runFeature is `nxy feature list|new|pause|resume|close` (worktree.go), `nxy feature rule` and `nxy feature stop`.
func runFeature(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	action := "list"
	if len(args) > 0 {
		action = args[0]
		args = args[1:]
	}
	if code, handled := featureWorktrees(action, args, stdout, stderr); handled {
		return code
	}
	if action == "rule" {
		return featureRule(args, stdout, stderr)
	}
	byID := ""
	if action == "stop" && len(args) == 2 && args[0] == "--feature" && args[1] != "" {
		byID, args = args[1], nil
	}
	if action != "stop" || len(args) != 0 {
		fmt.Fprintln(stderr, featureUsage)
		return 1
	}
	h, st := newHooksStore()
	defer st.Close()
	var f domain.Feature
	var warning error
	var err error
	if byID != "" {
		f, warning, err = h.StopFeatureByID(context.Background(), byID)
	} else {
		var dir string
		if dir, err = os.Getwd(); err != nil {
			fmt.Fprintf(stderr, "nxy feature stop: %v\n", err)
			return 1
		}
		f, warning, err = h.StopFeature(context.Background(), dir)
	}
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
