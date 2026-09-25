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
 */
import { join } from 'node:path';
import { appendJsonl } from '../../core/jsonl.mjs';
import { nxyRuntimeDir } from '../../core/paths.mjs';

/**
 * @param {string} cwd
 * @param {{ts: number, tool: string, allow: boolean, reason: string, contextTokens: number|null, threshold: number}} row
 */
export function recordGate(cwd, row) {
  try {
    appendJsonl(join(nxyRuntimeDir(cwd), 'metrics', 'gate.jsonl'), row);
  } catch {
    // telemetry for us; never a reason to change what happens to the user's edit
  }
}

/** Path of the ledger, for readers (`stats`, `/nxy:gate status`). */
export function gateLedgerPath(cwd) {
  return join(nxyRuntimeDir(cwd), 'metrics', 'gate.jsonl');
}
