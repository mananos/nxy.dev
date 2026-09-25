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

export const RETRY = 'Retry';
export const CONTINUE = 'Continue anyway';
export const STOP = 'Stop';

/**
 * @typedef {{kind: 'edit'} | {kind: 'run', command: string, ok: boolean, error?: string}} Event
 * @typedef {'pass' | 'fail' | 'not-run' | 'manual' | 'none'} Status
 * @typedef {{status: Status, error?: string}} Verdict
 */

/** Red: the approved criterion did not pass. `manual` and `none` are not red — they were never runnable. */
export const isRed = (status) => status === 'fail' || status === 'not-run';

/** The batch a dispatch is for: "Batch 2 — ..." in the prompt, outside nxy's own context block. */
export function batchOfPrompt(prompt) {
  const m = /\bBatch\s+(\d+)\b/i.exec(stripContextBlock(prompt));
  return m ? Number(m[1]) : null;
}

const squash = (s) => String(s || '').replace(/\s+/g, ' ').trim();

/**
 * Whether a shell command ran the acceptance command in a way whose exit status is the command's
 * own. A prefix is fine (`cd api && …`, `rtk …`); after it only redirections or `&& …` are — a pipe,
 * `;` or `||` would report the exit status of something else, and a red test would read green.
 * @param {string} run
 * @param {string} accept
 */
export function runMatches(run, accept) {
  const r = squash(run);
  const a = squash(accept);
  if (!a) return false;
  const i = r.lastIndexOf(a);
  if (i < 0) return false;
  return /^\s*(?:\d?>>?&?\s*\S+\s*)*(?:&&.*)?$/.test(r.slice(i + a.length));
}

/**
 * The verdict of a batch from what its implementer did, in order. The last matching run after the
 * last edit decides; a run before an edit proves nothing about the code as it was left.
 * @param {Event[]} events
 * @param {import('./plan.mjs').Accept} accept
 * @returns {Verdict}
 */
export function batchVerdict(events, accept) {
  if (accept.kind === 'manual') return { status: 'manual' };
  if (accept.kind !== 'command') return { status: 'none' };
  let lastEdit = -1;
  events.forEach((e, i) => {
    if (e.kind === 'edit') lastEdit = i;
  });
  /** @type {Verdict} */
  let verdict = { status: 'not-run' };
  events.forEach((e, i) => {
    if (i > lastEdit && e.kind === 'run' && runMatches(e.command, accept.command)) {
      verdict = e.ok ? { status: 'pass' } : { status: 'fail', ...(e.error ? { error: e.error } : {}) };
    }
  });
  return verdict;
}

/** A batch that others may build on: it passed, or there was nothing to run. */
const isDone = (status) => status === 'pass' || status === 'manual' || status === 'none';

/**
 * The first batch `n` depends on that is not done — red and not waved through by the user, or not
 * carried out yet — or null. Independent batches never wait for each other (0.4.4).
 * @param {Record<string, {status: Status}>} batches recorded verdicts by batch number
 * @param {number[]} depends the batches `n` builds on
 * @param {(k: number) => boolean} continued whether the user chose "Continue anyway" for batch k
 * @returns {{k: number, why: 'red' | 'pending'}|null}
 */
export function blockingBatch(batches, depends, continued) {
  for (const k of [...depends].sort((a, b) => a - b)) {
    const status = batches?.[String(k)]?.status;
    if (isDone(status)) continue;
    if (isRed(status)) {
      if (continued(k)) continue;
      return { k, why: 'red' };
    }
    return { k, why: 'pending' };
  }
  return null;
}

/** The exact question the dispatch hook looks for when a batch is red. */
export function continueQuestion(hash, n) {
  return `Batch ${n} of nxy plan ${hash} did not pass — how to continue?`;
}

/**
 * What the implementer is told when it tries to stop with its batch unproven. Said once: a second
 * stop goes through and the batch is recorded red.
 * @param {number} n @param {string} command @param {Verdict} verdict
 */
export function stopMessage(n, command, verdict) {
  const why = verdict.status === 'fail'
    ? `it failed after your last edit${verdict.error ? ` (${verdict.error})` : ''}`
    : 'it has not been run after your last edit';
  return [
    `nxy verify: batch ${n}'s acceptance command has not passed — ${why}.`,
    `Run it exactly as written, not piped and not in the background: \`${command}\``,
    'If it fails because of your change, fix it and run it again. If it was already failing before your change, say so in your Notes and stop.',
  ].join('\n');
}

/**
 * The line the main thread sees when an implementer returns, and what to do next.
 * A fix from the review's checkpoint 2 (`fix`) closes with its own line: one correction round, so
 * it never leads to another suite and another review.
 * `dependents`: the batches that build on this one (default: the next one).
 * @param {{hash: string, n: number, command?: string, verdict: Verdict, batches: number[], recorded: Record<string, {status: Status}>, fix?: boolean,
 *   dependents?: number[]}} o
 */
export function afterBatch(o) {
  const { hash, n, verdict } = o;
  const cmd = o.command ? ` \`${o.command}\`` : '';
  if (isRed(verdict.status)) {
    const waiting = o.dependents ?? o.batches.filter((k) => k > n).slice(0, 1);
    const input = {
      questions: [{
        question: continueQuestion(hash, n),
        header: 'Verify',
        multiSelect: false,
        options: [
          { label: RETRY, description: `dispatch the implementer for batch ${n} again, with the failure` },
          { label: CONTINUE, description: 'carry on with the next batch (e.g. it was already failing before this change)' },
          { label: STOP, description: 'stop here and report' },
        ],
      }],
    };
    return [
      `nxy verify: batch ${n} ✘${cmd} ${verdict.status === 'fail' ? `failed after the last edit${verdict.error ? ` (${verdict.error})` : ''}` : 'was not run after the last edit'}.`,
      `Re-dispatch the implementer for batch ${n} with the failure (always allowed), or ask the user with AskUserQuestion, with exactly this input:`,
      `  ${JSON.stringify(input)}`,
      waiting.length
        ? `${waiting.length === 1 ? `Batch ${waiting[0]}` : `Batches ${waiting.join(', ')}`} will not be dispatched until batch ${n} passes or the user picks "${CONTINUE}".`
        : '',
    ].filter(Boolean).join('\n');
  }
  const mark = verdict.status === 'pass' ? `✔${cmd} passed` : verdict.status === 'manual' ? '– manual: the user checks it' : '– no acceptance command';
  const lines = [`nxy verify: batch ${n} ${mark}.`];
  if (o.fix) {
    lines.push('Review fix done. One correction round only: no new suite or review for it; tell the user what was fixed and what was left.');
    return lines.join('\n');
  }
  const done = o.batches.every((k) => {
    const s = (k === n ? verdict : o.recorded[String(k)])?.status;
    return s === 'pass' || s === 'manual' || s === 'none';
  });
  if (done && o.batches.length) {
    lines.push(`All ${o.batches.length} batch${o.batches.length === 1 ? '' : 'es'} of plan ${hash} verified. Next: dispatch the nxy:tester subagent once with "Full suite for nxy plan ${hash}" — it runs the suite of every repo the plan touched.`);
  }
  return lines.join('\n');
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
 * @param {Record<string, {status: Status, error?: string}>} recorded
 * @param {{id: string, change: number, preexisting: number, chosen: number}|null} [review]
 */
export function progressLine(batches, recorded, review = null) {
  if (!batches.length) return '';
  const mark = (n) => {
    const r = recorded?.[String(n)];
    if (!r) return `${n} pending`;
    if (r.status === 'pass') return `${n} ✔`;
    if (r.status === 'manual') return `${n} – manual`;
    if (r.status === 'none') return `${n} – no command`;
    return `${n} ✘${r.status === 'not-run' ? ' (not run)' : r.error ? ` (${r.error})` : ''}`;
  };
  const rv = review ? ` · review ${review.id}: ${review.change} from the change, ${review.chosen} chosen to fix, ${review.preexisting} preexisting` : '';
  return `Progress (recorded by nxy, not by the model): batch ${batches.map((b) => mark(b.n)).join(', ')}${rv}`;
}

/** @param {number[]} batches */
export function unnamedBatchMessage(batches) {
  return `nxy: this branch has an approved plan; say which batch this dispatch carries out, so it can be verified. Start the prompt with "Batch N — ..." (batches: ${batches.join(', ')}).`;
}
