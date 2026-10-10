// @ts-check
/**
 * Maintainer check, never run by the plugin: downloads Anthropic's pricing page and lists what differs
 * from core/pricing.json — a model the table lacks, a price that changed, a fast-mode rate missing.
 * It never writes: you decide what to copy. `npm run pricing:check` (or `-- --file page.md` offline).
 * Exit 0 = same, 1 = differences, 2 = the page could not be read or its format changed.
 */
import { readFileSync, realpathSync } from 'node:fs';
import { pathToFileURL } from 'node:url';
import { loadPricing } from '../core/pricing.mjs';

export const PRICING_URL = 'https://platform.claude.com/docs/en/about-claude/pricing.md';

/** Columns of the "Model pricing" table, in page order → key in core/pricing.json. */
const COLUMNS = [
  [/^base input/i, 'input'],
  [/^5m cache writes/i, 'cache_write_5m'],
  [/^1h cache writes/i, 'cache_write_1h'],
  [/^cache hits/i, 'cache_read'],
  [/^output/i, 'output'],
];
const FIELDS = COLUMNS.map(([, k]) => /** @type {string} */ (k));

/** `Claude Opus 4.1 ([retired, …](…))` → `claude-opus-4-1`. */
export function modelIdFromName(name) {
  const bare = name.replace(/\(\[.*$/, '').replace(/\(.*?\)/g, '').trim();
  return bare.toLowerCase().replace(/\./g, '-').replace(/\s+/g, '-');
}

/**
 * `Claude Haiku 5.5 (for prompts over 100,000 tokens)` → { threshold: 100000, tier: 'above' }; `up to` → 'base'.
 * Null when the name has no such parenthesis.
 * @returns {{threshold: number, tier: 'base'|'above'} | null}
 */
export function tierOfName(name) {
  const m = /\(\s*for prompts\s+(up to|over)\s+([\d,]+)\s+tokens\s*\)/i.exec(name);
  if (!m) return null;
  return { threshold: Number(m[2].replace(/,/g, '')), tier: m[1].toLowerCase() === 'over' ? 'above' : 'base' };
}

/** `$12.50 / MTok<sup>1</sup>` → 12.5; anything else → null. */
function parseUsd(cell) {
  const m = /^\$\s*([\d.]+)\s*\/\s*MTok/i.exec(cell.replace(/<sup>.*?<\/sup>/gi, '').trim());
  return m ? Number(m[1]) : null;
}

/** Cells of a Markdown table row, or null if the line is not one. */
function cells(line) {
  const t = line.trim();
  if (!t.startsWith('|')) return null;
  return t.replace(/^\||\|$/g, '').split('|').map((c) => c.trim());
}

/** First table after the heading that matches `heading`: { header, rows } or null. */
function tableAfter(lines, heading) {
  const start = lines.findIndex((l) => heading.test(l));
  if (start < 0) return null;
  let i = start + 1;
  while (i < lines.length && !cells(lines[i])) {
    if (/^#{1,3}\s/.test(lines[i])) return null;
    i++;
  }
  const header = cells(lines[i] || '');
  if (!header) return null;
  const rows = [];
  for (i += 2; i < lines.length; i++) {
    const c = cells(lines[i]);
    if (!c) break;
    rows.push(c);
  }
  return { header, rows };
}

/**
 * Prices from the page. Throws with the reason when the format is not the expected one — the check
 * must fail loudly rather than compare against half a table.
 * @param {string} md
 * @returns {Record<string, Record<string, any>>}
 */
export function parsePricingDoc(md) {
  const lines = md.split(/\r?\n/);
  const main = tableAfter(lines, /^##\s+Model pricing\s*$/i);
  if (!main) throw new Error('no "## Model pricing" table on the page');
  const cols = main.header.slice(1);
  if (cols.length !== COLUMNS.length || !COLUMNS.every(([re], i) => /** @type {RegExp} */ (re).test(cols[i]))) {
    throw new Error(`the "Model pricing" columns changed: ${main.header.join(' | ')}`);
  }
  /** @type {Record<string, Record<string, any>>} */
  const models = {};
  /** @type {Record<string, Record<string, any>>} */
  const tiers = {};
  /** @type {Record<string, number>} */
  const baseThreshold = {};
  for (const row of main.rows) {
    const id = modelIdFromName(row[0]);
    /** @type {Record<string, any>} */
    const price = {};
    FIELDS.forEach((k, i) => {
      const v = parseUsd(row[i + 1] || '');
      if (v === null) throw new Error(`unreadable price for ${row[0]}: "${row[i + 1]}"`);
      price[k] = v;
    });
    const t = tierOfName(row[0]);
    if (!t || t.tier === 'base') {
      models[id] = price;
      if (t) baseThreshold[id] = t.threshold;
    } else tiers[id] = { threshold: t.threshold, ...price };
  }
  for (const [id, above] of Object.entries(tiers)) {
    if (!models[id]) throw new Error(`tier without base row: ${id}`);
    if (baseThreshold[id] !== undefined && baseThreshold[id] !== above.threshold) throw new Error(`tier thresholds differ: ${id}`);
    models[id].above = above;
  }
  for (const row of main.rows) {
    if (/\((?=[^)]*\btokens\b)[^)]*\)/i.test(row[0]) && !tierOfName(row[0])) throw new Error(`unreadable tier label: ${row[0]}`);
  }
  if (!Object.keys(models).length) throw new Error('the "Model pricing" table is empty');

  const fast = tableAfter(lines, /^###\s+Fast mode pricing\s*$/i);
  if (fast) {
    for (const row of fast.rows) {
      const input = parseUsd(row[1] || '');
      const output = parseUsd(row[2] || '');
      if (input === null || output === null) throw new Error(`unreadable fast-mode price for ${row[0]}`);
      for (const name of row[0].split('/')) {
        const id = modelIdFromName(name);
        if (models[id]) models[id].fast = { input, output };
      }
    }
  }
  return models;
}

/**
 * What differs between the page and the table, as lines a person can act on.
 * @param {Record<string, Record<string, any>>} doc
 * @param {Record<string, Record<string, any>>} table
 */
export function comparePricing(doc, table) {
  const missing = [];
  const changed = [];
  const gone = [];
  for (const [id, p] of Object.entries(doc)) {
    const t = table[id];
    if (!t) {
      missing.push(`${id}  ${JSON.stringify(p)}`);
      continue;
    }
    for (const k of FIELDS) if (t[k] !== p[k]) changed.push(`${id}.${k}: ${t[k]} → ${p[k]}`);
    if (p.fast && !t.fast) changed.push(`${id}.fast: missing → ${JSON.stringify(p.fast)}`);
    else if (p.fast && (t.fast.input !== p.fast.input || t.fast.output !== p.fast.output)) {
      changed.push(`${id}.fast: ${JSON.stringify(t.fast)} → ${JSON.stringify(p.fast)}`);
    } else if (!p.fast && t.fast) changed.push(`${id}.fast: ${JSON.stringify(t.fast)} → not on the page`);
    if (p.above && !t.above) changed.push(`${id}.above: missing → ${JSON.stringify(p.above)}`);
    else if (p.above) {
      for (const k of ['threshold', ...FIELDS]) if (t.above[k] !== p.above[k]) changed.push(`${id}.above.${k}: ${t.above[k]} → ${p.above[k]}`);
    } else if (t.above) changed.push(`${id}.above: ${JSON.stringify(t.above)} → not on the page`);
  }
  for (const id of Object.keys(table)) if (!doc[id]) gone.push(id);
  return { missing, changed, gone };
}

/** @param {string[]} argv */
async function run(argv) {
  const fileIdx = argv.indexOf('--file');
  let md;
  try {
    if (fileIdx >= 0) md = readFileSync(argv[fileIdx + 1], 'utf8');
    else {
      const res = await fetch(PRICING_URL, { signal: AbortSignal.timeout(20_000) });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      md = await res.text();
    }
  } catch (e) {
    console.error(`pricing:check — could not read ${fileIdx >= 0 ? argv[fileIdx + 1] : PRICING_URL}: ${/** @type {Error} */ (e).message}`);
    return 2;
  }
  let doc;
  try {
    doc = parsePricingDoc(md);
  } catch (e) {
    console.error(`pricing:check — the page format changed, nothing compared: ${/** @type {Error} */ (e).message}`);
    return 2;
  }
  const { missing, changed, gone } = comparePricing(doc, loadPricing().models);
  const out = [`pricing:check — ${Object.keys(doc).length} models on the page vs core/pricing.json`];
  if (missing.length) out.push('', 'Missing in core/pricing.json:', ...missing.map((l) => `  ${l}`));
  if (changed.length) out.push('', 'Different price:', ...changed.map((l) => `  ${l}`));
  if (gone.length) out.push('', 'In core/pricing.json but not on the page (keep them: old transcripts use them):', ...gone.map((l) => `  ${l}`));
  if (!missing.length && !changed.length) out.push('', 'Same prices. Nothing to update.');
  else out.push('', 'Nothing was written. Copy what applies to core/pricing.json and update "_fetched".');
  console.log(out.join('\n'));
  return missing.length || changed.length ? 1 : 0;
}

function isMain() {
  if (!process.argv[1]) return false;
  try {
    return import.meta.url === pathToFileURL(realpathSync(process.argv[1])).href;
  } catch {
    return false;
  }
}

if (isMain()) run(process.argv.slice(2)).then((code) => process.exit(code));
