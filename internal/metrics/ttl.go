package metrics

import (
	"math"

	"github.com/mananos/nxy.dev/internal/pricing"
)

const (
	fiveMinutes int64 = 300_000
	oneHour     int64 = 3_600_000
)

// TTLWhatIf is what a thread would have cost with a one-hour prompt cache (core/metrics/cache-ttl.mjs).
type TTLWhatIf struct {
	ActualUSD       float64 `json:"actualUsd"`
	HourUSD         float64 `json:"hourUsd"`
	Writes5m        int     `json:"writes5m"`
	RebuildsAvoided int     `json:"rebuildsAvoided"`
}

// HourTTLWhatIf works out the one-hour-cache cost of one thread's calls, in order.
func HourTTLWhatIf(calls []Call) TTLWhatIf {
	var out TTLWhatIf
	var prevContext float64
	havePrev := false
	for _, call := range calls {
		u := call.Usage
		context := pricing.TotalInput(u)
		actual, ok := pricing.CostFor(call.Model, u, nil)
		if ok {
			hour := actual
			if u.CacheWrite5m > 0 {
				out.Writes5m++
				reread := 0.0
				if havePrev && call.GapMs != nil && *call.GapMs >= fiveMinutes && *call.GapMs < oneHour && !(call.After != nil && *call.After == "compact") {
					// The prefix the previous call sent is what a live cache would have served.
					reread = math.Max(0, math.Min(u.CacheWrite5m, math.Min(prevContext, context)-u.CacheRead))
					if reread > 0 {
						out.RebuildsAvoided++
					}
				}
				w := u
				w.CacheWrite5m = 0
				w.CacheWrite1h = u.CacheWrite1h + u.CacheWrite5m - reread
				w.CacheRead = u.CacheRead + reread
				if h, hok := pricing.CostFor(call.Model, w, nil); hok {
					hour = h
				} else {
					hour = actual
				}
			}
			out.ActualUSD += actual
			out.HourUSD += hour
		}
		prevContext = context
		havePrev = true
	}
	return out
}

// SumTTLWhatIf adds several threads' results (a trend over many sessions).
func SumTTLWhatIf(parts []TTLWhatIf) TTLWhatIf {
	var out TTLWhatIf
	for _, p := range parts {
		out.ActualUSD += p.ActualUSD
		out.HourUSD += p.HourUSD
		out.Writes5m += p.Writes5m
		out.RebuildsAvoided += p.RebuildsAvoided
	}
	return out
}
