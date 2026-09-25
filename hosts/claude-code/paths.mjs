// @ts-check
/**
 * Where Claude Code keeps the files nxy reads. Everything host-specific about locating
 * transcripts lives here; `core/paths.mjs` knows nothing about Claude Code.
 */
import { homedir } from 'node:os';
import { join } from 'node:path';

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
