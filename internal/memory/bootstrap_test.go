package memory

import (
	"fmt"
	"strings"
	"testing"
)

func TestDecodeBootstrap(t *testing.T) {
	ok := `[{"text":" run lint ","question":"what before closing?","evidence":["Makefile:12","ci.yml:3-9"],"paths":["src/**"]}]`
	got, err := DecodeBootstrap([]byte(ok))
	if err != nil || len(got) != 1 || got[0].Text != "run lint" {
		t.Fatalf("%+v %v", got, err)
	}
	many := "[" + strings.TrimSuffix(strings.Repeat(`{"text":"t","question":"q","evidence":["a:1"]},`, 11), ",") + "]"
	cases := map[string]string{
		"not json":     `nope`,
		"empty":        `[]`,
		"over 10":      many,
		"no text":      `[{"question":"q","evidence":["a:1"]}]`,
		"no question":  `[{"text":"t","evidence":["a:1"]}]`,
		"no evidence":  `[{"text":"t","question":"q"}]`,
		"bad evidence": `[{"text":"t","question":"q","evidence":["Makefile"]}]`,
	}
	for name, in := range cases {
		if _, err := DecodeBootstrap([]byte(in)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	ten := "[" + strings.TrimSuffix(strings.Repeat(`{"text":"t","question":"q","evidence":["a:1"]},`, 10), ",") + "]"
	if got, err := DecodeBootstrap([]byte(ten)); err != nil || len(got) != 10 {
		t.Errorf("ten: %d %v", len(got), err)
	}
}

func TestBootstrapPrompt(t *testing.T) {
	p := BootstrapPrompt([]Rule{{Text: "existing rule text"}}, "/x/nxy")
	for _, want := range []string{"existing rule text", `"/x/nxy" mem bootstrap save <<'JSON'`, "no tool enforces", "run X before closing", "CONTRIBUTING"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	if strings.Contains(BootstrapPrompt(nil, "nxy"), "already saved") {
		t.Error("lists rules when there are none")
	}
}

func TestBootstrapPromptHasNoLocate(t *testing.T) {
	p := BootstrapPrompt(nil, "/x/nxy")
	if strings.Contains(p, "locate") || !strings.HasPrefix(p, BootstrapPromptHead) {
		t.Errorf("prompt = %q", p)
	}
}

func cands(n int) []BootstrapCandidate {
	var out []BootstrapCandidate
	for i := 1; i <= n; i++ {
		out = append(out, BootstrapCandidate{Text: fmt.Sprintf("rule %d", i), Question: fmt.Sprintf("q%d", i), Evidence: []string{fmt.Sprintf("f%d.go:%d", i, i)}})
	}
	return out
}

func TestBootstrapQuestionsPaging(t *testing.T) {
	qs := BootstrapQuestions(cands(10))
	if len(qs) != 3 || len(qs[0].Options) != 4 || len(qs[1].Options) != 4 || len(qs[2].Options) != 2 {
		t.Fatalf("pages %+v", qs)
	}
	if qs[0].Question != "nxy bootstrap: ¿qué reglas guardo? (1/3)" {
		t.Errorf("question %q", qs[0].Question)
	}
	if qs[0].Options[0].Label != "B1 rule 1" {
		t.Errorf("label %q", qs[0].Options[0].Label)
	}
	d := qs[0].Options[0].Description
	for _, want := range []string{"rule 1", "q1", "f1.go:1"} {
		if !strings.Contains(d, want) {
			t.Errorf("description %q lacks %q", d, want)
		}
	}
	// a page of one gets "Ninguna"
	one := BootstrapQuestions(cands(5))
	if len(one) != 2 || len(one[1].Options) != 2 || one[1].Options[1].Label != "Ninguna" {
		t.Errorf("single page %+v", one)
	}
	if len(BootstrapQuestions(nil)) != 0 {
		t.Error("questions for nothing")
	}
}

func TestBootstrapChosenAcrossPages(t *testing.T) {
	cs := cands(10)
	answers := map[string]string{
		BootstrapQuestion(1, 3): "B2 rule 2, B4 rule 4",
		BootstrapQuestion(2, 3): "B5 rule 5",
		BootstrapQuestion(3, 3): "B1", // B1 belongs to page 1: not a pick here
	}
	got := BootstrapChosen(answers, cs)
	if len(got) != 3 || got[0].Text != "rule 2" || got[1].Text != "rule 4" || got[2].Text != "rule 5" {
		t.Errorf("chosen %+v", got)
	}
	if got := BootstrapChosen(map[string]string{BootstrapQuestion(2, 2): "Ninguna"}, cands(5)); len(got) != 0 {
		t.Errorf("none picked %+v", got)
	}
	// text that mentions another label inside a rule is not a pick
	if got := BootstrapChosen(map[string]string{BootstrapQuestion(1, 1): "B3 rule 3"}, cands(3)); len(got) != 1 || got[0].Text != "rule 3" {
		t.Errorf("single %+v", got)
	}
}

func TestBootstrapChosenQuotedLabel(t *testing.T) {
	cs := cands(3)
	q := BootstrapQuestion(1, 1)
	got := BootstrapChosen(map[string]string{q: `"B2 texto, con coma"`}, cs)
	if len(got) != 1 || got[0].Text != "rule 2" {
		t.Errorf("chosen %+v", got)
	}
}

func TestBootstrapChosenText(t *testing.T) {
	cands := make([]BootstrapCandidate, 5)
	for i := range cands {
		cands[i] = BootstrapCandidate{Text: "t", Question: "q"}
	}
	got, ok := BootstrapChosenText("B1, B2, B4", cands)
	if !ok || len(got) != 3 {
		t.Fatalf("B1, B2, B4 = %v %v", got, ok)
	}
	if got, ok := BootstrapChosenText("todas", cands); !ok || len(got) != 5 {
		t.Errorf("todas = %v %v", got, ok)
	}
	if got, ok := BootstrapChosenText("b1, B3", cands); !ok || len(got) != 2 {
		t.Errorf("b1, B3 = %v %v", got, ok)
	}
	if got, ok := BootstrapChosenText("Todas", cands); !ok || len(got) != 5 {
		t.Errorf("Todas = %v %v", got, ok)
	}
	if got, ok := BootstrapChosenText("ninguna", cands); !ok || len(got) != 0 {
		t.Errorf("ninguna = %v %v", got, ok)
	}
	for _, in := range []string{"b2 no anda el login", "B9", "B1 y B2", "hacé el login", ""} {
		if _, ok := BootstrapChosenText(in, cands); ok {
			t.Errorf("%q accepted", in)
		}
	}
}
