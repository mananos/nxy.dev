// @ts-check
/**
 * What the main thread must hear when one of nxy's agents has finished, from recorded state only
 * (`.nxy/local/verify/<plan>/`, written by the SubagentStop hook; the agent's own report is never read).
 *
 * Shared by the sync PostToolUse:Agent path (the agent just returned) and by SubagentStop (a background
 * agent ended: the lines become a note for the next hook on the main thread).
 */
import { statSync } from 'node:fs';
import { taskKey } from './tasks.mjs';
import { extractPlan, parseBatches, planHash } from '../../core/plan.mjs';
import {
  afterBatch, afterReviewRecorded, afterReviewer, afterSuiteFix, afterTester, batchOfPrompt, reviewFixOfPrompt, reviewInstruction, suiteFixOfPrompt,
} from '../../core/verify.mjs';
import { lookupHandoff } from './handoff.mjs';
import {
  readReview, readReviewParts, readSuite, readSuiteFix, readSuiteRepos, suiteReposWithoutTester, readVerify, reviewLaunchRepos, reviewLaunchTs, reviewPartRecorded,
} from './verify-state.mjs';
import { repoOfPrompt, testerPrompt } from '../../core/batch-status.mjs';

/** The review instruction (or why there is none) once the suite is green. */
async function reviewNext(cwd, hash) {
  const { reviewFiles } = await import('./baseline.mjs');
  const { reviewNeeded } = await import('../../core/review.mjs');
  const files = reviewFiles(cwd, taskKey(cwd), hash).files;
  const need = reviewNeeded(files.map((f) => f.path));
  let repos;
  if (need.needed) {
    // One reviewer per repo that has changed files, when there are two or more.
    try {
      const { touchedRepos, repoOf, repoNames } = await import('./repos.mjs');
      const roots = touchedRepos(cwd, taskKey(cwd));
      const key = (r) => String(r).replace(/\\/g, '/').replace(/\/+$/, '').toLowerCase();
      const withFiles = roots.filter((r) => files.some((f) => (f.abs ? key(repoOf(cwd, f.abs, roots)?.root ?? '') === key(r) : key(r) === key(roots[0]))));
      if (withFiles.length >= 2) {
        const names = repoNames(withFiles);
        repos = withFiles.map((r) => ({ label: names.get(r) ?? r, root: r }));
      }
    } catch { /* single reviewer */ }
  }
  return reviewInstruction({ hash, project: cwd, needed: need.needed, reason: need.reason, repos });
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
    if (reviewNeeded(reviewFiles(cwd, taskKey(cwd), hash).files.map((f) => f.path)).needed) return true;
  } catch { /* fall through to the packet */ }
  try {
    const { packetPath } = await import('./baseline.mjs');
    return statSync(packetPath(cwd, taskKey(cwd))).mtimeMs >= reviewLaunchTs(cwd, hash) - 2000;
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
    const { repo } = repoOfPrompt(prompt);
    const same = (a, b) => String(a).replace(/\\/g, '/').replace(/\/+$/, '').toLowerCase() === String(b).replace(/\\/g, '/').replace(/\/+$/, '').toLowerCase();
    const nameOf = (r) => String(r).replace(/\\/g, '/').split('/').filter(Boolean).pop() || String(r);
    const recs = repo ? readSuiteRepos(cwd, hash) : [];
    const mine = recs.find((r) => same(r.repo, repo));
    if (repo && mine && suite?.status !== 'done') {
      // One tester per repo (1.3.0): this one is in, the plan's suite is not complete yet.
      const running = recs.filter((r) => r !== mine && r.status === 'running');
      if (mine.status === 'recheck') {
        const first = recs.filter((r) => r.status === 'recheck').sort((a, b) => String(a.repo).localeCompare(String(b.repo)))[0];
        lines.push(`nxy: suite of ${nameOf(repo)} failed while ${nameOf(mine.with || 'another repo')} ran at the same time; it is rerun alone once the others finish.`);
        if (!running.length && first === mine) {
          lines.push(`Next: dispatch one nxy:tester: \`${testerPrompt(hash, repo, { alone: true })}\``);
        } else if (!running.length) {
          lines.push(`${nameOf(first.repo)} is rerun first (one at a time).`);
        }
      } else if (mine.status === 'done') {
        const waiting = [...new Set([...recs.flatMap((r) => r.expected || [])])].filter((r) => !recs.some((x) => x.status === 'done' && same(x.repo, r))).map(nameOf);
        lines.push(`nxy: suite of ${nameOf(repo)} ${(mine.red || []).length ? 'failed' : 'passed'}; waiting for ${waiting.join(', ') || 'the others'}.`);
        const next = recs.filter((r) => r.status === 'recheck').sort((a, b) => String(a.repo).localeCompare(String(b.repo)))[0];
        const noTester = suiteReposWithoutTester(cwd, hash).map(nameOf);
        if (noTester.length) lines.push(`nxy: no tester ran for ${noTester.join(', ')}: dispatch one for each, or a plain "Full suite for nxy plan ${hash}" dispatch resets the suite.`);
        if (next && !running.length) lines.push(`Next: dispatch one nxy:tester: \`${testerPrompt(hash, next.repo, { alone: true })}\``);
      }
      return { lines, pause };
    }
    if (suite?.status !== 'done') {
      // Still running, or never recorded: nothing proves the suite is green.
      lines.push(`nxy: the full suite of plan ${hash} has no recorded result (${suite ? 'it is still running' : 'the tester left none'}): do not review yet; dispatch nxy:tester again once if it is not running.`);
      return { lines, pause };
    }
    const red = suite.red || [];
    lines.push(afterTester({ hash, red, reviewNext: red.length ? undefined : await reviewNext(cwd, hash) }));
    for (const [r, x] of Object.entries(suite.rerun || {})) {
      lines.push(x.passedAlone
        ? `${nameOf(r)}: failed in parallel, rerun alone — passed (the first failure was likely contention: ${x.firstRed.map((f) => `\`${f.command}\``).join(', ') || 'unrecorded'}).`
        : `${nameOf(r)}: failed in parallel, rerun alone — failed again alone: red.`);
    }
    pause = true;
  } else if (role === 'reviewer') {
    const review = readReview(cwd, hash);
    const { repo: reviewRepo } = repoOfPrompt(prompt);
    if (review) lines.push(afterReviewRecorded({ hash, id: review.id }));
    else if (reviewRepo && reviewPartRecorded(cwd, hash, reviewRepo)) {
      // One reviewer per repo: this part is in, checkpoint 2 comes with the last one.
      const nameOf = (r) => String(r).replace(/\\/g, '/').split('/').filter(Boolean).pop() || String(r);
      const same = (a, b) => String(a).replace(/\\/g, '/').replace(/\/+$/, '').toLowerCase() === String(b).replace(/\\/g, '/').replace(/\/+$/, '').toLowerCase();
      const parts = readReviewParts(cwd, hash);
      const others = reviewLaunchRepos(cwd, hash).filter((r) => !same(r, reviewRepo) && !parts.some((p) => same(p.repo, r))).map(nameOf);
      lines.push(`nxy: review of ${nameOf(reviewRepo)} recorded; waiting for ${others.join(', ') || 'the other reviewers'}. Checkpoint 2 comes with the last one.`);
    } else if (await reviewExpected(cwd, hash)) lines.push(afterReviewer({ hash, recorded: false }));
    else lines.push(`nxy: the reviewer returned and saved no review packet for plan ${hash}: there was nothing to review.`);
    pause = true;
  } else if (suiteFixOfPrompt(prompt)) {
    const rec = readSuiteFix(cwd, hash);
    if (rec?.status === 'done') {
      const red = rec.red || [];
      const first = /** @type {any} */ (red[0]);
      // No suite command to re-run: not verified, never a pass, and the review does not open.
      const none = !red.length && !(rec.ran || []).length;
      const verdict = none ? { status: 'none' } : red.length ? { status: first?.status === 'not-run' ? 'not-run' : 'fail', error: first?.error, detail: first?.detail } : { status: 'pass' };
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
      const state = readVerify(cwd, taskKey(cwd), hash);
      const recorded = state.batches[String(n)];
      if (recorded && recorded.status !== 'running') {
        // Two or more repos touched: one tester per repo (a single repo keeps the 1.2.0 wording).
        let repos;
        try {
          const { repoNames, touchedRepos } = await import('./repos.mjs');
          const roots = touchedRepos(cwd, taskKey(cwd));
          if (roots.length >= 2) {
            const names = repoNames(roots);
            repos = roots.map((root) => ({ label: names.get(root) ?? root, root }));
          }
        } catch { /* single-repo wording */ }
        lines.push(afterBatch({
          repos,
          hash, n, verdict: recorded, recorded: state.batches,
          command: batch.accept.kind === 'command' ? batch.accept.command : undefined,
          batches: batches.map((b) => b.n),
          dependents: batches.filter((b) => b.depends.includes(n)).map((b) => b.n),
          fix: reviewFixOfPrompt(prompt),
        }));
        pause = true;
      }
    }
  }
  return { lines, pause };
}
