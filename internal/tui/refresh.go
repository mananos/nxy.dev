package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mananos/nxy.dev/internal/views"
)

const (
	pollEvery = 500 * time.Millisecond
	liveEvery = time.Second
)

type pollTick struct{}
type pollMsg struct {
	ver    int64
	schema int
	err    error
}
type liveTick struct{}

// pollCmd asks the db whether something changed.
func (st *state) pollCmd() tea.Cmd {
	if st.watch == nil || st.ld == nil || st.stale {
		return nil
	}
	w, ctx := st.watch, st.ld.ctx
	return func() tea.Msg {
		v, s, err := w(ctx)
		return pollMsg{ver: v, schema: s, err: err}
	}
}

func (st *state) pollTickCmd() tea.Cmd {
	if st.watch == nil || st.stale {
		return nil
	}
	return tea.Tick(pollEvery, func(time.Time) tea.Msg { return pollTick{} })
}

// reload loads the cheap views and the one of the screen on top again; with a cheap load in
// flight it is asked again as soon as that one ends.
func (st *state) reload() tea.Cmd {
	if st.ld == nil {
		return nil
	}
	st.forgetFar()
	if st.cheapBusy {
		st.cheapDirty = true
		return st.topCmd(true)
	}
	return tea.Batch(st.cheapCmd(), st.topCmd(true))
}

// forgetFar marks as not loaded the top views (agents, plan, engineer, file) of the screens that
// are not on top, so entering them again asks for them anew.
func (st *state) forgetFar() {
	keep := topKey(st.top())
	for k := range st.data.Done {
		if k == keep {
			continue
		}
		for _, p := range []string{"agents/", "plan/", "engineer/", "file/"} {
			if strings.HasPrefix(k, p) {
				delete(st.data.Done, k)
				break
			}
		}
	}
}

// onPoll applies what a poll found and keeps the polling going.
func (st *state) onPoll(m pollMsg) tea.Cmd {
	if m.err != nil {
		return st.pollTickCmd()
	}
	if m.schema > st.schema {
		// a newer nxy migrated the db: load nothing else
		if !st.stale && st.onStale != nil {
			st.onStale()
		}
		st.stale = true
		return nil
	}
	if !st.haveVer {
		st.haveVer, st.lastVer = true, m.ver
		return st.pollTickCmd()
	}
	if m.ver != st.lastVer {
		st.lastVer = m.ver
		return tea.Batch(st.pollTickCmd(), st.reload())
	}
	return st.pollTickCmd()
}

// working tells whether some feature of the cockpit is running.
func (st *state) working() bool {
	for _, f := range st.data.Cockpit.Features {
		if f.Status == views.StatusWorking || f.Status == views.StatusVerifying {
			return true
		}
	}
	return false
}

// liveTickCmd arms the one-second tick, only while a feature works and only once.
func (st *state) liveTickCmd() tea.Cmd {
	if st.watch == nil || st.stale || st.liveOn || !st.working() {
		return nil
	}
	st.liveOn = true
	return tea.Tick(liveEvery, func(time.Time) tea.Msg { return liveTick{} })
}

// onLive reloads the cockpit and, on Feature or Agentes, the agents; then arms the next tick.
func (st *state) onLive() tea.Cmd {
	st.liveOn = false
	if st.stale || !st.working() {
		return nil
	}
	st.forgetFar()
	cmds := []tea.Cmd{st.cheapCmd()}
	if s := st.top().screen; s == "feature" || s == "agentes" {
		cmds = append(cmds, st.topCmd(true))
	}
	cmds = append(cmds, st.liveTickCmd())
	return tea.Batch(cmds...)
}

// cursorRows are the ids of the rows one cursor of a screen moves over. suffix is the part of the
// cursor key after the route's; implicit means a cursor never moved counts as row 0.
type cursorRows struct {
	suffix   string
	ids      []string
	implicit bool
}

// rowKeyer is a screen whose cursors follow the row (by id) when the data changes.
type rowKeyer interface {
	rowCursors(st *state, r route) []cursorRows
}

type cursorSnap struct {
	r  route
	i  int
	id string
}

// snapCursors remembers the id under each cursor of the path.
func (st *state) snapCursors() []cursorSnap {
	var out []cursorSnap
	for _, r := range st.stack {
		kr, ok := st.lookup(r.screen).(rowKeyer)
		if !ok {
			continue
		}
		for i, c := range kr.rowCursors(st, r) {
			idx, set := st.cur[r.ckey()+c.suffix]
			if !set {
				if !c.implicit {
					continue
				}
				idx = 0
			}
			if idx >= 0 && idx < len(c.ids) {
				out = append(out, cursorSnap{r: r, i: i, id: c.ids[idx]})
			}
		}
	}
	return out
}

// restoreCursors puts each cursor on its id; one that left stays where it was (the screens clamp).
func (st *state) restoreCursors(snap []cursorSnap) {
	for _, e := range snap {
		kr, ok := st.lookup(e.r.screen).(rowKeyer)
		if !ok {
			continue
		}
		cs := kr.rowCursors(st, e.r)
		if e.i >= len(cs) {
			continue
		}
		for j, id := range cs[e.i].ids {
			if id == e.id {
				st.cur[e.r.ckey()+cs[e.i].suffix] = j
				break
			}
		}
	}
}

func (inicioScreen) rowCursors(st *state, _ route) []cursorRows {
	var ids []string
	for _, f := range inicioOrder(st.data.Cockpit) {
		ids = append(ids, f.ID)
	}
	return []cursorRows{{ids: ids, implicit: true}}
}

func (featureScreen) rowCursors(st *state, r route) []cursorRows {
	var ids []string
	for _, n := range batchNodes(st.data.Agents[r.key]) {
		ids = append(ids, n.ID)
	}
	return []cursorRows{{ids: ids}}
}

func (agentsScreen) rowCursors(st *state, r route) []cursorRows {
	var ids []string
	for _, x := range agentRows(st, r.key) {
		ids = append(ids, x.node.ID)
	}
	return []cursorRows{{ids: ids}}
}

func (planScreen) rowCursors(st *state, r route) []cursorRows {
	p := st.data.Plans[r.key]
	if p == nil || len(p.Batches) == 0 {
		return nil
	}
	var batches, files []string
	for _, b := range p.Batches {
		batches = append(batches, fmt.Sprintf("b%d", b.N))
	}
	for _, f := range planFiles(p, p.Batches[st.planBatch(r, p)].N) {
		files = append(files, f.Repo+"/"+f.Path)
	}
	return []cursorRows{{suffix: "#b", ids: batches}, {suffix: "#f", ids: files}}
}

func (configScreen) rowCursors(st *state, _ route) []cursorRows {
	c := st.data.Config
	if c == nil {
		return nil
	}
	var ids []string
	for _, x := range cfgRows(c) {
		switch x.kind {
		case "role":
			ids = append(ids, "role/"+c.Roles[x.idx].Role)
		case "setting":
			ids = append(ids, "setting/"+c.Settings[x.idx].ID)
		case "tool":
			ids = append(ids, "tool/"+c.Tools[x.idx].Name)
		default:
			ids = append(ids, x.kind)
		}
	}
	return []cursorRows{{ids: ids, implicit: true}}
}
