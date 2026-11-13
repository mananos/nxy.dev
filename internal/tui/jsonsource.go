package tui

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/mananos/nxy.dev/internal/views"
)

// jsonSource is a views.Source over the JSON files of the contract: the demo (testdata/views) and
// the golden of the screens (testdata/golden/views).
type jsonSource struct{ d *Data }

// NewJSONSource reads dir/*.json from fsys. The part of the file name before the first dot picks
// the struct; an unknown view or an unknown field is an error.
func NewJSONSource(fsys fs.FS, dir string) (views.Source, error) {
	return newJSONSource(fsys, dir)
}

func newJSONSource(fsys fs.FS, dir string) (*jsonSource, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	d := newData()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		id, _, _ := strings.Cut(name, ".")
		v := views.New(id)
		if v == nil {
			return nil, fmt.Errorf("%s: unknown view %q", name, id)
		}
		data, err := fs.ReadFile(fsys, path.Join(dir, name))
		if err != nil {
			return nil, err
		}
		if err := views.Decode(data, v); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		switch x := v.(type) {
		case *views.Cockpit:
			d.Cockpit = x
		case *views.Agents:
			d.Agents[x.Feature.ID] = x
		case *views.Plan:
			d.Plans[x.Feature.ID] = x
		case *views.Decision:
			d.Decisions[x.FeatureID] = x
		case *views.Engineer:
			d.Engineer[x.Feature.ID] = x
		case *views.Memory:
			d.Memory[x.Repo.Name] = x
		case *views.File:
			d.Files[x.Repo+"/"+x.Path] = x
		case *views.Stats:
			d.Stats = x
		case *views.Trend:
			d.Trend = x
		case *views.Config:
			d.Config = x
		}
	}
	if d.Cockpit == nil {
		return nil, fmt.Errorf("%s: cockpit.json is missing", dir)
	}
	return &jsonSource{d: d}, nil
}

var errNoView = errors.New("no view for it in the data")

func (s *jsonSource) Cockpit(context.Context) (views.Cockpit, error) { return *s.d.Cockpit, nil }

func (s *jsonSource) Agents(_ context.Context, id string) (views.Agents, error) {
	if a := s.d.Agents[id]; a != nil {
		return *a, nil
	}
	return views.Agents{}, errNoView
}

func (s *jsonSource) Decision(_ context.Context, id string) (views.Decision, bool, error) {
	if d := s.d.Decisions[id]; d != nil {
		return *d, true, nil
	}
	return views.Decision{}, false, nil
}

// Memory: "global" is the global one; "repo" is the first repo by name (extraMemory brings the
// others, so the demo can move between its repos).
func (s *jsonSource) Memory(_ context.Context, scope string) (views.Memory, error) {
	var repos []string
	for k, m := range s.d.Memory {
		if scope == views.ScopeGlobal && m.Scope == views.ScopeGlobal {
			return *m, nil
		}
		if m.Scope != views.ScopeGlobal {
			repos = append(repos, k)
		}
	}
	sort.Strings(repos)
	if scope == "repo" && len(repos) > 0 {
		return *s.d.Memory[repos[0]], nil
	}
	return views.Memory{}, errNoView
}

func (s *jsonSource) extraMemory(context.Context) []views.Memory {
	var out []views.Memory
	for _, m := range s.d.Memory {
		out = append(out, *m)
	}
	return out
}

func (s *jsonSource) Stats(context.Context, string) (views.Stats, error) {
	if s.d.Stats == nil {
		return views.Stats{}, errNoView
	}
	return *s.d.Stats, nil
}

func (s *jsonSource) Trend(context.Context) (views.Trend, error) {
	if s.d.Trend == nil {
		return views.Trend{}, errNoView
	}
	return *s.d.Trend, nil
}

func (s *jsonSource) Config(context.Context) (views.Config, error) {
	if s.d.Config == nil {
		return views.Config{}, errNoView
	}
	return *s.d.Config, nil
}

func (s *jsonSource) Plan(_ context.Context, id string) (views.Plan, bool, error) {
	if p := s.d.Plans[id]; p != nil {
		return *p, true, nil
	}
	return views.Plan{}, false, nil
}

// File finds the file by "repo/path" or by path.
func (s *jsonSource) File(_ context.Context, _, p string) (views.File, error) {
	var keys []string
	for k := range s.d.Files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if f := s.d.Files[k]; k == p || f.Path == p {
			return *f, nil
		}
	}
	return views.File{}, errNoView
}

func (s *jsonSource) Engineer(_ context.Context, id string) (views.Engineer, bool, error) {
	if e := s.d.Engineer[id]; e != nil {
		return *e, true, nil
	}
	return views.Engineer{}, false, nil
}
