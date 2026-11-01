package pricing

import (
	"encoding/json"
	"testing"

	"github.com/mananos/nxy.dev/internal/goldentest"
)

func matchJSON(m Match, ok bool, withPrice bool) any {
	if !ok {
		return nil
	}
	out := map[string]any{"key": m.Key, "estimated": m.Estimated}
	if withPrice {
		out["price"] = m.Price
	}
	return out
}

func TestGoldenModels(t *testing.T) {
	for _, c := range goldentest.Load(t, "pricing") {
		if c.File != "models.json" {
			continue
		}
		t.Run(c.Name, func(t *testing.T) {
			var model string
			c.In(t, "model", &model)
			if c.Has("models") {
				var raw json.RawMessage
				c.In(t, "models", &raw)
				tbl, err := ParseModels(raw)
				if err != nil {
					t.Fatal(err)
				}
				m, ok := tbl.PriceIn(model)
				goldentest.Check(t, c, map[string]any{"priceIn": matchJSON(m, ok, true)})
				return
			}
			fam, famOK := FamilyOf(model)
			var famV any
			if famOK {
				famV = fam
			}
			var basis any
			if b, ok := EstimateBasis(model); ok {
				basis = b
			}
			m, ok := Default().PriceIn(model)
			goldentest.Check(t, c, map[string]any{
				"normalizeModel":   NormalizeModel(model),
				"familyOf":         famV,
				"versionOf":        VersionOf(NormalizeModel(model)),
				"priceFor":         matchJSON(m, ok, false),
				"estimateBasis":    basis,
				"isSyntheticModel": IsSyntheticModel(model),
			})
		})
	}
}

func TestGoldenUsage(t *testing.T) {
	for _, c := range goldentest.Load(t, "pricing") {
		if c.File != "usage.json" {
			continue
		}
		t.Run(c.Name, func(t *testing.T) {
			var raw any
			c.In(t, "raw", &raw)
			u := ToUsage(raw)
			var hit any
			if p := CacheHitPct(u); p != nil {
				hit = *p
			}
			goldentest.Check(t, c, map[string]any{
				"toUsage":     u,
				"isZeroUsage": IsZeroUsage(u),
				"totalInput":  TotalInput(u),
				"cacheHitPct": hit,
			})
		})
	}
}

func TestGoldenCost(t *testing.T) {
	for _, c := range goldentest.Load(t, "pricing") {
		if c.File != "cost.json" {
			continue
		}
		t.Run(c.Name, func(t *testing.T) {
			if _, ok := c.Output["tierFor"]; ok {
				var price ModelPrice
				var prompt float64
				c.In(t, "price", &price)
				c.In(t, "promptTokens", &prompt)
				goldentest.Check(t, c, map[string]any{"tierFor": TierFor(price, prompt)})
				return
			}
			var model string
			var raw any
			c.In(t, "model", &model)
			c.In(t, "usage", &raw)
			var prompt *float64
			if c.Has("promptTokens") {
				var p float64
				c.In(t, "promptTokens", &p)
				prompt = &p
			}
			u := ToUsage(raw)
			cost, ok := CostFor(model, u, prompt)
			var costV any
			if ok {
				costV = cost
			}
			goldentest.Check(t, c, map[string]any{"usage": u, "costFor": costV})
		})
	}
}
