package tools

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"time"

	"github.com/mananos/nxy.dev/internal/setup"
)

// stepTimeout bounds each step of an install plan.
const stepTimeout = 300 * time.Second

// HasBin is true when `bin --version` can be started (the binary is on PATH); its exit code does not matter.
func HasBin(bin string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, bin, "--version")
	hideWindow(c)
	err := c.Run()
	if ctx.Err() != nil {
		return false
	}
	var ee *exec.ExitError
	return err == nil || errors.As(err, &ee)
}

// RunPlan runs the removes and then the steps of a plan in order, stopping at the first failure.
// It is the only place that installs anything, and only when the user asked for it.
func RunPlan(plan setup.Plan) setup.RunResult {
	return setup.Run(plan, runStep)
}

func runStep(s setup.Step) setup.SpawnResult {
	ctx, cancel := context.WithTimeout(context.Background(), stepTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, s.File, s.Args...)
	hideWindow(c)
	var stdout, stderr bytes.Buffer
	c.Stdout, c.Stderr = &stdout, &stderr
	err := c.Run()
	r := setup.SpawnResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if err == nil {
		return r
	}
	var ee *exec.ExitError
	switch {
	case ctx.Err() != nil:
		r.Err = "spawnSync " + s.File + " ETIMEDOUT"
		r.Status = -1
	case errors.As(err, &ee):
		r.Status = ee.ExitCode()
		if r.Status == 0 {
			r.Status = -1
		}
	default:
		r.Err = err.Error()
		r.Status = -1
	}
	return r
}
