package report

import (
	"bytes"
	"encoding/json"
	"math"
	"sort"

	"github.com/mananos/nxy.dev/v2/internal/jscompat"
)

// KindRow is the filter activity of one command kind. Kind is the raw JSON value of the row:
// jscompat.Undefined (a row without command_kind) leaves the key out of the JSON, as JS does.
type KindRow struct {
	Kind   any
	Rows   int
	RTK    int
	Chars  float64
	Tokens float64
}

// MarshalJSON writes {kind, rows, rtk, chars, tokens} without kind when it is undefined.
func (k KindRow) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	if !jscompat.IsUndefined(k.Kind) {
		kj, err := json.Marshal(k.Kind)
		if err != nil {
			return nil, err
		}
		b.WriteString(`"kind":`)
		b.Write(kj)
		b.WriteByte(',')
	}
	rest, err := json.Marshal(struct {
		Rows   int     `json:"rows"`
		RTK    int     `json:"rtk"`
		Chars  float64 `json:"chars"`
		Tokens float64 `json:"tokens"`
	}{k.Rows, k.RTK, k.Chars, k.Tokens})
	if err != nil {
		return nil, err
	}
	b.Write(rest[1:])
	return b.Bytes(), nil
}

// SkipReason is one [reason, count] entry (Object.entries of the skip counters).
type SkipReason struct {
	Reason string
	Count  float64
}

// MarshalJSON writes the pair as a two-element array.
func (s SkipReason) MarshalJSON() ([]byte, error) { return json.Marshal([]any{s.Reason, s.Count}) }

// FilterSummary is summarizeFilter of entries/stats.mjs.
type FilterSummary struct {
	Rows          int          `json:"rows"`
	ByEngine      Counts       `json:"byEngine"`
	SkipReasons   []SkipReason `json:"skipReasons"`
	FilteredChars float64      `json:"filteredChars"`
	EstTokens     float64      `json:"estTokens"`
	ByKind        []KindRow    `json:"byKind"`
}

// numOr0 is `v || 0` for a value that is added to a number (a decoded number; other types add
// nothing here, where JS would concatenate strings).
func numOr0(v any) float64 {
	f, ok := v.(float64)
	if !ok || math.IsNaN(f) {
		return 0
	}
	return f
}

// FilterGroup is the filter activity of one (engine, reason, kind) key: the rows of filter.jsonl
// folded to counts. Engine is the stringified engine; Reason is only kept when Engine is "skip";
// Kind is the raw command_kind (jscompat.Undefined when the row has none).
type FilterGroup struct {
	Engine string
	Reason string
	Kind   any
	Rows   int
	Chars  float64
	Tokens float64
}

func (g FilterGroup) key() [3]string {
	return [3]string{g.Engine, g.Reason, jscompat.String(g.Kind)}
}

// GroupFilterRows folds rows into groups, in order of first appearance.
func GroupFilterRows(rows []any) []FilterGroup {
	var gs []FilterGroup
	idx := map[[3]string]int{}
	for _, r := range rows {
		engine := jscompat.Get(r, "engine")
		g := FilterGroup{
			Engine: jscompat.String(engine),
			Kind:   jscompat.Get(r, "command_kind"),
			Rows:   1,
			Chars:  numOr0(jscompat.Get(r, "filtered_chars")),
			Tokens: numOr0(jscompat.Get(r, "est_tokens")),
		}
		if engine == "skip" {
			g.Reason = jscompat.String(jscompat.Get(r, "reason"))
		}
		gs = addGroup(gs, idx, g)
	}
	return gs
}

func addGroup(gs []FilterGroup, idx map[[3]string]int, g FilterGroup) []FilterGroup {
	k := g.key()
	if i, ok := idx[k]; ok {
		gs[i].Rows += g.Rows
		gs[i].Chars += g.Chars
		gs[i].Tokens += g.Tokens
		return gs
	}
	idx[k] = len(gs)
	return append(gs, g)
}

// MergeFilterGroups adds the groups of next to those of old by key; new keys go at the end.
func MergeFilterGroups(old, next []FilterGroup) []FilterGroup {
	out := append([]FilterGroup{}, old...)
	idx := map[[3]string]int{}
	for i, g := range out {
		idx[g.key()] = i
	}
	for _, g := range next {
		out = addGroup(out, idx, g)
	}
	return out
}

// FilterGroupsRows is the number of rows the groups stand for.
func FilterGroupsRows(gs []FilterGroup) int {
	n := 0
	for _, g := range gs {
		n += g.Rows
	}
	return n
}

// FilterGroupsRTK is the number of rows whose engine is rtk.
func FilterGroupsRTK(gs []FilterGroup) int {
	n := 0
	for _, g := range gs {
		if g.Engine == "rtk" {
			n += g.Rows
		}
	}
	return n
}

// SummarizeFilter folds the rows of filter.jsonl: counts by engine and skip reason, characters
// and estimated tokens, and the per-kind table sorted by characters (stable).
func SummarizeFilter(rows []any) FilterSummary {
	return SummarizeFilterGroups(GroupFilterRows(rows))
}

// SummarizeFilterGroups is SummarizeFilter over rows already folded to groups.
func SummarizeFilterGroups(gs []FilterGroup) FilterSummary {
	s := FilterSummary{SkipReasons: []SkipReason{}}
	var skip Counts
	kindIdx := map[string]int{}
	kinds := []KindRow{}
	for _, g := range gs {
		n := float64(g.Rows)
		s.ByEngine.Add(g.Engine, n)
		if g.Engine == "skip" {
			skip.Add(g.Reason, n)
		}
		s.FilteredChars += g.Chars
		s.EstTokens += g.Tokens
		key := jscompat.String(g.Kind)
		i, ok := kindIdx[key]
		if !ok {
			i = len(kinds)
			kindIdx[key] = i
			kinds = append(kinds, KindRow{Kind: g.Kind})
		}
		k := &kinds[i]
		k.Rows += g.Rows
		if g.Engine == "rtk" {
			k.RTK += g.Rows
		}
		k.Chars += g.Chars
		k.Tokens += g.Tokens
	}
	s.Rows = FilterGroupsRows(gs)
	for _, key := range skip.Keys() {
		s.SkipReasons = append(s.SkipReasons, SkipReason{key, skip.Get(key)})
	}
	sort.SliceStable(s.SkipReasons, func(a, b int) bool { return s.SkipReasons[a].Count > s.SkipReasons[b].Count })
	sort.SliceStable(kinds, func(a, b int) bool { return kinds[a].Chars > kinds[b].Chars })
	s.ByKind = kinds
	return s
}
