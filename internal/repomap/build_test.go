package repomap

import (
	"strings"
	"testing"
)

func TestBuildReuseAndOrder(t *testing.T) {
	texts := map[string]string{
		"b.ts":      "export class B {}\n",
		"a.go":      "func A() {}\ntype T int\n",
		"\uffff.ts": "class Hi {}\n",
		"😀.ts":      "class Emoji {}\n",
		"x.txt":     "class Ignored {}\n",
	}
	files := []SourceFile{{"b.ts", 10, 1}, {"a.go", 20, 2}, {"\uffff.ts", 5, 3}, {"😀.ts", 5, 4}, {"x.txt", 1, 5}, {"gone.ts", 1, 6}}
	reads := 0
	read := func(rel string) (string, bool) {
		reads++
		s, ok := texts[rel]
		return s, ok
	}
	next, mapText, parsed, reused := Build(files, nil, read)
	if parsed != 4 || reused != 0 || len(next) != 4 {
		t.Fatalf("first build: parsed %d reused %d files %d", parsed, reused, len(next))
	}
	want := "a.go\t1\tfunc\tA\tfunc A() {}\na.go\t2\ttype\tT\ttype T int\nb.ts\t1\tclass\tB\texport class B {}\n" +
		"😀.ts\t1\tclass\tEmoji\tclass Emoji {}\n\uffff.ts\t1\tclass\tHi\tclass Hi {}\n"
	if mapText != want { // UTF-16 order: the emoji (surrogates) sorts before U+FFFF
		t.Fatalf("map text:\n%q\nwant\n%q", mapText, want)
	}

	// meta round trip, then a rebuild reads only what changed
	prev := DecodeMeta(EncodeMeta(1234, "/repo", next))
	reads = 0
	files[0].MtimeMs = 99
	_, text2, parsed, reused := Build(files, prev, read)
	if parsed != 1 || reused != 3 || reads != 2 { // b.ts re-read, gone.ts tried again
		t.Fatalf("rebuild: parsed %d reused %d reads %d", parsed, reused, reads)
	}
	if text2 != mapText {
		t.Fatalf("rebuilt map differs: %q", text2)
	}
}

func TestBuildEmptyMapIsEmptyText(t *testing.T) {
	_, text, _, _ := Build(nil, nil, nil)
	if text != "" {
		t.Fatalf("got %q", text)
	}
}

func TestEncodeMetaSpelling(t *testing.T) {
	got := string(EncodeMeta(1.5e21, "C:\\r<>", map[string]Entry{"a.go": {MtimeMs: 1.25, Size: 3, Lines: []string{"a<b"}}, "b.go": {}}))
	want := `{"builtAt":1.5e+21,"root":"C:\\r<>","files":{"a.go":{"mtimeMs":1.25,"size":3,"lines":["a<b"]},"b.go":{"mtimeMs":0,"size":0,"lines":[]}}}`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestDecodeMetaTolerance(t *testing.T) {
	for _, raw := range []string{"", "not json", "null", "[]", `{"files":null}`, `{"files":[1]}`, `{"files":"x"}`, `{"files":{"a.go":5}}`, `{"files":{"a.go":{"mtimeMs":1,"size":2}}}`, `{"files":{"a.go":{"mtimeMs":"1","size":2,"lines":[]}}}`} {
		if m := DecodeMeta([]byte(raw)); len(m) != 0 {
			t.Errorf("%q gave %v", raw, m)
		}
	}
	m := DecodeMeta([]byte(`{"files":{"a.go":{"mtimeMs":1.5,"size":2,"lines":["x"]},"b.go":{"mtimeMs":1,"size":2.5,"lines":[]}}}`))
	if len(m) != 2 || m["a.go"].Size != 2 || strings.Join(m["a.go"].Lines, "") != "x" || m["b.go"].Size != -1 {
		t.Fatalf("got %+v", m)
	}
}
