package report

import (
	"bytes"
	"encoding/json"
	"math"
	"sort"

	"github.com/mananos/nxy.dev/internal/jscompat"
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

// SummarizeFilter folds the rows of filter.jsonl: counts by engine and skip reason, characters
// and estimated tokens, and the per-kind table sorted by characters (stable).
func SummarizeFilter(rows []any) FilterSummary {
	s := FilterSummary{SkipReasons: []SkipReason{}}
	var skip Counts
	kindIdx := map[string]int{}
	kinds := []KindRow{}
	for _, r := range rows {
		engine := jscompat.Get(r, "engine")
		s.ByEngine.Add(jscompat.String(engine), 1)
		if engine == "skip" {
			skip.Add(jscompat.String(jscompat.Get(r, "reason")), 1)
		}
		chars := numOr0(jscompat.Get(r, "filtered_chars"))
		tokens := numOr0(jscompat.Get(r, "est_tokens"))
		s.FilteredChars += chars
		s.EstTokens += tokens
		kind := jscompat.Get(r, "command_kind")
		key := jscompat.String(kind)
		i, ok := kindIdx[key]
		if !ok {
			i = len(kinds)
			kindIdx[key] = i
			kinds = append(kinds, KindRow{Kind: kind})
		}
		k := &kinds[i]
		k.Rows++
		if engine == "rtk" {
			k.RTK++
		}
		k.Chars += chars
		k.Tokens += tokens
	}
	s.Rows = len(rows)
	for _, key := range skip.Keys() {
		s.SkipReasons = append(s.SkipReasons, SkipReason{key, skip.Get(key)})
	}
	sort.SliceStable(s.SkipReasons, func(a, b int) bool { return s.SkipReasons[a].Count > s.SkipReasons[b].Count })
	sort.SliceStable(kinds, func(a, b int) bool { return kinds[a].Chars > kinds[b].Chars })
	s.ByKind = kinds
	return s
}
