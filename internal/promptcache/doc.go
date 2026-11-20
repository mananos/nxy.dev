// Package promptcache is the port of core/cache.mjs: the pure model of the prompt cache (life
// left, cost of a cold rewrite, TTL choice and expiry warnings). It reuses internal/pricing for
// the price lookup. Results must be identical to the 1.x JS, so numbers are float64 and the cost
// keeps the 1.x operation order. JS null/undefined numbers come in as NaN, as in internal/format.
//
// The 1.x consumer is hosts/claude-code/mod/driver.mjs (the cache toast and the panel), so
// nothing calls this package until F5. core/metrics/cache-ttl.mjs is ported apart, in
// internal/metrics/ttl.go.
//
// Differences from the 1.x: HitOf takes the usage as decoded by encoding/json into any; the
// golden corpus holds numbers and null only, not JS strings in numeric arguments.
package promptcache
