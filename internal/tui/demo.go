package tui

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/mananos/nxy.dev/internal/views"
)

// Data is everything the screens show: one struct of the contract per screen.
type Data struct {
	Cockpit   *views.Cockpit
	Agents    map[string]*views.Agents   // by feature id
	Plans     map[string]*views.Plan     // by feature id
	Decisions map[string]*views.Decision // by feature id: one pending question per feature
	Engineer  map[string]*views.Engineer // by feature id
	Memory    map[string]*views.Memory   // by repo name
	Files     map[string]*views.File     // by repo + "/" + path
	Stats     *views.Stats
	Trend     *views.Trend
	Config    *views.Config
}

// decisionQueue is the pending questions, the oldest first.
func (d *Data) decisionQueue() []*views.Decision {
	var out []*views.Decision
	for _, x := range d.Decisions {
		out = append(out, x)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].AskedAt != out[j].AskedAt {
			return out[i].AskedAt < out[j].AskedAt
		}
		return out[i].FeatureID < out[j].FeatureID
	})
	return out
}

const demoDir = "testdata/views"

// LoadDemo reads testdata/views/*.json from fsys. The part of the file name before the first
// dot picks the struct; an unknown screen or an unknown field is an error.
func LoadDemo(fsys fs.FS) (*Data, error) {
	entries, err := fs.ReadDir(fsys, demoDir)
	if err != nil {
		return nil, err
	}
	d := &Data{
		Agents:    map[string]*views.Agents{},
		Plans:     map[string]*views.Plan{},
		Decisions: map[string]*views.Decision{},
		Engineer:  map[string]*views.Engineer{},
		Memory:    map[string]*views.Memory{},
		Files:     map[string]*views.File{},
	}
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
		data, err := fs.ReadFile(fsys, path.Join(demoDir, name))
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
		return nil, fmt.Errorf("%s: cockpit.json is missing", demoDir)
	}
	return d, nil
}
