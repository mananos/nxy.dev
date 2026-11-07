// @ts-check
/**
 * Generates the golden files of the 1.x filter (function level): shell, rtkcmd, permissions, filter.
 *
 *   node scripts/golden/filter.mjs            write testdata/golden/<area>/<set>.json
 *   node scripts/golden/filter.mjs --check    regenerate in memory, exit 1 on any difference
 *   node scripts/golden/filter.mjs --record   also ask the real rtk for cases without an `rtk` entry
 *
 * The Go port (internal/filter) must reproduce every file exactly.
 */
import { existsSync } from 'node:fs';
import { join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { writeTargets } from '../../core/shellwrites.mjs';
import { decide } from '../../core/filter/decide.mjs';
import { detectRtkHook, findRtk } from '../../core/filter/engine.mjs';
import { commandKind } from '../../core/filter/kind.mjs';
import { isRtkInvocation, normalizeForRtk, pinRtkPath, usesRtk } from '../../core/filter/rtk.mjs';
import {
  firstToken, hasHeredoc, hasRedirect, hasSubstitution, isBackgrounded, leadingAssignments, splitTopLevel, stripTrailingLimit,
} from '../../core/shell.mjs';
import { commandSegments, originalVerdict } from '../../hosts/claude-code/permissions.mjs';
import {
  CORPUS_DIR, diffArea, loadCorpus, recordingRewrite, replayRewrite, saveCorpus, staleSets, writeArea,
} from './lib.mjs';
import { HOOK_AREA, generateHookCases } from './hook.mjs';

export const AREAS = ['shell', 'rtkcmd', 'permissions', 'filter', 'shellwrites'];
const COMMAND_SETS = ['commands', 'harvested'];
const PIN_PATHS = ['', 'rtk', 'rtk.exe', '/opt/rtk/rtk', 'D:\\tools\\rtk.exe', 'D:\\tools\\$rtk\\rtk.exe', '/opt/$&/rtk'];
const PIN_PATHS_FOR_OUTPUT = ['', '/opt/rtk/rtk', 'D:\\tools\\rtk.exe'];

/** @param {string} set */
function loadCommands(set) {
  return existsSync(join(CORPUS_DIR, `${set}.json`)) ? loadCorpus(set) : [];
}

/** @param {any[]} cases */
function shellArea(cases) {
  return cases.map((c) => ({
    name: c.name,
    input: { command: c.command },
    output: {
      splitTopLevel: splitTopLevel(c.command),
      hasHeredoc: hasHeredoc(c.command),
      hasSubstitution: hasSubstitution(c.command),
      hasRedirect: hasRedirect(c.command),
      stripTrailingLimit: stripTrailingLimit(c.command),
      isBackgrounded: isBackgrounded(c.command),
      firstToken: firstToken(c.command),
      firstTokens: splitTopLevel(c.command).map(firstToken),
      leadingAssignments: leadingAssignments(c.command),
      commandKind: commandKind(c.command),
    },
  }));
}

/** @param {any[]} cases */
function rtkcmdArea(cases) {
  const out = [];
  const pinned = new Set();
  /** @param {string} id @param {string} text @param {string[]} paths */
  const pin = (id, text, paths) => {
    paths.forEach((rtkPath, i) => {
      out.push({ name: `pin:${id}:${i}`, input: { command: text, rtkPath }, output: { pinRtkPath: pinRtkPath(text, rtkPath) } });
    });
  };
  for (const c of cases) {
    out.push({
      name: c.name,
      input: { command: c.command },
      output: { normalizeForRtk: normalizeForRtk(c.command), isRtkInvocation: isRtkInvocation(c.command), usesRtk: usesRtk(c.command) },
    });
    if (/rtk/i.test(c.command)) pin(c.name, c.command, PIN_PATHS);
    const o = c.rtk;
    if (o && o !== 'throw' && o.stdout && !pinned.has(o.stdout)) {
      pinned.add(o.stdout);
      pin(`out:${c.name}`, o.stdout, PIN_PATHS_FOR_OUTPUT);
    }
  }
  return out;
}

// A directory that does not exist, so `cp`/`mv` never find a directory on disk; outputs are
// relative to it with forward slashes (the same on Windows and Linux).
const WRITES_CWD = resolve('/nxy-golden-cwd/work');

/** @param {any[]} cases */
function shellwritesArea(cases) {
  /** @param {string[]} paths */
  const rel = (paths) => paths.map((p) => relative(WRITES_CWD, p).replace(/\\/g, '/'));
  return cases.map((c) => {
    const tool = c.tool || 'Bash';
    return {
      name: c.name,
      input: { command: c.command, tool },
      output: {
        targets: rel(writeTargets(c.command, WRITES_CWD, tool)),
        contentTargets: rel(writeTargets(c.command, WRITES_CWD, tool, { content: true })),
      },
    };
  });
}

/** @param {any[]} cases @param {any[]} ruleSets */
function permissionsArea(cases, ruleSets) {
  const out = [];
  for (const c of cases) {
    out.push({ name: c.name, input: { command: c.command }, output: { commandSegments: commandSegments(c.command) } });
    if (!c.perm) continue;
    for (const rs of ruleSets) {
      const rules = { allow: rs.allow, deny: rs.deny };
      out.push({ name: `${c.name}@${rs.name}`, input: { command: c.command, rules }, output: { originalVerdict: originalVerdict(c.command, rules) } });
    }
  }
  return out;
}

/**
 * @param {any[]} cases corpus cases (mutated with the recorded answer when `rtkPath` is given)
 * @param {{rtkPath: string | null}} opts
 * @returns {{cases: any[], missing: string[], recorded: number}}
 */
function filterArea(cases, { rtkPath }) {
  const out = [];
  const missing = [];
  let recorded = 0;
  for (const c of cases) {
    const ctx = { engine: 'rtk', ...(c.ctx || {}) };
    const tool = c.tool || 'Bash';
    let rewrite;
    let asked = () => /** @type {string | null} */ (null);
    let rec = null;
    let miss = false;
    if (c.rtk === null) {
      rewrite = undefined;
    } else if (c.rtk !== undefined) {
      const r = replayRewrite(c.rtk);
      rewrite = r;
      asked = () => r.state.asked;
    } else if (rtkPath) {
      rec = recordingRewrite(rtkPath);
      rewrite = rec;
      asked = () => /** @type {any} */ (rec).state.asked;
    } else {
      rewrite = () => {
        miss = true;
        return { code: 1, stdout: '' };
      };
    }
    const decision = decide(c.command, { ...ctx, tool, rewrite });
    if (miss) {
      missing.push(c.name);
      continue;
    }
    if (rec && rec.state.entry) {
      c.rtk = rec.state.entry;
      recorded++;
    }
    out.push({
      name: c.name,
      // engine: false = ran with no rewrite function (no-engine); rtk: null = never asked.
      input: { command: c.command, tool, ctx, engine: c.rtk !== null, rtk: c.rtk === undefined ? null : c.rtk },
      output: { decision, asked: asked() },
    });
  }
  return { cases: out, missing, recorded };
}

/**
 * @param {{check?: boolean, record?: boolean}} [opts]
 * @returns {{ok: boolean, differences: string[], missing: string[], recorded: number, written: string[], hookSkipped: string | null}}
 */
export function generate({ check = false, record = false } = {}) {
  const ruleSets = loadCorpus('permission-rules');
  const settings = loadCorpus('settings-hooks');
  let rtkPath = null;
  if (record) {
    rtkPath = findRtk()?.path ?? null;
    if (!rtkPath) throw new Error('--record needs rtk on PATH (or NXY_RTK_PATH)');
  }

  /** @type {Record<string, Array<[string, any[]]>>} */
  const produced = Object.fromEntries(AREAS.map((a) => [a, []]));
  const missing = [];
  let recorded = 0;
  for (const set of COMMAND_SETS) {
    const cases = loadCommands(set);
    if (!cases.length) continue;
    const f = filterArea(cases, { rtkPath });
    missing.push(...f.missing.map((n) => `${set}:${n}`));
    if (f.recorded) {
      recorded += f.recorded;
      saveCorpus(set, cases); // `filterArea` filled the new rtk answers in
    }
    produced.shell.push([set, shellArea(cases)]);
    produced.rtkcmd.push([set, rtkcmdArea(cases)]);
    produced.permissions.push([set, permissionsArea(cases, ruleSets)]);
    produced.filter.push([set, f.cases]);
  }
  produced.shellwrites.push(['cases', shellwritesArea(loadCorpus('shellwrites'))]);
  produced.rtkcmd.push([
    'settings-hooks',
    settings.map((/** @type {any} */ s) => ({ name: s.name, input: { settings: s.settings }, output: { detectRtkHook: detectRtkHook(s.settings) } })),
  ]);

  const differences = [];
  const written = [];
  /** @type {string | null} */
  let hookSkipped = null;
  for (const area of AREAS) {
    for (const [set, cases] of produced[area]) {
      if (check) differences.push(...diffArea(area, set, cases));
      else if (!missing.length) written.push(writeArea(area, set, cases));
    }
    differences.push(...staleSets(area, produced[area].map(([s]) => s)));
  }
  // hook level: needs `go` for the rtk stub; without it the area is left alone
  const hook = generateHookCases();
  if (hook.error) {
    hookSkipped = hook.error;
  } else {
    if (check) differences.push(...diffArea(HOOK_AREA, 'cases', hook.cases));
    else if (!missing.length) written.push(writeArea(HOOK_AREA, 'cases', hook.cases));
    differences.push(...staleSets(HOOK_AREA, ['cases']));
  }
  return { ok: !differences.length && !missing.length, differences, missing, recorded, written, hookSkipped };
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  const args = process.argv.slice(2);
  const r = generate({ check: args.includes('--check'), record: args.includes('--record') });
  if (r.recorded) console.log(`recorded ${r.recorded} rtk answers into the corpus`);
  if (r.missing.length) console.error(`no rtk answer for: ${r.missing.join(', ')}\n(run with --record, needs rtk)`);
  if (r.hookSkipped) console.error(`hook area skipped: ${r.hookSkipped}`);
  for (const d of r.differences) console.error(d);
  if (!r.ok) process.exit(1);
  console.log(args.includes('--check') ? 'golden files up to date' : `wrote ${r.written.length} golden files`);
}
