package metrics

import (
	"bytes"
	"encoding/json"

	"github.com/mananos/nxy.dev/v2/internal/pricing"
)

// BreakCauses are the causes of a cache rebuild, in report order.
var BreakCauses = []string{"idle", "subagent", "compact", "start", "other"}

// ttlMsOf is the cache lifetime a call wrote with: 1h if it wrote any 1h tokens, else 5 minutes.
func ttlMsOf(u pricing.Usage) int64 {
	if u.CacheWrite1h > 0 {
		return oneHour
	}
	return fiveMinutes
}

// BreakCause says why a call had to rebuild the cache: compact, start (first call), idle or
// subagent (the gap outlived the TTL) or other (prefix changed with the cache still alive).
func BreakCause(call Call) string {
	if call.After != nil && *call.After == "compact" {
		return "compact"
	}
	if call.GapMs == nil {
		return "start"
	}
	if *call.GapMs >= ttlMsOf(call.Usage) {
		if call.After != nil && *call.After == "subagent" {
			return "subagent"
		}
		return "idle"
	}
	return "other"
}

// Break is one cache rebuild above the threshold.
type Break struct {
	Ts        int64    `json:"ts"`
	Uncached  float64  `json:"uncached"`
	Total     float64  `json:"total"`
	AgentType *string  `json:"agentType"`
	Cause     string   `json:"cause"`
	TTL       string   `json:"ttl"`
	USD       *float64 `json:"usd"`
}

// CauseTotals sums the breaks of one cause.
type CauseTotals struct {
	Count  int     `json:"count"`
	Tokens float64 `json:"tokens"`
	USD    float64 `json:"usd"`
}

// TTLCounts counts breaks by the TTL they wrote with.
type TTLCounts struct {
	FiveMin int `json:"5m"`
	Hour    int `json:"1h"`
}

// BreakSummary groups rebuilds by cause. AvoidableUSD (idle + subagent) is the ceiling of what
// keeping the cache warm could have saved.
type BreakSummary struct {
	ByCause      map[string]*CauseTotals `json:"byCause"`
	TTL          TTLCounts               `json:"ttl"`
	AvoidableUSD float64                 `json:"avoidableUsd"`
}

// SummarizeBreaks groups breaks by cause; an unknown cause counts as other.
func SummarizeBreaks(breaks []Break) BreakSummary {
	by := map[string]*CauseTotals{}
	for _, c := range BreakCauses {
		by[c] = &CauseTotals{}
	}
	var ttl TTLCounts
	for _, b := range breaks {
		g, ok := by[b.Cause]
		if !ok {
			g = by["other"]
		}
		g.Count++
		g.Tokens += b.Uncached
		if b.USD != nil {
			g.USD += *b.USD
		}
		if b.TTL == "1h" {
			ttl.Hour++
		} else {
			ttl.FiveMin++
		}
	}
	return BreakSummary{ByCause: by, TTL: ttl, AvoidableUSD: by["idle"].USD + by["subagent"].USD}
}

// MarshalJSON writes byCause in BreakCauses order (a JS object), not the sorted order of a Go map.
func (s BreakSummary) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(`{"byCause":{`)
	first := true
	for _, c := range BreakCauses {
		g, ok := s.ByCause[c]
		if !ok {
			continue
		}
		raw, err := json.Marshal(g)
		if err != nil {
			return nil, err
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		k, _ := json.Marshal(c)
		b.Write(k)
		b.WriteByte(':')
		b.Write(raw)
	}
	b.WriteString(`},"ttl":`)
	ttl, err := json.Marshal(s.TTL)
	if err != nil {
		return nil, err
	}
	b.Write(ttl)
	b.WriteString(`,"avoidableUsd":`)
	avoid, err := json.Marshal(s.AvoidableUSD)
	if err != nil {
		return nil, err
	}
	b.Write(avoid)
	b.WriteByte('}')
	return b.Bytes(), nil
}
