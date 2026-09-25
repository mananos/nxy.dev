// @ts-check
/**
 * Claude Code side of the plan checkpoint (core/plan.mjs): finding the user's approval in the
 * transcript, and a small marker so the edit hook knows a plan exists without opening SQLite on
 * every edit.
 *
 * Authority comes from the transcript. The approval is an AskUserQuestion that the *main thread*
 * asked ("Approve nxy plan <hash>?") and a tool result carrying the user's answer — both written by
 * Claude Code, not by the model. The model can ask the question; it cannot answer it.
 *
 * The marker (`.nxy/local/plan.json`) and the approval cache (`plan-approvals.json`) are indexes,
 * not authority: the marker is written by `mem handoff plan|save`, the cache only after a transcript
 * scan found the approval. Losing either costs one lookup, never a wrong verdict in the user's
 * disfavour.
 */
import { closeSync, existsSync, openSync, readFileSync, readSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { ensureDir, nxyRuntimeDir } from '../../core/paths.mjs';
import { APPROVE, approvalQuestion } from '../../core/plan.mjs';

const markerPath = (cwd) => join(nxyRuntimeDir(cwd), 'plan.json');
const cachePath = (cwd) => join(nxyRuntimeDir(cwd), 'plan-approvals.json');

/** A transcript bigger than this is scanned from its tail: an approval is recent by nature. */
const MAX_SCAN_BYTES = 32 * 1024 * 1024;

/**
 * Records that the branch has a plan with this hash and this many open questions (null clears it).
 * @param {string} cwd @param {string|null} branch @param {string|null} hash @param {number} [questions]
 */
export function writeMarker(cwd, branch, hash, questions = 0) {
  try {
    if (!hash) {
      rmSync(markerPath(cwd), { force: true });
      return;
    }
    ensureDir(nxyRuntimeDir(cwd));
    writeFileSync(markerPath(cwd), JSON.stringify({ branch: branch || null, hash, questions }), 'utf8');
  } catch {
    /* best-effort */
  }
}

/**
 * The plan pending on this branch, or null.
 * @returns {{hash: string, questions: number}|null}
 */
export function readMarker(cwd, branch) {
  try {
    if (!existsSync(markerPath(cwd))) return null;
    const m = JSON.parse(readFileSync(markerPath(cwd), 'utf8'));
    return m && (m.branch || null) === (branch || null) && typeof m.hash === 'string'
      ? { hash: m.hash, questions: Number(m.questions) || 0 }
      : null;
  } catch {
    return null;
  }
}

function readCache(cwd) {
  try {
    const v = JSON.parse(readFileSync(cachePath(cwd), 'utf8'));
    return Array.isArray(v) ? v.map(String) : [];
  } catch {
    return [];
  }
}

function addToCache(cwd, hash) {
  try {
    ensureDir(nxyRuntimeDir(cwd));
    writeFileSync(cachePath(cwd), JSON.stringify([...new Set([...readCache(cwd), hash])].slice(-50)), 'utf8');
  } catch {
    /* best-effort */
  }
}

/** Reads the transcript, or its last MAX_SCAN_BYTES. */
function readTranscript(path) {
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

/**
 * Whether the user approved this plan hash, according to the main transcript: the latest answer to
 * "Approve nxy plan <hash>?" wins, so "Change" after an "Approve" of the same hash revokes it.
 * @param {string|null|undefined} transcriptPath
 * @param {string} hash
 */
export function findApproval(transcriptPath, hash) {
  return findAnswer(transcriptPath, approvalQuestion(hash)) === APPROVE;
}

/**
 * The user's latest answer to one exact AskUserQuestion question of nxy's, in the main transcript,
 * or null. Sidechain (subagent) records are ignored: only the main thread's question counts.
 * @param {string|null|undefined} transcriptPath
 * @param {string} wanted the question text, exactly as nxy dictated it
 * @returns {string|null}
 */
export function findAnswer(transcriptPath, wanted) {
  if (!transcriptPath || !existsSync(transcriptPath)) return null;
  /** @type {Set<string>} */
  const asked = new Set();
  /** @type {string|null} */
  let verdict = null;
  let text;
  try {
    text = readTranscript(transcriptPath);
  } catch {
    return null;
  }
  for (const line of text.split('\n')) {
    if (!line || line[0] !== '{' || !line.includes('nxy plan')) continue;
    let e;
    try {
      e = JSON.parse(line);
    } catch {
      continue;
    }
    if (e.isSidechain) continue;
    const content = e.message?.content;
    if (!Array.isArray(content)) continue;
    if (e.type === 'assistant') {
      for (const b of content) {
        if (b?.type === 'tool_use' && b.name === 'AskUserQuestion' && Array.isArray(b.input?.questions)
          && b.input.questions.some((q) => q?.question === wanted)) asked.add(b.id);
      }
    } else if (e.type === 'user') {
      for (const b of content) {
        if (b?.type !== 'tool_result' || b.is_error || !asked.has(b.tool_use_id)) continue;
        const answers = e.toolUseResult?.answers;
        if (answers && typeof answers === 'object' && typeof answers[wanted] === 'string') verdict = answers[wanted];
      }
    }
  }
  return verdict;
}

/**
 * Approval with the cache in front of the scan. Only a positive result is cached: a pending plan
 * is rescanned until approved (the scan is what notices the approval).
 */
export function isApproved(cwd, transcriptPath, hash) {
  if (readCache(cwd).includes(hash)) return true;
  const ok = findApproval(transcriptPath, hash);
  if (ok) addToCache(cwd, hash);
  return ok;
}
