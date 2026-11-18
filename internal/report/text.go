package report

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/mananos/nxy.dev/v2/internal/format"
	"github.com/mananos/nxy.dev/v2/internal/jscompat"
	"github.com/mananos/nxy.dev/v2/internal/metrics"
)

func usd2(n float64) string { return "$" + jscompat.ToFixed(n, 2) }

// FormatBreaks is formatBreaks of hosts/claude-code/transcripts.mjs: the two lines `stats` and
// `trend` print about cache rebuilds.
func FormatBreaks(s metrics.BreakSummary, totalUsd float64) []string {
	pct := func(n float64) string {
		if totalUsd > 0 {
			return " (" + jscompat.ToFixed(float64(100*n)/totalUsd, 1) + "% del costo)"
		}
		return ""
	}
	var parts []string
	for _, c := range metrics.BreakCauses {
		g := s.ByCause[c]
		if g == nil || g.Count == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %d · %s · %s", c, g.Count, format.FmtTokens(g.Tokens), usd2(g.USD)))
	}
	joined := strings.Join(parts, "  |  ")
	if joined == "" {
		joined = "ninguno"
	}
	return []string{
		"reconstrucciones de cache por causa: " + joined,
		fmt.Sprintf("TTL de cache escrito: 5m ×%d · 1h ×%d  ·  evitable calentando (inactividad + subagente): %s%s", s.TTL.FiveMin, s.TTL.Hour, usd2(s.AvoidableUSD), pct(s.AvoidableUSD)),
	}
}

// FormatTTLWhatIf is formatTtlWhatIf of transcripts.mjs: the one-hour cache verdict for the main
// thread, or false when there is nothing to decide.
func FormatTTLWhatIf(w metrics.TTLWhatIf) (string, bool) {
	if w.Writes5m == 0 || w.ActualUSD <= 0 {
		return "", false
	}
	pct := float64(100*(w.HourUSD-w.ActualUSD)) / w.ActualUSD
	sign := ""
	if pct > 0 {
		sign = "+"
	}
	head := fmt.Sprintf("cache TTL 1h, ¿y si…? (hilo principal): %s → %s (%s%s%%)", usd2(w.ActualUSD), usd2(w.HourUSD), sign, jscompat.ToFixed(pct, 1))
	rebuilds := ""
	if w.RebuildsAvoided != 0 {
		rebuilds = fmt.Sprintf(", %d reconstrucción(es) habrían sido lecturas", w.RebuildsAvoided)
		if w.RebuildsAvoided == 1 {
			rebuilds = ", 1 reconstrucción habría sido una lectura"
		}
	}
	switch {
	case pct <= -1:
		return head + rebuilds + ` · conviene: poné "promptCacheTtl": "1h" en ~/.claude/settings.json (Claude Code ≥ 2.1.242)`, true
	case pct >= 0:
		return head + " · acá no conviene: dejá el cache de 5 minutos", true
	}
	return head + rebuilds + " · da casi igual: no hace falta cambiar", true
}

// FormatAgentTiming is formatAgentTiming of core/metrics/timing.mjs: the SUBAGENT TIME lines.
func FormatAgentTiming(summary *metrics.TimingSummary) []string {
	if summary == nil || len(summary.Types) == 0 {
		return []string{"no hubo corridas de subagentes"}
	}
	pctOf := func(v, tot int64) string {
		if tot > 0 {
			return format.FmtPct(float64(float64(v)/float64(tot))*100, 0)
		}
		return format.FmtPct(math.NaN(), 0)
	}
	cols := []format.Col{
		{Label: "agente"}, {Label: "corridas", Right: true},
		{Label: "mediana", Right: true}, {Label: "p90", Right: true}, {Label: "máx", Right: true},
		{Label: "modelo", Right: true}, {Label: "bash", Right: true}, {Label: "herram.", Right: true}, {Label: "espera", Right: true},
		{Label: "locate", Right: true},
	}
	cells := make([][]string, 0, len(summary.Types))
	for _, t := range summary.Types {
		tot := t.ModelMs + t.BashMs + t.ToolMs + t.WaitMs
		cells = append(cells, []string{
			t.AgentType, strconv.Itoa(t.Runs),
			format.FmtDuration(float64(t.MedianMs)), format.FmtDuration(float64(t.P90Ms)), format.FmtDuration(float64(t.MaxMs)),
			pctOf(t.ModelMs, tot), pctOf(t.BashMs, tot), pctOf(t.ToolMs, tot), pctOf(t.WaitMs, tot),
			fmt.Sprintf("%d/%d", t.WithLocate, t.Runs),
		})
	}
	out := strings.Split(format.Table(cols, cells), "\n")
	if len(summary.Slowest) > 0 {
		out = append(out, "", "comandos más lentos")
		for _, s := range summary.Slowest {
			out = append(out, "  "+jscompat.PadStart(format.FmtDuration(float64(s.Ms)), 6, " ")+"  "+s.AgentType+"  "+format.Clip(jscompat.CollapseSpace(s.Text), 70))
		}
	}
	if summary.LimitStops > 0 {
		out = append(out, "", fmt.Sprintf("cortes por límite de uso excluidos: %d", summary.LimitStops))
	}
	return out
}
