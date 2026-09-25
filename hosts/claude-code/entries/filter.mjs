#!/usr/bin/env node
// @ts-check
/**
 * `/nxy:filter [status|on|off]` — shows engine status; `on`/`off` toggle `modules.filter`
 * in the project override (`<cwd>/.nxy/config.json`) and refresh the engine cache.
 */
import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname } from 'node:path';
import { deepMerge, loadConfig, projectConfigPath } from '../../../core/config.mjs';
import { parseArgs } from '../../../core/format.mjs';
import { ensureDir } from '../../../core/paths.mjs';
import { describeEngine, resolveEngine, writeEngineCache } from '../../../core/filter/engine.mjs';
import { claudeSettingsFiles } from '../settings.mjs';

const { opts, positional } = parseArgs(process.argv.slice(2));
const cwd = typeof opts.cwd === 'string' ? opts.cwd : process.cwd();
const action = (positional[0] || 'status').toLowerCase();

if (action === 'on' || action === 'off') {
  const p = projectConfigPath(cwd);
  let current = {};
  try {
    if (existsSync(p)) current = JSON.parse(readFileSync(p, 'utf8'));
  } catch {
    current = {};
  }
  ensureDir(dirname(p));
  writeFileSync(p, JSON.stringify(deepMerge(current, { modules: { filter: action === 'on' } }), null, 2) + '\n');
  console.log(`filter module: ${action.toUpperCase()} (written to ${p})`);
} else if (action !== 'status') {
  console.log(`unknown action "${action}". Use: status | on | off`);
}

const cfg = loadConfig(cwd);
const info = resolveEngine(cfg, claudeSettingsFiles(cwd));
writeEngineCache(info);
console.log(`filter module: ${cfg.modules.filter ? 'enabled' : 'DISABLED'} (metrics module: ${cfg.modules.metrics ? 'enabled' : 'disabled'})`);
console.log(describeEngine(info, cfg));
if (cfg.filter.excludeCommands.length) console.log(`excluded:      ${cfg.filter.excludeCommands.join(', ')}`);
if (cfg.filter.onlyCommands.length) console.log(`only:          ${cfg.filter.onlyCommands.join(', ')}`);
console.log('escape hatch:  prefix a command with NXY_RAW=1 (or append "# raw") to run it unfiltered');
