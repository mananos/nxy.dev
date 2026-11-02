// @ts-check
/**
 * Golden of the 1.x `describeEngine` (core/filter/engine.mjs) that `internal/filter` ports. Node is the oracle.
 *   describe: {info, engine} -> {text}   (info = EngineInfo as the 1.x caches it; engine = config filter.engine)
 * Sets: describe.
 *
 *   node scripts/golden/filterstatus.mjs          write testdata/golden/filterstatus/*.json
 *   node scripts/golden/filterstatus.mjs --check  fail when they differ
 */
import { fileURLToPath } from 'node:url';
import { describeEngine } from '../../core/filter/engine.mjs';
import { diffArea, loadCorpus, staleSets, writeArea } from './lib.mjs';

export const AREA = 'filterstatus';

/** @returns {Record<string, any[]>} */
export function generate() {
  /** @type {any} */
  const c = loadCorpus('filterstatus');
  const describe = c.describe.map((/** @type {[any, string]} */ [info, engine], /** @type {number} */ i) => ({
    name: `describe:${String(i).padStart(3, '0')}:${info.engine}:${info.reason}`,
    input: { info, engine },
    output: { text: describeEngine(info, { filter: { engine } }) },
  }));
  return { describe };
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
