// @ts-check
/**
 * The gate's one-shot escape.
 *
 * `/nxy:gate once` writes a marker; the next edit that *would* be blocked consumes it and goes
 * through. One use, and it expires on its own, because an escape that outlives the reason for it
 * silently turns the gate off — which is the failure mode of every "temporarily disable" switch.
 */
import { existsSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { ensureDir, nxyRuntimeDir } from '../../core/paths.mjs';

function escapePath(cwd) {
  return join(nxyRuntimeDir(cwd), 'gate-escape.json');
}

/** Arms a single bypass. Returns when it expires. */
export function armEscape(cwd, minutes = 5) {
  const until = Date.now() + Math.max(1, minutes) * 60_000;
  const p = escapePath(cwd);
  ensureDir(nxyRuntimeDir(cwd));
  writeFileSync(p, JSON.stringify({ until }), 'utf8');
  return until;
}

/** When the armed escape expires, or null if there is none / it is stale. */
export function escapeUntil(cwd, minutes = 5) {
  const p = escapePath(cwd);
  if (!existsSync(p)) return null;
  try {
    const { until } = JSON.parse(readFileSync(p, 'utf8'));
    if (typeof until !== 'number' || until < Date.now()) return null;
    // An escape can never outlive its own window, whatever the file claims.
    return Math.min(until, Date.now() + Math.max(1, minutes) * 60_000);
  } catch {
    return null;
  }
}

/** Uses up the escape if one is valid. True means "let this edit through". */
export function consumeEscape(cwd, minutes = 5) {
  const until = escapeUntil(cwd, minutes);
  if (!until) return false;
  try {
    rmSync(escapePath(cwd), { force: true });
  } catch {
    // If we cannot remove it, better to deny the next edit than to leave the gate open forever.
    return false;
  }
  return true;
}

/** Clears any armed escape (used by `/nxy:gate on`). */
export function clearEscape(cwd) {
  try {
    rmSync(escapePath(cwd), { force: true });
  } catch {
    /* nothing to clear */
  }
}
