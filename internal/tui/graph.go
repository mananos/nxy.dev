package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/mananos/nxy.dev/internal/views"
)

// The batches drawn as a graph without boxes: one filled label per batch (tonal by state, solid
// for the one under the cursor) and lines between them. Batches go in layers by how deep their
// dependencies go; with a single repo they come out in a row.

const (
	graphNodeW  = 24 // a label with two repos
	chainNodeW  = 20 // a label in a row
	graphConnW  = 5
	chainConnW  = 2
	maxFallback = 30
)

// batchTok is the color token of the state of a batch; pending has none.
func batchTok(s views.StepStatus) tok {
	switch s {
	case views.StepGreen, views.StepAccepted:
		return tDone
	case views.StepFailed:
		return tFail
	case views.StepRunning:
		return tWork
	case views.StepVerifying:
		return tVer
	}
	return ""
}

func batchDone(s views.StepStatus) bool { return s == views.StepGreen || s == views.StepAccepted }

// shorten cuts a title to n cells on a word, leaving out the connectors that would end it
// ("Componente de nota parcial" -> "Componente").
func shorten(title string, n int) string {
	if ansi.StringWidth(title) <= n {
		return title
	}
	words := strings.Fields(title)
	for len(words) > 1 && ansi.StringWidth(strings.Join(words, " ")) > n {
		words = words[:len(words)-1]
	}
	for len(words) > 1 {
		switch strings.ToLower(words[len(words)-1]) {
		case "de", "del", "la", "el", "y", "a", "en", "con", "para", "por", "al", "que", "un", "una", "su", "sus", "los", "las":
			words = words[:len(words)-1]
			continue
		}
		break
	}
	s := strings.Join(words, " ")
	if ansi.StringWidth(s) > n {
		s = ansi.Truncate(s, n, "…")
	}
	return s
}

// batchNode is the label of one batch, exactly w cells.
func (st *state) batchNode(b views.PlanBatch, sel bool, w int, repo string) seg {
	left := " " + st.stepGlyph(b.Status).Text + " " + fmt.Sprint(b.N) + " "
	right := " "
	if repo != "" {
		right = repo + " "
	}
	room := w - ansi.StringWidth(left) - ansi.StringWidth(right)
	title := shorten(b.Title, max(room, 1))
	text := left + title
	text += strings.Repeat(" ", max(w-ansi.StringWidth(text)-ansi.StringWidth(right), 0)) + right
	style := "ch-mute"
	k := batchTok(b.Status)
	switch {
	case sel && k == "":
		style = "ch-ac"
	case sel:
		style = "ch-" + string(k)
	case k != "":
		style = "tn-" + string(k)
	}
	s := S(ansi.Truncate(text, w, ""), style)
	s.Mark = sel
	return s
}

// riskWord is the risk of a batch in words and the color it gets.
func riskWord(risk string) (string, tok) {
	switch risk {
	case "high":
		return "alto", tFail
	case "medium":
		return "medio", tNeed
	}
	return "bajo", tMu
}

// notable says whether the risk is worth showing under the graph.
func notable(b views.PlanBatch) bool { return b.Risk == "high" || b.Risk == "medium" }

// batchLayers groups batches by depth: layer 0 has no dependencies. A dependency on a batch
// that is not in the plan is ignored.
func batchLayers(batches []views.PlanBatch) [][]views.PlanBatch {
	byN := map[int]views.PlanBatch{}
	for _, b := range batches {
		byN[b.N] = b
	}
	depth := map[int]int{}
	var walk func(n int, seen map[int]bool) int
	walk = func(n int, seen map[int]bool) int {
		if d, ok := depth[n]; ok {
			return d
		}
		if seen[n] {
			return 0
		}
		seen[n] = true
		d := 0
		for _, x := range byN[n].Depends {
			if _, ok := byN[x]; ok {
				d = max(d, walk(x, seen)+1)
			}
		}
		depth[n] = d
		return d
	}
	var layers [][]views.PlanBatch
	for _, b := range batches {
		d := walk(b.N, map[int]bool{})
		for len(layers) <= d {
			layers = append(layers, nil)
		}
		layers[d] = append(layers[d], b)
	}
	for _, l := range layers {
		sort.Slice(l, func(i, j int) bool { return l[i].N < l[j].N })
	}
	return layers
}

// parallelBatches is the biggest layer with 2+ batches ("el 2 y el 3"), or nil.
func parallelBatches(batches []views.PlanBatch) []int {
	var best []int
	for _, l := range batchLayers(batches) {
		if len(l) > 1 && len(l) > len(best) {
			best = best[:0:0]
			for _, b := range l {
				best = append(best, b.N)
			}
		}
	}
	return best
}

// nlist is "el 2", "el 2 y el 3" or "el 2, el 3 y el 4" (art: "el" or "al").
func nlist(ns []int, art string) string {
	p := make([]string, len(ns))
	for i, n := range ns {
		p[i] = fmt.Sprintf("%s %d", art, n)
	}
	switch len(p) {
	case 0:
		return ""
	case 1:
		return p[0]
	}
	return strings.Join(p[:len(p)-1], ", ") + " y " + p[len(p)-1]
}

type gnode struct {
	b     views.PlanBatch
	layer int
	row   int
}

// connector is the 5-cell piece between two layers at one row: arms on the sides and a bus in
// the middle that joins the parents with the children.
func connector(r int, parents, children map[int]bool, top, bottom int) (string, bool) {
	l, rr := parents[r], children[r]
	arm := func(on bool) string {
		if on {
			return "──"
		}
		return "  "
	}
	if r < top || r > bottom {
		return strings.Repeat(" ", graphConnW), false
	}
	var mid string
	above, below := r > top, r < bottom
	switch {
	case !above && !below:
		mid = "─"
	case !above && l && rr:
		mid = "┬"
	case !above && l:
		mid = "┐"
	case !above:
		mid = "┌"
	case !below && l && rr:
		mid = "┴"
	case !below && l:
		mid = "┘"
	case !below:
		mid = "└"
	case l && rr:
		mid = "┼"
	case l:
		mid = "┤"
	case rr:
		mid = "├"
	default:
		mid = "│"
	}
	return arm(l) + mid + arm(rr), true
}

// batchGraph draws the batches in w cells. With a single repo (or no parallel batches) they go
// in a row; otherwise in layers. When that does not fit, one row per batch with what it waits
// for. In an approval the risk of the medium and high batches goes under their labels.
func (st *state) batchGraph(batches []views.PlanBatch, sel, w int, short func(string) string, approval bool) []line {
	layers := batchLayers(batches)
	if len(layers) == 0 {
		return nil
	}
	chain := true
	for _, l := range layers {
		if len(l) != 1 {
			chain = false
		}
	}
	nodeW, connW := graphNodeW, graphConnW
	if chain {
		nodeW, connW = chainNodeW, chainConnW
	}
	if need := len(layers)*nodeW + (len(layers)-1)*connW; need > w {
		return st.graphRows(batches, sel, w, short, approval)
	}
	repoOf := func(b views.PlanBatch) string {
		if chain {
			return ""
		}
		return short(b.Repo)
	}

	if chain {
		var row, risk line
		hasRisk := false
		for i, l := range layers {
			b := l[0]
			if i > 0 {
				prev := layers[i-1][0]
				c := tSb
				if batchDone(prev.Status) {
					c = tAc
				}
				row = append(row, T(strings.Repeat("─", connW), c))
				risk = append(risk, spaces(connW, ""))
			}
			row = append(row, st.batchNode(b, b.N == sel, nodeW, repoOf(b)))
			if approval && notable(b) {
				word, c := riskWord(b.Risk)
				risk = append(risk, fit(L(T(" riesgo ", tSb), T(word, c)), nodeW)...)
				hasRisk = true
			} else {
				risk = append(risk, spaces(nodeW, ""))
			}
		}
		out := []line{row}
		if hasRisk {
			out = append(out, risk)
		}
		return out
	}

	// rows: layer 0 from the top, two rows apart; a single batch goes between its parents
	nodes := map[int]*gnode{}
	var all [][]*gnode
	for i, l := range layers {
		var gl []*gnode
		for _, b := range l {
			gl = append(gl, &gnode{b: b, layer: i})
			nodes[b.N] = gl[len(gl)-1]
		}
		all = append(all, gl)
	}
	parentsOf := func(n *gnode) []*gnode {
		var ps []*gnode
		if n.layer == 0 {
			return nil
		}
		for _, d := range n.b.Depends {
			if p, ok := nodes[d]; ok && p.layer == n.layer-1 {
				ps = append(ps, p)
			}
		}
		return ps
	}
	nrows := 1
	for i, gl := range all {
		for j, n := range gl {
			if i == 0 {
				n.row = 2 * j
				continue
			}
			lo, hi := -1, -1
			for _, p := range func() []*gnode {
				if len(gl) == 1 {
					return parentsOf(n)
				}
				var ps []*gnode
				for _, x := range gl {
					ps = append(ps, parentsOf(x)...)
				}
				return ps
			}() {
				if lo < 0 || p.row < lo {
					lo = p.row
				}
				hi = max(hi, p.row)
			}
			if lo < 0 {
				lo, hi = 0, 0
			}
			if len(gl) == 1 {
				n.row = (lo + hi) / 2
			} else {
				n.row = lo + 2*j
			}
		}
		for _, n := range gl {
			nrows = max(nrows, n.row+1)
		}
	}
	rows := make([]line, nrows)
	for r := range rows {
		for i, gl := range all {
			var cell line
			for _, n := range gl {
				if n.row == r {
					cell = L(st.batchNode(n.b, n.b.N == sel, nodeW, repoOf(n.b)))
				}
			}
			if cell == nil {
				cell = L(spaces(nodeW, ""))
			}
			rows[r] = append(rows[r], cell...)
			if i == len(all)-1 {
				continue
			}
			parents, children := map[int]bool{}, map[int]bool{}
			top, bottom := nrows, -1
			allDone := true
			for _, c := range all[i+1] {
				ps := parentsOf(c)
				if len(ps) == 0 {
					continue
				}
				children[c.row] = true
				top, bottom = min(top, c.row), max(bottom, c.row)
				for _, p := range ps {
					parents[p.row] = true
					top, bottom = min(top, p.row), max(bottom, p.row)
					if !batchDone(p.b.Status) {
						allDone = false
					}
				}
			}
			s, on := connector(r, parents, children, top, bottom)
			c := tSb
			if allDone {
				c = tAc
			}
			if !on {
				c = ""
			}
			rows[r] = append(rows[r], T(s, c))
		}
	}
	return rows
}

// graphRows is the graph when it does not fit: one row per batch.
func (st *state) graphRows(batches []views.PlanBatch, sel, w int, short func(string) string, approval bool) []line {
	nw := min(maxFallback, max(w-24, 16))
	var out []line
	for _, b := range batches {
		l := L(st.batchNode(b, b.N == sel, nw, short(b.Repo)))
		if len(b.Depends) > 0 {
			l = append(l, T("  espera a "+joinInts(b.Depends), tMu))
		}
		if approval && notable(b) {
			word, c := riskWord(b.Risk)
			l = append(l, T("  riesgo ", tSb), T(word, c))
		}
		out = append(out, l)
	}
	return out
}
