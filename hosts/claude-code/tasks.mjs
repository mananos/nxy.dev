// @ts-check
// I/O over `<runtime>/tasks.json`: which task is active in this checkout. Pure transforms live in core/tasks.mjs.
// A corrupt file is never overwritten: reads fall back to the branch key (today's behavior) and writes are no-ops.
import { existsSync, readFileSync, renameSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { randomBytes } from 'node:crypto';
import { dirname, join } from 'node:path';
import { branchExists, ensureDir, gitBranch, nxyRuntimeDir } from '../../core/paths.mjs';
import {
  emptyTasks, findTask, isEmptyTasks, normalizeTasks, resolveTask, staleTasks, withGoal, withOffered, withPinned,
  withUnpinned, withoutTask,
} from '../../core/tasks.mjs';
import { writeMarker } from './plan-approval.mjs';
import { clearBaseline } from './baseline.mjs';

const tasksFile = (cwd) => join(nxyRuntimeDir(cwd), 'tasks.json');
const notedFile = (cwd) => join(nxyRuntimeDir(cwd), 'tasks-corrupt-noted.json');

/**
 * @param {string} cwd
 * @returns {{state: import('../../core/tasks.mjs').TasksState, corrupt: boolean}}
 */
export function readTasks(cwd) {
  const file = tasksFile(cwd);
  if (!existsSync(file)) return { state: emptyTasks(), corrupt: false };
  try {
    const raw = JSON.parse(readFileSync(file, 'utf8'));
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return { state: emptyTasks(), corrupt: true };
    return { state: normalizeTasks(raw), corrupt: false };
  } catch {
    return { state: emptyTasks(), corrupt: true };
  }
}

/**
 * Re-reads right before writing, applies `fn`, writes atomically; removes the file when empty. `fn` returns the
 * new state, the same state (no write) or null (abort). Returns false when aborted, corrupt, or the write failed.
 * @param {string} cwd
 * @param {(state: import('../../core/tasks.mjs').TasksState) => import('../../core/tasks.mjs').TasksState|null} fn
 */
export function updateTasks(cwd, fn) {
  const { state, corrupt } = readTasks(cwd);
  if (corrupt) return false;
  const next = fn(state);
  if (!next) return false;
  if (next === state) return true;
  const file = tasksFile(cwd);
  try {
    if (isEmptyTasks(next)) {
      rmSync(file, { force: true });
      return true;
    }
    ensureDir(dirname(file));
    const tmp = `${file}.${process.pid}.${randomBytes(4).toString('hex')}.tmp`;
    writeFileSync(tmp, JSON.stringify(next, null, 2));
    renameSync(tmp, file);
    return true;
  } catch {
    return false;
  }
}

/**
 * One line the first time a given corrupt tasks.json (size + mtime) is seen; null otherwise.
 * @param {string} cwd
 * @returns {string|null}
 */
export function corruptNotice(cwd) {
  if (!readTasks(cwd).corrupt) return null;
  try {
    const st = statSync(tasksFile(cwd));
    const sig = { size: st.size, mtimeMs: st.mtimeMs };
    try {
      const noted = JSON.parse(readFileSync(notedFile(cwd), 'utf8'));
      if (noted && noted.size === sig.size && noted.mtimeMs === sig.mtimeMs) return null;
    } catch { /* never noted */ }
    ensureDir(nxyRuntimeDir(cwd));
    writeFileSync(notedFile(cwd), JSON.stringify(sig));
    return 'nxy: .nxy/local/tasks.json is unreadable, so tasks are keyed by branch as before and the file is left untouched. Fix or delete it to use tasks again.';
  } catch {
    return null;
  }
}

/** The task active in this checkout (one read + the branch). Corrupt or missing file → the branch itself. */
export function activeTask(cwd) {
  const { state, corrupt } = readTasks(cwd);
  return resolveTask(corrupt ? emptyTasks() : state, gitBranch(cwd));
}

/** Key for per-task state (marker, baseline, handoff). */
export const taskKey = (cwd) => activeTask(cwd).id;

/** A task by id; an unknown id is a legacy handoff whose id is its branch. */
export function taskById(cwd, id) {
  const { state, corrupt } = readTasks(cwd);
  const t = corrupt ? null : findTask(state, id);
  if (!t) return { id, label: id, goal: '', branch: id, pinned: false, source: /** @type {const} */ ('branch') };
  const pinned = t.branch !== null;
  return {
    id: t.id, label: t.goal || t.id, goal: t.goal, branch: pinned ? t.branch : gitBranch(cwd),
    pinned, source: /** @type {'pinned'|'unpinned'} */ (pinned ? 'pinned' : 'unpinned'),
  };
}

/** Starts the unpinned task. False if another unpinned task exists meanwhile (or the file is corrupt). */
export function startTask(cwd, { id, goal, now = Date.now() }) {
  return updateTasks(cwd, (s) => (s.unpinned && s.unpinned.id !== id ? null : withUnpinned(s, { id, goal, now })));
}

export const setTaskGoal = (cwd, id, goal) => updateTasks(cwd, (s) => (findTask(s, id) ? withGoal(s, id, goal) : s));

/** Pins the unpinned task to the current branch (no-op when there is none or the branch already has one). */
export const pinActiveTask = (cwd, now = Date.now()) => updateTasks(cwd, (s) => withPinned(s, gitBranch(cwd), now));

export const forgetTask = (cwd, id) => updateTasks(cwd, (s) => (findTask(s, id) ? withoutTask(s, id) : s));

/**
 * Moves the unpinned task `id` of `fromCwd` to `toCwd`. The target gets it first; only then is it removed from
 * the source (entry, marker, baseline), so a failure before that leaves it in the source and it is never lost.
 * @param {string} fromCwd @param {string} toCwd @param {string} id
 * @returns {{ok: true, id: string, goal: string} | {ok: false, reason: string}}
 */
export function carryTask(fromCwd, toCwd, id) {
  const src = readTasks(fromCwd);
  if (src.corrupt) return { ok: false, reason: 'tasks.json ilegible en este checkout' };
  const t = src.state.unpinned;
  if (!t || t.id !== id) return { ok: false, reason: 'esa tarea no es la tarea sin rama de este checkout' };
  const added = updateTasks(toCwd, (s) => (s.unpinned && s.unpinned.id !== id ? null : withUnpinned(s, { id: t.id, goal: t.goal, now: t.created })));
  if (!added) return { ok: false, reason: 'el worktree destino ya tiene otra tarea sin rama (o su tasks.json no se pudo escribir)' };
  if (!forgetTask(fromCwd, id)) {
    updateTasks(toCwd, (s) => (findTask(s, id) ? withoutTask(s, id) : s));
    return { ok: false, reason: 'no se pudo quitar la tarea del checkout de origen' };
  }
  writeMarker(fromCwd, id, null);
  clearBaseline(fromCwd, id);
  return { ok: true, id: t.id, goal: t.goal };
}

/**
 * Pinned tasks whose branch is gone, each returned once (marked offered).
 * @param {string} cwd
 */
export function takeStaleTasks(cwd) {
  const { state, corrupt } = readTasks(cwd);
  if (corrupt) return [];
  const stale = staleTasks(state, (b) => branchExists(cwd, b));
  if (!stale.length) return [];
  const ids = stale.map((t) => t.id);
  return updateTasks(cwd, (s) => withOffered(s, ids)) ? stale : [];
}
