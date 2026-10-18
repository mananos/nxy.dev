// @ts-check
/**
 * Claude Code side of batch verification (core/verify.mjs): what an implementer did, read from its
 * own transcript, and the verdicts recorded per plan.
 *
 * `.nxy/local/verify/<plan hash>/` holds the verdicts of a plan, one file per batch, and which
 * implementers were already sent back once (the anti-loop: a second stop always goes through). One
 * file per batch because batches run in parallel: two SubagentStop hooks finishing together must not
 * read-modify-write the same file and lose a verdict. Written only by the SubagentStop hook, from the
 * transcript — an index of what the runtime recorded, not a place the model can write its own result.
 * Cost: a few files under 1 KB per plan; a plan's folder is removed 30 days after its last verdict.
 */
import { createHash } from 'node:crypto';
import {
  closeSync, existsSync, openSync, readFileSync, readSync, readdirSync, renameSync, rmSync, statSync, unlinkSync, writeFileSync,
} from 'node:fs';
import { join } from 'node:path';
import { ensureDir, nxyRuntimeDir } from '../../core/paths.mjs';

const verifyRoot = (cwd) => join(nxyRuntimeDir(cwd), 'verify');
const planDir = (cwd, hash) => join(verifyRoot(cwd), String(hash).replace(/[^0-9a-z]/gi, ''));
const KEEP_MS = 30 * 86_400_000;

/** A subagent transcript bigger than this is read from its tail: the run that counts is the last one. */
const MAX_SCAN_BYTES = 16 * 1024 * 1024;

const EDIT_TOOLS = new Set(['Edit', 'Write', 'MultiEdit', 'NotebookEdit']);
const SHELL_TOOLS = new Set(['Bash', 'PowerShell']);

/**
 * @typedef {{branch: string|null, hash: string, batches: Record<string, {status: import('../../core/verify.mjs').Status, error?: string, detail?: string, ts: number, launched?: number}>, sentBack: string[]}} VerifyState
 */

/**
 * The verdicts of this plan on this branch; empty when the plan changed.
 * @returns {VerifyState}
 */
export function readVerify(cwd, branch, hash) {
  /** @type {VerifyState} */
  const state = { branch: branch || null, hash, batches: {}, sentBack: [] };
  let names = [];
  try {
    names = readdirSync(planDir(cwd, hash));
  } catch {
    return state;
  }
  for (const name of names) {
    try {
      const batch = /^batch-(\d+)\.json$/.exec(name);
      if (batch) {
        const b = JSON.parse(readFileSync(join(planDir(cwd, hash), name), 'utf8'));
        if (b && b.status && (b.branch || null) === (branch || null)) {
          const { branch: _b, ...verdict } = b;
          state.batches[batch[1]] = verdict;
        }
      } else if (name.startsWith('sent-')) {
        state.sentBack.push(readFileSync(join(planDir(cwd, hash), name), 'utf8'));
      }
    } catch {
      /* a file being replaced right now: the next read sees it */
    }
  }
  return state;
}

/**
 * Records one batch's verdict in its own file, replaced atomically (write aside, then rename), so a
 * parallel batch never overwrites it and a reader never sees half a file.
 * @param {string} cwd @param {string|null} branch @param {string} hash @param {number|string} n
 * @param {{status: import('../../core/verify.mjs').Status, error?: string, detail?: string, ts: number, launched?: number}} verdict
 */
export function recordBatch(cwd, branch, hash, n, verdict) {
  try {
    const dir = ensureDir(planDir(cwd, hash));
    const path = join(dir, `batch-${n}.json`);
    const tmp = `${path}.${process.pid}.tmp`;
    writeFileSync(tmp, JSON.stringify({ branch: branch || null, ...verdict }), 'utf8');
    try {
      renameSync(tmp, path);
    } catch {
      // Windows refuses the rename while another hook has the file open: write it in place instead.
      writeFileSync(path, readFileSync(tmp, 'utf8'), 'utf8');
      unlinkSync(tmp);
    }
    pruneOldPlans(cwd, hash);
  } catch {
    /* best-effort */
  }
}

/**
 * True the first time this implementer run is sent back for a batch; false after that. The marker
 * is created exclusively (`wx`), so two hooks cannot both claim the first time.
 * @param {string} cwd @param {string} hash @param {string} key
 */
export function claimSendBack(cwd, hash, key) {
  try {
    const dir = ensureDir(planDir(cwd, hash));
    writeFileSync(join(dir, `sent-${createHash('sha1').update(key).digest('hex').slice(0, 16)}`), key, { encoding: 'utf8', flag: 'wx' });
    return true;
  } catch {
    return false; // already sent back once, or the disk said no: either way, let it stop
  }
}

/** Atomic JSON write of a file in the plan's folder (write aside, rename), then the usual prune. */
function writePlanFile(cwd, hash, name, data) {
  try {
    const dir = ensureDir(planDir(cwd, hash));
    const path = join(dir, name);
    const tmp = `${path}.${process.pid}.tmp`;
    writeFileSync(tmp, JSON.stringify(data), 'utf8');
    try {
      renameSync(tmp, path);
    } catch {
      writeFileSync(path, readFileSync(tmp, 'utf8'), 'utf8');
      unlinkSync(tmp);
    }
    pruneOldPlans(cwd, hash);
  } catch {
    /* best-effort */
  }
}

function readPlanFile(cwd, hash, name) {
  try {
    return JSON.parse(readFileSync(join(planDir(cwd, hash), name), 'utf8'));
  } catch {
    return null;
  }
}

/**
 * @typedef {{status: 'running' | 'done', ts: number, red?: {command: string, error?: string, status?: string, detail?: string}[], ran?: string[], rerun?: Record<string, {passedAlone: boolean, firstRed: {command: string, error?: string, detail?: string}[]}>}} SuiteRecord
 */

/** The full suite's state for this plan (`suite.json`). @param {string} cwd @param {string} hash @param {SuiteRecord} rec */
export function recordSuite(cwd, hash, rec) {
  // A suite run for the whole plan replaces any per-repo state of an earlier round.
  for (const f of suiteRepoFiles(cwd, hash)) {
    try { unlinkSync(join(planDir(cwd, hash), f)); } catch { /* best-effort */ }
  }
  writePlanFile(cwd, hash, 'suite.json', rec);
}

// ---- one tester per repo (1.3.0): `suite-<slug>.json`, a few hundred bytes each ----

/**
 * @typedef {{repo: string, status: 'running' | 'done' | 'recheck', ts: number, started?: number, ended?: number, expected?: string[],
 *   red?: {command: string, error?: string, detail?: string}[], ran?: string[], alone?: boolean,
 *   firstRed?: {command: string, error?: string, detail?: string}[], with?: string, rerun?: {passedAlone: boolean, firstRed: {command: string, error?: string, detail?: string}[]}}} SuiteRepoRecord
 */

const rootKey = (r) => {
  const s = String(r).replace(/\\/g, '/').replace(/\/+$/, '');
  return process.platform === 'win32' ? s.toLowerCase() : s;
};
const suiteSlug = (repo) => `${(rootKey(repo).split('/').pop() || 'repo').replace(/[^0-9a-z]+/gi, '-').slice(0, 24)}-${createHash('sha1').update(rootKey(repo)).digest('hex').slice(0, 6)}`;

function suiteRepoFiles(cwd, hash) {
  try {
    return readdirSync(planDir(cwd, hash)).filter((n) => /^suite-.+\.json$/.test(n));
  } catch {
    return [];
  }
}

/** Every repo's suite record of this plan. @returns {SuiteRepoRecord[]} */
export function readSuiteRepos(cwd, hash) {
  return suiteRepoFiles(cwd, hash).map((n) => readPlanFile(cwd, hash, n)).filter((r) => r && r.repo && r.status);
}

/**
 * Records one repo's tester. A red stop whose run overlapped another repo's tester, and that is not the
 * rerun alone, is stored as `recheck` (with the first failure): the suite is rerun alone before it counts.
 * A rerun keeps the first failure and records `rerun: {passedAlone, firstRed}`.
 * @param {string} cwd @param {string} hash @param {string} repo
 * @param {Omit<SuiteRepoRecord, 'repo' | 'ts'> & {ts?: number}} rec
 */
export function recordSuiteRepo(cwd, hash, repo, rec) {
  const now = Date.now();
  const mine = readSuiteRepos(cwd, hash).find((r) => rootKey(r.repo) === rootKey(repo));
  /** @type {SuiteRepoRecord} */
  let out = { repo, ts: now, ...rec };
  if (rec.status === 'running' && !rec.alone && mine?.status === 'done') {
    // A new round: this repo already had a finished run, so the other repos' non-running records are the
    // old round's. Drop them, or the aggregate would count them as this round's results.
    for (const f of suiteRepoFiles(cwd, hash)) {
      const r = readPlanFile(cwd, hash, f);
      if (r && rootKey(r.repo) !== rootKey(repo) && r.status !== 'running') {
        try { unlinkSync(join(planDir(cwd, hash), f)); } catch { /* best-effort */ }
      }
    }
  }
  if (rec.status === 'done') {
    const started = rec.started ?? mine?.started ?? now;
    const ended = rec.ended ?? now;
    out = { ...out, started, ended };
    if (rec.alone || mine?.alone) {
      const firstRed = rec.firstRed || mine?.firstRed || [];
      out = { ...out, rerun: { passedAlone: !(rec.red || []).length, firstRed } };
      delete out.firstRed;
    } else if ((rec.red || []).length) {
      const other = readSuiteRepos(cwd, hash).find((r) => rootKey(r.repo) !== rootKey(repo)
        && (r.started ?? r.ts) <= ended && (r.status === 'running' ? now - (Number(r.ts) || 0) <= 6 * 3600_000 : (r.ended ?? 0) >= started));
      if (other) {
        out = { ...out, status: 'recheck', firstRed: rec.red, with: other.repo };
        delete out.red;
      }
    }
  }
  writePlanFile(cwd, hash, `suite-${suiteSlug(repo)}.json`, out);
}

/**
 * Touched repos that have no tester record at all, while no repo tester is running: the aggregate suite
 * would stay `running` for them. Empty when a tester is in flight or every expected repo has a record.
 * @param {string} cwd @param {string} hash @returns {string[]}
 */
export function suiteReposWithoutTester(cwd, hash) {
  const repos = readSuiteRepos(cwd, hash);
  if (!repos.length || repos.some((r) => r.status === 'running')) return [];
  const expected = [...new Set(repos.flatMap((r) => r.expected || []).map(String))];
  return expected.filter((e) => !repos.some((r) => rootKey(r.repo) === rootKey(e)));
}

/** @returns {SuiteRecord|null} */
export function readSuite(cwd, hash) {
  const repos = readSuiteRepos(cwd, hash);
  if (!repos.length) return readPlanFile(cwd, hash, 'suite.json');
  const expected = [...new Set([...repos.flatMap((r) => r.expected || []), ...repos.map((r) => r.repo)].map(String))];
  const doneKeys = new Set(repos.filter((r) => r.status === 'done').map((r) => rootKey(r.repo)));
  const allDone = expected.every((r) => doneKeys.has(rootKey(r)));
  const ts = Math.max(...repos.map((r) => Number(r.ts) || 0));
  if (!allDone) return { status: 'running', ts };
  /** @type {Record<string, {passedAlone: boolean, firstRed: any[]}>} */
  const rerun = {};
  for (const r of repos) if (r.rerun) rerun[r.repo] = r.rerun;
  return {
    status: 'done', ts,
    red: repos.filter((r) => r.status === 'done').flatMap((r) => r.red || []),
    ran: repos.flatMap((r) => r.ran || []),
    ...(Object.keys(rerun).length ? { rerun } : {}),
  };
}

/** The post-suite fix's state (`suitefix.json`). @param {string} cwd @param {string} hash @param {SuiteRecord} rec */
export function recordSuiteFix(cwd, hash, rec) {
  writePlanFile(cwd, hash, 'suitefix.json', rec);
}

/** @returns {SuiteRecord|null} */
export function readSuiteFix(cwd, hash) {
  return readPlanFile(cwd, hash, 'suitefix.json');
}

/** When the reviewer was dispatched (`reviewlaunch.json`). @param {string} cwd @param {string} hash @param {number} [ts] @param {string[]} [repos] one reviewer per repo: the touched roots */
export function recordReviewLaunch(cwd, hash, ts = Date.now(), repos) {
  if (repos && repos.length) {
    // One reviewer per repo (1.3.0): a new round clears the parts and the merge claim of the last one.
    for (const n of reviewPartFiles(cwd, hash)) {
      try { unlinkSync(join(planDir(cwd, hash), n)); } catch { /* best-effort */ }
    }
    try { unlinkSync(join(planDir(cwd, hash), 'review-merge.lock')); } catch { /* none */ }
  }
  writePlanFile(cwd, hash, 'reviewlaunch.json', repos && repos.length ? { ts, repos } : { ts });
}

/** The repos a per-repo review was launched for (empty for a single review). @param {string} cwd @param {string} hash @returns {string[]} */
export function reviewLaunchRepos(cwd, hash) {
  const r = readPlanFile(cwd, hash, 'reviewlaunch.json')?.repos;
  return Array.isArray(r) ? r : [];
}

// ---- one reviewer per repo (1.3.0): `review-<slug>.json`, unnumbered findings, merged by the last part ----

function reviewPartFiles(cwd, hash) {
  try {
    return readdirSync(planDir(cwd, hash)).filter((n) => /^review-.+\.json$/.test(n));
  } catch {
    return [];
  }
}

/** One repo's review part. @param {string} cwd @param {string} hash @param {string} repo @param {any} part */
export function recordReviewPart(cwd, hash, repo, part) {
  writePlanFile(cwd, hash, `review-${suiteSlug(repo)}.json`, { repo, ts: Date.now(), ...part });
}

/** Every recorded review part of this plan. @returns {any[]} */
export function readReviewParts(cwd, hash) {
  return reviewPartFiles(cwd, hash).map((n) => readPlanFile(cwd, hash, n)).filter((r) => r && r.repo);
}

/** Whether this repo's part of the review is recorded. @param {string} cwd @param {string} hash @param {string} repo */
export function reviewPartRecorded(cwd, hash, repo) {
  return readReviewParts(cwd, hash).some((p) => rootKey(p.repo) === rootKey(repo));
}

/** Claims the merge of the parts: true for exactly one caller (exclusive create). @param {string} cwd @param {string} hash */
export function claimReviewMerge(cwd, hash) {
  try {
    writeFileSync(join(ensureDir(planDir(cwd, hash)), 'review-merge.lock'), String(Date.now()), { flag: 'wx' });
    return true;
  } catch {
    return false;
  }
}

/** When the reviewer was last dispatched for this plan, or 0. @param {string} cwd @param {string} hash */
export function reviewLaunchTs(cwd, hash) {
  return readPlanFile(cwd, hash, 'reviewlaunch.json')?.ts ?? 0;
}

/**
 * Whether a review of this plan was recorded since `sinceTs` (default: the last reviewer launch).
 * @param {string} cwd @param {string} hash @param {number} [sinceTs]
 */
export function reviewRecorded(cwd, hash, sinceTs) {
  return !!readReview(cwd, hash, sinceTs);
}

/**
 * The review of this plan recorded since `sinceTs` (default: the last reviewer launch), or null.
 * @param {string} cwd @param {string} hash @param {number} [sinceTs]
 * @returns {{id: string, ts: number}|null}
 */
export function readReview(cwd, hash, sinceTs) {
  const since = typeof sinceTs === 'number' ? sinceTs : (readPlanFile(cwd, hash, 'reviewlaunch.json')?.ts ?? 0);
  try {
    const r = JSON.parse(readFileSync(join(nxyRuntimeDir(cwd), 'review.json'), 'utf8'));
    return r && r.planHash === hash && typeof r.ts === 'number' && r.ts >= since ? { id: String(r.id ?? ''), ts: r.ts } : null;
  } catch {
    return null;
  }
}

/**
 * The last review of this plan in full (for the panel's checkpoint): findings (capped at 40) and the
 * ids already chosen. Null when there is no review of this plan.
 * @param {string} cwd @param {string} hash
 */
export async function readReviewDetail(cwd, hash) {
  try {
    const r = JSON.parse(readFileSync(join(nxyRuntimeDir(cwd), 'review.json'), 'utf8'));
    if (!r || r.planHash !== hash || !Array.isArray(r.findings)) return null;
    const { readJsonl } = await import('../../core/jsonl.mjs');
    const chosen = readJsonl(join(nxyRuntimeDir(cwd), 'review.jsonl'))
      .filter((x) => x && x.kind === 'chosen' && x.id === r.id).map((x) => x.finding);
    return {
      id: String(r.id ?? ''),
      ts: typeof r.ts === 'number' ? r.ts : 0,
      findings: r.findings.slice(0, 40).map((f) => ({
        id: String(f.id ?? ''),
        severity: String(f.severity ?? ''),
        lens: String(f.lens ?? ''),
        file: String(f.file ?? ''),
        line: Number.isFinite(Number(f.line)) && Number(f.line) > 0 && f.line !== null && f.line !== '' ? Number(f.line) : null,
        title: String(f.title ?? ''),
        cause: String(f.cause ?? ''),
      })),
      chosen: [...new Set(chosen.map(String))],
    };
  } catch {
    return null;
  }
}

/** Plans nobody verified anything for in 30 days. @param {string} cwd @param {string} keep */
function pruneOldPlans(cwd, keep) {
  const now = Date.now();
  for (const name of readdirSync(verifyRoot(cwd))) {
    const dir = join(verifyRoot(cwd), name);
    if (dir === planDir(cwd, keep)) continue;
    try {
      if (now - statSync(dir).mtimeMs > KEEP_MS) rmSync(dir, { recursive: true, force: true });
    } catch {
      /* best-effort */
    }
  }
}

/**
 * The plan's progress line (0.4.5), from the verification record and the last review of this plan.
 * @param {string} cwd @param {string|null} branch @param {string} plan
 */
export async function progressFor(cwd, branch, plan) {
  const { parseBatches, planHash } = await import('../../core/plan.mjs');
  const { progressLine } = await import('../../core/verify.mjs');
  const hash = planHash(plan);
  const batches = parseBatches(plan);
  const recorded = readVerify(cwd, branch, hash).batches;
  let review = null;
  try {
    const r = JSON.parse(readFileSync(join(nxyRuntimeDir(cwd), 'review.json'), 'utf8'));
    if (r && r.planHash === hash && Array.isArray(r.findings)) {
      const { readJsonl } = await import('../../core/jsonl.mjs');
      const chosen = new Set(readJsonl(join(nxyRuntimeDir(cwd), 'review.jsonl'))
        .filter((x) => x && x.kind === 'chosen' && x.id === r.id).map((x) => x.finding));
      review = {
        id: r.id,
        change: r.findings.filter((f) => f.cause === 'change').length,
        preexisting: r.findings.filter((f) => f.cause !== 'change').length,
        chosen: chosen.size,
      };
    }
  } catch {
    /* no review yet */
  }
  return progressLine(batches, recorded, review);
}

/** The subagent's own transcript: given by the hook, or where Claude Code keeps it. */
export function agentTranscriptPath(input) {
  if (typeof input?.agent_transcript_path === 'string') return input.agent_transcript_path;
  if (typeof input?.transcript_path === 'string' && typeof input?.agent_id === 'string') {
    return join(input.transcript_path.replace(/\.jsonl$/, ''), 'subagents', `agent-${input.agent_id}.jsonl`);
  }
  return null;
}

function readTail(path) {
  const size = statSync(path).size;
  if (size <= MAX_SCAN_BYTES) return readFileSync(path, 'utf8');
  const fd = openSync(path, 'r');
  try {
    const buf = Buffer.alloc(MAX_SCAN_BYTES);
    readSync(fd, buf, 0, MAX_SCAN_BYTES, size - MAX_SCAN_BYTES);
    return buf.toString('utf8');
  } finally {
    closeSync(fd);
  }
}

const textOf = (content) => (typeof content === 'string'
  ? content
  : Array.isArray(content) ? content.map((b) => (typeof b?.text === 'string' ? b.text : '')).join('\n') : '');

/**
 * What an implementer was asked and what it did, in order: its first prompt, its edits, and its
 * shell runs with how each ended (`is_error` on the tool result = non-zero exit, timeout, interrupt).
 * @param {string|null} path
 * @returns {{prompt: string, events: import('../../core/verify.mjs').Event[]}}
 */
export function readAgentRun(path) {
  /** @type {import('../../core/verify.mjs').Event[]} */
  const events = [];
  let prompt = '';
  if (!path || !existsSync(path)) return { prompt, events };
  /** @type {Map<string, {kind: 'run', command: string, ok: boolean, error?: string}>} */
  const runs = new Map();
  let text;
  try {
    text = readTail(path);
  } catch {
    return { prompt, events };
  }
  for (const line of text.split('\n')) {
    if (!line || line[0] !== '{') continue;
    let e;
    try {
      e = JSON.parse(line);
    } catch {
      continue;
    }
    const content = e.message?.content;
    if (e.type === 'user' && !prompt) {
      const t = textOf(content);
      if (t && !(Array.isArray(content) && content.some((b) => b?.type === 'tool_result'))) {
        prompt = t;
        continue;
      }
    }
    if (!Array.isArray(content)) continue;
    for (const b of content) {
      if (e.type === 'assistant' && b?.type === 'tool_use') {
        if (EDIT_TOOLS.has(b.name)) events.push({ kind: 'edit' });
        else if (SHELL_TOOLS.has(b.name) && typeof b.input?.command === 'string' && !b.input.run_in_background) {
          // Not ok until its result says so: a run with no result (killed, still going) proves nothing.
          const run = { kind: /** @type {const} */ ('run'), command: b.input.command, ok: false, error: 'no result' };
          runs.set(b.id, run);
          events.push(run);
        }
      } else if (e.type === 'user' && b?.type === 'tool_result' && runs.has(b.tool_use_id)) {
        const run = /** @type {{kind: 'run', command: string, ok: boolean, error?: string}} */ (runs.get(b.tool_use_id));
        run.ok = !b.is_error;
        const first = textOf(b.content).split('\n').map((l) => l.trim()).find(Boolean) || '';
        if (run.ok) delete run.error;
        else run.error = first.slice(0, 160);
      }
    }
  }
  return { prompt, events };
}
