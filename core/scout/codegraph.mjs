// @ts-check
/**
 * codegraph as an optional engine, detected and guided the same way rtk is for the filter.
 *
 * Two rules from the roadmap (temas 3 y 8) shape this file:
 *  - codegraph is only ever invoked *inside the scout*, never in the main thread. Its payload is
 *    dense (its own benchmark reports ~80% more residual context when used directly); keeping it
 *    in a subagent that dies is exactly what makes it worth using.
 *  - the payload is capped and the fallback is declared. If codegraph is missing, does not know
 *    the language, is slow or answers with more than we agreed to read, the scout falls back to
 *    the repo map and says so. A silent degradation would be a wrong `path:line`, not a slow one.
 */
import { spawnSync } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';
import { homedir } from 'node:os';
import { dirname, join } from 'node:path';

/** Install locations to try when codegraph is not on PATH (same rationale as rtk's). */
export function knownCodegraphLocations(env = process.env, platform = process.platform) {
  const home = env.HOME || env.USERPROFILE || homedir();
  const out = [];
  if (platform === 'win32') {
    const local = env.LOCALAPPDATA || join(home, 'AppData', 'Local');
    // Official install.ps1: bundle in CODEGRAPH_INSTALL_DIR or %LOCALAPPDATA%\codegraph.
    out.push(join(env.CODEGRAPH_INSTALL_DIR || join(local, 'codegraph'), 'current', 'bin', 'codegraph.cmd'));
    out.push(join(local, 'Microsoft', 'WinGet', 'Links', 'codegraph.exe'));
    out.push(join(home, '.local', 'bin', 'codegraph.exe'), join(home, '.cargo', 'bin', 'codegraph.exe'));
    out.push(join(home, 'AppData', 'Roaming', 'npm', 'codegraph.cmd'));
  } else {
    // Official install.sh: symlink in CODEGRAPH_BIN_DIR (default ~/.local/bin), bundle in ~/.codegraph.
    if (env.CODEGRAPH_BIN_DIR) out.push(join(env.CODEGRAPH_BIN_DIR, 'codegraph'));
    out.push('/usr/local/bin/codegraph', '/usr/bin/codegraph', '/opt/homebrew/bin/codegraph');
    out.push(join(home, '.local', 'bin', 'codegraph'), join(home, '.cargo', 'bin', 'codegraph'));
    out.push(join(env.CODEGRAPH_INSTALL_DIR || join(home, '.codegraph'), 'current', 'bin', 'codegraph'));
  }
  return out;
}

/**
 * Turns a codegraph binary into something spawnSync can run without a shell.
 *
 * On Windows, Node cannot spawn a `.cmd`/`.bat` without `shell: true` (EINVAL), and we must not use
 * a shell: `explore` passes the user's free text as an argument and cmd.exe would interpret
 * `& | % ^ <>`. So we read the shim and run its node script directly.
 *
 * @param {string} bin
 * @param {{platform?: NodeJS.Platform, env?: NodeJS.ProcessEnv}} [opts]
 * @returns {{file: string, prefix: string[]}|null}
 */
export function resolveLauncher(bin, { platform = process.platform } = {}) {
  if (platform !== 'win32' || !/\.(cmd|bat)$/i.test(bin)) return { file: bin, prefix: [] };
  let text;
  try {
    text = readFileSync(bin, 'utf8');
  } catch {
    return null;
  }
  const dir = dirname(bin);
  // Quoted tokens rooted at the shim's own dir: `%~dp0` (official) or `%dp0%` (npm shims).
  const rooted = (/** @type {RegExp} */ ext) => {
    for (const m of text.matchAll(/"((?:%~dp0|%dp0%)[^"]*)"/gi)) {
      if (!ext.test(m[1])) continue;
      const rest = m[1].replace(/^(?:%~dp0|%dp0%)/i, '').replace(/^[\\/]+/, '').replace(/\\/g, '/');
      return join(dir, rest);
    }
    return null;
  };
  const js = rooted(/\.js$/i);
  if (!js || !existsSync(js)) return null;
  const node = rooted(/node\.exe$/i);
  return { file: node && existsSync(node) ? node : process.execPath, prefix: [js] };
}

/** @param {{file: string, prefix: string[]}} launcher */
function tryVersion(launcher, env = process.env) {
  try {
    const r = spawnSync(launcher.file, [...launcher.prefix, '--version'], { encoding: 'utf8', timeout: 4000, env, windowsHide: true });
    if (r.status !== 0 || !r.stdout) return null;
    return r.stdout.trim().split('\n')[0];
  } catch {
    return null;
  }
}

/** Windows: spawnSync('codegraph') does not resolve PATHEXT, so walk PATH for .exe then .cmd. */
function findOnWindowsPath(env) {
  for (const dir of String(env.PATH || env.Path || '').split(';')) {
    if (!dir) continue;
    for (const name of ['codegraph.exe', 'codegraph.cmd']) {
      const full = join(dir, name);
      if (existsSync(full)) return full;
    }
  }
  return null;
}

/**
 * Finds codegraph on PATH or in a known install dir.
 * @returns {{path: string, version: string, launcher: {file: string, prefix: string[]}}|null}
 */
export function findCodegraph(env = process.env, platform = process.platform) {
  /** @param {string} bin */
  const attempt = (bin) => {
    const launcher = resolveLauncher(bin, { platform, env });
    if (!launcher) return null;
    const version = tryVersion(launcher, env);
    return version === null ? null : { version, launcher };
  };
  if (env.NXY_CODEGRAPH_PATH) {
    const a = attempt(env.NXY_CODEGRAPH_PATH);
    return a ? { path: env.NXY_CODEGRAPH_PATH, ...a } : null;
  }
  const pathHit = platform === 'win32' ? findOnWindowsPath(env) : 'codegraph';
  if (pathHit) {
    const a = attempt(pathHit);
    if (a) return { path: 'codegraph', ...a };
  }
  for (const bin of knownCodegraphLocations(env, platform)) {
    if (!existsSync(bin)) continue;
    const a = attempt(bin);
    if (a) return { path: bin.replace(/\\/g, '/'), ...a };
  }
  return null;
}

/**
 * True when this repo has a codegraph index to query (otherwise `explore` has nothing to say).
 *
 * Honours `CODEGRAPH_DIR`, which matters for the exact setup this plugin targets: codegraph's own
 * docs say not to share one `.codegraph/` between Windows and WSL (the SQLite lock does not survive
 * the filesystem boundary), and the fix is a distinct dir name on one side. Hard-coding
 * `.codegraph` would silently report "no index" on whichever side was renamed.
 */
export function hasIndex(cwd, env = process.env) {
  const names = [env.CODEGRAPH_DIR, '.codegraph'].filter(Boolean);
  return names.some((n) => existsSync(join(cwd, /** @type {string} */ (n))));
}

/**
 * Runs `codegraph explore` for a question, capped in both time and payload.
 *
 * Never throws: every failure path returns `{ok: false, reason}` so the caller can declare the
 * fallback instead of losing the answer. `reason` is what ends up in the scout's report and in
 * the ledger, which is how we learn whether codegraph is earning its place without anyone
 * running an experiment.
 *
 * @param {string} cwd
 * @param {string} question
 * @param {{bin?: string, maxChars?: number, timeoutMs?: number, env?: NodeJS.ProcessEnv, platform?: NodeJS.Platform}} [opts]
 */
export function explore(cwd, question, opts = {}) {
  const env = opts.env || process.env;
  const platform = opts.platform || process.platform;
  const maxChars = opts.maxChars ?? 24_000; // ~6k tokens: dense, but it dies with the scout
  const timeoutMs = opts.timeoutMs ?? 20_000;
  const launcher = opts.bin ? resolveLauncher(opts.bin, { platform, env }) : findCodegraph(env, platform)?.launcher;
  if (!launcher) return { ok: false, reason: 'not-installed', text: '', truncated: false, ms: 0 };
  if (!hasIndex(cwd, env)) return { ok: false, reason: 'no-index', text: '', truncated: false, ms: 0 };
  const started = Date.now();
  let r;
  try {
    // No shell: the question is free text (see resolveLauncher).
    r = spawnSync(launcher.file, [...launcher.prefix, 'explore', question], { cwd, encoding: 'utf8', timeout: timeoutMs, env, windowsHide: true, maxBuffer: 8 * 1024 * 1024 });
  } catch {
    return { ok: false, reason: 'spawn-failed', text: '', truncated: false, ms: Date.now() - started };
  }
  const ms = Date.now() - started;
  if (r.error && /** @type {any} */ (r.error).code === 'ETIMEDOUT') return { ok: false, reason: 'timeout', text: '', truncated: false, ms };
  if (r.status !== 0) return { ok: false, reason: `exit-${r.status}`, text: '', truncated: false, ms };
  const raw = (r.stdout || '').trim();
  if (!raw) return { ok: false, reason: 'empty', text: '', truncated: false, ms };
  const truncated = raw.length > maxChars;
  return { ok: true, reason: 'ok', text: truncated ? raw.slice(0, maxChars) : raw, truncated, ms };
}

/** One-line status for `/nxy:locate --status` and the scout's own report. */
export function describeCodegraph(cwd, env = process.env) {
  const found = findCodegraph(env);
  if (!found) return { available: false, line: 'codegraph:    not installed (scout falls back to the repo map + rg)' };
  if (!hasIndex(cwd, env)) return { available: false, line: `codegraph:    ${found.path} ${found.version} — but this repo has no index; run \`codegraph init\` to enable it` };
  return { available: true, line: `codegraph:    ${found.path} ${found.version} — indexed` };
}
