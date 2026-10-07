#!/usr/bin/env node
// @ts-check
/**
 * The orchestrator's read-only view of a plan (core/orchestrator.mjs), for the Mod, which cannot open
 * SQLite or scan a transcript itself.
 *
 * Usage:
 *   orch.mjs snapshot --cwd <dir> [--session <id> | --transcript <path>] [--projects-dir <dir>]
 *                                 one JSON line: the plan, its batches, verdicts, suite, review, config
 *   orch.mjs release --cwd <dir>  removes the orchestrator's marker, tickets and state (a stuck marker)
 *
 * Fail-open: any error prints {"ok":false}.
 */
import { join } from 'node:path';
import { parseArgs } from '../../../core/format.mjs';
import { gitBranch, nxyRuntimeDir, toNativePath } from '../../../core/paths.mjs';
import { loadConfig } from '../../../core/config.mjs';
import { extractPlan, parseBatches, parseQuestions, planHash } from '../../../core/plan.mjs';
import { claudeProjectsDir, projectSlug } from '../paths.mjs';
import { isApproved } from '../plan-approval.mjs';
import { lookupHandoff } from '../handoff.mjs';
import { readReview, readSuite, readVerify } from '../verify-state.mjs';
import { clearOrch } from '../orch-state.mjs';

async function snapshot(cwd, opts) {
  const cfg = loadConfig(cwd);
  const branch = gitBranch(cwd);
  const known = await lookupHandoff(cwd);
  const plan = known.handoff ? extractPlan(known.handoff.body) : null;
  const base = {
    ok: true,
    runtimeDir: nxyRuntimeDir(cwd),
    projectDir: cwd,
    branch: branch || null,
    config: {
      pauseAfterBatch: cfg.flow?.pauseAfterBatch === true,
      orchestrator: cfg.flow?.orchestrator === 'off' ? 'off' : 'auto',
      ui: { panel: cfg.ui?.panel === 'off' ? 'off' : 'auto' },
      gate: { enabled: cfg.gate?.enabled === true, contextTokens: cfg.gate?.contextTokens ?? 0 },
    },
    handoff: known.handoff ? { updated: known.handoff.updated } : null,
  };
  if (!plan) return { ...base, hash: null, approved: false, fresh: false, questions: 0, batches: [], verdicts: {}, suite: null, review: null, reviewNeeded: { needed: false, reason: '' } };

  const hash = planHash(plan);
  const questions = parseQuestions(plan).length;
  let transcript = typeof opts.transcript === 'string' ? toNativePath(opts.transcript) : null;
  if (!transcript && typeof opts.session === 'string' && /^[\w-]+$/.test(opts.session)) {
    const root = claudeProjectsDir(typeof opts['projects-dir'] === 'string' ? opts['projects-dir'] : cfg.metrics?.projectsDir || undefined);
    transcript = join(root, projectSlug(cwd), `${opts.session}.jsonl`);
  }
  const approved = questions === 0 && !!transcript && isApproved(cwd, transcript, hash);
  const verdicts = readVerify(cwd, branch, hash).batches;
  const suite = readSuite(cwd, hash);
  const review = readReview(cwd, hash);
  const fresh = approved && Object.keys(verdicts).length === 0 && !suite && !review;

  let need = { needed: false, reason: '' };
  try {
    const { reviewFiles } = await import('../baseline.mjs');
    const { reviewNeeded } = await import('../../../core/review.mjs');
    need = reviewNeeded(reviewFiles(cwd, branch, hash).files.map((f) => f.path));
  } catch {
    /* unknown: not needed */
  }
  return {
    ...base,
    hash,
    approved,
    fresh,
    questions,
    batches: parseBatches(plan).map((b) => ({ n: b.n, depends: b.depends, text: b.text, accept: b.accept.kind })),
    verdicts,
    suite: suite ? { status: suite.status, red: suite.red || [] } : null,
    review,
    reviewNeeded: need,
  };
}

try {
  const { opts, positional } = parseArgs(process.argv.slice(2));
  const cwd = toNativePath(typeof opts.cwd === 'string' ? opts.cwd : process.env.CLAUDE_PROJECT_DIR || process.cwd());
  const action = (positional[0] || 'snapshot').toLowerCase();
  if (action === 'release') {
    clearOrch(cwd);
    console.log(JSON.stringify({ ok: true }));
  } else {
    console.log(JSON.stringify(await snapshot(cwd, opts)));
  }
} catch {
  console.log(JSON.stringify({ ok: false }));
}
