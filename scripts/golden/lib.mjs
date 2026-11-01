// @ts-check
/**
 * Helpers shared by the golden generators: corpus I/O, deterministic golden files,
 * and the two flavours of `rewrite` (replay a recorded rtk answer / record a real one).
 */
import { existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { rtkRewrite } from '../../core/filter/rtk.mjs';

export const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..');
export const CORPUS_DIR = join(ROOT, 'scripts', 'golden', 'corpus');
export const GOLDEN_DIR = join(ROOT, 'testdata', 'golden');

/**
 * @typedef {{code: number, stdout: string}} RtkAnswer
 * @typedef {RtkAnswer | 'throw'} RtkEntry
 * @typedef {{name: string, input: any, output: any}} GoldenCase
 */

/** @param {string} name  corpus file without extension */
export function loadCorpus(name) {
  return JSON.parse(readFileSync(join(CORPUS_DIR, `${name}.json`), 'utf8'));
}

/** Stable text of a JSON value: 2 spaces, LF, trailing newline. */
export function toJson(value) {
  return `${JSON.stringify(value, null, 2)}\n`;
}

/** Compact text of a golden file: one case per line, LF, trailing newline (same content, smaller). */
export function toCompactJson(cases) {
  return `[\n${cases.map((c) => JSON.stringify(c)).join(',\n')}\n]\n`;
}

/**
 * @param {string} name
 * @param {any} data
 */
export function saveCorpus(name, data) {
  writeFileSync(join(CORPUS_DIR, `${name}.json`), toJson(data), 'utf8');
}

/** Cases sorted by name (code-unit order, locale independent); names must be unique. */
function sortedCases(cases) {
  const out = [...cases].sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));
  for (let i = 1; i < out.length; i++) {
    if (out[i].name === out[i - 1].name) throw new Error(`duplicate golden case name: ${out[i].name}`);
  }
  return out;
}

function areaFile(area, set) {
  return join(GOLDEN_DIR, area, `${set}.json`);
}

/**
 * @param {string} area
 * @param {string} set
 * @param {GoldenCase[]} cases
 * @param {boolean} [compact]  one case per line, no indentation
 * @returns {string} written path
 */
export function writeArea(area, set, cases, compact = false) {
  const p = areaFile(area, set);
  mkdirSync(dirname(p), { recursive: true });
  const sorted = sortedCases(cases);
  writeFileSync(p, compact ? toCompactJson(sorted) : toJson(sorted), 'utf8');
  return p;
}

/**
 * Compares what `writeArea` would write with what is on disk.
 * @param {string} area
 * @param {string} set
 * @param {GoldenCase[]} cases
 * @param {boolean} [compact]  the file is written compact (see writeArea)
 * @returns {string[]} human readable differences, empty when identical
 */
export function diffArea(area, set, cases, compact = false) {
  const p = areaFile(area, set);
  const want = sortedCases(cases);
  if (!existsSync(p)) return [`${area}/${set}.json: missing (run node scripts/golden/filter.mjs)`];
  const text = readFileSync(p, 'utf8').replace(/\r\n/g, '\n');
  if (text === (compact ? toCompactJson(want) : toJson(want))) return [];
  let have = [];
  try {
    have = JSON.parse(text);
  } catch {
    return [`${area}/${set}.json: not valid JSON`];
  }
  const diffs = [];
  const haveByName = new Map(have.map((c) => [c.name, JSON.stringify(c)]));
  const wantNames = new Set();
  for (const c of want) {
    wantNames.add(c.name);
    const h = haveByName.get(c.name);
    if (h === undefined) diffs.push(`${area}/${set}.json: new case ${c.name}`);
    else if (h !== JSON.stringify(c)) diffs.push(`${area}/${set}.json: changed case ${c.name}`);
  }
  for (const n of haveByName.keys()) if (!wantNames.has(n)) diffs.push(`${area}/${set}.json: stale case ${n}`);
  return diffs.length ? diffs : [`${area}/${set}.json: formatting differs`];
}

/** Golden files of an area that no corpus set produces anymore. */
export function staleSets(area, sets) {
  const dir = join(GOLDEN_DIR, area);
  if (!existsSync(dir)) return [];
  return readdirSync(dir)
    .filter((f) => f.endsWith('.json') && !sets.includes(f.slice(0, -5)))
    .map((f) => `${area}/${f}: no corpus set produces it`);
}

/**
 * `rewrite` that replays a recorded rtk answer and remembers what it was asked.
 * @param {RtkEntry} entry
 * @returns {((cmd: string) => RtkAnswer) & {state: {asked: string | null}}}
 */
export function replayRewrite(entry) {
  const state = { asked: /** @type {string | null} */ (null) };
  const fn = (/** @type {string} */ cmd) => {
    state.asked = cmd;
    if (entry === 'throw') throw new Error('rtk unavailable');
    return { code: entry.code, stdout: entry.stdout };
  };
  return Object.assign(fn, { state });
}

/**
 * `rewrite` that asks the real rtk and keeps `{asked, code, stdout}` (or `entry: 'throw'`).
 * @param {string} rtkPath
 * @returns {((cmd: string) => RtkAnswer) & {state: {asked: string | null, entry: RtkEntry | null}}}
 */
export function recordingRewrite(rtkPath) {
  const state = { asked: /** @type {string | null} */ (null), entry: /** @type {RtkEntry | null} */ (null) };
  const fn = (/** @type {string} */ cmd) => {
    state.asked = cmd;
    try {
      const r = rtkRewrite(rtkPath, cmd);
      state.entry = { code: r.code, stdout: r.stdout };
      return r;
    } catch (e) {
      state.entry = 'throw';
      throw e;
    }
  };
  return Object.assign(fn, { state });
}
