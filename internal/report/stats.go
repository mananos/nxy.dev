package report

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/internal/format"
	"github.com/mananos/nxy.dev/internal/host/claudecode/transcript"
	"github.com/mananos/nxy.dev/internal/jscompat"
	"github.com/mananos/nxy.dev/internal/metrics"
)

// Output is what a command prints: stdout and stderr texts (with their final newline) and the exit code.
type Output struct {
	Stdout string
	Stderr string
	Code   int
}

// StatsData is everything `nxy stats` needs once the session is parsed.
type StatsData struct {
	Session  transcript.Session
	Settings Settings
	JSON     bool
	Loc      *time.Location
	Filter   FilterSummary
	// RtkSavings is the session's share of rtk's ledger; nil when the ledger was not read or could not be.
	RtkSavings *Savings
	// LedgerWhy ("no-db" or "") and LedgerPath explain a nil RtkSavings when rtk rows exist.
	LedgerWhy  string
	LedgerPath string
	Broad      BroadSummary
	// CacheTTL is the TTL the main thread last wrote with; nil when unknown.
	CacheTTL *string
}

func id8(s transcript.Session) string { return jscompat.UTF16Prefix(s.SessionID, 8) }

// StatsNoCalls is the text printed when a session has no API call yet (and --json was not asked).
func StatsNoCalls(s transcript.Session) Output {
	plural := "s"
	if s.Turns == 1 {
		plural = ""
	}
	return Output{Stdout: fmt.Sprintf("nxy stats — session %s: no API calls recorded yet (%d prompt%s sent). Numbers appear from the next turn on.\n", id8(s), s.Turns, plural)}
}

type shape struct {
	CallsPerTurn     *float64 `json:"callsPerTurn"`
	MainCallsPerTurn *float64 `json:"mainCallsPerTurn"`
	MainCtxAvg       *float64 `json:"mainCtxAvg"`
	SubagentSharePct *float64 `json:"subagentSharePct"`
}

func ptr(v float64) *float64 { return &v }

// NaNOr is a nullable number as the formatters take it: nil (JS null/undefined) becomes NaN.
func NaNOr(p *float64) float64 {
	if p == nil {
		return math.NaN()
	}
	return *p
}

func computeShape(s transcript.Session) shape {
	var sh shape
	if s.Turns != 0 {
		sh.CallsPerTurn = ptr(float64(s.Usage.Calls) / float64(s.Turns))
		sh.MainCallsPerTurn = ptr(float64(s.Main.Calls) / float64(s.Turns))
	}
	if s.Main.Calls != 0 {
		sh.MainCtxAvg = ptr(s.Main.TotalInput / float64(s.Main.Calls))
	}
	if den := s.Usage.TotalInput + s.Usage.Usage.Output; den != 0 {
		sh.SubagentSharePct = ptr(((s.Subagents.TotalInput + s.Subagents.Usage.Output) / den) * 100)
	}
	return sh
}

type statsJSON struct {
	transcript.Session
	Shape         shape         `json:"shape"`
	BroadSearches BroadSummary  `json:"broadSearches"`
	Filter        FilterSummary `json:"filter"`
	RtkSavings    *Savings      `json:"rtkSavings"`
	CacheTTL      *string       `json:"cacheTtl"`
}

func tsFloat(p *int64) float64 {
	if p == nil {
		return math.NaN()
	}
	return float64(*p)
}

// text is String(v ?? ”) of a decoded JSON value.
func text(v any) string {
	if v == nil || jscompat.IsUndefined(v) {
		return ""
	}
	return jscompat.String(v)
}

func tokens(v float64) string { return format.FmtTokens(v) }

func count(n int) string { return strconv.Itoa(n) }

func bucketUsd(b metrics.Finished) string {
	return format.FmtUsdPartial(b.USD, b.USDPartial, b.USDEstimated)
}

type bucketRow struct {
	name   string
	b      metrics.Finished
	agents int
}

func bucketCells(r bucketRow) []string {
	b := r.b
	return []string{
		count(b.Calls), tokens(b.Usage.Input), tokens(b.Usage.CacheWrite5m + b.Usage.CacheWrite1h), tokens(b.Usage.CacheRead), tokens(b.Usage.Output),
		format.FmtPct(NaNOr(b.CacheHitPct), 0), bucketUsd(b),
	}
}

func bucketCols(usdLabel string) []format.Col {
	return []format.Col{
		{Label: "calls", Right: true}, {Label: "input", Right: true}, {Label: "cache-w", Right: true}, {Label: "cache-r", Right: true},
		{Label: "output", Right: true}, {Label: "hit", Right: true}, {Label: usdLabel, Right: true},
	}
}

func sortByUsd(rows []bucketRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if d := b.b.USD - a.b.USD; d != 0 {
			return d < 0
		}
		return float64(b.b.Calls-a.b.Calls) < 0
	})
}

// bucketTable prints rows under the first columns firstCols (their cells come from lead).
func bucketTable(rows []bucketRow, firstCols []format.Col, lead func(bucketRow) []string, usdLabel string) string {
	cols := append(append([]format.Col{}, firstCols...), bucketCols(usdLabel)...)
	cells := make([][]string, len(rows))
	for i, r := range rows {
		cells[i] = append(lead(r), bucketCells(r)...)
	}
	return format.Table(cols, cells)
}

// Stats is the output of `nxy stats` for a parsed session (entries/stats.mjs from the shape on).
func Stats(d StatsData) Output {
	s := d.Session
	sh := computeShape(s)
	if d.JSON {
		raw, err := json.Marshal(statsJSON{Session: s, Shape: sh, BroadSearches: d.Broad, Filter: d.Filter, RtkSavings: d.RtkSavings, CacheTTL: d.CacheTTL})
		if err == nil {
			raw, err = jscompat.Restringify(raw, "  ")
		}
		if err != nil {
			return Output{Stderr: "nxy stats: " + err.Error() + "\n", Code: 1}
		}
		return Output{Stdout: string(raw) + "\n"}
	}

	usdLabel := "USD"
	if d.Settings.Subscription {
		usdLabel = "USD-equiv"
	}
	var out []string
	head := "nxy stats — session " + id8(s) + " · " + s.Project
	if jscompat.Truthy(s.GitBranch) {
		head += " · " + jscompat.String(s.GitBranch)
	}
	if jscompat.Truthy(s.Version) {
		head += " · Claude Code " + jscompat.String(s.Version)
	}
	out = append(out, head)
	out = append(out, "started "+format.FmtDate(tsFloat(s.FirstTs), d.Loc, true)+" · wall "+format.FmtDuration(float64(s.WallMs))+" · active "+format.FmtDuration(float64(s.ActiveMs))+
		" · turns "+count(s.Turns)+" · API calls "+count(s.Usage.Calls)+" (main "+count(s.Main.Calls)+", subagents "+count(s.Subagents.Calls)+" in "+count(len(s.Agents))+" agents)")
	out = append(out, "shape: "+format.FmtRatio(NaNOr(sh.CallsPerTurn))+" calls/turn (main "+format.FmtRatio(NaNOr(sh.MainCallsPerTurn))+") · main context avg "+tokens(NaNOr(sh.MainCtxAvg))+
		"/call · peak "+tokens(s.Main.ContextPeak)+" · subagents "+format.FmtPct(NaNOr(sh.SubagentSharePct), 0)+" of tokens")
	out = append(out, "")

	u, m, a := s.Usage.Usage, s.Main.Usage, s.Subagents.Usage
	tok := func(label string, t, mm, aa float64) []string {
		return []string{label, tokens(t), tokens(mm), tokens(aa)}
	}
	out = append(out, format.Table(
		[]format.Col{{Label: "TOKENS"}, {Label: "total", Right: true}, {Label: "main", Right: true}, {Label: "subagents", Right: true}},
		[][]string{
			tok("input (uncached)", u.Input, m.Input, a.Input),
			tok("cache write 5m", u.CacheWrite5m, m.CacheWrite5m, a.CacheWrite5m),
			tok("cache write 1h", u.CacheWrite1h, m.CacheWrite1h, a.CacheWrite1h),
			tok("cache read", u.CacheRead, m.CacheRead, a.CacheRead),
			tok("output", u.Output, m.Output, a.Output),
			tok("  of which thinking", u.Thinking, m.Thinking, a.Thinking),
			{"cache hit", format.FmtPct(NaNOr(s.Usage.CacheHitPct), 0), format.FmtPct(NaNOr(s.Main.CacheHitPct), 0), format.FmtPct(NaNOr(s.Subagents.CacheHitPct), 0)},
			tok("context peak", s.Usage.ContextPeak, s.Main.ContextPeak, s.Subagents.ContextPeak),
			{usdLabel, bucketUsd(s.Usage), bucketUsd(s.Main), bucketUsd(s.Subagents)},
		},
	))
	if len(s.UnknownModels) > 0 {
		out = append(out, "(no pricing for: "+strings.Join(s.UnknownModels, ", ")+" — "+usdLabel+" shown as $…+? where affected)")
	}
	if s.Usage.USDEstimated {
		out = append(out, format.EstimatedNote(s.Usage.EstimatedModels))
	}
	out = append(out, "")

	nameLead := func(r bucketRow) []string { return []string{r.name} }
	out = append(out, "BY MODEL")
	var rows []bucketRow
	for _, k := range s.ByModel.Keys {
		rows = append(rows, bucketRow{name: k, b: s.ByModel.Values[k]})
	}
	sortByUsd(rows)
	out = append(out, bucketTable(rows, []format.Col{{Label: "model"}}, nameLead, usdLabel))
	out = append(out, "")

	if len(s.ByAgentType.Keys) > 0 {
		out = append(out, "BY AGENT TYPE")
		rows = nil
		for _, k := range s.ByAgentType.Keys {
			t := s.ByAgentType.Values[k]
			rows = append(rows, bucketRow{name: k, b: t.Finished, agents: t.Agents})
		}
		sortByUsd(rows)
		out = append(out, bucketTable(rows, []format.Col{{Label: "type"}, {Label: "agents", Right: true}}, func(r bucketRow) []string { return []string{r.name, count(r.agents)} }, usdLabel))
		out = append(out, "")
		out = append(out, "AGENTS")
		n := len(s.Agents)
		if n > 20 {
			n = 20
		}
		cells := make([][]string, 0, n)
		for _, ag := range s.Agents[:n] {
			model := "?"
			if jscompat.Truthy(ag.Model) {
				model = jscompat.String(ag.Model)
			}
			dur := math.NaN()
			if ag.DurationMs != nil {
				dur = float64(*ag.DurationMs)
			}
			cells = append(cells, []string{
				ag.AgentType, format.Clip(text(ag.Description), 40), model,
				tokens(ag.TotalInput + ag.Usage.Output), format.FmtUsdPartial(ag.USD, ag.USDPartial, ag.USDEstimated), format.FmtDuration(dur),
			})
		}
		out = append(out, format.Table([]format.Col{{Label: "type"}, {Label: "description"}, {Label: "model"}, {Label: "tokens", Right: true}, {Label: usdLabel, Right: true}, {Label: "time", Right: true}}, cells))
		out = append(out, "")
		out = append(out, "SUBAGENT TIME")
		out = append(out, FormatAgentTiming(s.AgentTiming)...)
		out = append(out, "")
	}

	if len(s.BySkill.Keys) > 0 {
		out = append(out, "BY SKILL / COMMAND")
		rows = nil
		for _, k := range s.BySkill.Keys {
			rows = append(rows, bucketRow{name: k, b: s.BySkill.Values[k]})
		}
		sortByUsd(rows)
		out = append(out, bucketTable(rows, []format.Col{{Label: "skill"}}, nameLead, usdLabel))
		out = append(out, "")
	}

	type toolCount struct {
		name string
		n    int
	}
	var tools []toolCount
	for _, k := range s.Tools.Calls.Keys {
		tools = append(tools, toolCount{k, s.Tools.Calls.Values[k]})
	}
	sort.SliceStable(tools, func(i, j int) bool { return tools[i].n > tools[j].n })
	parts := make([]string, len(tools))
	for i, t := range tools {
		parts[i] = t.name + " " + count(t.n)
	}
	toolList := strings.Join(parts, " · ")
	if toolList == "" {
		toolList = "none"
	}
	out = append(out, "TOOLS: "+toolList)
	b := s.Tools.Bash
	interrupted := ""
	if b.Interrupted != 0 {
		interrupted = " · " + count(b.Interrupted) + " interrupted"
	}
	out = append(out, "  files read (distinct): "+count(len(s.Tools.FilesRead))+" · files edited: "+count(len(s.Tools.FilesEdited))+" (by main "+count(len(s.Tools.FilesEditedMain))+") · Bash calls "+count(b.Calls)+
		" → "+tokens(float64(b.ResultLines))+" lines / "+tokens(float64(b.ResultChars))+" chars back"+interrupted)
	if d.Broad.Count > 0 {
		var kinds []string
		for _, k := range d.Broad.ByKind.Keys() {
			kinds = append(kinds, k+" "+jscompat.String(d.Broad.ByKind.Get(k)))
		}
		th := math.NaN()
		if d.Broad.Threshold != nil {
			th = *d.Broad.Threshold
		}
		out = append(out, "  broad searches in the main thread (above "+tokens(th)+" context): "+count(d.Broad.Count)+" — "+strings.Join(kinds, " · ")+
			" · context carried at those searches "+tokens(d.Broad.ContextTokens)+" tokens (sum of context size, an upper bound)")
	}
	out = append(out, "")

	f := d.Filter
	out = append(out, "FILTER (nxy hook rows for this session): "+count(f.Rows))
	if f.Rows != 0 {
		var engines []string
		for _, k := range f.ByEngine.Keys() {
			engines = append(engines, k+" "+jscompat.String(f.ByEngine.Get(k)))
		}
		out = append(out, "  engine: "+strings.Join(engines, " · ")+" · Bash output seen by the model: "+tokens(f.FilteredChars)+" chars (~"+tokens(f.EstTokens)+" tokens)")
		if len(f.SkipReasons) > 0 {
			var sr []string
			for _, r := range f.SkipReasons {
				sr = append(sr, r.Reason+" "+jscompat.String(r.Count))
			}
			out = append(out, "  skip reasons: "+strings.Join(sr, " · "))
		}
		if f.ByEngine.Get("rtk") != 0 {
			switch rs := d.RtkSavings; {
			case rs != nil && rs.Commands != 0:
				out = append(out, "  rtk saved: "+tokens(rs.Saved)+" tokens ("+format.FmtPct(SavedPct(*rs), 0)+") — "+jscompat.String(rs.Commands)+" filtered commands printed "+tokens(rs.Input)+", the model read "+tokens(rs.Output)+" (rtk's own ledger)")
			case rs != nil:
				out = append(out, "  rtk saved: no ledger rows for this session window (rtk tracking off, or another cwd)")
			default:
				cause := "rtk ledger not readable (locked?)"
				if d.LedgerWhy == "no-db" {
					cause = "no rtk ledger at " + d.LedgerPath + " (rtk tracking off, RTK_DB_PATH wrong, or rtk installed on another OS)"
				}
				out = append(out, "  rtk saved: n/a — "+cause+"; rtk gain still has the global totals")
			}
		}
		n := len(f.ByKind)
		if n > 12 {
			n = 12
		}
		cells := make([][]string, 0, n)
		for _, k := range f.ByKind[:n] {
			cells = append(cells, []string{text(k.Kind), count(k.Rows), count(k.RTK), tokens(k.Chars), tokens(k.Tokens)})
		}
		out = append(out, format.Table([]format.Col{{Label: "command kind"}, {Label: "calls", Right: true}, {Label: "via rtk", Right: true}, {Label: "chars", Right: true}, {Label: "~tokens", Right: true}}, cells))
	}
	out = append(out, "")

	if len(s.CacheBreaks) > 0 {
		out = append(out, "CACHE BREAKS (> "+tokens(d.Settings.CacheBreakThreshold)+" uncached in one call): "+count(len(s.CacheBreaks)))
		usd := s.Usage.USD
		if math.IsNaN(usd) {
			usd = 0
		}
		for _, l := range FormatBreaks(metrics.SummarizeBreaks(s.CacheBreaks), usd) {
			out = append(out, "  "+l)
		}
		n := len(s.CacheBreaks)
		if n > 5 {
			n = 5
		}
		for _, cb := range s.CacheBreaks[:n] {
			agent := ""
			if cb.AgentType != nil && *cb.AgentType != "" {
				agent = " · agent " + *cb.AgentType
			}
			out = append(out, "  "+format.FmtDate(float64(cb.Ts), d.Loc, true)+" · "+cb.Cause+" · "+tokens(cb.Uncached)+" uncached of "+tokens(cb.Total)+" · ttl "+cb.TTL+agent)
		}
	} else {
		out = append(out, "CACHE BREAKS: none")
	}
	if line, ok := FormatTTLWhatIf(s.TTLWhatIf); ok {
		out = append(out, "  "+line)
	}
	return Output{Stdout: strings.Join(out, "\n") + "\n"}
}
