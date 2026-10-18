// @ts-check
import { existsSync, mkdirSync, readFileSync, statSync } from 'node:fs';
import { homedir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

/** Plugin root (the directory that contains `.claude-plugin/plugin.json`). */
export const PLUGIN_ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..');

/** User-level nxy directory (`~/.nxy`; `NXY_HOME` overrides it — tests use a temp dir). */
export function nxyUserDir() {
  return process.env.NXY_HOME || join(homedir(), '.nxy');
}

/**
 * Project-level nxy directory (`<git root>/.nxy`, else `<cwd>/.nxy`). Holds `config.json`, which is meant to be
 * committed: it is how a repo shares its nxy conventions with everyone who clones it.
 */
export function nxyProjectDir(cwd) {
  return join(gitRoot(cwd) ?? cwd, '.nxy');
}

/** Filesystem-safe directory name for a task key or branch (null and '(no branch)' → `no-branch`). */
export const slug = (branch) => {
  const s = String(branch && branch !== '(no branch)' ? branch : 'no-branch').replace(/[^A-Za-z0-9._-]+/g, '_');
  // '.' and '..' (or only dots) would resolve to the parent dir and reach a recursive rm.
  return /^\.+$/.test(s) ? `_${s.replace(/\./g, '%')}` : s;
};

/**
 * Per-checkout runtime state (`<git root>/.nxy/local`): ledgers and caches that every clone
 * regenerates on its own. Ignored by git — shareable config never belongs here.
 */
export function nxyRuntimeDir(cwd) {
  return join(nxyProjectDir(cwd), 'local');
}

/**
 * Makes a path usable by Node on the current platform. On Windows, an MSYS/Git Bash path
 * such as `/c/Users/x` becomes `C:/Users/x` (Node would otherwise read it as `C:\c\Users\x`).
 * Any other path is returned unchanged.
 * @param {string} p
 */
export function toNativePath(p) {
  if (process.platform === 'win32') {
    const m = /^\/([A-Za-z])(\/.*)?$/.exec(p || '');
    if (m) return `${m[1].toUpperCase()}:${m[2] || '/'}`;
  }
  return p;
}

/** Generated bundles, lockfiles, dumps: recorded as changed, not copied or diffed (baseline and git review). */
export const MAX_COPY_BYTES = 2 * 1024 * 1024;

/** Creates the directory (recursively) if it does not exist and returns it. */
export function ensureDir(dir) {
  if (!existsSync(dir)) mkdirSync(dir, { recursive: true });
  return dir;
}

/**
 * Root of the git checkout that holds `cwd`: the first ancestor (or itself) with a `.git`
 * (directory or worktree pointer file). Not a repo → null.
 * @param {string} cwd
 * @returns {string|null}
 */
export function gitRoot(cwd) {
  let dir = resolve(cwd);
  for (let i = 0; i < 64; i++) {
    if (existsSync(join(dir, '.git'))) return dir;
    const parent = dirname(dir);
    if (parent === dir) return null;
    dir = parent;
  }
  return null;
}

/**
 * Current git branch without spawning git: finds the checkout with `gitRoot`, reads HEAD.
 * Detached HEAD → short sha; not a repo → null.
 * @param {string} cwd
 * @returns {string|null}
 */
export function gitBranch(cwd) {
  const gitDir = gitDirOf(gitRoot(cwd));
  if (!gitDir) return null;
  try {
    const head = readFileSync(join(gitDir, 'HEAD'), 'utf8').trim();
    const ref = /^ref:\s*refs\/heads\/(.+)$/.exec(head);
    return ref ? ref[1] : head.slice(0, 7);
  } catch {
    return null;
  }
}

/**
 * The git dir of a checkout root: `<root>/.git`, or the target of a worktree's `.git` pointer file.
 * @param {string|null} root
 * @returns {string|null}
 */
function gitDirOf(root) {
  if (!root) return null;
  const dotGit = join(root, '.git');
  try {
    if (statSync(dotGit).isFile()) {
      const m = /^gitdir:\s*(.+?)\s*$/m.exec(readFileSync(dotGit, 'utf8'));
      return m ? resolve(root, m[1].trim()) : null;
    }
    return dotGit;
  } catch {
    return null;
  }
}

/**
 * Whether a local branch exists, read from the refs on disk (never spawns git). Worktrees share refs through
 * `commondir`. Not a repo or unreadable → true (never claim a branch is gone without evidence).
 * @param {string} cwd
 * @param {string} branch
 * @returns {boolean}
 */
export function branchExists(cwd, branch) {
  const gitDir = gitDirOf(gitRoot(cwd));
  if (!gitDir) return true;
  try {
    let common = gitDir;
    try {
      common = resolve(gitDir, readFileSync(join(gitDir, 'commondir'), 'utf8').trim());
    } catch { /* not a linked worktree */ }
    if (existsSync(join(common, 'refs', 'heads', ...branch.split('/')))) return true;
    try {
      const packed = readFileSync(join(common, 'packed-refs'), 'utf8');
      const want = ` refs/heads/${branch}`;
      if (packed.split(/\r?\n/).some((l) => !l.startsWith('#') && !l.startsWith('^') && l.slice(l.indexOf(' ')) === want)) return true;
    } catch { /* no packed-refs */ }
    return false;
  } catch {
    return true;
  }
}
