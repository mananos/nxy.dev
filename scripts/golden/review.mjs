// @ts-check
/**
 * Golden of the 1.x lens selection (`core/review.mjs`): parse + merge + select over the corpus.
 * Output per case: the lenses that applied and the files that triggered each (always-on: none).
 *
 *   node scripts/golden/review.mjs          write testdata/golden/lenses/cases.json
 *   node scripts/golden/review.mjs --check  fail when it differs
 */
import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { mergeLenses, parseLens, selectLenses } from '../../core/review.mjs';
import { ROOT, diffArea, loadCorpus, writeArea } from './lib.mjs';

export const AREA = 'lenses';

function builtIn() {
  const dir = join(ROOT, 'lenses');
  return readdirSync(dir)
    .filter((f) => f.endsWith('.md'))
    .sort()
    .map((f) => parseLens(readFileSync(join(dir, f), 'utf8'), f.slice(0, -3)))
    .filter((l) => l);
}

export function generate() {
  const base = /** @type {import('../../core/review.mjs').Lens[]} */ (builtIn());
  /** @type {any[]} */
  const corpus = loadCorpus('lenses');
  return corpus.map((c) => {
    const repo = /** @type {import('../../core/review.mjs').Lens[]} */ (
      (c.repo || []).map((/** @type {{name: string, text: string}} */ r) => parseLens(r.text, r.name)).filter((l) => l)
    );
    const files = (c.files || []).map((/** @type {{path: string, content: string|null}} */ f) => ({ path: f.path, content: f.content ?? null }));
    const selected = selectLenses(mergeLenses(base, repo), files);
    return {
      name: c.name,
      input: { repo: c.repo || [], files: c.files || [] },
      output: { lenses: selected.map((s) => ({ name: s.lens.name, files: s.files })) },
    };
  });
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  const cases = generate();
  if (process.argv.includes('--check')) {
    const diffs = diffArea(AREA, 'cases', cases);
    for (const d of diffs) console.error(d);
    if (diffs.length) process.exit(1);
    console.log('golden files up to date');
  } else {
    console.log(`wrote ${writeArea(AREA, 'cases', cases)}`);
  }
}
