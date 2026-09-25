// @ts-check
/**
 * Host-agnostic aggregation of API calls.
 *
 * This is the core side of the core↔host contract: a host adapter is responsible for turning
 * whatever its tool writes on disk into `Call` records; everything from here on — summing usage,
 * pricing it, tracking the context peak — is the same for every host.
 *
 * @typedef {object} Call
 * @property {string} key       dedupe key (requestId / message id)
 * @property {number} ts        epoch ms
 * @property {string} model
 * @property {import('../pricing.mjs').Usage} usage
 * @property {string|null} effort
 * @property {string|null} skill
 * @property {string|null} promptKey
 * @property {string|null} agentId  null for the main transcript
 * @property {number|null} [gapMs]  time since the previous call of the same thread; null for the first
 * @property {'subagent'|'compact'|null} [after]  what the thread waited on before this call
 */
import { addUsage, cacheHitPct, costFor, emptyUsage, totalInput } from '../pricing.mjs';

/**
 * Accumulator for a group of calls. `usd` es la suma de lo que sí tiene tarifa; `usdUnknownCalls`
 * cuenta las llamadas que quedaron fuera (modelo sin precio) y `unknownModels` cuáles fueron.
 */
export function newBucket() {
  return { calls: 0, usage: emptyUsage(), usd: 0, usdUnknownCalls: 0, unknownModels: /** @type {Set<string>} */ (new Set()), contextPeak: 0 };
}

/** Adds one call into a bucket (mutates). Cost is recomputed from the pricing table each time. */
export function addCall(bucket, call) {
  bucket.calls++;
  addUsage(bucket.usage, call.usage);
  const usd = costFor(call.model, call.usage);
  if (usd === null) {
    bucket.usdUnknownCalls++;
    bucket.unknownModels.add(call.model);
  } else bucket.usd += usd;
  bucket.contextPeak = Math.max(bucket.contextPeak, totalInput(call.usage));
  return bucket;
}

/**
 * Cierra un bucket para salida: `usd` queda como la parte conocida (nunca null) y `usdPartial`
 * avisa que faltan llamadas por tarifar — quien muestre el número decide cómo marcarlo.
 * @template {ReturnType<typeof newBucket>} B
 * @param {B} bucket
 */
export function finishBucket(bucket) {
  return {
    ...bucket,
    usdPartial: bucket.usdUnknownCalls > 0,
    unknownModels: [...bucket.unknownModels],
    cacheHitPct: cacheHitPct(bucket.usage),
    totalInput: totalInput(bucket.usage),
  };
}
