// @ts-check
/**
 * One row per gate decision, so `stats` can answer the only question that matters about it:
 * is it earning its place, or just nagging?
 *
 * The numbers to watch (roadmap tema 9.3): `main edits` and average main context go down,
 * subagent share goes up, `$/turno` does not go up. If calls per turn go up and `$/turno` does
 * not come down, the threshold sits too low.
 *
 * Rows are also how the default threshold gets chosen without anyone running an experiment:
 * the distribution of context-at-edit is already in here after normal use.
 *
 * The file is capped: past `maxBytes` it is rewritten without rows older than 90 days, and if that
 * is still too big, only the newest rows that fit in half the cap survive (so the rewrite cannot
 * run on every append).
 */
import { readFileSync, renameSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { appendJsonl } from '../../core/jsonl.mjs';
import { nxyRuntimeDir } from '../../core/paths.mjs';

const DEFAULT_MAX_BYTES = 256 * 1024;
const KEEP_MS = 90 * 86_400_000;

/**
 * @param {string} cwd
 * @param {{ts: number, tool: string, allow: boolean, reason: string, contextTokens: number|null, threshold: number, kind?: string}} row
 * @param {{maxBytes?: number}} [opts]
 */
export function recordGate(cwd, row, opts = {}) {
  try {
    const path = gateLedgerPath(cwd);
    appendJsonl(path, row);
    const maxBytes = opts.maxBytes ?? DEFAULT_MAX_BYTES;
    if (statSync(path).size > maxBytes) trim(path, maxBytes);
  } catch {
    // telemetry for us; never a reason to change what happens to the user's edit
  }
}

function trim(path, maxBytes) {
  const tmp = `${path}.tmp`;
  try {
    const lines = readFileSync(path, 'utf8').split('\n').filter((l) => l.trim());
    const cutoff = Date.now() - KEEP_MS;
    let kept = lines.filter((l) => {
      try {
        const ts = JSON.parse(l).ts;
        return typeof ts !== 'number' || ts >= cutoff;
      } catch {
        return false;
      }
    });
    const size = (ls) => ls.reduce((n, l) => n + Buffer.byteLength(l) + 1, 0);
    if (size(kept) > maxBytes) {
      const half = Math.floor(maxBytes / 2);
      const out = [];
      let used = 0;
      for (let i = kept.length - 1; i >= 0; i--) {
        const n = Buffer.byteLength(kept[i]) + 1;
        if (used + n > half) break;
        used += n;
        out.unshift(kept[i]);
      }
      kept = out;
    }
    writeFileSync(tmp, kept.length ? kept.join('\n') + '\n' : '', 'utf8');
    renameSync(tmp, path);
  } catch {
    try {
      rmSync(tmp, { force: true });
    } catch {
      /* leave it */
    }
  }
}

/** Path of the ledger, for readers (`stats`, `/nxy:gate status`). */
export function gateLedgerPath(cwd) {
  return join(nxyRuntimeDir(cwd), 'metrics', 'gate.jsonl');
}
