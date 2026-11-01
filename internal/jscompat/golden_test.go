package jscompat

import (
	"sort"
	"testing"

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
