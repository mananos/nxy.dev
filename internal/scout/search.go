package scout

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mananos/nxy.dev/v2/internal/jscompat"
	"github.com/mananos/nxy.dev/v2/internal/repomap"
)

// maxOutputChars is the cap of a report (~10k tokens), inside a scout that dies, never the main thread.
const maxOutputChars = 40_000

const cappedNote = "\n\n_[output capped by nxy]_"

// Hit is one place a report points at: from the repo map (source "index") or from rg (source "rg").
type Hit struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Source string `json:"source"`
	Text   string `json:"text,omitempty"`
}

// Report is what a search or a relations query answers: the text the scout reads, the hits it
// points at, what degraded and the state of codegraph ("ok", "disabled", a reason or "no-terms").
type Report struct {
	Text      string
	Degraded  []string
	Terms     []string
	Hits      []Hit
	Codegraph string
}

// IndexInfo is what an existing (or just built) repo map holds.
type IndexInfo struct {
	Files   int
	Symbols int
	AgeMs   float64
}

// RgRow is one line rg found.
type RgRow struct {
	Path string
	Line int
	Text string
}

// RgOpts narrows an rg search; zero values mean the 1.x defaults (30 rows, 3 per file, literal).
type RgOpts struct {
	MaxRows    int
	MaxPerFile int
	Word       bool
}

// RgResult is the answer of rg: OK is false when rg could not answer (no match is OK with no rows).
type RgResult struct {
	OK        bool
	Reason    string
	Rows      []RgRow
	Truncated bool
}

// CodeResult is the answer of a codegraph query, already capped by the adapter.
type CodeResult struct {
	OK        bool
	Reason    string
	Text      string
	Truncated bool
	Ms        int64
}

// Engines are the three model-free engines as functions, so the report is built by pure code.
type Engines struct {
	// Index is the existing repo map, ok false when there is none.
	Index func() (IndexInfo, bool)
	// Build builds (force: from scratch) the repo map.
	Build func(force bool) (IndexInfo, error)
	// Query searches the repo map for a term.
	Query func(term string, limit int) []repomap.Row
	// RgFound says whether rg is available.
	RgFound func() bool
	Rg      func(pattern string, o RgOpts) RgResult
	// Explore and Relations ask codegraph.
	Explore   func(question string) CodeResult
	Relations func(kind, symbol string) CodeResult
}

// SearchOpts are the options of Search.
type SearchOpts struct {
	Codegraph bool // false: codegraph is disabled by configuration
	Rebuild   bool
}

// ordered is an insertion-ordered set of rows keyed by path:line, like a JS Map: setting an existing
// key replaces the value and keeps the position.
type ordered[V any] struct {
	keys []string
	m    map[string]V
}

func newOrdered[V any]() *ordered[V] { return &ordered[V]{m: map[string]V{}} }

func (o *ordered[V]) set(k string, v V) {
	if _, ok := o.m[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.m[k] = v
}

func (o *ordered[V]) has(k string) bool { _, ok := o.m[k]; return ok }

func (o *ordered[V]) values() []V {
	out := make([]V, 0, len(o.keys))
	for _, k := range o.keys {
		out = append(out, o.m[k])
	}
	return out
}

func rowKey(path string, line int) string { return path + ":" + strconv.Itoa(line) }

// capText applies the output cap (in UTF-16 units, like the 1.x) and records it in degraded.
func capText(text string, degraded []string) (string, []string) {
	if jscompat.UTF16Len(text) > maxOutputChars {
		return jscompat.UTF16Prefix(text, maxOutputChars) + cappedNote, append(degraded, "output:truncated")
	}
	return text, degraded
}

func capHits(h []Hit, n int) []Hit {
	if len(h) > n {
		return h[:n]
	}
	if h == nil {
		return []Hit{}
	}
	return h
}

// Search runs every available engine for a question and builds the compact report plus what degraded
// (searchWithoutModel of the 1.x). It never fails and never answers nothing: when every engine fails
// the report says so.
func Search(question string, opts SearchOpts, e Engines) Report {
	var degraded []string
	identifiers, terms := QueryTerms(question)
	out := []string{`# model-free search — "` + question + `"`, ""}

	if len(terms) == 0 {
		out = append(out, "No searchable term could be pulled out of this question. Search it yourself with Grep/Glob.")
		return Report{Text: strings.Join(out, "\n"), Degraded: []string{"no-terms"}, Terms: terms, Hits: []Hit{}, Codegraph: "no-terms"}
	}
	hits := newOrdered[Hit]()
	codegraph := "ok"

	// 1. repo map
	info, has := e.Index()
	if !has || opts.Rebuild {
		built, err := e.Build(opts.Rebuild)
		if err != nil {
			degraded = append(degraded, "repomap:error")
			built = IndexInfo{}
		}
		info = IndexInfo{Files: built.Files, Symbols: built.Symbols}
	}
	symbols := newOrdered[repomap.Row]()
	for _, t := range terms {
		for _, r := range e.Query(t, 25) {
			symbols.set(rowKey(r.Path, r.Line), r)
		}
	}
	for _, r := range symbols.values() {
		hits.set(rowKey(r.Path, r.Line), Hit{Path: r.Path, Line: r.Line, Kind: r.Kind, Name: r.Name, Source: "index"})
	}
	out = append(out, fmt.Sprintf("## repo index (%d files, %d symbols)", info.Files, info.Symbols))
	if len(symbols.keys) > 0 {
		out = append(out, "```")
		for i, r := range symbols.values() {
			if i >= 60 {
				break
			}
			out = append(out, fmt.Sprintf("%s:%d\t%s\t%s\t%s", r.Path, r.Line, r.Kind, r.Name, r.Sig))
		}
		out = append(out, "```")
	} else {
		out = append(out, "_No symbols for these terms — the domain vocabulary probably differs from the code._")
		degraded = append(degraded, "repomap:no-hits")
	}
	out = append(out, "")

	// 2. rg
	out = append(out, "## exact text (rg)")
	if !e.RgFound() {
		out = append(out, "_ripgrep is not installed: this layer is skipped._")
		degraded = append(degraded, "rg:missing")
	} else {
		anyHit := false
		asked := append(append([]string{}, identifiers...), terms...)
		if len(asked) > 5 {
			asked = asked[:5]
		}
		for _, t := range asked {
			res := e.Rg(t, RgOpts{MaxRows: 12})
			if !res.OK || len(res.Rows) == 0 {
				continue
			}
			anyHit = true
			capped := ""
			if res.Truncated {
				capped = " (capped)"
			}
			out = append(out, "`"+t+"`"+capped+":", "```")
			for _, r := range res.Rows {
				out = append(out, fmt.Sprintf("%s:%d: %s", r.Path, r.Line, r.Text))
				if k := rowKey(r.Path, r.Line); !hits.has(k) {
					hits.set(k, Hit{Path: r.Path, Line: r.Line, Kind: "text", Name: t, Source: "rg", Text: jscompat.UTF16Prefix(r.Text, 200)})
				}
			}
			out = append(out, "```")
		}
		if !anyHit {
			out = append(out, "_None of the terms appear literally anywhere in the repo._")
			degraded = append(degraded, "rg:no-hits")
		}
	}
	out = append(out, "")

	// 3. codegraph
	out = append(out, "## codegraph")
	if !opts.Codegraph {
		out = append(out, "_Disabled by configuration._")
		codegraph = "disabled"
	} else {
		res := e.Explore(question)
		if res.OK {
			out = append(out, payloadLine(res), "```", res.Text, "```")
		} else {
			out = append(out, "_Unavailable ("+res.Reason+") — answered with the index and rg instead._")
			degraded = append(degraded, "codegraph:"+res.Reason)
			codegraph = res.Reason
		}
	}

	text, degraded := capText(strings.Join(out, "\n"), degraded)
	return Report{Text: text, Degraded: nonNil(degraded), Terms: terms, Hits: capHits(hits.values(), 25), Codegraph: codegraph}
}

func payloadLine(res CodeResult) string {
	capped := ""
	if res.Truncated {
		capped = ", payload capped"
	}
	return fmt.Sprintf("_%d ms%s_", res.Ms, capped)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// Relations answers "who calls X", "what X calls" and "what breaks if X changes" (relationsWithoutModel
// of the 1.x): codegraph first; when it is missing, unindexed, slow or empty, rg -w (direct references
// only) or, for callees, the definition in the repo map, and it says so. kind is callers, callees or impact.
func Relations(kind, symbol string, opts SearchOpts, e Engines) Report {
	var degraded []string
	hits := []Hit{}
	out := []string{`# model-free relations — ` + kind + ` of "` + symbol + `"`, ""}
	codegraph := "ok"

	if !opts.Codegraph {
		codegraph = "disabled"
	} else {
		res := e.Relations(kind, symbol)
		if res.OK {
			out = append(out, "## codegraph", payloadLine(res), "```", res.Text, "```")
			text, degraded := capText(strings.Join(out, "\n"), degraded)
			return Report{Text: text, Degraded: nonNil(degraded), Terms: []string{symbol}, Hits: hits, Codegraph: codegraph}
		}
		codegraph = res.Reason
	}
	degraded = append(degraded, "codegraph:"+codegraph)
	out = append(out, "_codegraph unavailable ("+codegraph+") — answered with the index and rg instead._", "")

	if kind == "callees" {
		degraded = append(degraded, "callees:definition-only")
		out = append(out, "## definition (repo map)")
		rows := e.Query(symbol, 10)
		if len(rows) > 0 {
			out = append(out, "```")
			for _, r := range rows {
				out = append(out, fmt.Sprintf("%s:%d\t%s\t%s\t%s", r.Path, r.Line, r.Kind, r.Name, r.Sig))
				hits = append(hits, Hit{Path: r.Path, Line: r.Line, Kind: r.Kind, Name: r.Name, Source: "index"})
			}
			out = append(out, "```")
		} else {
			out = append(out, "_No definition found in the repo map._")
			degraded = append(degraded, "repomap:no-hits")
		}
		out = append(out, "", "_Callees need codegraph: read the body of the definition above to see what it calls._")
	} else {
		degraded = append(degraded, "rg:direct-references-only")
		out = append(out, "## direct references (rg -w)")
		if !e.RgFound() {
			out = append(out, "_ripgrep is not installed: nothing to search with._")
			degraded = append(degraded, "rg:missing")
		} else {
			res := e.Rg(symbol, RgOpts{Word: true, MaxRows: 40, MaxPerFile: 20})
			if res.OK && len(res.Rows) > 0 {
				capped := ""
				if res.Truncated {
					capped = " (capped)"
				}
				out = append(out, "_direct references only"+capped+": indirect callers (dynamic dispatch, re-exports) are not visible without codegraph._", "```")
				for _, r := range res.Rows {
					out = append(out, fmt.Sprintf("%s:%d: %s", r.Path, r.Line, r.Text))
					hits = append(hits, Hit{Path: r.Path, Line: r.Line, Kind: "text", Name: symbol, Source: "rg", Text: jscompat.UTF16Prefix(r.Text, 200)})
				}
				out = append(out, "```")
			} else {
				out = append(out, "_No literal reference to this symbol anywhere in the repo._")
				degraded = append(degraded, "rg:no-hits")
			}
		}
	}

	text, degraded := capText(strings.Join(out, "\n"), degraded)
	return Report{Text: text, Degraded: nonNil(degraded), Terms: []string{symbol}, Hits: capHits(hits, 40), Codegraph: codegraph}
}

// DescribeEngines is the status block of `nxy locate --status`: which engines the scout would have.
// info is nil when there is no repo map yet.
func DescribeEngines(info *IndexInfo, rgFound bool, codegraphLine string) string {
	repoMap := "not built — it builds itself on the first /nxy:locate"
	if info != nil {
		age := strconv.FormatInt(int64(jscompat.Round(info.AgeMs/1000)), 10)
		repoMap = fmt.Sprintf("%d files, %d symbols (built %ss ago)", info.Files, info.Symbols, age)
	}
	rg := "not found (the scout uses the index only)"
	if rgFound {
		rg = "available"
	}
	return strings.Join([]string{"repo map:     " + repoMap, "ripgrep (rg): " + rg, codegraphLine}, "\n")
}
