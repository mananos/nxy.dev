// @ts-check
/**
 * The gate's one-shot escapes.
 *
 * `/nxy:gate once` writes a marker; the next edit that *would* be blocked consumes it and goes
 * through. `/nxy:gate once search` does the same for a broad main-thread search, in its own file,
 * so one can never be used up by the other. One use, and it expires on its own, because an escape
 * that outlives the reason for it silently turns the gate off — which is the failure mode of every
 * "temporarily disable" switch.
 */
import { existsSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { ensureDir, nxyRuntimeDir } from '../../core/paths.mjs';

/** @typedef {'edit'|'search'} EscapeKind */

function escapePath(cwd, kind = 'edit') {
  return join(nxyRuntimeDir(cwd), kind === 'search' ? 'gate-escape-search.json' : 'gate-escape.json');
}

/**
 * Arms a single bypass. Returns when it expires.
 * @param {string} cwd
 * @param {number} [minutes]
 * @param {EscapeKind} [kind]
 */
export function armEscape(cwd, minutes = 5, kind = 'edit') {
  const until = Date.now() + Math.max(1, minutes) * 60_000;
  const p = escapePath(cwd, kind);
  ensureDir(nxyRuntimeDir(cwd));
  writeFileSync(p, JSON.stringify({ until }), 'utf8');
  return until;
}

/**
 * When the armed escape expires, or null if there is none / it is stale.
 * @param {string} cwd
 * @param {number} [minutes]
 * @param {EscapeKind} [kind]
 */
export function escapeUntil(cwd, minutes = 5, kind = 'edit') {
  const p = escapePath(cwd, kind);
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

/**
 * Uses up the escape if one is valid. True means "let this one through".
 * @param {string} cwd
 * @param {number} [minutes]
 * @param {EscapeKind} [kind]
 */
export function consumeEscape(cwd, minutes = 5, kind = 'edit') {
  const until = escapeUntil(cwd, minutes, kind);
  if (!until) return false;
  try {
    rmSync(escapePath(cwd, kind), { force: true });
  } catch {
    // If we cannot remove it, better to deny the next one than to leave the gate open forever.
    return false;
  }
  return true;
}

/** Clears every armed escape (used by `/nxy:gate on`). */
export function clearEscape(cwd) {
  for (const kind of /** @type {EscapeKind[]} */ (['edit', 'search'])) {
    try {
      rmSync(escapePath(cwd, kind), { force: true });
    } catch {
      /* nothing to clear */
    }
  }
}
