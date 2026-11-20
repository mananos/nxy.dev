package gate

import (
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/goldentest"
)

func TestGolden(t *testing.T) {
	for _, c := range goldentest.Load(t, "gate") {
		if c.File == "context.json" {
			continue // run by internal/host/claudecode/transcript
		}
		t.Run(c.File+"/"+c.Name, func(t *testing.T) {
			switch c.File {
			case "readdecide.json":
				var in struct {
					Ctx *float64
					Sub bool
					Esc bool
					Cfg struct {
						Enabled       *bool
						ContextTokens float64
						Reads         *bool
					}
					Kind  *string
					Broad bool
				}
				c.In(t, "ctx", &in.Ctx)
				c.In(t, "sub", &in.Sub)
				c.In(t, "esc", &in.Esc)
				c.In(t, "cfg", &in.Cfg)
				c.In(t, "kind", &in.Kind)
				c.In(t, "broad", &in.Broad)
				cfg := Config{Enabled: in.Cfg.Enabled == nil || *in.Cfg.Enabled, ContextTokens: in.Cfg.ContextTokens, Reads: in.Cfg.Reads == nil || *in.Cfg.Reads}
				kind := ""
				if in.Kind != nil {
					kind = *in.Kind
				}
				d := DecideRead(ReadInput{ContextTokens: in.Ctx, IsSubagent: in.Sub, EscapeActive: in.Esc, Kind: kind, Broad: in.Broad}, cfg)
				goldentest.Check(t, c, map[string]any{"allow": d.Allow, "reason": d.Reason})
			case "readmsg.json":
				var search struct {
					Kind    *string
					Pattern *string
				}
				var ctx, threshold float64
				var first bool
				c.In(t, "search", &search)
				c.In(t, "ctx", &ctx)
				c.In(t, "threshold", &threshold)
				c.In(t, "first", &first)
				s := Search{}
				if search.Kind != nil {
					s.Kind = *search.Kind
				}
				if search.Pattern != nil {
					s.Pattern = *search.Pattern
				}
				goldentest.Check(t, c, map[string]any{"text": ReadGateMessage(s, ctx, threshold, first)})
			case "summary.json":
				var raw []struct {
					Kind          string   `json:"kind"`
					Allow         bool     `json:"allow"`
					ContextTokens *float64 `json:"contextTokens"`
				}
				c.In(t, "rows", &raw)
				rows := make([]Row, len(raw))
				for i, r := range raw {
					rows[i] = Row{Kind: r.Kind, Allow: r.Allow, ContextTokens: r.ContextTokens}
				}
				goldentest.Check(t, c, map[string]any{"lines": Summary(rows)})
			default:
				t.Fatalf("unknown golden file %s", c.File)
			}
		})
	}
}

func TestCodeFiles(t *testing.T) {
	exts := CodeExtensions([]string{".go", ".TS"}, []string{"*Test.kt", "**/*.groovy", "Makefile", "*.go"})
	want := []string{".go", ".ts", ".kt", ".groovy"}
	if len(exts) != len(want) {
		t.Fatalf("got %v, want %v", exts, want)
	}
	for i := range want {
		if exts[i] != want[i] {
			t.Fatalf("got %v, want %v", exts, want)
		}
	}
	for p, code := range map[string]bool{`C:\r\a.GO`: true, "src/A.kt": true, "README.md": false, "Makefile": false, ".gitignore": false, "a.": false} {
		if IsCodeFile(p, exts) != code {
			t.Errorf("IsCodeFile(%q) = %v", p, !code)
		}
	}
}

func TestDecideWrite(t *testing.T) {
	cfg := Config{Enabled: true}
	for _, tc := range []struct {
		in     WriteInput
		allow  bool
		reason string
	}{
		{WriteInput{InsideRepo: true, IsCode: true}, false, "code"},
		{WriteInput{InsideRepo: true, IsCode: true, EscapeActive: true}, true, "escape"},
		{WriteInput{InsideRepo: true}, true, "text"},
		{WriteInput{IsCode: true}, true, "outside"},
		{WriteInput{InsideRepo: true, IsCode: true, IsSubagent: true}, true, "subagent"},
	} {
		if d := DecideWrite(tc.in, cfg); d.Allow != tc.allow || d.Reason != tc.reason {
			t.Errorf("%+v: got %+v", tc.in, d)
		}
	}
	if d := DecideWrite(WriteInput{InsideRepo: true, IsCode: true}, Config{}); !d.Allow {
		t.Errorf("disabled gate must allow: %+v", d)
	}
}
