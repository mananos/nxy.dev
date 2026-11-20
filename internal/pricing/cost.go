package pricing

// CostFor is the cache-aware cost in USD. A call with no tokens costs 0 for any model; with
// tokens and an unknown model it returns (0, false) -- never an invented number.
// The `above` tier belongs to ONE request: u must be a single call's usage. When u is partial,
// promptTokens gives the whole prompt of the request.
//
// The operation order is the 1.x one and every product is wrapped in float64(...) so the compiler
// cannot fuse multiply-add (arm64) and change the last bit.
func CostFor(model string, u Usage, promptTokens *float64) (float64, bool) {
	if IsZeroUsage(u) {
		return 0, true
	}
	m, ok := Default().PriceIn(model)
	if !ok {
		return 0, false
	}
	prompt := TotalInput(u)
	if promptTokens != nil {
		prompt = *promptTokens
	}
	price := TierFor(m.Price, prompt)
	inputRate, outputRate := price.Input, price.Output
	if u.Speed == "fast" && price.Fast != nil {
		inputRate, outputRate = price.Fast.Input, price.Fast.Output
	}
	// Cache multipliers stack on top of fast-mode input pricing.
	ratio := inputRate / price.Input
	sum := float64(u.Input * inputRate)
	sum = sum + float64(float64(u.CacheWrite5m*price.CacheWrite5m)*ratio)
	sum = sum + float64(float64(u.CacheWrite1h*price.CacheWrite1h)*ratio)
	sum = sum + float64(float64(u.CacheRead*price.CacheRead)*ratio)
	sum = sum + float64(u.Output*outputRate)
	return sum / 1_000_000, true
}
