package report

import (
	"encoding/json"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/format"
	"github.com/mananos/nxy.dev/v2/internal/host/claudecode/transcript"
	"github.com/mananos/nxy.dev/v2/internal/jscompat"
	"github.com/mananos/nxy.dev/v2/internal/metrics"
)

// TrendSession is one parsed session as `nxy trend` sees it (the `sessions` of the JSON, in the
// 1.x key order).
type TrendSession struct {
	SessionID       string                                  `json:"sessionId"`
	Project         string                                  `json:"project"`
	Ts              float64                                 `json:"ts"`
	Turns           int                                     `json:"turns"`
	Calls           int                                     `json:"calls"`
	MainCalls       int                                     `json:"mainCalls"`
	MainInput       float64                                 `json:"mainInput"`
	MainEdits       int                                     `json:"mainEdits"`
	RtkSaved        *float64                                `json:"rtkSaved"`
	Input           float64                                 `json:"input"`
	CacheW          float64                                 `json:"cacheW"`
	CacheR          float64                                 `json:"cacheR"`
	Output          float64                                 `json:"output"`
	TotalTokens     float64                                 `json:"totalTokens"`
	USD             float64                                 `json:"usd"`
	USDPartial      bool                                    `json:"usdPartial"`
	UnknownModels   []string                                `json:"unknownModels"`
	USDEstimated    bool                                    `json:"usdEstimated"`
	EstimatedModels metrics.ModelBasis                      `json:"estimatedModels"`
	AgentTokens     float64                                 `json:"agentTokens"`
	Agents          int                                     `json:"agents"`
	BashLines       int                                     `json:"bashLines"`
	BashCalls       int                                     `json:"bashCalls"`
	RtkCalls        int                                     `json:"rtkCalls"`
	WallMs          int64                                   `json:"wallMs"`
	Nxy             bool                                    `json:"nxy"`
	FirstPrompt     string                                  `json:"firstPrompt"`
	ByModel         transcript.OrderedMap[metrics.Finished] `json:"byModel"`
	CacheBreaks     []metrics.Break                         `json:"cacheBreaks"`
	TTLWhatIf       metrics.TTLWhatIf                       `json:"ttlWhatIf"`
}

// TrendData is everything `nxy trend` needs once the sessions are parsed.
type TrendData struct {
	// Since is the lower bound in epoch ms; nil when --since did not parse (JS null).
	Since    *int64
	By       string
	Here     bool
	Cwd      string
	JSON     bool
	Settings Settings
	Loc      *time.Location
	Sessions []TrendSession
	// By agent: the timing of each subagent run and how many sessions had calls.
	AgentRuns     []metrics.Run
	AgentSessions int
	// LedgerOK says rtk's ledger was read (the rtkSaved of each session is then a number).
	LedgerOK bool
	// HasGain says `rtk gain` answered (only asked when the ledger was not read); GainDaily is its days.
	HasGain   bool
	GainDaily []Day
}

func sinceFloat(p *int64) float64 {
	if p == nil {
		return math.NaN()
	}
	return float64(*p)
}

func scope(d TrendData) string {
	if d.Here {
		return d.Cwd
	}
	return "todos los proyectos"
}

// nxyCell is the text-table reading of NxyLabel; the JSON keeps "yes".
func nxyCell(label string) string {
	if label == "yes" {
		return "sí"
	}
	return label
}

// byLabel is how a --by value reads in the text output; the JSON keeps the value as typed.
func byLabel(by string) string {
	switch by {
	case "day":
		return "día"
	case "week":
		return "semana"
	case "session":
		return "sesión"
	case "model":
		return "modelo"
	case "agent":
		return "agente"
	}
	return by
}

func jsonOut(v any) Output {
	raw, err := json.Marshal(v)
	if err == nil {
		raw, err = jscompat.Restringify(raw, "  ")
	}
	if err != nil {
		return Output{Stderr: "nxy trend: " + err.Error() + "\n", Code: 1}
	}
	return Output{Stdout: string(raw) + "\n"}
}

// usdSum is sumUsd of trend.mjs: the known USD plus the notices that something had no price.
type usdSum struct {
	USD             float64            `json:"usd"`
	USDPartial      bool               `json:"usdPartial"`
	UnknownModels   []string           `json:"unknownModels"`
	USDEstimated    bool               `json:"usdEstimated"`
	EstimatedModels metrics.ModelBasis `json:"estimatedModels"`
}

type usdPart struct {
	usd       float64
	partial   bool
	unknown   []string
	estimated bool
	models    metrics.ModelBasis
}

func sumUsd(rows []usdPart) usdSum {
	var out usdSum
	seen := map[string]bool{}
	out.UnknownModels = []string{}
	for _, r := range rows {
		for _, m := range r.unknown {
			if !seen[m] {
				seen[m] = true
				out.UnknownModels = append(out.UnknownModels, m)
			}
		}
		for _, k := range r.models.Keys() {
			v, _ := r.models.Get(k)
			out.EstimatedModels.Set(k, v)
		}
		if r.usd != 0 && !math.IsNaN(r.usd) {
			out.USD += r.usd
		}
		if r.partial {
			out.USDPartial = true
		}
		if r.estimated {
			out.USDEstimated = true
		}
	}
	return out
}

func unknownNote(t usdSum) string {
	s := ""
	if t.USDPartial {
		s += "\n(sin precios para: " + strings.Join(t.UnknownModels, ", ") + " — USD se muestra como $…+? donde afecta)"
	}
	if t.USDEstimated {
		s += "\n" + format.EstimatedNote(t.EstimatedModels)
	}
	return s
}

func hitPct(input, cacheW, cacheR float64) *float64 {
	if den := input + cacheW + cacheR; den != 0 {
		return ptr((cacheR / den) * 100)
	}
	return nil
}

func ratio(num, den float64) *float64 {
	if den != 0 {
		return ptr(num / den)
	}
	return nil
}

type modelRow struct {
	Model           string             `json:"model"`
	Sessions        int                `json:"sessions"`
	Calls           int                `json:"calls"`
	Input           float64            `json:"input"`
	CacheW          float64            `json:"cacheW"`
	CacheR          float64            `json:"cacheR"`
	Output          float64            `json:"output"`
	Thinking        float64            `json:"thinking"`
	USD             float64            `json:"usd"`
	USDPartial      bool               `json:"usdPartial"`
	UnknownModels   []string           `json:"unknownModels"`
	USDEstimated    bool               `json:"usdEstimated"`
	EstimatedModels metrics.ModelBasis `json:"estimatedModels"`
	Total           float64            `json:"total"`
	Hit             *float64           `json:"hit"`
}

type periodRow struct {
	Period          string             `json:"period"`
	Ts              float64            `json:"ts"`
	Sessions        int                `json:"sessions"`
	Turns           float64            `json:"turns"`
	Calls           float64            `json:"calls"`
	MainCalls       float64            `json:"mainCalls"`
	MainInput       float64            `json:"mainInput"`
	MainEdits       float64            `json:"mainEdits"`
	Input           float64            `json:"input"`
	CacheW          float64            `json:"cacheW"`
	CacheR          float64            `json:"cacheR"`
	Output          float64            `json:"output"`
	TotalTokens     float64            `json:"totalTokens"`
	USD             float64            `json:"usd"`
	USDPartial      bool               `json:"usdPartial"`
	UnknownModels   []string           `json:"unknownModels"`
	USDEstimated    bool               `json:"usdEstimated"`
	EstimatedModels metrics.ModelBasis `json:"estimatedModels"`
	AgentTokens     float64            `json:"agentTokens"`
	Agents          float64            `json:"agents"`
	BashLines       float64            `json:"bashLines"`
	BashCalls       float64            `json:"bashCalls"`
	RtkCalls        float64            `json:"rtkCalls"`
	RtkSaved        *float64           `json:"rtkSaved"`
	WallMs          float64            `json:"wallMs"`
	Nxy             int                `json:"nxy"`
	Project         string             `json:"project"`
	FirstPrompt     string             `json:"firstPrompt"`
	Hit             *float64           `json:"hit"`
	AgentPct        *float64           `json:"agentPct"`
	PerTurn         *float64           `json:"perTurn"`
	CallsPerTurn    *float64           `json:"callsPerTurn"`
	CtxPerCall      *float64           `json:"ctxPerCall"`
	NxyLabel        string             `json:"nxyLabel"`

	unknownSeen map[string]bool
}

func (r periodRow) part() usdPart {
	return usdPart{usd: r.USD, partial: r.USDPartial, unknown: r.UnknownModels, estimated: r.USDEstimated, models: r.EstimatedModels}
}

func usdCell(usd float64, partial, estimated bool) string {
	return format.FmtUsdPartial(usd, partial, estimated)
}

// PeriodKey is the label of the period a timestamp falls in: its local date, or the Monday of its week.
func PeriodKey(ts float64, by string, loc *time.Location) string {
	if by == "week" {
		if y, mon, d, _, _, wd, ok := jscompat.LocalTime(ts, loc); ok {
			monday := time.Date(y, time.Month(mon+1), d-(wd+6)%7, 12, 0, 0, 0, loc)
			return format.FmtDate(float64(monday.UnixMilli()), loc, false)
		}
	}
	return format.FmtDate(ts, loc, false)
}

// utcDay is the UTC date rtk books its daily totals under.
func utcDay(ts float64) string {
	return time.UnixMilli(int64(ts)).UTC().Format("2006-01-02")
}

// Trend is the output of `nxy trend` for parsed sessions (entries/trend.mjs from the session rows on).
func Trend(d TrendData) Output {
	if d.By == "agent" {
		summary := metrics.SummarizeTiming(d.AgentRuns)
		if d.JSON {
			return jsonOut(struct {
				Since       *int64                `json:"since"`
				By          string                `json:"by"`
				AgentTiming metrics.TimingSummary `json:"agentTiming"`
			}{d.Since, d.By, summary})
		}
		out := []string{
			"nxy trend — " + scope(d) + " · desde " + format.FmtDate(sinceFloat(d.Since), d.Loc, true) + " · por agente · " + count(d.AgentSessions) + " sesiones · " + count(len(d.AgentRuns)) + " corridas",
			"",
		}
		out = append(out, FormatAgentTiming(&summary)...)
		return Output{Stdout: strings.Join(out, "\n") + "\n"}
	}

	sessions := append([]TrendSession{}, d.Sessions...)
	sort.SliceStable(sessions, func(i, j int) bool { return sessions[i].Ts-sessions[j].Ts < 0 })
	for i := range sessions {
		if sessions[i].CacheBreaks == nil {
			sessions[i].CacheBreaks = []metrics.Break{}
		}
		if sessions[i].UnknownModels == nil {
			sessions[i].UnknownModels = []string{}
		}
	}
	usdLabel := "USD"
	if d.Settings.Subscription {
		usdLabel = "USD~"
	}
	head := func(by string) string {
		return "nxy trend — " + scope(d) + " · desde " + format.FmtDate(sinceFloat(d.Since), d.Loc, true) + " · por " + byLabel(by) + " · " + count(len(sessions)) + " sesiones"
	}
	usdCol := func() format.Col { return format.Col{Label: usdLabel, Right: true} }

	if d.By == "model" {
		return trendByModel(d, sessions, head("model"), usdCol())
	}

	var order []string
	groups := map[string]*periodRow{}
	for _, s := range sessions {
		key := s.SessionID
		if d.By != "session" {
			key = PeriodKey(s.Ts, d.By, d.Loc)
		}
		g, ok := groups[key]
		if !ok {
			g = &periodRow{Period: key, Ts: s.Ts, UnknownModels: []string{}, Project: s.Project, FirstPrompt: s.FirstPrompt, unknownSeen: map[string]bool{}}
			if d.LedgerOK {
				g.RtkSaved = ptr(0)
			}
			groups[key] = g
			order = append(order, key)
		}
		g.Sessions++
		g.Turns += float64(s.Turns)
		g.Calls += float64(s.Calls)
		g.MainCalls += float64(s.MainCalls)
		g.MainInput += s.MainInput
		g.MainEdits += float64(s.MainEdits)
		g.Input += s.Input
		g.CacheW += s.CacheW
		g.CacheR += s.CacheR
		g.Output += s.Output
		g.TotalTokens += s.TotalTokens
		g.AgentTokens += s.AgentTokens
		g.Agents += float64(s.Agents)
		g.BashLines += float64(s.BashLines)
		g.BashCalls += float64(s.BashCalls)
		g.RtkCalls += float64(s.RtkCalls)
		g.WallMs += float64(s.WallMs)
		if g.RtkSaved != nil && s.RtkSaved != nil {
			g.RtkSaved = ptr(*g.RtkSaved + *s.RtkSaved)
		}
		g.USD += s.USD
		if s.USDPartial {
			g.USDPartial = true
		}
		if s.USDEstimated {
			g.USDEstimated = true
			for _, k := range s.EstimatedModels.Keys() {
				v, _ := s.EstimatedModels.Get(k)
				g.EstimatedModels.Set(k, v)
			}
		}
		for _, u := range s.UnknownModels {
			if !g.unknownSeen[u] {
				g.unknownSeen[u] = true
				g.UnknownModels = append(g.UnknownModels, u)
			}
		}
		if s.Nxy {
			g.Nxy++
		}
	}
	gain := map[string]float64{}
	for _, day := range d.GainDaily {
		gain[day.Date] = day.Saved
	}
	rows := make([]periodRow, 0, len(order))
	for _, k := range order {
		g := *groups[k]
		g.Hit = hitPct(g.Input, g.CacheW, g.CacheR)
		g.AgentPct = nil
		if g.TotalTokens != 0 {
			g.AgentPct = ptr((g.AgentTokens / g.TotalTokens) * 100)
		}
		g.PerTurn = ratio(g.TotalTokens, g.Turns)
		g.CallsPerTurn = ratio(g.Calls, g.Turns)
		g.CtxPerCall = ratio(g.MainInput, g.MainCalls)
		if g.RtkSaved == nil && d.HasGain {
			g.RtkSaved = ptr(gain[utcDay(g.Ts)])
		}
		if d.By == "session" {
			g.NxyLabel = "no"
			if g.Nxy != 0 {
				g.NxyLabel = "yes"
			}
		} else {
			g.NxyLabel = count(g.Nxy) + "/" + count(g.Sessions)
		}
		rows = append(rows, g)
	}

	parts := make([]usdPart, len(rows))
	for i, r := range rows {
		parts[i] = r.part()
	}
	total := sumUsd(parts)
	var allBreaks []metrics.Break
	ttls := make([]metrics.TTLWhatIf, len(sessions))
	for i, s := range sessions {
		allBreaks = append(allBreaks, s.CacheBreaks...)
		ttls[i] = s.TTLWhatIf
	}
	rebuilds := metrics.SummarizeBreaks(allBreaks)
	ttl := metrics.SumTTLWhatIf(ttls)
	if d.JSON {
		return jsonOut(struct {
			Since    *int64               `json:"since"`
			By       string               `json:"by"`
			Sessions []TrendSession       `json:"sessions"`
			Periods  []periodRow          `json:"periods"`
			Total    usdSum               `json:"total"`
			Rebuilds metrics.BreakSummary `json:"rebuilds"`
			TTL      metrics.TTLWhatIf    `json:"ttlWhatIf"`
		}{d.Since, d.By, sessions, rows, total, rebuilds, ttl})
	}

	out := []string{head(d.By), ""}
	var cols []format.Col
	bySession := d.By == "session"
	if bySession {
		cols = append(cols, format.Col{Label: "sesión"}, format.Col{Label: "inicio"}, format.Col{Label: "primer prompt"})
	} else {
		cols = append(cols, format.Col{Label: byLabel(d.By)}, format.Col{Label: "ses", Right: true})
	}
	for _, l := range []string{"turnos", "tokens", "sin caché", "cache-r", "salida", "hit", "agentes", "tok/turno", "llam/turno", "ctx/llam", "ediciones", "líneas bash", "rtk", "rtk ahorró"} {
		cols = append(cols, format.Col{Label: l, Right: true})
	}
	cols = append(cols, usdCol(), format.Col{Label: "nxy", Right: true})
	cells := make([][]string, len(rows))
	for i, r := range rows {
		var c []string
		if bySession {
			c = append(c, jscompat.UTF16Prefix(r.Period, 8), format.FmtDate(r.Ts, d.Loc, true), format.Clip(jscompat.CollapseSpace(r.FirstPrompt), 28))
		} else {
			c = append(c, r.Period, jscompat.String(float64(r.Sessions)))
		}
		c = append(c,
			jscompat.String(r.Turns), tokens(r.TotalTokens), tokens(r.Input), tokens(r.CacheR), tokens(r.Output),
			format.FmtPct(NaNOr(r.Hit), 0), format.FmtPct(NaNOr(r.AgentPct), 0),
			tokens(NaNOr(r.PerTurn)), format.FmtRatio(NaNOr(r.CallsPerTurn)), tokens(NaNOr(r.CtxPerCall)),
			jscompat.String(r.MainEdits), tokens(r.BashLines), jscompat.String(r.RtkCalls), tokens(NaNOr(r.RtkSaved)),
			usdCell(r.USD, r.USDPartial, r.USDEstimated), nxyCell(r.NxyLabel),
		)
		cells[i] = c
	}
	out = append(out, format.Table(cols, cells), "")

	here := "todos los proyectos"
	if d.Here {
		here = "este proyecto"
	}
	var rtkNote string
	switch {
	case d.LedgerOK:
		rtkNote = "rtk ahorró = tokens que rtk le sacó a la salida de Bash, según su propio registro"
	case d.HasGain:
		rtkNote = "rtk ahorró = total diario de rtk (" + here + ", de rtk gain, por día UTC; instalá Node ≥ 22.13 para cifras por sesión)"
	default:
		rtkNote = "rtk ahorró = ? (no se puede leer el registro de rtk)"
	}
	sub := ""
	if d.Settings.Subscription {
		sub = " · USD~ = equivalente a precios de API (suscripción)"
	}
	out = append(out, "leyenda: tokens = toda la entrada (sin caché + caché) + salida · hit = lecturas de caché / toda la entrada · agentes = parte de los tokens gastada por subagentes · llam/turno = llamadas a la API por prompt · ctx/llam = contexto promedio que arrastra cada llamada del agente principal · ediciones = archivos que editó el agente principal (Edit/Write) · rtk = llamadas a Bash filtradas por nxy · "+rtkNote+" · nxy = sesiones que corrieron con el plugin"+sub+unknownNote(total))
	var sumTok, sumWall float64
	for _, r := range rows {
		sumTok += r.TotalTokens
		sumWall += r.WallMs
	}
	out = append(out, "total: "+tokens(sumTok)+" tokens · "+usdCell(total.USD, total.USDPartial, total.USDEstimated)+" · "+format.FmtDuration(sumWall)+" de reloj")
	out = append(out, FormatBreaks(rebuilds, total.USD)...)
	if line, ok := FormatTTLWhatIf(ttl); ok {
		out = append(out, line)
	}
	return Output{Stdout: strings.Join(out, "\n") + "\n"}
}

type modelGroup struct {
	modelRow
	sessions map[string]bool
	unknown  map[string]bool
}

func trendByModel(d TrendData, sessions []TrendSession, head string, usdCol format.Col) Output {
	var order []string
	groups := map[string]*modelGroup{}
	for _, s := range sessions {
		for _, model := range s.ByModel.Keys {
			b := s.ByModel.Values[model]
			g, ok := groups[model]
			if !ok {
				g = &modelGroup{modelRow: modelRow{Model: model, UnknownModels: []string{}}, sessions: map[string]bool{}, unknown: map[string]bool{}}
				groups[model] = g
				order = append(order, model)
			}
			g.sessions[s.SessionID] = true
			g.Calls += b.Calls
			g.Input += b.Usage.Input
			g.CacheW += b.Usage.CacheWrite5m + b.Usage.CacheWrite1h
			g.CacheR += b.Usage.CacheRead
			g.Output += b.Usage.Output
			g.Thinking += b.Usage.Thinking
			g.USD += b.USD
			if b.USDPartial {
				g.USDPartial = true
			}
			if b.USDEstimated {
				g.USDEstimated = true
				for _, k := range b.EstimatedModels.Keys() {
					v, _ := b.EstimatedModels.Get(k)
					g.EstimatedModels.Set(k, v)
				}
			}
			for _, u := range b.UnknownModels {
				if !g.unknown[u] {
					g.unknown[u] = true
					g.UnknownModels = append(g.UnknownModels, u)
				}
			}
		}
	}
	rows := make([]modelRow, 0, len(order))
	for _, k := range order {
		g := groups[k]
		r := g.modelRow
		r.Sessions = len(g.sessions)
		r.Total = r.Input + r.CacheW + r.CacheR + r.Output
		r.Hit = hitPct(r.Input, r.CacheW, r.CacheR)
		rows = append(rows, r)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if x := b.USD - a.USD; x != 0 {
			return x < 0
		}
		return b.Total-a.Total < 0
	})
	var all float64
	parts := make([]usdPart, len(rows))
	for i, r := range rows {
		all += r.Total
		parts[i] = usdPart{usd: r.USD, partial: r.USDPartial, unknown: r.UnknownModels, estimated: r.USDEstimated, models: r.EstimatedModels}
	}
	total := sumUsd(parts)
	if d.JSON {
		return jsonOut(struct {
			Since  *int64     `json:"since"`
			By     string     `json:"by"`
			Models []modelRow `json:"models"`
			Total  usdSum     `json:"total"`
		}{d.Since, d.By, rows, total})
	}
	cols := []format.Col{
		{Label: "modelo"}, {Label: "ses", Right: true}, {Label: "llamadas", Right: true}, {Label: "tokens", Right: true}, {Label: "parte", Right: true},
		{Label: "sin caché", Right: true}, {Label: "cache-r", Right: true}, {Label: "salida", Right: true}, {Label: "razonam.", Right: true},
		{Label: "hit", Right: true}, usdCol,
	}
	cells := make([][]string, len(rows))
	for i, r := range rows {
		share := math.NaN()
		if all != 0 {
			share = (r.Total / all) * 100
		}
		cells[i] = []string{
			r.Model, count(r.Sessions), count(r.Calls), tokens(r.Total), format.FmtPct(share, 0),
			tokens(r.Input), tokens(r.CacheR), tokens(r.Output), tokens(r.Thinking),
			format.FmtPct(NaNOr(r.Hit), 0), usdCell(r.USD, r.USDPartial, r.USDEstimated),
		}
	}
	out := []string{
		head, "",
		format.Table(cols, cells), "",
		"leyenda: parte = parte de todos los tokens del rango · razonam. es un subconjunto de la salida" + unknownNote(total),
		"total: " + tokens(all) + " tokens · " + usdCell(total.USD, total.USDPartial, total.USDEstimated),
	}
	return Output{Stdout: strings.Join(out, "\n") + "\n"}
}
