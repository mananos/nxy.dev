package promptcache

import (
	"math"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/goldentest"
	"github.com/mananos/nxy.dev/v2/internal/pricing"
)

// num reads input[key] as a float64; JSON null or a missing key is NaN (JS null).
func num(t *testing.T, c goldentest.Case, key string) float64 {
	t.Helper()
	var p *float64
	c.In(t, key, &p)
	if p == nil {
		return math.NaN()
	}
	return *p
}

func optNum(t *testing.T, c goldentest.Case, key string) *float64 {
	t.Helper()
	var p *float64
	c.In(t, key, &p)
	return p
}

func str(t *testing.T, c goldentest.Case, key string) string {
	t.Helper()
	var s *string
	c.In(t, key, &s)
	if s == nil {
		return ""
	}
	return *s
}

// table decodes input.table: "default", null (nil) or a models object.
func table(t *testing.T, c goldentest.Case) *pricing.Table {
	t.Helper()
	raw := c.Input["table"]
	if string(raw) == "null" {
		return nil
	}
	if string(raw) == `"default"` {
		d := pricing.Default()
		return &d
	}
	tbl, err := pricing.ParseModels(raw)
	if err != nil {
		t.Fatal(err)
	}
	return &tbl
}

func orNil(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func TestGolden(t *testing.T) {
	for _, c := range goldentest.Load(t, "promptcache") {
		t.Run(c.File+"/"+c.Name, func(t *testing.T) {
			switch c.File {
			case "ttlms.json":
				ms, ok := TTLMsOf(str(t, c, "ttl"))
				var v any
				if ok {
					v = ms
				}
				goldentest.Check(t, c, map[string]any{"ms": v})
			case "life.json":
				l := CacheLife(num(t, c, "lastTs"), num(t, c, "ttlMs"), num(t, c, "now"))
				goldentest.Check(t, c, map[string]any{"leftMs": orNil(l.LeftMs), "state": l.State})
			case "coldcost.json":
				cost, ok := ColdCost(num(t, c, "tokens"), str(t, c, "model"), str(t, c, "ttl"), table(t, c))
				var v any
				if ok {
					v = cost
				}
				goldentest.Check(t, c, map[string]any{"coldCost": v})
			case "hit.json":
				var usage any
				c.In(t, "usage", &usage)
				goldentest.Check(t, c, map[string]any{"hit": orNil(HitOf(usage))})
			case "infer.json":
				goldentest.Check(t, c, map[string]any{"ttl": InferTTL(str(t, c, "ttl"), num(t, c, "gapMs"), num(t, c, "readTokens"), num(t, c, "prevTokens"))})
			case "expiry.json":
				p := ExpiryPlan(num(t, c, "lastTs"), num(t, c, "ttlMs"), num(t, c, "now"), optNum(t, c, "coldUsd"))
				goldentest.Check(t, c, map[string]any{"warnInMs": orNil(p.WarnInMs), "expireInMs": p.ExpireInMs, "silent": p.Silent})
			case "pickttl.json":
				var raw *struct {
					GapMs      float64 `json:"gapMs"`
					ReadTokens float64 `json:"readTokens"`
					PrevTokens float64 `json:"prevTokens"`
				}
				c.In(t, "infer", &raw)
				var inf *Infer
				if raw != nil {
					inf = &Infer{GapMs: raw.GapMs, ReadTokens: raw.ReadTokens, PrevTokens: raw.PrevTokens}
				}
				ttl, source := PickTTL(str(t, c, "observed"), str(t, c, "settings"), inf)
				goldentest.Check(t, c, map[string]any{"ttl": ttl, "source": source})
			case "cacheof.json":
				p := CacheOf(num(t, c, "lastTs"), str(t, c, "ttl"), optNum(t, c, "hitPct"), num(t, c, "tokens"), str(t, c, "model"), table(t, c), num(t, c, "now"))
				var v any
				if p != nil {
					v = p
				}
				goldentest.Check(t, c, map[string]any{"cache": v})
			default:
				t.Fatalf("unknown golden file %s", c.File)
			}
		})
	}
}
