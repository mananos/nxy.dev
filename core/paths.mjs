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
  const dir = gitRoot(cwd);
  if (!dir) return null;
  const dotGit = join(dir, '.git');
  try {
    let gitDir = dotGit;
    if (statSync(dotGit).isFile()) {
      const m = /^gitdir:\s*(.+?)\s*$/m.exec(readFileSync(dotGit, 'utf8'));
      if (!m) return null;
      gitDir = resolve(dir, m[1].trim());
    }
    const head = readFileSync(join(gitDir, 'HEAD'), 'utf8').trim();
    const ref = /^ref:\s*refs\/heads\/(.+)$/.exec(head);
    return ref ? ref[1] : head.slice(0, 7);
  } catch {
    return null;
  }
}
