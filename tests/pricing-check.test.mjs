// @ts-check
/**
 * What has to hold for the maintainer pricing check: model names from the page become table ids, the
 * "Model pricing" and "Fast mode pricing" tables are parsed (footnotes ignored), a renamed column or a
 * missing heading fails loudly instead of comparing half a table, differences are listed as missing /
 * changed / gone, and the CLI exits 2 on a broken page. All offline: fixtures are inline.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { modelIdFromName, parsePricingDoc, comparePricing } from '../scripts/pricing-check.mjs';

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..');

const HEADER = '| Model | Base input tokens | 5m cache writes | 1h cache writes | Cache hits and refreshes | Output tokens |';
const ALIGN = '|---|---|---|---|---|---|';
const page = (header = HEADER) => `# Pricing

## Model pricing

${header}
${ALIGN}
| Claude Opus 5.5 | $4 / MTok | $5 / MTok | $8 / MTok | $0.20 / MTok<sup>2</sup> | $20 / MTok |
| Claude Opus 5 | $5 / MTok | $6.25 / MTok | $10 / MTok | $0.50 / MTok | $25 / MTok |
| Claude Opus 4.8 | $5 / MTok | $6.25 / MTok | $10 / MTok | $0.50 / MTok | $25 / MTok |

### Fast mode pricing

| Model | Input | Output |
|---|---|---|
| Claude Opus 5.5 | $8 / MTok | $40 / MTok |
| Claude Opus 5 / Claude Opus 4.8 | $10 / MTok | $50 / MTok |
`;

test('modelIdFromName turns page names into table ids', () => {
  assert.equal(modelIdFromName('Claude Opus 5.5'), 'claude-opus-5-5');
  assert.equal(modelIdFromName('Claude Opus 4.1 ([retired, except on Bedrock and Google Cloud](https://x))'), 'claude-opus-4-1');
  assert.equal(modelIdFromName('Claude Mythos 5.1 ([limited availability](https://x))'), 'claude-mythos-5-1');
});

test('parsePricingDoc reads prices and attaches fast mode', () => {
  const m = parsePricingDoc(page());
  assert.deepEqual(m['claude-opus-5-5'], {
    input: 4, cache_write_5m: 5, cache_write_1h: 8, cache_read: 0.2, output: 20, fast: { input: 8, output: 40 },
  });
  assert.deepEqual(m['claude-opus-5'].fast, { input: 10, output: 50 });
  assert.deepEqual(m['claude-opus-4-8'].fast, { input: 10, output: 50 });
});

test('parsePricingDoc throws on a missing heading or a renamed column', () => {
  assert.throws(() => parsePricingDoc('# Pricing\n\nnothing here\n'), /Model pricing/);
  const renamed = HEADER.replace('5m cache writes', 'Cache reads');
  assert.throws(() => parsePricingDoc(page(renamed)), /columns/);
});

test('comparePricing lists missing, changed and gone', () => {
  const p = { input: 1, cache_write_5m: 1, cache_write_1h: 1, cache_read: 0.2, output: 1 };
  const doc = { a: { ...p }, b: { ...p, fast: { input: 2, output: 3 } } };
  assert.deepEqual(comparePricing(doc, { a: { ...p }, b: { ...p, fast: { input: 2, output: 3 } } }), { missing: [], changed: [], gone: [] });

  assert.equal(comparePricing(doc, { a: { ...p } }).missing.length, 1);
  assert.ok(comparePricing(doc, { a: { ...p } }).missing[0].startsWith('b '));

  const diff = comparePricing({ a: { ...p } }, { a: { ...p, cache_read: 0.5 } });
  assert.deepEqual(diff.changed, ['a.cache_read: 0.5 → 0.2']);

  const noFast = comparePricing(doc, { a: { ...p }, b: { ...p } });
  assert.ok(noFast.changed.some((l) => l.includes('fast: missing')));

  assert.deepEqual(comparePricing({ a: { ...p } }, { a: { ...p }, old: { ...p } }).gone, ['old']);
});

test('CLI exits 2 and says "format changed" on a broken page', () => {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-pricing-'));
  try {
    const file = join(dir, 'page.md');
    writeFileSync(file, page(HEADER.replace('5m cache writes', 'Cache reads')));
    const r = spawnSync(process.execPath, ['scripts/pricing-check.mjs', '--file', file], { cwd: ROOT, encoding: 'utf8' });
    assert.equal(r.status, 2);
    assert.match(r.stderr, /format changed/);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});
