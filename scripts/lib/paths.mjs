// @ts-check
import { existsSync, mkdirSync, readFileSync, statSync } from 'node:fs';
import { homedir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

/** Plugin root (the directory that contains `.claude-plugin/plugin.json`). */
export const PLUGIN_ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..');

/** User-level nxy directory (`~/.nxy`; `NXY_HOME` overrides it — tests use a temp dir). */
export function nxyUserDir() {
  return process.env.NXY_HOME || join(homedir(), '.nxy');
}

/** Project-level nxy directory (`<cwd>/.nxy`). */
export function nxyProjectDir(cwd) {
  return join(cwd, '.nxy');
}

/** Claude Code's transcript root (`~/.claude/projects`) unless overridden. */
export function claudeProjectsDir(override) {
  return override || join(homedir(), '.claude', 'projects');
}

/**
 * Claude Code derives the per-project folder name from the working directory by
 * replacing every non-alphanumeric character with `-`.
 * @param {string} cwd
 */
export function projectSlug(cwd) {
  return cwd.replace(/[^A-Za-z0-9]/g, '-');
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

/** Creates the directory (recursively) if it does not exist and returns it. */
export function ensureDir(dir) {
  if (!existsSync(dir)) mkdirSync(dir, { recursive: true });
  return dir;
}

/**
 * Current git branch without spawning git: walks up from `cwd` to the first `.git` (directory
 * or worktree pointer file), reads HEAD. Detached HEAD → short sha; not a repo → null.
 * @param {string} cwd
 * @returns {string|null}
 */
export function gitBranch(cwd) {
  let dir = resolve(cwd);
  for (let i = 0; i < 64; i++) {
    const dotGit = join(dir, '.git');
    if (existsSync(dotGit)) {
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
    const parent = dirname(dir);
    if (parent === dir) return null;
    dir = parent;
  }
  return null;
}
