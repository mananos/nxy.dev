package scout

import (
	"testing"

	"github.com/mananos/nxy.dev/internal/goldentest"
)

func TestGoldenTerms(t *testing.T) {
	for _, c := range goldentest.Load(t, "locate") {
		if c.File != "terms.json" {
			continue // report.json runs end to end in cmd/nxy (TestGoldenLocateReport)
		}
		t.Run(c.Name, func(t *testing.T) {
			var q string
			c.In(t, "question", &q)
			ids, terms := QueryTerms(q)
			goldentest.Check(t, c, map[string]any{"identifiers": ids, "terms": terms})
		})
	}
}
