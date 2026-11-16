package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/mananos/nxy.dev/internal/views"
)

// staleText is what the TUI says when the db is from a newer nxy.
const staleText = "nxy se actualizó: cerrá y volvé a abrir"

// actionReq is a button the user pressed: what to run and what to do when it worked.
type actionReq struct {
	key   string // busy key ("tool/rtk"): the row that shows the running line; empty: none
	label string
	busy  string // the running line; default label + "…"
	a     views.Action
	after func() // runs only when the command worked
	// reloadOnErr reloads the views when the command fails: a decide that came late ("ya contestada")
	// leaves the screen showing a question nobody waits for.
	reloadOnErr bool
	// filled says every placeholder of the argv got a value: a `{x}` inside a value the user
	// typed is not a placeholder nobody filled in.
	filled bool
}

// actionDoneMsg is the end of a command run in the background.
type actionDoneMsg struct {
	q   actionReq
	out string
	err error
}

// runAction starts an action the way every button does and returns the toast to show.
// Planned actions say they are not there yet; a stale db runs nothing; one with a Confirm asks
// y/n first; the rest run in the background through Options.Run. Without Run (the demo) it only
// says what would run.
func (st *state) runAction(q actionReq) string {
	switch {
	case q.a.Planned:
		return "todavía no existe"
	case st.stale:
		return staleText
	case !q.filled && hasPlaceholder(q.a.Argv):
		st.errNext = true
		return q.label + ": falta el formulario, no se corrió nada"
	case len(q.a.Argv) == 0:
		if q.after != nil {
			q.after()
		}
		return q.label
	case st.run == nil:
		if q.after != nil {
			q.after()
		}
		return actionToast(q.label, q.a)
	case q.a.Confirm != "":
		st.confirm = &q
		return ""
	}
	st.startAction(q)
	return ""
}

// hasPlaceholder says whether an argv still carries a `{name}` nobody filled in.
func hasPlaceholder(argv []string) bool {
	for _, a := range argv {
		if i := strings.Index(a, "{"); i >= 0 && strings.Contains(a[i:], "}") {
			return true
		}
	}
	return false
}

func (st *state) startAction(q actionReq) {
	if q.busy == "" {
		q.busy = q.label + "…"
	}
	if q.key != "" {
		st.busy[q.key] = q.busy
	}
	run, ctx := st.run, context.Background()
	if st.ld != nil {
		ctx = st.ld.ctx
	}
	argv := q.a.Argv
	st.cmd = tea.Batch(st.cmd, func() tea.Msg {
		out, err := run(ctx, argv)
		return actionDoneMsg{q: q, out: out, err: err}
	})
	if q.key == "" {
		st.toast, st.toastErr = q.busy, false
	}
}

// confirmKey answers the y/n of the footer; every other key is ignored until it is answered.
func (st *state) confirmKey(k string) {
	q := *st.confirm
	switch k {
	case "y":
		st.confirm = nil
		st.startAction(q)
	case "n", "esc":
		st.confirm = nil
		st.toast, st.toastErr = "", false
	}
}

// confirmLine is the question in the footer.
func (st *state) confirmLine() string {
	q := st.confirm
	return strings.TrimSpace(q.label + " · " + q.a.Confirm + " · y/n")
}

// onActionDone applies the end of a command: on success the toast, the after and a reload; on
// failure the first line of what it printed, in red.
func (st *state) onActionDone(m actionDoneMsg) tea.Cmd {
	if m.q.key != "" {
		delete(st.busy, m.q.key)
	}
	if m.err != nil {
		st.toast, st.toastErr = firstLine(m.out, m.err.Error()), true
		if m.q.reloadOnErr {
			return st.reload()
		}
		return nil
	}
	st.toast, st.toastErr = m.q.label+" · listo", false
	if m.q.after != nil {
		m.q.after()
	}
	return st.reload()
}

// firstLine is the first line of out that is not blank, or the fallback.
func firstLine(out, fallback string) string {
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return fallback
}

// lifeAction is the pause or resume of a feature, if the view brings one.
func lifeAction(f views.CockpitFeature) (views.Action, bool) {
	for _, a := range f.Actions {
		if a.ID == "pause" || a.ID == "resume" {
			return a, true
		}
	}
	return views.Action{}, false
}

// toggleLife is the space key of Inicio and Feature.
func (st *state) toggleLife(f views.CockpitFeature) intent {
	a, ok := lifeAction(f)
	if !ok {
		return intent{}
	}
	verb := "pausar"
	if a.ID == "resume" {
		verb = "reanudar"
	}
	return intent{toast: st.runAction(actionReq{label: verb + " " + f.ID, a: a})}
}
