// @ts-check
/**
 * What the main thread must hear when one of nxy's agents has finished, from recorded state only
 * (`.nxy/local/verify/<plan>/`, written by the SubagentStop hook; the agent's own report is never read).
 *
 * Shared by the sync PostToolUse:Agent path (the agent just returned) and by SubagentStop (a background
 * agent ended: the lines become a note for the next hook on the main thread).
 */
import { statSync } from 'node:fs';
import { gitBranch } from '../../core/paths.mjs';
import { extractPlan, parseBatches, planHash } from '../../core/plan.mjs';
import {
  afterBatch, afterReviewer, afterSuiteFix, afterTester, batchOfPrompt, reviewInstruction, suiteFixOfPrompt,
} from '../../core/verify.mjs';
import { lookupHandoff } from './handoff.mjs';
import { readSuite, readSuiteFix, readVerify, reviewLaunchTs, reviewRecorded } from './verify-state.mjs';

/** The review instruction (or why there is none) once the suite is green. */
async function reviewNext(cwd, hash) {
  const { reviewFiles } = await import('./baseline.mjs');
  const { reviewNeeded } = await import('../../core/review.mjs');
  const need = reviewNeeded(reviewFiles(cwd, gitBranch(cwd), hash).files.map((f) => f.path));
  return reviewInstruction({ hash, project: cwd, needed: need.needed, reason: need.reason });
}

/**
 * Whether the reviewer had something to review: the plan's review is needed (there are changed files that
 * call for one: the same rule that dispatches the reviewer), or a packet was saved since the launch.
 * Only when neither holds may the reviewer return without recording (a rightly "no review needed" answer
 * is neither sent back nor reported as silent).
 * @param {string} cwd @param {string} hash
 */
export async function reviewExpected(cwd, hash) {
  try {
    const { reviewFiles } = await import('./baseline.mjs');
    const { reviewNeeded } = await import('../../core/review.mjs');
    if (reviewNeeded(reviewFiles(cwd, gitBranch(cwd), hash).files.map((f) => f.path)).needed) return true;
  } catch { /* fall through to the packet */ }
  try {
    const { packetPath } = await import('./baseline.mjs');
    return statSync(packetPath(cwd, gitBranch(cwd))).mtimeMs >= reviewLaunchTs(cwd, hash) - 2000;
  } catch {
    return false;
  }
}

/**
 * @param {{cwd: string, role: 'implementer' | 'tester' | 'reviewer', prompt?: string}} o
 * @returns {Promise<{lines: string[], pause: boolean}>}
 */
export async function completionLines({ cwd, role, prompt = '' }) {
  /** @type {string[]} */
  const lines = [];
  let pause = false;
  const known = await lookupHandoff(cwd);
  const plan = known.handoff ? extractPlan(known.handoff.body) : null;
  if (!plan) return { lines, pause: role === 'reviewer' };
  const hash = planHash(plan);

  if (role === 'tester') {
    const suite = readSuite(cwd, hash);
    if (suite?.status !== 'done') {
      // Still running, or never recorded: nothing proves the suite is green.
      lines.push(`nxy: the full suite of plan ${hash} has no recorded result (${suite ? 'it is still running' : 'the tester left none'}): do not review yet; dispatch nxy:tester again once if it is not running.`);
      return { lines, pause };
    }
    const red = suite.red || [];
    lines.push(afterTester({ hash, red, reviewNext: red.length ? undefined : await reviewNext(cwd, hash) }));
    pause = true;
  } else if (role === 'reviewer') {
    if (reviewRecorded(cwd, hash)) { /* the record command printed the verdict itself */ }
    else if (await reviewExpected(cwd, hash)) lines.push(afterReviewer({ hash, recorded: false }));
    else lines.push(`nxy: the reviewer returned and saved no review packet for plan ${hash}: there was nothing to review.`);
    pause = true;
  } else if (suiteFixOfPrompt(prompt)) {
    const rec = readSuiteFix(cwd, hash);
    if (rec?.status === 'done') {
      const red = rec.red || [];
      const first = /** @type {any} */ (red[0]);
      // No suite command to re-run: not verified, never a pass, and the review does not open.
      const none = !red.length && !(rec.ran || []).length;
      const verdict = none ? { status: 'none' } : red.length ? { status: first?.status === 'not-run' ? 'not-run' : 'fail', error: first?.error } : { status: 'pass' };
      lines.push(afterSuiteFix({
        hash, verdict: /** @type {any} */ (verdict), commands: red.length ? red.map((r) => r.command) : rec.ran || [],
        reviewNext: red.length || none ? undefined : await reviewNext(cwd, hash),
      }));
      pause = true;
    }
  } else {
    const n = batchOfPrompt(prompt);
    const batches = parseBatches(plan);
    const batch = batches.find((b) => b.n === n);
    if (batch && n != null) {
      const state = readVerify(cwd, gitBranch(cwd), hash);
      const recorded = state.batches[String(n)];
      if (recorded && recorded.status !== 'running') {
        lines.push(afterBatch({
          hash, n, verdict: recorded, recorded: state.batches,
          command: batch.accept.kind === 'command' ? batch.accept.command : undefined,
          batches: batches.map((b) => b.n),
          dependents: batches.filter((b) => b.depends.includes(n)).map((b) => b.n),
          fix: /\bfix R\d+ of review\b/.test(prompt),
        }));
        pause = true;
      }
    }
  }
  return { lines, pause };
}
