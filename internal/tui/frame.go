package tui

import (
	"fmt"
	"strings"

	"github.com/mananos/nxy.dev/v2/internal/format"
)

// contentWidth is the width of the content: the whole terminal, edge to edge with the bars.
func contentWidth(w int) int { return w }

// topBar is the bar of the sections, on the whole width.
func (st *state) topBar(w int) line {
	l := L("  ", S(" nxy ", "ch-ac"), "    ")
	active := st.currentSection()
	for _, s := range sections {
		if s.id == active {
			t := S(" "+s.title+" ", "tabon")
			t.Mark = true
			l = append(l, t)
		} else {
			l = append(l, T(" "+s.title+" ", tMu))
		}
		l = append(l, spaces(2, ""))
	}
	var right line
	if n := st.decisionCount(); n > 0 {
		label := fmt.Sprintf(" ● %d ", n)
		if w >= 100 {
			label = " ● " + decisionsText(n) + " "
		}
		right = append(right, S(label, "ch-need"), spaces(3, ""))
	}
	right = append(right, T("hoy ", tMu), B(format.FmtUsd(st.data.Cockpit.Totals.CostTodayUSD), ""), spaces(3, ""))
	return bgize(lr(l, right, w), tBg2)
}

// footBar is the bar of keys: the ones that do not fit fall from the end; ? stays.
func (st *state) footBar(w int, hints []hint) line {
	all := append(append([]hint(nil), hints...), hint{"?", "ayuda"})
	for n := len(hints); n >= 0; n-- {
		l := L("  ")
		for i, h := range append(append([]hint(nil), all[:n]...), all[len(all)-1]) {
			if i > 0 {
				l = append(l, spaces(5, ""))
			}
			l = append(l, B(h.key, ""), T("  "+h.desc, tMu))
		}
		if lw(l) <= w || n == 0 {
			return bgize(fit(l, w), tBg2)
		}
	}
	return nil
}

func (st *state) helpLines(w int) []line {
	r := st.top()
	rows := []line{L(B("Teclas", ""))}
	for _, h := range st.lookup(r.screen).keys(st, r) {
		rows = append(rows, L(keycap(h.key), "  ", h.desc))
	}
	rows = append(rows, nil,
		L(keycap("esc"), "  ", "sube un nivel; en la raíz de una sección vuelve a Inicio"),
		L(keycap("tab"), "  ", "cambia de sección"),
		L(keycap("q"), "  ", "salir"),
		L(keycap("ctrl+t"), "  ", "tema oscuro o claro"),
		L(keycap("ctrl+g"), "  ", "íconos nerd o unicode"),
	)
	if st.demo {
		rows = append(rows, L(keycap("ctrl+n"), "  ", "simula que llega una decisión"))
	}
	inner := append(append([]line{nil}, rows...), nil)
	return append([]line{nil}, indent(block(inner, w-4, tBg1, 3), 2)...)
}

// frame draws the whole screen. With dump the body is printed whole; interactive, it is cut to
// the height of the terminal keeping the focused row in view.
func (st *state) frame(dump bool) string {
	w := st.width
	cw := contentWidth(w)
	margin := strings.Repeat(" ", (w-cw)/2)
	r := st.top()
	scr := st.lookup(r.screen)
	paint := func(l line) string {
		if len(l) == 0 {
			return ""
		}
		return margin + render(l, st.th)
	}

	head := []string{render(st.topBar(w), st.th)}
	if len(st.stack) > 1 {
		parts := []string{sectionTitle(st.stack[0].screen)}
		for _, x := range st.stack[1:] {
			parts = append(parts, strings.Split(st.lookup(x.screen).crumb(st, x), " › ")...)
		}
		head = append(head, paint(lr(L("  ", crumb(parts)), L(T("esc vuelve", tSb), "  "), cw)))
	}

	if st.stale {
		head = append(head, paint(fit(L("  ", T("nxy se actualizó: cerrá y volvé a abrir", tNeed)), cw)))
	}

	var body []line
	focus := 0
	if st.help {
		body = st.helpLines(cw)
	} else {
		body, focus = scr.view(st, r, cw)
	}

	foot := []string{}
	if !dump && st.confirm != nil {
		for _, s := range wrap(st.confirmLine(), cw-4) {
			foot = append(foot, paint(fit(L("  ", T(s, tNeed)), cw)))
		}
	} else if !dump && st.toast != "" {
		// the toast carries commands: wrap it instead of cutting it
		tok := tAc
		if st.toastErr {
			tok = tFail
		}
		for _, s := range wrap(st.toast, cw-4) {
			foot = append(foot, paint(fit(L("  ", T(s, tok)), cw)))
		}
	}
	hints := scr.keys(st, r)
	if st.decOpen {
		hints = st.decHints()
	}
	foot = append(foot, render(st.footBar(w, hints), st.th))

	if !dump {
		avail := max(st.height-len(head)-len(foot), 1)
		if len(body) > avail {
			start := 0
			if focus >= avail {
				start = min(focus-avail+3, len(body)-avail)
			}
			body = body[start : start+avail]
		}
		for len(body) < avail {
			body = append(body, nil)
		}
	}

	out := append([]string(nil), head...)
	for _, l := range body {
		out = append(out, paint(l))
	}
	if st.decOpen && !st.help {
		if win := st.decWindow(cw); len(win) > 0 {
			ww := lw(win[0])
			top := 3
			if dump {
				for len(out) < top+len(win) {
					out = append(out, "")
				}
			} else {
				// never taller than the terminal: rise, then cut what does not fit
				top = max(min(top, len(out)-len(win)), 0)
				win = win[:min(len(win), len(out)-top)]
			}
			painted := make([]string, len(win))
			for i, l := range win {
				painted[i] = render(l, st.th)
			}
			out = overlay(dim(out, st.th), painted, (w-cw)/2+centerX(cw, ww), top)
		}
	}
	out = append(out, foot...)
	return strings.Join(out, "\n")
}
