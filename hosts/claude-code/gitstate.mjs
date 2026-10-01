// @ts-check
/**
 * The git half of the plan's review (batch B): files the baseline copies did not cover (a script,
 * `xargs`, `rm`, `git checkout -- f`...) are found with a read-only `git status`.
 *
 * At approval (`recordDirtyAtApproval`) nxy notes which files are already dirty, so the user's own
 * uncommitted work is not taken for the plan's. At review, a file that is dirty now, was clean then
 * and has no copy is the plan's (`gitChangedFiles`).
 *
 * Nothing is ever written into .git: every call is read-only and runs with GIT_OPTIONAL_LOCKS=0
 * (a plain `git status` would otherwise refresh .git/index). The list lives in
 * `.nxy/local/plan-dirty.json` (paths only, a few KB) and goes with the plan marker.
 */
import { existsSync, readFileSync, statSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import { MAX_COPY_BYTES, ensureDir, gitRoot, nxyRuntimeDir } from '../../core/paths.mjs';
import { fileDiff } from '../../core/diff.mjs';

/** A repo with an untracked node_modules would otherwise be walked and stored. */
export const MAX_DIRTY_FILES = 5000;

const dirtyPath = (cwd) => join(nxyRuntimeDir(cwd), 'plan-dirty.json');

/**
 * stdout of a read-only git command, or null on any error (no git, no repo, timeout).
 * @param {string} root @param {string[]} args
 * @returns {string|null}
 */
export function gitRun(root, args) {
  try {
    const r = spawnSync('git', args, {
      cwd: root, env: { ...process.env, GIT_OPTIONAL_LOCKS: '0' }, timeout: 10000, windowsHide: true,
      maxBuffer: 16 * 1024 * 1024, encoding: 'utf8',
    });
    if (r.error || r.status !== 0) return null;
    return r.stdout;
  } catch {
    return null;
  }
}

/**
 * `git status --porcelain -z -uall`, parsed. Root-relative paths, nothing under `.nxy/`.
 * @param {string} root
 * @returns {{files: string[]|null, reason: string|null}}
 */
function status(root) {
  const out = gitRun(root, ['status', '--porcelain', '-z', '-uall']);
  if (out == null) return { files: null, reason: 'git not available' };
  const parts = out.split('\0');
  const files = [];
  for (let i = 0; i < parts.length; i++) {
    const e = parts[i];
    if (e.length < 4) continue;
    const xy = e.slice(0, 2);
    const p = e.slice(3);
    if (/[RC]/.test(xy)) i++; // renames and copies: the next entry is the old path
    if (p === '.nxy' || p.startsWith('.nxy/')) continue;
    files.push(p);
  }
  if (files.length > MAX_DIRTY_FILES) return { files: null, reason: `too many changed files (${files.length})` };
  return { files, reason: null };
}

/**
 * Files with uncommitted changes, relative to the repo root; null when git is unavailable or the
 * list is over MAX_DIRTY_FILES.
 * @param {string} root
 */
export function dirtyFiles(root) {
  return status(root).files;
}

/**
 * Notes the files already dirty when the plan was approved. Does nothing when a list for the same
 * branch and hash exists: a `handoff save` that re-saves the plan mid-implementation must not count
 * the implementer's edits as "already dirty".
 * @param {string} cwd @param {string|null} branch @param {string} hash
 */
export function recordDirtyAtApproval(cwd, branch, hash) {
  try {
    if (readDirtyAtApproval(cwd, branch, hash)) return;
    const root = gitRoot(cwd);
    const s = root ? status(root) : { files: null, reason: 'not a git repo' };
    ensureDir(nxyRuntimeDir(cwd));
    writeFileSync(dirtyPath(cwd), JSON.stringify({
      branch: branch || null, hash, root, available: s.files != null, reason: s.reason, files: s.files || [],
    }), 'utf8');
  } catch {
    /* best-effort */
  }
}

/**
 * @param {string} cwd @param {string|null} branch @param {string} hash
 * @returns {{branch: string|null, hash: string, root: string|null, available: boolean, reason: string|null, files: string[]}|null}
 */
export function readDirtyAtApproval(cwd, branch, hash) {
  try {
    if (!existsSync(dirtyPath(cwd))) return null;
    const d = JSON.parse(readFileSync(dirtyPath(cwd), 'utf8'));
    if (!d || (d.branch || null) !== (branch || null) || d.hash !== hash || !Array.isArray(d.files)) return null;
    return d;
  } catch {
    return null;
  }
}

const read = (p) => {
  try {
    return readFileSync(p, 'utf8');
  } catch {
    return null;
  }
};

/**
 * What changed since approval without a baseline copy, from git.
 * @param {string} cwd @param {string|null} branch @param {string} hash
 * @param {Set<string>} covered absolute paths that have a baseline entry
 * @returns {{files: any[], notReviewed: {path: string, reason: string}[], note: string|null, atApproval: number, now: number|null}}
 */
export function gitChangedFiles(cwd, branch, hash, covered) {
  const none = (note, atApproval = 0) => ({ files: [], notReviewed: [], note, atApproval, now: null });
  const root = gitRoot(cwd);
  if (!root) return none('not a git repo');
  const rec = readDirtyAtApproval(cwd, branch, hash);
  if (!rec) return none('git: no list of changed files was taken when the plan was approved');
  if (!rec.available) return none(rec.reason || 'git not available');
  const s = status(root);
  if (!s.files) return none(s.reason, rec.files.length);
  const before = new Set(rec.files);
  const files = [];
  const notReviewed = [];
  for (const rel of s.files) {
    const abs = join(root, rel);
    if (covered.has(abs)) continue;
    if (before.has(rel)) {
      notReviewed.push({
        path: rel,
        reason: 'already modified before the plan was approved and nothing copied it, so the plan\'s part cannot be told from the user\'s',
      });
      continue;
    }
    const base = { path: rel, abs, added: 0, removed: 0, diff: '', ranges: [], content: null, source: 'git' };
    const exists = existsSync(abs);
    let size = 0;
    try {
      size = exists ? statSync(abs).size : 0;
    } catch {
      /* gone meanwhile */
    }
    if (size > MAX_COPY_BYTES) {
      files.push({ ...base, status: 'changed (large, not diffed)' });
      continue;
    }
    const now = exists ? read(abs) : null;
    if (now != null && now.includes('\0')) {
      files.push({ ...base, status: 'changed (binary, not diffed)' });
      continue;
    }
    const head = gitRun(root, ['show', `HEAD:${rel}`]);
    if (head != null && head.includes('\0')) {
      files.push({ ...base, status: 'changed (binary, not diffed)' });
      continue;
    }
    if (head === now) continue;
    const d = fileDiff(rel, head, now);
    if (!d.text) continue;
    const st = head == null ? 'new' : now == null ? 'deleted' : 'modified';
    files.push({ ...base, status: st, added: d.added, removed: d.removed, diff: d.text, ranges: d.ranges, content: now });
  }
  files.sort((a, b) => a.path.localeCompare(b.path));
  notReviewed.sort((a, b) => a.path.localeCompare(b.path));
  return { files, notReviewed, note: null, atApproval: rec.files.length, now: s.files.length };
}
