// @ts-check
/**
 * A light inventory of the repos that have no git (1.0.1): at approval, `rel -> [size, mtimeMs, hash]`
 * of every file (skipping dependency and build folders); at review, the files that are new, deleted or
 * whose content changed with no copy of their own. Where there is git, git covers it and no inventory
 * is taken.
 *
 * The hash is the sha1 of the content: a `touch` or a save with no change moves the mtime but not the
 * hash, so it is not reported. Files over MAX_COPY_BYTES are never read (size and mtime only).
 * One inventory per task: it lives in the baseline folder (`mem handoff done` removes it); a revised
 * plan only adds the roots that are not in it yet.
 */
import { createHash } from 'node:crypto';
import { existsSync, readFileSync, readdirSync, statSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { MAX_COPY_BYTES, ensureDir, gitRoot } from '../../core/paths.mjs';
import { fileDiff } from '../../core/diff.mjs';
import { SKIP_DIRS } from './docs.mjs';
import { baselineDir } from './baseline.mjs';
import { primaryRoot, repoLabel, repoNames } from './repos.mjs';

export const INVENTORY_CAP = 20000;
const MAX_ENTRIES = 200;
const WIN = process.platform === 'win32';
const keyOf = (p) => (WIN ? resolve(p).toLowerCase() : resolve(p));
const rootOf = (r) => (typeof r === 'string' ? r : r.root);
const invPath = (cwd, branch) => join(baselineDir(cwd, branch), 'inventory.json');
const hashOf = (buf) => createHash('sha1').update(buf).digest('base64');

/** The primary and the declared roots that are not inside a git repo. */
export function inventoryRoots(cwd, repos = []) {
  const out = [];
  const seen = new Set();
  for (const r of [primaryRoot(cwd), ...repos.map(rootOf)]) {
    const abs = resolve(r);
    if (seen.has(keyOf(abs))) continue;
    seen.add(keyOf(abs));
    if (existsSync(abs) && gitRoot(abs) == null) out.push(abs);
  }
  return out;
}

/**
 * Iterative walk: `[rel, size, mtimeMs][]`, symlinks not followed, SKIP_DIRS skipped; stops past `cap`.
 * @returns {{files: [string, number, number][], skipped: boolean}}
 */
function walk(root, cap) {
  /** @type {[string, number, number][]} */
  const files = [];
  const stack = [''];
  while (stack.length) {
    const rel = stack.pop() ?? '';
    let entries;
    try {
      entries = readdirSync(join(root, rel), { withFileTypes: true });
    } catch {
      continue;
    }
    for (const e of entries) {
      const r = rel ? `${rel}/${e.name}` : e.name;
      if (e.isDirectory()) {
        if (!SKIP_DIRS.has(e.name)) stack.push(r);
      } else if (e.isFile()) {
        if (files.length >= cap) return { files, skipped: true };
        try {
          const st = statSync(join(root, r));
          files.push([r, st.size, st.mtimeMs]);
        } catch {
          /* gone */
        }
      }
    }
  }
  return { files, skipped: false };
}

function readInventory(cwd, branch) {
  try {
    const v = JSON.parse(readFileSync(invPath(cwd, branch), 'utf8'));
    return Array.isArray(v?.roots) ? v : null;
  } catch {
    return null;
  }
}

/**
 * Takes the inventory of the roots that have none yet (all of them at the first approval of the task).
 * @param {string} cwd @param {string|null} branch @param {({root: string}|string)[]} [repos]
 * @param {{cap?: number}} [opts]
 * @returns {number} how many roots were walked now
 */
export function takeInventory(cwd, branch, repos = [], opts = {}) {
  const cap = opts.cap ?? INVENTORY_CAP;
  const cur = readInventory(cwd, branch) ?? { ts: Date.now(), roots: [] };
  const have = new Set(cur.roots.map((r) => keyOf(r.root)));
  let added = 0;
  for (const root of inventoryRoots(cwd, repos)) {
    if (have.has(keyOf(root))) continue;
    const w = walk(root, cap);
    if (w.skipped) {
      cur.roots.push({ root, files: null, count: w.files.length, skipped: 'too many files' });
    } else {
      /** @type {Record<string, [number, number, string|null]>} */
      const files = {};
      for (const [rel, size, mtime] of w.files) {
        let h = null;
        if (size <= MAX_COPY_BYTES) {
          try {
            h = hashOf(readFileSync(join(root, rel)));
          } catch {
            /* unreadable: size and mtime only */
          }
        }
        files[rel] = [size, mtime, h];
      }
      cur.roots.push({ root, files, count: w.files.length });
    }
    added += 1;
  }
  if (added) {
    try {
      ensureDir(baselineDir(cwd, branch));
      writeFileSync(invPath(cwd, branch), JSON.stringify(cur), 'utf8');
    } catch {
      /* best-effort */
    }
  }
  return added;
}

/**
 * Whether a file inside an inventoried root is new or has different content than at approval.
 * False when there is no inventory for its root (nothing to compare with).
 * @param {string} cwd @param {string|null} branch @param {string} abs
 */
export function inventoryDiffers(cwd, branch, abs) {
  const inv = readInventory(cwd, branch);
  if (!inv) return false;
  const k = keyOf(abs);
  for (const r of inv.roots) {
    if (r.skipped || !r.files) continue;
    const rk = keyOf(r.root);
    if (!k.startsWith(rk + (rk.endsWith('/') || rk.endsWith('\\') ? '' : (WIN ? '\\' : '/')))) continue;
    const rel = abs.slice(resolve(r.root).length + 1).replace(/\\/g, '/');
    const old = Object.entries(r.files).find(([p]) => (WIN ? p.toLowerCase() === rel.toLowerCase() : p === rel))?.[1];
    if (!existsSync(abs)) return !!old;
    if (!old) return true;
    try {
      const st = statSync(abs);
      if (st.size === old[0] && st.mtimeMs === old[1]) return false;
      if (old[2] == null || st.size > MAX_COPY_BYTES) return true;
      return hashOf(readFileSync(abs)) !== old[2];
    } catch {
      return false;
    }
  }
  return false;
}

/**
 * What changed in the inventoried roots since approval, with no copy of its own.
 * @param {string} cwd @param {string|null} branch @param {({root: string}|string)[]} repos
 * @param {Set<string>|string[]} covered absolute paths that have a baseline entry
 */
export function diffInventory(cwd, branch, repos, covered) {
  const inv = readInventory(cwd, branch);
  const empty = { files: [], more: 0, coverage: [] };
  if (!inv) return empty;
  const cov = new Set([...covered].map((p) => keyOf(p)));
  const known = [primaryRoot(cwd), ...(repos || []).map(rootOf)];
  const names = repoNames([...known, ...inv.roots.map((r) => r.root)]);
  const files = [];
  const coverage = [];
  for (const r of inv.roots) {
    const root = r.root;
    const name = names.get(root) ?? root;
    if (r.skipped) {
      coverage.push({ root, name, files: r.count, now: 0, skipped: r.skipped });
      continue;
    }
    const w = walk(root, INVENTORY_CAP);
    coverage.push({ root, name, files: r.count, now: w.files.length, skipped: w.skipped ? 'too many files' : null });
    const repo = { root, primary: keyOf(root) === keyOf(primaryRoot(cwd)) };
    const label = (abs) => repoLabel(repo, abs, names);
    const nowSet = new Set();
    const add = (rel, status, extra = {}) => {
      const abs = join(root, rel);
      files.push({ path: label(abs), abs, status, added: 0, removed: 0, diff: '', ranges: [], content: null, ...extra, source: 'inventory' });
    };
    for (const [rel, size, mtime] of w.files) {
      nowSet.add(rel);
      const abs = join(root, rel);
      if (cov.has(keyOf(abs))) continue;
      const old = r.files?.[rel];
      if (old && old[0] === size && old[1] === mtime) continue;
      const large = size > MAX_COPY_BYTES;
      /** @type {Buffer|null} */
      let buf = null;
      if (!large) {
        try {
          buf = readFileSync(abs);
        } catch {
          continue;
        }
      }
      if (buf && old && old[2] != null && hashOf(buf) === old[2]) continue;
      if (large || !buf || (old && old[2] == null)) {
        add(rel, 'changed (large, not diffed)');
        continue;
      }
      if (buf.includes(0)) {
        add(rel, 'changed (binary, not diffed)');
        continue;
      }
      const text = buf.toString('utf8');
      if (!old) {
        const d = fileDiff(label(abs), null, text);
        add(rel, 'new', { added: d.added, removed: d.removed, diff: d.text, ranges: d.ranges, content: text });
      } else {
        const n = text.split('\n').length;
        add(rel, "changed outside the plan's copies; no before", { ranges: [[1, n]], content: text });
      }
    }
    for (const rel of Object.keys(r.files ?? {})) {
      if (nowSet.has(rel) || w.skipped) continue;
      if (cov.has(keyOf(join(root, rel)))) continue;
      add(rel, 'deleted (no before)');
    }
  }
  files.sort((a, b) => a.path.localeCompare(b.path));
  return { files: files.slice(0, MAX_ENTRIES), more: Math.max(0, files.length - MAX_ENTRIES), coverage };
}
