// @ts-check
import { existsSync, mkdirSync } from 'node:fs';
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

/** Creates the directory (recursively) if it does not exist and returns it. */
export function ensureDir(dir) {
  if (!existsSync(dir)) mkdirSync(dir, { recursive: true });
  return dir;
}
