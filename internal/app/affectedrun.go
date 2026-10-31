package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mananos/nxy.dev/internal/affected"
	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/flow"
)

// Verdicts of RunAffected.
const (
	AffectedGreen      = "green"
	AffectedRed        = "red"
	AffectedUnverified = "unverified"
)

// AffectedResult is the outcome of RunAffected.
type AffectedResult struct {
	// Verdict is AffectedGreen, AffectedRed or AffectedUnverified.
	Verdict string
	// Reason is why the run is unverified.
	Reason string
	// Record is what was worked out and run (the same thing stored on the feature).
	Record domain.AffectedRun
	// Check is true when the feature is in the fix phase: the run only reports.
	Check bool
	// Recorded is true when the verdict was stored and the flow moved.
	Recorded bool
	// Refused is set when the run could not be recorded (the feature moved on).
	Refused string
	// Feature is the stored feature after the run.
	Feature domain.Feature
}

type emptyTree struct{}

func (emptyTree) Read(string) ([]byte, bool) { return nil, false }

// RunAffected works out the tests the feature affects, runs them and records the verdict. In
// PhaseAffectedFix it only checks: it runs and reports, and records nothing.
func (h Hooks) RunAffected(ctx context.Context, id string) (AffectedResult, error) {
	if h.Store == nil {
		return AffectedResult{}, errors.New("no store")
	}
	if h.Shell == nil {
		return AffectedResult{}, errors.New("no shell")
	}
	f, err := h.Store.Feature(ctx, id)
	if err != nil {
		return AffectedResult{}, fmt.Errorf("feature %s: %w", id, err)
	}
	check := f.State.Phase == domain.PhaseAffectedFix
	if (f.State.Phase != domain.PhaseAffected && !check) || f.Plan == nil {
		return AffectedResult{}, fmt.Errorf("feature %s is not at the affected tests (state %s); next step %s", f.ID, f.State.Phase, f.Next.Label())
	}
	dir := f.ProjectDir
	res := AffectedResult{Check: check, Feature: f}

	rows := affected.Builtin()
	warn := ""
	if h.Runners != nil {
		rows, warn = h.Runners.Rows(dir)
	}
	rows, unapproved := approvedRows(rows, f.Plan.RepoRunners)
	var tree affected.Tree = emptyTree{}
	if h.Trees != nil {
		tree = h.Trees.Tree(dir)
	}

	planPaths := planChangePaths(f)
	changed, fromGit := planPaths, false
	if f.Base != "" && h.Changes != nil {
		if c, err := h.Changes.Changed(ctx, dir, f.Base); err == nil {
			changed, fromGit = c, true
		}
	}

	var notes []string
	if warn != "" {
		notes = append(notes, warn)
	}
	if len(unapproved) > 0 {
		notes = append(notes, "filas de .nxy/runners.json que no estaban en el plan aprobado, no se usaron: "+strings.Join(unapproved, ", "))
	}
	g := GraphResult{Status: GraphMissing}
	if h.Graph != nil {
		g = h.Graph.Affected(ctx, dir, changed)
	}
	var rec domain.AffectedRun
	var tests []string
	byName := func() []string {
		all := planPaths
		if fromGit && h.Changes != nil {
			if fs, err := h.Changes.Files(ctx, dir); err == nil {
				all = fs
			}
		}
		return affected.ByName(rows, changed, all)
	}
	switch g.Status {
	case GraphOK, GraphInitialized:
		rec.Source, tests = "codegraph", g.Tests
		if g.Status == GraphInitialized {
			notes = append(notes, fmt.Sprintf("índice de codegraph creado en %d s (.codegraph/, ignorado en .git/info/exclude)", int(g.Took.Seconds())))
		}
		if len(tests) == 0 {
			rec.Source, tests = "nombres", byName()
			notes = append(notes, "codegraph no encontró tests afectados; se buscaron por nombre")
		}
	default:
		rec.Source = "nombres"
		tests = byName()
		notes = append(notes, graphNote(g.Status))
	}
	rec.Tests = tests

	cmds, unmatched := affected.Commands(rows, tests, tree)
	rec.Unmatched = unmatched
	suite := strings.TrimSpace(f.Plan.Suite)
	reason := ""
	switch {
	case (len(tests) == 0 || len(unmatched) > 0 || len(cmds) == 0) && suite != "":
		rec.Source = "suite"
		cmds = []affected.Command{{Dir: ".", Line: suite}}
		if len(tests) == 0 {
			notes = append(notes, "ningún test afectado: se corre el Suite del plan")
		} else {
			notes = append(notes, "hay tests sin runner conocido: se corre el Suite del plan")
		}
	case len(tests) == 0:
		reason = "ningún test afectado y el plan no tiene Suite"
	case len(cmds) == 0:
		reason = "ningún runner conocido para los tests afectados y el plan no tiene Suite (agregá .nxy/runners.json o un suite)"
	case len(unmatched) > 0:
		notes = append(notes, "sin runner (no se corrieron): "+strings.Join(unmatched, ", "))
	}

	if reason != "" {
		res.Verdict, res.Reason = AffectedUnverified, reason
		rec.Reason = reason
	} else {
		var fails []string
		for _, c := range cmds {
			cwd := dir
			if c.Dir != "" && c.Dir != "." {
				cwd = filepath.Join(dir, filepath.FromSlash(c.Dir))
			}
			sr, err := h.Shell.Run(ctx, cwd, c.Line)
			if err != nil {
				return AffectedResult{}, fmt.Errorf("run %s: %w", c.Line, err)
			}
			cr := domain.CommandRun{Dir: c.Dir, Command: c.Line, Exit: sr.Exit, Tail: tailLines(sr.Output, acceptTailLines), TimedOut: sr.TimedOut, Row: c.Row, Repo: c.Repo}
			rec.Runs = append(rec.Runs, cr)
			switch {
			case sr.TimedOut:
				fails = append(fails, fmt.Sprintf("`%s` (timed out):\n%s", c.Line, cr.Tail))
			case sr.Exit != 0:
				fails = append(fails, fmt.Sprintf("`%s` (exit %d):\n%s", c.Line, sr.Exit, cr.Tail))
			}
		}
		if len(fails) > 0 {
			res.Verdict = AffectedRed
			rec.Failure = strings.Join(fails, "\n\n")
		} else {
			res.Verdict = AffectedGreen
		}
	}
	rec.Note = strings.Join(notes, "; ")
	res.Record = rec
	if check {
		return res, nil
	}

	kind := flow.Green
	switch res.Verdict {
	case AffectedRed:
		kind = flow.Red
	case AffectedUnverified:
		kind = flow.Unverified
	}
	nf, _, err := h.advanceWith(ctx, id, flow.Event{Kind: kind}, func(x *domain.Feature) error {
		if x.State.Phase != domain.PhaseAffected {
			return errNoChange
		}
		r := rec
		x.Affected = &r
		return nil
	})
	if errors.Is(err, errNoChange) {
		res.Refused = "the feature moved on: nothing recorded"
		return res, nil
	}
	if err != nil {
		return AffectedResult{}, err
	}
	res.Recorded, res.Feature = true, nf
	return res, nil
}

// approvedRows keeps the repo rows whose (name, command) the approved plan lists; the others are
// dropped (the builtin row of the same name takes over) and their names are returned.
func approvedRows(rows []affected.Runner, approved []domain.RepoRunner) ([]affected.Runner, []string) {
	var keep []affected.Runner
	var dropped []string
	for _, r := range rows {
		if !r.Repo {
			continue
		}
		ok := false
		for _, a := range approved {
			if a.Name == r.Name && a.Command == r.Command {
				ok = true
				break
			}
		}
		if ok {
			keep = append(keep, r)
		} else {
			dropped = append(dropped, r.Name)
		}
	}
	if len(dropped) == 0 {
		return rows, nil
	}
	return affected.Merge(keep, affected.Builtin()), dropped
}

// graphNote is the Spanish reason the tests were picked by name.
func graphNote(s GraphStatus) string {
	switch s {
	case GraphInitFailed:
		return "afectados por nombre: sin índice de codegraph (no se pudo crear; se reintenta en la próxima feature)"
	case GraphInitTimeout:
		return "afectados por nombre: sin índice de codegraph (no se pudo crear en 90 s; se reintenta en la próxima feature)"
	case GraphLocked:
		return "afectados por nombre: el índice de codegraph está bloqueado"
	case GraphSyncFailed, GraphQueryFailed:
		return "afectados por nombre: codegraph no respondió (sync o consulta fallida)"
	}
	return "afectados por nombre: codegraph no está instalado — instalalo con /nxy:setup"
}

// planChangePaths are the files the plan says it changes, in order, without repeats.
func planChangePaths(f domain.Feature) []string {
	var out []string
	seen := map[string]bool{}
	if f.Plan == nil {
		return nil
	}
	for _, b := range f.Plan.Batches {
		for _, c := range b.Change {
			p := filepath.ToSlash(changePath(c.At))
			if p != "" && !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}
