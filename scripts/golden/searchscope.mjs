// @ts-check
/**
 * Golden of the 1.x search classifier (`core/search-scope.mjs`) that `internal/searchscope` ports.
 * Node is the oracle. classify: [tool, input, files?] (a string input of Bash/PowerShell is
 * `{command}`; isFile is `files.includes(p)`); broadmain: [tool, input]; locate: a command string.
 * history: {lines (objects, cwd "@ROOT@/proj"), files (under <root>/proj), threshold, sinceMs} run through
 * `broadSearchesInTranscript` of the 1.x (search-history.mjs) over a temp root. Sets: classify, broadmain, locate, history.
 *
 *   node scripts/golden/searchscope.mjs          write testdata/golden/searchscope/*.json
 *   node scripts/golden/searchscope.mjs --check  fail when they differ
 */
import { mkdirSync, mkdtempSync, realpathSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { broadSearchesInTranscript } from '../../hosts/claude-code/search-history.mjs';
import { LOCATE_NUDGE_MARK, READ_GATE_MARK, classifySearch, isBroadMainSearch, isLocateCommand } from '../../core/search-scope.mjs';
import { diffArea, loadCorpus, staleSets, writeArea } from './lib.mjs';

export const AREA = 'searchscope';

const idx = (/** @type {number} */ i) => String(i).padStart(3, '0');
const inputOf = (/** @type {string} */ tool, /** @type {any} */ input) =>
  typeof input === 'string' && (tool === 'Bash' || tool === 'PowerShell') ? { command: input } : input;
const label = (/** @type {any} */ input) => JSON.stringify(input ?? null).slice(0, 60);

/** @returns {Record<string, any[]>} */
export function generate() {
  /** @type {any} */
  const c = loadCorpus('searchscope');

  const classify = c.classify.map((/** @type {any[]} */ [tool, raw, files = []], /** @type {number} */ i) => {
    const input = inputOf(tool, raw);
    return {
      name: `classify:${idx(i)}:${tool}:${label(input)}`,
      input: { tool, input, files },
      output: { class: classifySearch(tool, input, { isFile: (p) => files.includes(p) }) },
    };
  });

  const broadmain = c.broadmain.map((/** @type {any[]} */ [tool, input], /** @type {number} */ i) => ({
    name: `broadmain:${idx(i)}:${tool}:${label(input)}`,
    input: { tool, input },
    output: { broad: isBroadMainSearch(tool, input) },
  }));

  const locate = c.locate.map((/** @type {string} */ cmd, /** @type {number} */ i) => ({
    name: `locate:${idx(i)}`,
    input: { cmd },
    output: { locate: isLocateCommand(cmd), nudge: LOCATE_NUDGE_MARK, gate: READ_GATE_MARK },
  }));

  const history = c.history.map((/** @type {any} */ h) => ({
    name: `history:${h.name}`,
    input: { lines: h.lines, files: h.files, threshold: h.threshold, sinceMs: h.sinceMs },
    output: { rows: runHistory(h) },
  }));

  return { classify, broadmain, locate, history };
}

/**
 * Materializes a history case under a fresh root (files under <root>/proj) and runs the 1.x over it.
 * @param {any} h
 */
function runHistory(h) {
  const root = realpathSync(mkdtempSync(join(tmpdir(), 'nxy-broad-')));
  try {
    const esc = JSON.stringify(root).slice(1, -1);
    for (const rel of h.files) {
      const p = join(root, 'proj', ...rel.split('/'));
      mkdirSync(dirname(p), { recursive: true });
      writeFileSync(p, 'x\n');
    }
    mkdirSync(join(root, 'proj'), { recursive: true });
    const text = h.lines.map((/** @type {any} */ l) => JSON.stringify(l).replaceAll('@ROOT@', esc)).join('\n') + '\n';
    const file = join(root, 'transcript.jsonl');
    writeFileSync(file, text);
    return broadSearchesInTranscript(file, { threshold: h.threshold, sinceMs: h.sinceMs });
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
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
