package sqlite

import (
	"reflect"
	"testing"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/host/claudecode/transcript"
	"github.com/mananos/nxy.dev/v2/internal/metrics"
	"github.com/mananos/nxy.dev/v2/internal/pricing"
	"github.com/mananos/nxy.dev/v2/internal/report"
)

func sampleUsage(id string) report.SessionUsage {
	var est metrics.ModelBasis
	est.Set("zeta-model", "sonnet")
	est.Set("alpha-model", "opus")
	var by transcript.OrderedMap[metrics.Finished]
	by.Keys = []string{"zeta-model", "alpha-model", "mid-model"}
	by.Values = map[string]metrics.Finished{
		"zeta-model":  {Calls: 3, Usage: pricing.Usage{Input: 10, Output: 5}, USD: 1.5, UnknownModels: []string{"x"}, EstimatedModels: est},
		"alpha-model": {Calls: 1, USD: 0.25},
		"mid-model":   {Calls: 2, USD: 0.5},
	}
	return report.SessionUsage{
		SessionID:       id,
		Project:         "proj",
		Ts:              1700000000000,
		Turns:           4,
		Calls:           6,
		USD:             2.25,
		USDEstimated:    true,
		EstimatedModels: est,
		FirstPrompt:     "hola",
		ByModel:         by,
		CacheBreaks:     []metrics.Break{{Ts: 5, Cause: "ttl", TTL: "5m"}},
		Cwd:             "/work/r",
		FirstTs:         1700000000000,
		LastTs:          1700000900000,
		UsedNxySkill:    true,
	}
}

func TestSessionUsageRoundTrip(t *testing.T) {
	s, clock := newMem(t)
	fresh := clock.Add(-time.Hour).UnixMilli()
	old := clock.Add(-91 * 24 * time.Hour).UnixMilli()
	rows := []SessionUsageRow{
		{Path: "/t/a.jsonl", SessionID: "a", Stamp: "sig-a", MtimeMs: fresh, Usage: sampleUsage("a")},
		{Path: "/t/b.jsonl", SessionID: "b", Stamp: "sig-b", MtimeMs: old, Usage: sampleUsage("b")},
	}
	if err := s.SaveSessionUsage(ctx, rows); err != nil {
		t.Fatal(err)
	}
	got, err := s.SessionUsageRows(ctx, []string{"/t/a.jsonl", "/t/b.jsonl", "/t/none.jsonl"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("rows = %d, want only the fresh one (old pruned): %+v", len(got), got)
	}
	if !reflect.DeepEqual(got["/t/a.jsonl"], rows[0]) {
		t.Fatalf("round trip differs:\n got %+v\nwant %+v", got["/t/a.jsonl"], rows[0])
	}

	// An upsert replaces the row.
	rows[0].Stamp = "sig-a2"
	rows[0].Usage.Calls = 99
	if err := s.SaveSessionUsage(ctx, rows[:1]); err != nil {
		t.Fatal(err)
	}
	got, _ = s.SessionUsageRows(ctx, []string{"/t/a.jsonl"})
	if got["/t/a.jsonl"].Stamp != "sig-a2" || got["/t/a.jsonl"].Usage.Calls != 99 {
		t.Fatalf("upsert = %+v", got)
	}
}

func TestSessionUsageReaderWithoutTable(t *testing.T) {
	path := dbPath(t)
	migs, _ := embeddedMigrations()
	s, err := openWith(ctx, path, time.Now, migs[:16])
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	r, ok, err := OpenReadOnly(ctx, path)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	defer r.Close()
	got, err := r.SessionUsageRows(ctx, []string{"/t/a.jsonl"})
	if err != nil || len(got) != 0 {
		t.Fatalf("got %+v %v", got, err)
	}
}
