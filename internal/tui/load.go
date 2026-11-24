package tui

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mananos/nxy.dev/v2/internal/views"
)

// loader reads the views from the Source. One mutex: the real source and its caches are not for
// parallel calls.
type loader struct {
	ctx    context.Context
	src    views.Source
	inRepo bool
	mu     sync.Mutex
}

// extraMemorier is a Source that has more memory than the repo of the folder and the global one
// (the demo, to move between its repos).
type extraMemorier interface {
	extraMemory(ctx context.Context) []views.Memory
}

// result is what one load brings: a partial Data, the keys it answers (they stop loading and
// lose their old error) and whether Decisions is the whole queue.
type result struct {
	d            *Data
	keys         []string
	allDecisions bool
}

func newResult() *result { return &result{d: newData()} }

func (r *result) fail(key string, err error) {
	r.keys = append(r.keys, key)
	r.d.Errs[key] = err
}

// cheapOpts is what the user chose that the cheap load must keep: the session Gasto shows.
type cheapOpts struct {
	session string
	anchor  time.Time // Tendencia: the end of the window (zero is now)
}

func (st *state) cheapOpts() cheapOpts {
	return cheapOpts{session: st.statsSession, anchor: st.trendAnchor()}
}

type cheapMsg struct{ r *result }
type topMsg struct{ r *result }

// cheap loads what every screen shares: the cockpit, the questions, the memory, the spend and the
// config.
func (l *loader) cheap(opts cheapOpts) *result {
	l.mu.Lock()
	defer l.mu.Unlock()
	ctx, r := l.ctx, newResult()

	r.keys = append(r.keys, "cockpit")
	co, err := l.src.Cockpit(ctx)
	if err != nil {
		r.fail("cockpit", err)
	} else {
		r.d.Cockpit = &co
		r.allDecisions = true
		for _, f := range co.Features {
			if f.Decision == nil {
				continue
			}
			key := "decision/" + f.ID
			r.keys = append(r.keys, key)
			d, ok, err := l.src.Decision(ctx, f.ID)
			switch {
			case err != nil:
				r.fail(key, err)
			case ok:
				r.d.Decisions[f.ID] = &d
			}
		}
	}

	scopes := []string{views.ScopeGlobal}
	if l.inRepo {
		scopes = []string{"repo", views.ScopeGlobal}
	}
	for _, sc := range scopes {
		key := "memory/" + sc
		r.keys = append(r.keys, key)
		m, err := l.src.Memory(ctx, sc)
		if err != nil {
			r.fail(key, err)
			continue
		}
		name := m.Repo.Name
		if m.Scope == views.ScopeGlobal {
			name = views.ScopeGlobal
		}
		r.d.Memory[name] = &m
	}
	if x, ok := l.src.(extraMemorier); ok {
		for _, m := range x.extraMemory(ctx) {
			m := m
			r.d.Memory[m.Repo.Name] = &m
		}
	}

	r.keys = append(r.keys, "stats")
	if s, err := l.src.Stats(ctx, opts.session); errors.Is(err, views.ErrNoSession) {
		r.d.Stats = nil
	} else if err != nil {
		r.fail("stats", err)
	} else {
		r.d.Stats = &s
	}
	r.keys = append(r.keys, "trend")
	if t, err := l.src.Trend(ctx, opts.anchor); err != nil {
		r.fail("trend", err)
	} else {
		r.d.Trend = &t
	}
	r.keys = append(r.keys, "config")
	if c, err := l.src.Config(ctx); err != nil {
		r.fail("config", err)
	} else {
		r.d.Config = &c
	}
	return r
}

func (l *loader) agents(id string) *result {
	l.mu.Lock()
	defer l.mu.Unlock()
	r, key := newResult(), "agents/"+id
	r.keys = append(r.keys, key)
	if a, err := l.src.Agents(l.ctx, id); err != nil {
		r.fail(key, err)
	} else {
		r.d.Agents[id] = &a
	}
	return r
}

func (l *loader) plan(id string) *result {
	l.mu.Lock()
	defer l.mu.Unlock()
	r := newResult()
	l.planLocked(r, id)
	return r
}

func (l *loader) planLocked(r *result, id string) *views.Plan {
	key := "plan/" + id
	r.keys = append(r.keys, key)
	p, ok, err := l.src.Plan(l.ctx, id)
	switch {
	case err != nil:
		r.fail(key, err)
	case ok:
		r.d.Plans[id] = &p
		return &p
	}
	return nil
}

func (l *loader) engineer(id string) *result {
	l.mu.Lock()
	defer l.mu.Unlock()
	r, key := newResult(), "engineer/"+id
	r.keys = append(r.keys, key)
	e, ok, err := l.src.Engineer(l.ctx, id)
	switch {
	case err != nil:
		r.fail(key, err)
	case ok:
		r.d.Engineer[id] = &e
	}
	return r
}

// file loads the file "repo/path" of a feature. The source wants the path as the plan writes it:
// with the repo only when the feature has more than one.
func (l *loader) file(feature, key string, plan *views.Plan) *result {
	l.mu.Lock()
	defer l.mu.Unlock()
	r := newResult()
	if plan == nil {
		plan = l.planLocked(r, feature)
	}
	fk := "file/" + key
	r.keys = append(r.keys, fk)
	arg := key
	if plan == nil || len(plan.Repos) <= 1 {
		arg = stripRepo(plan, key)
	}
	f, err := l.src.File(l.ctx, feature, arg)
	if err != nil {
		r.fail(fk, err)
		return r
	}
	r.d.Files[key] = &f
	return r
}

// stripRepo takes the repo off "repo/path".
func stripRepo(p *views.Plan, key string) string {
	if p != nil {
		for _, f := range p.Impact.Files {
			if f.Repo+"/"+f.Path == key {
				return f.Path
			}
		}
		for _, rp := range p.Repos {
			if rest, ok := strings.CutPrefix(key, rp.Name+"/"); ok {
				return rest
			}
		}
	}
	return key
}

// merge puts a result in the cache.
func (d *Data) merge(r *result) {
	x := r.d
	if x.Cockpit != nil {
		d.Cockpit = x.Cockpit
	}
	if r.allDecisions {
		d.Decisions = x.Decisions
	}
	for k, v := range x.Agents {
		d.Agents[k] = v
	}
	for k, v := range x.Plans {
		d.Plans[k] = v
	}
	for k, v := range x.Engineer {
		d.Engineer[k] = v
	}
	for k, v := range x.Memory {
		d.Memory[k] = v
	}
	for k, v := range x.Files {
		d.Files[k] = v
	}
	if x.Stats != nil {
		d.Stats = x.Stats
	}
	if x.Trend != nil {
		d.Trend = x.Trend
	}
	if x.Config != nil {
		d.Config = x.Config
	}
	for _, k := range r.keys {
		delete(d.Loading, k)
		delete(d.Errs, k)
		d.Done[k] = true
	}
	for k, e := range x.Errs {
		d.Errs[k] = e
	}
}

// topKey is the key of the view a route needs on top of the cheap ones ("" when it needs none).
func topKey(r route) string {
	switch r.screen {
	case "feature", "agentes":
		return "agents/" + r.key
	case "plan":
		return "plan/" + r.key
	case "ingeniero":
		return "engineer/" + r.key
	case "archivo":
		return "file/" + r.key
	}
	return ""
}

// topRun loads the view of a route.
func (st *state) topRun(r route) func() *result {
	l := st.ld
	switch r.screen {
	case "feature", "agentes":
		return func() *result { return l.agents(r.key) }
	case "plan":
		return func() *result { return l.plan(r.key) }
	case "ingeniero":
		return func() *result { return l.engineer(r.key) }
	case "archivo":
		p := st.data.Plans[r.feature]
		return func() *result { return l.file(r.feature, r.key, p) }
	}
	return nil
}

// topCmd loads the view of the screen on top unless it is there or already loading. force asks
// again anyway (a refresh).
func (st *state) topCmd(force bool) tea.Cmd {
	if st.ld == nil {
		return nil
	}
	r := st.top()
	key := topKey(r)
	if key == "" || st.data.Loading[key] {
		return nil
	}
	if !force && st.data.Done[key] {
		return nil
	}
	run := st.topRun(r)
	if run == nil {
		return nil
	}
	st.data.Loading[key] = true
	return func() tea.Msg { return topMsg{r: run()} }
}

// cheapCmd loads the cheap views; with one already in flight it does nothing.
func (st *state) cheapCmd() tea.Cmd {
	if st.ld == nil || st.cheapBusy {
		return nil
	}
	st.cheapBusy = true
	st.data.Loading["cockpit"] = true
	l, o := st.ld, st.cheapOpts()
	return func() tea.Msg { return cheapMsg{r: l.cheap(o)} }
}

// onLoad applies what a load brought.
func (st *state) onLoad(msg tea.Msg) {
	// the cursors follow their row, not their position
	snap := st.snapCursors()
	defer st.restoreCursors(snap)
	switch m := msg.(type) {
	case cheapMsg:
		st.cheapBusy = false
		st.data.merge(m.r)
	case topMsg:
		st.data.merge(m.r)
	}
}

// loadAll loads everything now: the cheap views, and per feature its agents, plan, design and the
// files of the plan. Tests that drive the screens with keys use it so no screen waits.
func (st *state) loadAll() {
	if st.ld == nil {
		return
	}
	st.onLoad(cheapMsg{r: st.ld.cheap(st.cheapOpts())})
	if js, ok := st.ld.src.(*jsonSource); ok {
		for k, f := range js.d.Files {
			st.data.Files[k] = f
			st.data.Done["file/"+k] = true
		}
	}
	var ids []string
	for _, f := range st.data.Cockpit.Features {
		ids = append(ids, f.ID)
	}
	sort.Strings(ids)
	for _, id := range ids {
		st.data.merge(st.ld.agents(id))
		st.data.merge(st.ld.plan(id))
		st.data.merge(st.ld.engineer(id))
		if p := st.data.Plans[id]; p != nil {
			for _, f := range p.Impact.Files {
				st.data.merge(st.ld.file(id, f.Repo+"/"+f.Path, p))
			}
		}
	}
}

// loadRoute loads synchronously what a dump of this route needs.
func (st *state) loadRoute(r route) {
	if st.ld == nil {
		return
	}
	if run := st.topRun(r); run != nil && topKey(r) != "" {
		st.data.merge(run())
	}
}
