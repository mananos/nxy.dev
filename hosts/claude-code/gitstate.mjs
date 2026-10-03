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
import { existsSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';
import { MAX_COPY_BYTES, ensureDir, gitBranch, gitRoot, nxyRuntimeDir } from '../../core/paths.mjs';
import { fileDiff } from '../../core/diff.mjs';
import { primaryRoot, repoLabel, repoNames, undeclaredRepos } from './repos.mjs';

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

const same = (a, b) => keyOf(a) === keyOf(b);
const keyOf = (p) => (process.platform === 'win32' ? resolve(p).toLowerCase() : resolve(p));

/**
 * Notes the files already dirty when the task's first plan was approved: ONE snapshot per task. A
 * revised plan (new hash, same branch) does not retake it, so the implementer's edits never turn
 * into "already dirty"; it only adds the git repos the revised plan newly declares. A record for
 * another branch is replaced.
 * @param {string} cwd @param {string|null} branch
 * @param {({root: string, git?: boolean}|string)[]} [repos] the primary and the declared repos
 * @param {string} [hash] hash of the approved plan (informational)
 */
export function recordDirtyAtApproval(cwd, branch, repos = [], hash = '') {
  try {
    const prev = readDirtyAtApproval(cwd, branch);
    const primary = primaryRoot(cwd);
    const all = repos.length ? repos : [primary];
    const names = repoNames([primary, ...all]);
    const known = prev ? prev.others.map((o) => o.root) : [];
    const others = prev ? [...prev.others] : [];
    for (const r of all) {
      const root = typeof r === 'string' ? r : r.root;
      if (same(root, primary) || known.some((k) => same(k, root))) continue;
      if (typeof r !== 'string' ? r.git === false : !gitRoot(root)) continue;
      const s = status(root);
      others.push({
        root, name: names.get(root) ?? root, branch: gitBranch(root), available: s.files != null, reason: s.reason, files: s.files || [],
      });
    }
    /** @type {NonNullable<ReturnType<typeof readDirtyAtApproval>>} */
    let rec;
    if (!prev) {
      const root = gitRoot(cwd);
      const s = root ? status(root) : { files: null, reason: 'not a git repo' };
      rec = {
        branch: branch || null, hash, root, available: s.files != null, reason: s.reason, files: s.files || [], others,
      };
    } else if (others.length === prev.others.length) return;
    else rec = { ...prev, others };
    ensureDir(nxyRuntimeDir(cwd));
    writeFileSync(dirtyPath(cwd), JSON.stringify(rec), 'utf8');
  } catch {
    /* best-effort */
  }
}

/**
 * @typedef {{root: string, name: string, branch: string|null, available: boolean, reason: string|null, files: string[]}} DirtyOther
 * @param {string} cwd @param {string|null} branch
 * @returns {{branch: string|null, hash: string, root: string|null, available: boolean, reason: string|null, files: string[], others: DirtyOther[]}|null}
 */
export function readDirtyAtApproval(cwd, branch) {
  try {
    if (!existsSync(dirtyPath(cwd))) return null;
    const d = JSON.parse(readFileSync(dirtyPath(cwd), 'utf8'));
    if (!d || (d.branch || null) !== (branch || null) || !Array.isArray(d.files)) return null;
    return { ...d, others: Array.isArray(d.others) ? d.others : [] };
  } catch {
    return null;
  }
}

/** The task's snapshot ends with the task. */
export function clearDirtyAtApproval(cwd) {
  try {
    rmSync(dirtyPath(cwd), { force: true });
  } catch {
    /* best-effort */
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
 * The primary repo, then the repos of the approval record, then the undeclared ones edited since.
 * One `git status` per repo.
 * @param {string} cwd @param {string|null} branch
 * @param {Set<string>} covered absolute paths that have a baseline entry
 * @param {({root: string}|string)[]} [repos] the marker's repos
 * @returns {{files: any[], notReviewed: {path: string, reason: string}[], note: string|null, atApproval: number, now: number|null}}
 */
export function gitChangedFiles(cwd, branch, covered, repos = []) {
  const primary = primaryRoot(cwd);
  const rec = readDirtyAtApproval(cwd, branch);
  const recOthers = rec ? rec.others : [];
  const undeclared = undeclaredRepos(cwd).filter((r) => !same(r, primary) && !recOthers.some((o) => same(o.root, r))
    && !repos.some((d) => same(typeof d === 'string' ? d : d.root, r)));
  const names = repoNames([primary, ...repos, ...recOthers.map((o) => o.root), ...undeclared]);
  const files = [];
  const notReviewed = [];
  /** @type {string[]} */
  const notes = [];
  let atApproval = 0;
  /** @type {number|null} */
  let now = null;

  /** @param {string} root @param {boolean} isPrimary @param {Set<string>|null} before @param {string} prefix */
  const scan = (root, isPrimary, before, prefix) => {
    const s = status(root);
    if (!s.files) {
      notes.push(`${prefix}${s.reason}`);
      return;
    }
    now = (now ?? 0) + s.files.length;
    const repo = { root, primary: isPrimary };
    const name = names.get(root) ?? root;
    for (const rel of s.files) {
      const abs = join(root, rel);
      if (covered.has(abs)) continue;
      const label = repoLabel(repo, abs, names);
      if (!before || before.has(rel)) {
        notReviewed.push({
          path: label,
          reason: before
            ? 'already modified before the plan was approved and nothing copied it, so the plan\'s part cannot be told from the user\'s'
            : `${name} is not in the plan; its prior uncommitted changes cannot be told apart`,
        });
        continue;
      }
      one(root, rel, abs, label);
    }
  };

  if (!gitRoot(cwd)) notes.push('not a git repo');
  else if (!rec) notes.push('git: no list of changed files was taken when the plan was approved');
  else if (!rec.available) notes.push(rec.reason || 'git not available');
  else {
    atApproval += rec.files.length;
    scan(primary, true, new Set(rec.files), '');
  }
  for (const o of recOthers) {
    atApproval += o.files.length;
    if (!o.available) {
      notes.push(`${o.name}: ${o.reason || 'git not available'}`);
      continue;
    }
    const nowBranch = gitBranch(o.root);
    if (nowBranch !== o.branch) notes.push(`${o.name}: branch changed from ${o.branch ?? '(none)'} to ${nowBranch ?? '(none)'} since the plan was approved`);
    scan(o.root, false, new Set(o.files), `${o.name}: `);
  }
  for (const r of undeclared) scan(r, false, null, `${names.get(r) ?? r}: `);

  /** One file with no copy: the diff against HEAD. */
  function one(root, rel, abs, label) {
    const base = { path: label, abs, added: 0, removed: 0, diff: '', ranges: [], content: null, source: 'git' };
    const exists = existsSync(abs);
    let size = 0;
    try {
      size = exists ? statSync(abs).size : 0;
    } catch {
      /* gone meanwhile */
    }
    if (size > MAX_COPY_BYTES) {
      files.push({ ...base, status: 'changed (large, not diffed)' });
      return;
    }
    const cur = exists ? read(abs) : null;
    if (cur != null && cur.includes('\0')) {
      files.push({ ...base, status: 'changed (binary, not diffed)' });
      return;
    }
    const head = gitRun(root, ['show', `HEAD:${rel}`]);
    if (head != null && head.includes('\0')) {
      files.push({ ...base, status: 'changed (binary, not diffed)' });
      return;
    }
    if (head === cur) return;
    const d = fileDiff(label, head, cur);
    if (!d.text) return;
    const st = head == null ? 'new' : cur == null ? 'deleted' : 'modified';
    files.push({ ...base, status: st, added: d.added, removed: d.removed, diff: d.text, ranges: d.ranges, content: cur });
  }

  files.sort((a, b) => a.path.localeCompare(b.path));
  notReviewed.sort((a, b) => a.path.localeCompare(b.path));
  return { files, notReviewed, note: notes.length ? notes.join('; ') : null, atApproval, now };
}
