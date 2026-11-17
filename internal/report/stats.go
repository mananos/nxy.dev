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
	return Output{Stdout: fmt.Sprintf("nxy stats — sesión %s: todavía no hay llamadas a la API registradas (%d prompt%s enviado%s). Los números aparecen desde el próximo turno.\n", id8(s), s.Turns, plural, plural)}
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
		{Label: "llamadas", Right: true}, {Label: "entrada", Right: true}, {Label: "cache-w", Right: true}, {Label: "cache-r", Right: true},
		{Label: "salida", Right: true}, {Label: "hit", Right: true}, {Label: usdLabel, Right: true},
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
	head := "nxy stats — sesión " + id8(s) + " · " + s.Project
	if jscompat.Truthy(s.GitBranch) {
		head += " · " + jscompat.String(s.GitBranch)
	}
	if jscompat.Truthy(s.Version) {
		head += " · Claude Code " + jscompat.String(s.Version)
	}
	out = append(out, head)
	out = append(out, "inicio "+format.FmtDate(tsFloat(s.FirstTs), d.Loc, true)+" · reloj "+format.FmtDuration(float64(s.WallMs))+" · activo "+format.FmtDuration(float64(s.ActiveMs))+
		" · turnos "+count(s.Turns)+" · llamadas a la API "+count(s.Usage.Calls)+" (principal "+count(s.Main.Calls)+", subagentes "+count(s.Subagents.Calls)+" en "+count(len(s.Agents))+" agentes)")
	out = append(out, "forma: "+format.FmtRatio(NaNOr(sh.CallsPerTurn))+" llamadas/turno (principal "+format.FmtRatio(NaNOr(sh.MainCallsPerTurn))+") · contexto promedio del principal "+tokens(NaNOr(sh.MainCtxAvg))+
		"/llamada · pico "+tokens(s.Main.ContextPeak)+" · subagentes "+format.FmtPct(NaNOr(sh.SubagentSharePct), 0)+" de los tokens")
	out = append(out, "")

	u, m, a := s.Usage.Usage, s.Main.Usage, s.Subagents.Usage
	tok := func(label string, t, mm, aa float64) []string {
		return []string{label, tokens(t), tokens(mm), tokens(aa)}
	}
	out = append(out, format.Table(
		[]format.Col{{Label: "TOKENS"}, {Label: "total", Right: true}, {Label: "principal", Right: true}, {Label: "subagentes", Right: true}},
		[][]string{
			tok("entrada (sin caché)", u.Input, m.Input, a.Input),
			tok("escritura de caché 5m", u.CacheWrite5m, m.CacheWrite5m, a.CacheWrite5m),
			tok("escritura de caché 1h", u.CacheWrite1h, m.CacheWrite1h, a.CacheWrite1h),
			tok("lectura de caché", u.CacheRead, m.CacheRead, a.CacheRead),
			tok("salida", u.Output, m.Output, a.Output),
			tok("  de la cual razonamiento", u.Thinking, m.Thinking, a.Thinking),
			{"aciertos de caché", format.FmtPct(NaNOr(s.Usage.CacheHitPct), 0), format.FmtPct(NaNOr(s.Main.CacheHitPct), 0), format.FmtPct(NaNOr(s.Subagents.CacheHitPct), 0)},
			tok("pico de contexto", s.Usage.ContextPeak, s.Main.ContextPeak, s.Subagents.ContextPeak),
			{usdLabel, bucketUsd(s.Usage), bucketUsd(s.Main), bucketUsd(s.Subagents)},
		},
	))
	if len(s.UnknownModels) > 0 {
		out = append(out, "(sin precios para: "+strings.Join(s.UnknownModels, ", ")+" — "+usdLabel+" se muestra como $…+? donde afecta)")
	}
	if s.Usage.USDEstimated {
		out = append(out, format.EstimatedNote(s.Usage.EstimatedModels))
	}
	out = append(out, "")

	nameLead := func(r bucketRow) []string { return []string{r.name} }
	out = append(out, "POR MODELO")
	var rows []bucketRow
	for _, k := range s.ByModel.Keys {
		rows = append(rows, bucketRow{name: k, b: s.ByModel.Values[k]})
	}
	sortByUsd(rows)
	out = append(out, bucketTable(rows, []format.Col{{Label: "modelo"}}, nameLead, usdLabel))
	out = append(out, "")

	if len(s.ByAgentType.Keys) > 0 {
		out = append(out, "POR TIPO DE AGENTE")
		rows = nil
		for _, k := range s.ByAgentType.Keys {
			t := s.ByAgentType.Values[k]
			rows = append(rows, bucketRow{name: k, b: t.Finished, agents: t.Agents})
		}
		sortByUsd(rows)
		out = append(out, bucketTable(rows, []format.Col{{Label: "tipo"}, {Label: "agentes", Right: true}}, func(r bucketRow) []string { return []string{r.name, count(r.agents)} }, usdLabel))
		out = append(out, "")
		out = append(out, "AGENTES")
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
		out = append(out, format.Table([]format.Col{{Label: "tipo"}, {Label: "descripción"}, {Label: "modelo"}, {Label: "tokens", Right: true}, {Label: usdLabel, Right: true}, {Label: "tiempo", Right: true}}, cells))
		out = append(out, "")
		out = append(out, "TIEMPO DE SUBAGENTES")
		out = append(out, FormatAgentTiming(s.AgentTiming)...)
		out = append(out, "")
	}

	if len(s.BySkill.Keys) > 0 {
		out = append(out, "POR SKILL / COMANDO")
		rows = nil
		for _, k := range s.BySkill.Keys {
			rows = append(rows, bucketRow{name: k, b: s.BySkill.Values[k]})
		}
		sortByUsd(rows)
		out = append(out, bucketTable(rows, []format.Col{{Label: "skill / comando"}}, nameLead, usdLabel))
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
		toolList = "ninguna"
	}
	out = append(out, "HERRAMIENTAS: "+toolList)
	b := s.Tools.Bash
	interrupted := ""
	if b.Interrupted != 0 {
		interrupted = " · " + count(b.Interrupted) + " interrumpidas"
	}
	out = append(out, "  archivos leídos (distintos): "+count(len(s.Tools.FilesRead))+" · archivos editados: "+count(len(s.Tools.FilesEdited))+" (por el principal "+count(len(s.Tools.FilesEditedMain))+") · llamadas a Bash "+count(b.Calls)+
		" → vuelven "+tokens(float64(b.ResultLines))+" líneas / "+tokens(float64(b.ResultChars))+" caracteres"+interrupted)
	if d.Broad.Count > 0 {
		var kinds []string
		for _, k := range d.Broad.ByKind.Keys() {
			kinds = append(kinds, k+" "+jscompat.String(d.Broad.ByKind.Get(k)))
		}
		th := math.NaN()
		if d.Broad.Threshold != nil {
			th = *d.Broad.Threshold
		}
		out = append(out, "  búsquedas amplias en el hilo principal (con más de "+tokens(th)+" de contexto): "+count(d.Broad.Count)+" — "+strings.Join(kinds, " · ")+
			" · contexto arrastrado en esas búsquedas "+tokens(d.Broad.ContextTokens)+" tokens (suma del tamaño de contexto, una cota superior)")
	}
	out = append(out, "")

	f := d.Filter
	out = append(out, "FILTRO (filas del hook de nxy de esta sesión): "+count(f.Rows))
	if f.Rows != 0 {
		var engines []string
		for _, k := range f.ByEngine.Keys() {
			engines = append(engines, k+" "+jscompat.String(f.ByEngine.Get(k)))
		}
		out = append(out, "  motor: "+strings.Join(engines, " · ")+" · salida de Bash que vio el modelo: "+tokens(f.FilteredChars)+" caracteres (~"+tokens(f.EstTokens)+" tokens)")
		if len(f.SkipReasons) > 0 {
			var sr []string
			for _, r := range f.SkipReasons {
				sr = append(sr, r.Reason+" "+jscompat.String(r.Count))
			}
			out = append(out, "  motivos de salto: "+strings.Join(sr, " · "))
		}
		if f.ByEngine.Get("rtk") != 0 {
			switch rs := d.RtkSavings; {
			case rs != nil && rs.Commands != 0:
				out = append(out, "  rtk ahorró: "+tokens(rs.Saved)+" tokens ("+format.FmtPct(SavedPct(*rs), 0)+") — "+jscompat.String(rs.Commands)+" comandos filtrados imprimían "+tokens(rs.Input)+", el modelo leyó "+tokens(rs.Output)+" (según el registro de rtk)")
			case rs != nil:
				out = append(out, "  rtk ahorró: no hay filas del registro en la ventana de esta sesión (rtk sin tracking, o otra carpeta)")
			default:
				cause := "no se puede leer el registro de rtk (¿bloqueado?)"
				if d.LedgerWhy == "no-db" {
					cause = "no hay registro de rtk en " + d.LedgerPath + " (rtk sin tracking, RTK_DB_PATH mal puesto, o rtk instalado en otro sistema)"
				}
				out = append(out, "  rtk ahorró: n/d — "+cause+"; rtk gain todavía tiene los totales globales")
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
		out = append(out, format.Table([]format.Col{{Label: "tipo de comando"}, {Label: "llamadas", Right: true}, {Label: "vía rtk", Right: true}, {Label: "caracteres", Right: true}, {Label: "~tokens", Right: true}}, cells))
	}
	out = append(out, "")

	if len(s.CacheBreaks) > 0 {
		out = append(out, "QUIEBRES DE CACHE (> "+tokens(d.Settings.CacheBreakThreshold)+" sin cachear en una llamada): "+count(len(s.CacheBreaks)))
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
				agent = " · agente " + *cb.AgentType
			}
			out = append(out, "  "+format.FmtDate(float64(cb.Ts), d.Loc, true)+" · "+cb.Cause+" · "+tokens(cb.Uncached)+" sin cachear de "+tokens(cb.Total)+" · ttl "+cb.TTL+agent)
		}
	} else {
		out = append(out, "QUIEBRES DE CACHE: ninguno")
	}
	if line, ok := FormatTTLWhatIf(s.TTLWhatIf); ok {
		out = append(out, "  "+line)
	}
	return Output{Stdout: strings.Join(out, "\n") + "\n"}
}
