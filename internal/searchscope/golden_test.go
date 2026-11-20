package searchscope

import (
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/goldentest"
)

func classMap(c Class) map[string]any {
	m := map[string]any{"kind": c.Kind, "broad": c.Broad, "target": c.Target, "pattern": c.Pattern}
	if c.HasPath != nil {
		m["hasPath"] = *c.HasPath
	}
	return m
}

func TestGolden(t *testing.T) {
	for _, c := range goldentest.Load(t, "searchscope") {
		if c.File == "history.json" {
			continue // run by internal/host/claudecode (needs the filesystem)
		}
		t.Run(c.File+"/"+c.Name, func(t *testing.T) {
			var tool string
			var input any
			c.In(t, "tool", &tool)
			c.In(t, "input", &input)
			switch c.File {
			case "classify.json":
				var files []string
				c.In(t, "files", &files)
				isFile := func(p string) bool {
					for _, f := range files {
						if f == p {
							return true
						}
					}
					return false
				}
				goldentest.Check(t, c, map[string]any{"class": classMap(Classify(tool, input, isFile))})
			case "broadmain.json":
				goldentest.Check(t, c, map[string]any{"broad": IsBroadMainSearch(tool, input)})
			case "locate.json":
				var cmd string
				c.In(t, "cmd", &cmd)
				goldentest.Check(t, c, map[string]any{
					"locate": IsLocateCommand(cmd), "nudge": LocateNudgeMark, "gate": ReadGateMark,
				})
			default:
				t.Fatalf("unknown golden file %s", c.File)
			}
		})
	}
}
