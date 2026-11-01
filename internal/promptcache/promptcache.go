package promptcache

import (
	"math"

	"github.com/mananos/nxy.dev/internal/pricing"
)

const (
	ttl5mMs    = 5 * 60_000
	ttl1hMs    = 60 * 60_000
	warnLeadMs = 60_000
)

// TTLMsOf is the lifetime in ms of a cache TTL ("5m" or "1h"); anything else is false.
func TTLMsOf(ttl string) (float64, bool) {
	switch ttl {
	case "1h":
		return ttl1hMs, true
	case "5m":
		return ttl5mMs, true
	}
	return 0, false
}

func finite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

// Life is the remaining life of the cache. LeftMs is nil when State is "unknown".
type Life struct {
	LeftMs *float64 `json:"leftMs"`
	State  string   `json:"state"` // warm | cold | unknown
}

// CacheLife is the remaining life since the last message of the main thread. NaN in lastTs or
// ttlMs (JS null/undefined) gives "unknown".
func CacheLife(lastTs, ttlMs, now float64) Life {
	if !finite(lastTs) || !finite(ttlMs) {
		return Life{State: "unknown"}
	}
	left := lastTs + ttlMs - now
	if left > 0 {
		return Life{LeftMs: &left, State: "warm"}
	}
	zero := 0.0
	return Life{LeftMs: &zero, State: "cold"}
}

// Cost is what rewriting a prompt in cold would cost. Estimated = price by family.
type Cost struct {
	USD       float64 `json:"usd"`
	Estimated bool    `json:"estimated"`
}

// ColdCost is the cost of rewriting tokens (the whole prompt) with the write rate of the TTL
// ("1h" uses the 1h rate, anything else the 5m one). False for a nil table, tokens that are not
// finite or are negative, or a model with no price.
func ColdCost(tokens float64, model, ttl string, t *pricing.Table) (Cost, bool) {
	if !finite(tokens) || tokens < 0 || t == nil {
		return Cost{}, false
	}
	hit, ok := t.PriceIn(model)
	if !ok {
		return Cost{}, false
	}
	price := pricing.TierFor(hit.Price, tokens)
	rate := price.CacheWrite5m
	if ttl == "1h" {
		rate = price.CacheWrite1h
	}
	return Cost{USD: (tokens * rate) / 1_000_000, Estimated: hit.Estimated}, true
}

// HitOf is the % of the input served from cache (0..100) of a snake_case usage object (as decoded
// by encoding/json into any); nil when there was no input. Fields that are not numbers count 0.
func HitOf(usage any) *float64 {
	m, _ := usage.(map[string]any)
	n := func(key string) float64 {
		if v, ok := m[key].(float64); ok && finite(v) {
			return v
		}
		return 0
	}
	read := n("cache_read_input_tokens")
	total := n("input_tokens") + read + n("cache_creation_input_tokens")
	if total == 0 {
		return nil
	}
	p := (read / total) * 100
	return &p
}

// Infer is the evidence InferTTL and PickTTL use to detect a 1h cache.
type Infer struct {
	GapMs      float64
	ReadTokens float64
	PrevTokens float64
}

// InferTTL: if the cache survived a pause longer than 5 min the real TTL is 1h. Never goes down
// from "1h".
func InferTTL(ttl string, gapMs, readTokens, prevTokens float64) string {
	if ttl == "1h" {
		return "1h"
	}
	if gapMs > ttl5mMs && readTokens > 0 && readTokens >= prevTokens*0.5 {
		return "1h"
	}
	return ttl
}

// Plan is when to warn and when the cache expires.
type Plan struct {
	WarnInMs   *float64 `json:"warnInMs"`
	ExpireInMs float64  `json:"expireInMs"`
	Silent     bool     `json:"silent"`
}

// ExpiryPlan: when to warn and when it expires. Silent when the cold cost rounds to $0.00 (coldUsd
// below 0.005; nil = unknown cost, never silent).
func ExpiryPlan(lastTs, ttlMs, now float64, coldUsd *float64) Plan {
	expire := math.Max(0, lastTs+ttlMs-now)
	p := Plan{ExpireInMs: expire, Silent: coldUsd != nil && *coldUsd < 0.005}
	if warn := expire - warnLeadMs; warn > 0 {
		p.WarnInMs = &warn
	}
	return p
}

// PickTTL is the TTL to use: the observed one wins; without it promptCacheTtl (settings), then
// the inferred one (infer only runs without an observed value). Empty strings mean absent.
// Without anything: "5m". source is observed | settings | inferred | default.
func PickTTL(observed, settings string, infer *Infer) (ttl, source string) {
	if observed == "5m" || observed == "1h" {
		return observed, "observed"
	}
	base, source := "5m", "default"
	if settings == "5m" || settings == "1h" {
		base, source = settings, "settings"
	}
	if infer != nil {
		if t := InferTTL(base, infer.GapMs, infer.ReadTokens, infer.PrevTokens); t != base {
			return t, "inferred"
		}
	}
	return base, source
}

// Panel is the cache block buildPanel accepts. ColdCostUSD is left out of the JSON when there is
// no cost (JS undefined); ColdTokens is null when tokens was not finite.
type Panel struct {
	HitPct      float64  `json:"hitPct"`
	TTLLeftMs   float64  `json:"ttlLeftMs"`
	TTLMs       float64  `json:"ttlMs"`
	ColdCostUSD *float64 `json:"coldCostUsd,omitempty"`
	ColdTokens  *float64 `json:"coldTokens"`
	Estimated   bool     `json:"estimated"`
}

// CacheOf builds the panel, or nil when there is no last-message time or no valid TTL. NaN in
// lastTs or tokens is JS null/undefined; a nil hitPct counts 0; an empty model or a nil table
// give no cost.
func CacheOf(lastTs float64, ttl string, hitPct *float64, tokens float64, model string, t *pricing.Table, now float64) *Panel {
	ttlMs, ok := TTLMsOf(ttl)
	if !ok {
		ttlMs = math.NaN()
	}
	life := CacheLife(lastTs, ttlMs, now)
	if life.State == "unknown" {
		return nil
	}
	var cost *Cost
	if model != "" && t != nil && finite(tokens) {
		if c, ok := ColdCost(tokens, model, ttl, t); ok {
			cost = &c
		}
	}
	p := &Panel{TTLLeftMs: *life.LeftMs, TTLMs: ttlMs}
	if hitPct != nil {
		p.HitPct = *hitPct
	}
	if cost != nil {
		usd := cost.USD
		p.ColdCostUSD = &usd
		p.Estimated = cost.Estimated
	}
	if finite(tokens) {
		tk := tokens
		p.ColdTokens = &tk
	}
	return p
}
