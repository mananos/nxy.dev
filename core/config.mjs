// @ts-check
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { PLUGIN_ROOT, nxyProjectDir, nxyUserDir } from './paths.mjs';

/**
 * @typedef {object} NxyConfig
 * @property {{metrics: boolean, filter: boolean}} modules
 * @property {{engine: 'auto'|'rtk'|'off', excludeCommands: string[], onlyCommands: string[], autoAllowWhenOriginalAllowed: boolean}} filter
 * @property {{projectsDir: string|null, subscription: boolean, statusline: {color: boolean, preset: string, layout: string, separator?: string, brand: string, ctxWarnTokens: number, ctxCritTokens: number, turnWarnUsd: number, turnCritUsd: number, promptCacheTtlMin: number, theme: Record<string, string>}, cacheBreakThreshold: number}} metrics
 * @property {{codegraph: boolean}} scout
 * @property {{enabled: boolean, contextTokens: number, escapeMinutes: number}} gate
 * @property {Record<string, Role>} roles
 * @property {{mode: 'manual'|'assisted'|'proactive', handoff: {required: boolean}}} [memory]
 * @property {{plan?: "auto"|"always"}} [flow]  "always": every change that touches a second file goes through the planner (0.4.4)
 * @property {{paths?: string[]}} [docs]  where the documenter looks (0.4.3); unset = the docs that name the change + the wiki folder the CI publishes
 */

/**
 * One row of the role table. `provider` carries no weight yet — there is only one provider today —
 * but it is declared from the start so adding a second one is a config change, not a redesign.
 * @typedef {object} Role
 * @property {string} model
 * @property {string} provider
 */

function isPlainObject(v) {
  return v !== null && typeof v === 'object' && !Array.isArray(v);
}

/** Deep-merges `src` into a copy of `base`. Arrays and scalars are replaced, objects recurse. */
export function deepMerge(base, src) {
  const out = { ...base };
  for (const [k, v] of Object.entries(src || {})) {
    out[k] = isPlainObject(v) && isPlainObject(base?.[k]) ? deepMerge(base[k], v) : v;
  }
  return out;
}

function readJsonIfExists(path) {
  if (!existsSync(path)) return null;
  try {
    return JSON.parse(readFileSync(path, 'utf8'));
  } catch {
    return null; // a broken override must never take the plugin down
  }
}

/**
 * Resolution order (later wins): plugin defaults → ~/.nxy/config.json → <cwd>/.nxy/config.json → env.
 * Env: NXY_FILTER=0|1 toggles the filter module, NXY_ENGINE=rtk|off forces the engine.
 * @param {string} cwd
 * @param {NodeJS.ProcessEnv} [env]
 * @returns {NxyConfig}
 */
export function loadConfig(cwd, env = process.env) {
  let cfg = readJsonIfExists(join(PLUGIN_ROOT, 'nxy.config.json')) || {};
  cfg = deepMerge(cfg, readJsonIfExists(join(nxyUserDir(), 'config.json')) || {});
  cfg = deepMerge(cfg, readJsonIfExists(join(nxyProjectDir(cwd), 'config.json')) || {});
  if (env.NXY_FILTER === '0' || env.NXY_FILTER === '1') {
    cfg = deepMerge(cfg, { modules: { filter: env.NXY_FILTER === '1' } });
  }
  if (env.NXY_ENGINE === 'rtk' || env.NXY_ENGINE === 'off') {
    cfg = deepMerge(cfg, { filter: { engine: env.NXY_ENGINE } });
  }
  return /** @type {NxyConfig} */ (cfg);
}

/** Path of the project-level override file (created by `/nxy:filter on|off`). */
export function projectConfigPath(cwd) {
  return join(nxyProjectDir(cwd), 'config.json');
}
