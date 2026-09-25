// @ts-check
/**
 * Claude Code side of the live handoff: how the commands are spelled for this host, and the one
 * lookup the hooks share.
 *
 * The store is imported lazily. Loading `node:sqlite` is the only non-trivial cost these hooks
 * have, and the happy path of the edit hook (a cheap session) must never pay it.
 */
import { join } from 'node:path';
import { PLUGIN_ROOT, gitBranch } from '../../core/paths.mjs';

const MEM = join(PLUGIN_ROOT, 'hosts', 'claude-code', 'entries', 'mem.mjs').replace(/\\/g, '/');

/**
 * A command the model can run as-is from the Bash tool. Forward slashes work in Git Bash, WSL and
 * PowerShell alike; the flag silences node:sqlite's ExperimentalWarning (see store.mjs).
 * @param {string} args
 */
export function memCommand(args) {
  return `node --disable-warning=ExperimentalWarning "${MEM}" ${args}`;
}

const REVIEW = join(PLUGIN_ROOT, 'hosts', 'claude-code', 'entries', 'review.mjs').replace(/\\/g, '/');

/** @param {string} args */
export function reviewCommand(args) {
  return `node --disable-warning=ExperimentalWarning "${REVIEW}" ${args}`;
}

/**
 * Project, branch and live handoff for a checkout.
 * @param {string} cwd
 */
export async function lookupHandoff(cwd) {
  const { projectKey } = await import('../../core/memory/scope.mjs');
  const { openStore } = await import('../../core/memory/store.mjs');
  const { liveHandoff } = await import('../../core/memory/handoff.mjs');
  const project = projectKey(cwd).key;
  const branch = gitBranch(cwd);
  const db = openStore();
  try {
    return { project, branch, handoff: liveHandoff(db, project, branch) };
  } finally {
    db.close();
  }
}

/**
 * Whether the handoff rule applies to this call, and if so, the refusal text. Shared by the edit
 * hook and the agent hook so both refuse with the same words for the same reason.
 * `known` skips the lookup when the caller already did it (the agent hook needs the handoff anyway).
 * @param {{cwd: string, cfg: import('../../core/config.mjs').NxyConfig, isSubagent: boolean, contextTokens: number|null, then: string,
 *   known?: {branch: string|null, handoff: import('../../core/memory/store.mjs').Memory|null}}} o
 * @returns {Promise<{block: boolean, reason: string, message?: string}>}
 */
export async function handoffCheck(o) {
  const { decideHandoff, handoffDenyMessage } = await import('../../core/memory/handoff.mjs');
  const input = {
    // The rule lives on the gate's threshold, so it goes with the gate: someone who ran `/nxy:gate off`
    // expects no refusal on edits past it.
    required: o.cfg.memory?.handoff?.required !== false && o.cfg.gate?.enabled !== false,
    isSubagent: o.isSubagent,
    contextTokens: o.contextTokens,
    threshold: Number(o.cfg.gate?.contextTokens) || 0,
    hasHandoff: false,
  };
  // Decide first without touching the store: only a call that would block pays for the lookup.
  const pre = decideHandoff(input);
  if (!pre.block) return pre;
  const { branch, handoff } = o.known || await lookupHandoff(o.cwd);
  const verdict = decideHandoff({ ...input, hasHandoff: Boolean(handoff) });
  if (!verdict.block) return verdict;
  return { ...verdict, message: handoffDenyMessage({ saveCmd: memCommand('handoff save'), branch, then: o.then }) };
}
