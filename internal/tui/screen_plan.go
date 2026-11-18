package tui

import (
	"fmt"
	"path"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/mananos/nxy.dev/v2/internal/views"
)

func init() { register(planScreen{}) }

type planScreen struct{}

func (planScreen) id() string                 { return "plan" }
func (planScreen) section() string            { return "inicio" }
func (planScreen) crumb(*state, route) string { return "Plan" }

func (planScreen) keys(st *state, r route) []hint {
	if p := st.data.Plans[r.key]; p != nil && p.State == views.PlanApproval {
		return []hint{{"a", "aprobar"}, {"c", "cambiar"}, {"x", "parar"}, {"←→", "lote"}, {"enter", "código"}}
	}
	return []hint{{"←→", "lote"}, {"↑↓", "archivo"}, {"enter", "ver código"}, {"a", "agentes"}, {"d", "decidir"}, {"r", "recalcular"}}
}

// planBatch is the position of the batch under the cursor. It starts on the red batch of a plan
// that runs and on the first one of a plan that waits for approval.
func (st *state) planBatch(r route, p *views.Plan) int {
	if v, ok := st.cur[r.ckey()+"#b"]; ok {
		return min(max(v, 0), max(len(p.Batches)-1, 0))
	}
	if p.State != views.PlanApproval {
		for i, b := range p.Batches {
			if b.Status == views.StepFailed {
				return i
			}
		}
	}
	return 0
}

// planFiles are the files of the impact map that belong to a batch.
func planFiles(p *views.Plan, n int) []views.ImpactFile {
	var out []views.ImpactFile
	for _, f := range p.Impact.Files {
		if f.Batch == n {
			out = append(out, f)
		}
	}
	return out
}

func (st *state) planFile(r route, files []views.ImpactFile) int {
	return min(max(st.cur[r.ckey()+"#f"], 0), max(len(files)-1, 0))
}

func sourceLabel(s string) string {
	switch s {
	case "codegraph":
		return "codegraph"
	case "name":
		return "por nombre"
	}
	return "sin grafo"
}

// testGlyph is ✓ ✗ ○ for the state of a test.
func (st *state) testGlyph(status string) seg {
	switch status {
	case views.TestPass:
		return T(st.ic.Done, tDone)
	case views.TestFail:
		return T(st.ic.Failed, tFail)
	}
	return T(st.ic.Pending, tSb)
}

// paras wraps text in lines of w cells, the next ones indented by hang.
func paras(first line, text string, w, hang int) []line {
	var out []line
	for i, s := range wrap(text, max(w-hang, 8)) {
		if i == 0 {
			out = append(out, L(first, s))
		} else {
			out = append(out, L(spaces(hang, ""), s))
		}
	}
	return out
}

func (planScreen) view(st *state, r route, w int) ([]line, int) {
	p := st.data.Plans[r.key]
	if p == nil || len(p.Batches) == 0 {
		return st.missing("plan/"+r.key, "No hay un plan de "+r.key+" todavía.", w), 0
	}
	approval := p.State == views.PlanApproval
	iw := w - 4 - 6
	short := repoShortener(p.Repos)
	bi := st.planBatch(r, p)
	b := p.Batches[bi]

	out := featureHead(p.Feature, w)

	// the batches: the graph, then the one under the cursor
	var right line
	par := parallelBatches(p.Batches)
	switch {
	case approval:
		how := "en fila"
		if len(par) > 0 {
			how = nlist(par, "el") + " en paralelo"
		} else if len(p.Repos) < 2 {
			how += ", un solo repo"
		}
		right = L(T("esperando tu aprobación", tNeed), T("  ·  "+how, tSb))
	default:
		var parts []string
		if a := ago(st.data.Cockpit.GeneratedAt, p.ApprovedAt); a != "" {
			parts = append(parts, "aprobado "+a)
		}
		if len(par) > 0 {
			parts = append(parts, nlist(par, "el")+" en paralelo")
		}
		right = L(T(strings.Join(parts, " · "), tSb))
	}
	lotes := []line{lr(L(B("Lotes", "")), right, iw)}
	lotes = append(lotes, st.batchGraph(p.Batches, b.N, iw, short, approval)...)
	lotes = append(lotes, nil)

	left := L(B(fmt.Sprintf("Lote %d", b.N), ""), T(" · ", tSb))
	if s := short(b.Repo); s != "" {
		left = append(left, T(s+" · ", tSb))
	}
	left = append(left, T(b.Title, ""))
	word, wc := riskWord(b.Risk)
	rt := L(T("riesgo ", tSb), T(word, wc))
	switch {
	case b.Attempt > 1:
		rt = append(rt, T("  ·  ", tSb), T(fmt.Sprintf("intento %d de %d", b.Attempt, max(b.Attempts, b.Attempt)), tFail))
	case b.RiskWhy != "":
		rt = append(rt, T("  ·  "+b.RiskWhy, tMu))
	}
	var waits []string
	if len(b.Depends) > 0 {
		waits = append(waits, "espera "+nlist(b.Depends, "al"))
	}
	var after []int
	for _, x := range p.Batches {
		for _, d := range x.Depends {
			if d == b.N {
				after = append(after, x.N)
			}
		}
	}
	if len(after) == 1 {
		waits = append(waits, "lo espera "+nlist(after, "el"))
	} else if len(after) > 1 {
		waits = append(waits, "lo esperan "+nlist(after, "el"))
	}
	lotes = append(lotes, lr(left, rt, iw))
	lotes = append(lotes, lr(L(T("accept  ", tSb), T(b.Accept, tMu)), L(T(strings.Join(waits, "  ·  "), tMu)), iw))
	out = append(out, indent(block(lotes, w-4, tBg1, 3), 2)...)
	out = append(out, nil)

	// objective, criteria and decisions | impact of the batch
	side := w >= 100
	colW, rightW := w-4, w-4
	if side {
		colW = min((w-4-2)/2, 57) // the left keeps its width at 120; the impact takes the rest
		rightW = w - 4 - 2 - colW
	}
	goal := st.planGoal(p, short, colW-6)
	impact, selRow := st.planImpact(r, p, b, short, rightW, approval)
	base := len(out)
	focus := base
	if side {
		out = append(out, indent(hcat([][]line{block(goal, colW, tBg1, 3), impact}, []int{colW, rightW}, 2), 2)...)
		focus = base + selRow
	} else {
		out = append(out, indent(block(goal, colW, tBg1, 3), 2)...)
		out = append(out, nil)
		focus = len(out) + selRow
		out = append(out, indent(impact, 2)...)
	}
	return out, focus
}

// planGoal is the objective, the criteria and the decisions, as lines of the inner width.
func (st *state) planGoal(p *views.Plan, short func(string) string, iw int) []line {
	var g []line
	g = append(g, L(B("Objetivo", "")))
	for _, s := range wrap(p.Goal, iw) {
		g = append(g, L(s))
	}
	g = append(g, nil, L(B("Criterios", "")))
	for _, c := range p.Criteria {
		g = append(g, paras(L(T(c.ID+"  ", tSb)), c.Text, iw, 4)...)
	}
	g = append(g, nil)
	var note line
	for _, d := range p.Decisions {
		if d.FromDesign {
			note = L(T("del diseño", tSb))
		}
	}
	g = append(g, lr(L(B("Decisiones", "")), note, iw))
	for _, d := range p.Decisions {
		label := "tarea"
		if d.Scope != "task" {
			if label = short(d.Repo); label == "" {
				label = "repo"
			}
		}
		g = append(g, paras(L(col(T(label, tSb), 7)), d.Text, iw, 7)...)
	}
	return g
}

// planImpact is the impact column of a batch and the row of the file under the cursor.
func (st *state) planImpact(r route, p *views.Plan, b views.PlanBatch, short func(string) string, w int, approval bool) ([]line, int) {
	iw := w - 6
	files := planFiles(p, b.N)
	sel := st.planFile(r, files)
	batchOf := map[string]int{}
	for _, f := range p.Impact.Files {
		batchOf[path.Base(f.Path)] = f.Batch
	}
	out := block([]line{lr(L(B(fmt.Sprintf("Impacto · lote %d", b.N), "")), L(T(sourceLabel(p.Impact.Source), tSb)), iw), nil}, w, tBg1, 3)
	selRow := 0
	if len(files) == 0 {
		out = append(out, block([]line{L(T("este lote no toca archivos del mapa", tMu))}, w, tBg1, 3)...)
	}
	for i, f := range files {
		kind := "edita"
		if f.Kind == "new" {
			kind = "nuevo"
		}
		name := T(path.Base(f.Path), "")
		if i == sel {
			name = B(path.Base(f.Path), "")
		}
		g := []line{L(S(fmt.Sprintf("%-7s ", " "+kind), "ch-mute"), " ", name)}
		sub := func(parts ...any) { g = append(g, L(spaces(9, ""), L(parts...))) }
		for _, c := range f.Lines {
			sub(T(fmt.Sprintf(":%d  ", c.Line), tSb), T(c.What, tMu))
		}
		for _, imp := range f.ImportedBy {
			l := L(T("← ", tSb), T(imp, tMu))
			if n := batchOf[imp]; n != 0 && n != b.N {
				l = append(l, T(fmt.Sprintf("  lote %d", n), tSb))
			}
			sub(l)
		}
		for _, t := range f.Tests {
			sub(st.testGlyph(t.Status), " ", T(path.Base(t.Path), tMu))
		}
		if c := f.Contract; c != nil {
			repo := short(c.Repo)
			if repo == "" {
				repo = c.Repo
			}
			g = append(g, L(" ", T("↔ ", tAc), B(c.Name, ""), T(fmt.Sprintf("  del %s, lote %d ", repo, c.Batch), tMu), st.contractGlyph(c.Status)))
		}
		bg := tBg1
		if i == sel {
			bg = tSel
			selRow = len(out)
		}
		out = append(out, block(g, w, bg, 3)...)
	}
	legend := L(T("← ", tSb), T("importa   ", tMu), T("○ ", tSb), T("test que lo cubre", tMu))
	if !approval {
		legend = L(T("← ", tSb), T("importa   ", tMu), T("✗ ", tFail), T("test   ", tMu), T("↔ ", tAc), T("contrato", tMu))
	}
	out = append(out, block([]line{nil, legend}, w, tBg1, 3)...)
	return out, selRow
}

func (st *state) contractGlyph(s views.StepStatus) seg {
	switch s {
	case views.StepGreen, views.StepAccepted:
		return T(st.ic.Done, tDone)
	case views.StepFailed:
		return T(st.ic.Failed, tFail)
	}
	return T(st.ic.Pending, tSb)
}

// actionToast says what a button would run.
func actionToast(verb string, a views.Action) string {
	s := verb + " · $ " + strings.Join(a.Argv, " ")
	if a.Planned {
		s += " · el comando todavía no existe"
	}
	return s
}

func planAction(p *views.Plan, id string) (views.Action, bool) {
	for _, a := range p.Actions {
		if a.ID == id {
			return a, true
		}
	}
	return views.Action{}, false
}

func (planScreen) key(st *state, r route, k string) intent {
	if k == "r" {
		st.cmd = tea.Batch(st.cmd, st.topCmd(true))
		return intent{toast: "Recalculando el plan…"}
	}
	p := st.data.Plans[r.key]
	if p == nil || len(p.Batches) == 0 {
		return intent{}
	}
	bi := st.planBatch(r, p)
	files := planFiles(p, p.Batches[bi].N)
	fi := st.planFile(r, files)
	approval := p.State == views.PlanApproval
	moveBatch := func(d int) {
		st.cur[r.ckey()+"#b"] = min(max(bi+d, 0), len(p.Batches)-1)
		st.cur[r.ckey()+"#f"] = 0
	}
	switch k {
	case "left", "h":
		moveBatch(-1)
	case "right", "l":
		moveBatch(1)
	case "up", "k":
		st.cur[r.ckey()+"#f"] = max(fi-1, 0)
	case "down", "j":
		st.cur[r.ckey()+"#f"] = min(fi+1, max(len(files)-1, 0))
	case "enter":
		if len(files) == 0 {
			return intent{}
		}
		fk := files[fi].Repo + "/" + files[fi].Path
		return intent{push: &route{screen: "archivo", key: fk, feature: r.key}}
	case "a":
		if !approval {
			return intent{push: &route{screen: "agentes", key: r.key}}
		}
		if a, ok := planAction(p, "approve"); ok {
			if len(a.Argv) == 0 {
				return intent{toast: terminalText}
			}
			return intent{toast: st.runAction(actionReq{label: "aprobar", a: a, reloadOnErr: true})}
		}
	case "c":
		if a, ok := planAction(p, "change"); approval && ok {
			if len(a.Argv) == 0 {
				return intent{toast: terminalText}
			}
			return intent{toast: st.runAction(actionReq{label: "cambiar", a: a, reloadOnErr: true})}
		}
	case "x":
		if a, ok := planAction(p, "stop"); approval && ok {
			return intent{toast: st.runAction(actionReq{label: "parar", a: a, reloadOnErr: true,
				after: func() { st.toast = "feature parada" }})}
		}
	case "d":
		if approval {
			return intent{}
		}
		if indexOfDecision(st.pending(), r.key) < 0 {
			return intent{toast: "No hay una decisión pendiente en " + r.key + "."}
		}
		return intent{decide: true, decideFor: r.key}
	}
	return intent{}
}
