package tui

import (
	"fmt"
	"strings"

	"github.com/mananos/nxy.dev/v2/internal/views"
)

func init() { register(engineerScreen{}) }

type engineerScreen struct{}

func (engineerScreen) id() string                     { return "ingeniero" }
func (engineerScreen) section() string                { return "inicio" }
func (engineerScreen) crumb(_ *state, r route) string { return r.key + " › Ingeniero" }

func (engineerScreen) keys(st *state, r route) []hint {
	enter := hint{"enter", "contesta cuando Claude pregunte"}
	if e := st.data.Engineer[r.key]; e != nil && e.AskID != "" {
		enter = hint{"enter", "elegir"}
	}
	return []hint{{"↑↓", "opción"}, enter, {"h", "cómo es hoy"}, {"c", "qué elegiría Claude"}}
}

// engFlag is a toggle of the screen kept in the cursor map ("#h" today, "#c" Claude's opinion).
func (st *state) engFlag(r route, name string) bool { return st.cur[r.ckey()+"#"+name] == 1 }

func (st *state) engToggle(r route, name string) {
	k := r.ckey() + "#" + name
	st.cur[k] = 1 - st.cur[k]
}

func (st *state) engCursor(r route, e *views.Engineer) int {
	return min(max(st.cursor(r), 0), max(len(e.Options)-1, 0))
}

// engAgenda is the forks as joined labels, wrapped to w cells.
func engAgenda(e *views.Engineer, w int) []line {
	var out []line
	var row line
	for i, f := range e.Agenda {
		var sep, chipSeg line
		if i > 0 {
			sep = L(T(" ── ", tSb))
			if f.Status != views.ForkPending { // the way already walked is drawn thick
				sep = L(T(" ━━ ", tAc))
			}
		}
		switch f.Status {
		case views.ForkDecided:
			chipSeg = L(S(" ✓ "+f.Title+" ", "ch-mute"))
		case views.ForkCurrent:
			chipSeg = L(S(" ● "+f.Title+" ", "ch-ac"))
		default:
			chipSeg = L(S(" "+f.Title+" ", "ch-off"))
		}
		if len(row) > 0 && lw(row)+lw(sep)+lw(chipSeg) > w {
			out = append(out, row)
			row, sep = nil, nil
		}
		row = L(row, sep, chipSeg)
	}
	if len(row) > 0 {
		out = append(out, row)
	}
	return out
}

// engDiagram draws a diagram in d2 color; without drawn lines it shows the source.
func engDiagram(d views.Diagram, w int) []line {
	if len(d.Lines) > 0 {
		out := make([]line, len(d.Lines))
		for i, s := range d.Lines {
			out[i] = fit(L(T(s, tD2)), w)
		}
		return out
	}
	var out []line
	for _, s := range strings.Split(strings.TrimRight(d.Source, "\n"), "\n") {
		out = append(out, fit(L(T(s, tMu)), w))
	}
	return append(out, nil, L(T("el dibujo no está: instalá d2 desde Config", tSb)))
}

func (engineerScreen) view(st *state, r route, w int) ([]line, int) {
	e := st.data.Engineer[r.key]
	if e == nil {
		return st.missing("engineer/"+r.key, "No hay ingeniero de "+r.key+".", w), 0
	}
	aw := w - 4 // width of the blocks
	cur := st.engCursor(r, e)

	// title: chip "ingeniero" in place of the state, stepper under it
	f := e.Feature
	var names []string
	for _, x := range f.Repos {
		names = append(names, x.Name)
	}
	meta := []string{strings.Join(names, " + "), durText(f.ElapsedMs), costText(f.CostUSD, f.CostEstimated)}
	l1 := lr(L(T("←", tMu), "   ", B(f.Title, ""), "   ", T(f.ID, tSb)), L(S(" ingeniero ", "ch-ac")), aw-6)
	l2 := L("    ", T(strings.Join(nonEmpty(meta...), " · "), tMu))
	out := []line{nil}
	out = append(out, indent(block([]line{l1, l2}, aw, tBg1, 3), 2)...)
	out = append(out, nil, L("  ", stepper(f.Stage, f.StageDetail)), nil)
	for _, l := range engAgenda(e, aw) {
		out = append(out, L("  ", l))
	}
	out = append(out, nil)

	// the current fork
	n, of := 0, len(e.Agenda)
	title := ""
	for i, a := range e.Agenda {
		if a.ID == e.Current {
			n, title = i+1, a.Title
		}
	}
	two := w >= 100
	lwid, rwid := aw, aw
	if two {
		lwid = 46
		rwid = aw - lwid - 2
	}
	li := lwid - 6

	head := []line{
		L(B(title, "")),
		L(T(fmt.Sprintf("bifurcación %d de %d · ninguna elegida", n, of), tSb)),
	}
	if e.Question != "" {
		head = append(head, nil)
		for _, s := range wrap(e.Question, li) {
			head = append(head, L(T(s, tMu)))
		}
	}
	left := block(append(head, nil), lwid, tBg1, 3)
	focus := len(out) + len(left)
	for i, o := range e.Options {
		bg, mark := tBg1, "  "
		if i == cur {
			bg, mark = tSel, "▸ "
			focus = len(out) + len(left)
		}
		row := L(T(mark, tAc), S(" "+o.Key+" ", "ch-mute"), "  ")
		lab := T(o.Label, "")
		if i == cur {
			lab.Bold = true
		}
		left = append(left, block([]line{L(row, lab), L("       ", T(o.Summary, tMu))}, lwid, bg, 3)...)
	}
	tail := []line{nil, L(S(" ? ", "ch-off"), "  ", T(e.Other, tSb)), nil}
	if st.engFlag(r, "c") && e.Opinion != "" {
		tail = append(tail, paras(L(B("Claude", tAc), "  "), e.Opinion, li, 8)...)
	} else {
		tail = append(tail, L(keycap("c"), "  ", T("qué elegiría Claude", tMu)))
	}
	tail = append(tail, nil)
	left = append(left, block(tail, lwid, tBg1, 3)...)

	// the option under the cursor, or how it is today
	var right []line
	if cur < len(e.Options) {
		o := e.Options[cur]
		ri := rwid - 6
		if st.engFlag(r, "h") {
			right = []line{
				lr(L(B("Así es hoy", "")), L(S(" "+o.Key+" ", "ch-off"), " ", S(" hoy ", "ch-ac")), ri),
				nil,
			}
			right = append(right, engDiagram(e.Today, ri)...)
			right = append(right, nil, L(T("lo que hay en el repo ahora, sin ninguna opción aplicada", tMu)))
		} else {
			right = []line{
				lr(L(B(o.Key, tAc), "  ", B(o.Label, "")), L(S(" "+o.Key+" ", "ch-ac"), " ", S(" hoy ", "ch-off")), ri),
				nil,
			}
			right = append(right, engDiagram(o.Diagram, ri)...)
			right = append(right, nil)
			for _, p := range o.Pros {
				right = append(right, paras(L(T("+ ", tDone)), p, ri, 2)...)
			}
			for _, c := range o.Cons {
				right = append(right, paras(L(T("− ", tFail)), c, ri, 2)...)
			}
			if o.Touches != "" {
				right = append(right, paras(L(T("toca ", tSb)), o.Touches, ri, 5)...)
			}
		}
		right = block(append(right, nil), rwid, tBg1, 3)
	}

	if two {
		for _, l := range hcat([][]line{left, right}, []int{lwid, rwid}, 2) {
			out = append(out, L("  ", l))
		}
	} else {
		out = append(out, indent(left, 2)...)
		out = append(out, nil)
		out = append(out, indent(right, 2)...)
	}
	return out, focus
}

func (engineerScreen) key(st *state, r route, k string) intent {
	e := st.data.Engineer[r.key]
	if e == nil {
		return intent{}
	}
	cur := st.engCursor(r, e)
	switch k {
	case "up", "k":
		st.setCursor(r, max(cur-1, 0))
	case "down", "j":
		st.setCursor(r, min(cur+1, max(len(e.Options)-1, 0)))
	case "h":
		st.engToggle(r, "h")
	case "c":
		st.engToggle(r, "c")
	case "enter":
		if cur < len(e.Options) {
			a := e.Options[cur].Action
			if e.AskID == "" || len(a.Argv) == 0 {
				return intent{toast: "elegí en el chat; enter contesta cuando Claude haga la pregunta de la bifurcación"}
			}
			return intent{toast: st.runAction(actionReq{label: "Elegir " + e.Options[cur].Key, a: a, reloadOnErr: true})}
		}
	}
	return intent{}
}
