package textdiff

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/goldentest"
)

func str(t *testing.T, c goldentest.Case, key string) string {
	t.Helper()
	var s string
	c.In(t, key, &s)
	return s
}

func optStr(t *testing.T, c goldentest.Case, key string) *string {
	t.Helper()
	var s *string
	c.In(t, key, &s)
	return s
}

func opsJSON(ops []Op) []any {
	out := make([]any, len(ops))
	for i, o := range ops {
		out[i] = map[string]any{"op": string(rune(o.Op)), "line": o.Line}
	}
	return out
}

func hunksJSON(hs []Hunk) []any {
	out := make([]any, len(hs))
	for i, h := range hs {
		out[i] = map[string]any{"oldStart": h.OldStart, "oldLines": h.OldLines, "newStart": h.NewStart, "newLines": h.NewLines, "ops": opsJSON(h.Ops)}
	}
	return out
}

// bigText builds n lines "<prefix><i>" (the corpus spec of the big set).
func bigText(prefix string, n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(prefix + strconv.Itoa(i) + "\n")
	}
	return b.String()
}

func TestGolden(t *testing.T) {
	for _, c := range goldentest.Load(t, "textdiff") {
		t.Run(c.File+"/"+c.Name, func(t *testing.T) {
			switch c.File {
			case "lines.json":
				goldentest.Check(t, c, map[string]any{"ops": opsJSON(DiffLines(str(t, c, "before"), str(t, c, "after")))})
			case "hunks.json":
				var ctx int
				c.In(t, "context", &ctx)
				goldentest.Check(t, c, map[string]any{"hunks": hunksJSON(Hunks(DiffLines(str(t, c, "before"), str(t, c, "after")), ctx))})
			case "file.json":
				r := FileDiff(str(t, c, "label"), optStr(t, c, "before"), optStr(t, c, "after"))
				goldentest.Check(t, c, map[string]any{"text": r.Text, "added": r.Added, "removed": r.Removed, "ranges": r.Ranges})
			case "big.json":
				var spec struct {
					BeforePrefix, AfterPrefix string
					N, M                      int
				}
				c.In(t, "beforePrefix", &spec.BeforePrefix)
				c.In(t, "afterPrefix", &spec.AfterPrefix)
				c.In(t, "n", &spec.N)
				c.In(t, "m", &spec.M)
				b, a := bigText(spec.BeforePrefix, spec.N), bigText(spec.AfterPrefix, spec.M)
				r := FileDiff("big.txt", &b, &a)
				sum := sha256.Sum256([]byte(r.Text))
				goldentest.Check(t, c, map[string]any{"added": r.Added, "removed": r.Removed, "ranges": r.Ranges, "textLen": len([]rune(r.Text)), "textSha256": hex.EncodeToString(sum[:])})
			default:
				t.Fatalf("unknown golden file %s", c.File)
			}
		})
	}
}
