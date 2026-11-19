package tui

import (
	"fmt"
	"math"
	"strings"

	"github.com/mananos/nxy.dev/v2/internal/format"
	"github.com/mananos/nxy.dev/v2/internal/views"
)

type trendScreen struct{}

func (trendScreen) id() string                 { return "tendencia" }
func (trendScreen) section() string            { return "gasto" }
func (trendScreen) crumb(*state, route) string { return "Tendencia" }

// captures keeps shift+tab to go back to Sesión; tab goes on to the next section (Config).
func (trendScreen) captures(_ *state, _ route, k string) bool { return k == "shift+tab" }

func (trendScreen) keys(*state, route) []hint {
	return []hint{{"tab", "config"}, {"shift+tab", "sesión"}, {"enter", "cambiar agrupado"}, {"←→", "período"}}
}

// trendGroups are the groupings of the first block, in the order enter cycles them.
var trendGroups = []string{"día", "semana", "modelo", "agente"}

func (st *state) trendGroup(r route) int { return st.cur[r.ckey()+"#g"] }

func (trendScreen) key(st *state, r route, k string) intent {
	switch k {
	case "shift+tab":
		return intent{section: "gasto"}
	case "enter":
		st.cur[r.ckey()+"#g"] = (st.trendGroup(r) + 1) % len(trendGroups)
	case "left", "right", "h", "l":
		t := st.data.Trend
		if t == nil {
			return intent{}
		}
		return intent{toast: fmt.Sprintf("el demo tiene un solo período (%s a %s)", t.Range.Since, t.Range.Until)}
	}
	return intent{}
}

// trendRow is one bar of the screen.
type trendRow struct {
	label, count, extra string
	usd                 float64
	now                 string // marker at the end ("hoy"), empty for the others
}

const (
	trLabW = 12
	trCntW = 12
	trExtW = 10
	trMarW = 7
	trUsdW = 9
)

// trendBlock is a title with a bar per row; the biggest cost takes the whole bar.
func trendBlock(title string, right line, rows []trendRow, iw int) []line {
	bw := min(44, iw-(trLabW+trCntW+2+trUsdW+trExtW+trMarW))
	bw = max(bw, 4)
	top := 0.0
	for _, r := range rows {
		top = math.Max(top, r.usd)
	}
	out := []line{lr(L(heading(title)), right, iw)}
	for _, r := range rows {
		k := 1
		if top > 0 {
			k = max(1, int(math.Round(float64(bw)*r.usd/top)))
		}
		fg, name, usd := tD2, T(r.label, tMu), B(costText(r.usd, false), "")
		if r.now != "" {
			fg, name, usd = tAc, B(r.label, ""), B(costText(r.usd, false), tAc)
		}
		marker := L(spaces(trMarW, ""))
		if r.now != "" {
			marker = col(T("  "+r.now, tSb), trMarW)
		}
		out = append(out, L(col(name, trLabW), col(T(r.count, tSb), trCntW), T(strings.Repeat("━", k), fg), spaces(bw-k+2, ""),
			rcol(usd, trUsdW), rcol(T(r.extra, tMu), trExtW), marker))
	}
	return out
}

func (trendScreen) view(st *state, r route, w int) ([]line, int) {
	t := st.data.Trend
	if t == nil {
		return st.missing("trend", "No hay tendencia para mostrar.", w), 0
	}
	iw := w - 4 - 6
	g := st.trendGroup(r)

	chips := L(T("por  ", tSb))
	for i, name := range trendGroups {
		if i > 0 {
			chips = append(chips, spaces(1, ""))
		}
		if i == g {
			chips = append(chips, S(" "+name+" ", "ch-mute"))
		} else {
			chips = append(chips, T(" "+name+" ", tSb))
		}
	}
	out := []line{nil, lr(statsNav("tendencia"), L(chips, "  "), w), nil}

	var rows []trendRow
	title := "Últimos 7 días"
	right := L(T("total ", tSb), B(costText(t.Totals.USD, false), ""), T(fmt.Sprintf("  ·  %d sesiones", t.Totals.Sessions), tSb))
	period := func(src []views.PeriodRow) {
		for i, p := range src {
			row := trendRow{label: p.Label, count: fmt.Sprintf("%d ses", p.Sessions), extra: fmt.Sprintf("%.0f %%", p.CacheHitPct), usd: p.USD}
			if i == len(src)-1 {
				row.now = "hoy"
				if g == 1 {
					row.now = "ahora"
				}
			}
			rows = append(rows, row)
		}
	}
	switch g {
	case 0:
		period(t.Days)
	case 1:
		title = "Últimas 8 semanas"
		var sum float64
		var ses int
		for _, p := range t.Weeks {
			sum += p.USD
			ses += p.Sessions
		}
		right = L(T("total ", tSb), B(costText(sum, false), ""), T(fmt.Sprintf("  ·  %d sesiones", ses), tSb))
		period(t.Weeks)
	case 2:
		title = "Por modelo"
		for _, m := range t.Models {
			rows = append(rows, trendRow{label: shortModel(m.Model), count: fmt.Sprintf("%d llamadas", m.Calls), extra: fmt.Sprintf("%.0f %%", m.CacheHitPct), usd: m.USD})
		}
	case 3:
		title, right = "Por agente", agentRest(t)
		rows = agentRows2(t)
	}
	out = append(out, indent(block(trendBlock(title, right, rows, iw), w-4, tBg1, 3), 2)...)

	if g != 3 {
		out = append(out, nil)
		out = append(out, indent(block(trendBlock("Por agente", agentRest(t), agentRows2(t), iw), w-4, tBg1, 3), 2)...)
	}
	return out, 0
}

func agentRows2(t *views.Trend) []trendRow {
	var rows []trendRow
	for _, a := range t.Agents {
		rows = append(rows, trendRow{label: a.Role, count: fmt.Sprintf("%d corridas", a.Runs), extra: "prom " + format.FmtDuration(float64(a.AvgMs)), usd: a.USD})
	}
	return rows
}

// agentRest is the cost of the main thread: what the total has beyond the subagents.
func agentRest(t *views.Trend) line {
	rest := t.Totals.USD
	for _, a := range t.Agents {
		rest -= a.USD
	}
	return L(T("el resto es el hilo principal: ", tSb), B(costText(math.Max(rest, 0), false), ""))
}
