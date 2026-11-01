// @ts-check
/**
 * Golden of the 1.x pricing (`core/price-table.mjs`, `core/pricing.mjs`): model ids, usage
 * normalisation and cost. Sets: models, usage, cost. Also keeps `internal/pricing/pricing.json`
 * (the go:embed copy of `core/pricing.json`) in sync.
 *
 *   node scripts/golden/pricing.mjs          write testdata/golden/pricing/*.json and the copy
 *   node scripts/golden/pricing.mjs --check  fail when they differ
 */
import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { priceIn, tierFor } from '../../core/price-table.mjs';
import {
  cacheHitPct,
  costFor,
  estimateBasis,
  isSyntheticModel,
  isZeroUsage,
  loadPricing,
  normalizeModel,
  familyOf,
  versionOf,
  priceFor,
  toUsage,
  totalInput,
} from '../../core/pricing.mjs';
import { ROOT, diffArea, loadCorpus, staleSets, writeArea } from './lib.mjs';

export const AREA = 'pricing';
const COPY = join(ROOT, 'internal', 'pricing', 'pricing.json');
const SOURCE = join(ROOT, 'core', 'pricing.json');

/** @returns {Record<string, any[]>} */
export function generate() {
  /** @type {any} */
  const corpus = loadCorpus('pricing');
  const ids = [...new Set([...Object.keys(loadPricing().models), ...corpus.models])];

  /** @type {any[]} */
  const models = ids.map((id) => {
    const r = priceFor(id);
    return {
      name: `id:${id}`,
      input: { model: id },
      output: {
        normalizeModel: normalizeModel(id),
        familyOf: familyOf(id),
        versionOf: versionOf(normalizeModel(id)),
        priceFor: r ? { key: r.key, estimated: r.estimated } : null,
        estimateBasis: estimateBasis(id),
        isSyntheticModel: isSyntheticModel(id),
      },
    };
  });
  for (const t of corpus.tables) {
    for (const id of t.ids) {
      const r = priceIn(t.models, id);
      models.push({
        name: `table:${t.name}:${id}`,
        input: { models: t.models, model: id },
        output: { priceIn: r ? { key: r.key, estimated: r.estimated, price: r.price } : null },
      });
    }
  }

  const usage = corpus.usages.map((/** @type {{name: string, raw: any}} */ u) => {
    const n = toUsage(u.raw);
    return {
      name: u.name,
      input: { raw: u.raw },
      output: { toUsage: n, isZeroUsage: isZeroUsage(n), totalInput: totalInput(n), cacheHitPct: cacheHitPct(n) },
    };
  });

  const cost = corpus.costs.map((/** @type {{name: string, model: string, usage: any, promptTokens?: number}} */ c) => {
    const n = toUsage(c.usage);
    const opts = c.promptTokens === undefined ? undefined : { promptTokens: c.promptTokens };
    const input = { model: c.model, usage: c.usage, ...(c.promptTokens === undefined ? {} : { promptTokens: c.promptTokens }) };
    return { name: c.name, input, output: { usage: n, costFor: costFor(c.model, n, opts) } };
  });
  for (const t of corpus.tiers) {
    const price = priceFor(t.model)?.price;
    if (!price) throw new Error(`tier case ${t.name}: no price for ${t.model}`);
    cost.push({
      name: t.name,
      input: { price, promptTokens: t.promptTokens },
      output: { tierFor: tierFor(price, t.promptTokens) },
    });
  }
  return { models, usage, cost };
}

function copyText() {
  return readFileSync(SOURCE, 'utf8').replace(/\r\n/g, '\n');
}

/** @returns {string[]} differences, empty when up to date */
export function check() {
  const sets = generate();
  const diffs = [];
  for (const [set, cases] of Object.entries(sets)) diffs.push(...diffArea(AREA, set, cases));
  diffs.push(...staleSets(AREA, Object.keys(sets)));
  const have = existsSync(COPY) ? readFileSync(COPY, 'utf8').replace(/\r\n/g, '\n') : null;
  if (have !== copyText()) diffs.push('internal/pricing/pricing.json: differs from core/pricing.json (node scripts/golden/pricing.mjs)');
  return diffs;
}

export function write() {
  const out = [];
  for (const [set, cases] of Object.entries(generate())) out.push(writeArea(AREA, set, cases));
  writeFileSync(COPY, copyText(), 'utf8');
  out.push(COPY);
  return out;
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
