package claudecode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/goldentest"
	"github.com/mananos/nxy.dev/v2/internal/host/claudecode/transcript"
)

type incStep struct {
	Write       *string `json:"write"`
	Append      *string `json:"append"`
	Remove      bool    `json:"remove"`
	Reload      bool    `json:"reload"`
	TurnSinceTs *int64  `json:"turnSinceTs"`
}

func TestGoldenIncremental(t *testing.T) {
	n := 0
	for _, c := range goldentest.Load(t, "transcripts") {
		if c.File != "incremental.json" {
			continue
		}
		n++
		t.Run(c.Name, func(t *testing.T) {
			var steps []incStep
			c.In(t, "steps", &steps)
			path := filepath.Join(t.TempDir(), "t.jsonl")
			st := transcript.NewIncrementalState()
			out := []any{}
			for _, s := range steps {
				switch {
				case s.Write != nil:
					if err := os.WriteFile(path, []byte(*s.Write), 0o644); err != nil {
						t.Fatal(err)
					}
				case s.Append != nil:
					f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := f.WriteString(*s.Append); err != nil {
						t.Fatal(err)
					}
					f.Close()
				case s.Remove:
					os.Remove(path)
				}
				if s.Reload { // the statusline cache: the state goes through JSON between renders
					raw, err := json.Marshal(st)
					if err != nil {
						t.Fatal(err)
					}
					st = &transcript.IncrementalState{}
					if err := json.Unmarshal(raw, st); err != nil {
						t.Fatal(err)
					}
				}
				ReadIncremental(path, st, IncrementalOpts{TurnSinceTs: s.TurnSinceTs})
				out = append(out, map[string]any{"state": goldentest.Norm(t, st), "seenKeys": st.Seen.Keys()})
			}
			goldentest.Check(t, c, map[string]any{"steps": out})
		})
	}
	if n == 0 {
		t.Fatal("no incremental cases")
	}
}

func TestGoldenTail(t *testing.T) {
	n := 0
	for _, c := range goldentest.Load(t, "transcripts") {
		if c.File != "tail.json" {
			continue
		}
		n++
		t.Run(c.Name, func(t *testing.T) {
			var text *string
			var pathNull bool
			var tailBytes int64
			c.In(t, "file", &text)
			c.In(t, "pathNull", &pathNull)
			c.In(t, "tailBytes", &tailBytes)
			path := filepath.Join(t.TempDir(), "t.jsonl")
			if text != nil {
				if err := os.WriteFile(path, []byte(*text), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if pathNull {
				path = ""
			}
			var got any // nil is the JS null
			if ttl, ok := LastCacheTTL(path, tailBytes); ok {
				got = ttl
			}
			goldentest.Check(t, c, map[string]any{"ttl": got})
		})
	}
	if n == 0 {
		t.Fatal("no tail cases")
	}
}
