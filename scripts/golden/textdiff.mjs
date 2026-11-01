// @ts-check
/**
 * Golden of the 1.x line diff (`core/diff.mjs`) that `internal/textdiff` ports. Node is the oracle.
 * Sets: lines (diffLines), hunks, file (fileDiff), big (a middle above MAX_LCS_CELLS, generated from a
 * spec and recorded as counts, ranges, text length and sha256 to keep the file small).
 *
 *   node scripts/golden/textdiff.mjs          write testdata/golden/textdiff/*.json
 *   node scripts/golden/textdiff.mjs --check  fail when they differ
 */
import { createHash } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { diffLines, fileDiff, hunks } from '../../core/diff.mjs';
import { diffArea, loadCorpus, staleSets, writeArea } from './lib.mjs';

export const AREA = 'textdiff';

const idx = (/** @type {number} */ i) => String(i).padStart(3, '0');
const text = (/** @type {string} */ prefix, /** @type {number} */ n) => Array.from({ length: n }, (_, i) => `${prefix}${i}\n`).join('');

/** @returns {Record<string, any[]>} */
export function generate() {
  /** @type {any} */
  const c = loadCorpus('textdiff');

  const lines = c.lines.map((/** @type {[string, string]} */ [before, after], /** @type {number} */ i) => ({
    name: `lines:${idx(i)}`,
    input: { before, after },
    output: { ops: diffLines(before, after) },
  }));

  const hunkSet = c.hunks.map((/** @type {any} */ x, /** @type {number} */ i) => ({
    name: `hunks:${idx(i)}:ctx${x.context}`,
    input: x,
    output: { hunks: hunks(diffLines(x.before, x.after), x.context) },
  }));

  const file = c.file.map((/** @type {any} */ x, /** @type {number} */ i) => ({
    name: `file:${idx(i)}:${x.label}`,
    input: x,
    output: fileDiff(x.label, x.before, x.after),
  }));

  const big = c.big.map((/** @type {any} */ x, /** @type {number} */ i) => {
    const r = fileDiff('big.txt', text(x.beforePrefix, x.n), text(x.afterPrefix, x.m));
    return {
      name: `big:${idx(i)}`,
      input: x,
      output: {
        added: r.added,
        removed: r.removed,
        ranges: r.ranges,
        textLen: [...r.text].length,
        textSha256: createHash('sha256').update(r.text, 'utf8').digest('hex'),
      },
    };
  });

  return { lines, hunks: hunkSet, file, big };
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
