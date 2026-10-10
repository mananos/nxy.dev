import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { cacheLife, coldCost, hitOf, inferTtl, expiryPlan, pickTtl, cacheOf } from '../core/cache.mjs';
import { priceFor, normalizeModel } from '../core/pricing.mjs';
import { priceIn } from '../core/price-table.mjs';

const prices = {
  'claude-sonnet-4-5': { input: 3, cache_write_5m: 3.75, cache_write_1h: 6, cache_read: 0.3, output: 15 },
  'claude-sonnet-5': { input: 3, cache_write_5m: 4, cache_write_1h: 6, cache_read: 0.3, output: 15 },
};

test('cacheLife: warm, cold, unknown', () => {
  assert.deepEqual(cacheLife({ lastTs: 1000, ttlMs: 300000, now: 101000 }), { leftMs: 200000, state: 'warm' });
  assert.deepEqual(cacheLife({ lastTs: 1000, ttlMs: 300000, now: 400000 }), { leftMs: 0, state: 'cold' });
  assert.equal(cacheLife({ lastTs: null, ttlMs: 300000, now: 5 }).state, 'unknown');
  assert.equal(cacheLife({ lastTs: 5, ttlMs: null, now: 5 }).state, 'unknown');
});

test('coldCost: exact price, 1h rate, family estimate marked, unknown model null', () => {
  assert.deepEqual(coldCost({ tokens: 1_000_000, model: 'claude-sonnet-5', ttl: '5m', prices }), { usd: 4, estimated: false });
  assert.deepEqual(coldCost({ tokens: 500_000, model: 'claude-sonnet-5', ttl: '1h', prices }), { usd: 3, estimated: false });
  const est = coldCost({ tokens: 1_000_000, model: 'claude-sonnet-9[1m]', ttl: '5m', prices });
  assert.equal(est?.estimated, true);
  assert.equal(est?.usd, 4);
  assert.equal(coldCost({ tokens: 10, model: 'gpt-x', ttl: '5m', prices }), null);
});

test('coldCost: Haiku 5.5 cambia de tarifa de escritura al pasar de 100K; Sonnet no', () => {
  const p = { ...prices, 'claude-haiku-5-5': { input: 0.1, cache_write_5m: 0.125, cache_write_1h: 0.2, cache_read: 0.01, output: 0.5,
    above: { threshold: 100000, input: 0.5, cache_write_5m: 0.625, cache_write_1h: 1, cache_read: 0.05, output: 2.5 } } };
  assert.equal(coldCost({ tokens: 90_000, model: 'claude-haiku-5-5', ttl: '5m', prices: p })?.usd, 90_000 * 0.125 / 1e6);
  assert.equal(coldCost({ tokens: 100_000, model: 'claude-haiku-5-5', ttl: '5m', prices: p })?.usd, 100_000 * 0.125 / 1e6);
  assert.equal(coldCost({ tokens: 110_000, model: 'claude-haiku-5-5', ttl: '5m', prices: p })?.usd, 110_000 * 0.625 / 1e6);
  assert.equal(coldCost({ tokens: 110_000, model: 'claude-haiku-5-5', ttl: '1h', prices: p })?.usd, 110_000 * 1 / 1e6);
  assert.equal(coldCost({ tokens: 110_000, model: 'claude-sonnet-5', ttl: '5m', prices: p })?.usd, 110_000 * 4 / 1e6);
});

test('hitOf: read share of the whole input', () => {
  assert.equal(hitOf({ input_tokens: 10, cache_read_input_tokens: 80, cache_creation_input_tokens: 10 }), 80);
  assert.equal(hitOf({}), null);
});

test('inferTtl: survives a long pause -> 1h, never goes down', () => {
  assert.equal(inferTtl({ ttl: '5m', gapMs: 600000, readTokens: 90, prevTokens: 100 }), '1h');
  assert.equal(inferTtl({ ttl: '5m', gapMs: 60000, readTokens: 90, prevTokens: 100 }), '5m');
  assert.equal(inferTtl({ ttl: '5m', gapMs: 600000, readTokens: 0, prevTokens: 100 }), '5m');
  assert.equal(inferTtl({ ttl: '1h', gapMs: 1, readTokens: 0, prevTokens: 100 }), '1h');
});

test('expiryPlan: warns 60 s before; silent when the cold cost rounds to $0.00', () => {
  const p = expiryPlan({ lastTs: 0, ttlMs: 300000, now: 100000, coldUsd: 1 });
  assert.deepEqual(p, { warnInMs: 140000, expireInMs: 200000, silent: false });
  assert.equal(expiryPlan({ lastTs: 0, ttlMs: 300000, now: 100000, coldUsd: 0.004 }).silent, true);
  assert.equal(expiryPlan({ lastTs: 0, ttlMs: 300000, now: 250000 }).warnInMs, null);
  assert.equal(expiryPlan({ lastTs: 0, ttlMs: 300000, now: 400000 }).expireInMs, 0);
});

test('pickTtl: observed wins, then settings, then inference, then 5m', () => {
  const infer = { gapMs: 600000, readTokens: 90, prevTokens: 100 };
  assert.deepEqual(pickTtl({ observed: '5m', settings: '1h', infer }), { ttl: '5m', source: 'observed' });
  assert.deepEqual(pickTtl({ observed: null, settings: '1h' }), { ttl: '1h', source: 'settings' });
  assert.deepEqual(pickTtl({ observed: null, settings: null, infer }), { ttl: '1h', source: 'inferred' });
  assert.deepEqual(pickTtl({}), { ttl: '5m', source: 'default' });
});

test('cacheOf: shape for buildPanel, null without data', () => {
  assert.equal(cacheOf({ lastTs: null, ttl: '5m' }), null);
  assert.equal(cacheOf({ lastTs: 1, ttl: null }), null);
  const c = cacheOf({ lastTs: 0, ttl: '5m', hitPct: 90, tokens: 1_000_000, model: 'claude-sonnet-9', prices, now: 100000 });
  assert.deepEqual(c, { hitPct: 90, ttlLeftMs: 200000, ttlMs: 300000, coldCostUsd: 4, coldTokens: 1_000_000, estimated: true });
});

test('price-table: priceFor keeps working and the module has no node: imports', () => {
  assert.equal(priceFor('claude-sonnet-5-5')?.key !== undefined, true);
  assert.equal(priceIn(prices, normalizeModel('claude-sonnet-5-20260101'))?.estimated, false);
  const src = readFileSync(new URL('../core/price-table.mjs', import.meta.url), 'utf8');
  assert.doesNotMatch(src, /node:/);
  assert.doesNotMatch(readFileSync(new URL('../core/cache.mjs', import.meta.url), 'utf8'), /node:/);
});
