package repomap

import (
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/goldentest"
	"github.com/mananos/nxy.dev/v2/internal/jscompat"
)

func TestExplicitIFlagIsRegex(t *testing.T) {
	mapText := "a.go\t1\tfunc\tAppRun\tfunc AppRun()\nb.go\t2\tfunc\tother\tfunc other()\n"
	for _, q := range []string{"/^app/i", "/^APP/gi"} {
		r := Query(mapText, q, Opts{})
		if len(r.Rows) != 1 || r.Rows[0].Name != "AppRun" {
			t.Errorf("%s: rows = %+v, want only AppRun", q, r.Rows)
		}
	}
	if re := compileQuery("/^app/ii"); re != nil {
		t.Error("a flag repeated by the user must still mean substring")
	}
}

func kinds(rs []Rule) any {
	if rs == nil {
		return nil
	}
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Kind
	}
	return out
}

func TestGolden(t *testing.T) {
	for _, c := range goldentest.Load(t, "repomap") {
		t.Run(c.File+"/"+c.Name, func(t *testing.T) {
			switch c.File {
			case "build.json", "info.json", "noindex.json":
				t.Skip("filesystem cases: internal/platform/repomap_golden_test.go")
			case "ext.json":
				if c.Name == "indexed" {
					goldentest.Check(t, c, map[string]any{"extensions": IndexedExtensions()})
					return
				}
				var name string
				c.In(t, "name", &name)
				ext := Ext(name)
				goldentest.Check(t, c, map[string]any{"ext": ext, "kinds": kinds(RulesFor(ext))})
			case "symbols.json":
				var ext, text string
				c.In(t, "ext", &ext)
				c.In(t, "text", &text)
				rs := RulesFor(ext)
				if rs == nil {
					t.Fatalf("no rules for %s", ext)
				}
				out := []any{}
				for _, s := range ExtractSymbols(text, rs) {
					out = append(out, map[string]any{"line": s.Line, "kind": s.Kind, "name": s.Name, "sig": s.Sig})
				}
				goldentest.Check(t, c, map[string]any{"symbols": out})
			case "ignore.json":
				var in struct {
					Text   string
					Probes []struct {
						Rel   string `json:"rel"`
						Name  string `json:"name"`
						IsDir bool   `json:"isDir"`
					}
				}
				c.In(t, "text", &in.Text)
				c.In(t, "probes", &in.Probes)
				m := Matcher(in.Text)
				got := []bool{}
				for _, p := range in.Probes {
					got = append(got, m(p.Rel, p.Name, p.IsDir))
				}
				goldentest.Check(t, c, map[string]any{"ignored": got})
			case "query.json":
				var mapText, query string
				var opts struct {
					Limit *int   `json:"limit"`
					Kind  string `json:"kind"`
					Path  string `json:"path"`
				}
				c.In(t, "map", &mapText)
				c.In(t, "query", &query)
				c.In(t, "opts", &opts)
				if m := regexQuery.FindStringSubmatch(jscompat.Trim(query)); m != nil && strings.Contains(m[2], "i") {
					t.Skip("explicit i flag: the 1.x doubled it and fell back to substring; here it is a regex (TestExplicitIFlagIsRegex)")
				}
				r := Query(mapText, query, Opts{Limit: opts.Limit, Kind: opts.Kind, Path: opts.Path})
				rows := []any{}
				for _, x := range r.Rows {
					rows = append(rows, map[string]any{"path": x.Path, "line": x.Line, "kind": x.Kind, "name": x.Name, "sig": x.Sig})
				}
				goldentest.Check(t, c, map[string]any{"ok": r.OK, "reason": r.Reason, "rows": rows, "scanned": r.Scanned, "truncated": r.Truncated})
			default:
				t.Fatalf("unknown golden file %s", c.File)
			}
		})
	}
}
