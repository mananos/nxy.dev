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
 *   orch.mjs release --snapshot --cwd <dir> [--session <id>]
 *                                 the same release, then the snapshot's JSON line instead of {"ok":true}
 *   orch.mjs summary --hash <h> [--session <id>] [--fallback-since <ms>] --cwd <dir>
 *                                 one JSON line: the consumption of a closed feature ({feature, session,
 *                                 approvedAt, sinceKind, cacheTtl}): the session from the plan's approval on
 *                                 (else --fallback-since, else the whole session) and the session entire
 *
 * Fail-open: any error prints {"ok":false}.
 */
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { parseAgentMeta } from '../../../core/agentview.mjs';
import { parseArgs } from '../../../core/format.mjs';
import { gitBranch, nxyRuntimeDir, toNativePath } from '../../../core/paths.mjs';
import { loadConfig } from '../../../core/config.mjs';
import { batchFiles, extractPlan, parseBatches, parseGoal, parseQuestions, planHash } from '../../../core/plan.mjs';
import { claudeProjectsDir, projectSlug } from '../paths.mjs';
import { loadPricing } from '../../../core/pricing.mjs';
import { findApprovalAt, isApproved } from '../plan-approval.mjs';
import { lastCacheTtl, parseSession, resolveSession, summarizeBreaks } from '../transcripts.mjs';
import { lookupHandoff } from '../handoff.mjs';
import { readReview, readReviewDetail, readSuite, readVerify } from '../verify-state.mjs';
import { clearOrch } from '../orch-state.mjs';

/** A batch title without its "Batch N — " prefix (the raw title when it does not match). */
function titleOf(raw) {
  const t = String(raw ?? '');
  return t.replace(/^#*\s*Batch\s+\d+\s*[—–-]\s*/i, '') || t;
}

/** The model and effort each nxy role declares in agents/<role>.md ({} when unreadable). */
function nxyRoles() {
  try {
    const dir = fileURLToPath(new URL('../../../agents/', import.meta.url));
    const roles = {};
    for (const f of readdirSync(dir).filter((n) => n.endsWith('.md')).sort()) {
      roles[f.slice(0, -3)] = parseAgentMeta(readFileSync(join(dir, f), 'utf8'));
    }
    return roles;
  } catch {
    return {};
  }
}

async function snapshot(cwd, opts) {
  const cfg = loadConfig(cwd);
  const branch = gitBranch(cwd);
  const known = await lookupHandoff(cwd);
  const plan = known.handoff ? extractPlan(known.handoff.body) : null;
  let transcript = typeof opts.transcript === 'string' ? toNativePath(opts.transcript) : null;
  if (!transcript && typeof opts.session === 'string' && /^[\w-]+$/.test(opts.session)) {
    const root = claudeProjectsDir(typeof opts['projects-dir'] === 'string' ? opts['projects-dir'] : cfg.metrics?.projectsDir || undefined);
    transcript = join(root, projectSlug(cwd), `${opts.session}.jsonl`);
  }
  const base = {
    ok: true,
    // The cache TTL the session writes with (null without a session or a cache write) and the price
    // table, so the Mod can price a cold cache without importing anything node-side.
    cacheTtl: lastCacheTtl(transcript),
    prices: loadPricing().models,
    roles: nxyRoles(),
    runtimeDir: nxyRuntimeDir(cwd),
    projectDir: cwd,
    branch: branch || null,
    config: {
      pauseAfterBatch: cfg.flow?.pauseAfterBatch === true,
      orchestrator: cfg.flow?.orchestrator === 'off' ? 'off' : 'auto',
      ui: { panel: cfg.ui?.panel === 'off' ? 'off' : 'auto' },
      gate: { enabled: cfg.gate?.enabled === true, contextTokens: cfg.gate?.contextTokens ?? 0 },
      filter: cfg.modules?.filter === true,
    },
    handoff: known.handoff ? { updated: known.handoff.updated } : null,
  };
  if (!plan) return { ...base, goal: '', reviewDetail: null, hash: null, approved: false, fresh: false, questions: 0, batches: [], verdicts: {}, suite: null, review: null, reviewNeeded: { needed: false, reason: '' } };

  const hash = planHash(plan);
  const questions = parseQuestions(plan).length;
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
    goal: parseGoal(plan),
    batches: parseBatches(plan).map((b) => ({ n: b.n, title: titleOf(b.title), depends: b.depends, text: b.text, files: batchFiles(b.text), accept: b.accept })),
    verdicts,
    suite: suite ? { status: suite.status, red: suite.red || [] } : null,
    review,
    reviewDetail: await readReviewDetail(cwd, hash),
    reviewNeeded: need,
  };
}

/** Only what the Mod's summary view reads from a parsed session. */
function trimmed(s) {
  return {
    usage: s.usage, main: s.main, subagents: s.subagents, byModel: s.byModel, byAgentType: s.byAgentType,
    cacheBreaks: { count: s.cacheBreaks.length, ...summarizeBreaks(s.cacheBreaks) },
    ttlWhatIf: s.ttlWhatIf, firstTs: s.firstTs, lastTs: s.lastTs, turns: s.turns,
  };
}

function summary(cwd, opts) {
  const cfg = loadConfig(cwd);
  const selector = typeof opts.session === 'string' && /^[\w-]+$/.test(opts.session) ? opts.session : 'last';
  const ref = resolveSession(selector, { cwd, projectsDir: typeof opts['projects-dir'] === 'string' ? opts['projects-dir'] : cfg.metrics?.projectsDir });
  if (!ref) return { ok: false };
  const hash = typeof opts.hash === 'string' ? opts.hash : '';
  const approvedAt = hash ? findApprovalAt(ref.path, hash) : null;
  const fallback = Number(opts['fallback-since']);
  const sinceKind = approvedAt ? 'approval' : Number.isFinite(fallback) && fallback > 0 ? 'first-batch' : 'session';
  const since = approvedAt || (sinceKind === 'first-batch' ? fallback : null);
  const parseOpts = { cacheBreakThreshold: cfg.metrics?.cacheBreakThreshold };
  const feature = parseSession(ref, { ...parseOpts, since });
  const session = parseSession(ref, parseOpts);
  return { ok: true, feature: trimmed(feature), session: trimmed(session), approvedAt, sinceKind, cacheTtl: lastCacheTtl(ref.path) };
}

try {
  const { opts, positional } = parseArgs(process.argv.slice(2));
  const cwd = toNativePath(typeof opts.cwd === 'string' ? opts.cwd : process.env.CLAUDE_PROJECT_DIR || process.cwd());
  const action = (positional[0] || 'snapshot').toLowerCase();
  if (action === 'release') {
    clearOrch(cwd);
    console.log(JSON.stringify(opts.snapshot ? await snapshot(cwd, opts) : { ok: true }));
  } else if (action === 'summary') {
    console.log(JSON.stringify(summary(cwd, opts)));
  } else {
    console.log(JSON.stringify(await snapshot(cwd, opts)));
  }
} catch {
  console.log(JSON.stringify({ ok: false }));
}
