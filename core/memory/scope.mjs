// @ts-check
/**
 * Memory scopes: global -> project -> area.
 *
 * One store per user holds everything; the scope is a field, not a separate database (the same
 * shape engram landed on, and for the same reason: a single index is what makes one search answer
 * across scopes). What nxy adds on top is `area`.
 *
 *  - `global`  you, everywhere. Travels between machines. Never exported to a repo.
 *  - `project` this repository. Can be committed and shared with the team.
 *  - `area`    a subtree of the repository (`api/auth/**`). In a monorepo, the index for `api/`
 *              must not drag in everything `web/` ever learned.
 *
 * `project` is keyed on the normalised git remote rather than the path, so the same repo is the
 * same project across clones, machines and worktrees. Read from `.git/config` directly — no spawn,
 * same approach `gitBranch` already takes in core/paths.mjs.
 */
import { existsSync, readFileSync, statSync } from 'node:fs';
import { dirname, join, resolve, sep } from 'node:path';

/** @typedef {'global'|'project'|'area'} Scope */

/** Walks up to the repository root (the first directory holding `.git`), or null. */
export function repoRoot(cwd) {
  let dir = resolve(cwd);
  for (let i = 0; i < 64; i++) {
    if (existsSync(join(dir, '.git'))) return dir;
    const parent = dirname(dir);
    if (parent === dir) return null;
    dir = parent;
  }
  return null;
}

/** The `.git` directory itself, following a worktree pointer file when there is one. */
function gitDir(root) {
  const dot = join(root, '.git');
  try {
    if (!statSync(dot).isFile()) return dot;
    const m = /^gitdir:\s*(.+?)\s*$/m.exec(readFileSync(dot, 'utf8'));
    return m ? resolve(root, m[1].trim()) : null;
  } catch {
    return null;
  }
}

/**
 * Normalises a git remote URL into a stable project key.
 *
 * `git@github.com:mananos/nxy.dev.git`, `https://github.com/mananos/nxy.dev.git` and
 * `ssh://git@github.com/mananos/nxy.dev` must all produce `github.com/mananos/nxy.dev`, or the
 * same repository would hold different memories depending on how it was cloned.
 * @param {string} url
 */
export function normalizeRemote(url) {
  let s = (url || '').trim();
  if (!s) return null;
  s = s.replace(/\.git$/i, '');
  s = s.replace(/^[a-z+]+:\/\//i, '');            // scheme
  s = s.replace(/^[^@/]+@/, '');                   // user@
  s = s.replace(/:(?=[^\d])/, '/');                // scp-style host:path -> host/path
  s = s.replace(/:\d+\//, '/');                    // host:port/ -> host/
  s = s.replace(/\/+$/, '').replace(/\\/g, '/');
  return s.toLowerCase() || null;
}

/** First remote URL in `.git/config` (`origin` wins when present). */
export function gitRemote(cwd) {
  const root = repoRoot(cwd);
  if (!root) return null;
  const gd = gitDir(root);
  if (!gd) return null;
  let text;
  try {
    text = readFileSync(join(gd, 'config'), 'utf8');
  } catch {
    return null;
  }
  /** @type {Record<string, string>} */
  const remotes = {};
  let current = null;
  for (const raw of text.split('\n')) {
    const line = raw.trim();
    const header = /^\[remote\s+"([^"]+)"\]$/.exec(line);
    if (header) {
      current = header[1];
      continue;
    }
    if (/^\[/.test(line)) {
      current = null;
      continue;
    }
    const kv = /^url\s*=\s*(.+)$/.exec(line);
    if (current && kv) remotes[current] = kv[1].trim();
  }
  return remotes.origin || Object.values(remotes)[0] || null;
}

/**
 * The project key for a working directory.
 *
 * Falls back to the absolute path when there is no remote, and says so: a path key still works on
 * one machine, it just will not follow the repo to another one. Callers surface `warning` rather
 * than pretending the memory is portable.
 * @param {string} cwd
 */
export function projectKey(cwd) {
  const remote = gitRemote(cwd);
  const normalized = remote ? normalizeRemote(remote) : null;
  if (normalized) return { key: normalized, from: 'remote', warning: null };
  const root = repoRoot(cwd);
  if (root) {
    return {
      key: `path:${root.replace(/\\/g, '/').toLowerCase()}`,
      from: 'path',
      warning: 'this repo has no git remote: memories are keyed on its path and will not follow it to another machine',
    };
  }
  return {
    key: `path:${resolve(cwd).replace(/\\/g, '/').toLowerCase()}`,
    from: 'cwd',
    warning: 'not a git repository: memories are keyed on this directory',
  };
}

/**
 * The area for a path inside the repo: its top two directory levels, which is the granularity a
 * monorepo actually splits on (`api/auth`, `services/billing`) without turning every folder into
 * its own scope.
 * @param {string} cwd
 * @param {string} filePath  absolute or repo-relative
 */
export function areaFor(cwd, filePath) {
  const root = repoRoot(cwd);
  if (!root || !filePath) return null;
  const abs = resolve(root, filePath);
  const rel = abs.slice(root.length).replace(/^[\\/]+/, '').split(sep).join('/');
  if (!rel || rel.startsWith('..')) return null;
  const parts = rel.split('/').filter(Boolean);
  if (parts.length <= 1) return null; // a file at the repo root belongs to the project, not an area
  return parts.slice(0, Math.min(2, parts.length - 1)).join('/');
}

/**
 * Which scopes a query should look at, in the order they matter.
 * Area memories are the most specific, so they rank first when an area is in play.
 * @param {{area?: string|null}} [opts]
 * @returns {Scope[]}
 */
export function activeScopes(opts = {}) {
  return opts.area ? ['area', 'project', 'global'] : ['project', 'global'];
}
