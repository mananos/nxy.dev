package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/mananos/nxy.dev/v2/internal/format"
	"github.com/mananos/nxy.dev/v2/internal/views"
)

func init() { register(agentsScreen{}) }

type agentsScreen struct{}

func (agentsScreen) id() string                 { return "agentes" }
func (agentsScreen) section() string            { return "inicio" }
func (agentsScreen) crumb(*state, route) string { return "Agentes" }

func (agentsScreen) keys(*state, route) []hint {
	return []hint{{"↑↓", "agente"}, {"enter", "plegar"}, {"d", "decidir"}}
}

// agentRow is one visible row of the tree.
type agentRow struct {
	node    *views.AgentNode
	prefix  string // "├ ", "│ └ "
	depth   int
	hasKids bool
}

// agentRows flattens the tree of a feature, leaving out the children of the folded agents.
func agentRows(st *state, feature string) []agentRow {
	a := st.data.Agents[feature]
	if a == nil {
		return nil
	}
	var out []agentRow
	var walk func(n *views.AgentNode, cont string, last bool, depth int)
	walk = func(n *views.AgentNode, cont string, last bool, depth int) {
		prefix := ""
		if depth > 0 {
			prefix = cont + "├ "
			if last {
				prefix = cont + "└ "
			}
		}
		out = append(out, agentRow{node: n, prefix: prefix, depth: depth, hasKids: len(n.Children) > 0})
		if st.folded[feature+"/"+n.ID] {
			return
		}
		next := cont
		if depth > 0 {
			next = cont + "│ "
			if last {
				next = cont + "  "
			}
		}
		for i := range n.Children {
			walk(&n.Children[i], next, i == len(n.Children)-1, depth+1)
		}
	}
	walk(&a.Root, "", true, 0)
	return out
}

func (st *state) agentsCursor(r route, rows []agentRow) int {
	sel, ok := st.cur[r.ckey()]
	if !ok {
		sel = 0
		for i, x := range rows {
			if x.node.Status == views.StepFailed && x.node.Role == "implementer" {
				sel = i
				break
			}
		}
	}
	return min(max(sel, 0), max(len(rows)-1, 0))
}

// agentLabel is the short name of an agent in the tree.
func agentLabel(n *views.AgentNode, short func(string) string) string {
	switch n.Role {
	case "main":
		return "Hilo principal"
	case "implementer":
		s := fmt.Sprintf("lote %d", n.Batch)
		if r := short(n.Repo); r != "" {
			s += " · " + r
		}
		return s
	}
	return n.Role
}

func (agentsScreen) view(st *state, r route, w int) ([]line, int) {
	a := st.data.Agents[r.key]
	f, _ := st.findFeature(r.key)
	if a == nil {
		return st.missing("agents/"+r.key, "No hay agentes de "+r.key+".", w), 0
	}
	rows := agentRows(st, r.key)
	sel := st.agentsCursor(r, rows)
	short := repoShortener(f.Repos)
	iw := w - 4 - 6

	full := iw >= 88 // narrower: ↑ and cache fall, the model loses its effort
	mc := 19
	if !full {
		mc = 12
	}
	head := L(col(T("Agente", tSb), 21), col(T("Modelo", tSb), mc), rcol(T(st.ic.Down, tSb), 7))
	if full {
		head = L(head, rcol(T(st.ic.Up, tSb), 6), rcol(T("cache", tSb), 7))
	}
	head = L(head, rcol(T("tiempo", tSb), 7), rcol(T("$", tSb), 8))
	title := lr(L(B("Agentes", "")), L(T(fmt.Sprintf("%d agentes · %s", countAgents(&a.Root), costText(a.Feature.CostUSD, a.Feature.CostEstimated)), tSb)), iw)

	out := []line{nil}
	out = append(out, indent(block([]line{title, head}, w-4, tBg1, 3), 2)...)
	focus := len(out)
	for i, x := range rows {
		n := x.node
		label := agentLabel(n, short)
		if x.hasKids && st.folded[r.key+"/"+n.ID] {
			label += " +"
		}
		name := T(label, "")
		if n.Status == views.StepPending {
			name = T(label, tMu)
		}
		if i == sel || n.Status == views.StepFailed && n.Role == "implementer" {
			name.Bold = true
		}
		model := shortModel(n.Model)
		if full {
			model = modelEffort(n, " · ")
		}
		var tin, tout, cache, when, cost string
		if n.Status != views.StepPending {
			tin, tout = format.FmtTokens(float64(n.TokensIn)), format.FmtTokens(float64(n.TokensOut))
			if n.CacheHitPct != nil {
				cache = fmt.Sprintf("%.0f %%", *n.CacheHitPct)
			}
			when = durText(n.ElapsedMs)
			cost = costText(n.CostUSD, n.CostEstimated)
		}
		note := T("", tMu)
		switch {
		case n.Attempt > 1:
			note = T(fmt.Sprintf("intento %d", n.Attempt), tFail)
		case len(n.WaitingOn) > 0:
			note = T("espera a "+joinInts(n.WaitingOn), tMu)
		case x.depth == 0 && f.Status == views.StatusNeedsYou:
			note = T("esperando", tMu)
		case n.CurrentTool != nil:
			note = T(n.CurrentTool.Name, tMu)
		}
		pw := ansi.StringWidth(x.prefix)
		l := L(T(x.prefix, tSb), st.stepGlyph(n.Status), " ", col(name, max(19-pw, 4)), col(T(model, tMu), mc), rcol(T(tin, tMu), 7))
		if full {
			l = L(l, rcol(T(tout, tMu), 6), rcol(T(cache, tMu), 7))
		}
		l = L(l, rcol(T(when, tMu), 7), rcol(B(cost, ""), 8), "   ", note)
		bg := tBg1
		if i == sel {
			bg = tSel
			focus = len(out)
		}
		out = append(out, indent(block([]line{l}, w-4, bg, 3), 2)...)
	}

	// the detail of the agent under the cursor
	if len(rows) > 0 {
		out = append(out, nil)
		out = append(out, indent(block(agentDetail(rows[sel].node, f, short, iw), w-4, tBg1, 3), 2)...)
	}
	return out, focus
}

// agentDetail is what one agent did: tools, what it received and what it learned.
func agentDetail(n *views.AgentNode, f views.CockpitFeature, short func(string) string, iw int) []line {
	var left line
	if n.Role == "implementer" {
		left = L(B(fmt.Sprintf("Lote %d", n.Batch), ""))
		sub := nonEmpty(short(n.Repo), attemptText(n))
		if len(sub) > 0 {
			left = append(left, T(" · "+strings.Join(sub, " · "), tMu))
		}
	} else {
		left = L(B(n.Label, ""))
	}
	out := []line{lr(left, L(T(modelEffort(n, " · "), tSb)), iw)}

	tools := T("todavía no corrió", tSb)
	if len(n.Tools) > 0 {
		var p []string
		for _, t := range n.Tools {
			s := fmt.Sprintf("%s %d", t.Name, t.Count)
			if t.Detail != "" {
				s += " (" + t.Detail + ")"
			}
			p = append(p, s)
		}
		tools = T(strings.Join(p, " · "), tMu)
	}
	out = append(out, L(col(T("herramientas", tSb), 14), tools))

	if len(n.Received) == 0 {
		out = append(out, L(col(T("recibió", tSb), 14), T("nada", tSb)))
	}
	uncited := ""
	for i, m := range n.Received {
		lead := L(spaces(14, ""))
		if i == 0 {
			lead = col(T("recibió", tSb), 14)
		}
		row := L(lead, B(m.ID, ""), "  ", m.Text)
		if !m.Cited && m.Kind == "rule" {
			row = append(row, T("   no la citó", tNeed))
			if uncited == "" {
				uncited = m.ID
			}
		}
		out = append(out, row)
	}
	if len(n.Learned) == 0 {
		out = append(out, L(col(T("aprendió", tSb), 14), T("nada", tSb)))
	}
	for i, s := range n.Learned {
		lead := L(spaces(14, ""))
		if i == 0 {
			lead = col(T("aprendió", tSb), 14)
		}
		out = append(out, L(lead, s))
	}
	if n.Status == views.StepFailed && uncited != "" {
		out = append(out, nil, L(T("Falló teniendo la regla "+uncited+": la recibió y no la citó.", tMu)))
	}
	return out
}

func (agentsScreen) key(st *state, r route, k string) intent {
	rows := agentRows(st, r.key)
	sel := st.agentsCursor(r, rows)
	switch k {
	case "up", "k":
		st.setCursor(r, max(sel-1, 0))
	case "down", "j":
		st.setCursor(r, min(sel+1, max(len(rows)-1, 0)))
	case "enter":
		if len(rows) > 0 && rows[sel].hasKids {
			id := r.key + "/" + rows[sel].node.ID
			st.folded[id] = !st.folded[id]
			st.setCursor(r, sel)
		}
	case "d":
		if indexOfDecision(st.pending(), r.key) < 0 {
			return intent{toast: "No hay una decisión pendiente en " + r.key + "."}
		}
		return intent{decide: true, decideFor: r.key}
	}
	return intent{}
}
