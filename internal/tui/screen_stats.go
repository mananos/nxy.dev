package tui

import (
	"fmt"
	"strings"

	"github.com/mananos/nxy.dev/v2/internal/format"
	"github.com/mananos/nxy.dev/v2/internal/views"
)

func init() {
	register(statsScreen{})
	register(trendScreen{})
}

// statsNav is the sub-navigation of Gasto: Sesión and Tendencia.
func statsNav(active string) line {
	l := L("  ")
	for i, x := range []struct{ id, name string }{{"gasto", "Sesión"}, {"tendencia", "Tendencia"}} {
		if i > 0 {
			l = append(l, spaces(1, ""))
		}
		if x.id == active {
			t := S(" "+x.name+" ", "ch-ac")
			t.Mark = true
			l = append(l, t)
		} else {
			l = append(l, T(" "+x.name+" ", tMu))
		}
	}
	return l
}

type statsScreen struct{}

func (statsScreen) id() string                 { return "gasto" }
func (statsScreen) section() string            { return "gasto" }
func (statsScreen) crumb(*state, route) string { return "Gasto" }

// captures takes tab to go on to Tendencia; shift+tab goes to the previous section (Memoria).
func (statsScreen) captures(_ *state, _ route, k string) bool { return k == "tab" }

func (statsScreen) keys(*state, route) []hint {
	return []hint{{"tab", "tendencia"}, {"s", "otra sesión"}}
}

func (statsScreen) key(st *state, _ route, k string) intent {
	switch k {
	case "tab":
		return intent{section: "tendencia"}
	case "s":
		s := st.data.Stats
		if s == nil || len(s.Sessions) == 0 {
			return intent{toast: "No hay otras sesiones en el demo."}
		}
		var p []string
		for _, x := range s.Sessions {
			p = append(p, fmt.Sprintf("%s %s", x.Label, costText(x.USD, false)))
		}
		return intent{toast: "sesiones · " + strings.Join(p, " · ") + " · el demo muestra una sola"}
	}
	return intent{}
}

// topModelShare is the name and share of the model that costs the most.
func topModelShare(s *views.Stats) (string, float64) {
	var best *views.ModelRow
	for i := range s.ByModel {
		if best == nil || s.ByModel[i].USD > best.USD {
			best = &s.ByModel[i]
		}
	}
	if best == nil || s.USD <= 0 {
		return "", 0
	}
	name, _, _ := strings.Cut(shortModel(best.Model), "-")
	return name, 100 * best.USD / s.USD
}

func tile(label, value, sub string, valueTok tok, w int) []line {
	return block([]line{L(T(label, tSb)), L(B(value, valueTok)), L(T(sub, tMu))}, w, tBg1, 2)
}

func (statsScreen) view(st *state, _ route, w int) ([]line, int) {
	s := st.data.Stats
	if s == nil {
		return st.missing("stats", "No hay una sesión para mostrar.", w), 0
	}
	iw := w - 4 - 6
	out := []line{nil, lr(statsNav("gasto"), L(T(path0(s.Project), tSb), "  "), w), nil}

	share := ""
	if name, pct := topModelShare(s); name != "" {
		share = fmt.Sprintf("%s %.0f %%", name, pct)
	}
	mk := func(tw int) [][]line {
		return [][]line{
			tile("Gasto", costText(s.USD, s.USDEstimated), share, tAc, tw),
			tile("Cache", fmt.Sprintf("%.0f %%", s.CacheHitPct), "leído de cache", "", tw),
			tile("Llamadas", fmt.Sprint(s.Calls), fmt.Sprintf("%.1f por turno", s.Shape.CallsPerTurn), "", tw),
			tile("Turnos", fmt.Sprint(s.Turns), "en esta sesión", "", tw),
		}
	}
	if w >= 100 {
		tw := (w - 4 - 6) / 4
		out = append(out, indent(hcat(mk(tw), []int{tw, tw, tw, tw}, 2), 2)...)
	} else {
		tw := (w - 4 - 2) / 2
		t := mk(tw)
		out = append(out, indent(hcat(t[:2], []int{tw, tw}, 2), 2)...)
		out = append(out, nil)
		out = append(out, indent(hcat(t[2:], []int{tw, tw}, 2), 2)...)
	}
	out = append(out, nil)

	// by model
	mw := min(iw-46, 30)
	head := L(col(T("Modelo", tSb), mw), rcol(T("Llamadas", tSb), 10), rcol(T("Entrada", tSb), 10), rcol(T("Salida", tSb), 9), rcol(T("Cache", tSb), 8), rcol(T("$", tSb), 9))
	rows := []line{head}
	for _, m := range s.ByModel {
		rows = append(rows, L(col(T(shortModel(m.Model), ""), mw), rcol(T(fmt.Sprint(m.Calls), tMu), 10),
			rcol(T(format.FmtTokens(m.Usage.Input), tMu), 10), rcol(T(format.FmtTokens(m.Usage.Output), tMu), 9),
			rcol(T(fmt.Sprintf("%.0f %%", m.CacheHitPct), tMu), 8), rcol(B(costText(m.USD, m.Estimated), ""), 9)))
	}
	out = append(out, indent(block(rows, w-4, tBg1, 3), 2)...)
	out = append(out, nil)

	// shape of the session
	kv := func(k, v string) line { return L(T(k+"  ", tMu), B(v, ""), "     ") }
	forma := []line{L(B("Forma de la sesión", "")), L(
		kv("por turno", fmt.Sprintf("%.1f llamadas", s.Shape.CallsPerTurn)),
		kv("del hilo", fmt.Sprintf("%.1f", s.Shape.MainCallsPerTurn)),
		kv("contexto", format.FmtTokens(s.Shape.MainCtxAvg)),
		kv("subagentes", fmt.Sprintf("%.0f %%", s.Shape.SubagentSharePct)))}
	out = append(out, indent(block(forma, w-4, tBg1, 3), 2)...)
	out = append(out, nil)

	// filter and broad searches
	filtro := []line{L(B("Filtro y búsquedas", ""))}
	if s.Filter.Rows > 0 {
		engine := "el filtro"
		if len(s.Filter.ByEngine) == 1 {
			engine = s.Filter.ByEngine[0].Name
		}
		filtro = append(filtro, L(T(engine+" filtró ", tMu), B(fmt.Sprint(s.Filter.Rows), ""), T(" salidas  ·  ~", tMu),
			B(format.FmtTokens(s.Filter.EstTokens), ""), T(" tokens que no entraron al contexto", tMu)))
	} else {
		filtro = append(filtro, L(T("El filtro todavía no sacó nada de esta sesión.", tMu)))
	}
	if s.Broad.Count > 0 {
		filtro = append(filtro, L(T("búsquedas amplias frenadas ", tMu), B(fmt.Sprint(s.Broad.Count), ""),
			T("  ·  con más de "+format.FmtTokens(float64(s.Broad.Threshold))+" de contexto", tMu)))
	} else {
		filtro = append(filtro, L(T("ninguna búsqueda amplia frenada", tMu)))
	}
	out = append(out, indent(block(filtro, w-4, tBg1, 3), 2)...)
	return out, 0
}

// path0 is the last folder of a path (the name of the checkout the session ran in).
func path0(p string) string {
	p = strings.TrimRight(strings.ReplaceAll(p, "\\", "/"), "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
