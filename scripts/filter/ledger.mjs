// @ts-check
/**
 * rtk's own savings ledger. Every filtered call rtk runs lands in its SQLite history
 * (`commands`: timestamp, project_path, input/output/saved tokens — "input" is what the raw
 * command printed, "output" what rtk let through). nxy only ever sees the filtered side, so
 * this is the one place the real saving can be read from.
 *
 * Read directly with `node:sqlite` (Node ≥ 22.13, read-only). When that module is missing the
 * caller falls back to `rtk gain -a --format json`, which only has daily totals.
 */
import { existsSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, join } from 'node:path';
import { rtkWarnMarkerPath } from './rtk.mjs';

/**
 * @typedef {object} LedgerRow
 * @property {number} ts          epoch ms
 * @property {string} project     rtk's cwd when it ran, normalized (forward slashes, no `\\?\`)
 * @property {number} input       tokens the raw command produced
 * @property {number} output      tokens rtk let through
 * @property {number} saved       input - output
 */

/** `RTK_DB_PATH` when set (rtk honours it too), else `history.db` next to the warning marker. */
export function rtkHistoryDbPath(env = process.env, platform = process.platform) {
  if (env.RTK_DB_PATH) return env.RTK_DB_PATH;
  return join(dirname(rtkWarnMarkerPath(env, platform)), 'history.db');
}

/** Forward slashes, no Windows extended-length prefix, no trailing slash; case-folded on win32. */
export function normalizeProjectPath(p, platform = process.platform) {
  let s = String(p || '').replace(/^\\\\\?\\/, '').replace(/\\/g, '/').replace(/\/+$/, '');
  if (platform === 'win32') s = s.toLowerCase();
  return s;
}

/** True when rtk ran inside `cwd` (same dir or a subdir: `cd sub && rtk grep …` records the subdir). */
export function underProject(rowProject, cwd, platform = process.platform) {
  const base = normalizeProjectPath(cwd, platform);
  if (!base) return true;
  return rowProject === base || rowProject.startsWith(base + '/');
}

/**
 * Rows in [sinceMs, untilMs]. Returns null when the ledger cannot be read (no node:sqlite,
 * no db yet, locked db) — the caller decides whether to fall back or stay silent.
 * @param {{sinceMs?: number|null, untilMs?: number|null, dbPath?: string}} [opts]
 * @returns {LedgerRow[]|null}
 */
export function readLedgerRows(opts = {}) {
  const dbPath = opts.dbPath || rtkHistoryDbPath();
  if (!existsSync(dbPath)) return null;
  let DatabaseSync;
  try {
    // node:sqlite prints an ExperimentalWarning on load in Node 22; mute just that one.
    const orig = process.emitWarning;
    process.emitWarning = /** @type {any} */ ((w, ...rest) => { if (!/sqlite/i.test(String(w))) orig.call(process, w, ...rest); });
    try {
      ({ DatabaseSync } = createRequire(import.meta.url)('node:sqlite'));
    } finally {
      process.emitWarning = orig;
    }
  } catch {
    return null;
  }
  try {
    const db = new DatabaseSync(dbPath, { readOnly: true });
    try {
      // ISO strings compare lexicographically to the second; the exact bound is applied in JS.
      const lo = opts.sinceMs ? new Date(opts.sinceMs - 1000).toISOString().slice(0, 19) : '';
      const hi = opts.untilMs ? new Date(opts.untilMs + 1000).toISOString().slice(0, 19) + 'Z' : '9';
      const rows = db.prepare(
        'SELECT timestamp, project_path, input_tokens, output_tokens, saved_tokens FROM commands WHERE timestamp >= ? AND timestamp <= ? ORDER BY timestamp',
      ).all(lo, hi);
      /** @type {LedgerRow[]} */
      const out = [];
      for (const r of /** @type {any[]} */ (rows)) {
        const ts = Date.parse(String(r.timestamp));
        if (Number.isNaN(ts)) continue;
        if (opts.sinceMs && ts < opts.sinceMs) continue;
        if (opts.untilMs && ts > opts.untilMs) continue;
        out.push({ ts, project: normalizeProjectPath(r.project_path), input: Number(r.input_tokens) || 0, output: Number(r.output_tokens) || 0, saved: Number(r.saved_tokens) || 0 });
      }
      return out;
    } finally {
      db.close();
    }
  } catch {
    return null;
  }
}

/**
 * Why `readLedgerRows` would return null, for an accurate message: 'no-db' (rtk tracking off,
 * `RTK_DB_PATH` wrong, rtk installed on another OS), 'no-sqlite' (Node without node:sqlite), or null when fine.
 * @param {string} [dbPath]
 * @returns {'no-db'|'no-sqlite'|null}
 */
export function ledgerUnavailableReason(dbPath = rtkHistoryDbPath()) {
  if (!existsSync(dbPath)) return 'no-db';
  try {
    const orig = process.emitWarning;
    process.emitWarning = /** @type {any} */ (() => {});
    try {
      createRequire(import.meta.url)('node:sqlite');
    } finally {
      process.emitWarning = orig;
    }
  } catch {
    return 'no-sqlite';
  }
  return null;
}

export function emptySavings() {
  return { commands: 0, input: 0, output: 0, saved: 0 };
}

/** @param {LedgerRow[]} rows */
export function sumSavings(rows) {
  const s = emptySavings();
  for (const r of rows) {
    s.commands++;
    s.input += r.input;
    s.output += r.output;
    s.saved += r.saved;
  }
  return s;
}

/** Percentage saved, or null when nothing went through. */
export function savedPct(s) {
  return s.input ? (s.saved / s.input) * 100 : null;
}

/**
 * Savings attributable to one session: rtk rows inside the session's time window that ran
 * under its cwd. Two sessions of the same project open at once would share rows; the
 * timestamps are the only join available.
 * @param {LedgerRow[]} rows
 * @param {{cwd: string|null, firstTs: number|null, lastTs: number|null}} session
 */
export function sessionSavings(rows, session) {
  if (session.firstTs === null || session.lastTs === null) return emptySavings();
  const lo = session.firstTs - 5_000;
  const hi = session.lastTs + 60_000;
  return sumSavings(rows.filter((r) => r.ts >= lo && r.ts <= hi && underProject(r.project, session.cwd || '')));
}

/**
 * Daily totals from `rtk gain -a --format json` (fallback when node:sqlite is unavailable):
 * `YYYY-MM-DD` → savings. Global or per rtk project (`-p`), never per session.
 */
export function dailyFromGain(gainJson) {
  /** @type {Map<string, ReturnType<typeof emptySavings>>} */
  const out = new Map();
  for (const d of gainJson?.daily || []) {
    if (!d?.date) continue;
    out.set(String(d.date), { commands: Number(d.commands) || 0, input: Number(d.input_tokens) || 0, output: Number(d.output_tokens) || 0, saved: Number(d.saved_tokens) || 0 });
  }
  return out;
}
