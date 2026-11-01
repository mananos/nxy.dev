// @ts-check
/**
 * Golden of the 1.x prompt-cache model (`core/cache.mjs`) that `internal/promptcache` ports. Node is
 * the oracle. JS null stands for the NaN / absent value the Go callers pass; `now` is always explicit.
 * A table is "default" (core/pricing.json), null, or a models object.
 * Sets: ttlms, life, coldcost, hit, infer, expiry, pickttl, cacheof.
 *
 *   node scripts/golden/promptcache.mjs          write testdata/golden/promptcache/*.json
 *   node scripts/golden/promptcache.mjs --check  fail when they differ
 */
import { fileURLToPath } from 'node:url';
import { cacheLife, cacheOf, coldCost, expiryPlan, hitOf, inferTtl, pickTtl, ttlMsOf } from '../../core/cache.mjs';
import { loadPricing } from '../../core/pricing.mjs';
import { diffArea, loadCorpus, staleSets, writeArea } from './lib.mjs';

export const AREA = 'promptcache';

const idx = (/** @type {number} */ i) => String(i).padStart(3, '0');
const tableOf = (/** @type {any} */ t) => (t === 'default' ? loadPricing().models : t);
const num = (/** @type {any} */ v) => (v === null ? NaN : v);

/** @returns {Record<string, any[]>} */
export function generate() {
  /** @type {any} */
  const c = loadCorpus('promptcache');

  const ttlms = c.ttlms.map((/** @type {any} */ ttl, /** @type {number} */ i) => ({
    name: `ttlms:${idx(i)}:${JSON.stringify(ttl)}`,
    input: { ttl },
    output: { ms: ttlMsOf(ttl) },
  }));

  const life = c.life.map((/** @type {any[]} */ [lastTs, ttlMs, now], /** @type {number} */ i) => ({
    name: `life:${idx(i)}`,
    input: { lastTs, ttlMs, now },
    output: cacheLife({ lastTs: num(lastTs), ttlMs: num(ttlMs), now }),
  }));

  const coldcost = c.coldcost.map((/** @type {any} */ x, /** @type {number} */ i) => ({
    name: `coldcost:${idx(i)}:${x.model}`,
    input: x,
    output: { coldCost: coldCost({ tokens: num(x.tokens), model: x.model, ttl: x.ttl, prices: tableOf(x.table) }) },
  }));

  const hit = c.hit.map((/** @type {any} */ usage, /** @type {number} */ i) => ({
    name: `hit:${idx(i)}`,
    input: { usage },
    output: { hit: hitOf(usage) },
  }));

  const infer = c.infer.map((/** @type {any} */ x, /** @type {number} */ i) => ({
    name: `infer:${idx(i)}`,
    input: x,
    output: { ttl: inferTtl(x) },
  }));

  const expiry = c.expiry.map((/** @type {any} */ x, /** @type {number} */ i) => ({
    name: `expiry:${idx(i)}`,
    input: x,
    output: expiryPlan(x),
  }));

  const pickttl = c.pickttl.map((/** @type {any} */ x, /** @type {number} */ i) => ({
    name: `pickttl:${idx(i)}`,
    input: x,
    output: pickTtl(x),
  }));

  const cacheof = c.cacheof.map((/** @type {any} */ x, /** @type {number} */ i) => ({
    name: `cacheof:${idx(i)}`,
    input: x,
    output: {
      cache: cacheOf({
        lastTs: num(x.lastTs),
        ttl: x.ttl,
        hitPct: x.hitPct,
        tokens: num(x.tokens),
        model: x.model,
        prices: tableOf(x.table),
        now: x.now,
      }),
    },
  }));

  return { ttlms, life, coldcost, hit, infer, expiry, pickttl, cacheof };
}

/** @returns {string[]} differences, empty when up to date */
export function check() {
  const sets = generate();
  const diffs = [];
  for (const [set, cases] of Object.entries(sets)) diffs.push(...diffArea(AREA, set, cases, true));
  diffs.push(...staleSets(AREA, Object.keys(sets)));
  return diffs;
}

export function write() {
  return Object.entries(generate()).map(([set, cases]) => writeArea(AREA, set, cases, true));
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  if (process.argv.includes('--check')) {
    const diffs = check();
    for (const d of diffs) console.error(d);
    if (diffs.length) process.exit(1);
    console.log('golden files up to date');
  } else {
    for (const p of write()) console.log(`wrote ${p}`);
  }
}
