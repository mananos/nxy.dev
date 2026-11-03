package memory

import (
	"strings"
	"testing"
	"time"
)

var renderNow = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

func daysAgo(n int) int64 { return renderNow.Add(-time.Duration(n) * 24 * time.Hour).UnixMilli() }

func TestRenderShowRulesAndLearnings(t *testing.T) {
	rules := []Rule{
		{Meta: Meta{CreatedAt: daysAgo(91)}, Short: "K1", Text: "use tabs", Question: "indent?", Origin: "manual", Deliveries: 2, Citations: 1},
		{Meta: Meta{CreatedAt: daysAgo(91)}, Short: "K2", Text: "no globals", Question: "state?", Origin: "plan", LastCitedAt: daysAgo(10)},
	}
	learnings := []Learning{
		{Short: "A1", Text: "a", Seen: 1},
		{Short: "A2", Text: "b", Seen: 3},
	}
	want := "Rules (2)\n" +
		"K1 · use tabs\n   settles: indent? · manual · delivered 2 · cited 1 · to review\n" +
		"K2 · no globals\n   settles: state? · plan · delivered 0 · cited 0\n" +
		"\nLearnings (2)\n" +
		"A2 · b · seen 3 times · candidate: nxy mem promote A2 --question \"...\"\n" +
		"A1 · a · seen 1 time\n"
	if got := RenderShow(rules, learnings, renderNow); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderShowEmptyAndBudget(t *testing.T) {
	if got := RenderShow(nil, nil, renderNow); got != "no rules for this repo yet\n" {
		t.Fatalf("empty: %q", got)
	}
	long := strings.Repeat("x", 1600)
	rules := []Rule{
		{Meta: Meta{CreatedAt: daysAgo(1)}, Short: "K1", Text: long, Question: "q", Origin: "manual"},
		{Meta: Meta{CreatedAt: daysAgo(1)}, Short: "K2", Text: long, Question: "q", Origin: "manual"},
	}
	got := RenderShow(rules, nil, renderNow)
	if !strings.Contains(got, "too many rules for one dispatch (3200 chars > 3000): review them\n") {
		t.Fatalf("no budget warning:\n%s", got)
	}
	if strings.Contains(RenderShow(rules[:1], nil, renderNow), "too many rules") {
		t.Fatal("warning below the budget")
	}
}

func TestRenderRelatedSearchNotes(t *testing.T) {
	if got := RenderRelated(nil, nil); got != "nothing related to those files\n" {
		t.Fatalf("related empty: %q", got)
	}
	got := RenderRelated([]ClosedFeature{{Meta: Meta{ID: "F1"}, Title: "add greet"}}, []Learning{{Short: "A4", Text: "x", Seen: 2}})
	want := "Closed features (1)\nF1 · add greet\n\nLearnings (1)\nA4 · x · seen 2 times\n"
	if got != want {
		t.Fatalf("related: %q", got)
	}
	if got := RenderSearch(nil); got != "no results\n" {
		t.Fatalf("search empty: %q", got)
	}
	got = RenderSearch([]Hit{{Kind: "rule", Short: "K1", Text: "a  b"}, {Kind: "feature", ID: "F9", Text: "t"}})
	if got != "rule · K1 · a b\nfeature · F9 · t\n" {
		t.Fatalf("search: %q", got)
	}
	notes := []Note{{Short: "N1", Title: "wip", Meta: Meta{UpdatedAt: daysAgo(0)}}, {Short: "N2", Title: "old", Meta: Meta{UpdatedAt: daysAgo(3)}}}
	if got := RenderNotes(notes, renderNow); got != "N1 · wip · today\nN2 · old · 3 days ago\n" {
		t.Fatalf("notes: %q", got)
	}
	if got := RenderNotes(nil, renderNow); got != "no notes for this folder\n" {
		t.Fatalf("notes empty: %q", got)
	}
}

func TestRenderDiff(t *testing.T) {
	d := Diff{
		Rules: []Change[Rule]{
			{Kind: New, Row: Rule{Text: "r1"}, ReassignShort: true},
			{Kind: Skipped, Row: Rule{Text: "r2"}},
		},
		Learnings: []Change[Learning]{
			{Kind: Changed, Row: Learning{Text: "l1"}, Deleted: true},
			{Kind: Folded, Row: Learning{Text: "l2"}},
		},
	}
	want := "rules: 1 new, 0 changed, 0 deleted, 0 folded, 1 skipped\n" +
		"features: 0 new, 0 changed, 0 deleted, 0 folded, 0 skipped\n" +
		"learnings: 0 new, 0 changed, 1 deleted, 1 folded, 0 skipped\n" +
		"notes: 0 new, 0 changed, 0 deleted, 0 folded, 0 skipped\n" +
		"  new rule: r1 (short reference reassigned)\n" +
		"  changed (deleted) learning: l1\n" +
		"  folded learning: l2\n" +
		"dry run: nothing written, run with --apply\n"
	if got := RenderDiff(d, false); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if got := RenderDiff(d, true); !strings.HasSuffix(got, "imported 3 changes\n") {
		t.Fatalf("apply: %q", got)
	}
	if got := RenderDiff(Diff{}, false); !strings.HasSuffix(got, "nothing to import\n") {
		t.Fatalf("empty: %q", got)
	}
}
