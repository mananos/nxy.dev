// @ts-check
/**
 * Golden of the 1.x presentation helpers (`core/format.mjs`) that `internal/format` ports. Node is
 * the oracle. JS null stands for the NaN the Go callers pass. The date and since sets depend on
 * the local zone, so each one is computed by a child node per zone (TZ=<zone>, see tz-child.mjs).
 * Sets: tokens, usd, usdpartial, estnote, pct, ratio, duration, date, gauge, colorize, severity,
 * table, clip, since, args.
 *
 *   node scripts/golden/format.mjs          write testdata/golden/format/*.json
 *   node scripts/golden/format.mjs --check  fail when they differ
 */
import { fileURLToPath } from 'node:url';
import { diffArea, loadCorpus, staleSets, writeArea } from './lib.mjs';
import { ZONES, inZone } from './tz-child.mjs';
import { clip, colorize, estimatedNote, fmtDate, fmtDuration, fmtPct, fmtRatio, fmtTokens, fmtUsd, fmtUsdPartial, gauge, parseArgs, parseSince, severity, table } from '../../core/format.mjs';

export const AREA = 'format';

const idx = (/** @type {number} */ i) => String(i).padStart(3, '0');
const label = (/** @type {any} */ v) => (Object.is(v, -0) ? '-0' : JSON.stringify(v));

/** One case per value: `{input: {x}, output: {s}}`. */
function unary(/** @type {string} */ name, /** @type {any[]} */ xs, /** @type {(x: any) => string} */ fn) {
  return xs.map((x, i) => ({ name: `${name}:${idx(i)}:${label(x)}`, input: { x }, output: { s: fn(x) } }));
}

/** @returns {Record<string, any[]>} */
export function generate() {
  /** @type {any} */
  const c = loadCorpus('format');

  const tokens = unary('tokens', c.numbers, fmtTokens);
  const usd = unary('usd', c.numbers, fmtUsd);
  const ratio = unary('ratio', c.numbers, fmtRatio);
  const duration = unary('duration', c.durations, fmtDuration);

  const pct = c.numbers.map((/** @type {any} */ x, /** @type {number} */ i) => ({
    name: `pct:${idx(i)}:${label(x)}`,
    input: { x, digits: c.digits },
    output: { s: c.digits.map((/** @type {number} */ d) => fmtPct(x, d)) },
  }));

  const usdpartial = c.partials.map((/** @type {any} */ x, /** @type {number} */ i) => {
    const flags = [];
    for (const partial of [false, true]) for (const estimated of [false, true]) flags.push({ partial, estimated });
    return {
      name: `usdpartial:${idx(i)}:${label(x)}`,
      input: { x, flags },
      output: { s: flags.map((f) => fmtUsdPartial(x, f.partial, f.estimated)) },
    };
  });

  const estnote = c.estimates.map((/** @type {[string, string][]} */ pairs, /** @type {number} */ i) => ({
    name: `estnote:${idx(i)}`,
    input: { pairs },
    output: { s: estimatedNote(Object.fromEntries(pairs)) },
  }));

  const gaugeSet = c.gauges.flatMap((/** @type {any} */ x, /** @type {number} */ i) =>
    c.cells.map((/** @type {number} */ cells) => ({
      name: `gauge:${idx(i)}:${label(x)}/${cells}`,
      input: { x, cells },
      output: { s: gauge(x, cells) },
    })),
  );

  const colorizeSet = c.styles.map((/** @type {any} */ style, /** @type {number} */ i) => ({
    name: `colorize:${idx(i)}:${label(style)}`,
    input: { style },
    output: { on: colorize('x', style, true), off: colorize('x', style, false) },
  }));

  const severitySet = c.severities.map((/** @type {any[]} */ [v, warn, crit], /** @type {number} */ i) => ({
    name: `severity:${idx(i)}:${label([v, warn, crit])}`,
    input: { v, warn, crit },
    output: { s: severity(v, warn === null ? NaN : warn, crit === null ? NaN : crit) ?? '' },
  }));

  const tableSet = c.tables.map((/** @type {any} */ t) => {
    const columns = t.cols.map((/** @type {any} */ col, /** @type {number} */ i) => ({ key: `c${i}`, label: col.label, align: col.align }));
    const rows = t.rows.map((/** @type {string[]} */ r) => Object.fromEntries(r.map((v, i) => [`c${i}`, v])));
    return { name: `table:${t.name}`, input: { cols: t.cols, rows: t.rows }, output: { s: table(rows, columns) } };
  });

  const clipSet = c.clips.map((/** @type {any[]} */ [s, max], /** @type {number} */ i) => ({
    name: `clip:${idx(i)}:${label(s)}/${max}`,
    input: { s, max },
    output: { s: clip(s, max) },
  }));

  const argsSet = c.argvs.map((/** @type {string[]} */ argv, /** @type {number} */ i) => ({
    name: `args:${idx(i)}:${label(argv)}`,
    input: { argv },
    output: parseArgs(argv),
  }));

  // fmtDate: the date getters in each zone; ts fractions too.
  const tsList = [...c.dates, ...c.datesFraction];
  const date = ZONES.map((zone) => ({
    name: `date:${zone}`,
    input: { zone, ts: tsList },
    output: {
      full: inZone(zone, 'core/format.mjs', 'fmtDate', tsList.map((ts) => [ts, { time: true }])),
      dateOnly: inZone(zone, 'core/format.mjs', 'fmtDate', tsList.map((ts) => [ts, { time: false }])),
    },
  }));

  // parseSince: relative spans do not depend on the zone, ISO date-times without offset do.
  const since = ZONES.map((zone) => ({
    name: `since:${zone}`,
    input: { zone, now: c.sinceNow, values: c.sinces },
    output: { ms: inZone(zone, 'core/format.mjs', 'parseSince', c.sinces.map((/** @type {any} */ s) => [s, c.sinceNow])) },
  }));

  return { tokens, usd, usdpartial, estnote, pct, ratio, duration, date, gauge: gaugeSet, colorize: colorizeSet, severity: severitySet, table: tableSet, clip: clipSet, since, args: argsSet };
}

/** Every set is written one case per line. */
const COMPACT = true;

/** @returns {string[]} differences, empty when up to date */
export function check() {
  const sets = generate();
  const diffs = [];
  for (const [set, cases] of Object.entries(sets)) diffs.push(...diffArea(AREA, set, cases, COMPACT));
  diffs.push(...staleSets(AREA, Object.keys(sets)));
  return diffs;
}

export function write() {
  return Object.entries(generate()).map(([set, cases]) => writeArea(AREA, set, cases, COMPACT));
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
