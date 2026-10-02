// @ts-check
/**
 * The repos one task touches (1.0.1). The session's own repo is the primary; the plan can declare
 * others under `### Repos`; a repo edited that the plan did not declare is "undeclared" and is
 * remembered (once per task) in `.nxy/local/plan-extra-repos.json`.
 *
 * Everything here is spawn-free: a repo is found by walking up for `.git` (core/paths gitRoot).
 * Names for display are automatic: the folder name, with parent folders added only when two known
 * repos share it. The user configures nothing.
 *
 * @typedef {{root: string, name?: string, primary?: boolean, git?: boolean, missing?: boolean}} Repo
 */
import { existsSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { basename, dirname, join, resolve, sep } from 'node:path';
import { ensureDir, gitRoot, nxyRuntimeDir, toNativePath } from '../../core/paths.mjs';
import { readMarker } from './plan-approval.mjs';

const extraPath = (cwd) => join(nxyRuntimeDir(cwd), 'plan-extra-repos.json');
const WIN = process.platform === 'win32';

/** Comparison key of a path: absolute, no trailing separator, case-insensitive on Windows. */
const keyOf = (p) => {
  const r = resolve(toNativePath(p)).replace(/[\\/]+$/, '');
  return WIN ? r.toLowerCase() : r;
};
const rootOf = (r) => (typeof r === 'string' ? r : r.root);

/** True when `abs` is `root` or inside it. */
function inside(root, abs) {
  const r = keyOf(root);
  const a = keyOf(abs);
  return a === r || a.startsWith(r + sep) || a.startsWith(r + '/');
}

/** Primary repo of a project dir. */
export function primaryRoot(cwd) {
  return gitRoot(cwd) ?? resolve(cwd);
}

/**
 * The primary plus every repo the plan declares.
 * @param {string} cwd @param {({path: string}|string)[]} declared
 * @returns {Repo[]}
 */
export function resolveRepos(cwd, declared) {
  const root = primaryRoot(cwd);
  /** @type {Repo[]} */
  const out = [{ root, name: basename(root), primary: true, git: gitRoot(cwd) != null }];
  const seen = new Set([keyOf(root)]);
  for (const d of declared || []) {
    const p = typeof d === 'string' ? d : d?.path;
    if (!p) continue;
    const abs = resolve(cwd, toNativePath(p));
    const found = existsSync(abs) ? gitRoot(abs) : null;
    const r = found ?? abs;
    if (seen.has(keyOf(r))) continue;
    seen.add(keyOf(r));
    out.push(found ? { root: r, git: true } : { root: r, git: false, missing: !existsSync(abs) });
  }
  return out;
}

/**
 * The repo a file belongs to: the primary, then a known root (the longest wins), then any git repo
 * found by walking up from the file. Null when it is outside every repo.
 * @param {string} cwd @param {string} file
 * @param {(Repo|string)[]} [repos] known repos (roots or objects)
 * @returns {{root: string, primary: boolean}|null}
 */
export function repoOf(cwd, file, repos = []) {
  const abs = resolve(cwd, toNativePath(file));
  const primary = primaryRoot(cwd);
  let best = null;
  for (const r of [primary, ...repos.map(rootOf)]) {
    if (inside(r, abs) && (!best || keyOf(r).length > keyOf(best).length)) best = r;
  }
  if (best) return { root: best, primary: keyOf(best) === keyOf(primary) };
  let dir = dirname(abs);
  try {
    if (existsSync(abs) && statSync(abs).isDirectory()) dir = abs;
  } catch {
    /* gone */
  }
  const g = gitRoot(dir);
  return g ? { root: g, primary: false } : null;
}

/**
 * Display name of each root: its folder name; roots that share one take parent folders until unique
 * within their group (`clientX/api`, `clientY/api`); the full path when the parents run out.
 * @param {(Repo|string)[]} roots
 * @returns {Map<string, string>} root as given → name
 */
export function repoNames(roots) {
  const list = [...new Set(roots.map(rootOf))];
  const segs = new Map(list.map((r) => [r, resolve(toNativePath(r)).split(/[\\/]+/).filter(Boolean)]));
  const norm = (s) => (WIN ? s.toLowerCase() : s);
  /** @param {string} r */
  const segOf = (r) => segs.get(r) ?? [];
  /** @type {Map<string, string[]>} */
  const groups = new Map();
  for (const r of list) {
    const k = norm(segOf(r).at(-1) ?? r);
    groups.set(k, [...(groups.get(k) || []), r]);
  }
  const names = new Map();
  for (const members of groups.values()) {
    if (members.length === 1) {
      names.set(members[0], segOf(members[0]).at(-1) ?? members[0]);
      continue;
    }
    const max = Math.max(...members.map((m) => segOf(m).length));
    let done = false;
    for (let d = 2; d <= max && !done; d++) {
      const cand = members.map((m) => segOf(m).slice(-d).join('/'));
      if (new Set(cand.map(norm)).size === members.length) {
        members.forEach((m, i) => names.set(m, cand[i]));
        done = true;
      }
    }
    if (!done) members.forEach((m) => names.set(m, resolve(toNativePath(m)).replace(/\\/g, '/')));
  }
  return names;
}

/**
 * How a file is named in the review: `<rel>` in the primary, `<name>/<rel>` elsewhere, the absolute
 * path (forward slashes) when it belongs to no repo.
 * @param {{root: string, primary?: boolean}|null} repo @param {string} abs @param {Map<string, string>} names
 */
export function repoLabel(repo, abs, names) {
  const a = resolve(abs);
  if (!repo) return a.replace(/\\/g, '/');
  const rel = a.slice(resolve(repo.root).length).replace(/^[\\/]+/, '').replace(/\\/g, '/');
  if (repo.primary) return rel;
  const name = names.get(repo.root) ?? basename(repo.root);
  return rel ? `${name}/${rel}` : name;
}

/** Roots edited that the plan did not declare, in order of first sight. */
export function undeclaredRepos(cwd) {
  try {
    const v = JSON.parse(readFileSync(extraPath(cwd), 'utf8'));
    return Array.isArray(v?.roots) ? v.roots.map(String) : [];
  } catch {
    return [];
  }
}

/** Remembers an undeclared root; true only the first time (the "warn once" state). */
export function noteUndeclaredRepo(cwd, root) {
  try {
    const roots = undeclaredRepos(cwd);
    if (roots.some((r) => keyOf(r) === keyOf(root))) return false;
    ensureDir(nxyRuntimeDir(cwd));
    writeFileSync(extraPath(cwd), JSON.stringify({ roots: [...roots, root] }), 'utf8');
    return true;
  } catch {
    return false;
  }
}

/**
 * Every repo the task touches: the primary, the plan's declared repos (from the marker), then the
 * undeclared ones edited so far. Roots, primary first, no duplicates.
 * @param {string} cwd @param {string|null} branch
 * @returns {string[]}
 */
export function touchedRepos(cwd, branch) {
  const marked = readMarker(cwd, branch)?.repos ?? [];
  const out = [];
  const seen = new Set();
  for (const r of [primaryRoot(cwd), ...marked, ...undeclaredRepos(cwd)]) {
    if (seen.has(keyOf(r))) continue;
    seen.add(keyOf(r));
    out.push(r);
  }
  return out;
}

/**
 * The conventions of every repo given: `listMemories` per project key, deduped by title, the first
 * repo (the primary) first so its wording wins.
 * @param {any} db an open memory store @param {string[]} repos roots
 * @param {{limit?: number}} [opts]
 */
export async function conventionsOf(db, repos, opts = {}) {
  const { projectKey } = await import('../../core/memory/scope.mjs');
  const { listMemories } = await import('../../core/memory/store.mjs');
  const seenKeys = new Set();
  const seenTitles = new Set();
  const out = [];
  for (const r of repos) {
    const key = projectKey(r).key;
    if (seenKeys.has(key)) continue;
    seenKeys.add(key);
    for (const m of listMemories(db, { project: key, allAreas: true, type: 'convention', limit: opts.limit ?? 500 })) {
      if (seenTitles.has(m.title)) continue;
      seenTitles.add(m.title);
      out.push(m);
    }
  }
  return out;
}

/** Forgets the undeclared repos (the task is over). */
export function clearUndeclaredRepos(cwd) {
  rmSync(extraPath(cwd), { force: true });
}
