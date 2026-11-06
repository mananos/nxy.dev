package repos

import (
	"testing"

	"github.com/mananos/nxy.dev/internal/goldentest"
)

func TestGolden(t *testing.T) {
	for _, c := range goldentest.Load(t, "repos") {
		t.Run(c.File+"/"+c.Name, func(t *testing.T) {
			var roots []string
			c.In(t, "roots", &roots)
			switch c.File {
			case "names.json":
				got := map[string]any{}
				for k, v := range Names(roots, false) {
					got[k] = v
				}
				goldentest.Check(t, c, map[string]any{"names": got})
			case "label.json":
				var root, abs string
				var primary bool
				c.In(t, "root", &root)
				c.In(t, "abs", &abs)
				c.In(t, "primary", &primary)
				goldentest.Check(t, c, map[string]any{"label": Label(root, abs, Names(roots, false), primary)})
			case "of.json":
				var file string
				c.In(t, "file", &file)
				var idx any
				if r, ok := Of(file, roots, false); ok {
					for i, x := range roots {
						if x == r {
							idx = float64(i)
							break
						}
					}
				}
				goldentest.Check(t, c, map[string]any{"index": idx})
			default:
				t.Fatalf("unknown golden file %s", c.File)
			}
		})
	}
}
