package memory

import (
	"strings"
	"testing"
	"time"
)

var renderNow = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

func daysAgo(n int) int64 { return renderNow.Add(-time.Duration(n) * 24 * time.Hour).UnixMilli() }

func TestRenderShowRulesAndLearnings(t *testing.T) {
	rules := []Rule{
		{Meta: Meta{CreatedAt: daysAgo(91)}, Short: "K1", Text: "use tabs", Question: "indent?", Origin: "manual", Deliveries: 2, Citations: 1},
		{Meta: Meta{CreatedAt: daysAgo(91)}, Short: "K2", Text: "no globals", Question: "state?", Origin: "plan", LastCitedAt: daysAgo(10)},
	}
	learnings := []Learning{
		{Short: "A1", Text: "a", Seen: 1},
		{Short: "A2", Text: "b", Seen: 3},
	}
	want := "Reglas (2)\n" +
		"K1 · use tabs\n   resuelve: indent? · manual · entregada 2 · citada 1 · para revisar\n" +
		"K2 · no globals\n   resuelve: state? · plan · entregada 0 · citada 0\n" +
		"\nAprendizajes (2)\n" +
		"A2 · b · visto 3 veces · candidato: nxy mem promote A2 --question \"...\"\n" +
		"A1 · a · visto 1 vez\n"
	if got := RenderShow(rules, learnings, renderNow); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderGlobal(t *testing.T) {
	if RenderGlobal(nil, nil, renderNow) != "" {
		t.Error("empty must render nothing")
	}
	rules := []Rule{
		{Meta: Meta{ID: "g1", CreatedAt: daysAgo(1)}, Short: "G1", Text: "answer short", Question: "how long?", Roles: []string{"planner", "reviewer"}, Deliveries: 3, Citations: 1},
		{Meta: Meta{ID: "g2", CreatedAt: daysAgo(1)}, Short: "G2", Text: "no emojis", Question: "emojis?"},
	}
	out := RenderGlobal(rules, []Learning{{Meta: Meta{ID: "l1"}, Short: "A9", Text: "prefer tabs"}}, renderNow)
	for _, want := range []string{"Reglas globales (2)", "G1 · answer short", "resuelve: how long? · roles: planner, reviewer · entregada 3 · citada 1", "roles: todos", "Aprendizajes globales (1)", `promover: nxy mem promote A9 --question "..."`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if got := RenderSearch([]Hit{{Kind: "rule", Short: "G1", Text: "x"}}); !strings.Contains(got, "global") {
		t.Errorf("search: %q", got)
	}
}

func TestRenderShowEmptyAndBudget(t *testing.T) {
	if got := RenderShow(nil, nil, renderNow); got != "todavía no hay reglas para este repo\n" {
		t.Fatalf("empty: %q", got)
	}
	long := strings.Repeat("x", 1600)
	rules := []Rule{
		{Meta: Meta{CreatedAt: daysAgo(1)}, Short: "K1", Text: long, Question: "q", Origin: "manual"},
		{Meta: Meta{CreatedAt: daysAgo(1)}, Short: "K2", Text: long, Question: "q", Origin: "manual"},
	}
	got := RenderShow(rules, nil, renderNow)
	if !strings.Contains(got, "demasiadas reglas para un despacho (3200 caracteres > 3000): revisalas\n") {
		t.Fatalf("no budget warning:\n%s", got)
	}
	if strings.Contains(RenderShow(rules[:1], nil, renderNow), "too many rules") {
		t.Fatal("warning below the budget")
	}
}

func TestRenderRelatedSearchNotes(t *testing.T) {
	if got := RenderRelated(nil, nil); got != "nothing related to those files\n" {
		t.Fatalf("related empty: %q", got)
	}
	got := RenderRelated([]ClosedFeature{{Meta: Meta{ID: "F1"}, Title: "add greet"}}, []Learning{{Short: "A4", Text: "x", Seen: 2}})
	want := "Closed features (1)\nF1 · add greet\n\nLearnings (1)\nA4 · x · seen 2 times\n"
	if got != want {
		t.Fatalf("related: %q", got)
	}
	if got := RenderSearch(nil); got != "sin resultados\n" {
		t.Fatalf("search empty: %q", got)
	}
	got = RenderSearch([]Hit{{Kind: "rule", Short: "K1", Repo: "r", Text: "a  b"}, {Kind: "feature", ID: "F9", Text: "t"}})
	if got != "rule · K1 · a b\nfeature · F9 · t\n" {
		t.Fatalf("search: %q", got)
	}
	notes := []Note{{Short: "N1", Title: "wip", Meta: Meta{UpdatedAt: daysAgo(0)}}, {Short: "N2", Title: "old", Meta: Meta{UpdatedAt: daysAgo(3)}}}
	if got := RenderNotes(notes, renderNow); got != "N1 · wip · hoy\nN2 · old · hace 3 días\n" {
		t.Fatalf("notes: %q", got)
	}
	if got := RenderNotes(nil, renderNow); got != "no hay notas para esta carpeta\n" {
		t.Fatalf("notes empty: %q", got)
	}
}

func TestRenderDiff(t *testing.T) {
	d := Diff{
		Rules: []Change[Rule]{
			{Kind: New, Row: Rule{Text: "r1"}, ReassignShort: true},
			{Kind: Skipped, Row: Rule{Text: "r2"}},
		},
		Learnings: []Change[Learning]{
			{Kind: Changed, Row: Learning{Text: "l1"}, Deleted: true},
			{Kind: Folded, Row: Learning{Text: "l2"}},
		},
	}
	want := "reglas: nuevos 1, cambiados 0, borrados 0, unificados 0, salteados 1\n" +
		"features: nuevos 0, cambiados 0, borrados 0, unificados 0, salteados 0\n" +
		"aprendizajes: nuevos 0, cambiados 0, borrados 1, unificados 1, salteados 0\n" +
		"notas: nuevos 0, cambiados 0, borrados 0, unificados 0, salteados 0\n" +
		"  regla nueva: r1 (referencia corta reasignada)\n" +
		"  aprendizaje cambiado (borrado): l1\n" +
		"  aprendizaje unificado: l2\n" +
		"simulacro: no escribí nada, corré con --apply\n"
	if got := RenderDiff(d, false); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if got := RenderDiff(d, true); !strings.HasSuffix(got, "importé 3 cambios\n") {
		t.Fatalf("apply: %q", got)
	}
	if got := RenderDiff(Diff{}, false); !strings.HasSuffix(got, "no hay nada para importar\n") {
		t.Fatalf("empty: %q", got)
	}
}
