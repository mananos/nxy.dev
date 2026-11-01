package report

import (
	"bytes"
	"encoding/json"
	"math"
)

// Counts is a JS object of counters: keys keep their insertion order (the callers hand the JSON
// to jscompat.Restringify, which puts integer-like keys first as JS does).
type Counts struct {
	keys []string
	m    map[string]float64
}

// Add adds n to key, appending the key when it is new.
func (c *Counts) Add(key string, n float64) {
	if c.m == nil {
		c.m = map[string]float64{}
	}
	if _, ok := c.m[key]; !ok {
		c.keys = append(c.keys, key)
	}
	c.m[key] += n
}

// Keys is the key order.
func (c Counts) Keys() []string { return append([]string{}, c.keys...) }

// Get is the count of key (0 when absent).
func (c Counts) Get(key string) float64 { return c.m[key] }

// Len is the number of keys.
func (c Counts) Len() int { return len(c.keys) }

// MarshalJSON writes the object in insertion order.
func (c Counts) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range c.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kj, _ := json.Marshal(k)
		vj, err := json.Marshal(c.m[k])
		if err != nil {
			return nil, err
		}
		b.Write(kj)
		b.WriteByte(':')
		b.Write(vj)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// BroadRow is one broad search of the main thread made at or above the gate threshold.
type BroadRow struct {
	Ts            int64
	Tool          string
	Kind          string
	ContextTokens float64
}

// BroadSummary is what SummarizeBroadSearches returns. Threshold is nil when there is none.
type BroadSummary struct {
	Count         int      `json:"count"`
	ByKind        Counts   `json:"byKind"`
	ContextTokens float64  `json:"contextTokens"`
	Threshold     *float64 `json:"threshold"`
}

// SummarizeBroadSearches is summarizeBroadSearches of hosts/claude-code/search-history.mjs:
// PowerShell counts as Bash; a threshold that is not above zero (or NaN) gives an empty summary.
func SummarizeBroadSearches(rows []BroadRow, threshold float64) BroadSummary {
	if !(threshold > 0) || math.IsNaN(threshold) {
		return BroadSummary{}
	}
	var s BroadSummary
	for _, r := range rows {
		k := r.Tool
		if k == "PowerShell" {
			k = "Bash"
		}
		s.ByKind.Add(k, 1)
		s.ContextTokens += r.ContextTokens
	}
	s.Count = len(rows)
	t := threshold
	s.Threshold = &t
	return s
}
