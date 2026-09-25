// @ts-check
/**
 * Claude Code's settings files. The core filter only knows "some settings objects may
 * carry hooks"; which files those are, and where they live, is host knowledge.
 */
import { homedir } from 'node:os';
import { join } from 'node:path';

/** Settings files that can carry hooks, in precedence order. */
export function claudeSettingsFiles(cwd) {
  return [
    join(homedir(), '.claude', 'settings.json'),
    join(cwd, '.claude', 'settings.json'),
    join(cwd, '.claude', 'settings.local.json'),
  ];
}

/** The user-level settings file — the only one `/nxy:statusline --apply` writes to. */
export function claudeUserSettingsPath() {
  return join(homedir(), '.claude', 'settings.json');
}
