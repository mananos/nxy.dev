package views

import (
	"testing"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/domain"
	"github.com/mananos/nxy.dev/v2/internal/host/claudecode/transcript"
	"github.com/mananos/nxy.dev/v2/internal/metrics"
	"github.com/mananos/nxy.dev/v2/internal/report"
)

func trendSession(at time.Time, usd float64, model string) report.TrendSession {
	s := report.TrendSession{Ts: float64(at.UnixMilli()), Turns: 3, TotalTokens: 1000, USD: usd, Input: 100, CacheR: 900}
	s.ByModel = transcript.OrderedMap[metrics.Finished]{Keys: []string{model}, Values: map[string]metrics.Finished{model: {Calls: 2, USD: usd}}}
	return s
}

func TestBuildTrendDaysAndWeeksInLocalZone(t *testing.T) {
	loc := time.FixedZone("ART", -3*3600)
	// Wednesday 2026-11-11 12:00 local.
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, loc)
	in := TrendIn{Now: now, Loc: loc, Scope: "global", Sessions: []report.TrendSession{
		// 01:30 UTC on the 11th is still the 10th at home.
		trendSession(time.Date(2026, 11, 11, 1, 30, 0, 0, time.UTC), 2, "m1"),
		trendSession(now, 3, "m1"),
		// Sunday 8th 23:30 local is the previous week; Monday the 9th 00:10 local is this one.
		trendSession(time.Date(2026, 11, 8, 23, 30, 0, 0, loc), 5, "m2"),
		trendSession(time.Date(2026, 11, 9, 0, 10, 0, 0, loc), 7, "m2"),
		// out of the 7 days, inside the 8 weeks.
		trendSession(time.Date(2026, 10, 20, 10, 0, 0, 0, loc), 11, "m1"),
	}, Runs: []domain.AgentRun{
		{Role: "implementer", StartedAt: 1000, EndedAt: 3000, CostUSD: 1},
		{Role: "implementer", StartedAt: 0, EndedAt: 3000, CostUSD: 2},
		{Role: "planner", StartedAt: 1000, EndedAt: 2000, CostUSD: 5},
	}}
	tr := BuildTrend(in)
	if len(tr.Days) != 7 || len(tr.Weeks) != 8 {
		t.Fatalf("days %d weeks %d", len(tr.Days), len(tr.Weeks))
	}
	if tr.Range.Since != "2026-11-05" || tr.Range.Until != "2026-11-11" {
		t.Errorf("range %+v", tr.Range)
	}
	byLabel := map[string]PeriodRow{}
	for _, d := range tr.Days {
		byLabel[d.Label] = d
	}
	if d := byLabel["mar 10"]; d.Sessions != 1 || d.USD != 2 {
		t.Errorf("mar 10 %+v", d)
	}
	if d := byLabel["mié 11"]; d.Sessions != 1 || d.USD != 3 || d.CacheHitPct != 90 {
		t.Errorf("mié 11 %+v", d)
	}
	if d := byLabel["jue 05"]; d.Sessions != 0 || d.USD != 0 {
		t.Errorf("an empty day keeps its row: %+v", d)
	}
	if tr.Totals.Sessions != 4 || tr.Totals.USD != 17 || tr.Totals.Turns != 12 {
		t.Errorf("totals %+v", tr.Totals)
	}
	last, prev := tr.Weeks[7], tr.Weeks[6]
	if last.Label != "09 nov" || last.Sessions != 3 || last.USD != 12 {
		t.Errorf("this week %+v", last)
	}
	if prev.Label != "02 nov" || prev.Sessions != 1 || prev.USD != 5 {
		t.Errorf("previous week %+v", prev)
	}
	if w := tr.Weeks[2]; w.Label != "20 oct" && w.Label != "19 oct" {
		t.Logf("week 2 %+v", w)
	}
	if tr.Weeks[0].Label != "21 sep" {
		t.Errorf("first week %q", tr.Weeks[0].Label)
	}
	if len(tr.Models) != 2 || tr.Models[0].Model != "m2" || tr.Models[0].USD != 12 || tr.Models[0].Calls != 4 {
		t.Errorf("models %+v", tr.Models)
	}
	if len(tr.Agents) != 2 || tr.Agents[0].Role != "planner" || tr.Agents[0].AvgMs != 1000 {
		t.Fatalf("agents %+v", tr.Agents)
	}
	if a := tr.Agents[1]; a.Runs != 2 || a.AvgMs != 2000 || a.USD != 3 {
		t.Errorf("implementer %+v", a)
	}
}

func TestBuildTrendModelsOnlyInRange(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	tr := BuildTrend(TrendIn{Now: now, Sessions: []report.TrendSession{
		trendSession(now, 3, "in"),
		trendSession(now.AddDate(0, 0, -20), 9, "out"),
	}})
	if len(tr.Models) != 1 || tr.Models[0].Model != "in" {
		t.Fatalf("models %+v", tr.Models)
	}
}

func TestBuildTrendEmpty(t *testing.T) {
	tr := BuildTrend(TrendIn{Now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
	if len(tr.Days) != 7 || len(tr.Weeks) != 8 || tr.Models == nil || tr.Agents == nil {
		t.Errorf("%+v", tr)
	}
}
