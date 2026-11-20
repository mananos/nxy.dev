package app

import (
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/host/claudecode/transcript"
	"github.com/mananos/nxy.dev/v2/internal/metrics"
	"github.com/mananos/nxy.dev/v2/internal/report"
)

// trendFake lists sessions by id and gives each the first timestamp in ts.
type trendFake struct{ ts map[string]int64 }

func (trendFake) ResolveSession(string, SessionLookup) *transcript.SessionRef { return nil }
func (f trendFake) ListSessions(SessionLookup) []transcript.SessionRef {
	var out []transcript.SessionRef
	for id := range f.ts {
		out = append(out, transcript.SessionRef{SessionID: id})
	}
	return out
}
func (f trendFake) ParseSession(ref transcript.SessionRef, _ transcript.SessionOpts) transcript.Session {
	ts := f.ts[ref.SessionID]
	return transcript.Session{SessionID: ref.SessionID, FirstTs: &ts, Usage: metrics.Finished{Calls: 1}}
}
func (trendFake) LastCacheTTL(string) (string, bool) { return "", false }

type noLedger struct{}

func (noLedger) Rows(since, until *int64) ([]report.LedgerRow, bool) { return nil, false }
func (noLedger) Unavailable() string                                 { return "no-db" }
func (noLedger) DBPath() string                                      { return "" }

func TestTrendSessionsUntil(t *testing.T) {
	t.Setenv("NXY_HOME", t.TempDir())
	u := Usage{
		Transcripts:  trendFake{ts: map[string]int64{"early": 1000, "late": 5000}},
		Ledger:       noLedger{},
		FilterGroups: func(string, string) []report.FilterGroup { return nil },
		Config:       func(string) map[string]any { return map[string]any{} },
	}
	if got := u.TrendSessions(nil, nil, false, t.TempDir()); len(got) != 2 {
		t.Fatalf("without a bound: %d sessions", len(got))
	}
	until := int64(2000)
	got := u.TrendSessions(nil, &until, false, t.TempDir())
	if len(got) != 1 || got[0].SessionID != "early" {
		t.Fatalf("until: %+v", got)
	}
}
