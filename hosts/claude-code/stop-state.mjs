// @ts-check
/**
 * What the `Stop` hook needs (0.4.5), kept outside SQLite so the hook at the end of every turn reads
 * two small JSON files and nothing else:
 *
 * - `.nxy/local/handoff-saved.json` — the branch's live handoff and when it was last saved; written
 *   by `mem handoff save|plan`, removed by `mem handoff done`. No file → no live handoff → silent.
 * - `.nxy/local/stop/<session>.json` — when this session last edited a file that is not plan batch
 *   work (plan progress is derived, never stale), and which of those states it was already reminded
 *   about. Per session: that is the baseline — earlier sessions' edits are not this one's to save.
 */
import { existsSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { ensureDir, nxyRuntimeDir } from '../../core/paths.mjs';

const savedPath = (cwd) => join(nxyRuntimeDir(cwd), 'handoff-saved.json');
const safeId = (s) => String(s || 'unknown').replace(/[^\w.-]/g, '_').slice(0, 100);
const sessionPath = (cwd, session) => join(nxyRuntimeDir(cwd), 'stop', `${safeId(session)}.json`);

const readJson = (p) => {
  try {
    return existsSync(p) ? JSON.parse(readFileSync(p, 'utf8')) : null;
  } catch {
    return null;
  }
};

/**
 * The handoff of `branch` was saved now (`ts`), or closed (`null`).
 * @param {string} cwd @param {string|null} branch @param {number|null} [ts]
 */
export function markHandoffSaved(cwd, branch, ts = Date.now()) {
  try {
    if (ts == null) {
      rmSync(savedPath(cwd), { force: true });
      return;
    }
    ensureDir(nxyRuntimeDir(cwd));
    writeFileSync(savedPath(cwd), JSON.stringify({ branch: branch || null, ts }), 'utf8');
  } catch {
    /* best-effort */
  }
}

/** When the live handoff of `branch` was last saved, or null when there is none. */
export function handoffSavedTs(cwd, branch) {
  const m = readJson(savedPath(cwd));
  return m && (m.branch || null) === (branch || null) && typeof m.ts === 'number' ? m.ts : null;
}

/** @returns {{lastEditTs: number|null, remindedTs: number|null}} */
export function readStopState(cwd, session) {
  const s = readJson(sessionPath(cwd, session)) || {};
  return {
    lastEditTs: typeof s.lastEditTs === 'number' ? s.lastEditTs : null,
    remindedTs: typeof s.remindedTs === 'number' ? s.remindedTs : null,
  };
}

/** @param {string} cwd @param {string} session @param {{lastEditTs?: number, remindedTs?: number}} patch */
export function writeStopState(cwd, session, patch) {
  try {
    ensureDir(join(nxyRuntimeDir(cwd), 'stop'));
    writeFileSync(sessionPath(cwd, session), JSON.stringify({ ...readStopState(cwd, session), ...patch }), 'utf8');
  } catch {
    /* best-effort */
  }
}
