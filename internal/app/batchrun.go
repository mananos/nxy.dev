package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/plan"
)

// acceptTailLines is how many trailing output lines of an Accept run are kept.
const acceptTailLines = 40

// RunKind says which proof `nxy batch` records.
type RunKind string

const (
	RunRed   RunKind = "red"
	RunGreen RunKind = "green"
)

// BatchRun is the outcome of RunBatchCheck. Refused is set when the run does not count
// (nothing recorded); Recorded is true when the proof was stored.
type BatchRun struct {
	Exit     int
	Tail     string
	Recorded bool
	Refused  string
}

// RunBatchCheck runs the Accept command of the current batch and records it as the red or
// green proof. The command runs outside any transaction.
func (h Hooks) RunBatchCheck(ctx context.Context, featureID string, kind RunKind) (BatchRun, error) {
	if h.Store == nil {
		return BatchRun{}, fmt.Errorf("no store")
	}
	if h.Shell == nil {
		return BatchRun{}, fmt.Errorf("no shell")
	}
	if kind != RunRed && kind != RunGreen {
		return BatchRun{}, fmt.Errorf("unknown run %q: use red or green", kind)
	}
	f, err := h.Store.Feature(ctx, featureID)
	if err != nil {
		return BatchRun{}, fmt.Errorf("feature %s: %w", featureID, err)
	}
	if f.State.Phase != domain.PhaseRunningBatch || f.Plan == nil {
		return BatchRun{}, fmt.Errorf("feature %s is not running a batch (state %s); next step %s", f.ID, f.State.Phase, f.Next.Label())
	}
	n := f.State.Batch
	if n < 1 || n > len(f.Plan.Batches) {
		return BatchRun{}, fmt.Errorf("feature %s: batch %d is not in the plan", f.ID, n)
	}
	command := f.Plan.Batches[n-1].Test.Accept
	if command == "" {
		return BatchRun{}, fmt.Errorf("batch %d has no Accept", n)
	}
	need := plan.NeedOf(*f.Plan, n)
	if kind == RunRed && !need.Red {
		return BatchRun{Refused: fmt.Sprintf("batch %d is not TDD: run green", n)}, nil
	}
	pr := f.Proof
	if pr == nil || pr.Batch != n {
		pr = &domain.Proof{Batch: n}
	}
	if kind == RunGreen && need.Red && (pr.Red == nil || pr.Red.Exit == 0) {
		return BatchRun{Refused: fmt.Sprintf("batch %d is TDD: run red first, with the test failing before the code", n)}, nil
	}
	edits := pr.Edits

	res, err := h.Shell.Run(ctx, f.ProjectDir, command)
	if err != nil {
		return BatchRun{}, fmt.Errorf("run Accept: %w", err)
	}
	out := BatchRun{Exit: res.Exit, Tail: tailLines(res.Output, acceptTailLines)}
	if kind == RunRed {
		switch {
		case res.TimedOut:
			out.Refused = "the Accept timed out: that is not proof the test fails"
			return out, nil
		case res.Exit == 0:
			out.Refused = "your test does not run, or the Accept passes without it"
			return out, nil
		case res.Exit == 127 || res.Exit == 9009:
			out.Refused = "Accept command not found"
			return out, nil
		}
	}

	run := &domain.Run{Exit: res.Exit, Tail: out.Tail, Edits: edits}
	_, applied, err := h.mutate(ctx, featureID, func(f *domain.Feature) bool {
		if f.State.Phase != domain.PhaseRunningBatch || f.State.Batch != n {
			return false
		}
		if f.Proof == nil || f.Proof.Batch != n {
			f.Proof = &domain.Proof{Batch: n}
		}
		if kind == RunRed {
			f.Proof.Red = run
		} else {
			f.Proof.Green = run
		}
		return true
	})
	if err != nil {
		return BatchRun{}, err
	}
	if !applied {
		out.Refused = "the feature moved on while the Accept ran: nothing recorded"
		return out, nil
	}
	out.Recorded = true
	return out, nil
}

// tailLines keeps the last n lines of s, without the trailing newline.
func tailLines(s string, n int) string {
	s = strings.TrimRight(s, "\r\n")
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
