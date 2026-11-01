package jscompat

import (
	"math"
	"sort"
	"testing"
	"time"
	_ "time/tzdata" // zones load on Windows CI too

	"github.com/mananos/nxy.dev/internal/goldentest"
)

// revive turns {"$undefined": true} (how the corpus writes JS undefined) into Undefined.
func revive(v any) any {
	if m, ok := v.(map[string]any); ok && m["$undefined"] == true {
		return Undefined
	}
	return v
}

func orNull(v any) any {
	if IsUndefined(v) {
		return nil
	}
	return v
}

func forFile(t *testing.T, file string, fn func(t *testing.T, c goldentest.Case)) {
	t.Helper()
	n := 0
	for _, c := range goldentest.Load(t, "jscompat") {
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

func TestGoldenDate(t *testing.T) {
	forFile(t, "date.json", func(t *testing.T, c goldentest.Case) {
		var v any
		c.In(t, "value", &v)
		var ms any
		if n, ok := ParseDate(v); ok {
			ms = n
		}
		goldentest.Check(t, c, map[string]any{"ms": ms})
	})
}

func TestGoldenValue(t *testing.T) {
	forFile(t, "value.json", func(t *testing.T, c goldentest.Case) {
		var v any = Undefined
		if !c.Has("undefined") {
			c.In(t, "value", &v)
		}
		var num any
		if f, ok := Num(v); ok {
			num = f
		}
		goldentest.Check(t, c, map[string]any{"string": String(v), "truthy": Truthy(v), "num": num})
	})
}

func TestGoldenGet(t *testing.T) {
	forFile(t, "get.json", func(t *testing.T, c goldentest.Case) {
		var obj any
		var keys []string
		c.In(t, "obj", &obj)
		c.In(t, "keys", &keys)
		r := Get(obj, keys...)
		goldentest.Check(t, c, map[string]any{"undefined": IsUndefined(r), "string": String(r), "value": orNull(r)})
	})
}

func TestGoldenOr(t *testing.T) {
	forFile(t, "or.json", func(t *testing.T, c goldentest.Case) {
		var items []any
		c.In(t, "items", &items)
		for i := range items {
			items[i] = revive(items[i])
		}
		r := Or(items...)
		goldentest.Check(t, c, map[string]any{"undefined": IsUndefined(r), "value": orNull(r)})
	})
}

func TestGoldenText(t *testing.T) {
	forFile(t, "text.json", func(t *testing.T, c goldentest.Case) {
		var s string
		var cuts []int
		c.In(t, "s", &s)
		c.In(t, "cuts", &cuts)
		prefixes := make([]string, len(cuts))
		for i, n := range cuts {
			prefixes[i] = UTF16Prefix(s, n)
		}
		goldentest.Check(t, c, map[string]any{"length": UTF16Len(s), "prefixes": prefixes})
	})
}

func TestGoldenReplace(t *testing.T) {
	forFile(t, "replace.json", func(t *testing.T, c goldentest.Case) {
		var s string
		c.In(t, "s", &s)
		slug := ReplaceUnits(s, func(u rune) bool {
			return u >= 'a' && u <= 'z' || u >= 'A' && u <= 'Z' || u >= '0' && u <= '9'
		}, "-")
		goldentest.Check(t, c, map[string]any{"slug": slug})
	})
}

// numIn reads a corpus number: null is NaN and the strings "Infinity"/"-Infinity" are the infinities.
func numIn(v any) float64 {
	switch x := v.(type) {
	case nil:
		return math.NaN()
	case string:
		if x == "-Infinity" {
			return math.Inf(-1)
		}
		return math.Inf(1)
	}
	return v.(float64)
}

func TestGoldenToFixed(t *testing.T) {
	forFile(t, "tofixed.json", func(t *testing.T, c goldentest.Case) {
		var x any
		var digits []int
		c.In(t, "x", &x)
		c.In(t, "digits", &digits)
		out := make([]string, len(digits))
		for i, d := range digits {
			out[i] = ToFixed(numIn(x), d)
		}
		goldentest.Check(t, c, map[string]any{"s": out})
	})
}

func TestGoldenRound(t *testing.T) {
	forFile(t, "round.json", func(t *testing.T, c goldentest.Case) {
		var x any
		c.In(t, "x", &x)
		r := Round(numIn(x))
		goldentest.Check(t, c, map[string]any{"s": formatNumber(r), "negzero": r == 0 && math.Signbit(r)})
	})
}

func TestGoldenToNumber(t *testing.T) {
	forFile(t, "tonumber.json", func(t *testing.T, c goldentest.Case) {
		var v any
		c.In(t, "value", &v)
		goldentest.Check(t, c, map[string]any{"s": formatNumber(ToNumber(v))})
	})
}

func TestGoldenPad(t *testing.T) {
	forFile(t, "pad.json", func(t *testing.T, c goldentest.Case) {
		var s, fill string
		var n int
		c.In(t, "s", &s)
		c.In(t, "n", &n)
		c.In(t, "fill", &fill)
		goldentest.Check(t, c, map[string]any{"start": PadStart(s, n, fill), "end": PadEnd(s, n, fill)})
	})
}

func TestGoldenSpace(t *testing.T) {
	forFile(t, "space.json", func(t *testing.T, c goldentest.Case) {
		var s string
		c.In(t, "s", &s)
		goldentest.Check(t, c, map[string]any{"collapsed": CollapseSpace(s), "trim": Trim(s), "trimEnd": TrimEnd(s)})
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

func TestGoldenLocalDate(t *testing.T) {
	forFile(t, "localdate.json", func(t *testing.T, c goldentest.Case) {
		loc := zoneOf(t, c)
		var ms []any
		c.In(t, "ms", &ms)
		parts := make([]any, len(ms))
		for i, m := range ms {
			if y, mon, d, h, min, wd, ok := LocalTime(numIn(m), loc); ok {
				parts[i] = map[string]any{"y": y, "mon": mon, "d": d, "h": h, "min": min, "wd": wd}
			}
		}
		goldentest.Check(t, c, map[string]any{"parts": parts})
	})
}

func TestGoldenParseLocal(t *testing.T) {
	forFile(t, "parselocal.json", func(t *testing.T, c goldentest.Case) {
		loc := zoneOf(t, c)
		var values []string
		c.In(t, "values", &values)
		out := make([]any, len(values))
		for i, s := range values {
			if n, ok := ParseDateIn(s, loc); ok {
				out[i] = n
			}
		}
		goldentest.Check(t, c, map[string]any{"ms": out})
	})
}

func TestGoldenStringify(t *testing.T) {
	forFile(t, "stringify.json", func(t *testing.T, c goldentest.Case) {
		var raw, indent string
		c.In(t, "raw", &raw)
		c.In(t, "indent", &indent)
		got, err := Restringify([]byte(raw), indent)
		if err != nil {
			t.Fatal(err)
		}
		goldentest.Check(t, c, map[string]any{"text": string(got)})
	})
}

func TestToFixedNegativeZeroAndRound(t *testing.T) {
	if got := ToFixed(math.Copysign(0, -1), 2); got != "0.00" {
		t.Errorf("ToFixed(-0) = %q", got)
	}
	if got := ToFixed(-0.001, 2); got != "-0.00" {
		t.Errorf("ToFixed(-0.001) = %q", got)
	}
	if r := Round(math.Copysign(0, -1)); !math.Signbit(r) {
		t.Error("Round(-0) lost its sign")
	}
	if !math.IsNaN(ToNumber([]any{})) || ToNumber(nil) != 0 {
		t.Error("ToNumber of non-scalars")
	}
}

func TestRestringifyRejectsBadInput(t *testing.T) {
	for _, raw := range []string{"", "{", "[1,]", "{} {}", "nope"} {
		if _, err := Restringify([]byte(raw), "  "); err == nil {
			t.Errorf("Restringify(%q) did not fail", raw)
		}
	}
}

func TestGoldenCollate(t *testing.T) {
	forFile(t, "collate.json", func(t *testing.T, c goldentest.Case) {
		if c.Has("list") {
			var list []string
			c.In(t, "list", &list)
			sorted := append([]string(nil), list...)
			sort.SliceStable(sorted, func(i, j int) bool { return LocaleCompare(sorted[i], sorted[j]) < 0 })
			goldentest.Check(t, c, map[string]any{"sorted": sorted})
			return
		}
		var names []string
		c.In(t, "names", &names)
		var signs []int
		for _, a := range names {
			for _, b := range names {
				signs = append(signs, LocaleCompare(a, b))
			}
		}
		goldentest.Check(t, c, map[string]any{"signs": signs})
	})
}
