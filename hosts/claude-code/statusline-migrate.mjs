// @ts-check
/**
 * SessionStart migration for the statusline. Two jobs, both scoped to nxy's own files:
 *  1. keep `~/.nxy/statusline.mjs` (the shim) at the current generation, silently;
 *  2. rewrite a `statusLine.command` that points straight at a versioned nxy cache path
 *     (what nxy 0.1.x wrote) to the shim, with a settings backup and a one-time notice.
 * A statusLine that does not reference nxy is never touched. Dev checkouts and
 * `--plugin-dir` sessions (no versioned cache layout) never migrate, so they cannot repin
 * the user's shim or settings to a checkout.
 */
import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { PLUGIN_ROOT, ensureDir, nxyUserDir } from '../../core/paths.mjs';
import { claudeUserSettingsPath } from './settings.mjs';
import { SHIM_GENERATION, applyStatusLine, shimSource, versionsDirFor } from './entries/statusline-setup.mjs';

const fwd = (p) => p.replace(/\\/g, '/');
const TAIL = String.raw`\/cache\/[^/"']+\/nxy\/\d+\.\d+\.\d+[^/"']*\/(?:scripts\/metrics|hosts\/claude-code)\/statusline\.mjs`;
// Only the plain `node <path>` form: quoted (spaces allowed inside) or bare, nothing after it.
const DIRECT = new RegExp(`^node\\s+(?:"[^"]*${TAIL}"|'[^']*${TAIL}'|[^\\s"'|&;<>]*${TAIL})$`);

/**
 * True when the command is exactly `node <path>` for a statusline script inside a versioned
 * nxy cache dir. Pipes, `&&`, extra args or wrappers do not match.
 * @param {unknown} command
 */
export function isNxyDirectStatusline(command) {
  return typeof command === 'string' && DIRECT.test(fwd(command).trim());
}

/** @param {string} shimPath */
function shimIsCurrent(shimPath) {
  try {
    const first = readFileSync(shimPath, 'utf8').split('\n', 1)[0];
    return first.includes(`generation=${SHIM_GENERATION}`);
  } catch {
    return false;
  }
}

/**
 * @param {{ root?: string, settingsPath?: string, shimPath?: string }} [o]
 * @returns {string | null} one-time notice, or null when nothing user-visible happened
 */
export function migrateStatusline({ root = PLUGIN_ROOT, settingsPath = claudeUserSettingsPath(), shimPath = join(nxyUserDir(), 'statusline.mjs') } = {}) {
  if (!versionsDirFor(root)) return null;

  const shimExists = existsSync(shimPath);
  if (shimExists && !shimIsCurrent(shimPath)) {
    ensureDir(dirname(shimPath));
    writeFileSync(shimPath, shimSource(root));
  }

  let settings;
  try {
    settings = JSON.parse(readFileSync(settingsPath, 'utf8'));
  } catch {
    return null;
  }
  const sl = settings?.statusLine;
  if (!sl || typeof sl !== 'object' || !isNxyDirectStatusline(sl.command)) return null;

  const old = sl.command;
  const command = `node "${fwd(shimPath)}"`;
  const { backup } = applyStatusLine({ root, settingsPath, shimPath, patch: { statusLine: { ...sl, command } } });
  return [
    `nxy: your statusline command pointed at a versioned plugin path (${old}), which stops working on every update.`,
    `It now runs ${command}, which always picks the installed nxy.`,
    backup ? `Backup of settings.json: ${backup}.` : '',
    'Nothing to do; restart Claude Code to see it.',
  ].filter(Boolean).join(' ');
}
