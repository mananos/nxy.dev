package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/mananos/nxy.dev/v2/internal/views"
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

// decQ is one question of a Decision: the first one and the ones in Rest.
type decQ struct {
	header, question string
	multi            bool
	opts             []views.Option
}

// decQuestions is every question of d, in the order they are asked.
func decQuestions(d *views.Decision) []decQ {
	qs := []decQ{{d.Header, d.Question, d.MultiSelect, d.Options}}
	for _, r := range d.Rest {
		qs = append(qs, decQ{r.Header, r.Question, r.MultiSelect, r.Options})
	}
	return qs
}

// decCurrent is the question the window is on for d, and its place: 0-based index and total.
func (st *state) decCurrent(d *views.Decision) (q decQ, idx, total int) {
	if old, ok := st.decAsk[d.FeatureID]; ok && old != d.AskID {
		st.decReset(d.FeatureID)
	}
	st.decAsk[d.FeatureID] = d.AskID
	qs := decQuestions(d)
	idx = min(max(st.decIdx[d.FeatureID], 0), len(qs)-1)
	return qs[idx], idx, len(qs)
}

// decReset forgets the progress of d: the answers given, the marks and the cursor.
func (st *state) decReset(feature string) {
	delete(st.decIdx, feature)
	delete(st.decAsk, feature)
	delete(st.decAns, feature)
	delete(st.decMark, feature)
	delete(st.cur, "decision/"+feature)
}

// decOption is the option under the cursor: the recommended one until the user moves.
func (st *state) decOption(d *views.Decision, opts []views.Option) int {
	if v, ok := st.cur["decision/"+d.FeatureID]; ok {
		return min(max(v, 0), max(len(opts)-1, 0))
	}
	for i, o := range opts {
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
		q, _, _ := st.decCurrent(d)
		if q.multi {
			h = append(h, hint{"space", "marcar"})
		}
		h = append(h, hint{fmt.Sprintf("1-%d", len(q.opts)), "elegir"}, hint{"enter", "confirmar"})
		if d.AskID != "" {
			h = append(h, hint{"t", "en la terminal"})
		}
	}
	if n > 1 {
		h = append(h, hint{"tab", "la otra"})
	}
	return append(h, hint{"esc", "después"})
}

// terminalText is what the window says when no hook is waiting for the answer.
const terminalText = "contestá en la terminal de Claude Code"

// decAnswer takes the choice of the current question; with the last one it runs the decide.
func (st *state) decAnswer(d *views.Decision) {
	q, idx, total := st.decCurrent(d)
	if len(q.opts) == 0 {
		return
	}
	if d.AskID == "" {
		st.toast, st.toastErr = terminalText, false
		return
	}
	feat := d.FeatureID
	sel := st.decOption(d, q.opts)
	label := q.opts[sel].Label
	if q.multi {
		var ls []string
		for i, o := range q.opts {
			if st.decMark[feat][i] {
				ls = append(ls, o.Label)
			}
		}
		if len(ls) > 0 {
			label = strings.Join(ls, ", ")
		}
	}
	confirm := q.opts[sel].Action.Confirm
	st.decAns[feat] = append(st.decAns[feat], label)
	if idx+1 < total {
		st.decIdx[feat] = idx + 1
		delete(st.decMark, feat)
		delete(st.cur, "decision/"+feat)
		return
	}
	argv := []string{"nxy", "action", "decide", "--ask", d.AskID}
	for _, a := range st.decAns[feat] {
		argv = append(argv, "--answer", a)
	}
	st.decReset(feat)
	a := views.Action{ID: "decide", Label: label, Argv: argv, Confirm: confirm}
	if t := st.runAction(actionReq{label: "decidir", a: a, reloadOnErr: true, after: func() {
		st.answered[feat] = true
		st.decNext()
	}}); t != "" {
		st.toast, st.toastErr = t, false
	}
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
		q, _, _ := st.decCurrent(d)
		sel := st.decOption(d, q.opts)
		if k == "up" || k == "k" {
			sel--
		} else {
			sel++
		}
		st.cur["decision/"+d.FeatureID] = min(max(sel, 0), max(len(q.opts)-1, 0))
	case "space":
		q, _, _ := st.decCurrent(d)
		if isFork(d) || !q.multi || len(q.opts) == 0 {
			break
		}
		sel := st.decOption(d, q.opts)
		if st.decMark[d.FeatureID] == nil {
			st.decMark[d.FeatureID] = map[int]bool{}
		}
		st.decMark[d.FeatureID][sel] = !st.decMark[d.FeatureID][sel]
	case "t":
		if isFork(d) {
			break
		}
		if d.AskID == "" {
			st.toast, st.toastErr = terminalText, false
			break
		}
		feat := d.FeatureID
		if t := st.runAction(actionReq{label: "contestar en la terminal", a: d.Release, reloadOnErr: true, after: func() {
			st.decReset(feat)
			st.answered[feat] = true
			st.decNext()
		}}); t != "" {
			st.toast, st.toastErr = t, false
		}
	case "enter":
		if isFork(d) {
			// a fork is answered in Ingeniero, not here
			st.closeDecision()
			st.goSection("inicio")
			st.stack = append(st.stack, route{screen: "ingeniero", key: d.FeatureID})
			break
		}
		st.decAnswer(d)
	default:
		if len(k) == 1 && k[0] >= '1' && k[0] <= '9' && !isFork(d) {
			q, _, _ := st.decCurrent(d)
			if v := int(k[0] - '1'); v < len(q.opts) {
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
	dq, qi, qn := st.decCurrent(d)
	ww := min(decisionWidth, cw-6)
	iw := ww - 6
	head := lr(
		L(chip("Te necesita", tNeed, chipW), "   ", B(dq.header, "")),
		L(T(d.FeatureID+" · "+d.Repo, tSb), "  ", S(fmt.Sprintf(" %d de %d ", i+1, n), "ch-mute")), iw)

	top := []line{nil, head, nil}
	if qn > 1 {
		top = append(top, L(T(fmt.Sprintf("pregunta %d de %d", qi+1, qn), tSb)))
	}
	for _, s := range wrap(dq.question, iw) {
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

	sel := st.decOption(d, dq.opts)
	for k, o := range dq.opts {
		var row line
		bg := tBg2
		if isFork(d) {
			row = L("  ", S(" "+string(rune('A'+k))+" ", "ch-mute"), "  ", B(o.Label, ""), "  ", T(o.Description, tMu))
		} else {
			marker, num := T("  ", tAc), B(fmt.Sprint(k+1), "")
			if k == sel {
				marker, num, bg = T("▸ ", tAc), B(fmt.Sprint(k+1), tAc), tSel
			}
			if dq.multi && st.decMark[d.FeatureID][k] {
				marker = T("✓ ", tDone)
			}
			row = L(marker, num, "  ", col(B(o.Label, ""), 12), T(o.Description, tMu))
			if o.Recommended {
				row = append(row, spaces(3, ""), T("recomendada", tDone))
			}
		}
		out = append(out, block([]line{row}, ww, bg, 3)...)
	}

	note := "No espera a la TUI: " + terminalText + "."
	if d.AskID != "" {
		note = "t contesta en la terminal."
	}
	if isFork(d) {
		note = "Se contesta en Ingeniero: enter la abre."
	}
	bottom := []line{nil, L(T(note, tSb)), nil}
	return append(out, block(bottom, ww, tBg2, 3)...)
}
