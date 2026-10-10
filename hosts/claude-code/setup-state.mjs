// @ts-check
// Detection shared by entries/setup.mjs and entries/config.mjs tools: what is installed,
// what collides, and the install plan for each tool. Reads files; never installs.
import { spawnSync } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';
import { homedir } from 'node:os';
import { join, resolve } from 'node:path';
import { loadConfig } from '../../core/config.mjs';
import { resolveEngine } from '../../core/filter/engine.mjs';
import { findCodegraph } from '../../core/scout/codegraph.mjs';
import { TOOLS, detectCodegraphMcp, detectCodegraphWiring, detectRtkConflict, installPlan, packageManager } from '../../core/setup.mjs';
import { claudeSettingsFiles } from './settings.mjs';

/** Parses a JSON object file; a missing or broken file is ignored. */
function readLoose(path) {
  try {
    if (!existsSync(path)) return null;
    const data = JSON.parse(readFileSync(path, 'utf8'));
    return data && typeof data === 'object' && !Array.isArray(data) ? data : null;
  } catch {
    return null;
  }
}

/** True when `bin --version` runs (the binary is on PATH). */
function hasBin(bin) {
  const r = spawnSync(bin, ['--version'], { encoding: 'utf8', timeout: 5000, windowsHide: true });
  return !r.error;
}

/**
 * @param {string} cwd
 * @param {{platform?: string, has?: (bin: string) => boolean}} [opts]
 */
export function toolsState(cwd, opts = {}) {
  const platform = opts.platform || process.platform;
  const eng = resolveEngine(loadConfig(cwd), claudeSettingsFiles(cwd));
  const cg = findCodegraph();
  const settings = /** @type {any[]} */ ([]);
  for (const f of claudeSettingsFiles(cwd)) {
    const o = readLoose(f);
    if (o) settings.push(o);
  }
  const mcp = /** @type {any[]} */ ([]);
  for (const f of [join(homedir(), '.claude.json'), join(cwd, '.mcp.json')]) {
    const o = readLoose(f);
    if (o) mcp.push(o);
  }
  const pm = packageManager(platform, opts.has || hasBin);
  // Where the codegraph MCP lives, as `claude mcp remove -s` names it.
  const hasCg = detectCodegraphMcp;
  const userJson = readLoose(join(homedir(), '.claude.json'));
  const mcpScope = hasCg(userJson)
    ? 'user'
    : hasCg(userJson?.projects?.[cwd])
      ? 'local'
      : hasCg(readLoose(join(cwd, '.mcp.json')))
        ? 'project'
        : null;
  const hasMcp = [...settings, ...mcp].some(hasCg) || hasCg(userJson?.projects?.[cwd]);
  // Where rtk's hook lives: ~/.claude/settings.json is user scope, anything else project scope.
  const userSettings = join(homedir(), '.claude', 'settings.json');
  const hookScopes = /** @type {string[]} */ ([]);
  for (const f of claudeSettingsFiles(cwd)) {
    const o = readLoose(f);
    if (!o || !detectRtkConflict([o])) continue;
    const scope = resolve(f) === resolve(userSettings) ? 'user' : 'project';
    if (!hookScopes.includes(scope)) hookScopes.push(scope);
  }
  if (!hookScopes.length) hookScopes.push('user');
  const cgConflict = hasMcp ? 'mcp' : detectCodegraphWiring(settings, mcp) ? 'hooks' : false;
  const info = {
    rtk: { found: !!eng.rtkPath, path: eng.rtkPath ?? null, version: eng.rtkVersion ?? null, onPath: !!eng.rtkPath && !/[\\/]/.test(eng.rtkPath), conflict: detectRtkConflict(settings) },
    rg: {
      found: eng.rgAvailable || !!eng.rgInstalledAt,
      path: eng.rgAvailable ? 'rg' : eng.rgInstalledAt ?? null,
      version: null,
      onPath: !!eng.rgAvailable,
      conflict: false,
    },
    codegraph: { found: !!cg, path: cg?.path ?? null, version: cg?.version ?? null, onPath: !!cg && !/[\\/]/.test(cg.path ?? ''), conflict: cgConflict },
  };
  const tools = {};
  for (const t of TOOLS) {
    const i = info[t];
    tools[t] = { ...i, plan: installPlan(t, { platform, pm, found: i.found, conflict: i.conflict, rtkPath: info.rtk.path, hookScopes, mcpScope }) };
  }
  return { platform, tools, eng };
}
