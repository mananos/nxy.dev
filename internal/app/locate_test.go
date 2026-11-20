package app

import (
	"context"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/repomap"
	"github.com/mananos/nxy.dev/v2/internal/scout"
)

type fakeIndex struct {
	info  *scout.IndexInfo
	rows  []repomap.Row
	built []bool
}

func (f *fakeIndex) Info(string) (scout.IndexInfo, bool) {
	if f.info == nil {
		return scout.IndexInfo{}, false
	}
	return *f.info, true
}

func (f *fakeIndex) Build(_ string, force bool) (scout.IndexInfo, error) {
	f.built = append(f.built, force)
	return scout.IndexInfo{Files: 1, Symbols: len(f.rows)}, nil
}

func (f *fakeIndex) Query(_, _ string, _ int) []repomap.Row { return f.rows }

type fakeText struct {
	on   bool
	seen []string
	res  scout.RgResult
}

func (f *fakeText) Available() bool { return f.on }
func (f *fakeText) Search(_ context.Context, _, pattern string, _ scout.RgOpts) scout.RgResult {
	f.seen = append(f.seen, pattern)
	return f.res
}

type fakeCode struct{ res scout.CodeResult }

func (f fakeCode) Explore(context.Context, string, string) scout.CodeResult { return f.res }
func (f fakeCode) Relations(context.Context, string, string, string) scout.CodeResult {
	return f.res
}
func (f fakeCode) Describe(string) string { return "codegraph:    fake" }

func TestLocatorRunsTheThreeEngines(t *testing.T) {
	idx := &fakeIndex{rows: []repomap.Row{{Path: "a.go", Line: 3, Kind: "func", Name: "CancelBooking", Sig: "func CancelBooking()"}}}
	txt := &fakeText{on: true, res: scout.RgResult{OK: true, Rows: []scout.RgRow{{Path: "b.go", Line: 9, Text: "cancelBooking(x)"}}}}
	l := Locator{Index: idx, Text: txt, Code: fakeCode{scout.CodeResult{OK: true, Text: "graph says", Ms: 4}}}
	r := l.Locate(context.Background(), "/p", "where is cancelBooking", scout.SearchOpts{Codegraph: true})
	if len(idx.built) != 1 || idx.built[0] {
		t.Errorf("builds = %v: no index yet, an incremental build", idx.built)
	}
	if len(txt.seen) == 0 || txt.seen[0] != "cancelBooking" {
		t.Errorf("rg asked %v", txt.seen)
	}
	for _, want := range []string{"a.go:3\tfunc\tCancelBooking", "b.go:9: cancelBooking(x)", "graph says"} {
		if !strings.Contains(r.Text, want) {
			t.Errorf("text lacks %q:\n%s", want, r.Text)
		}
	}
	if len(r.Hits) != 2 || r.Hits[0].Source != "index" || r.Hits[1].Source != "rg" || r.Codegraph != "ok" {
		t.Errorf("hits = %+v codegraph = %s", r.Hits, r.Codegraph)
	}
}

func TestLocatorWithNoEnginesStillAnswers(t *testing.T) {
	r := Locator{}.Locate(context.Background(), "/p", "where is cancelBooking", scout.SearchOpts{Codegraph: true})
	if r.Text == "" || r.Codegraph != "not-installed" {
		t.Fatalf("text=%q codegraph=%s", r.Text, r.Codegraph)
	}
	for _, d := range []string{"repomap:no-hits", "rg:missing", "codegraph:not-installed"} {
		found := false
		for _, x := range r.Degraded {
			found = found || x == d
		}
		if !found {
			t.Errorf("degraded %v lacks %s", r.Degraded, d)
		}
	}
	rel := Locator{}.Relations(context.Background(), "/p", "callers", "x", scout.SearchOpts{Codegraph: true})
	if rel.Text == "" || len(rel.Terms) != 1 || rel.Terms[0] != "x" {
		t.Fatalf("relations = %+v", rel)
	}
}

func TestLocatorStatus(t *testing.T) {
	l := Locator{Index: &fakeIndex{info: &scout.IndexInfo{Files: 2, Symbols: 5, AgeMs: 3000}}, Text: &fakeText{on: true}, Code: fakeCode{}}
	want := "repo map:     2 files, 5 symbols (built 3s ago)\nripgrep (rg): available\ncodegraph:    fake"
	if got := l.Status("/p"); got != want {
		t.Errorf("status\n%s\nwant\n%s", got, want)
	}
	if got := (Locator{}).Status("/p"); !strings.Contains(got, "not built") || !strings.Contains(got, "not installed") {
		t.Errorf("empty status\n%s", got)
	}
}
