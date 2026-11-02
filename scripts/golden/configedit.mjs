// @ts-check
/**
 * Golden of the 1.x config schema helpers (`core/configedit.mjs`) that `internal/configedit` ports,
 * on the keys both versions share (modules.filter and the model/effort of planner, implementer,
 * reviewer, scout and documenter). Node is the oracle.
 *   validate: {field, value} -> {ok, reason (only when invalid)}
 *   setpath:  {obj, path, value} -> {obj: setPath}
 *   sources:  {field, files, env} -> {source: sourcesOf}
 * `has` of sourcesOf counts a key set to null as present only when it is not undefined, as the 1.x.
 *
 *   node scripts/golden/configedit.mjs          write testdata/golden/configedit/*.json
 *   node scripts/golden/configedit.mjs --check  fail when they differ
 */
import { fileURLToPath } from 'node:url';
import { setPath, sourcesOf, validate } from '../../core/configedit.mjs';
import { diffArea, loadCorpus, staleSets, writeArea } from './lib.mjs';

export const AREA = 'configedit';

const idx = (/** @type {number} */ i) => String(i).padStart(3, '0');

/** @returns {Record<string, any[]>} */
export function generate() {
  /** @type {any} */
  const c = loadCorpus('configedit');

  const validateCases = [];
  for (const field of [...c.fields, ...c.unknownFields]) {
    for (const value of c.values) {
      const r = validate(field, value);
      validateCases.push({
        name: `validate:${field || 'empty'}:${JSON.stringify(value)}`,
        input: { field, value },
        output: r.ok ? { ok: true } : { ok: false, reason: r.reason },
      });
    }
  }

  const setpath = c.setPath.map((/** @type {any} */ s, /** @type {number} */ i) => ({
    name: `setpath:${idx(i)}`,
    input: { obj: s.obj, path: s.path, value: s.value },
    output: { obj: setPath(s.obj, s.path, s.value) },
  }));

  const sources = [];
  let i = 0;
  for (const s of c.sources) {
    for (const field of c.sourceFields) {
      sources.push({ name: `sources:${idx(i)}:${field}`, input: { field, files: s.files, env: s.env }, output: { source: sourcesOf(field, s.files, s.env) } });
    }
    i++;
  }

  return { validate: validateCases, setpath, sources };
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
