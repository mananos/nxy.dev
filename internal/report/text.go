package report

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/mananos/nxy.dev/internal/format"
	"github.com/mananos/nxy.dev/internal/jscompat"
	"github.com/mananos/nxy.dev/internal/metrics"
)

func usd2(n float64) string { return "$" + jscompat.ToFixed(n, 2) }

// FormatBreaks is formatBreaks of hosts/claude-code/transcripts.mjs: the two lines `stats` and
// `trend` print about cache rebuilds.
func FormatBreaks(s metrics.BreakSummary, totalUsd float64) []string {
	pct := func(n float64) string {
		if totalUsd > 0 {
			return " (" + jscompat.ToFixed(float64(100*n)/totalUsd, 1) + "% of cost)"
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
		joined = "none"
	}
	return []string{
		"cache rebuilds by cause: " + joined,
		fmt.Sprintf("cache TTL written: 5m ×%d · 1h ×%d  ·  avoidable by warming (idle + subagent): %s%s", s.TTL.FiveMin, s.TTL.Hour, usd2(s.AvoidableUSD), pct(s.AvoidableUSD)),
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
	head := fmt.Sprintf("cache TTL 1h what-if (main thread): %s → %s (%s%s%%)", usd2(w.ActualUSD), usd2(w.HourUSD), sign, jscompat.ToFixed(pct, 1))
	rebuilds := ""
	if w.RebuildsAvoided != 0 {
		plural := "s"
		if w.RebuildsAvoided == 1 {
			plural = ""
		}
		rebuilds = fmt.Sprintf(", %d rebuild%s would have been reads", w.RebuildsAvoided, plural)
	}
	switch {
	case pct <= -1:
		return head + rebuilds + ` · worth it: set "promptCacheTtl": "1h" in ~/.claude/settings.json (Claude Code ≥ 2.1.242)`, true
	case pct >= 0:
		return head + " · not worth it here: keep the 5-minute cache", true
	}
	return head + rebuilds + " · about the same: no need to change", true
}

// FormatAgentTiming is formatAgentTiming of core/metrics/timing.mjs: the SUBAGENT TIME lines.
func FormatAgentTiming(summary *metrics.TimingSummary) []string {
	if summary == nil || len(summary.Types) == 0 {
		return []string{"no subagent runs"}
	}
	pctOf := func(v, tot int64) string {
		if tot > 0 {
			return format.FmtPct(float64(float64(v)/float64(tot))*100, 0)
		}
		return format.FmtPct(math.NaN(), 0)
	}
	cols := []format.Col{
		{Label: "agent"}, {Label: "runs", Right: true},
		{Label: "median", Right: true}, {Label: "p90", Right: true}, {Label: "max", Right: true},
		{Label: "model", Right: true}, {Label: "bash", Right: true}, {Label: "tools", Right: true}, {Label: "waiting", Right: true},
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
		out = append(out, "", "slowest commands")
		for _, s := range summary.Slowest {
			out = append(out, "  "+jscompat.PadStart(format.FmtDuration(float64(s.Ms)), 6, " ")+"  "+s.AgentType+"  "+format.Clip(jscompat.CollapseSpace(s.Text), 70))
		}
	}
	if summary.LimitStops > 0 {
		out = append(out, "", fmt.Sprintf("usage-limit stops excluded: %d", summary.LimitStops))
	}
	return out
}
