// @ts-check
/**
 * Runs 1.x functions inside a child node process started with TZ=<zone>, so goldens that depend on
 * the local time zone are deterministic and do not depend on the machine running the generator.
 * One child per zone; the caller sends {module, fn, calls: [[args...], ...]} and gets one result per call.
 *
 *   import { inZone } from './tz-child.mjs';
 *   inZone('Europe/Madrid', 'scripts/golden/tz-child.mjs', 'localParts', [[0], [1e12]]);
 *
 * Results travel as JSON: a function that can return NaN must map it to null itself.
 */
import { spawnSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { pathToFileURL, fileURLToPath } from 'node:url';
import { resolve } from 'node:path';
import { ROOT } from './lib.mjs';

/** Zones every date golden covers: UTC, no DST (west), DST (Europe), DST at midnight, half-hour offset. */
export const ZONES = ['UTC', 'America/Argentina/Buenos_Aires', 'Europe/Madrid', 'America/Santiago', 'Asia/Kolkata'];

/**
 * The Date getters of an instant, in the local zone of the process (null for an invalid time).
 * @param {number | null} ms  null stands for NaN
 */
export function localParts(ms) {
  const d = new Date(ms === null ? NaN : ms);
  if (Number.isNaN(d.getTime())) return null;
  return { y: d.getFullYear(), mon: d.getMonth(), d: d.getDate(), h: d.getHours(), min: d.getMinutes(), wd: d.getDay() };
}

/** Date.parse in the local zone of the process (null for NaN). */
export function parseLocal(/** @type {string} */ s) {
  const n = Date.parse(s);
  return Number.isNaN(n) ? null : n;
}

/**
 * @param {string} zone
 * @param {string} module  path from the repo root
 * @param {string} fn      exported function name
 * @param {any[][]} calls
 * @returns {any[]}
 */
export function inZone(zone, module, fn, calls) {
  const r = spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', fileURLToPath(import.meta.url)], {
    input: JSON.stringify({ module, fn, calls }),
    env: { ...process.env, TZ: zone },
    encoding: 'utf8',
    maxBuffer: 64 * 1024 * 1024,
    windowsHide: true,
  });
  if (r.status !== 0) throw new Error(`tz child (${zone}) failed: ${r.stderr || r.error}`);
  return JSON.parse(r.stdout);
}

async function main() {
  const req = JSON.parse(readFileSync(0, 'utf8'));
  const mod = await import(pathToFileURL(resolve(ROOT, req.module)).href);
  const out = req.calls.map((/** @type {any[]} */ args) => mod[req.fn](...args));
  process.stdout.write(JSON.stringify(out));
}

if (process.argv[1] && fileURLToPath(import.meta.url) === resolve(process.argv[1])) {
  main().catch((e) => {
    console.error(e);
    process.exit(1);
  });
}
