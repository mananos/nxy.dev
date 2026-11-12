package main

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/mananos/nxy.dev/internal/affected"
	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/platform"
	"github.com/mananos/nxy.dev/internal/tools"
	"github.com/mananos/nxy.dev/internal/views"
)

// Sources of the impact map, weakest first.
const (
	srcNone      = "none"
	srcName      = "name"
	srcCodegraph = "codegraph"
)

func impactRank(s string) int {
	switch s {
	case srcCodegraph:
		return 2
	case srcName:
		return 1
	}
	return 0
}

// repoTarget is one repo of a feature as the plan screens read it: where it is and what it is
// compared against.
type repoTarget struct {
	name, key, dir, base string
}

// repoTargets lists the repos of f. The base is the one of the repo, else the feature's, else HEAD.
func repoTargets(f domain.Feature) []repoTarget {
	var out []repoTarget
	for _, r := range f.RepoList() {
		name := r.Name
		dir := r.Checkout
		if dir == "" {
			dir = r.Root
		}
		if name == "" {
			name = path.Base(strings.TrimRight(filepath.ToSlash(r.Root), "/"))
		}
		base := r.Base
		if base == "" {
			base = f.Base
		}
		if base == "" {
			base = "HEAD"
		}
		out = append(out, repoTarget{name: name, key: r.Key, dir: dir, base: base})
	}
	return out
}

// planFiles lists the files the plan changes, per repo name, in plan order.
func planFiles(f domain.Feature, ts []repoTarget) map[string][]string {
	out := map[string][]string{}
	if f.Plan == nil {
		return out
	}
	names := views.RepoNames(f)
	seen := map[string]bool{}
	for _, pb := range f.Plan.Batches {
		for _, c := range pb.Change {
			r, p, _, _ := views.ParseAt(c.At, names)
			if p == "" {
				continue
			}
			r = views.RepoOfChange(f, pb, r)
			k := views.FileKey(r, p)
			if seen[k] {
				continue
			}
			seen[k] = true
			out[r] = append(out[r], p)
		}
	}
	return out
}

// rawImpact is what was found about one file; the pass/fail of its tests is worked out on each read.
type rawImpact struct {
	exists     bool
	importedBy []string
	tests      []string
}

// planImpact is the impact map of a plan: the weakest source of the repos it touches and each file.
type planImpact struct {
	source string
	files  map[string]rawImpact
}

// repoSource is how the tests and importers of a repo are found: the code index when there is one,
// else by name when rg is there, else not at all.
func (v *viewSource) repoSource(dir string) string {
	ts := v.tools()
	switch {
	case ts.Indexed != nil && ts.Indexed(dir):
		return srcCodegraph
	case ts.RgFound != nil && ts.RgFound():
		return srcName
	}
	return srcNone
}

// probe finds the tests of a file and the places that mention its module. all is the file list of
// the repo (for the by-name tests).
func (v *viewSource) probe(ctx context.Context, dir, source, file string, all []string) (tests []string, refs []tools.RgRow) {
	vt := v.tools()
	switch source {
	case srcCodegraph:
		if vt.Affected != nil {
			if ts, ok := vt.Affected(ctx, dir, []string{file}); ok {
				tests = ts
			}
		}
	case srcName:
		tests = affected.ByName(affected.Builtin(), []string{file}, all)
	}
	if source != srcNone && vt.RgFound != nil && vt.RgFound() && vt.Grep != nil {
		mod := strings.TrimSuffix(path.Base(file), path.Ext(file))
		if mod != "" {
			for _, r := range vt.Grep(ctx, dir, mod) {
				if r.Path != file {
					refs = append(refs, r)
				}
			}
		}
	}
	return tests, refs
}

func (v *viewSource) filesOf(ctx context.Context, dir, source string) []string {
	if source != srcName {
		return nil
	}
	all, _ := platform.GitFiles(ctx, dir)
	return all
}

// impactOf looks at every file of the plan: whether it exists at the base, who uses it and its tests.
// It is kept by feature, plan revision and HEAD of each repo: a view never recomputes it for nothing.
func (v *viewSource) impactOf(ctx context.Context, f domain.Feature) planImpact {
	ts := repoTargets(f)
	key := fmt.Sprintf("%s|%d", f.ID, f.PlanRev)
	for _, t := range ts {
		key += "|" + platform.GitHead(t.dir)
	}
	v.mu.Lock()
	if c, ok := v.impacts[key]; ok {
		v.mu.Unlock()
		return c
	}
	v.mu.Unlock()

	imp := planImpact{files: map[string]rawImpact{}}
	least, touched := 99, false
	byRepo := planFiles(f, ts)
	for _, t := range ts {
		list := byRepo[t.name]
		if len(list) == 0 {
			continue
		}
		touched = true
		src := v.repoSource(t.dir)
		if r := impactRank(src); r < least {
			least, imp.source = r, src
		}
		all := v.filesOf(ctx, t.dir, src)
		for _, p := range list {
			ri := rawImpact{}
			if _, ok, err := platform.GitFileAt(ctx, t.dir, t.base, p); err == nil {
				ri.exists = ok
			} else if _, serr := os.Stat(filepath.Join(t.dir, filepath.FromSlash(p))); serr == nil {
				ri.exists = true
			}
			tests, refs := v.probe(ctx, t.dir, src, p, all)
			ri.tests = tests
			seen := map[string]bool{}
			for _, r := range refs {
				if b := path.Base(r.Path); !seen[b] {
					seen[b] = true
					ri.importedBy = append(ri.importedBy, b)
				}
			}
			imp.files[views.FileKey(t.name, p)] = ri
		}
	}
	if !touched {
		imp.source = srcNone
	}
	v.mu.Lock()
	if v.impacts == nil {
		v.impacts = map[string]planImpact{}
	}
	v.impacts[key] = imp
	v.mu.Unlock()
	return imp
}

// testMatch says two test paths name the same file (one may carry a repo prefix).
func testMatch(a, b string) bool {
	return a == b || strings.HasSuffix(a, "/"+b) || strings.HasSuffix(b, "/"+a)
}

// mentions says name appears in text as a whole word: after the start, a space, a slash or a quote,
// and before the end, a colon, a space or a closing mark (so NotaTest does not match NotaTestHelper).
func mentions(text, name string) bool {
	if name == "" {
		return false
	}
	for from := 0; ; {
		i := strings.Index(text[from:], name)
		if i < 0 {
			return false
		}
		i += from
		end := i + len(name)
		if (i == 0 || strings.ContainsRune(" \t\r\n/\\\"'(", rune(text[i-1]))) &&
			(end == len(text) || strings.ContainsRune(": \t\r\n)\"',", rune(text[end]))) {
			return true
		}
		from = i + 1
	}
}

// testRefs gives each test its state from what the feature already ran: fail when a failed batch or
// the affected run names it, pass when the green affected run includes it, else none.
func testRefs(f domain.Feature, tests []string) []views.TestRef {
	failedStep := map[int]bool{}
	for _, st := range f.State.Steps {
		if st.Status == domain.StepFailed {
			failedStep[st.N] = true
		}
	}
	var fail strings.Builder
	for _, p := range f.Proofs {
		if failedStep[p.Batch] {
			fail.WriteString(p.Failure + "\n")
		}
	}
	var green []string
	if a := f.Affected; a != nil {
		if a.Failure != "" {
			fail.WriteString(a.Failure)
		} else {
			green = a.Tests
		}
	}
	text := fail.String()
	out := []views.TestRef{}
	for _, t := range tests {
		st := views.TestNone
		if strings.TrimSpace(text) != "" && (mentions(text, t) || mentions(text, path.Base(t))) {
			st = views.TestFail
		} else {
			for _, g := range green {
				if testMatch(g, t) {
					st = views.TestPass
					break
				}
			}
		}
		out = append(out, views.TestRef{Path: t, Status: st})
	}
	return out
}

// Plan is the plan screen: the batches and the impact map, with codegraph or rg when there are.
func (v *viewSource) Plan(ctx context.Context, featureID string) (views.Plan, bool, error) {
	row, err := v.row(ctx, featureID)
	if err != nil {
		return views.Plan{}, false, err
	}
	f := row.Feature
	if f.Plan == nil {
		return views.Plan{}, false, nil
	}
	fd, err := v.featureData(ctx, row)
	if err != nil {
		return views.Plan{}, false, err
	}
	imp := v.impactOf(ctx, f)
	in := views.PlanIn{FeatureIn: fd.featureIn(v.now()), Source: imp.source, Files: map[string]views.FileImpact{}}
	for k, r := range imp.files {
		in.Files[k] = views.FileImpact{Exists: r.exists, ImportedBy: r.importedBy, Tests: testRefs(f, r.tests)}
	}
	p, ok := views.BuildPlan(in)
	return p, ok, nil
}
