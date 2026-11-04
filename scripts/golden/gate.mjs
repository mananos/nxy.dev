// @ts-check
/**
 * Golden of the 1.x gate that `internal/gate` ports. Node is the oracle. Sets:
 *  - readdecide: {ctx, sub, esc, cfg, kind, broad} -> decideReadGate (hasScout always true)
 *  - readmsg: {search, ctx, threshold, first} -> readGateMessage
 *  - summary: {rows} -> the `decisions:` / `context at edit:` / `searches:` lines that
 *    `hosts/claude-code/entries/gate.mjs status` prints over a ledger holding those rows
 *  - context: {lines, truncated} -> lastContextTokens (hosts/claude-code/context.mjs) over a transcript file
 *
 *   node scripts/golden/gate.mjs          write testdata/golden/gate/*.json
 *   node scripts/golden/gate.mjs --check  fail when they differ
 */
import { spawnSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, realpathSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { decideReadGate, readGateMessage } from '../../core/gate.mjs';
import { lastContextTokens } from '../../hosts/claude-code/context.mjs';
import { ROOT, diffArea, loadCorpus, staleSets, writeArea } from './lib.mjs';

export const AREA = 'gate';

const ENTRY = join(ROOT, 'hosts', 'claude-code', 'entries', 'gate.mjs');
const SUMMARY_PREFIXES = ['decisions:', 'context at edit:', 'searches:'];

/** @returns {Record<string, any[]>} */
export function generate() {
  /** @type {any} */
  const c = loadCorpus('gate');

  const readdecide = c.readdecide.map((/** @type {any} */ t) => ({
    name: `readdecide:${t.name}`,
    input: { ctx: t.ctx, sub: Boolean(t.sub), esc: Boolean(t.esc), cfg: t.cfg, kind: t.kind, broad: t.broad },
    output: {
      ...decideReadGate(
        { contextTokens: t.ctx, isSubagent: Boolean(t.sub), hasScout: true, escapeActive: Boolean(t.esc), search: { kind: t.kind, broad: t.broad } },
        t.cfg,
      ),
    },
  }));

  const readmsg = c.readmsg.map((/** @type {any} */ t) => ({
    name: `readmsg:${t.name}`,
    input: { search: t.search, ctx: t.ctx, threshold: t.threshold, first: Boolean(t.first) },
    output: { text: readGateMessage(t.search, t.ctx, t.threshold, { firstInSession: Boolean(t.first) }) },
  }));

  const summary = c.summary.map((/** @type {any} */ t) => ({
    name: `summary:${t.name}`,
    input: { rows: t.rows },
    output: { lines: runSummary(t.rows) },
  }));

  const context = c.context.map((/** @type {any} */ t) => ({
    name: `context:${t.name}`,
    input: { text: textOf(t.lines), truncated: Boolean(t.truncated) },
    output: { tokens: runContext(t.lines) },
  }));

  return { readdecide, readmsg, summary, context };
}

/** @param {any[]} lines objects are JSON, strings are raw lines */
function textOf(lines) {
  return lines.map((l) => (typeof l === 'string' ? l : JSON.stringify(l))).join('\n') + (lines.length ? '\n' : '');
}

/** @param {any[]} lines */
function runContext(lines) {
  const root = realpathSync(mkdtempSync(join(tmpdir(), 'nxy-gate-ctx-')));
  try {
    const file = join(root, 'transcript.jsonl');
    writeFileSync(file, textOf(lines));
    return lastContextTokens(file);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
}

/** @param {any[]} rows */
function runSummary(rows) {
  const root = realpathSync(mkdtempSync(join(tmpdir(), 'nxy-gate-sum-')));
  try {
    const cwd = join(root, 'proj');
    const home = join(root, 'home');
    mkdirSync(join(cwd, '.nxy', 'local', 'metrics'), { recursive: true });
    mkdirSync(home, { recursive: true });
    if (rows.length) writeFileSync(join(cwd, '.nxy', 'local', 'metrics', 'gate.jsonl'), rows.map((r) => JSON.stringify(r)).join('\n') + '\n');
    const r = spawnSync(process.execPath, [ENTRY, 'status', '--cwd', cwd], {
      cwd: root,
      encoding: 'utf8',
      windowsHide: true,
      env: { ...process.env, NXY_HOME: home, HOME: home, USERPROFILE: home },
    });
    if (r.status !== 0) throw new Error(`gate.mjs status failed: ${r.stderr}`);
    return r.stdout
      .split(/\r?\n/)
      .filter((l) => SUMMARY_PREFIXES.some((p) => l.startsWith(p)));
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
