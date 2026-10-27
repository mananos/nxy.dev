// @ts-check
/**
 * Batch status helpers shared by core/verify.mjs and core/orchestrator.mjs.
 *
 * Pure and import-free on purpose: the Mod's module cannot load `node:*` (verify.mjs pulls in
 * memory/handoff.mjs: node:crypto, sqlite), so everything the module needs about statuses and the
 * wording the main thread sees lives here. verify.mjs re-exports all of it.
 */

export const RETRY = 'Retry';
export const CONTINUE = 'Continue anyway';
export const STOP = 'Stop';

/**
 * @typedef {'pass' | 'fail' | 'not-run' | 'manual' | 'none' | 'running'} Status
 * @typedef {{status: Status, error?: string, detail?: string}} Verdict
 */

/** Red: the approved criterion did not pass. `manual` and `none` are not red — they were never runnable. */
export const isRed = (status) => status === 'fail' || status === 'not-run';

/** A batch that others may build on: it passed, or there was nothing to run. */
export const isDone = (status) => status === 'pass' || status === 'manual' || status === 'none';

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
 * The line the main thread sees when an implementer returns, and what to do next.
 * A fix from the review's checkpoint 2 (`fix`: the finding and the review) closes with its own line:
 * the other picked findings are dispatched next, one at a time (never in parallel), and it never leads to another suite and review.
 * `dependents`: the batches that build on this one (default: the next one).
 * @param {{hash: string, n: number, command?: string, verdict: Verdict, batches: number[], recorded: Record<string, {status: Status}>,
 *   fix?: {finding?: string, review?: string} | boolean | null, dependents?: number[], repos?: {label: string, root: string}[]}} o
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
    const notRun = verdict.status === 'not-run';
    return [
      `nxy verify: batch ${n} ✘${cmd} ${notRun ? `was not run after the last edit${verdict.detail ? ` (${verdict.detail})` : ''}` : `failed after the last edit${verdict.error ? ` (${verdict.error})` : ''}`}.`,
      notRun
        ? `Next: dispatch \`Batch ${n} — run the acceptance command only\` again, to run the Accept exactly as written and alone (no extra cd, pipe, redirection or absolute path; a cd that is part of the Accept stays): the code may already be right. Or ask the user with AskUserQuestion, with exactly this input:`
        : `Re-dispatch the implementer for batch ${n} with the failure (always allowed), or ask the user with AskUserQuestion, with exactly this input:`,
      `  ${JSON.stringify(input)}`,
      waiting.length
        ? `${waiting.length === 1 ? `Batch ${waiting[0]}` : `Batches ${waiting.join(', ')}`} will not be dispatched until batch ${n} passes or the user picks "${CONTINUE}".`
        : '',
    ].filter(Boolean).join('\n');
  }
  const mark = verdict.status === 'pass' ? `✔${cmd} passed` : verdict.status === 'manual' ? '– manual: the user checks it' : '– no acceptance command';
  const lines = [`nxy verify: batch ${n} ${mark}.`];
  if (o.fix) {
    const f = typeof o.fix === 'object' ? o.fix : {};
    const what = f.finding && f.review ? `${f.finding} of review ${f.review}` : f.finding ? f.finding : 'the picked finding';
    const rid = f.review || '<id>';
    lines.push(`Review fix ${what} done (batch ${n} ✔). If the user picked more findings, dispatch the remaining ones one at a time, never in parallel (implementers on the same tree break each other's checks), each with its own checkpoint-2 line \`Batch ${n} — fix R<k> of review ${rid}: ...\`; findings of the same batch may go together in one implementer; once every picked finding is fixed there is no new suite or review: tell the user what was fixed and what was left.`);
    return lines.join('\n');
  }
  const done = o.batches.every((k) => {
    const s = (k === n ? verdict : o.recorded[String(k)])?.status;
    return s === 'pass' || s === 'manual' || s === 'none';
  });
  if (done && o.batches.length) {
    const all = `All ${o.batches.length} batch${o.batches.length === 1 ? '' : 'es'} of plan ${hash} verified.`;
    if (o.repos && o.repos.length >= 2) {
      lines.push(`${all} Next: dispatch one nxy:tester per repo, all in one message (they run in parallel):`);
      for (const r of o.repos) lines.push(`- ${r.label}: "${testerPrompt(hash, r.root)}"`);
    } else {
      lines.push(`${all} Next: dispatch the nxy:tester subagent once with "Full suite for nxy plan ${hash}" — it runs the suite of every repo the plan touched.`);
    }
  }
  return lines.join('\n');
}

const fwdPath = (p) => String(p).replace(/\\/g, '/');

/**
 * The tester's request. With a repo (1.3.0) it runs only that repo's suite; `alone` marks the rerun of a
 * suite that failed while another repo's tester ran (nothing changes for the tester itself).
 * @param {string} hash @param {string} [repo] @param {{alone?: boolean}} [opts]
 */
export const testerPrompt = (hash, repo, opts = {}) => (repo
  ? `Full suite for nxy plan ${hash} (repo: ${fwdPath(repo)}${opts.alone ? ', alone' : ''})`
  : `Full suite for nxy plan ${hash}`);

/** @param {string} hash @param {string} project @param {string} [repo] */
export const reviewerPrompt = (hash, project, repo) => `Review nxy plan ${hash} (project: ${fwdPath(project)})${repo ? ` (repo: ${fwdPath(repo)})` : ''}`;

/** The repo a tester or reviewer request names, and whether it is the rerun alone. @param {unknown} prompt */
export function repoOfPrompt(prompt) {
  const s = typeof prompt === 'string' ? prompt : '';
  // The clause ends the prompt, so anchor to the end: a ")" inside the path stays part of the path.
  const m = /\(repo: (.+?)(, alone)?\)\s*$/.exec(s) || /\(repo: ([^)]*?)(, alone)?\)/.exec(s);
  return { repo: m ? m[1].trim() : null, alone: !!(m && m[2]) };
}

/**
 * After the full suite: the instruction to review, or why there is nothing to review.
 * With 2+ repos (1.3.0) it is one reviewer per repo, all in one message; one repo or none: the single reviewer.
 * @param {{hash: string, project: string, needed: boolean, reason?: string, repos?: {label: string, root: string}[]}} o
 */
export function reviewInstruction({ hash, project, needed, reason, repos }) {
  if (needed && repos && repos.length >= 2) {
    return [
      `nxy: full suite done. Next: dispatch one nxy:reviewer per repo, all in one message (they run in parallel); checkpoint 2 comes with the last one:`,
      ...repos.map((r) => `- ${r.label}: "${reviewerPrompt(hash, project, r.root)}"`),
    ].join('\n');
  }
  return needed
    ? `nxy: full suite done. Next: dispatch the nxy:reviewer subagent once with "Review nxy plan ${hash} (project: ${project.replace(/\\/g, '/')})" — it reviews the plan's diff and returns the findings for checkpoint 2.`
    : `nxy: full suite done. No review needed for plan ${hash}: ${reason}.`;
}
