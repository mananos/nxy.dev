// @ts-check
/**
 * The write gate: decides whether the main thread should edit a file itself, or hand the edit to
 * the implementer.
 *
 * It decides **by the size of the main thread's context**, not by the size of the change and not
 * by a declared tier (roadmap tema 9). The reasoning: editing was never the expensive part —
 * editing while carrying 200k of context is. To edit, the main thread first reads the file, and
 * those tokens do not get paid once: they stay in its context and are re-read on every later call
 * (cache reads are 92-97% of a real session's tokens). The implementer pays them once and dies.
 *
 * What falls out of that, and is intended:
 *  - A tiny session edits freely. Nobody has to declare "this is a small task" — it just never
 *    reaches the threshold.
 *  - A one-line fix in an already-loaded session *is* delegated. The fix is not what costs; the
 *    150k call around it is.
 *
 * This module is pure: it takes numbers and returns a verdict, so it can be tested without a
 * transcript, a hook or a host.
 */

/**
 * @typedef {object} GateConfig
 * @property {boolean} enabled
 * @property {number} contextTokens   threshold; at or above it the main thread stops editing
 * @property {number} escapeMinutes   how long a `/nxy:gate once` escape stays valid
 */

/**
 * @typedef {object} GateInput
 * @property {number|null} contextTokens  main thread's current context, null when unknown
 * @property {boolean} isSubagent         the implementer must always be allowed to write
 * @property {boolean} hasImplementer     blocking with nowhere to delegate is just nagging
 * @property {boolean} escapeActive       a one-shot escape the user asked for
 * @property {string} [tool]              Edit / Write / NotebookEdit, for the message
 * @property {string} [file]              path being edited, for the message
 */

/**
 * @param {GateInput} input
 * @param {GateConfig} cfg
 * @returns {{allow: boolean, reason: string, message?: string}}
 */
export function decideGate(input, cfg) {
  if (!cfg || cfg.enabled === false) return { allow: true, reason: 'disabled' };
  // The implementer edits. Gating it would deadlock the whole design.
  if (input.isSubagent) return { allow: true, reason: 'subagent' };
  if (!input.hasImplementer) return { allow: true, reason: 'no-implementer' };
  if (input.escapeActive) return { allow: true, reason: 'escape' };
  // Fail open: if we cannot read the context, we do not guess. A gate that blocks on missing
  // data would break editing entirely the first time a transcript format changes.
  if (typeof input.contextTokens !== 'number' || !Number.isFinite(input.contextTokens)) {
    return { allow: true, reason: 'context-unknown' };
  }
  const threshold = cfg.contextTokens;
  if (!Number.isFinite(threshold) || threshold <= 0) return { allow: true, reason: 'no-threshold' };
  if (input.contextTokens < threshold) return { allow: true, reason: 'under-threshold' };
  return {
    allow: false,
    reason: 'over-threshold',
    message: denyMessage(input.contextTokens, threshold, input.file),
  };
}

const k = (n) => (n >= 1000 ? `${Math.round(n / 1000)}k` : String(n));

/**
 * The only text in this feature that costs tokens, and only when it actually blocks. So it has to
 * do three things at once: say why, say exactly what to do instead, and say how to override.
 */
function denyMessage(contextTokens, threshold, file) {
  const target = file ? ` \`${file}\`` : '';
  return [
    `nxy: main thread context is ${k(contextTokens)} > ${k(threshold)} — do not edit${target} yourself.`,
    '',
    'Dispatch the `implementer` subagent with: the exact `path:line`, the concrete change, and an acceptance criterion.',
    'Do not read the file in order to pass it along — the implementer reads it, and that is precisely the saving.',
    '',
    'If doing it here really is cheaper (one line, session about to end), the user can run `/nxy:gate once`.',
  ].join('\n');
}

/**
 * Human-readable status for `/nxy:gate status`.
 * @param {GateConfig} cfg
 * @param {{contextTokens: number|null, hasImplementer: boolean, escapeUntil: number|null}} state
 */
export function describeGate(cfg, state) {
  const now = Date.now();
  const escape = state.escapeUntil && state.escapeUntil > now
    ? `armed, ${Math.ceil((state.escapeUntil - now) / 60000)} min left`
    : 'no';
  return [
    `gate:            ${cfg.enabled === false ? 'DISABLED' : 'on'} (threshold ${k(cfg.contextTokens)} tokens of main-thread context)`,
    `context now:     ${typeof state.contextTokens === 'number' ? k(state.contextTokens) : 'unknown (only measurable inside a hook)'}`,
    `implementer:     ${state.hasImplementer ? 'available' : 'unavailable -> the gate never blocks'}`,
    `one-shot escape: ${escape}`,
  ].join('\n');
}
