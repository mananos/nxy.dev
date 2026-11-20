package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/views"
)

func init() { register(inicioScreen{}) }

type inicioScreen struct{}

func (inicioScreen) id() string                 { return "inicio" }
func (inicioScreen) section() string            { return "inicio" }
func (inicioScreen) crumb(*state, route) string { return "Inicio" }

func (inicioScreen) keys(*state, route) []hint {
	return []hint{{"↑↓", "mover"}, {"enter", "abrir"}, {"d", "decidir"}, {"tab", "sección"}, {"w", "worktrees"}, {"space", "pausar/reanudar"}}
}

type inicioGroup struct {
	title string
	feats []views.CockpitFeature
}

// inicioGroups sorts the features in the three groups of the screen, empty ones left out.
func inicioGroups(c *views.Cockpit) []inicioGroup {
	g := []inicioGroup{{title: "Te necesita"}, {title: "En curso"}, {title: "Listo hoy"}}
	for _, f := range c.Features {
		switch f.Status {
		case views.StatusNeedsYou:
			g[0].feats = append(g[0].feats, f)
		case views.StatusDone:
			g[2].feats = append(g[2].feats, f)
		default:
			g[1].feats = append(g[1].feats, f)
		}
	}
	var out []inicioGroup
	for _, x := range g {
		if len(x.feats) > 0 {
			out = append(out, x)
		}
	}
	return out
}

// inicioOrder is the features in the order of the screen: the cursor indexes it.
func inicioOrder(c *views.Cockpit) []views.CockpitFeature {
	var out []views.CockpitFeature
	for _, g := range inicioGroups(c) {
		out = append(out, g.feats...)
	}
	return out
}

func (inicioScreen) key(st *state, r route, k string) intent {
	order := inicioOrder(st.data.Cockpit)
	sel := min(st.cursor(r), max(len(order)-1, 0))
	switch k {
	case "up", "k":
		st.setCursor(r, max(sel-1, 0))
	case "down", "j":
		st.setCursor(r, min(sel+1, max(len(order)-1, 0)))
	case "enter":
		if len(order) == 0 {
			return intent{}
		}
		f := order[sel]
		screen := "feature"
		if f.Stage == views.StageDesign {
			screen = "ingeniero"
		}
		return intent{push: &route{screen: screen, key: f.ID}}
	case "d":
		return intent{decide: true}
	case "w":
		return intent{push: &route{screen: "worktrees"}}
	case "space", " ":
		if len(order) > 0 {
			return st.toggleLife(order[sel])
		}
	}
	return intent{}
}

// phaseText is the short phase of a feature: "lote 3/4", "diseño 2/4", "review", "cerrada".
func phaseText(f views.CockpitFeature) string {
	detail := f.StageDetail
	if detail == "" && f.Stage == views.StageBatches && f.Batches > 0 {
		detail = fmt.Sprintf("%d/%d", f.Batch, f.Batches)
	}
	switch f.Stage {
	case views.StageDesign:
		return strings.TrimSpace("diseño " + detail)
	case views.StagePlan:
		return "plan"
	case views.StageBatches:
		return strings.TrimSpace("lote " + detail)
	case views.StageReview:
		return "review"
	case views.StageClose:
		return "cerrada"
	}
	return f.Phase
}

// progressOf is how much of the bar to paint.
func progressOf(f views.CockpitFeature) (n, m int) {
	if _, err := fmt.Sscanf(f.StageDetail, "%d/%d", &n, &m); err == nil && m > 0 {
		return n, m
	}
	if f.Batches > 0 {
		return f.Batch, f.Batches
	}
	return 0, 1
}

func costText(v float64, estimated bool) string {
	s := fmt.Sprintf("$%.2f", v)
	if estimated {
		s += "*"
	}
	return s
}

// ago says "hace 2h" for a time of the demo, against the moment the view was generated.
func ago(now, then string) string {
	a, err1 := time.Parse(time.RFC3339, now)
	b, err2 := time.Parse(time.RFC3339, then)
	if err1 != nil || err2 != nil {
		return ""
	}
	d := a.Sub(b)
	switch {
	case d < time.Minute:
		return "recién"
	case d < time.Hour:
		return fmt.Sprintf("hace %dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("hace %dh", int(d.Hours()))
	}
	return fmt.Sprintf("hace %dd", int(d.Hours()/24))
}

// card is the block of a feature: two lines, or one when it is done.
func card(f views.CockpitFeature, selected bool, w int, now string) []line {
	iw := w - 6
	if f.Status == views.StatusDone {
		when := ago(now, f.ClosedAt)
		l := L(chip(statusLabel(f.Status), statusTok(f.Status), chipW), "   ", B(f.Title, ""), "   ", T(f.ID+" · "+when, tSb))
		return pick([]line{lr(l, rcol(B(costText(f.CostUSD, f.CostEstimated), ""), 6), iw)}, w, tBg1, selected, 3)
	}
	st := statusTok(f.Status)
	right1 := L(T(durText(f.ElapsedMs), tMu), "   ", rcol(B(costText(f.CostUSD, f.CostEstimated), ""), 6))
	l1 := lr(L(chip(statusLabel(f.Status), st, chipW), "   ", B(f.Title, ""), "   ", T(f.ID, tSb)), right1, iw)
	n, m := progressOf(f)
	right2 := L(progress(n, m, 12, st), "  ", col(T(phaseText(f), tMu), 10))
	l2 := lr(L(spaces(chipW+3, ""), T(f.LastEvent, tMu)), right2, iw)
	return pick([]line{l1, l2}, w, tBg1, selected, 3)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// worktreeLine counts only what exists: "3 activos · 1 huérfano".
func worktreeLine(c *views.Cockpit) line {
	var active, paused, orphan int
	for _, x := range c.Worktrees {
		switch x.State {
		case "orphan":
			orphan++
		case "paused":
			paused++
		default:
			active++
		}
	}
	if active+paused+orphan == 0 {
		return nil
	}
	l := L("  ", T("worktrees  ", tMu))
	first := true
	add := func(n int, one, many string, fg tok) {
		if n == 0 {
			return
		}
		if !first {
			l = append(l, T(" · ", tSb))
		}
		first = false
		l = append(l, T(plural(n, one, many), fg))
	}
	add(active, "activo", "activos", tFg)
	add(paused, "pausado", "pausados", tFg)
	add(orphan, "huérfano", "huérfanos", tNeed)
	return append(l, spaces(3, ""), T("w ver todos", tSb))
}

func (inicioScreen) view(st *state, r route, w int) ([]line, int) {
	c := st.data.Cockpit
	sel := min(st.cursor(r), max(len(inicioOrder(c))-1, 0))
	var out []line
	focus, n := 0, 0
	for gi, g := range inicioGroups(c) {
		if gi > 0 {
			out = append(out, nil)
		}
		out = append(out, L(" ", headBar(), heading(g.title), T("  "+fmt.Sprint(len(g.feats)), tSb)))
		for i, f := range g.feats {
			if i > 0 {
				out = append(out, nil)
			}
			out = append(out, indent(card(f, n == sel, w-4, c.GeneratedAt), 2)...)
			if n == sel {
				focus = len(out) - 1
			}
			n++
		}
	}
	if len(c.Features) == 0 && (st.data.Errs["cockpit"] != nil || st.data.Loading["cockpit"]) {
		out = append(out, st.missing("cockpit", "", w)[1:]...)
	} else if len(c.Features) == 0 && st.data.Done["cockpit"] {
		out = append(out, L(" ", T("No hay features abiertas. En Claude Code: /nxy:feature <lo que querés hacer> (o /nxy:engineer)", tMu)))
	}
	if wl := worktreeLine(c); wl != nil {
		out = append(out, nil, wl)
	}
	return append([]line{nil}, out...), focus + 1
}
