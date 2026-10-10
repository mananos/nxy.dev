// @ts-check
/**
 * What the panel's Config tab shows: the effective config (user file, then repo file, over the plugin
 * defaults), where each editable field comes from, and the state of the two files it can write to.
 * A broken file never throws: it is marked `broken` and ignored, as `loadConfig` does.
 */
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { frontmatterEffort } from './roles.mjs';
import { FIELDS, ROLES, sourcesOf } from '../../core/configedit.mjs';
import { loadConfig, projectConfigPath } from '../../core/config.mjs';
import { nxyUserDir } from '../../core/paths.mjs';

/** @param {string} path */
function readScope(path) {
  if (!existsSync(path)) return { path, exists: false, broken: false, data: null };
  try {
    const data = JSON.parse(readFileSync(path, 'utf8'));
    if (!data || typeof data !== 'object' || Array.isArray(data)) return { path, exists: true, broken: true, data: null };
    return { path, exists: true, broken: false, data };
  } catch {
    return { path, exists: true, broken: true, data: null };
  }
}

/** @param {string} cwd */
export function configView(cwd) {
  const cfg = loadConfig(cwd);
  const user = readScope(join(nxyUserDir(), 'config.json'));
  const repo = readScope(projectConfigPath(cwd));
  /** @type {Record<string, {model: string|null, effort: string|null}>} */
  const roles = {};
  for (const r of ROLES) {
    const c = /** @type {any} */ (cfg).roles?.[r];
    roles[r] = {
      model: typeof c?.model === 'string' ? c.model : null,
      effort: typeof c?.effort === 'string' ? c.effort : frontmatterEffort(r),
    };
  }
  /** @type {Record<string, 'default'|'user'|'repo'|'env'>} */
  const sources = {};
  for (const id of Object.keys(FIELDS)) sources[id] = sourcesOf(id, { user: user.data, repo: repo.data }, process.env);
  const strip = ({ path, exists, broken }) => ({ path, exists, broken });
  return {
    pauseAfterBatch: cfg.flow?.pauseAfterBatch === true,
    orchestrator: cfg.flow?.orchestrator === 'off' ? 'off' : 'auto',
    ui: { panel: cfg.ui?.panel === 'off' ? 'off' : 'auto' },
    gate: { enabled: cfg.gate?.enabled === true, contextTokens: cfg.gate?.contextTokens ?? 0 },
    filter: cfg.modules?.filter === true,
    roles,
    scopes: { user: strip(user), repo: strip(repo) },
    sources,
  };
}
