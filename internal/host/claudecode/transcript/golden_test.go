package transcript

import (
	"encoding/json"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/goldentest"
)

// fakeRoot stands for the temp dir the JS golden wrote the texts under; every label is
// <fakeRoot>/<rel>, which equals the JS output after normRoot.
const fakeRoot = "/nxy-golden-root"

func loadTexts(t *testing.T, cases []goldentest.Case) map[string]string {
	t.Helper()
	for _, c := range cases {
		if c.Name != "texts" {
			continue
		}
		var texts map[string]string
		if err := json.Unmarshal(c.Output["texts"], &texts); err != nil {
			t.Fatal(err)
		}
		return texts
	}
	t.Fatal("texts case not found in testdata/golden/transcripts")
	return nil
}

func TestGoldenFiles(t *testing.T) {
	cases := goldentest.Load(t, "transcripts")
	texts := loadTexts(t, cases)
	n := 0
	for _, c := range cases {
		if c.File != "files.json" {
			continue
		}
		n++
		t.Run(c.Name, func(t *testing.T) {
			var files []string
			var agentID *string
			var since *float64
			var share bool
			c.In(t, "files", &files)
			c.In(t, "agentId", &agentID)
			c.In(t, "since", &since)
			c.In(t, "share", &share)
			opts := FileOpts{AgentID: agentID}
			if since != nil {
				opts.Since = int64(*since)
			}
			if share {
				opts.Seen = NewUUIDSet()
			}
			results := make([]FileResult, 0, len(files))
			order := make([][]string, 0, len(files))
			for _, rel := range files {
				text, ok := texts[rel]
				r := ParseFile(fakeRoot+"/"+rel, []byte(text), ok, opts)
				results = append(results, r)
				order = append(order, append([]string{}, r.Tools.Calls.Keys...))
			}
			got := goldentest.NormRoot(goldentest.Norm(t, results), fakeRoot)
			goldentest.Check(t, c, map[string]any{"results": got, "callsOrder": order})
		})
	}
	if n == 0 {
		t.Fatal("no files cases")
	}
}

func TestGoldenContext(t *testing.T) {
	n := 0
	for _, c := range goldentest.Load(t, "gate") {
		if c.File != "context.json" {
			continue
		}
		n++
		t.Run(c.Name, func(t *testing.T) {
			var text string
			var truncated bool
			c.In(t, "text", &text)
			c.In(t, "truncated", &truncated)
			var tokens any
			if v, ok := LastContextTokens([]byte(text), truncated); ok {
				tokens = v
			}
			goldentest.Check(t, c, map[string]any{"tokens": tokens})
		})
	}
	if n == 0 {
		t.Fatal("no context cases")
	}
}

func TestEvents(t *testing.T) {
	got := Events([]byte("{\"a\":1}\nnope\n\n[2]\r\n{\"b\":\r\n"))
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2: %v", len(got), got)
	}
}
