package app

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/mananos/nxy.dev/v2/internal/domain"
	"github.com/mananos/nxy.dev/v2/internal/plan"
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
	// Warn is a notice for a recorded red that came after code was written.
	Warn string
}

// RunBatchCheck runs the Accept command of the current batch and records it as the red or
// green proof. The command runs outside any transaction.
func (h Hooks) RunBatchCheck(ctx context.Context, featureID string, kind RunKind, batch int) (BatchRun, error) {
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
	n := batch
	running := f.State.Running()
	if n == 0 {
		switch len(running) {
		case 0:
			n = f.State.Batch
		case 1:
			n = running[0]
		default:
			return BatchRun{}, fmt.Errorf("several batches are running (%s): pass --batch", domain.JoinInts(running, "and"))
		}
	} else if len(running) > 0 && !slices.Contains(running, n) {
		return BatchRun{}, fmt.Errorf("batch %d is not running (running: %s)", n, domain.JoinInts(running, "and"))
	}
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
	pv := f.ProofOf(n)
	pr := &pv
	if kind == RunGreen && need.Red && (pr.Red == nil || pr.Red.Exit == 0) {
		return BatchRun{Refused: fmt.Sprintf("batch %d is TDD: run red first, with the test failing before the code", n)}, nil
	}
	edits := pr.Edits

	res, err := h.Shell.Run(ctx, batchDir(f, n), command)
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

	if kind == RunGreen && res.Exit == 0 {
		if bad := h.unformatted(ctx, f, n); len(bad) > 0 {
			out.Refused = fmt.Sprintf("Go files are not gofmt-formatted: %s; run gofmt -w %s and run green again", strings.Join(bad, ", "), strings.Join(bad, " "))
			return out, nil
		}
	}

	run := &domain.Run{Exit: res.Exit, Tail: out.Tail, Edits: edits}
	if kind == RunRed && len(pr.CodeFirst) > 0 {
		run.CodeFirst = slices.Clone(pr.CodeFirst)
		out.Warn = fmt.Sprintf("code written before the red (%s): the reviewer will check that your test fails without it", strings.Join(run.CodeFirst, ", "))
	}
	_, applied, err := h.mutate(ctx, featureID, func(f *domain.Feature) bool {
		if f.State.Phase != domain.PhaseRunningBatch {
			return false
		}
		if r := f.State.Running(); len(r) > 0 {
			if !slices.Contains(r, n) {
				return false
			}
		} else if f.State.Batch != n {
			return false
		}
		p := f.ProofOf(n)
		if kind == RunRed {
			p.Red = run
		} else {
			p.Green = run
		}
		f.SetProof(p)
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

// unformatted lists the .go files changed since the base of batch n's repo that gofmt
// would change. Without a Formatter, a base or git it finds nothing.
func (h Hooks) unformatted(ctx context.Context, f domain.Feature, n int) []string {
	if h.Format == nil || h.Changes == nil {
		return nil
	}
	r := batchRepo(f, n)
	base := r.Base
	if base == "" {
		base = f.Base
	}
	if base == "" {
		return nil
	}
	dir := batchDir(f, n)
	changed, err := h.Changes.Changed(ctx, dir, base)
	if err != nil {
		return nil
	}
	var gos []string
	for _, p := range changed {
		if strings.HasSuffix(p, ".go") {
			gos = append(gos, p)
		}
	}
	if len(gos) == 0 {
		return nil
	}
	bad, err := h.Format.Unformatted(ctx, dir, gos)
	if err != nil {
		return nil
	}
	return bad
}

// batchRepo is the repo batch n works in: the one its changes name, or the first.
func batchRepo(f domain.Feature, n int) domain.FeatureRepo {
	if f.Plan != nil && n >= 1 && n <= len(f.Plan.Batches) {
		if r, ok := f.RepoByName(f.Plan.Batches[n-1].Repo); ok {
			return r
		}
	}
	return f.RepoList()[0]
}

// batchDir is the folder the Accept of batch n runs in.
func batchDir(f domain.Feature, n int) string {
	if d := repoDir(batchRepo(f, n)); d != "" {
		return d
	}
	return f.ProjectDir
}

// maxFailLines caps the failure lines failTail lifts out of a long output.
const maxFailLines = 20

var failMarkers = []string{"FAIL", "ERROR", "panic:", "●", "✕"}

// failTail is tailLines plus, above the last n lines, the lines of the whole
// output that mark a failure (up to maxFailLines), so a failing package that
// scrolled out of the tail stays visible.
func failTail(s string, n int) string {
	s = strings.TrimRight(s, "\r\n")
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return strings.Join(lines, "\n")
	}
	cut := len(lines) - n
	var fails []string
	for _, l := range lines[:cut] {
		for _, m := range failMarkers {
			if strings.Contains(l, m) {
				fails = append(fails, l)
				break
			}
		}
	}
	if len(fails) > maxFailLines {
		fails = fails[len(fails)-maxFailLines:]
	}
	head := append([]string{cutLine(cut)}, fails...)
	return strings.Join(append(head, lines[cut:]...), "\n")
}

// tailLines keeps the last n lines of s, without the trailing newline.
func tailLines(s string, n int) string {
	s = strings.TrimRight(s, "\r\n")
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		cut := len(lines) - n
		lines = append([]string{cutLine(cut)}, lines[cut:]...)
	}
	return strings.Join(lines, "\n")
}

// cutLine is the first line of a tail that dropped cut earlier lines.
func cutLine(cut int) string {
	return fmt.Sprintf("… %d líneas anteriores recortadas", cut)
}
