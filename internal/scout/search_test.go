package scout

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mananos/nxy.dev/v2/internal/repomap"
)

// fake is a set of engines with one canned answer each.
type fake struct {
	info    *IndexInfo
	built   bool
	rows    []repomap.Row
	rg      bool
	rgRes   RgResult
	code    CodeResult
	buildEr error
}

func (f *fake) engines() Engines {
	return Engines{
		Index: func() (IndexInfo, bool) {
			if f.info == nil {
				return IndexInfo{}, false
			}
			return *f.info, true
		},
		Build: func(bool) (IndexInfo, error) {
			f.built = true
			return IndexInfo{Files: 2, Symbols: 3}, f.buildEr
		},
		Query:     func(string, int) []repomap.Row { return f.rows },
		RgFound:   func() bool { return f.rg },
		Rg:        func(string, RgOpts) RgResult { return f.rgRes },
		Explore:   func(string) CodeResult { return f.code },
		Relations: func(string, string) CodeResult { return f.code },
	}
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestSearchWithoutRgNorCodegraph(t *testing.T) {
	f := &fake{}
	r := Search("where is cancelBooking", SearchOpts{}, f.engines())
	if !f.built {
		t.Error("no index: it must build one")
	}
	for _, want := range []string{"## repo index (2 files, 3 symbols)", "_ripgrep is not installed: this layer is skipped._", "_Disabled by configuration._"} {
		if !strings.Contains(r.Text, want) {
			t.Errorf("text lacks %q:\n%s", want, r.Text)
		}
	}
	for _, d := range []string{"repomap:no-hits", "rg:missing"} {
		if !has(r.Degraded, d) {
			t.Errorf("degraded %v lacks %s", r.Degraded, d)
		}
	}
	if r.Codegraph != "disabled" || len(r.Hits) != 0 {
		t.Errorf("codegraph=%q hits=%v", r.Codegraph, r.Hits)
	}
}

func TestSearchReusesAnIndexUnlessRebuild(t *testing.T) {
	f := &fake{info: &IndexInfo{Files: 9, Symbols: 8}}
	r := Search("cancelBooking", SearchOpts{}, f.engines())
	if f.built || !strings.Contains(r.Text, "(9 files, 8 symbols)") {
		t.Errorf("built=%v text=%s", f.built, r.Text)
	}
	r = Search("cancelBooking", SearchOpts{Rebuild: true}, f.engines())
	if !f.built || !strings.Contains(r.Text, "(2 files, 3 symbols)") {
		t.Errorf("rebuild: built=%v text=%s", f.built, r.Text)
	}
}

func TestSearchBuildErrorIsReportedNotFatal(t *testing.T) {
	f := &fake{buildEr: errors.New("disk full")}
	r := Search("cancelBooking", SearchOpts{}, f.engines())
	if !has(r.Degraded, "repomap:error") || r.Text == "" {
		t.Errorf("degraded=%v", r.Degraded)
	}
}

func TestSearchCapsTheOutput(t *testing.T) {
	f := &fake{rg: true, rgRes: RgResult{OK: true, Rows: []RgRow{{Path: "a.go", Line: 1, Text: strings.Repeat("x", 200)}}},
		code: CodeResult{OK: true, Text: strings.Repeat("y", 50_000), Ms: 3}}
	r := Search("cancelBooking", SearchOpts{Codegraph: true}, f.engines())
	if !strings.HasSuffix(r.Text, "\n\n_[output capped by nxy]_") || !has(r.Degraded, "output:truncated") {
		t.Errorf("not capped: %d chars, degraded %v", len(r.Text), r.Degraded)
	}
	if n := utf8.RuneCountInString(strings.TrimSuffix(r.Text, "\n\n_[output capped by nxy]_")); n != 40_000 {
		t.Errorf("kept %d chars, want 40000", n)
	}
}

func TestRelationsFallbacks(t *testing.T) {
	f := &fake{rg: true, code: CodeResult{Reason: "timeout"},
		rgRes: RgResult{OK: true, Truncated: true, Rows: []RgRow{{Path: "a.go", Line: 4, Text: "call(x)"}}}}
	r := Relations("callers", "x", SearchOpts{Codegraph: true}, f.engines())
	for _, d := range []string{"codegraph:timeout", "rg:direct-references-only"} {
		if !has(r.Degraded, d) {
			t.Errorf("degraded %v lacks %s", r.Degraded, d)
		}
	}
	if !strings.Contains(r.Text, "direct references only (capped)") || len(r.Hits) != 1 || r.Hits[0].Source != "rg" {
		t.Errorf("text=%s hits=%v", r.Text, r.Hits)
	}

	f.rg = false
	r = Relations("impact", "x", SearchOpts{}, f.engines())
	if !has(r.Degraded, "rg:missing") || r.Codegraph != "disabled" {
		t.Errorf("degraded=%v codegraph=%s", r.Degraded, r.Codegraph)
	}
}

func TestDescribeEngines(t *testing.T) {
	got := DescribeEngines(&IndexInfo{Files: 3, Symbols: 7, AgeMs: 12_500}, true, "codegraph:    x")
	want := "repo map:     3 files, 7 symbols (built 13s ago)\nripgrep (rg): available\ncodegraph:    x"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	got = DescribeEngines(nil, false, "c")
	if !strings.Contains(got, "not built — it builds itself on the first /nxy:locate") || !strings.Contains(got, "not found (the scout uses the index only)") {
		t.Errorf("got\n%s", got)
	}
}
