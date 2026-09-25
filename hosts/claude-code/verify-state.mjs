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
 * @typedef {{branch: string|null, hash: string, batches: Record<string, {status: import('../../core/verify.mjs').Status, error?: string, ts: number}>, sentBack: string[]}} VerifyState
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
 * @param {{status: import('../../core/verify.mjs').Status, error?: string, ts: number}} verdict
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
