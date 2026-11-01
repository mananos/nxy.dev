// @ts-check
/**
 * Golden of the JS semantics the Go ports depend on (`internal/jscompat`): Date.parse, String(),
 * truthiness / `||`, UTF-16 length and slice, per-unit replace and localeCompare. Node is the oracle.
 * Sets: date, value, get, or, text, replace, collate, tofixed, round, tonumber, pad, space, localdate,
 * parselocal, stringify, trimstart, utf16cmp, utf8decode. The `date` set leaves date-times without an offset out on purpose (V8 reads
 * them in local time); `localdate` and `parselocal` cover local time through one child node per zone.
 *
 *   node scripts/golden/jscompat.mjs          write testdata/golden/jscompat/*.json
 *   node scripts/golden/jscompat.mjs --check  fail when they differ
 */
import { fileURLToPath } from 'node:url';
import { diffArea, loadCorpus, staleSets, writeArea } from './lib.mjs';
import { ZONES, inZone } from './tz-child.mjs';

export const AREA = 'jscompat';

/** Sets written one case per line (the large ones). */
const COMPACT = new Set(['tofixed', 'tonumber', 'localdate', 'parselocal', 'stringify']);

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

  // toFixed: hand-picked ties and edges, then LCG doubles (magnitudes 1e-3..1e6, half of them rounded
  // to 3 decimals and some to eighths so exact binary ties show up), each at 0..3 digits.
  const toNum = (/** @type {any} */ x) => (x === null ? NaN : Number(x));
  const tofixedCase = (/** @type {string} */ name, /** @type {any} */ x) => ({
    name,
    input: { x, digits: [0, 1, 2, 3] },
    output: { s: [0, 1, 2, 3].map((d) => toNum(x).toFixed(d)) },
  });
  let seed = 12345;
  const next = () => (seed = (Math.imul(seed, 1664525) + 1013904223) >>> 0) / 2 ** 32;
  const tofixed = [
    ...corpus.tofixed.map((/** @type {any} */ x, /** @type {number} */ i) => tofixedCase(`tofixed:h${String(i).padStart(3, '0')}:${String(x)}`, x)),
  ];
  for (let i = 0; i < 400; i++) {
    let x = (next() - 0.5) * 10 ** ((i % 10) - 3);
    if (i % 2) x = Math.round(x * 1000) / 1000;
    if (i % 5 === 0) x = Math.round(next() * 2000) / 8;
    tofixed.push(tofixedCase(`tofixed:l${String(i).padStart(3, '0')}:${String(x)}`, x));
  }

  const round = corpus.round.map((/** @type {any} */ x, /** @type {number} */ i) => {
    const r = Math.round(toNum(x));
    return { name: `round:${String(i).padStart(2, '0')}:${String(x)}`, input: { x }, output: { s: String(r), negzero: Object.is(r, -0) } };
  });

  const tonumber = corpus.tonumber.map((/** @type {any} */ v, /** @type {number} */ i) => ({
    name: `tonumber:${String(i).padStart(2, '0')}:${JSON.stringify(v)}`,
    input: { value: v },
    output: { s: String(Number(v)) },
  }));

  const pad = corpus.pads.map((/** @type {any} */ c, /** @type {number} */ i) => ({
    name: `pad:${String(i).padStart(2, '0')}:${JSON.stringify(c.s)}/${c.n}/${JSON.stringify(c.fill)}`,
    input: c,
    output: { start: c.s.padStart(c.n, c.fill), end: c.s.padEnd(c.n, c.fill) },
  }));

  const space = corpus.spaces.map((/** @type {string} */ s, /** @type {number} */ i) => ({
    name: `space:${String(i).padStart(2, '0')}:${JSON.stringify(s)}`,
    input: { s },
    output: { collapsed: s.replace(/\s+/g, ' '), trim: s.trim(), trimEnd: s.trimEnd() },
  }));

  const trimstart = corpus.trimStarts.map((/** @type {string} */ s, /** @type {number} */ i) => ({
    name: `trimstart:${String(i).padStart(2, '0')}:${JSON.stringify(s)}`,
    input: { s },
    output: { trimStart: s.trimStart() },
  }));

  // Default sort() and < compare UTF-16 code units, not bytes: that is the order the Go side must reproduce.
  const utf16cmp = corpus.utf16Lists.map((/** @type {string[]} */ list, /** @type {number} */ i) => ({
    name: `utf16cmp:${String(i).padStart(2, '0')}`,
    input: { list },
    output: {
      signs: list.flatMap((a) => list.map((b) => (a < b ? -1 : a > b ? 1 : 0))),
      sorted: [...list].sort(),
    },
  }));

  // readFileSync(path, 'utf8') = Buffer#toString('utf8'): how bytes that are not valid UTF-8 decode.
  const utf8decode = corpus.utf8Hex.map((/** @type {string} */ hex, /** @type {number} */ i) => ({
    name: `utf8decode:${String(i).padStart(2, '0')}:${hex}`,
    input: { hex },
    output: { text: Buffer.from(hex, 'hex').toString('utf8') },
  }));

  // Date getters and offset-less Date.parse, computed by a child node per zone (TZ=<zone>).
  const instants = corpus.localInstants.map((/** @type {string} */ s) => Date.parse(s));
  const ms = [...corpus.localMs, ...instants];
  const localdate = ZONES.map((zone) => ({
    name: `localdate:${zone}`,
    input: { zone, ms },
    output: { parts: inZone(zone, 'scripts/golden/tz-child.mjs', 'localParts', ms.map((/** @type {any} */ m) => [m])) },
  }));
  const parselocal = ZONES.map((zone) => ({
    name: `parselocal:${zone}`,
    input: { zone, values: corpus.localDates },
    output: { ms: inZone(zone, 'scripts/golden/tz-child.mjs', 'parseLocal', corpus.localDates.map((/** @type {string} */ s) => [s])) },
  }));

  const stringify = corpus.stringifyIndents.flatMap((/** @type {string} */ indent) =>
    corpus.stringify.map((/** @type {any} */ c) => ({
      name: `stringify:${c.name}|indent=${JSON.stringify(indent)}`,
      input: { raw: c.raw, indent },
      output: { text: JSON.stringify(JSON.parse(c.raw), null, indent || undefined) },
    })),
  );

  return { date, value, get, or, text, replace, collate, tofixed, round, tonumber, pad, space, localdate, parselocal, stringify, trimstart, utf16cmp, utf8decode };
}

function nullNaN(/** @type {number} */ n) {
  return Number.isNaN(n) ? null : n;
}

/** @returns {string[]} differences, empty when up to date */
export function check() {
  const sets = generate();
  const diffs = [];
  for (const [set, cases] of Object.entries(sets)) diffs.push(...diffArea(AREA, set, cases, COMPACT.has(set)));
  diffs.push(...staleSets(AREA, Object.keys(sets)));
  return diffs;
}

export function write() {
  return Object.entries(generate()).map(([set, cases]) => writeArea(AREA, set, cases, COMPACT.has(set)));
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
