// @ts-check
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

/**
 * @typedef {object} ModelPrice
 * @property {number} input
 * @property {number} cache_write_5m
 * @property {number} cache_write_1h
 * @property {number} cache_read
 * @property {number} output
 * @property {{input: number, output: number}} [fast]
 */

/**
 * Normalized token counts for one API call (all fields in tokens).
 * @typedef {object} Usage
 * @property {number} input        uncached input tokens
 * @property {number} cacheWrite5m
 * @property {number} cacheWrite1h
 * @property {number} cacheRead
 * @property {number} output
 * @property {number} thinking     subset of output (informational)
 * @property {'standard'|'fast'} [speed]
 */

let cached = null;

/** @returns {{models: Record<string, ModelPrice>}} */
export function loadPricing() {
  if (!cached) {
    const p = join(dirname(fileURLToPath(import.meta.url)), 'pricing.json');
    cached = JSON.parse(readFileSync(p, 'utf8'));
  }
  return cached;
}

/**
 * Reduce un id de modelo a la clave de la tabla de precios. Cubre las tres plataformas:
 *   - API directa: `claude-opus-5-20260401[1m]` → `claude-opus-5`, `-latest` fuera.
 *   - Bedrock: `us.anthropic.claude-sonnet-4-5-20250929-v1:0` → `claude-sonnet-4-5`
 *     (prefijos de región/alcance `us.` `eu.` `apac.` `global.`, luego `anthropic.`, sufijo `-vN:M`).
 *   - Vertex: `claude-sonnet-4-5@20250929` → `claude-sonnet-4-5`.
 * Lo que no matchea queda como está (`claude-3-5-haiku-latest` → `claude-3-5-haiku`, desconocido).
 * @param {string} id
 */
export function normalizeModel(id) {
  return String(id || '')
    .toLowerCase()
    .replace(/^(?:us|eu|apac|global)\./, '')
    .replace(/^anthropic\./, '')
    .replace(/-v\d+:\d+$/, '')
    .replace(/@\d{8}$/, '')
    .replace(/\[1m\]$/, '')
    .replace(/-\d{8}$/, '')
    .replace(/-latest$/, '');
}

/**
 * Claude Code escribe mensajes internos con `model: "<synthetic>"` (y usage en cero): no son
 * llamadas a la API. Cualquier id envuelto en `<...>` cuenta como sintético.
 * @param {string} model
 */
export function isSyntheticModel(model) {
  return /^<[^<>]*>$/.test(String(model || '').trim());
}

/**
 * true cuando la llamada no consumió ningún token (input, cache, output).
 * @param {Usage} usage
 */
export function isZeroUsage(usage) {
  return usage.input === 0 && usage.cacheWrite5m === 0 && usage.cacheWrite1h === 0 && usage.cacheRead === 0 && usage.output === 0;
}

/**
 * Maps the raw `message.usage` object of a transcript line to {@link Usage}.
 * Missing fields are 0, never NaN.
 * @param {any} u
 * @returns {Usage}
 */
export function toUsage(u) {
  const n = (v) => (typeof v === 'number' && Number.isFinite(v) ? v : 0);
  const cc = u?.cache_creation || {};
  const totalWrite = n(u?.cache_creation_input_tokens);
  let w5 = n(cc.ephemeral_5m_input_tokens);
  let w1 = n(cc.ephemeral_1h_input_tokens);
  if (w5 + w1 === 0 && totalWrite > 0) w5 = totalWrite; // older transcripts: no TTL breakdown → assume 5m
  return {
    input: n(u?.input_tokens),
    cacheWrite5m: w5,
    cacheWrite1h: w1,
    cacheRead: n(u?.cache_read_input_tokens),
    output: n(u?.output_tokens),
    thinking: n(u?.output_tokens_details?.thinking_tokens),
    speed: u?.speed === 'fast' ? 'fast' : 'standard',
  };
}

/**
 * Cache-aware cost in USD. Una llamada sin tokens cuesta 0 sea cual sea el modelo (no hay nada
 * que tarifar). Con tokens y modelo desconocido devuelve `null` — nunca un número inventado.
 * @param {string} model
 * @param {Usage} usage
 * @returns {number|null}
 */
export function costFor(model, usage) {
  if (isZeroUsage(usage)) return 0;
  const price = loadPricing().models[normalizeModel(model)];
  if (!price) return null;
  const fast = usage.speed === 'fast' && price.fast ? price.fast : null;
  const inputRate = fast ? fast.input : price.input;
  const outputRate = fast ? fast.output : price.output;
  // Cache multipliers stack on top of fast-mode input pricing (docs: "prompt caching multipliers apply on top").
  const ratio = inputRate / price.input;
  return (
    (usage.input * inputRate +
      usage.cacheWrite5m * price.cache_write_5m * ratio +
      usage.cacheWrite1h * price.cache_write_1h * ratio +
      usage.cacheRead * price.cache_read * ratio +
      usage.output * outputRate) /
    1_000_000
  );
}

/** Empty accumulator for {@link addUsage}. */
export function emptyUsage() {
  return { input: 0, cacheWrite5m: 0, cacheWrite1h: 0, cacheRead: 0, output: 0, thinking: 0 };
}

/** Adds `u` into `acc` (mutates and returns `acc`). */
export function addUsage(acc, u) {
  acc.input += u.input;
  acc.cacheWrite5m += u.cacheWrite5m;
  acc.cacheWrite1h += u.cacheWrite1h;
  acc.cacheRead += u.cacheRead;
  acc.output += u.output;
  acc.thinking += u.thinking;
  return acc;
}

/** Total input (uncached + cache writes + cache reads). */
export function totalInput(u) {
  return u.input + u.cacheWrite5m + u.cacheWrite1h + u.cacheRead;
}

/** Share of input served from cache, 0..100; null when there was no input. */
export function cacheHitPct(u) {
  const t = totalInput(u);
  return t ? (u.cacheRead / t) * 100 : null;
}
