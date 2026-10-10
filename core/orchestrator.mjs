// @ts-check
/**
 * The orchestrator's brain (1.1.0): after the user approves a plan, a pure state machine decides what
 * runs next — implementer batches (in parallel by `Depends:`), the full suite, the review — and when
 * the main thread gets the conversation back. The Mod's module executes the actions; this file only
 * decides, and holds the contracts the module and the node-side hooks share (prompts, tickets, texts).
 *
 * Pure: snapshot in, actions out. No `node:*`, no `process`, no dynamic import — the module cannot
 * load them. Its only import is batch-status.mjs, which is import-free too.
 */
import { RETRY, CONTINUE, STOP, isRed, isDone, blockingBatch, afterBatch, reviewInstruction } from './batch-status.mjs';

/** Where the orchestrator keeps its files, under nxy's runtime dir. */
export const ORCH_DIR = 'orch';
/** The marker: while it exists (and is fresh) the main thread may not dispatch implementer/tester/reviewer. */
export const MARKER_FILE = 'active.json';
/** One file per spawn the module is about to make; the dispatch hook consumes it. */
export const TICKET_DIR = 'tickets';
export const MARKER_TTL_MS = 2 * 60 * 60 * 1000;

/** Pane buttons of the pause ask. */
export const PAUSE_CONTINUE = 'Continue';
export const PAUSE_ADJUST = 'Adjust';
export const PAUSE_STOP = 'Stop';

/**
 * @typedef {'pass' | 'fail' | 'not-run' | 'manual' | 'none' | 'running'} Status
 * @typedef {{status: Status, error?: string, detail?: string, ts?: number}} OrchVerdict
 * @typedef {{
 *   hash: string,
 *   batches: {n: number, depends: number[]}[],
 *   verdicts: Record<string, OrchVerdict>,
 *   launched: Record<string, number>,
 *   continued?: number[], acked?: number[], retry?: number[],
 *   stopped?: boolean, adjust?: boolean, pauseAfterBatch?: boolean,
 *   suite?: {status?: 'running' | 'done', red?: {command?: string, error?: string}[]} | null, suiteLaunched?: boolean | number,
 *   review?: {id: string} | null, reviewLaunched?: boolean | number,
 *   reviewNeeded?: {needed: boolean, reason?: string},
 * }} Snap
 * @typedef {{role: 'implementer', batch: number, retry?: boolean, failure?: string} | {role: 'tester'} | {role: 'reviewer'}} Spawn
 * @typedef {{type: 'spawn'} & Spawn
 *   | {type: 'ask', kind: 'red', batch: number}
 *   | {type: 'ask', kind: 'pause', batches: number[]}
 *   | {type: 'wait', reason: string}
 *   | {type: 'handback', reason: 'checkpoint2' | 'suite-red' | 'review-skipped' | 'stopped' | 'adjust'}} Action
 */

/** A suite record is final only when SubagentStop wrote `done`; `running` is written at dispatch. */
export const suiteDone = (/** @type {NonNullable<Snap['suite']>} */ s) => s.status === 'done';
/** A finished suite is red when it lists red items (empty is green). */
export const suiteIsRed = (/** @type {NonNullable<Snap['suite']>} */ s) => Array.isArray(s.red) && s.red.length > 0;

/**
 * Per batch: what the orchestrator treats it as. A verdict older than the launch is the previous
 * attempt's, so the batch counts as running; a launch with no verdict yet is running too.
 * @param {Snap} snap
 * @returns {Record<string, {status: Status}>}
 */
export function effective(snap) {
  /** @type {Record<string, {status: Status}>} */
  const out = {};
  for (const { n } of snap.batches) {
    const v = snap.verdicts?.[String(n)];
    const launched = snap.launched?.[String(n)];
    if (v && v.status !== 'running' && !(launched != null && (v.ts ?? 0) < launched)) out[String(n)] = { status: v.status };
    else if (v || launched != null) out[String(n)] = { status: 'running' };
  }
  return out;
}

// The status helpers the driver shares (it may only import this file).
export { RETRY, CONTINUE, STOP, isRed, isDone };

/** Batches finished by the snapshot: done, or red and waved through. @param {Snap} snap @returns {number[]} */
export function finishedBatches(snap) {
  const eff = effective(snap);
  const continued = snap.continued ?? [];
  return snap.batches.map(({ n }) => n).filter((n) => {
    const s = eff[String(n)]?.status;
    return s != null && (isDone(s) || (isRed(s) && continued.includes(n)));
  });
}

/** @param {Snap} snap @returns {{phase: string, actions: Action[]}} */
export function nextActions(snap) {
  if (snap.stopped) return { phase: 'stopped', actions: [{ type: 'handback', reason: 'stopped' }] };
  if (snap.adjust) return { phase: 'stopped', actions: [{ type: 'handback', reason: 'adjust' }] };
  const continued = new Set(snap.continued ?? []);
  const acked = new Set(snap.acked ?? []);
  const retry = new Set(snap.retry ?? []);
  const eff = effective(snap);
  const isContinued = (k) => continued.has(k);
  const running = snap.batches.filter(({ n }) => eff[String(n)]?.status === 'running').map(({ n }) => n);
  const settled = (n) => { const s = eff[String(n)]?.status; return s != null && (isDone(s) || (isRed(s) && continued.has(n))); };
  const unstarted = snap.batches.filter(({ n }) => !eff[String(n)]);

  /** @type {Action[]} */
  const actions = [];
  // Retries first: the user asked for them explicitly.
  for (const { n } of snap.batches) {
    const s = eff[String(n)]?.status;
    if (retry.has(n) && s && isRed(s)) {
      const v = snap.verdicts[String(n)];
      actions.push({ type: 'spawn', role: 'implementer', batch: n, retry: true, failure: v?.error ?? v?.detail ?? v?.status });
    }
  }
  const finishedUnacked = snap.batches.some(({ n }) => settled(n) && !acked.has(n));
  const holdForPause = !!snap.pauseAfterBatch && finishedUnacked;
  if (!holdForPause) {
    for (const { n, depends } of unstarted) {
      if (!blockingBatch(eff, depends ?? [], isContinued)) actions.push({ type: 'spawn', role: 'implementer', batch: n });
    }
  }
  if (actions.length) return { phase: 'batches', actions };
  if (running.length) return { phase: 'batches', actions: [{ type: 'wait', reason: `batch ${running.join(', ')} running` }] };

  // Nothing runs and nothing spawns: a red batch, a pause, or the plan is carried out.
  const red = snap.batches.find(({ n }) => isRed(eff[String(n)]?.status) && !continued.has(n));
  if (red) return { phase: 'batches', actions: [{ type: 'ask', kind: 'red', batch: red.n }] };
  if (unstarted.length) {
    if (holdForPause) return { phase: 'pause', actions: [{ type: 'ask', kind: 'pause', batches: unstarted.map(({ n }) => n) }] };
    // Unstarted batches whose dependencies can never be done (should not happen: red ones ask above).
    return { phase: 'batches', actions: [{ type: 'wait', reason: 'batches blocked' }] };
  }
  if (!snap.batches.length) return { phase: 'batches', actions: [{ type: 'wait', reason: 'no batches' }] };

  if (!snap.suite || !suiteDone(snap.suite)) {
    return snap.suite || snap.suiteLaunched
      ? { phase: 'suite', actions: [{ type: 'wait', reason: 'full suite running' }] }
      : { phase: 'suite', actions: [{ type: 'spawn', role: 'tester' }] };
  }
  if (suiteIsRed(snap.suite)) return { phase: 'suite', actions: [{ type: 'handback', reason: 'suite-red' }] };
  if (snap.reviewNeeded && !snap.reviewNeeded.needed) return { phase: 'review', actions: [{ type: 'handback', reason: 'review-skipped' }] };
  if (snap.review) return { phase: 'done', actions: [{ type: 'handback', reason: 'checkpoint2' }] };
  return snap.reviewLaunched
    ? { phase: 'review', actions: [{ type: 'wait', reason: 'review running' }] }
    : { phase: 'review', actions: [{ type: 'spawn', role: 'reviewer' }] };
}

// ---- prompts: what the module hands each agent (same wording the main thread uses today) ----

/**
 * The plan's own batch section (it already starts "Batch N — " and ends with its Accept line).
 * @param {{text: string, failure?: string}} o
 */
export function batchPrompt({ text, failure }) {
  const base = String(text).trimEnd();
  return failure ? `${base}\n\nThe previous attempt did not pass: ${failure}. Fix that, then run the Accept command again.` : base;
}

/** @param {string} hash */
export const testerPrompt = (hash) => `Full suite for nxy plan ${hash}`;

/** @param {string} hash @param {string} project */
export const reviewerPrompt = (hash, project) => `Review nxy plan ${hash} (project: ${String(project).replace(/\\/g, '/')})`;

// ---- tickets: the sentinel description that tells the dispatch hook a spawn is the module's own ----

/** @param {string} nonce @param {string} label */
export const ticketDescription = (nonce, label) => `nxy-orch:${nonce} ${label}`;

/** @param {unknown} description @returns {{nonce: string, label: string} | null} */
export function ticketOf(description) {
  const m = /^nxy-orch:([A-Za-z0-9_-]+)(?: (.*))?$/s.exec(typeof description === 'string' ? description : '');
  return m ? { nonce: m[1], label: m[2] ?? '' } : null;
}

// ---- texts ----

/** Constant on purpose: identical on every request, so the prompt cache is not broken. */
export const COMPOSE_SECTION = [
  '## nxy orchestrator',
  'When the user approves an nxy plan, nxy\'s orchestrator may take over: it runs the batches, the full suite and the review itself, and hands the conversation back at checkpoint 2 (or when it needs you).',
  'While it runs, end your turn without dispatching the nxy:implementer, nxy:tester or nxy:reviewer subagents (nxy refuses those dispatches), and act on what nxy hands back.',
].join('\n');

/**
 * What the main thread is told when the orchestrator hands the conversation back.
 * `ctx`: {hash, project, n, command, verdict, batches, recorded, dependents, tester, review, request}.
 * @param {'checkpoint2' | 'suite-red' | 'review-skipped' | 'stopped' | 'adjust' | 'red' | 'pause' | 'failed'} reason
 * @param {any} ctx
 */
export function handbackText(reason, ctx = {}) {
  const hash = ctx.hash;
  switch (reason) {
    case 'red':
    case 'pause':
      return afterBatch(ctx);
    case 'checkpoint2':
      return [
        `nxy: plan ${hash} is carried out: every batch verified, the full suite done and the review finished by nxy's orchestrator${ctx.review?.id ? ` (review ${ctx.review.id})` : ''}.`,
        'Next: checkpoint 2. Present the reviewer\'s report below to the user, verbatim, and let them pick the findings to fix.',
        ctx.report ?? ctx.review?.report ?? '',
      ].filter(Boolean).join('\n');
    case 'suite-red':
      return [
        `nxy: the full suite of plan ${hash} is red${ctx.tester ? `:\n${ctx.tester}` : '.'}`,
        'Next: the "Suite fix" step as usual — dispatch the implementer to fix it, run the suite again, and only then go on to the review.',
      ].join('\n');
    case 'review-skipped':
      return reviewInstruction({ hash, project: ctx.project ?? '', needed: false, reason: ctx.reason });
    case 'failed': {
      const all = (ctx.batches ?? []).map(Number);
      const done = finishedBatches(/** @type {any} */ ({ batches: all.map((n) => ({ n })), verdicts: ctx.recorded ?? {}, continued: ctx.continued ?? [] }));
      const left = all.filter((n) => !done.includes(n));
      return [
        `nxy: the orchestrator failed while running plan ${hash} and handed it back.`,
        `Finished batches: ${done.length ? done.join(', ') : 'none'}. Left: ${left.length ? left.join(', ') : 'none'}.`,
        'Next: carry on with the plan by hand, the way it ran before the orchestrator: dispatch the nxy:implementer for each batch left, then the tester and the reviewer as usual.',
      ].join('\n');
    }
    case 'stopped':
      return `nxy: the user stopped plan ${hash} in the nxy panel${ctx.n != null ? ` at batch ${ctx.n}` : ''}. Report what was done and what is left; do not dispatch anything else for this plan.`;
    case 'adjust':
      return [
        `nxy: the user wants to adjust plan ${hash}${ctx.n != null ? ` after batch ${ctx.n}` : ''}. The orchestrator will not resume for this plan.`,
        `Ask the user what to change, then dispatch \`Batch ${ctx.n ?? '<n>'} — adjust: <request>\` through the usual path, and carry on with the plan by hand.`,
      ].join('\n');
    default:
      return `nxy: the orchestrator handed plan ${hash} back.`;
  }
}

/** @param {string} hash @param {string} releaseCmd */
export function denyMessage(hash, releaseCmd) {
  return [
    `nxy: the orchestrator is running plan ${hash}: it dispatches the implementer, tester and reviewer itself, so this dispatch is refused.`,
    'End your turn and wait: nxy hands the conversation back at checkpoint 2, or when it needs you.',
    `If nothing is running and this is stuck, release it with: ${releaseCmd}`,
  ].join('\n');
}

// ---- always-on panel (1.1.0): when node may run, the status line, the launcher and the idle view ----

/**
 * The one place that says when the Mod may run the snapshot (a node process).
 * @param {{trigger: 'ask' | 'turn' | 'agent' | 'press' | 'panel', asked?: boolean, owned?: boolean}} o
 */
export function wantsSnapshot({ trigger, asked = false, owned = false }) {
  if (trigger === 'ask' || trigger === 'panel') return true;
  if (trigger === 'turn') return !!asked || !!owned;
  if (trigger === 'agent' || trigger === 'press') return !!owned;
  return false;
}

/**
 * The status line under the prompt.
 * @param {Snap | null} snap the plan the orchestrator took over, or null
 * @param {Action | null} [ask] the open `ask` action, if any
 */
export function statusText(snap, ask = null) {
  const s = stateOf(snap, ask);
  return `${s.glyph} ${s.text}`;
}

/**
 * The state pill: glyph, text and tone (the engine prefixes the plugin name on the status line).
 * @param {Snap | null} snap @param {Action | null} [ask]
 * @returns {{glyph: string, text: string, tone: 'ok' | 'running' | 'error' | 'info'}}
 */
export function stateOf(snap, ask = null) {
  const ready = { glyph: '●', text: 'ready', tone: /** @type {'ok'} */ ('ok') };
  if (!snap) return ready;
  const { phase, actions } = nextActions(snap);
  const eff = effective(snap);
  const total = snap.batches.length;
  const doneCount = snap.batches.filter(({ n }) => isDone(eff[String(n)]?.status)).length;
  const open = ask && ask.type === 'ask' ? ask : actions.find((a) => a.type === 'ask') ?? null;
  if (open && open.type === 'ask') {
    if (open.kind === 'red') return { glyph: '✘', text: `batch ${open.batch} needs you`, tone: 'error' };
    const fin = finishedBatches(snap);
    return { glyph: '⏸', text: `paused after batch ${fin.length ? Math.max(...fin) : doneCount}`, tone: 'info' };
  }
  if (actions.some((a) => a.type === 'handback')) return ready;
  if (phase === 'batches') return { glyph: '▶', text: `batch ${Math.min(total, doneCount + 1)}/${total}`, tone: 'running' };
  if (phase === 'suite') return { glyph: '▶', text: 'tester', tone: 'running' };
  if (phase === 'review') return { glyph: '▶', text: 'review', tone: 'running' };
  return ready;
}

/** @param {unknown} v */
const isPlain = (v) => v != null && typeof v === 'object' && !Array.isArray(v);
/** Same deep-merge as core/config.mjs (this file may not import it). */
function merge(base, src) {
  const out = { ...base };
  for (const [k, v] of Object.entries(src || {})) out[k] = isPlain(v) && isPlain(base?.[k]) ? merge(base[k], v) : v;
  return out;
}

/**
 * The two settings the Mod needs before the first snapshot, from the config files' texts
 * (plugin defaults, user, project; broken or missing ones skipped, later wins).
 * @param {(string | null | undefined)[] | null} texts
 * @returns {{panel: 'auto' | 'off', gate: {enabled: boolean, contextTokens: number}}}
 */
export function readModConfig(texts) {
  let cfg = {};
  for (const t of texts ?? []) {
    if (typeof t !== 'string') continue;
    try {
      const j = JSON.parse(t);
      if (isPlain(j)) cfg = merge(cfg, j);
    } catch { /* a broken file is skipped */ }
  }
  return {
    panel: cfg.ui?.panel === 'off' ? 'off' : 'auto',
    gate: { enabled: cfg.gate?.enabled === true, contextTokens: Number(cfg.gate?.contextTokens) || 0 },
  };
}
