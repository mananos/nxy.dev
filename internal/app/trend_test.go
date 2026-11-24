package app

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/version"

	"github.com/mananos/nxy.dev/v2/internal/host/claudecode/transcript"
	"github.com/mananos/nxy.dev/v2/internal/metrics"
	"github.com/mananos/nxy.dev/v2/internal/report"
)

// trendFake lists sessions by id and gives each the first timestamp in ts. stamps and mtimes are
// what Stamp answers per id; parses counts the ParseSession calls when set.
type trendFake struct {
	ts     map[string]int64
	stamps map[string]string
	mtimes map[string]float64
	parses *int
}

func (trendFake) ResolveSession(string, SessionLookup) *transcript.SessionRef { return nil }
func (f trendFake) ListSessions(SessionLookup) []transcript.SessionRef {
	var out []transcript.SessionRef
	for id := range f.ts {
		out = append(out, transcript.SessionRef{SessionID: id, Path: "p/" + id, MtimeMs: f.mtimes[id]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SessionID < out[j].SessionID })
	return out
}
func (f trendFake) ParseSession(ref transcript.SessionRef, _ transcript.SessionOpts) transcript.Session {
	if f.parses != nil {
		*f.parses++
	}
	ts := f.ts[ref.SessionID]
	last := ts + 5000
	s := transcript.Session{SessionID: ref.SessionID, Cwd: "/w", FirstTs: &ts, LastTs: &last, Usage: metrics.Finished{Calls: 1}}
	if ref.SessionID == "quiet" {
		s.BySkill.Keys = []string{"nxy:plan"}
	}
	return s
}
func (trendFake) LastCacheTTL(string) (string, bool) { return "", false }
func (f trendFake) Stamp(ref transcript.SessionRef) (string, float64) {
	return f.stamps[ref.SessionID], f.mtimes[ref.SessionID]
}

// memCache is a SessionUsageCache in memory.
type memCache struct {
	rows  map[string]SessionUsageRow
	saves int
}

func (c *memCache) Rows(paths []string) map[string]SessionUsageRow {
	out := map[string]SessionUsageRow{}
	for _, p := range paths {
		if r, ok := c.rows[p]; ok {
			out[p] = r
		}
	}
	return out
}
func (c *memCache) Save(rows []SessionUsageRow) {
	c.saves++
	for _, r := range rows {
		c.rows[r.Path] = r
	}
}

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

func TestTrendSessionsCached(t *testing.T) {
	t.Setenv("NXY_HOME", t.TempDir())
	now := time.UnixMilli(1_800_000_000_000)
	quietMs := float64(now.UnixMilli()) - 11*60_000
	liveMs := float64(now.UnixMilli()) - 60_000
	parses, manyCalls := 0, 0
	fake := trendFake{
		ts:     map[string]int64{"quiet": 1000, "live": 2000},
		stamps: map[string]string{"quiet": "q1", "live": "l1"},
		mtimes: map[string]float64{"quiet": quietMs, "live": liveMs},
		parses: &parses,
	}
	threshold := 100000.0
	cache := &memCache{rows: map[string]SessionUsageRow{}}
	filters := func(keys []FilterKey) map[FilterKey][]report.FilterGroup {
		manyCalls++
		out := map[FilterKey][]report.FilterGroup{}
		for _, k := range keys {
			out[k] = []report.FilterGroup{{Engine: "rtk", Rows: 3}}
		}
		return out
	}
	mk := func(c SessionUsageCache) Usage {
		return Usage{
			Transcripts:      fake,
			Ledger:           noLedger{},
			FilterGroups:     func(string, string) []report.FilterGroup { return []report.FilterGroup{{Engine: "rtk", Rows: 3}} },
			FilterGroupsMany: filters,
			SessionCache:     c,
			Clock:            func() time.Time { return now },
			Config: func(string) map[string]any {
				return map[string]any{"metrics": map[string]any{"cacheBreakThreshold": threshold}}
			},
		}
	}
	u := mk(cache)
	run := func() []report.TrendSession { return u.TrendSessions(nil, nil, false, t.TempDir()) }

	first := run()
	if parses != 2 || manyCalls != 1 || len(first) != 2 {
		t.Fatalf("first run: parses %d, many %d, sessions %d", parses, manyCalls, len(first))
	}
	if _, ok := cache.rows["p/quiet"]; !ok || len(cache.rows) != 1 {
		t.Fatalf("only the quiet session is stored: %v", cache.rows)
	}

	parses = 0
	second := run()
	if parses != 1 {
		t.Fatalf("same stamp: only the live session is parsed, got %d", parses)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("cached rows differ:\n%+v\n%+v", first, second)
	}

	bare := mk(nil)
	if want := bare.TrendSessions(nil, nil, false, t.TempDir()); !reflect.DeepEqual(want, second) {
		t.Fatalf("rows differ from the run without a table:\n%+v\n%+v", want, second)
	}
	if !first[0].Nxy || first[0].RtkCalls != 3 {
		t.Fatalf("filter data not attached: %+v", first[0])
	}

	parses = 0
	fake.stamps["quiet"] = "q2"
	run()
	if parses != 2 {
		t.Fatalf("changed stamp re-reads the session: %d", parses)
	}
	parses = 0
	threshold = 5
	run()
	if parses != 2 {
		t.Fatalf("changed threshold re-reads the session: %d", parses)
	}
	parses = 0
	old := version.Version
	version.Version = "9.9.9-test"
	defer func() { version.Version = old }()
	run()
	if parses != 2 {
		t.Fatalf("changed nxy version re-reads the session: %d", parses)
	}
	parses = 0
	run()
	if parses != 1 {
		t.Fatalf("stored again: only the live one is parsed, got %d", parses)
	}
}
