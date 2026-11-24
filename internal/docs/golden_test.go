package docs

import (
	"slices"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/domain"
	"github.com/mananos/nxy.dev/v2/internal/goldentest"
)

func TestGolden(t *testing.T) {
	for _, c := range goldentest.Load(t, "docs") {
		t.Run(c.File+"/"+c.Name, func(t *testing.T) {
			switch c.File {
			case "terms.json":
				var paths []string
				c.In(t, "paths", &paths)
				goldentest.Check(t, c, map[string]any{"terms": DocTerms(paths)})
			case "userdoc.json":
				var path string
				var wiki *string
				c.In(t, "path", &path)
				c.In(t, "wikiDir", &wiki)
				w := ""
				if wiki != nil {
					w = *wiki
				}
				goldentest.Check(t, c, map[string]any{"isUserDoc": IsUserDoc(path, w)})
			case "globs.json":
				var wiki *string
				c.In(t, "wikiDir", &wiki)
				w := ""
				if wiki != nil {
					w = *wiki
				}
				goldentest.Check(t, c, map[string]any{"globs": UserDocGlobs(w)})
			case "pattern.json":
				var terms []string
				c.In(t, "terms", &terms)
				goldentest.Check(t, c, map[string]any{"pattern": TermsPattern(terms)})
			case "wiki.json":
				var texts, dirs []string
				c.In(t, "texts", &texts)
				c.In(t, "dirs", &dirs)
				got := WikiDirFromCi(texts, func(rel string) bool { return slices.Contains(dirs, rel) })
				var out any
				if got != "" {
					out = got
				}
				goldentest.Check(t, c, map[string]any{"wikiDir": out})
			case "packet.json":
				var o struct {
					ID    string `json:"id"`
					Plan  string `json:"plan"`
					Files []struct {
						Path    string `json:"path"`
						Status  string `json:"status"`
						Added   int    `json:"added"`
						Removed int    `json:"removed"`
						Diff    string `json:"diff"`
					} `json:"files"`
					Docs         []domain.DocFile `json:"docs"`
					WikiDir      *string          `json:"wikiDir"`
					MaxDiffLines int              `json:"maxDiffLines"`
				}
				c.In(t, "o", &o)
				in := PacketIn{ID: o.ID, Plan: o.Plan, Docs: o.Docs, MaxDiffLines: o.MaxDiffLines}
				if o.WikiDir != nil {
					in.WikiDir = *o.WikiDir
				}
				for _, f := range o.Files {
					in.Files = append(in.Files, File{Path: f.Path, Status: f.Status, Added: f.Added, Removed: f.Removed, Diff: f.Diff})
				}
				goldentest.Check(t, c, map[string]any{"text": Packet(in)})
			case "shown.json":
				var docs []string
				c.In(t, "docs", &docs)
				goldentest.Check(t, c, map[string]any{"shown": Shown(docs)})
			default:
				t.Fatalf("unknown golden file %s", c.File)
			}
		})
	}
}

func TestQuestion(t *testing.T) {
	q := Question("abc123", []string{"README.md", "docs/a.md"})
	if q.Question != QuestionText("abc123") || len(q.Options) != 2 {
		t.Fatalf("question = %+v", q)
	}
	if q.Options[0].Label != LabelUpdate || q.Options[0].Description != "README.md, docs/a.md" || q.Options[1].Label != LabelLeave {
		t.Errorf("options = %+v", q.Options)
	}
}

func TestDocumenterPrompt(t *testing.T) {
	f := domain.Feature{ID: "abc123", Request: "add greet", Docs: &domain.Docs{
		Docs:    []domain.DocFile{{Path: "README.md", Lines: []domain.DocLine{{Line: 3, Text: "greet"}}}},
		WikiDir: "docs",
	}}
	p := DocumenterPrompt(f, `C:\bin\nxy.exe`)
	for _, want := range []string{"add greet", "README.md:3: greet", `"C:/bin/nxy.exe" review docs --feature abc123`, "`docs/`", "Edit and Write", "without `| head`, `| tail` or a redirect", "read its whole output"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt misses %q:\n%s", want, p)
		}
	}
}
