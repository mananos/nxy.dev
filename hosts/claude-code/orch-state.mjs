// @ts-check
/**
 * Claude Code side of the orchestrator's files (core/orchestrator.mjs), under `.nxy/local/orch/`:
 * the marker (`active.json`: the module is running a plan, so the main thread must not dispatch),
 * one ticket file per module-spawned agent (one use), and the module's own `state.json`.
 * The module writes them with `$.fs`; the hooks only read them (and consume tickets).
 */
import { existsSync, readFileSync, renameSync, rmSync, unlinkSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { ensureDir, nxyRuntimeDir } from '../../core/paths.mjs';
import { MARKER_FILE, MARKER_TTL_MS, ORCH_DIR, TICKET_DIR } from '../../core/orchestrator.mjs';

export const orchDir = (cwd) => join(nxyRuntimeDir(cwd), ORCH_DIR);
const safe = (s) => String(s).replace(/[^0-9a-z_-]/gi, '');

/** Atomic JSON write (write aside, rename; in place when Windows refuses the rename). */
export function writeOrchFile(cwd, name, data) {
  try {
    const dir = ensureDir(orchDir(cwd));
    const path = join(dir, name);
    const tmp = `${path}.${process.pid}.tmp`;
    writeFileSync(tmp, JSON.stringify(data), 'utf8');
    try {
      renameSync(tmp, path);
    } catch {
      writeFileSync(path, readFileSync(tmp, 'utf8'), 'utf8');
      unlinkSync(tmp);
    }
  } catch {
    /* best-effort */
  }
}

/**
 * The marker, or null when absent, unreadable or older than MARKER_TTL_MS (a stuck marker expires).
 * @returns {{ts: number, [k: string]: any}|null}
 */
export function readActive(cwd) {
  try {
    const m = JSON.parse(readFileSync(join(orchDir(cwd), MARKER_FILE), 'utf8'));
    return m && typeof m.ts === 'number' && Date.now() - m.ts <= MARKER_TTL_MS ? m : null;
  } catch {
    return null;
  }
}

/** One use: true when the ticket exists, and it is gone afterwards. */
export function consumeTicket(cwd, nonce) {
  const path = join(orchDir(cwd), TICKET_DIR, safe(nonce));
  if (!safe(nonce) || !existsSync(path)) return false;
  try {
    unlinkSync(path);
    return true;
  } catch {
    return false; // another hook took it first
  }
}

/** The module's own state: what the user waved through, acknowledged, and when batches launched. */
export function readOrchState(cwd) {
  /** @type {{continued: number[], acked: number[], launched: Record<string, number>}} */
  const out = { continued: [], acked: [], launched: {} };
  try {
    const s = JSON.parse(readFileSync(join(orchDir(cwd), 'state.json'), 'utf8'));
    if (Array.isArray(s?.continued)) out.continued = s.continued.map(Number);
    if (Array.isArray(s?.acked)) out.acked = s.acked.map(Number);
    if (s?.launched && typeof s.launched === 'object') out.launched = s.launched;
  } catch {
    /* none yet */
  }
  return out;
}

/** Removes the marker, the tickets and the state: the escape hatch for a stuck marker. */
export function clearOrch(cwd) {
  try {
    rmSync(orchDir(cwd), { recursive: true, force: true });
  } catch {
    /* best-effort */
  }
}
