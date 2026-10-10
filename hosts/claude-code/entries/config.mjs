#!/usr/bin/env node
// @ts-check
/**
 * What the panel's Config tab writes, one action per process (the Mod cannot touch files itself).
 *
 * Usage (all take --cwd <dir>):
 *   config.mjs set --scope user|repo --key <fieldId> --json <value>
 *                                one JSON line: {ok, path, bytes, config} (or {ok:false, reason}, exit 1)
 *   config.mjs tools             one JSON line: the installed nxy version and rtk, rg, codegraph
 *   config.mjs cache-ttl 1h|off  writes or removes promptCacheTtl in ~/.claude/settings.json (backup first)
 *   config.mjs statusline install|remove
 *   config.mjs update            `claude plugin update nxy@nxy-dev` (restart Claude Code afterwards)
 *
 * Fail-open: any error prints a short message and exits 1; nothing is left half written.
 */
import { spawnSync } from 'node:child_process';
import { copyFileSync, existsSync, readFileSync, readdirSync, renameSync, statSync, unlinkSync, writeFileSync } from 'node:fs';
import { basename, dirname, join } from 'node:path';
import { loadConfig, projectConfigPath } from '../../../core/config.mjs';
import { setPath, validate, FIELDS } from '../../../core/configedit.mjs';
import { resolveEngine, writeEngineCache } from '../../../core/filter/engine.mjs';
import { parseArgs } from '../../../core/format.mjs';
import { PLUGIN_ROOT, ensureDir, nxyUserDir, toNativePath } from '../../../core/paths.mjs';
import { findCodegraph } from '../../../core/scout/codegraph.mjs';
import { configView } from '../config-view.mjs';
import { claudeMissing } from '../update-check.mjs';
import { claudeSettingsFiles, claudeUserSettingsPath } from '../settings.mjs';
import { applyStatusLine } from './statusline-setup.mjs';

const KEEP_BACKUPS = 3;
const BACKUP_TAG = 'bak-nxy-';
const fwd = (/** @type {string} */ p) => p.replace(/\\/g, '/');

/** Writes `text` to `path` through a temp file in the same directory; no temp is left behind on failure. */
function writeAtomic(path, text) {
  ensureDir(dirname(path));
  const tmp = join(dirname(path), `.${basename(path)}.tmp-${process.pid}`);
  try {
    writeFileSync(tmp, text);
    renameSync(tmp, path);
  } catch (err) {
    try {
      if (existsSync(tmp)) unlinkSync(tmp);
    } catch {
      /* nothing more to do */
    }
    throw err;
  }
}

/** Parses a JSON object file: {data, exists}; throws on a broken file. */
function readObject(path) {
  if (!existsSync(path)) return { data: {}, exists: false };
  const data = JSON.parse(readFileSync(path, 'utf8'));
  if (!data || typeof data !== 'object' || Array.isArray(data)) throw new Error('no es un objeto JSON');
  return { data, exists: true };
}

/** Copies `path` to `<path>.bak-nxy-<ts>` and prunes our own older backups (never the `.bak-<ts>` of others). */
function backupAndPrune(path) {
  let ts = Date.now();
  while (existsSync(`${path}.${BACKUP_TAG}${ts}`)) ts += 1;
  const backup = `${path}.${BACKUP_TAG}${ts}`;
  copyFileSync(path, backup);
  const prefix = `${basename(path)}.${BACKUP_TAG}`;
  const mine = readdirSync(dirname(path))
    .filter((n) => n.startsWith(prefix) && /^\d+$/.test(n.slice(prefix.length)))
    .sort((a, b) => Number(b.slice(prefix.length)) - Number(a.slice(prefix.length)));
  for (const old of mine.slice(KEEP_BACKUPS)) {
    try {
      unlinkSync(join(dirname(path), old));
    } catch {
      /* a backup we cannot delete is harmless */
    }
  }
  return { backup, kept: Math.min(mine.length, KEEP_BACKUPS) };
}

function fail(msg) {
  console.log(msg);
  process.exitCode = 1;
}

function doSet(cwd, opts) {
  const scope = opts.scope === 'repo' ? 'repo' : opts.scope === 'user' ? 'user' : null;
  const key = typeof opts.key === 'string' ? opts.key : '';
  let value;
  try {
    value = JSON.parse(typeof opts.json === 'string' ? opts.json : 'null');
  } catch {
    return out({ ok: false, reason: 'valor ilegible' });
  }
  if (!scope) return out({ ok: false, reason: 'scope inválido' });
  const v = validate(key, value);
  if (!v.ok) return out({ ok: false, reason: v.reason });
  const path = scope === 'repo' ? projectConfigPath(cwd) : join(nxyUserDir(), 'config.json');
  let cur;
  try {
    cur = readObject(path).data;
  } catch {
    return out({ ok: false, reason: `${path} está roto; no se toca` });
  }
  const text = JSON.stringify(setPath(cur, FIELDS[key].path, value), null, 2) + '\n';
  writeAtomic(path, text);
  if (key === 'modules.filter') {
    try {
      writeEngineCache(resolveEngine(loadConfig(cwd), claudeSettingsFiles(cwd)));
    } catch {
      /* the cache refreshes itself on the next session */
    }
  }
  return out({ ok: true, path, bytes: Buffer.byteLength(text), config: configView(cwd) });
}

function out(obj) {
  console.log(JSON.stringify(obj));
  if (!obj.ok) process.exitCode = 1;
}

function doTools(cwd) {
  let version = null;
  try {
    version = JSON.parse(readFileSync(join(PLUGIN_ROOT, '.claude-plugin', 'plugin.json'), 'utf8')).version ?? null;
  } catch {
    /* unknown */
  }
  const eng = resolveEngine(loadConfig(cwd), claudeSettingsFiles(cwd));
  const cg = findCodegraph();
  out({
    ok: true,
    version,
    rtk: { found: !!eng.rtkPath, path: eng.rtkPath ?? null, version: eng.rtkVersion ?? null },
    rg: { found: eng.rgAvailable || !!eng.rgInstalledAt, path: eng.rgAvailable ? 'rg' : eng.rgInstalledAt ?? null, version: null },
    codegraph: { found: !!cg, path: cg?.path ?? null, version: cg?.version ?? null },
  });
}

function doCacheTtl(arg) {
  if (arg !== '1h' && arg !== 'off') return fail('uso: cache-ttl 1h|off');
  const path = claudeUserSettingsPath();
  let cur;
  try {
    cur = readObject(path);
  } catch {
    return fail(`${path} está roto; no se toca`);
  }
  const next = { ...cur.data };
  if (arg === '1h') next.promptCacheTtl = '1h';
  else delete next.promptCacheTtl;
  const b = cur.exists ? backupAndPrune(path) : null;
  writeAtomic(path, JSON.stringify(next, null, 2) + '\n');
  console.log(`promptCacheTtl ${arg === '1h' ? '= 1h' : 'quitado'} en ${path}`);
  console.log(b ? `backup: ${b.backup} (se conservan ${b.kept})` : 'backup: ninguno (el archivo no existía)');
  console.log('Claude Code lo toma en la próxima sesión.');
}

function doStatusline(arg) {
  const shimPath = join(nxyUserDir(), 'statusline.mjs');
  const settingsPath = claudeUserSettingsPath();
  if (arg === 'install') {
    const snippet = { statusLine: { type: 'command', command: `node "${fwd(shimPath)}"`, padding: 0, refreshInterval: 30 } };
    const { backup } = applyStatusLine({ root: PLUGIN_ROOT, settingsPath, shimPath, patch: snippet });
    console.log(`shim: ${shimPath} (${Math.round(statSync(shimPath).size / 1024)} KB)`);
    console.log(backup ? `backup: ${backup}` : 'backup: ninguno (el archivo no existía)');
    console.log(`statusLine escrita en ${settingsPath}. Reiniciá Claude Code para verla.`);
    return;
  }
  if (arg !== 'remove') return fail('uso: statusline install|remove');
  let cur;
  try {
    cur = readObject(settingsPath);
  } catch {
    return fail(`${settingsPath} está roto; no se toca`);
  }
  const cmd = String(cur.data.statusLine?.command ?? '');
  if (!cur.data.statusLine) return fail('no hay statusLine configurada.');
  if (!fwd(cmd).includes(fwd(shimPath))) return fail('la statusLine actual no es la de nxy; no se toca.');
  const next = { ...cur.data };
  delete next.statusLine;
  const b = backupAndPrune(settingsPath);
  writeAtomic(settingsPath, JSON.stringify(next, null, 2) + '\n');
  console.log(`statusLine de nxy quitada de ${settingsPath}`);
  console.log(`shim: ${shimPath} (queda en disco, ~3 KB)`);
  console.log(`backup: ${b.backup} (se conservan ${b.kept})`);
  console.log('Reiniciá Claude Code para aplicarlo.');
}

function doUpdate() {
  const win = process.platform === 'win32';
  const r = spawnSync('claude', ['plugin', 'update', 'nxy@nxy-dev'], { encoding: 'utf8', shell: win, timeout: 120000, windowsHide: true });
  const text = `${r.stdout ?? ''}${r.stderr ?? ''}`.trim();
  if (claudeMissing(r, process.platform)) return fail('No encontré `claude` en el PATH; actualizá con `claude plugin update nxy@nxy-dev` desde una terminal.');
  if (r.error || r.status !== 0) return fail(`${text || String(r.error?.message ?? 'falló')}\nLa actualización falló.`);
  console.log(text);
  console.log('Reiniciá Claude Code para aplicarlo.');
}

const argv = process.argv.slice(2);
try {
  const { opts, positional } = parseArgs(argv);
  const cwd = toNativePath(typeof opts.cwd === 'string' ? opts.cwd : process.env.CLAUDE_PROJECT_DIR || process.cwd());
  const action = (positional[0] || '').toLowerCase();
  if (action === 'set') doSet(cwd, opts);
  else if (action === 'tools') doTools(cwd);
  else if (action === 'cache-ttl') doCacheTtl(positional[1]);
  else if (action === 'statusline') doStatusline(positional[1]);
  else if (action === 'update') doUpdate();
  else fail('uso: config.mjs set|tools|cache-ttl|statusline|update');
} catch (err) {
  const reason = String((err instanceof Error && err.message) || err).split('\n')[0];
  if (argv[0] === 'set' || argv[0] === 'tools') out({ ok: false, reason });
  else fail(`error: ${reason}`);
}
