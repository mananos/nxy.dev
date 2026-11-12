package views

import (
	"sort"

	"github.com/mananos/nxy.dev/internal/host/claudecode/transcript"
	"github.com/mananos/nxy.dev/internal/metrics"
	"github.com/mananos/nxy.dev/internal/report"
)

// BuildStats maps the numbers of `nxy stats --json` to the screen. others are the other
// sessions of the repo, for the session switch.
func BuildStats(d report.StatsData, others []SessionRef) Stats {
	s := d.Session
	st := Stats{
		SessionID:    s.SessionID,
		Project:      s.Project,
		Turns:        s.Turns,
		Calls:        s.Usage.Calls,
		MainCalls:    s.Main.Calls,
		Usage:        s.Usage.Usage,
		USD:          s.Usage.USD,
		USDEstimated: s.Usage.USDEstimated,
		CacheHitPct:  pctOf(s.Usage.CacheHitPct),
		ByModel:      []ModelRow{},
		Filter:       FilterRow{Rows: d.Filter.Rows, ByEngine: []Count{}, EstTokens: d.Filter.EstTokens},
		Broad:        BroadRow{Count: d.Broad.Count, ContextTokens: int(d.Broad.ContextTokens)},
		Rtk:          d.RtkSavings,
		Sessions:     append([]SessionRef{}, others...),
	}
	if d.Broad.Threshold != nil {
		st.Broad.Threshold = int(*d.Broad.Threshold)
	}
	if d.CacheTTL != nil {
		st.CacheTTL = *d.CacheTTL
	}
	if s.Turns != 0 {
		st.Shape.CallsPerTurn = float64(s.Usage.Calls) / float64(s.Turns)
		st.Shape.MainCallsPerTurn = float64(s.Main.Calls) / float64(s.Turns)
	}
	if s.Main.Calls != 0 {
		st.Shape.MainCtxAvg = s.Main.TotalInput / float64(s.Main.Calls)
	}
	if den := s.Usage.TotalInput + s.Usage.Usage.Output; den != 0 {
		st.Shape.SubagentSharePct = (s.Subagents.TotalInput + s.Subagents.Usage.Output) / den * 100
	}
	for _, k := range d.Filter.ByEngine.Keys() {
		st.Filter.ByEngine = append(st.Filter.ByEngine, Count{Name: k, Count: int(d.Filter.ByEngine.Get(k))})
	}
	st.ByModel = modelRows(s.ByModel)
	if len(st.ByModel) > 0 {
		st.Model = st.ByModel[0].Model
	}
	return st
}

func pctOf(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

// modelRows lists the buckets by USD, the biggest first (ties: more calls, then name).
func modelRows(m transcript.OrderedMap[metrics.Finished]) []ModelRow {
	rows := make([]ModelRow, 0, len(m.Keys))
	for _, k := range m.Keys {
		f := m.Values[k]
		rows = append(rows, ModelRow{Model: k, Calls: f.Calls, Usage: f.Usage, USD: f.USD, CacheHitPct: pctOf(f.CacheHitPct), Estimated: f.USDEstimated})
	}
	sortModelRows(rows)
	return rows
}

func sortModelRows(rows []ModelRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.USD != b.USD {
			return a.USD > b.USD
		}
		if a.Calls != b.Calls {
			return a.Calls > b.Calls
		}
		return a.Model < b.Model
	})
}
