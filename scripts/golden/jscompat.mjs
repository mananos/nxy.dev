// @ts-check
/**
 * Golden of the JS semantics the Go ports depend on (`internal/jscompat`): Date.parse, String(),
 * truthiness / `||`, UTF-16 length and slice, per-unit replace and localeCompare. Node is the oracle.
 * Sets: date, value, get, or, text, replace, collate. Date-times without an offset are left out of the
 * corpus on purpose (V8 reads them in local time, so the golden would depend on the machine).
 *
 *   node scripts/golden/jscompat.mjs          write testdata/golden/jscompat/*.json
 *   node scripts/golden/jscompat.mjs --check  fail when they differ
 */
import { fileURLToPath } from 'node:url';
import { diffArea, loadCorpus, staleSets, writeArea } from './lib.mjs';

export const AREA = 'jscompat';

/** `{"$undefined": true}` stands for JS undefined inside JSON data. */
const revive = (/** @type {any} */ v) => (v !== null && typeof v === 'object' && v.$undefined === true ? undefined : v);

const isNum = (/** @type {any} */ v) => typeof v === 'number' && Number.isFinite(v);

/** @returns {Record<string, any[]>} */
export function generate() {
  /** @type {any} */
  const corpus = loadCorpus('jscompat');

  const date = [
    ...corpus.dates.map((/** @type {string} */ s) => ({ name: `date:${JSON.stringify(s)}`, input: { value: s }, output: { ms: nullNaN(Date.parse(s)) } })),
    ...corpus.nonStringDates.map((/** @type {any} */ d) => ({ name: `nonstring:${d.name}`, input: { value: d.value }, output: { ms: null } })),
  ];

  const value = corpus.values.map((/** @type {any} */ c) => {
    const v = c.undefined ? undefined : c.value;
    return {
      name: c.name,
      input: c.undefined ? { undefined: true } : { value: c.value },
      output: { string: String(v), truthy: Boolean(v), num: isNum(v) ? v : null },
    };
  });

  const get = corpus.gets.map((/** @type {any} */ c) => {
    const r = c.keys.reduce((/** @type {any} */ a, /** @type {string} */ k) => a?.[k], c.obj);
    return {
      name: c.name,
      input: { obj: c.obj, keys: c.keys },
      output: { undefined: r === undefined, string: String(r), value: r === undefined ? null : r },
    };
  });

  const or = corpus.ors.map((/** @type {any} */ c) => {
    const items = c.items.map(revive);
    let r = items.length ? items[items.length - 1] : undefined;
    for (const it of items) {
      if (it) {
        r = it;
        break;
      }
    }
    return { name: c.name, input: { items: c.items }, output: { undefined: r === undefined, value: r === undefined ? null : r } };
  });

  const text = corpus.texts.map((/** @type {any} */ c) => {
    const s = c.s;
    const cuts = [];
    for (let n = 0; n <= s.length + 2; n++) {
      const p = s.slice(0, n);
      const last = p.length ? p.charCodeAt(p.length - 1) : 0;
      if (last >= 0xd800 && last <= 0xdbff) continue; // would split a pair: Go cannot hold a lone surrogate
      cuts.push(n);
    }
    return {
      name: c.name,
      input: { s, cuts },
      output: { length: s.length, prefixes: cuts.map((/** @type {number} */ n) => s.slice(0, n)) },
    };
  });

  const replace = corpus.replaces.map((/** @type {any} */ c) => ({
    name: c.name,
    input: { s: c.s },
    output: { slug: c.s.replace(/[^A-Za-z0-9]/g, '-') },
  }));

  // Explicit root locale so the golden is the same on any machine. The 1.x itself calls
  // localeCompare with the default locale; for the agent-type names it sorts (ASCII) 'und',
  // 'en-US' and 'es-AR' were checked to agree.
  const collate = [
    ...corpus.sorts.map((/** @type {any} */ c) => ({
      name: `sort:${c.name}`,
      input: { list: c.list },
      output: { sorted: [...c.list].sort((a, b) => a.localeCompare(b, 'und')) },
    })),
    {
      name: 'pairs',
      input: { names: corpus.pairs },
      output: {
        signs: corpus.pairs.flatMap((/** @type {string} */ a) => corpus.pairs.map((/** @type {string} */ b) => Math.sign(a.localeCompare(b, 'und')))),
      },
    },
  ];

  return { date, value, get, or, text, replace, collate };
}

function nullNaN(/** @type {number} */ n) {
  return Number.isNaN(n) ? null : n;
}

/** @returns {string[]} differences, empty when up to date */
export function check() {
  const sets = generate();
  const diffs = [];
  for (const [set, cases] of Object.entries(sets)) diffs.push(...diffArea(AREA, set, cases));
  diffs.push(...staleSets(AREA, Object.keys(sets)));
  return diffs;
}

export function write() {
  return Object.entries(generate()).map(([set, cases]) => writeArea(AREA, set, cases));
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
