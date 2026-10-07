// @ts-check
/**
 * Background agents (async launches) and the notes that tell the main thread how they ended.
 *
 * PostToolUse:Agent fires when a background agent is *launched*, not when it ends. So the launch is
 * marked here (by role identity, never by agentId), and the verdict is left as a note when the agent
 * stops; whichever hook runs next on the main thread delivers it. A note is delivered once: each is
 * one small file in `.nxy/local/notes/`, claimed by renaming it, so two hooks never deliver the same.
 */
import { existsSync, readFileSync, readdirSync, renameSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { randomBytes } from 'node:crypto';
import { join } from 'node:path';
import { ensureDir, nxyRuntimeDir } from '../../core/paths.mjs';

export const ASYNC_TTL_MS = 6 * 3_600_000;
const NOTE_TTL_MS = 86_400_000;

const asyncDir = (cwd) => join(nxyRuntimeDir(cwd), 'async');
const notesDir = (cwd) => join(nxyRuntimeDir(cwd), 'notes');
const safe = (key) => String(key).replace(/[^0-9a-z-]/gi, '_');

/** Whether an Agent tool_response says the agent was launched in the background (string, object or content blocks). */
export function isAsyncLaunch(toolResponse) {
  const r = toolResponse;
  if (!r) return false;
  if (typeof r === 'string') return /^\s*Async agent launched/i.test(r);
  if (Array.isArray(r)) return r.some((b) => isAsyncLaunch(b));
  if (typeof r !== 'object') return false;
  if (r.isAsync === true || r.status === 'async_launched') return true;
  if (typeof r.text === 'string' && /^\s*Async agent launched/i.test(r.text)) return true;
  if (Array.isArray(r.content)) return r.content.some((b) => isAsyncLaunch(b));
  if (typeof r.content === 'string') return isAsyncLaunch(r.content);
  return false;
}

/** Marks a background launch by role identity (`implementer:batch-3`, `implementer:suite-fix`, `tester`, `reviewer`). */
export function markAsync(cwd, key) {
  try {
    writeFileSync(join(ensureDir(asyncDir(cwd)), safe(key)), String(Date.now()), 'utf8');
  } catch {
    /* best-effort */
  }
}

/** Forgets a launch marker: a new dispatch of the same role starts clean (PostToolUse re-marks it if it is async). */
export function clearAsync(cwd, key) {
  try {
    rmSync(join(asyncDir(cwd), safe(key)), { force: true });
  } catch {
    /* best-effort */
  }
}

/** True once if the launch was marked in the last 6 hours; the marker is consumed. */
export function takeAsync(cwd, key) {
  const path = join(asyncDir(cwd), safe(key));
  try {
    const ts = Number(readFileSync(path, 'utf8'));
    rmSync(path, { force: true });
    return Number.isFinite(ts) && Date.now() - ts <= ASYNC_TTL_MS;
  } catch {
    return false;
  }
}

/** Whether the launch was marked in the last 6 hours (a background agent); the marker is kept. */
export function hasAsync(cwd, key) {
  try {
    const ts = Number(readFileSync(join(asyncDir(cwd), safe(key)), 'utf8'));
    return Number.isFinite(ts) && Date.now() - ts <= ASYNC_TTL_MS;
  } catch {
    return false;
  }
}

/** Leaves a note for the main thread; the same text already waiting is not added twice. */
export function pushNote(cwd, text) {
  const body = String(text || '').trim();
  if (!body) return;
  try {
    const dir = ensureDir(notesDir(cwd));
    for (const name of readdirSync(dir)) {
      if (name.endsWith('.note') && readFileSync(join(dir, name), 'utf8') === body) return;
    }
    const name = `${Date.now()}-${process.pid}-${randomBytes(3).toString('hex')}.note`;
    const tmp = join(dir, `${name}.tmp`);
    writeFileSync(tmp, body, 'utf8');
    renameSync(tmp, join(dir, name));
  } catch {
    /* best-effort */
  }
}

/**
 * Runs `fn`, retrying while Windows reports the file busy (another process or an antivirus has it
 * open). Any other error, ENOENT included, is thrown at once.
 * @template T @param {() => T} fn @returns {T}
 */
function retryBusy(fn) {
  for (let i = 0; ; i++) {
    try {
      return fn();
    } catch (e) {
      const code = /** @type {any} */ (e)?.code;
      if (i >= 20 || !['EPERM', 'EBUSY', 'EACCES'].includes(code)) throw e;
      Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, 10);
    }
  }
}

/**
 * The waiting notes, oldest first, each delivered to one caller only. Empty when none.
 *
 * A note is claimed by creating `<note>.lock` exclusively (`wx`), atomic on NTFS and POSIX. Not by
 * renaming it: on Windows a rename opens the source first, so a second reader can move the file a
 * first one already renamed and both deliver it. The owner deletes the note before its lock, so a
 * late reader that gets the lock finds no note.
 */
export function takeNotes(cwd) {
  const dir = notesDir(cwd);
  if (!existsSync(dir)) return [];
  let names = [];
  try {
    names = readdirSync(dir).filter((n) => n.endsWith('.note')).sort();
  } catch {
    return [];
  }
  const out = [];
  for (const name of names) {
    const path = join(dir, name);
    const lock = `${path}.lock`;
    try {
      writeFileSync(lock, String(process.pid), { flag: 'wx' });
    } catch {
      // Another reader holds it; a lock a day old belongs to a reader that died: drop both.
      try {
        if (Date.now() - statSync(lock).mtimeMs > NOTE_TTL_MS) {
          rmSync(path, { force: true });
          rmSync(lock, { force: true });
        }
      } catch {
        /* gone meanwhile */
      }
      continue;
    }
    let text = '';
    let fresh = false;
    let removed = false;
    try {
      text = retryBusy(() => readFileSync(path, 'utf8')).trim();
      fresh = Date.now() - statSync(path).mtimeMs <= NOTE_TTL_MS;
      retryBusy(() => rmSync(path));
      removed = true;
    } catch {
      /* ENOENT: a reader before us delivered it */
    }
    // Read but not removed: the lock stays, so nobody delivers it again.
    if (removed || !text) {
      try {
        retryBusy(() => rmSync(lock, { force: true }));
      } catch {
        /* a leftover lock delivers nothing */
      }
    }
    if (text && fresh && !out.includes(text)) out.push(text);
  }
  return out;
}
