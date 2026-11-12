package views

import (
	"testing"

	"github.com/mananos/nxy.dev/internal/host/claudecode/transcript"
	"github.com/mananos/nxy.dev/internal/metrics"
	"github.com/mananos/nxy.dev/internal/report"
)

func TestBuildStats(t *testing.T) {
	hit := 90.0
	thr := 120000.0
	ttl := "5m"
	var s transcript.Session
	s.SessionID, s.Project, s.Turns = "abc", "/srv/dev/x", 4
	s.Usage = metrics.Finished{Calls: 20, USD: 3, CacheHitPct: &hit, TotalInput: 900}
	s.Usage.Usage.Output = 100
	s.Main = metrics.Finished{Calls: 8, TotalInput: 400}
	s.Subagents = metrics.Finished{TotalInput: 500}
	s.ByModel = transcript.OrderedMap[metrics.Finished]{Keys: []string{"cheap", "dear"}, Values: map[string]metrics.Finished{
		"cheap": {Calls: 15, USD: 1}, "dear": {Calls: 5, USD: 2},
	}}
	d := report.StatsData{Session: s, CacheTTL: &ttl, Broad: report.BroadSummary{Count: 2, ContextTokens: 5000, Threshold: &thr}}
	d.Filter.Rows = 3
	d.Filter.ByEngine.Add("rtk", 3)
	st := BuildStats(d, []SessionRef{{ID: "o"}})
	if st.Model != "dear" || st.ByModel[0].Model != "dear" {
		t.Errorf("by model %+v", st.ByModel)
	}
	if st.Calls != 20 || st.MainCalls != 8 || st.USD != 3 || st.CacheHitPct != 90 || st.CacheTTL != "5m" {
		t.Errorf("stats %+v", st)
	}
	if st.Shape.CallsPerTurn != 5 || st.Shape.MainCallsPerTurn != 2 || st.Shape.MainCtxAvg != 50 || st.Shape.SubagentSharePct != 50 {
		t.Errorf("shape %+v", st.Shape)
	}
	if st.Broad.Count != 2 || st.Broad.Threshold != 120000 || st.Filter.Rows != 3 || len(st.Filter.ByEngine) != 1 || st.Filter.ByEngine[0].Count != 3 {
		t.Errorf("broad/filter %+v %+v", st.Broad, st.Filter)
	}
	if len(st.Sessions) != 1 || st.Rtk != nil {
		t.Errorf("sessions %v rtk %v", st.Sessions, st.Rtk)
	}
}
