// @ts-check
/**
 * Verification of a plan's batches (0.4.1).
 *
 * What has to hold: a batch is green only when its `Accept:` command — written by the planner,
 * approved by the user with the plan — ran **after the batch's last edit** and exited cleanly. The
 * implementer runs it (it has the context and can fix what it broke); nxy reads the verdict from
 * the implementer's transcript, never from its report (the same lesson as the plan approval: what
 * the runtime recorded, not what the model says).
 *
 * A red batch stops the next one: later batches build on it. Retrying the same batch is always
 * allowed; carrying on regardless is the user's call ("Continue anyway" also covers "it was already
 * failing before this change").
 *
 * Pure: events in, verdicts and messages out. Reading transcripts is the host's job.
 */
import { stripContextBlock } from './memory/handoff.mjs';
import { RETRY, CONTINUE, STOP, isRed, isDone, blockingBatch, continueQuestion, afterBatch, reviewInstruction } from './batch-status.mjs';

// The status helpers and the wording the main thread sees live in batch-status.mjs (import-free, so
// the Mod's module can load them); everything stays importable from here.
export { RETRY, CONTINUE, STOP, isRed, isDone, blockingBatch, continueQuestion, afterBatch, reviewInstruction };

/**
 * @typedef {{kind: 'edit'} | {kind: 'run', command: string, ok: boolean, error?: string}} Event
 * @typedef {import('./batch-status.mjs').Status} Status
 * @typedef {import('./batch-status.mjs').Verdict} Verdict
 */

/** The batch a dispatch is for: "Batch 2 — ..." in the prompt, outside nxy's own context block. */
export function batchOfPrompt(prompt) {
  if (suiteFixOfPrompt(prompt)) return null;
  const m = /\bBatch\s+(\d+)\b/i.exec(stripContextBlock(prompt));
  return m ? Number(m[1]) : null;
}

const squash = (s) => String(s || '').replace(/\s+/g, ' ').trim();

/**
 * Canonical form of a command for comparing equivalent ways of typing it: whitespace squashed,
 * backslashes as slashes, quotes dropped, no leading `./` on a token, no `.exe`, no PowerShell `& `.
 * @param {string} s
 */
export function canonCommand(s) {
  return squash(s)
    .replace(/\\/g, '/')
    .replace(/["']/g, '')
    .replace(/(^|\s)\.\//g, '$1')
    .replace(/\.exe(?=\s|$)/gi, '')
    .replace(/^&\s+/, '');
}

/**
 * Whether a shell command ran the acceptance command in a way whose exit status is the command's
 * own. A prefix is fine (`cd api && …`, `rtk …`); after it only redirections or `&& …` are — a pipe,
 * `;` or `||` would report the exit status of something else, and a red test would read green.
 * @param {string} run
 * @param {string} accept
 * @param {string} [root] the project root (see `inspectRun`)
 */
export function runMatches(run, accept, root) {
  return inspectRun(run, accept, root).match;
}

const REDIRECTIONS = /^\s*(?:\d?>>?&?\s*\S+\s*)*/;

/** `<root>/` out of a canonical command (also the Git Bash `/c/...` form of a drive), case-insensitive. */
function stripRoot(s, root) {
  const base = canonCommand(root || '').replace(/\/+$/, '');
  if (!base) return s;
  const forms = [base];
  const drive = /^([a-z]):(\/.*)?$/i.exec(base);
  if (drive) forms.push(`/${drive[1]}${drive[2] || ''}`);
  let out = s;
  for (const f of forms) {
    out = out.replace(new RegExp(`(?<=^|\\s)${f.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}/`, 'gi'), '');
  }
  return out;
}

/**
 * Like `runMatches`, and when the run contains the accept command but nothing certifies it, says why.
 * @param {string} run
 * @param {string} accept
 * @param {string} [root] the project root: a path under it written absolute equals the relative one
 * @returns {{match: boolean, why?: string}}
 */
export function inspectRun(run, accept, root) {
  const r = stripRoot(canonCommand(run), root);
  const a = stripRoot(canonCommand(accept), root);
  if (!a) return { match: false };
  const i = r.lastIndexOf(a);
  if (i < 0) return { match: false };
  const tail = r.slice(i + a.length);
  if (/^\s*(?:\d?>>?&?\s*\S+\s*)*(?:&&.*)?$/.test(tail)) return { match: true };
  const rest = tail.replace(REDIRECTIONS, '');
  if (rest.startsWith('||')) return { match: false, why: 'followed by ||: a failure is hidden' };
  if (rest.startsWith('|')) {
    const word = /^\|\s*(\S+)/.exec(rest)?.[1];
    return { match: false, why: `piped through ${word || 'another command'}: the exit code is hidden` };
  }
  if (rest.startsWith(';')) return { match: false, why: 'followed by ;: the exit code is the last command one' };
  return { match: false };
}

/**
 * The verdict of a batch from what its implementer did, in order. The last matching run after the
 * last edit decides; a run before an edit proves nothing about the code as it was left.
 * @param {Event[]} events
 * @param {import('./plan.mjs').Accept} accept
 * @param {string} [root] the project root, for absolute paths under it
 * @returns {Verdict}
 */
export function batchVerdict(events, accept, root) {
  if (accept.kind === 'manual') return { status: 'manual' };
  if (accept.kind !== 'command') return { status: 'none' };
  let lastEdit = -1;
  events.forEach((e, i) => {
    if (e.kind === 'edit') lastEdit = i;
  });
  /** @type {Verdict} */
  let verdict = { status: 'not-run' };
  let nearMiss = '';
  let before = false;
  events.forEach((e, i) => {
    if (e.kind !== 'run') return;
    const r = inspectRun(e.command, accept.command, root);
    if (i > lastEdit) {
      if (r.match) verdict = e.ok ? { status: 'pass' } : { status: 'fail', ...(e.error ? { error: e.error } : {}) };
      else if (r.why) nearMiss = r.why;
    } else if (r.match && e.ok) before = true;
  });
  if (verdict.status === 'not-run') {
    if (nearMiss) verdict = { status: 'not-run', detail: nearMiss };
    else if (before) verdict = { status: 'not-run', detail: 'it ran before the last edit' };
  }
  return verdict;
}

/**
 * What the implementer is told when it tries to stop with its batch unproven. Said once: a second
 * stop goes through and the batch is recorded red.
 * @param {number} n @param {string} command @param {Verdict} verdict
 */
export function stopMessage(n, command, verdict) {
  const why = verdict.status === 'fail'
    ? `it failed after your last edit${verdict.error ? ` (${verdict.error})` : ''}`
    : `it has not been run after your last edit${verdict.detail ? `: ${verdict.detail}` : ''}`;
  return [
    `nxy verify: batch ${n}'s acceptance command has not passed — ${why}.`,
    `Run it exactly as written, not piped and not in the background: \`${command}\``,
    'If it fails because of your change, fix it and run it again. If it was already failing before your change, say so in your Notes and stop.',
  ].join('\n');
}

/**
 * @param {string} hash @param {number} k the batch in the way @param {number} n the batch being dispatched
 * @param {'red' | 'pending'} [why]
 */
export function blockedDispatchMessage(hash, k, n, why = 'red') {
  if (why === 'pending') {
    return `nxy: batch ${n} of plan ${hash} builds on batch ${k}, which has not passed yet. Dispatch batch ${k} first (or wait for it); batches that do not depend on each other can run in parallel.`;
  }
  return [
    `nxy: batch ${k} of plan ${hash} did not pass its acceptance command, and batch ${n} builds on it.`,
    `Re-dispatch the implementer for batch ${k} with the failure, or ask the user "${continueQuestion(hash, k)}" with AskUserQuestion (header "Verify", options "${RETRY}" / "${CONTINUE}" / "${STOP}").`,
    `Batch ${n} goes through once batch ${k} passes or the user picks "${CONTINUE}".`,
  ].join('\n');
}

/**
 * The session cut (0.4.4), suggested at a natural pause when the main thread's context is past the
 * statusline's warning: every call re-reads all of it. The user decides; nxy never clears.
 * @param {number} tokens @param {number} warn @param {string} saveCmd @param {string} showCmd
 */
export function cutSuggestion(tokens, warn, saveCmd, showCmd) {
  const k = (n) => `${Math.round(n / 1000)}k`;
  return [
    `nxy: the main thread carries ${k(tokens)} tokens of context (the statusline warns at ${k(warn)}), and every call re-reads all of it.`,
    `This is a good point to cut: bring the handoff's Done/Next up to date (\`${saveCmd}\`), then suggest the user run /clear and continue in the new session with \`${showCmd}\` — the plan and its verified batches carry over. Say it once; the user decides.`,
  ].join('\n');
}

/**
 * The plan's progress, derived from what nxy recorded (0.4.5): never written by the model, so it is
 * never stale. Shown by `handoff show` and in the implementer's context block.
 * @param {{n: number}[]} batches
 * @param {Record<string, {status: Status, error?: string, detail?: string}>} recorded
 * @param {{id: string, change: number, preexisting: number, chosen: number}|null} [review]
 */
export function progressLine(batches, recorded, review = null) {
  if (!batches.length) return '';
  const mark = (n) => {
    const r = recorded?.[String(n)];
    if (!r) return `${n} pending`;
    if (r.status === 'running') return `${n} running`;
    if (r.status === 'pass') return `${n} ✔`;
    if (r.status === 'manual') return `${n} – manual`;
    if (r.status === 'none') return `${n} – no command`;
    return `${n} ✘${r.status === 'not-run' ? ` (not run${r.detail ? `: ${r.detail}` : ''})` : r.error ? ` (${r.error})` : ''}`;
  };
  const rv = review ? ` · review ${review.id}: ${review.change} from the change, ${review.chosen} chosen to fix, ${review.preexisting} preexisting` : '';
  return `Progress (recorded by nxy, not by the model): batch ${batches.map((b) => mark(b.n)).join(', ')}${rv}`;
}

/**
 * @param {number[]} batches
 * @param {boolean} [allVerified] every batch is done: a post-suite fix has its own slot
 */
export function unnamedBatchMessage(batches, allVerified = false) {
  const suiteFix = allVerified ? ' Every batch is verified, so a fix after the full suite starts with "Suite fix — <what failed>".' : '';
  return `nxy: this branch has an approved plan; say which batch this dispatch carries out, so it can be verified. Start the prompt with "Batch N — ..." (batches: ${batches.join(', ')}).${suiteFix}`;
}

/** The suite fix a dispatch is for: the prompt, outside nxy's context block, starts with "Suite fix". */
export function suiteFixOfPrompt(prompt) {
  return /^\s*Suite\s+fix\b/i.test(stripContextBlock(prompt));
}

/**
 * The verdict of a post-suite fix: every suite command must have run green after the last edit.
 * @param {Event[]} events
 * @param {string[]} commands
 * @param {string} [root] the project root, for absolute paths under it
 * @returns {Verdict & {failed?: string[]}}
 */
export function suiteFixVerdict(events, commands, root) {
  if (!commands.length) return { status: 'none' };
  /** @type {string[]} */
  const failed = [];
  /** @type {Verdict} */
  let first = { status: 'pass' };
  for (const command of commands) {
    const v = batchVerdict(events, { kind: 'command', command }, root);
    if (v.status !== 'pass') {
      failed.push(command);
      if (first.status === 'pass') first = v;
    }
  }
  return failed.length ? { ...first, failed } : { status: 'pass' };
}

/**
 * What the main thread hears when the full suite ends.
 * @param {{hash: string, red: {command: string, error?: string, detail?: string}[], reviewNext?: string}} o
 */
export function afterTester({ hash, red, reviewNext }) {
  if (!red.length) return reviewNext || `nxy: full suite done for plan ${hash}.`;
  const list = red.map((r) => `\`${r.command}\`${r.error ? ` (${r.error})` : r.detail ? ` (${r.detail})` : ''}`).join(', ');
  return [
    `nxy: full suite of plan ${hash} ✘ — failed: ${list}.`,
    'Dispatch nxy:implementer once with "Suite fix — <what failed>"; nxy verifies it by re-running those commands. Then the review.',
  ].join('\n');
}

/**
 * What the main thread hears when a post-suite fix returns.
 * @param {{hash: string, verdict: Verdict, commands: string[], reviewNext?: string}} o
 */
export function afterSuiteFix({ hash, verdict, commands, reviewNext }) {
  const list = commands.map((c) => `\`${c}\``).join(', ');
  if (verdict.status === 'none' || !commands.length) {
    return `nxy verify: suite fix not verified for plan ${hash}: no suite command to re-run — tell the user.`;
  }
  if (verdict.status === 'pass') {
    return `nxy verify: suite fix ✔ ${list} passed for plan ${hash}.${reviewNext ? `\n${reviewNext}` : ''}`;
  }
  return [
    `nxy verify: suite fix ✘ ${verdict.status === 'fail' ? `failed after the last edit${verdict.error ? ` (${verdict.error})` : ''}` : `did not run the suite commands after the last edit${verdict.detail ? ` (${verdict.detail})` : ''}`}: ${list}.`,
    'Dispatch nxy:implementer again once with the failure ("Suite fix — ..."), or tell the user.',
  ].join('\n');
}

/**
 * Empty when the reviewer recorded its review; otherwise what to do about it.
 * @param {{hash: string, recorded: boolean}} o
 */
export function afterReviewer({ hash, recorded }) {
  if (recorded) return '';
  return `nxy: the reviewer returned without recording a review of plan ${hash}: dispatch nxy:reviewer again once, reading the packet in parts and ending with the record command; if it fails again tell the user.`;
}

/**
 * The reviewer recorded its review: what the main thread must do with it.
 * @param {{hash: string, id: string}} o
 */
export function afterReviewRecorded({ hash, id }) {
  return `nxy: review ${id} of plan ${hash} was recorded; checkpoint 2 (the findings to fix) is in the reviewer's report: show it to the user.`;
}

/**
 * A background agent was launched: its verdict comes when it ends.
 * @param {'batch' | 'suite-fix' | 'tester' | 'reviewer'} kind @param {number} [n]
 */
export function launchNote(kind, n) {
  const what = kind === 'batch' ? `batch ${n}` : kind === 'suite-fix' ? 'the suite fix' : kind === 'tester' ? 'the full suite' : 'the review';
  return `nxy: ${what} launched in the background; nxy will report its verdict when it ends: do not dispatch its dependents, the tester or the reviewer yet.`;
}
