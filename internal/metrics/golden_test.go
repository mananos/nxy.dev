package metrics

import (
	"testing"

	"github.com/mananos/nxy.dev/internal/goldentest"
)

func TestGoldenBuckets(t *testing.T) {
	for _, c := range goldentest.Load(t, "metrics") {
		if c.File != "buckets.json" {
			continue
		}
		t.Run(c.Name, func(t *testing.T) {
			var calls []Call
			c.In(t, "calls", &calls)
			b := NewBucket()
			for _, call := range calls {
				b.Add(call)
			}
			goldentest.Check(t, c, map[string]any{"bucket": b.Finish()})
		})
	}
}

func TestGoldenTTL(t *testing.T) {
	for _, c := range goldentest.Load(t, "metrics") {
		if c.File != "ttl.json" {
			continue
		}
		t.Run(c.Name, func(t *testing.T) {
			if c.Has("parts") {
				var parts []TTLWhatIf
				c.In(t, "parts", &parts)
				goldentest.Check(t, c, map[string]any{"sum": SumTTLWhatIf(parts)})
				return
			}
			var calls []Call
			c.In(t, "calls", &calls)
			goldentest.Check(t, c, map[string]any{"whatIf": HourTTLWhatIf(calls)})
		})
	}
}

func TestGoldenBreaks(t *testing.T) {
	for _, c := range goldentest.Load(t, "metrics") {
		if c.File != "breaks.json" {
			continue
		}
		t.Run(c.Name, func(t *testing.T) {
			if c.Has("breaks") {
				var breaks []Break
				c.In(t, "breaks", &breaks)
				goldentest.Check(t, c, map[string]any{"summary": SummarizeBreaks(breaks)})
				return
			}
			var call Call
			c.In(t, "call", &call)
			goldentest.Check(t, c, map[string]any{"cause": BreakCause(call)})
		})
	}
}

func TestGoldenTiming(t *testing.T) {
	for _, c := range goldentest.Load(t, "metrics") {
		if c.File != "timing.json" {
			continue
		}
		t.Run(c.Name, func(t *testing.T) {
			switch {
			case c.Has("events"):
				var events []any
				c.In(t, "events", &events)
				goldentest.Check(t, c, map[string]any{"timing": AgentTiming(events)})
			case c.Has("runs"):
				var runs []Run
				c.In(t, "runs", &runs)
				goldentest.Check(t, c, map[string]any{"summary": SummarizeTiming(runs)})
			default:
				var sorted []int64
				var p float64
				c.In(t, "sorted", &sorted)
				c.In(t, "p", &p)
				goldentest.Check(t, c, map[string]any{"quantile": Quantile(sorted, p)})
			}
		})
	}
}
