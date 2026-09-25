// @ts-check
/**
 * ripgrep: locating it, and running a capped search with it.
 *
 * Two different parts of nxy need rg and neither owns it — the filter needs to know whether rtk
 * will have it available, and the scout uses it directly as the zero-token way to answer "where
 * does this exact text appear". So it lives here rather than in either one.
 */
import { spawnSync } from 'node:child_process';
import { existsSync, readdirSync } from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';

/** Install locations to try when `rg` is not on PATH. */
export function knownRgLocations(env = process.env) {
  const home = env.HOME || env.USERPROFILE || homedir();
  const out = [];
  if (process.platform === 'win32') {
    const local = env.LOCALAPPDATA || join(home, 'AppData', 'Local');
    out.push(join(local, 'Microsoft', 'WinGet', 'Links', 'rg.exe'));
    const pkgs = join(local, 'Microsoft', 'WinGet', 'Packages');
    try {
      for (const d of readdirSync(pkgs)) {
        if (!d.startsWith('BurntSushi.ripgrep')) continue;
        const pkgDir = join(pkgs, d);
        out.push(join(pkgDir, 'rg.exe'));
        for (const sub of readdirSync(pkgDir)) if (sub.startsWith('ripgrep-')) out.push(join(pkgDir, sub, 'rg.exe'));
      }
    } catch {
      /* no winget packages dir */
    }
    out.push(join(home, '.cargo', 'bin', 'rg.exe'), join(home, 'scoop', 'shims', 'rg.exe'));
  } else {
    out.push('/usr/bin/rg', '/usr/local/bin/rg', '/opt/homebrew/bin/rg', join(home, '.cargo', 'bin', 'rg'), join(home, '.local', 'bin', 'rg'));
  }
  return out;
}

/**
 * Path to a working `rg`, or null. Checks PATH first so a user's own build wins.
 * @returns {string|null}
 */
export function findRg(env = process.env) {
  try {
    const r = spawnSync('rg', ['--version'], { encoding: 'utf8', timeout: 4000, env, windowsHide: true });
    if (r.status === 0) return 'rg';
  } catch {
    /* not on PATH */
  }
  for (const bin of knownRgLocations(env)) {
    if (!existsSync(bin)) continue;
    try {
      const r = spawnSync(bin, ['--version'], { encoding: 'utf8', timeout: 4000, env, windowsHide: true });
      if (r.status === 0) return bin;
    } catch {
      /* try the next one */
    }
  }
  return null;
}

/**
 * Literal search, capped. Returns `path:line:text` rows.
 *
 * Capped on purpose: an uncapped grep is the file dump the scout exists to avoid. When the cap
 * bites, `truncated` says so, which is a signal the query was too vague — worth reporting rather
 * than hiding.
 *
 * @param {string} cwd
 * @param {string} pattern
 * @param {{bin?: string|null, maxRows?: number, fixed?: boolean, globs?: string[], timeoutMs?: number, env?: NodeJS.ProcessEnv}} [opts]
 */
export function rgSearch(cwd, pattern, opts = {}) {
  const env = opts.env || process.env;
  const bin = opts.bin === undefined ? findRg(env) : opts.bin;
  if (!bin) return { ok: false, reason: 'rg-missing', rows: [], truncated: false };
  const maxRows = opts.maxRows ?? 30;
  const args = ['--line-number', '--no-heading', '--color', 'never', '--max-count', '3', '--max-filesize', '1M'];
  if (opts.fixed !== false) args.push('--fixed-strings');
  args.push('--smart-case');
  for (const g of opts.globs || []) args.push('--glob', g);
  args.push('--', pattern, '.');
  let r;
  try {
    r = spawnSync(bin, args, { cwd, encoding: 'utf8', timeout: opts.timeoutMs ?? 10_000, env, windowsHide: true, maxBuffer: 8 * 1024 * 1024 });
  } catch {
    return { ok: false, reason: 'spawn-failed', rows: [], truncated: false };
  }
  // rg exits 1 when there are simply no matches: that is an answer, not a failure.
  if (r.status !== 0 && r.status !== 1) return { ok: false, reason: `exit-${r.status}`, rows: [], truncated: false };
  /** @type {{path: string, line: number, text: string}[]} */
  const rows = [];
  for (const raw of (r.stdout || '').split('\n')) {
    if (!raw.trim()) continue;
    const m = /^(.+?):(\d+):(.*)$/.exec(raw);
    if (!m) continue;
    rows.push({ path: m[1].replace(/\\/g, '/').replace(/^\.\//, ''), line: Number(m[2]), text: m[3].trim().slice(0, 200) });
    if (rows.length >= maxRows) break;
  }
  return { ok: true, reason: 'ok', rows, truncated: rows.length >= maxRows };
}
