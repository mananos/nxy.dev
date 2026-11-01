package format

import (
	"math"
	"testing"
	"time"
	_ "time/tzdata" // zones load on Windows CI too

	"github.com/mananos/nxy.dev/internal/goldentest"
	"github.com/mananos/nxy.dev/internal/metrics"
)

func forFile(t *testing.T, file string, fn func(t *testing.T, c goldentest.Case)) {
	t.Helper()
	n := 0
	for _, c := range goldentest.Load(t, "format") {
		if c.File != file {
			continue
		}
		n++
		t.Run(c.Name, func(t *testing.T) { fn(t, c) })
	}
	if n == 0 {
		t.Fatalf("no cases in %s", file)
	}
}

// nan decodes a JSON number where null is the NaN the callers pass.
func nan(p *float64) float64 {
	if p == nil {
		return math.NaN()
	}
	return *p
}

func numIn(t *testing.T, c goldentest.Case, key string) float64 {
	t.Helper()
	var x *float64
	c.In(t, key, &x)
	return nan(x)
}

func unary(file string, fn func(float64) string) func(t *testing.T) {
	return func(t *testing.T) {
		forFile(t, file, func(t *testing.T, c goldentest.Case) {
			goldentest.Check(t, c, map[string]any{"s": fn(numIn(t, c, "x"))})
		})
	}
}

func TestGoldenTokens(t *testing.T)   { unary("tokens.json", FmtTokens)(t) }
func TestGoldenUsd(t *testing.T)      { unary("usd.json", FmtUsd)(t) }
func TestGoldenRatio(t *testing.T)    { unary("ratio.json", FmtRatio)(t) }
func TestGoldenDuration(t *testing.T) { unary("duration.json", FmtDuration)(t) }

func TestGoldenPct(t *testing.T) {
	forFile(t, "pct.json", func(t *testing.T, c goldentest.Case) {
		var digits []int
		c.In(t, "digits", &digits)
		x := numIn(t, c, "x")
		got := make([]string, len(digits))
		for i, d := range digits {
			got[i] = FmtPct(x, d)
		}
		goldentest.Check(t, c, map[string]any{"s": got})
	})
}

func TestGoldenUsdPartial(t *testing.T) {
	forFile(t, "usdpartial.json", func(t *testing.T, c goldentest.Case) {
		var flags []struct{ Partial, Estimated bool }
		c.In(t, "flags", &flags)
		x := numIn(t, c, "x")
		got := make([]string, len(flags))
		for i, f := range flags {
			got[i] = FmtUsdPartial(x, f.Partial, f.Estimated)
		}
		goldentest.Check(t, c, map[string]any{"s": got})
	})
}

func TestGoldenEstimatedNote(t *testing.T) {
	forFile(t, "estnote.json", func(t *testing.T, c goldentest.Case) {
		var pairs [][2]string
		c.In(t, "pairs", &pairs)
		var mb metrics.ModelBasis
		for _, p := range pairs {
			mb.Set(p[0], p[1])
		}
		goldentest.Check(t, c, map[string]any{"s": EstimatedNote(mb)})
	})
}

func TestGoldenGauge(t *testing.T) {
	forFile(t, "gauge.json", func(t *testing.T, c goldentest.Case) {
		var cells int
		c.In(t, "cells", &cells)
		goldentest.Check(t, c, map[string]any{"s": Gauge(numIn(t, c, "x"), cells)})
	})
}

func TestGoldenColorize(t *testing.T) {
	forFile(t, "colorize.json", func(t *testing.T, c goldentest.Case) {
		var style string // null is the empty style
		c.In(t, "style", &style)
		goldentest.Check(t, c, map[string]any{"on": Colorize("x", style, true), "off": Colorize("x", style, false)})
	})
}

func TestGoldenSeverity(t *testing.T) {
	forFile(t, "severity.json", func(t *testing.T, c goldentest.Case) {
		goldentest.Check(t, c, map[string]any{"s": Severity(numIn(t, c, "v"), numIn(t, c, "warn"), numIn(t, c, "crit"))})
	})
}

func TestGoldenTable(t *testing.T) {
	forFile(t, "table.json", func(t *testing.T, c goldentest.Case) {
		var in []struct {
			Label string
			Align string
		}
		var rows [][]string
		c.In(t, "cols", &in)
		c.In(t, "rows", &rows)
		cols := make([]Col, len(in))
		for i, x := range in {
			cols[i] = Col{Label: x.Label, Right: x.Align == "right"}
		}
		goldentest.Check(t, c, map[string]any{"s": Table(cols, rows)})
	})
}

func TestGoldenClip(t *testing.T) {
	forFile(t, "clip.json", func(t *testing.T, c goldentest.Case) {
		var s string
		var max int
		c.In(t, "s", &s)
		c.In(t, "max", &max)
		goldentest.Check(t, c, map[string]any{"s": Clip(s, max)})
	})
}

func zoneOf(t *testing.T, c goldentest.Case) *time.Location {
	t.Helper()
	var zone string
	c.In(t, "zone", &zone)
	loc, err := time.LoadLocation(zone)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestGoldenDate(t *testing.T) {
	forFile(t, "date.json", func(t *testing.T, c goldentest.Case) {
		loc := zoneOf(t, c)
		var ts []*float64
		c.In(t, "ts", &ts)
		full := make([]string, len(ts))
		dateOnly := make([]string, len(ts))
		for i, p := range ts {
			full[i] = FmtDate(nan(p), loc, true)
			dateOnly[i] = FmtDate(nan(p), loc, false)
		}
		goldentest.Check(t, c, map[string]any{"full": full, "dateOnly": dateOnly})
	})
}

func TestGoldenSince(t *testing.T) {
	forFile(t, "since.json", func(t *testing.T, c goldentest.Case) {
		loc := zoneOf(t, c)
		var now int64
		var values []*string
		c.In(t, "now", &now)
		c.In(t, "values", &values)
		got := make([]any, len(values))
		for i, p := range values {
			s := ""
			if p != nil {
				s = *p
			}
			if ms, ok := ParseSince(s, now, loc); ok {
				got[i] = ms
			}
		}
		goldentest.Check(t, c, map[string]any{"ms": got})
	})
}

func TestGoldenArgs(t *testing.T) {
	forFile(t, "args.json", func(t *testing.T, c goldentest.Case) {
		var argv []string
		c.In(t, "argv", &argv)
		a := ParseArgs(argv)
		opts := map[string]any{}
		for k, o := range a.Opts {
			if o.IsFlag {
				opts[k] = true
				if !a.Flag(k) {
					t.Errorf("Flag(%q) false for a bare flag", k)
				}
				if _, ok := a.Str(k); ok {
					t.Errorf("Str(%q) ok for a bare flag", k)
				}
			} else {
				opts[k] = o.Value
				if s, ok := a.Str(k); !ok || s != o.Value || a.Flag(k) != (o.Value != "") {
					t.Errorf("Str/Flag(%q) disagree with the value %q", k, o.Value)
				}
			}
		}
		pos := a.Positional
		if pos == nil {
			pos = []string{}
		}
		goldentest.Check(t, c, map[string]any{"opts": opts, "positional": pos})
	})
}
