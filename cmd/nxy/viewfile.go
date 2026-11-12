package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/platform"
	"github.com/mananos/nxy.dev/internal/store/sqlite"
	"github.com/mananos/nxy.dev/internal/views"
)

// maxFileView is the most of a file the screen reads; the rest is cut.
var maxFileView = 1 << 20

// confinedPath cleans a repo-relative path and refuses the ones that leave the repo: absolute, with a
// drive, or going up with "..".
func confinedPath(p string) (string, error) {
	if p == "" || strings.HasPrefix(p, "/") || filepath.IsAbs(p) || filepath.VolumeName(p) != "" {
		return "", fmt.Errorf("file: %q is not a path inside the repo", p)
	}
	c := path.Clean(p)
	if c == "." || c == ".." || strings.HasPrefix(c, "../") {
		return "", fmt.Errorf("file: %q is not a path inside the repo", p)
	}
	return c, nil
}

// insideDir says the real place of file (symlinks resolved) is under dir.
func insideDir(dir, file string) bool {
	rd, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false
	}
	rf, err := filepath.EvalSymlinks(file)
	if err != nil {
		return true // not there: nothing to leak
	}
	rel, err := filepath.Rel(rd, rf)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolveFile reads --path as the plan writes it: with the repo name in front when there are several
// repos, else relative to the only one. Without a prefix it is the repo whose batch changes it.
func resolveFile(f domain.Feature, arg string, ts []repoTarget) (repoTarget, string, error) {
	if len(ts) == 0 {
		return repoTarget{}, "", fmt.Errorf("feature %s: no repo", f.ID)
	}
	name, p, _, _ := views.ParseAt(arg, views.RepoNames(f))
	if p == "" {
		return repoTarget{}, "", fmt.Errorf("file: empty path")
	}
	p, err := confinedPath(p)
	if err != nil {
		return repoTarget{}, "", err
	}
	if name == "" && f.Plan != nil {
		names := views.RepoNames(f)
	find:
		for _, pb := range f.Plan.Batches {
			for _, c := range pb.Change {
				r, cp, _, _ := views.ParseAt(c.At, names)
				if cp == p {
					name = views.RepoOfChange(f, pb, r)
					break find
				}
			}
		}
	}
	if name == "" {
		return ts[0], p, nil
	}
	for _, t := range ts {
		if t.name == name {
			return t, p, nil
		}
	}
	return repoTarget{}, "", fmt.Errorf("feature %s: no repo %q", f.ID, name)
}

// readCapped reads at most maxFileView bytes of a file; nil when it does not exist. cut says the file
// was longer.
func readCapped(p string) (b *[]byte, cut bool) {
	fh, err := os.Open(p)
	if err != nil {
		return nil, false
	}
	defer fh.Close()
	if info, err := fh.Stat(); err != nil || info.IsDir() {
		return nil, false
	}
	data, err := io.ReadAll(io.LimitReader(fh, int64(maxFileView)+1))
	if err != nil {
		return nil, false
	}
	if len(data) > maxFileView {
		data, cut = data[:maxFileView], true
	}
	return &data, cut
}

func hasNUL(b []byte) bool {
	if len(b) > 8<<10 {
		b = b[:8<<10]
	}
	return bytes.IndexByte(b, 0) >= 0
}

// File is the screen of one file of a feature: the diff against the base, who uses it, its tests and
// the memory anchored to it.
func (v *viewSource) File(ctx context.Context, featureID, pathArg string) (views.File, error) {
	row, err := v.row(ctx, featureID)
	if err != nil {
		return views.File{}, err
	}
	f := row.Feature
	t, p, err := resolveFile(f, pathArg, repoTargets(f))
	if err != nil {
		return views.File{}, err
	}
	in := views.FileIn{FeatureIn: featData{row: row}.featureIn(v.now()), Repo: t.name, Path: p}
	if before, ok, err := platform.GitFileAt(ctx, t.dir, t.base, p); err == nil && ok {
		if len(before) > maxFileView {
			in.TooBig = true
			before = before[:maxFileView]
		}
		in.Before = &before
		in.Binary = hasNUL([]byte(before))
	}
	full := filepath.Join(t.dir, filepath.FromSlash(p))
	if !insideDir(t.dir, full) {
		return views.File{}, fmt.Errorf("file: %q is not a path inside the repo", pathArg)
	}
	if b, cut := readCapped(full); b != nil {
		s := string(*b)
		in.After = &s
		in.Binary = in.Binary || hasNUL(*b)
		in.TooBig = in.TooBig || cut
	}
	if in.Before == nil && in.After == nil && !slices.Contains(planFiles(f, repoTargets(f))[t.name], p) {
		return views.File{}, fmt.Errorf("file: %q is not in the plan, at the base or in the tree", pathArg)
	}

	src := v.repoSource(t.dir)
	tests, refs := v.probe(ctx, t.dir, src, p, v.filesOf(ctx, t.dir, src))
	in.Source = src
	in.Tests = testRefs(f, tests)
	for _, r := range refs {
		in.ImportedBy = append(in.ImportedBy, views.FileRef{Path: r.Path, Line: r.Line})
	}

	if in.Rules, err = v.store.Rules(ctx, t.key); err != nil {
		return views.File{}, err
	}
	if in.Closed, in.Learnings, err = v.store.Related(ctx, t.key, []string{p}); err != nil {
		return views.File{}, err
	}
	all, err := v.store.FeatureRows(ctx, sqlite.FeatureFilter{}, 0)
	if err != nil {
		return views.File{}, err
	}
	for _, r := range all {
		o := r.Feature
		if o.ID == f.ID || o.State.Phase == domain.PhaseDone || o.Plan == nil {
			continue
		}
		for _, or := range o.RepoList() {
			if or.Key == t.key {
				in.Active = append(in.Active, o)
				break
			}
		}
	}
	return views.BuildFile(in), nil
}
