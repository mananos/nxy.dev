package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mananos/nxy.dev/v2/internal/views"
)

func init() { register(featureScreen{}) }

type featureScreen struct{}

func (featureScreen) id() string                     { return "feature" }
func (featureScreen) section() string                { return "inicio" }
func (featureScreen) crumb(_ *state, r route) string { return r.key }

func (featureScreen) keys(*state, route) []hint {
	return []hint{{"d", "decidir"}, {"a", "agentes"}, {"p", "plan"}, {"enter", "ir al agente"}, {"↑↓", "lote"}, {"space", "pausar/reanudar"}}
}

// findFeature is the cockpit row of a feature.
func (st *state) findFeature(id string) (views.CockpitFeature, bool) {
	for _, f := range st.data.Cockpit.Features {
		if f.ID == id {
			return f, true
		}
	}
	return views.CockpitFeature{}, false
}

// shortModel is "sonnet-4.6" for "claude-sonnet-4-6".
func shortModel(m string) string {
	m = strings.TrimPrefix(m, "claude-")
	p := strings.Split(m, "-")
	isNum := func(s string) bool {
		if s == "" {
			return false
		}
		for _, c := range s {
			if c < '0' || c > '9' {
				return false
			}
		}
		return true
	}
	if n := len(p); n >= 3 && isNum(p[n-1]) && isNum(p[n-2]) {
		return strings.Join(p[:n-2], "-") + "-" + p[n-2] + "." + p[n-1]
	}
	return m
}

// modelEffort is "sonnet-4.6 · high".
func modelEffort(n *views.AgentNode, sep string) string {
	if n.Model == "" {
		return ""
	}
	if n.Effort == "" {
		return shortModel(n.Model)
	}
	return shortModel(n.Model) + sep + n.Effort
}

// stepGlyph is the icon of a batch or agent state.
func (st *state) stepGlyph(s views.StepStatus) seg {
	switch s {
	case views.StepGreen, views.StepAccepted:
		return T(st.ic.Done, tDone)
	case views.StepFailed:
		return T(st.ic.Failed, tFail)
	case views.StepRunning:
		return T(st.ic.Working, tWork)
	case views.StepVerifying:
		return T(st.ic.Verifying, tVer)
	}
	return T(st.ic.Pending, tSb)
}

// repoShortener names a repo inside a feature with 2+ repos by what they do not share
// ("facturacion-api" -> "api"); with one repo it is left out.
func repoShortener(repos []views.RepoRef) func(string) string {
	if len(repos) < 2 {
		return func(string) string { return "" }
	}
	prefix := repos[0].Name
	for _, r := range repos[1:] {
		i := 0
		for i < len(prefix) && i < len(r.Name) && prefix[i] == r.Name[i] {
			i++
		}
		prefix = prefix[:i]
	}
	if i := strings.LastIndexAny(prefix, "-."); i >= 0 {
		prefix = prefix[:i+1]
	} else {
		prefix = ""
	}
	return func(n string) string {
		if s := strings.TrimPrefix(n, prefix); s != "" {
			return s
		}
		return n
	}
}

func subtreeCost(n *views.AgentNode) float64 {
	c := n.CostUSD
	for i := range n.Children {
		c += subtreeCost(&n.Children[i])
	}
	return c
}

func countAgents(n *views.AgentNode) int {
	c := 1
	for i := range n.Children {
		c += countAgents(&n.Children[i])
	}
	return c
}

// batchNodes are the implementer agents of a feature, by batch number.
func batchNodes(a *views.Agents) []*views.AgentNode {
	if a == nil {
		return nil
	}
	var out []*views.AgentNode
	for i := range a.Root.Children {
		if n := &a.Root.Children[i]; n.Role == "implementer" && n.Batch > 0 {
			out = append(out, n)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Batch < out[j].Batch })
	return out
}

// startOnRed is the row the cursor starts on: the first red batch, else the first.
func startOnRed(rows []*views.AgentNode) int {
	for i, n := range rows {
		if n.Status == views.StepFailed {
			return i
		}
	}
	return 0
}

func (st *state) featureCursor(r route, rows []*views.AgentNode) int {
	sel, ok := st.cur[r.ckey()]
	if !ok {
		sel = startOnRed(rows)
	}
	return min(max(sel, 0), max(len(rows)-1, 0))
}

func attemptText(n *views.AgentNode) string {
	switch {
	case n.Attempts > 0 && n.Attempt > 0:
		return fmt.Sprintf("intento %d de %d", n.Attempt, n.Attempts)
	case n.Attempt > 0:
		return fmt.Sprintf("intento %d", n.Attempt)
	}
	return ""
}

func joinInts(v []int) string {
	p := make([]string, len(v))
	for i, x := range v {
		p[i] = fmt.Sprint(x)
	}
	return strings.Join(p, ",")
}

// refsText is "K7 (no la citó) y A5": what an agent received and did not quote.
func refsText(refs []views.MemRef) line {
	var out line
	for i, m := range refs {
		switch {
		case i == 0:
		case i == len(refs)-1:
			out = append(out, T(" y ", tMu))
		default:
			out = append(out, T(", ", tMu))
		}
		out = append(out, B(m.ID, ""))
		if !m.Cited {
			no := " (no la citó)"
			if m.Kind == "learning" {
				no = " (no lo citó)"
			}
			out = append(out, T(no, tMu))
		}
	}
	return out
}

func rootState(f views.CockpitFeature) string {
	switch f.Status {
	case views.StatusNeedsYou:
		return "esperando tu decisión"
	case views.StatusVerifying:
		return "verificando"
	case views.StatusDone:
		return "cerrada"
	}
	return "trabajando"
}

func roleName(role string) string {
	if role == "" {
		return role
	}
	return strings.ToUpper(role[:1]) + role[1:]
}

func nonEmpty(s ...string) []string {
	var out []string
	for _, x := range s {
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}

// featureHead is the title block of a feature (title, id, state chip, repos · branch · time ·
// cost) and the stepper under it. The Plan screen draws the same one.
func featureHead(f views.FeatureHeader, w int) []line {
	iw := w - 4 - 6
	var names []string
	branch := ""
	for _, x := range f.Repos {
		names = append(names, x.Name)
		if branch == "" {
			branch = x.Branch
		}
	}
	meta := []string{strings.Join(names, " + ")}
	if branch != "" {
		meta = append(meta, branch)
	}
	meta = append(meta, durText(f.ElapsedMs), costText(f.CostUSD, f.CostEstimated))
	l1 := lr(L(T("←", tMu), "   ", B(f.Title, ""), "   ", T(f.ID, tSb)), chip(statusLabel(f.Status), statusTok(f.Status), chipW), iw)
	l2 := L("    ", T(strings.Join(nonEmpty(meta...), " · "), tMu))
	out := []line{nil}
	out = append(out, indent(block([]line{l1, l2}, w-4, tBg1, 3), 2)...)
	return append(out, nil, L("  ", stepper(f.Stage, f.StageDetail)), nil)
}

func (featureScreen) view(st *state, r route, w int) ([]line, int) {
	f, ok := st.findFeature(r.key)
	if !ok {
		return st.missing("cockpit", "No hay datos de "+r.key+".", w), 0
	}
	a := st.data.Agents[r.key]
	rows := batchNodes(a)
	sel := st.featureCursor(r, rows)
	short := repoShortener(f.Repos)
	iw := w - 4 - 6
	out := featureHead(f.FeatureHeader, w)

	if len(rows) == 0 {
		if err := st.data.Errs["agents/"+r.key]; err != nil {
			out = append(out, indent(errBlock(err.Error(), w-4), 2)...)
		} else if st.data.Loading["agents/"+r.key] {
			out = append(out, L("  ", loadingLine()))
		} else {
			out = append(out, L("  ", T("Sin detalle de lotes todavía.", tMu)))
		}
		return out, 0
	}

	// one row per batch: the model falls first when it does not fit, then the time
	rw := 0
	if len(f.Repos) > 1 {
		rw = 6
	}
	timeW := 5
	base := 3 + 3 + rw + timeW + 8 + 3 + 10
	mw := 0
	if iw-base >= 30+21 {
		mw = 21
	}
	if iw-base-mw < 12 {
		timeW = 0
		base -= 5
	}
	tw := min(max(iw-base-mw, 12), 30)
	focus := len(out)
	for i, n := range rows {
		pending := n.Status == views.StepPending
		title := T(n.Title, "")
		switch {
		case n.Status == views.StepFailed:
			title = B(n.Title, "")
		case pending:
			title = T(n.Title, tMu)
		}
		extra := T("", tMu)
		switch {
		case n.Attempt > 1:
			extra = T(fmt.Sprintf("intento %d", n.Attempt), tFail)
		case len(n.WaitingOn) > 0:
			extra = T("espera a "+joinInts(n.WaitingOn), tMu)
		}
		model, when, cost := "", "", ""
		if !pending {
			model = modelEffort(n, " · ")
			when = durText(n.ElapsedMs)
			cost = costText(subtreeCost(n), n.CostEstimated)
		}
		l := L(st.stepGlyph(n.Status), "  ", col(fmt.Sprint(n.Batch), 3), col(title, tw-1), " ")
		if rw > 0 {
			l = L(l, col(T(short(n.Repo), tMu), rw))
		}
		if mw > 0 {
			l = L(l, col(T(model, tMu), mw))
		}
		if timeW > 0 {
			l = L(l, rcol(T(when, tMu), timeW))
		}
		l = L(l, rcol(T(cost, ""), 8), "   ", extra)
		bg := tBg1
		if i == sel {
			bg = tSel
			focus = len(out)
		}
		out = append(out, indent(block([]line{l}, w-4, bg, 3), 2)...)
	}

	// the red block: the batch under the cursor if it failed, else the first red one
	var red *views.AgentNode
	if n := rows[sel]; n.Status == views.StepFailed && n.Failure != nil {
		red = n
	} else {
		for _, n := range rows {
			if n.Status == views.StepFailed && n.Failure != nil {
				red = n
				break
			}
		}
	}
	if red != nil {
		label := S(fmt.Sprintf(" Lote %d ", red.Batch), "ch-fail")
		head := L(label, "   ", T(strings.Join(nonEmpty(attemptText(red), modelEffort(red, " "), durText(red.ElapsedMs)), " · "), tMu))
		pad := spaces(lw(L(label))+3, "")
		lines := []line{head, L(pad, T(red.Failure.Test, tMu))}
		for _, s := range red.Failure.Lines {
			lines = append(lines, L(pad, s))
		}
		rec := L(pad, T("no recibió reglas", tMu))
		if len(red.Received) > 0 {
			rec = L(pad, T("recibió ", tMu), refsText(red.Received))
		}
		if len(red.Learned) == 0 {
			rec = append(rec, T("  ·  no aprendió nada", tMu))
		} else {
			rec = append(rec, T("  ·  aprendió "+plural(len(red.Learned), "cosa", "cosas"), tMu))
		}
		lines = append(lines, rec)
		out = append(out, nil)
		out = append(out, indent(block(lines, w-4, tFailBg, 3), 2)...)
	}

	// the main thread, the planner and the rest, one line each
	out = append(out, nil)
	out = append(out, L("     ", B("Hilo principal", ""),
		T("   "+modelEffort(&a.Root, " · ")+" · "+rootState(f), tMu), T("   "+costText(a.Root.CostUSD, a.Root.CostEstimated), "")))
	for i := range a.Root.Children {
		n := &a.Root.Children[i]
		if n.Role == "implementer" {
			continue
		}
		l := L("     ", B(roleName(n.Role), ""),
			T("   "+strings.Join(nonEmpty(modelEffort(n, " · "), durText(n.ElapsedMs)), " · "), tMu),
			T("   "+costText(n.CostUSD, n.CostEstimated), ""))
		var scouts []*views.AgentNode
		for j := range n.Children {
			if n.Children[j].Role == "scout" {
				scouts = append(scouts, &n.Children[j])
			}
		}
		if len(scouts) > 0 {
			s := scouts[0]
			name := "scout"
			if len(scouts) > 1 {
				name = fmt.Sprintf("%d scouts", len(scouts))
			}
			sp := L(B(name, ""),
				T("   "+strings.Join(nonEmpty(modelEffort(s, " · "), durText(s.ElapsedMs)), " · "), tMu),
				T("   "+costText(s.CostUSD, s.CostEstimated), ""))
			if lw(l)+6+lw(sp) <= w {
				l = L(l, "      ", sp)
			} else {
				out = append(out, fit(l, w))
				l = L("     ", sp)
			}
		}
		out = append(out, fit(l, w))
	}
	return out, focus
}

func (featureScreen) key(st *state, r route, k string) intent {
	rows := batchNodes(st.data.Agents[r.key])
	sel := st.featureCursor(r, rows)
	switch k {
	case "up", "k":
		st.setCursor(r, max(sel-1, 0))
	case "down", "j":
		st.setCursor(r, min(sel+1, max(len(rows)-1, 0)))
	case "a":
		return intent{push: &route{screen: "agentes", key: r.key}}
	case "p":
		return intent{push: &route{screen: "plan", key: r.key}}
	case "space", " ":
		if f, ok := st.findFeature(r.key); ok {
			return st.toggleLife(f)
		}
	case "d":
		if indexOfDecision(st.pending(), r.key) < 0 {
			return intent{toast: "No hay una decisión pendiente en " + r.key + "."}
		}
		return intent{decide: true, decideFor: r.key}
	case "enter":
		if len(rows) == 0 {
			return intent{}
		}
		ar := route{screen: "agentes", key: r.key}
		for i, x := range agentRows(st, r.key) {
			if x.node == rows[sel] {
				st.setCursor(ar, i)
			}
		}
		return intent{push: &ar}
	}
	return intent{}
}
