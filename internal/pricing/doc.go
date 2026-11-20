// Package pricing is the port of core/pricing.mjs and core/price-table.mjs: model id
// normalisation, price lookup (exact or estimated by family), token usage and cost. It is pure:
// the price table is embedded. Results must be identical to the 1.x JS, so token counts are
// float64 (JS numbers) and the cost keeps the 1.x operation order.
package pricing
