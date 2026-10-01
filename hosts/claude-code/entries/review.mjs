#!/usr/bin/env node
// @ts-check
/**
 * `/nxy:review` — the review of a finished plan (core/review.mjs).
 *
 * Usage:
 *   review.mjs packet [--full]   what the reviewer reads: saved to a file, an index printed (--full prints it all)
 *   review.mjs record <stdin>    the reviewer's findings (JSON) → classified, saved, checkpoint 2
 *   review.mjs show [<id>]       the last review (or that one) again
 *   review.mjs docs              what the documenter reads: the docs that name the change, and the diff
 *   review.mjs escape "<rule>" --as convention|lens [--lens <l>]
 *                                something a review missed: recorded, and kept as a convention or a repo lens
 *   review.mjs status            findings per lens, how many the user chose to fix, escapes
 *
 * The diff is the plan's: each file against the copy kept before the plan's first edit of it
 * (hosts/claude-code/baseline.mjs); files changed without a copy come from a read-only git status
 * (hosts/claude-code/gitstate.mjs), marked `(git)`.
 */
import { existsSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { parseArgs } from '../../../core/format.mjs';
import { PLUGIN_ROOT, ensureDir, gitBranch, gitRoot, nxyRuntimeDir, toNativePath } from '../../../core/paths.mjs';
import { appendJsonl, readJsonl } from '../../../core/jsonl.mjs';
import { extractPlan, parseBatches, planHash } from '../../../core/plan.mjs';
import {
  batchForFile, checkpoint2, classifyFindings, mergeLenses, packetOutline, parseFindings, parseLens, reviewId, reviewNeeded,
  reviewPacket, selectLenses,
} from '../../../core/review.mjs';
import { packetPath, readBaseline, reviewFiles } from '../baseline.mjs';
import { readMarker } from '../plan-approval.mjs';
import { lookupHandoff } from '../handoff.mjs';
import { findDocs } from '../docs.mjs';
import { docsPacket, docsQuestion } from '../../../core/docs.mjs';
import { loadConfig } from '../../../core/config.mjs';

const { opts, positional } = parseArgs(process.argv.slice(2));
const cwd = toNativePath(typeof opts.cwd === 'string' ? opts.cwd : process.env.CLAUDE_PROJECT_DIR || process.cwd());
const action = (positional[0] || 'status').toLowerCase();
const branch = gitBranch(cwd);
const SELF = join(PLUGIN_ROOT, 'hosts', 'claude-code', 'entries', 'review.mjs').replace(/\\/g, '/');
const statePath = join(nxyRuntimeDir(cwd), 'review.json');
const ledgerPath = join(nxyRuntimeDir(cwd), 'review.jsonl');

/**
 * Why a packet is empty: where nxy looked, how many baseline copies it found, whether the plan
 * marker is there. Returned as lines so a later source of changes can add its own.
 * @param {string|null} planHash
 * @param {{note: string|null, git: {atApproval: number, now: number|null}|null}} [rv] the review's git result
 */
function emptyWhy(planHash, rv) {
  const copies = Object.keys(readBaseline(cwd, branch)).length;
  const marker = readMarker(cwd, branch);
  return [
    `looked in ${join(nxyRuntimeDir(cwd), 'baseline').replace(/\\/g, '/')} (project ${(gitRoot(cwd) ?? cwd).replace(/\\/g, '/')}, branch ${branch || '(none)'})`,
    `baseline copies: ${copies} · plan marker: ${marker ? `present (${marker.hash})` : 'absent'}${planHash && marker && marker.hash !== planHash ? ` (the handoff's plan is ${planHash})` : ''}`,
    ...(rv ? [rv.note ? `git: ${rv.note}` : rv.git ? `git: ${rv.git.atApproval} file(s) dirty at approval, ${rv.git.now ?? '?'} dirty now` : 'git: not checked (no plan marker)'] : []),
  ];
}

/** The `## Not reviewed` section and git note of a packet, as lines (empty when there is nothing to say). */
function notReviewedLines(rv) {
  const out = [];
  if (rv.notReviewed.length) {
    out.push('', '## Not reviewed', ...rv.notReviewed.map((n) => `- ${n.path}: ${n.reason}`));
  }
  if (rv.note) out.push('', `Git: ${rv.note}; only the files nxy copied before the plan touched them were reviewed.`);
  return out;
}

/** @param {string} dir */
function lensesIn(dir) {
  if (!existsSync(dir)) return [];
  return readdirSync(dir).filter((n) => n.endsWith('.md')).sort()
    .map((n) => parseLens(readFileSync(join(dir, n), 'utf8'), n.replace(/\.md$/, '')))
    .filter((l) => l !== null);
}

async function currentPlan() {
  const known = await lookupHandoff(cwd);
  const plan = known.handoff ? extractPlan(known.handoff.body) : null;
  return { plan, hash: plan ? planHash(plan) : null, project: known.project };
}

function readState() {
  try {
    return JSON.parse(readFileSync(statePath, 'utf8'));
  } catch {
    return null;
  }
}

/** @param {any} state */
function checkpointText(state, batches) {
  return checkpoint2({
    id: state.id, planHash: state.planHash, findings: state.findings, summary: state.summary,
    batchOf: (f) => batchForFile(batches, f.file),
    extra: state.docs?.docs?.length ? docsQuestion(state.id, state.docs.docs.map((d) => d.path)) : null,
  });
}

/** A repo lens that collects what past reviews missed: always applied. */
function appendRepoLens(line) {
  const dir = join(cwd, '.nxy', 'lenses');
  const file = join(dir, 'repo.md');
  ensureDir(dir);
  const head = '---\nname: repo\ntitle: This repo — what past reviews missed\nalways: true\n---\n';
  const body = existsSync(file) ? readFileSync(file, 'utf8') : head;
  writeFileSync(file, `${body.replace(/\s*$/, '\n')}- ${line}\n`, 'utf8');
  return file;
}

switch (action) {
  case 'packet': {
    const { plan, hash, project } = await currentPlan();
    if (!plan || !hash) {
      console.log(`no plan in the handoff of \`${branch || '(no branch)'}\`: nothing to review`);
      break;
    }
    const rv = reviewFiles(cwd, branch, hash);
    const files = rv.files;
    const need = reviewNeeded(files.map((f) => f.path));
    if (!need.needed) {
      console.log(`no review needed for plan ${hash}: ${need.reason}.`);
      if (!files.length) for (const l of emptyWhy(hash, rv)) console.log(l);
      for (const l of notReviewedLines(rv)) console.log(l);
      break;
    }
    const lenses = selectLenses(
      mergeLenses(lensesIn(join(PLUGIN_ROOT, 'lenses')), lensesIn(join(cwd, '.nxy', 'lenses'))),
      files.map((f) => ({ path: f.path, content: f.content })),
    );
    const { listMemories, openStore } = await import('../../../core/memory/store.mjs');
    const conventions = listMemories(openStore(), { project, allAreas: true, type: 'convention', limit: 200 })
      .map((m) => ({ title: m.title, body: m.body }));
    const packet = reviewPacket({
      planHash: hash, plan, conventions, lenses, files,
      recordCmd: `node --disable-warning=ExperimentalWarning "${SELF}" record --cwd "${cwd.replace(/\\/g, '/')}"`,
    });
    const notes = notReviewedLines(rv);
    if (opts.full) {
      console.log(packet);
      for (const l of notes) console.log(l);
      break;
    }
    // The packet can be thousands of lines: it is saved, and only an index and the way to answer are printed.
    const saved = packetPath(cwd, branch);
    const text = [packet, ...notes].join('\n');
    ensureDir(dirname(saved));
    writeFileSync(saved, text, 'utf8');
    const shown = saved.replace(/\\/g, '/');
    const answerAt = packet.lastIndexOf('## Your answer'); // the answer is the packet's last section; a diff may quote the heading
    console.log([
      `# nxy review packet · plan ${hash}`,
      '',
      `Saved at ${shown} (${text.split('\n').length} lines, ${Math.ceil(Buffer.byteLength(text) / 1024)} KB).`,
      '',
      packetOutline(text, { path: shown }),
      ...notes,
      '',
      answerAt >= 0 ? packet.slice(answerAt) : '',
    ].join('\n'));
    break;
  }

  case 'record': {
    const { plan, hash } = await currentPlan();
    if (!plan || !hash) {
      console.log('no plan on this branch: nothing to record');
      process.exitCode = 1;
      break;
    }
    const text = typeof opts.body === 'string' ? opts.body : process.stdin.isTTY ? '' : readFileSync(0, 'utf8');
    const { findings, errors } = parseFindings(text);
    if (errors.length && !findings.length) {
      console.log(`findings not recorded: ${errors.join('; ')}`);
      process.exitCode = 1;
      break;
    }
    const files = reviewFiles(cwd, branch, hash).files;
    const classified = classifyFindings(findings, new Map(files.map((f) => /** @type {[string, [number, number][]]} */ ([f.path, f.ranges]))));
    const docs = loadConfig(cwd).roles?.documenter ? findDocs(cwd, files.map((f) => f.path), loadConfig(cwd).docs || {}) : null;
    const state = {
      docs,
      id: reviewId(hash),
      planHash: hash,
      branch,
      ts: Date.now(),
      summary: { files: files.length, added: files.reduce((s, f) => s + f.added, 0), removed: files.reduce((s, f) => s + f.removed, 0) },
      findings: classified,
      chosen: [],
    };
    ensureDir(nxyRuntimeDir(cwd));
    writeFileSync(statePath, JSON.stringify(state), 'utf8');
    appendJsonl(ledgerPath, {
      ts: state.ts, kind: 'review', id: state.id, planHash: hash,
      findings: classified.map((f) => ({ id: f.id, lens: f.lens, severity: f.severity, cause: f.cause })),
    });
    for (const e of errors) console.log(`skipped: ${e}`);
    console.log(checkpointText(state, parseBatches(plan)));
    break;
  }

  case 'show': {
    const state = readState();
    if (!state || (positional[1] && positional[1] !== state.id)) {
      console.log(positional[1] ? `no review ${positional[1]} (only the last one is kept: ${state?.id || 'none'})` : 'no review yet');
      break;
    }
    const { plan } = await currentPlan();
    console.log(checkpointText(state, plan ? parseBatches(plan) : []));
    break;
  }

  case 'docs': {
    const state = readState();
    if (!state?.docs?.docs?.length) {
      console.log('no docs mention what this plan changed: nothing to update');
      break;
    }
    const { plan } = await currentPlan();
    const files = reviewFiles(cwd, branch).files;
    console.log(docsPacket({
      id: state.id, plan: plan || '(the plan was already closed)', files,
      docs: state.docs.docs, wikiDir: state.docs.wikiDir,
    }));
    break;
  }

  case 'escape': {
    const text = positional.slice(1).join(' ').trim();
    const as = opts.as === 'convention' || opts.as === 'lens' ? opts.as : null;
    if (!text || !as) {
      console.log('Usage: review.mjs escape "<what the review missed, as a rule>" --as convention|lens [--lens <the lens that should have caught it>]');
      console.log('  convention = a rule of this repo (the planner and the implementer follow it)');
      console.log('  lens       = something to look for (every future review checks it: .nxy/lenses/repo.md)');
      break;
    }
    const lens = typeof opts.lens === 'string' ? opts.lens : 'base';
    const last = readState();
    appendJsonl(ledgerPath, { ts: Date.now(), kind: 'escape', lens, as, text, reviewId: last?.branch === branch ? last.id : null });
    if (as === 'convention') {
      const { projectKey } = await import('../../../core/memory/scope.mjs');
      const { openStore, saveMemory } = await import('../../../core/memory/store.mjs');
      const res = saveMemory(openStore(), { scope: 'project', project: projectKey(cwd).key, type: 'convention', title: text, body: `${text}\n\nMissed by a review (lens: ${lens}); saved as a convention.` });
      console.log(`escape recorded (lens: ${lens}) · saved as convention ${res.id}: the planner and every implementer get it from now on`);
    } else {
      const file = appendRepoLens(text);
      console.log(`escape recorded (lens: ${lens}) · added to ${file.replace(/\\/g, '/')}: every review checks it from now on (commit it to share it)`);
    }
    break;
  }

  case 'status': {
    const since = Date.now() - 30 * 86_400_000;
    const rows = readJsonl(ledgerPath).filter((r) => r && r.ts >= since);
    const reviews = rows.filter((r) => r.kind === 'review');
    const escapes = rows.filter((r) => r.kind === 'escape');
    if (!reviews.length && !escapes.length) {
      console.log('no reviews in the last 30 days');
      break;
    }
    const chosen = new Set(rows.filter((r) => r.kind === 'chosen').map((r) => `${r.id}:${r.finding}`));
    /** @type {Map<string, {change: number, pre: number, fixed: number, escaped: number}>} */
    const byLens = new Map();
    const get = (lens) => byLens.get(lens) || { change: 0, pre: 0, fixed: 0, escaped: 0 };
    for (const r of reviews) {
      for (const f of r.findings || []) {
        const c = get(f.lens);
        if (f.cause === 'change') c.change++;
        else c.pre++;
        if (chosen.has(`${r.id}:${f.id}`)) c.fixed++;
        byLens.set(f.lens, c);
      }
    }
    for (const e of escapes) {
      const c = get(e.lens);
      c.escaped++;
      byLens.set(e.lens, c);
    }
    console.log(`reviews (30 days): ${reviews.length} · escapes: ${escapes.length} (what reviews missed and you or the PR found)`);
    console.log('lens            from the change   chosen to fix   preexisting   escaped');
    for (const [lens, c] of [...byLens].sort((a, b) => b[1].change - a[1].change)) {
      console.log(`${lens.padEnd(16)}${String(c.change).padStart(15)}${String(c.fixed).padStart(16)}${String(c.pre).padStart(14)}${String(c.escaped).padStart(10)}`);
    }
    break;
  }

  default:
    console.log('Usage: review.mjs [packet | record <stdin JSON> | show [<id>] | docs | escape "<rule>" --as convention|lens [--lens <l>] | status]');
}
