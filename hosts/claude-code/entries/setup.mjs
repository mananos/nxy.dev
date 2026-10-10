#!/usr/bin/env node
// @ts-check
/**
 * Detects and installs rtk, rg and codegraph. Only ever run by the user's explicit choice
 * (/nxy:setup or the panel's Config button); no hook calls it.
 *
 * Usage (all take --cwd <dir>):
 *   setup.mjs status [--json]
 *   setup.mjs run <rtk|rg|codegraph> --yes [--dry-run]
 *                                without --yes it refuses (exit 1) and prints the command;
 *                                --dry-run prints the steps and runs nothing
 */
import { loadConfig } from '../../../core/config.mjs';
import { resolveEngine, writeEngineCache } from '../../../core/filter/engine.mjs';
import { parseArgs } from '../../../core/format.mjs';
import { toNativePath } from '../../../core/paths.mjs';
import { TOOLS, describePlan, runPlan } from '../../../core/setup.mjs';
import { claudeSettingsFiles } from '../settings.mjs';
import { toolsState } from '../setup-state.mjs';

function status(cwd, json) {
  const st = toolsState(cwd);
  if (json) {
    console.log(JSON.stringify({ ok: true, platform: st.platform, tools: st.tools }));
    return;
  }
  console.log(`nxy setup (${st.platform})`);
  for (const t of TOOLS) {
    const i = st.tools[t];
    const where = !i.found ? 'no encontrada' : i.onPath ? 'en el PATH' : 'sólo en un directorio conocido (una terminal nueva la ve en el PATH)';
    console.log(`\n${t}: ${i.found ? `encontrada${i.version ? ` ${i.version}` : ''}${i.path ? ` (${i.path})` : ''}` : 'no encontrada'} — ${where}`);
    if (i.conflict) console.log(`  choca: ${t === 'rtk' ? 'hook propio de rtk en tus settings' : i.conflict === 'mcp' ? 'codegraph ya registrado como servidor MCP' : 'hook de codegraph en tus settings'}`);
    for (const l of describePlan(i.plan, { manual: true })) console.log(`  ${l}`);
  }
  console.log('\nNada se instaló. Para instalar: setup.mjs run <herramienta> --yes');
}

function run(cwd, tool, opts) {
  if (!TOOLS.includes(tool)) {
    console.log(`uso: setup.mjs run <${TOOLS.join('|')}> --yes [--dry-run]`);
    process.exitCode = 1;
    return;
  }
  const plan = toolsState(cwd).tools[tool].plan;
  const lines = describePlan(plan);
  if (opts.yes !== true) {
    console.log(`Falta --yes: no se instala nada. Lo que correría para ${tool}:`);
    for (const l of describePlan(plan, { manual: true })) console.log(`  ${l}`);
    process.exitCode = 1;
    return;
  }
  if (!plan.steps.length) {
    console.log(lines.join('\n'));
    process.exitCode = 1;
    return;
  }
  console.log(`${plan.action} ${tool}${opts['dry-run'] === true ? ' (dry-run: no se ejecuta nada)' : ''}:`);
  for (const l of lines) console.log(`  ${l}`);
  if (opts['dry-run'] === true) {
    for (const m of plan.manual) console.log(`a mano: ${m}`);
    return;
  }
  const r = runPlan(plan);
  if (r.tail) console.log(r.tail);
  if (!r.ok) {
    console.log(`Falló en: ${r.failedAt}`);
    if (r.manual.length) console.log(`Para correrlo en una terminal: ${r.manual.join(' ; ')}`);
    process.exitCode = 1;
    return;
  }
  try {
    writeEngineCache(resolveEngine(loadConfig(cwd), claudeSettingsFiles(cwd)));
  } catch {
    /* the cache refreshes itself on the next session */
  }
  console.log(`${tool}: listo (${r.ran.length} paso${r.ran.length === 1 ? '' : 's'}).`);
  // sudo fallbacks (plain `sudo ...`) belong to steps that just ran OK; only the other notes still apply
  for (const m of plan.manual.filter((n) => !n.startsWith('sudo '))) console.log(`a mano: ${m}`);
}

try {
  const { opts, positional } = parseArgs(process.argv.slice(2));
  const cwd = toNativePath(typeof opts.cwd === 'string' ? opts.cwd : process.env.CLAUDE_PROJECT_DIR || process.cwd());
  const action = (positional[0] || '').toLowerCase();
  if (action === 'status') status(cwd, opts.json === true);
  else if (action === 'run') run(cwd, (positional[1] || '').toLowerCase(), opts);
  else {
    console.log('uso: setup.mjs status [--json] | run <rtk|rg|codegraph> --yes [--dry-run]');
    process.exitCode = 1;
  }
} catch (err) {
  console.log(`error: ${String((err instanceof Error && err.message) || err).split('\n')[0]}`);
  process.exitCode = 1;
}
