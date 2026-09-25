// @ts-check
import { spawnSync } from 'node:child_process';
import { mkdirSync, statSync, utimesSync, writeFileSync } from 'node:fs';
import { homedir } from 'node:os';
import { dirname, join } from 'node:path';
import { splitTopLevel } from '../shell.mjs';

/**
 * rtk's warning marker (`.hook_warn_last`) lives under Rust's `dirs::data_local_dir()`:
 * %LOCALAPPDATA% on Windows, $XDG_DATA_HOME or ~/.local/share on Linux, ~/Library/Application Support on macOS.
 * @param {NodeJS.ProcessEnv} [env]
 * @param {NodeJS.Platform} [platform]
 */
export function rtkWarnMarkerPath(env = process.env, platform = process.platform) {
  const home = env.HOME || env.USERPROFILE || homedir();
  let dataDir;
  if (platform === 'win32') dataDir = env.LOCALAPPDATA || join(home, 'AppData', 'Local');
  else if (platform === 'darwin') dataDir = join(home, 'Library', 'Application Support');
  else dataDir = env.XDG_DATA_HOME || join(home, '.local', 'share');
  return join(dataDir, 'rtk', '.hook_warn_last');
}

const WARN_WINDOW_MS = 23 * 60 * 60 * 1000;

/**
 * rtk prints `[rtk] /!\ No hook installed — run rtk init -g` on stderr and rate-limits it to
 * once a day through the mtime of an empty marker file. On Windows that never works: it rewrites
 * 0 bytes over a 0-byte file and NTFS does not bump the mtime, so the line lands in EVERY
 * filtered result the model reads (~25 tokens each) — and the advice is wrong for nxy anyway,
 * which installs its own hook. We start rtk's window ourselves: touch the marker when it is
 * missing or older than 23 h. Cost: one stat per Bash call, one write per day.
 * @param {NodeJS.ProcessEnv} [env]
 * @param {number} [now]
 * @returns {boolean} true when the marker was touched
 */
export function silenceRtkHookWarning(env = process.env, now = Date.now()) {
  try {
    const p = rtkWarnMarkerPath(env);
    const st = statSync(p, { throwIfNoEntry: false });
    if (st && now - st.mtimeMs < WARN_WINDOW_MS) return false;
    mkdirSync(dirname(p), { recursive: true });
    if (!st) writeFileSync(p, '');
    const d = new Date(now);
    utimesSync(p, d, d);
    return true;
  } catch {
    return false; // best effort: a read-only data dir must never block the hook
  }
}

/**
 * Asks RTK for the filtered equivalent of a command.
 * Exit codes (rtk `rewrite`): 0 rewritten+allow · 1 no equivalent · 2 deny · 3 rewritten but ask.
 * Always argv-based (`shell: false`) so the original string is never re-quoted by a shell.
 * @param {string} rtkPath
 * @param {string} cmd
 * @returns {{code: number, stdout: string}}
 */
export function rtkRewrite(rtkPath, cmd) {
  const r = spawnSync(rtkPath, ['rewrite', cmd], {
    encoding: 'utf8', timeout: 2000, windowsHide: true, shell: false,
  });
  if (r.error) throw r.error;
  return { code: r.status ?? -1, stdout: r.stdout || '' };
}

/**
 * RTK's own savings ledger (`rtk gain`). Best effort: returns null when unavailable.
 * @param {string} rtkPath
 * @param {string[]} [extraArgs]
 * @param {string} [cwd]  what `--project` filters by (rtk uses its own cwd)
 */
export function rtkGain(rtkPath, extraArgs = ['--all'], cwd = process.cwd()) {
  try {
    const r = spawnSync(rtkPath, ['gain', ...extraArgs, '--format', 'json'], {
      encoding: 'utf8', timeout: 10000, windowsHide: true, shell: false, cwd,
    });
    if (r.status !== 0 || !r.stdout) return null;
    return JSON.parse(r.stdout);
  } catch {
    return null;
  }
}

/**
 * Spellings RTK's `rewrite` does not recognise but whose filtered form it does support.
 * Applied only to the string we ASK rtk about; if rtk declines, the original runs untouched.
 *   ./mvnw.cmd test   → mvnw.cmd test   (rtk mvn uses the wrapper when `mvn` is absent)
 *   .\gradlew.bat test → gradlew.bat test
 *   npm test / npm t  → npm run test     (rtk npm = npm run)
 *   pnpm test         → pnpm run test
 * Matches at the start of every top-level segment, after optional VAR=value prefixes.
 * @param {string} cmd
 */
export function normalizeForRtk(cmd) {
  const SEG = /(^|&&\s*|\|\|\s*|;\s*)((?:[A-Za-z_][A-Za-z0-9_]*=(?:"[^"]*"|'[^']*'|\S+)\s+)*)/.source;
  return cmd
    .replace(new RegExp(`${SEG}\\.[\\\\/](mvnw(?:\\.cmd)?|gradlew(?:\\.bat)?)(?=\\s|$)`, 'g'), '$1$2$3')
    .replace(new RegExp(`${SEG}(npm|pnpm)\\s+(?:test|t)(?=\\s|$)`, 'g'), '$1$2$3 run test')
    // pnpm lets you omit `run`; rtk only knows the `run` form (`pnpm build` → `pnpm run build`)
    .replace(new RegExp(`${SEG}pnpm\\s+(build|lint|start|dev|check|typecheck|format)(?=\\s|$)`, 'g'), '$1$2pnpm run $3');
}

/** True when the command already starts with rtk — bare (`rtk …`, `rtk.exe …`) or by absolute path (`"C:/…/rtk.exe" …`). */
export function isRtkInvocation(cmd) {
  return RTK_HEAD.test(cmd || '');
}

/** rtk — bare, `.exe` or absolute path — at the start of a segment, after optional VAR=value prefixes. */
const RTK_HEAD = /^\s*(?:[A-Za-z_][A-Za-z0-9_]*=(?:"[^"]*"|'[^']*'|\S+)\s+)*"?(?:[^"\s]*[\\/])?rtk(?:\.exe)?"?\s/i;

/** True when any top-level segment runs through rtk (`cd x && export A=1 && rtk mvn test`). */
export function usesRtk(cmd) {
  return splitTopLevel(cmd || '').some(isRtkInvocation);
}

/**
 * RTK's `rewrite` always emits a bare `rtk` prefix. When rtk was located by an absolute
 * path (NXY_RTK_PATH) the tool shell may not have it on PATH, so pin the path instead.
 * @param {string} rewritten
 * @param {string} rtkPath
 */
export function pinRtkPath(rewritten, rtkPath) {
  if (!rtkPath || /^rtk(\.exe)?$/i.test(rtkPath)) return rewritten;
  const quoted = `"${rtkPath.replace(/\\/g, '/')}"`;
  // `rtk` can follow the start, an operator, or a VAR=value prefix (`JAVA_HOME="…" rtk mvn test`).
  return rewritten.replace(/(^|&&\s*|\|\|\s*|;\s*|\|\s*|[A-Za-z_][A-Za-z0-9_]*=(?:"[^"]*"|'[^']*'|\S+)\s+)rtk(\.exe)?(?=\s)/g, `$1${quoted}`);
}

/** Extracts the recall hash RTK appends to filtered output (`rtk recall <hash>`), if present. */
export function extractRecallHash(text) {
  const m = /rtk recall\s+([A-Za-z0-9_-]{4,})/.exec(text || '');
  return m ? m[1] : null;
}
