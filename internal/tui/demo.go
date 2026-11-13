package tui

import (
	"sort"

	"github.com/mananos/nxy.dev/internal/views"
)

// Data is everything the screens show: one struct of the contract per screen. It is a cache the
// loader fills; a view that is not here yet is either in Loading or failed (Errs). The keys of
// Errs and Loading are "cockpit", "stats", "trend", "config", "memory/<scope>", "decision/<id>",
// "agents/<id>", "plan/<id>", "engineer/<id>" and "file/<repo/path>".
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

	Errs    map[string]error
	Loading map[string]bool
	Done    map[string]bool // keys already asked for: no second load unless forced
}

func newData() *Data {
	return &Data{
		Agents:    map[string]*views.Agents{},
		Plans:     map[string]*views.Plan{},
		Decisions: map[string]*views.Decision{},
		Engineer:  map[string]*views.Engineer{},
		Memory:    map[string]*views.Memory{},
		Files:     map[string]*views.File{},
		Errs:      map[string]error{},
		Loading:   map[string]bool{},
		Done:      map[string]bool{},
	}
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
