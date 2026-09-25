// @ts-check
/**
 * The "before" of a plan (0.4.2): a copy of each file the first time the plan's work touches it,
 * taken by the edit hook. The review diffs these copies against the files as they are now.
 *
 * Why copies and not git: `git diff HEAD` would put the user's own uncommitted changes in those
 * files into the review as if the plan had made them, and nxy does not run git actions on the
 * user's repo. Only files the plan edits are copied, once each (<1 ms for a source file); a file
 * over MAX_COPY_BYTES is recorded as changed but not copied. It lives in
 * `.nxy/local/baseline/<branch>/` and is removed with the handoff (`mem handoff done`).
 */
import { copyFileSync, existsSync, readFileSync, readdirSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { join, relative, resolve } from 'node:path';
import { ensureDir, nxyRuntimeDir } from '../../core/paths.mjs';
import { fileDiff } from '../../core/diff.mjs';

/** Generated bundles, lockfiles, dumps: recorded as changed, not diffed. */
export const MAX_COPY_BYTES = 2 * 1024 * 1024;

const slug = (branch) => String(branch || 'no-branch').replace(/[^A-Za-z0-9._-]+/g, '_');
const dirOf = (cwd, branch) => join(nxyRuntimeDir(cwd), 'baseline', slug(branch));
const idOf = (abs) => createHash('sha1').update(abs).digest('hex').slice(0, 16);

/**
 * One entry file per path (`<id>.json`) next to its copy (`<id>`), created with `wx`: two
 * implementers editing in parallel (0.4.4) can never overwrite each other's entry, and the first
 * edit of a file always wins.
 * @typedef {{path: string, existed: boolean, copy: string|null, skipped?: 'large'}} Entry
 * @typedef {Record<string, Entry>} BaselineIndex  keyed by absolute path
 */

/** @returns {BaselineIndex} */
export function readBaseline(cwd, branch) {
  /** @type {BaselineIndex} */
  const out = {};
  try {
    for (const n of readdirSync(dirOf(cwd, branch))) {
      if (!n.endsWith('.json')) continue;
      try {
        const e = JSON.parse(readFileSync(join(dirOf(cwd, branch), n), 'utf8'));
        if (e && typeof e.path === 'string') out[e.path] = e;
      } catch {
        /* half-written by a concurrent snapshot: its writer finishes it */
      }
    }
  } catch {
    /* no baseline yet */
  }
  return out;
}

/**
 * Keeps the "before" of a file, unless it already has one for this branch's task.
 * @param {string} cwd project root
 * @param {string|null} branch
 * @param {string} filePath as the edit tool gives it (absolute, or relative to cwd)
 * @returns {boolean} whether this call recorded it
 */
export function snapshot(cwd, branch, filePath) {
  try {
    const abs = resolve(cwd, filePath);
    const dir = dirOf(cwd, branch);
    const id = idOf(abs);
    const entryPath = join(dir, `${id}.json`);
    if (existsSync(entryPath)) return false;
    ensureDir(dir);
    /** @type {Entry} */
    let entry = { path: abs, existed: false, copy: null };
    if (existsSync(abs)) {
      if (statSync(abs).size > MAX_COPY_BYTES) entry = { path: abs, existed: true, copy: null, skipped: 'large' };
      else {
        // A name of its own: a concurrent snapshot that loses the race never touches the winner's copy.
        const copy = `${id}.${process.pid}.${Date.now()}`;
        copyFileSync(abs, join(dir, copy));
        entry = { path: abs, existed: true, copy };
      }
    }
    // `wx`: if another snapshot of the same file got here first, its entry stands and this copy goes.
    try {
      writeFileSync(entryPath, JSON.stringify(entry), { encoding: 'utf8', flag: 'wx' });
    } catch {
      if (entry.copy) rmSync(join(dir, entry.copy), { force: true });
      return false;
    }
    return true;
  } catch {
    return false;
  }
}

/** Removes the branch's copies (the task is over). */
export function clearBaseline(cwd, branch) {
  try {
    rmSync(dirOf(cwd, branch), { recursive: true, force: true });
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
 * What the plan changed: one entry per touched file whose content differs from its copy. Paths
 * are shown relative to the project when inside it (forward slashes), absolute otherwise.
 * @param {string} cwd
 * @param {string|null} branch
 */
export function changedFiles(cwd, branch) {
  const index = readBaseline(cwd, branch);
  const dir = dirOf(cwd, branch);
  const out = [];
  for (const [abs, e] of Object.entries(index)) {
    const rel = relative(cwd, abs);
    const label = (rel && !rel.startsWith('..') ? rel : abs).replace(/\\/g, '/');
    const now = existsSync(abs) ? read(abs) : null;
    if (e.skipped === 'large') {
      out.push({ path: label, abs, status: 'changed (large, not diffed)', added: 0, removed: 0, diff: '', ranges: [], content: null });
      continue;
    }
    const before = e.existed && e.copy ? read(join(dir, e.copy)) : null;
    if (before === now) continue;
    const d = fileDiff(label, before, now);
    if (!d.text) continue;
    const status = before == null ? 'new' : now == null ? 'deleted' : 'modified';
    out.push({ path: label, abs, status, added: d.added, removed: d.removed, diff: d.text, ranges: d.ranges, content: now });
  }
  return out.sort((a, b) => a.path.localeCompare(b.path));
}
