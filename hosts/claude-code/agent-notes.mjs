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

/** The waiting notes, oldest first, each delivered to one caller only. Empty when none. */
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
    const claimed = `${path}.${process.pid}.${randomBytes(2).toString('hex')}.taken`;
    try {
      renameSync(path, claimed); // only one process wins the rename
    } catch {
      continue;
    }
    try {
      const text = readFileSync(claimed, 'utf8').trim();
      const fresh = Date.now() - statSync(claimed).mtimeMs <= NOTE_TTL_MS;
      if (text && fresh && !out.includes(text)) out.push(text);
    } catch {
      /* unreadable: dropped */
    } finally {
      rmSync(claimed, { force: true });
    }
  }
  return out;
}
