package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/mananos/nxy.dev/internal/views"
)

// decisionWidth is the width of the window; narrower terminals get the width minus 6.
const decisionWidth = 74

// pending is the questions still open, the oldest first.
func (st *state) pending() []*views.Decision {
	var out []*views.Decision
	for _, d := range st.data.decisionQueue() {
		if !st.answered[d.FeatureID] {
			out = append(out, d)
		}
	}
	return out
}

func indexOfDecision(q []*views.Decision, feature string) int {
	for i, d := range q {
		if d.FeatureID == feature {
			return i
		}
	}
	return -1
}

// openDecision puts the window on top, first with the question of feature (empty: the oldest).
// A question the user postponed comes back only when asked for, like here. It does not open
// while a text box is open.
func (st *state) openDecision(feature string) {
	if st.editing {
		return
	}
	q := st.pending()
	if len(q) == 0 {
		st.toast = "No hay decisiones pendientes."
		return
	}
	// the plan that waits for approval is answered on its own screen
	first := ""
	for _, d := range q {
		if !st.answersHere(d.FeatureID) {
			first = d.FeatureID
			break
		}
	}
	if first == "" || (feature != "" && st.answersHere(feature)) {
		st.toast = "La decisión de esta feature se contesta acá: a aprueba, c cambia, x para."
		return
	}
	st.postponed = map[string]bool{}
	st.decOpen = true
	st.decCur = first
	if feature != "" && indexOfDecision(q, feature) >= 0 {
		st.decCur = feature
	}
}

// answersHere says whether the current screen is the Plan of feature waiting for its approval:
// there the question is answered with a, c and x, so the window does not jump in.
func (st *state) answersHere(feature string) bool {
	r := st.top()
	if r.screen != "plan" || r.key != feature {
		return false
	}
	p := st.data.Plans[feature]
	return p != nil && p.State == views.PlanApproval
}

func (st *state) closeDecision() { st.decOpen = false }

// current is the question the window shows.
func (st *state) currentDecision() (d *views.Decision, i, n int) {
	q := st.pending()
	i = indexOfDecision(q, st.decCur)
	if i < 0 {
		return nil, 0, len(q)
	}
	return q[i], i, len(q)
}

func isFork(d *views.Decision) bool { return d.Kind == "fork" }

// decOption is the option under the cursor: the recommended one until the user moves.
func (st *state) decOption(d *views.Decision) int {
	if v, ok := st.cur["decision/"+d.FeatureID]; ok {
		return min(max(v, 0), max(len(d.Options)-1, 0))
	}
	for i, o := range d.Options {
		if o.Recommended {
			return i
		}
	}
	return 0
}

func (st *state) decHints() []hint {
	d, _, n := st.currentDecision()
	var h []hint
	switch {
	case d == nil:
	case isFork(d):
		h = append(h, hint{"enter", "abrir Ingeniero"})
	default:
		h = append(h, hint{fmt.Sprintf("1-%d", len(d.Options)), "elegir"}, hint{"enter", "confirmar"})
	}
	if n > 1 {
		h = append(h, hint{"tab", "la otra"})
	}
	return append(h, hint{"esc", "después"})
}

// decKey handles a key while the window is open and tells whether it was taken. A few keys go
// on to the app (quit, theme, icons, help); the rest are swallowed so nothing moves behind.
func (st *state) decKey(k string) bool {
	switch k {
	case "q", "ctrl+t", "ctrl+g", "ctrl+n", "ctrl+c", "?":
		return false
	}
	d, i, n := st.currentDecision()
	if d == nil {
		st.closeDecision()
		return true
	}
	switch k {
	case "tab", "shift+tab":
		step := 1
		if k == "shift+tab" {
			step = n - 1
		}
		q := st.pending()
		for j := 1; j <= n; j++ {
			if next := q[(i+step*j)%n].FeatureID; !st.answersHere(next) {
				st.decCur = next
				break
			}
		}
	case "esc":
		st.postponed[d.FeatureID] = true
		st.decNext()
	case "up", "k", "down", "j":
		if isFork(d) {
			break
		}
		sel := st.decOption(d)
		if k == "up" || k == "k" {
			sel--
		} else {
			sel++
		}
		st.cur["decision/"+d.FeatureID] = min(max(sel, 0), len(d.Options)-1)
	case "enter":
		if isFork(d) {
			// a fork is answered in Ingeniero, not here
			st.closeDecision()
			st.goSection("inicio")
			st.stack = append(st.stack, route{screen: "ingeniero", key: d.FeatureID})
			break
		}
		if len(d.Options) == 0 {
			break
		}
		o := d.Options[st.decOption(d)]
		st.toast = actionToast("decidir", o.Action)
		st.answered[d.FeatureID] = true
		st.decNext()
	default:
		if len(k) == 1 && k[0] >= '1' && k[0] <= '9' && !isFork(d) {
			if v := int(k[0] - '1'); v < len(d.Options) {
				st.cur["decision/"+d.FeatureID] = v
			}
		}
	}
	return true
}

// decNext shows the next question that was not postponed; with none left the window closes and
// the user is where they were.
func (st *state) decNext() {
	for _, d := range st.pending() {
		if !st.postponed[d.FeatureID] {
			st.decCur = d.FeatureID
			return
		}
	}
	st.closeDecision()
}

// wrap breaks s in lines of at most w cells, on spaces.
func wrap(s string, w int) []string {
	if w < 8 || ansi.StringWidth(s) <= w {
		return []string{s}
	}
	var out []string
	cur := ""
	for _, word := range strings.Fields(s) {
		switch {
		case cur == "":
			cur = word
		case ansi.StringWidth(cur+" "+word) <= w:
			cur += " " + word
		default:
			out = append(out, cur)
			cur = word
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// decWindow is the window as lines of one width. Empty when there is nothing to ask.
func (st *state) decWindow(cw int) []line {
	d, i, n := st.currentDecision()
	if d == nil {
		return nil
	}
	ww := min(decisionWidth, cw-6)
	iw := ww - 6
	head := lr(
		L(chip("Te necesita", tNeed, chipW), "   ", B(d.Header, "")),
		L(T(d.FeatureID+" · "+d.Repo, tSb), "  ", S(fmt.Sprintf(" %d de %d ", i+1, n), "ch-mute")), iw)

	top := []line{nil, head, nil}
	for _, s := range wrap(d.Question, iw) {
		top = append(top, L(s))
	}
	top = append(top, nil)
	if d.Fork != nil {
		top = append(top, L("  ", T(fmt.Sprintf("bifurcación %d de %d · %s", d.Fork.N, d.Fork.Of, d.Fork.Title), tSb)))
	}
	for _, c := range d.Context {
		for _, s := range wrap(c, iw-2) {
			top = append(top, L("  ", T(s, tMu)))
		}
	}
	top = append(top, nil)
	out := block(top, ww, tBg2, 3)

	sel := st.decOption(d)
	for k, o := range d.Options {
		var row line
		bg := tBg2
		if isFork(d) {
			row = L("  ", S(" "+string(rune('A'+k))+" ", "ch-mute"), "  ", B(o.Label, ""), "  ", T(o.Description, tMu))
		} else {
			marker, num := T("  ", tAc), B(fmt.Sprint(k+1), "")
			if k == sel {
				marker, num, bg = T("▸ ", tAc), B(fmt.Sprint(k+1), tAc), tSel
			}
			row = L(marker, num, "  ", col(B(o.Label, ""), 12), T(o.Description, tMu))
			if o.Recommended {
				row = append(row, spaces(3, ""), T("recomendada", tDone))
			}
		}
		out = append(out, block([]line{row}, ww, bg, 3)...)
	}

	note := "Si no contestás acá, la pregunta vuelve a Claude Code."
	if isFork(d) {
		note = "Se contesta en Ingeniero: enter la abre."
	}
	bottom := []line{nil, L(T(note, tSb)), nil}
	return append(out, block(bottom, ww, tBg2, 3)...)
}
