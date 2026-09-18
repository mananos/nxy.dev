// @ts-check
import { spawnSync } from 'node:child_process';
import { existsSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';
import { ensureDir, nxyUserDir } from '../lib/paths.mjs';

/**
 * @typedef {object} EngineInfo
 * @property {'rtk'|'off'} engine
 * @property {string|null} rtkPath      executable name/path that answered `--version`
 * @property {string|null} rtkVersion
 * @property {boolean} rtkHookDetected  RTK's own PreToolUse hook found in Claude settings
 * @property {boolean} rgAvailable      ripgrep on PATH (RTK needs it for some filters)
 * @property {string} reason            why `engine` ended up as it is
 * @property {number} resolvedAt        epoch ms
 */

function tryVersion(bin, env = process.env) {
  try {
    const r = spawnSync(bin, ['--version'], { encoding: 'utf8', timeout: 3000, windowsHide: true, shell: false, env });
    if (r.status === 0) return (r.stdout || r.stderr || '').trim();
  } catch {
    /* not found */
  }
  return null;
}

/**
 * Well-known install locations, for processes whose PATH predates the install (on Windows a
 * terminal or IDE opened before `winget install` never sees the new PATH).
 * @param {NodeJS.ProcessEnv} env
 */
export function knownRtkLocations(env = process.env) {
  const home = env.HOME || env.USERPROFILE || homedir();
  const out = [];
  if (process.platform === 'win32') {
    const local = env.LOCALAPPDATA || join(home, 'AppData', 'Local');
    out.push(join(local, 'Microsoft', 'WinGet', 'Links', 'rtk.exe'));
    const pkgs = join(local, 'Microsoft', 'WinGet', 'Packages');
    try {
      for (const d of readdirSync(pkgs)) if (d.startsWith('rtk-ai.rtk')) out.push(join(pkgs, d, 'rtk.exe'));
    } catch {
      /* no winget packages dir */
    }
    out.push(join(home, '.local', 'bin', 'rtk.exe'), join(home, '.cargo', 'bin', 'rtk.exe'));
  } else {
    out.push(join(home, '.local', 'bin', 'rtk'), join(home, '.cargo', 'bin', 'rtk'), '/usr/local/bin/rtk', '/opt/homebrew/bin/rtk', '/home/linuxbrew/.linuxbrew/bin/rtk');
  }
  return out;
}

/** Locates the rtk binary: NXY_RTK_PATH → `rtk` on PATH → `rtk.exe` (win32) → known install locations. */
export function findRtk(env = process.env) {
  const candidates = [env.NXY_RTK_PATH, 'rtk', process.platform === 'win32' ? 'rtk.exe' : null].filter(Boolean);
  for (const bin of /** @type {string[]} */ (candidates)) {
    const v = tryVersion(bin, env);
    if (v !== null) return { path: bin, version: v.replace(/^rtk\s+/i, '') };
  }
  for (const bin of knownRtkLocations(env)) {
    if (!existsSync(bin)) continue;
    const v = tryVersion(bin, env);
    if (v !== null) return { path: bin.replace(/\\/g, '/'), version: v.replace(/^rtk\s+/i, '') };
  }
  return null;
}

/** Claude settings files that can carry hooks, in precedence order. */
export function claudeSettingsFiles(cwd) {
  return [
    join(homedir(), '.claude', 'settings.json'),
    join(cwd, '.claude', 'settings.json'),
    join(cwd, '.claude', 'settings.local.json'),
  ];
}

function readJson(path) {
  try {
    return existsSync(path) ? JSON.parse(readFileSync(path, 'utf8')) : null;
  } catch {
    return null;
  }
}

/** True when any PreToolUse hook command in the given settings objects invokes `rtk hook`. */
export function detectRtkHook(settingsObjects) {
  for (const s of settingsObjects) {
    const groups = s?.hooks?.PreToolUse;
    if (!Array.isArray(groups)) continue;
    for (const g of groups) {
      for (const h of g?.hooks || []) {
        if (typeof h?.command === 'string' && /\brtk(\.exe)?\b[^\n]*\bhook\b/.test(h.command)) return true;
      }
    }
  }
  return false;
}

/**
 * @param {{filter: {engine: 'auto'|'rtk'|'off'}}} cfg
 * @param {string} cwd
 * @param {NodeJS.ProcessEnv} [env]
 * @returns {EngineInfo}
 */
export function resolveEngine(cfg, cwd, env = process.env) {
  const rtk = findRtk(env);
  const rtkHookDetected = detectRtkHook(claudeSettingsFiles(cwd).map(readJson));
  const rgAvailable = tryVersion('rg', env) !== null;
  const base = {
    rtkPath: rtk?.path ?? null,
    rtkVersion: rtk?.version ?? null,
    rtkHookDetected,
    rgAvailable,
    resolvedAt: Date.now(),
  };
  const wanted = cfg.filter.engine;
  if (wanted === 'off') return { ...base, engine: 'off', reason: 'config' };
  if (!rtk) return { ...base, engine: 'off', reason: wanted === 'rtk' ? 'rtk-missing' : 'auto-no-rtk' };
  return { ...base, engine: 'rtk', reason: 'rtk-found' };
}

export function engineCachePath() {
  return join(nxyUserDir(), 'cache', 'engine.json');
}

/** @param {EngineInfo} info */
export function writeEngineCache(info) {
  const p = engineCachePath();
  ensureDir(join(nxyUserDir(), 'cache'));
  writeFileSync(p, JSON.stringify(info), 'utf8');
}

/**
 * Cached engine info if still fresh, otherwise null. A positive verdict (rtk found) is
 * trusted for 6h; a negative one only for 2 minutes, so installing rtk — or a session that
 * started with a poorer PATH — never leaves the filter silently off for hours.
 * @returns {EngineInfo|null}
 */
export function readEngineCache(maxAgeMs = 6 * 60 * 60 * 1000, negativeMaxAgeMs = 2 * 60 * 1000) {
  const info = readJson(engineCachePath());
  if (!info || typeof info.resolvedAt !== 'number') return null;
  const ttl = info.engine === 'rtk' || info.reason === 'config' ? maxAgeMs : negativeMaxAgeMs;
  return Date.now() - info.resolvedAt <= ttl ? info : null;
}

/** Cache if fresh, else resolve now and refresh the cache. */
export function engineInfo(cfg, cwd) {
  let info = readEngineCache();
  if (!info) {
    info = resolveEngine(cfg, cwd);
    writeEngineCache(info);
  }
  return info;
}

/** Human-readable status block for `/nxy:filter status`. */
export function describeEngine(info, cfg) {
  const lines = [
    `engine:        ${info.engine} (${info.reason}; config=${cfg.filter.engine})`,
    `rtk:           ${info.rtkPath ? `${info.rtkPath} v${info.rtkVersion}${/[\/]/.test(info.rtkPath) ? '  (not on this process PATH — found in a known install dir; a new terminal will have it on PATH)' : ''}` : 'not found on PATH nor in known install dirs'}`,
    `ripgrep (rg):  ${info.rgAvailable ? 'available' : 'not found (RTK needs it for some filters)'}`,
    `rtk own hook:  ${info.rtkHookDetected ? 'INSTALLED — nxy yields the rewrite to it; run `rtk init --uninstall` (or remove the hook) to let nxy manage it' : 'not installed (good)'}`,
  ];
  if (!info.rtkPath) {
    lines.push(
      'install:       Windows → winget install rtk-ai.rtk   |   Linux/macOS → see https://github.com/rtk-ai/rtk#installation',
      '               Do NOT run `rtk init -g` — nxy installs its own hook.',
    );
  }
  if (!info.rgAvailable) lines.push('install rg:    Windows → winget install BurntSushi.ripgrep.MSVC   |   Linux → apt/dnf install ripgrep');
  return lines.join('\n');
}
