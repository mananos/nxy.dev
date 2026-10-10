// @ts-check
// Modelo puro de la cache de prompts (sin módulos de Node): vida restante, costo en frío, TTL y avisos.
import { priceIn, tierFor } from './price-table.mjs';

/** @typedef {'5m'|'1h'} Ttl */

const TTL_MS = { '5m': 5 * 60_000, '1h': 60 * 60_000 };
const WARN_LEAD_MS = 60_000;

/** @param {Ttl|null|undefined} ttl */
export function ttlMsOf(ttl) {
  return ttl === '1h' ? TTL_MS['1h'] : ttl === '5m' ? TTL_MS['5m'] : null;
}

/**
 * Vida restante de la cache desde el último mensaje del hilo principal.
 * @param {{lastTs?: number|null, ttlMs?: number|null, now?: number}} a
 * @returns {{leftMs: number|null, state: 'warm'|'cold'|'unknown'}}
 */
export function cacheLife({ lastTs, ttlMs, now = Date.now() }) {
  if (!Number.isFinite(lastTs) || !Number.isFinite(ttlMs)) return { leftMs: null, state: 'unknown' };
  const leftMs = /** @type {number} */ (lastTs) + /** @type {number} */ (ttlMs) - now;
  return leftMs > 0 ? { leftMs, state: 'warm' } : { leftMs: 0, state: 'cold' };
}

/**
 * Costo de reescribir `tokens` en frío con la tarifa de escritura del TTL. `estimated` = precio por familia.
 * @param {{tokens: number, model: string, ttl?: Ttl|null, prices: Record<string, import('./price-table.mjs').ModelPrice>}} a
 * @returns {{usd: number, estimated: boolean}|null}
 */
export function coldCost({ tokens, model, ttl, prices }) {
  if (!Number.isFinite(tokens) || tokens < 0 || !prices) return null;
  const hit = priceIn(prices, model);
  if (!hit) return null;
  const price = tierFor(hit.price, tokens); // tokens = el prompt entero que se reescribiría
  const rate = ttl === '1h' ? price.cache_write_1h : price.cache_write_5m;
  return { usd: (tokens * rate) / 1_000_000, estimated: hit.estimated };
}

/**
 * % del input servido desde cache (0..100) de un `usage` snake_case; null si no hubo input.
 * @param {any} usage
 * @returns {number|null}
 */
export function hitOf(usage) {
  const n = (v) => (typeof v === 'number' && Number.isFinite(v) ? v : 0);
  const read = n(usage?.cache_read_input_tokens);
  const total = n(usage?.input_tokens) + read + n(usage?.cache_creation_input_tokens);
  return total ? (read / total) * 100 : null;
}

/**
 * Si la cache sobrevivió a una pausa mayor a 5 min, el TTL real es 1 h. Nunca baja de '1h'.
 * @param {{ttl: Ttl, gapMs: number, readTokens: number, prevTokens: number}} a
 * @returns {Ttl}
 */
export function inferTtl({ ttl, gapMs, readTokens, prevTokens }) {
  if (ttl === '1h') return '1h';
  if (gapMs > TTL_MS['5m'] && readTokens > 0 && readTokens >= prevTokens * 0.5) return '1h';
  return ttl;
}

/**
 * Cuándo avisar y cuándo vence. `silent` si el costo en frío redondea a $0.00 (no vale el aviso).
 * @param {{lastTs: number, ttlMs: number, now?: number, coldUsd?: number|null}} a
 * @returns {{warnInMs: number|null, expireInMs: number, silent: boolean}}
 */
export function expiryPlan({ lastTs, ttlMs, now = Date.now(), coldUsd = null }) {
  const expireInMs = Math.max(0, lastTs + ttlMs - now);
  const warn = expireInMs - WARN_LEAD_MS;
  return {
    warnInMs: warn > 0 ? warn : null,
    expireInMs,
    silent: typeof coldUsd === 'number' && coldUsd < 0.005,
  };
}

/**
 * TTL a usar: el observado en la transcripción manda; si no hay, `promptCacheTtl` y luego lo
 * inferido (`infer` solo corre sin dato observado). Sin nada: '5m'.
 * @param {{observed?: Ttl|null, settings?: string|null, infer?: {gapMs: number, readTokens: number, prevTokens: number}|null}} a
 * @returns {{ttl: Ttl, source: 'observed'|'settings'|'inferred'|'default'}}
 */
export function pickTtl({ observed, settings, infer = null }) {
  if (observed === '5m' || observed === '1h') return { ttl: observed, source: 'observed' };
  /** @type {Ttl} */
  let base = '5m';
  /** @type {'settings'|'default'} */
  let source = 'default';
  if (settings === '5m' || settings === '1h') {
    base = settings;
    source = 'settings';
  }
  if (infer) {
    const ttl = inferTtl({ ttl: base, ...infer });
    if (ttl !== base) return { ttl, source: 'inferred' };
  }
  return { ttl: base, source };
}

/**
 * Arma el `cache` que acepta `buildPanel`, o null si no hay dato de último mensaje o de TTL.
 * @param {{lastTs?: number|null, ttl?: Ttl|null, hitPct?: number|null, tokens?: number|null, model?: string|null,
 *   prices?: Record<string, import('./price-table.mjs').ModelPrice>|null, now?: number}} a
 */
export function cacheOf({ lastTs, ttl, hitPct, tokens, model, prices, now = Date.now() }) {
  const ttlMs = ttlMsOf(ttl);
  const life = cacheLife({ lastTs, ttlMs, now });
  if (life.state === 'unknown') return null;
  const cost = model && prices && Number.isFinite(tokens)
    ? coldCost({ tokens: /** @type {number} */ (tokens), model, ttl, prices })
    : null;
  return /** @type {{hitPct: number, ttlLeftMs: number, ttlMs: number, coldCostUsd: number, coldTokens: number|null, estimated: boolean}} */ ({
    hitPct: typeof hitPct === 'number' ? hitPct : 0,
    ttlLeftMs: life.leftMs,
    ttlMs,
    coldCostUsd: cost ? cost.usd : undefined,
    coldTokens: Number.isFinite(tokens) ? tokens : null,
    estimated: cost ? cost.estimated : false,
  });
}
