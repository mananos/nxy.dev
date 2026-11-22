package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mananos/nxy.dev/v2/internal/affected"
	"github.com/mananos/nxy.dev/v2/internal/domain"
	"github.com/mananos/nxy.dev/v2/internal/flow"
	"github.com/mananos/nxy.dev/v2/internal/repos"
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
// PhaseAffectedFix or PhaseFixing it only checks: it runs and reports, and records nothing. In
// PhaseFixVerify (after the review fixes) it records like in PhaseAffected.
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
	check := f.State.Phase == domain.PhaseAffectedFix || f.State.Phase == domain.PhaseFixing
	if (f.State.Phase != domain.PhaseAffected && f.State.Phase != domain.PhaseFixVerify && !check) || f.Plan == nil {
		return AffectedResult{}, fmt.Errorf("feature %s is not at the affected tests (state %s); next step %s", f.ID, f.State.Phase, f.Next.Label())
	}
	res := AffectedResult{Check: check, Feature: f}

	rl := f.RepoList()
	multi := len(f.Repos) > 1
	var rec domain.AffectedRun
	var notes, reasons, fails, sources []string
	var perRepo []repoAffected
	for _, r := range rl {
		ra := h.affectedOfRepo(ctx, f, r, multi)
		perRepo = append(perRepo, ra)
	}
	for i, ra := range perRepo {
		pre := ""
		if multi {
			pre = rl[i].Name + ": "
		}
		for _, n := range ra.notes {
			notes = append(notes, pre+n)
		}
		if ra.skipped {
			continue
		}
		if ra.source != "" && !slices.Contains(sources, ra.source) {
			sources = append(sources, ra.source)
		}
		for _, t := range ra.tests {
			rec.Tests = append(rec.Tests, labelPath(rl[i].Name, t, multi))
		}
		for _, t := range ra.unmatched {
			rec.Unmatched = append(rec.Unmatched, labelPath(rl[i].Name, t, multi))
		}
		if ra.reason != "" {
			reasons = append(reasons, pre+ra.reason)
			continue
		}
		for _, c := range ra.cmds {
			cwd := repoDir(rl[i])
			if c.Dir != "" && c.Dir != "." {
				cwd = filepath.Join(cwd, filepath.FromSlash(c.Dir))
			}
			sr, err := h.Shell.Run(ctx, cwd, c.Line)
			if err != nil {
				return AffectedResult{}, fmt.Errorf("run %s: %w", c.Line, err)
			}
			tail := tailLines(sr.Output, acceptTailLines)
			if sr.Exit != 0 || sr.TimedOut {
				tail = failTail(sr.Output, acceptTailLines)
			}
			cr := domain.CommandRun{Dir: c.Dir, Command: c.Line, Exit: sr.Exit, Tail: tail, TimedOut: sr.TimedOut, Row: c.Row, Repo: c.Repo}
			if multi {
				cr.Dir, cr.RepoName = cwd, rl[i].Name
			}
			rec.Runs = append(rec.Runs, cr)
			tag := "`" + c.Line + "`"
			if multi {
				tag = "[" + rl[i].Name + "] " + tag
			}
			switch {
			case sr.TimedOut:
				fails = append(fails, fmt.Sprintf("%s (timed out):\n%s", tag, cr.Tail))
			case sr.Exit != 0:
				fails = append(fails, fmt.Sprintf("%s (exit %d):\n%s", tag, sr.Exit, cr.Tail))
			}
		}
	}
	switch len(sources) {
	case 0:
	case 1:
		rec.Source = sources[0]
	default:
		rec.Source = "varios"
	}
	switch {
	case len(fails) > 0:
		res.Verdict = AffectedRed
		rec.Failure = strings.Join(fails, "\n\n")
	case len(reasons) > 0:
		res.Verdict, res.Reason = AffectedUnverified, strings.Join(reasons, "; ")
		rec.Reason = res.Reason
	case multi && len(rec.Runs) == 0:
		res.Verdict, res.Reason = AffectedUnverified, "ningún repo tiene cambios: no hay nada que correr"
		rec.Reason = res.Reason
	default:
		res.Verdict = AffectedGreen
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
		if x.State.Phase != domain.PhaseAffected && x.State.Phase != domain.PhaseFixVerify {
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

// repoAffected is what RunAffected works out for one repo of the feature.
type repoAffected struct {
	skipped   bool // no changes: nothing to run (only with several repos)
	source    string
	tests     []string
	unmatched []string
	cmds      []affected.Command
	reason    string
	notes     []string
}

// labelPath puts the repo name in front of p when the feature has several repos.
func labelPath(name, p string, multi bool) string {
	if !multi || name == "" {
		return p
	}
	return name + "/" + p
}

// affectedOfRepo works out the tests and commands of one repo: rows, tree, changes since its base,
// codegraph, names, and its suite (the repo's own, or the plan's when it has one repo).
func (h Hooks) affectedOfRepo(ctx context.Context, f domain.Feature, r domain.FeatureRepo, multi bool) repoAffected {
	var out repoAffected
	dir := repoDir(r)
	if dir == "" {
		dir = f.ProjectDir
	}
	base := r.Base
	if base == "" {
		base = f.Base
	}
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
	if multi || len(f.Plan.Repos) > 0 {
		planPaths = planPathsOf(f, r.Name)
	}
	changed, fromGit := planPaths, false
	if base != "" && h.Changes != nil {
		if c, err := h.Changes.Changed(ctx, dir, base); err == nil {
			changed, fromGit = c, true
		}
	}
	if multi && len(changed) == 0 {
		out.skipped = true
		out.notes = append(out.notes, "sin cambios: no se corre nada")
		return out
	}

	notes := &out.notes
	if warn != "" {
		*notes = append(*notes, warn)
	}
	if len(unapproved) > 0 {
		*notes = append(*notes, "filas de .nxy/runners.json que no estaban en el plan aprobado, no se usaron: "+strings.Join(unapproved, ", "))
	}
	g := GraphResult{Status: GraphMissing}
	if h.Graph != nil {
		g = h.Graph.Affected(ctx, dir, changed)
	}
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
		out.source, tests = "codegraph", g.Tests
		if g.Status == GraphInitialized {
			*notes = append(*notes, fmt.Sprintf("índice de codegraph creado en %d s (.codegraph/, ignorado en .git/info/exclude)", int(g.Took.Seconds())))
		}
		if len(tests) == 0 {
			out.source, tests = "nombres", byName()
			*notes = append(*notes, "codegraph no encontró tests afectados; se buscaron por nombre")
		}
	default:
		out.source = "nombres"
		tests = byName()
		*notes = append(*notes, graphNote(g.Status))
	}
	out.tests = tests

	cmds, unmatched := affected.Commands(rows, tests, tree)
	out.unmatched = unmatched
	suite := strings.TrimSpace(f.Plan.Suite)
	if len(f.Plan.Repos) > 0 {
		suite = ""
		for _, pr := range f.Plan.Repos {
			if pr.Name == r.Name {
				suite = strings.TrimSpace(pr.Suite)
			}
		}
	}
	switch {
	case (len(tests) == 0 || len(unmatched) > 0 || len(cmds) == 0) && suite != "":
		out.source = "suite"
		cmds = []affected.Command{{Dir: ".", Line: suite}}
		if len(tests) == 0 {
			*notes = append(*notes, "ningún test afectado: se corre el Suite del plan")
		} else {
			*notes = append(*notes, "hay tests sin runner conocido: se corre el Suite del plan")
		}
	case len(tests) == 0:
		out.reason = "ningún test afectado y el plan no tiene Suite"
	case len(cmds) == 0:
		out.reason = "ningún runner conocido para los tests afectados y el plan no tiene Suite (agregá .nxy/runners.json o un suite)"
	case len(unmatched) > 0:
		*notes = append(*notes, "sin runner (no se corrieron): "+strings.Join(unmatched, ", "))
	}
	out.cmds = cmds
	return out
}

// planPathsOf are the files the plan changes in the repo called name, relative to the repo.
func planPathsOf(f domain.Feature, name string) []string {
	all := planChangePaths(f)
	if f.Plan == nil || len(f.Plan.Repos) == 0 {
		return all
	}
	names := featureRepoNames(f)
	var out []string
	for _, p := range all {
		if n, rel, ok := repos.Split(p, names); ok && n == name {
			out = append(out, rel)
		}
	}
	return out
}

// featureRepoNames are the names of the repos of f.
func featureRepoNames(f domain.Feature) []string {
	var names []string
	for _, r := range f.Repos {
		names = append(names, r.Name)
	}
	return names
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
