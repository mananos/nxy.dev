package pricing

import (
	"math"
	"regexp"
	"strings"
)

// Usage is the normalised token count of one API call. Fields are float64 because the 1.x uses
// JS numbers: sums and costs must come out bit for bit the same.
type Usage struct {
	Input        float64 `json:"input"`
	CacheWrite5m float64 `json:"cacheWrite5m"`
	CacheWrite1h float64 `json:"cacheWrite1h"`
	CacheRead    float64 `json:"cacheRead"`
	Output       float64 `json:"output"`
	Thinking     float64 `json:"thinking"`
	// Speed is "standard" or "fast" after ToUsage; EmptyUsage leaves it empty (absent, as the 1.x).
	Speed string `json:"speed,omitempty"`
}

// field is raw[key] when raw is a JSON object (map), else nil: JS `u?.key` on anything else is undefined.
func field(raw any, key string) any {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	return m[key]
}

func num(v any) float64 {
	if f, ok := v.(float64); ok && !math.IsInf(f, 0) && !math.IsNaN(f) {
		return f
	}
	return 0
}

// ToUsage maps the raw `message.usage` of a transcript line (decoded into any) to Usage.
// Missing or non-numeric fields are 0.
func ToUsage(raw any) Usage {
	cc := field(raw, "cache_creation")
	totalWrite := num(field(raw, "cache_creation_input_tokens"))
	w5 := num(field(cc, "ephemeral_5m_input_tokens"))
	w1 := num(field(cc, "ephemeral_1h_input_tokens"))
	if w5+w1 == 0 && totalWrite > 0 {
		w5 = totalWrite // older transcripts: no TTL breakdown -> assume 5m
	}
	speed := "standard"
	if s, ok := field(raw, "speed").(string); ok && s == "fast" {
		speed = "fast"
	}
	return Usage{
		Input:        num(field(raw, "input_tokens")),
		CacheWrite5m: w5,
		CacheWrite1h: w1,
		CacheRead:    num(field(raw, "cache_read_input_tokens")),
		Output:       num(field(raw, "output_tokens")),
		Thinking:     num(field(field(raw, "output_tokens_details"), "thinking_tokens")),
		Speed:        speed,
	}
}

// IsZeroUsage is true when the call consumed no token (input, cache, output).
func IsZeroUsage(u Usage) bool {
	return u.Input == 0 && u.CacheWrite5m == 0 && u.CacheWrite1h == 0 && u.CacheRead == 0 && u.Output == 0
}

var syntheticRe = regexp.MustCompile(`^<[^<>]*>$`)

// jsSpace is the JS String.prototype.trim whitespace set.
func jsSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// IsSyntheticModel: Claude Code writes internal messages with model "<synthetic>"; any id wrapped
// in <...> counts.
func IsSyntheticModel(model string) bool {
	return syntheticRe.MatchString(strings.TrimFunc(model, jsSpace))
}

// EmptyUsage is the accumulator for AddUsage.
func EmptyUsage() Usage { return Usage{} }

// AddUsage adds u into acc and returns acc.
func AddUsage(acc *Usage, u Usage) *Usage {
	acc.Input += u.Input
	acc.CacheWrite5m += u.CacheWrite5m
	acc.CacheWrite1h += u.CacheWrite1h
	acc.CacheRead += u.CacheRead
	acc.Output += u.Output
	acc.Thinking += u.Thinking
	return acc
}

// TotalInput is uncached input + cache writes + cache reads.
func TotalInput(u Usage) float64 {
	return u.Input + u.CacheWrite5m + u.CacheWrite1h + u.CacheRead
}

// CacheHitPct is the share of input served from cache, 0..100; nil when there was no input.
func CacheHitPct(u Usage) *float64 {
	t := TotalInput(u)
	if t == 0 {
		return nil
	}
	p := float64(u.CacheRead/t) * 100
	return &p
}

// EstimateBasis is the key a model's price was estimated from; false when exact or unpriced.
func EstimateBasis(model string) (string, bool) {
	m, ok := Default().PriceIn(model)
	if ok && m.Estimated {
		return m.Key, true
	}
	return "", false
}
