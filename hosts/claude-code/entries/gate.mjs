#!/usr/bin/env node
// @ts-check
/**
 * `/nxy:gate [status|on|off|once]` — the write gate's control surface.
 *
 * `status` also reports what the gate has actually been doing, because the honest answer to
 * "should I keep this on?" is a count, not an opinion.
 */
import { existsSync, readFileSync } from 'node:fs';
import { dirname } from 'node:path';
import { deepMerge, loadConfig, projectConfigPath } from '../../../core/config.mjs';
import { parseArgs } from '../../../core/format.mjs';
import { ensureDir } from '../../../core/paths.mjs';
import { describeGate } from '../../../core/gate.mjs';
import { writeFileSync } from 'node:fs';
import { armEscape, clearEscape, escapeUntil } from '../gate-state.mjs';
import { gateLedgerPath } from '../gate-ledger.mjs';
import { readJsonl } from '../../../core/jsonl.mjs';

const { opts, positional } = parseArgs(process.argv.slice(2));
const cwd = typeof opts.cwd === 'string' ? opts.cwd : process.cwd();
const action = (positional[0] || 'status').toLowerCase();

function writeProjectConfig(patch) {
  const p = projectConfigPath(cwd);
  let current = {};
  try {
    if (existsSync(p)) current = JSON.parse(readFileSync(p, 'utf8'));
  } catch {
    current = {};
  }
  ensureDir(dirname(p));
  writeFileSync(p, JSON.stringify(deepMerge(current, patch), null, 2) + '\n', 'utf8');
  return p;
}

if (action === 'on' || action === 'off') {
  const p = writeProjectConfig({ gate: { enabled: action === 'on' } });
  if (action === 'on') clearEscape(cwd);
  console.log(`gate: ${action.toUpperCase()} (written to ${p})`);
  if (action === 'off') console.log('Past the threshold, edits are no longer delegated and no handoff is required first.');
} else if (action === 'once') {
  const cfg = loadConfig(cwd);
  const mins = cfg.gate?.escapeMinutes ?? 5;
  const until = armEscape(cwd, mins);
  console.log(`gate: the next main-thread edit goes through unblocked (expires in ${mins} min, ${new Date(until).toLocaleTimeString()}).`);
  console.log('Single use: the edit after that is subject to the threshold again.');
} else if (action !== 'status') {
  console.log(`unknown action "${action}". Use: status | on | off | once`);
}

const cfg = loadConfig(cwd);
const gateCfg = cfg.gate || { enabled: false, contextTokens: 0, escapeMinutes: 5 };
console.log(describeGate(gateCfg, {
  contextTokens: null, // outside a hook there is no "current" main thread to measure
  hasImplementer: Boolean(cfg.roles && cfg.roles.implementer),
  escapeUntil: escapeUntil(cwd, gateCfg.escapeMinutes),
}));

// What it has done so far, from the ledger.
const rows = readJsonl(gateLedgerPath(cwd));
if (rows.length) {
  const decided = rows.filter((r) => typeof r.contextTokens === 'number');
  const blocked = rows.filter((r) => r.allow === false);
  const pct = rows.length ? Math.round((blocked.length / rows.length) * 100) : 0;
  console.log(`\ndecisions:       ${blocked.length} blocked of ${rows.length} edits (${pct}%)`);
  if (decided.length) {
    const ctx = decided.map((r) => Number(r.contextTokens)).sort((a, b) => a - b);
    const q = (p) => ctx[Math.min(ctx.length - 1, Math.floor(ctx.length * p))];
    const k = (n) => `${Math.round(n / 1000)}k`;
    console.log(`context at edit: p10 ${k(q(0.1))} - p50 ${k(q(0.5))} - p90 ${k(q(0.9))} - max ${k(ctx[ctx.length - 1])}`);
  }
} else {
  console.log('\ndecisions:       no edit has gone through the gate in this repo yet');
}
