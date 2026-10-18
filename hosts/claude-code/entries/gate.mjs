#!/usr/bin/env node
// @ts-check
/**
 * `/nxy:gate [status|on|off|once [search]|reads on|off]` —the write gate's control surface.
 *
 * `status` also reports what the gate has actually been doing, because the honest answer to
 * "should I keep this on?" is a count, not an opinion.
 */
import { existsSync, readFileSync } from 'node:fs';
import { dirname } from 'node:path';
import { deepMerge, loadConfig, projectConfigPath } from '../../../core/config.mjs';
import { fmtTokens, parseArgs, parseSince } from '../../../core/format.mjs';
import { projectBroadSearches } from '../search-history.mjs';
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
} else if (action === 'once' && (positional[1] || '').toLowerCase() === 'search') {
  const cfg = loadConfig(cwd);
  const mins = cfg.gate?.escapeMinutes ?? 5;
  const until = armEscape(cwd, mins, 'search');
  console.log(`gate: the next broad main-thread search goes through unstopped (expires in ${mins} min, ${new Date(until).toLocaleTimeString()}).`);
  console.log('Single use: the search after that is subject to the gate again. Edits are unaffected.');
} else if (action === 'once') {
  const cfg = loadConfig(cwd);
  const mins = cfg.gate?.escapeMinutes ?? 5;
  const until = armEscape(cwd, mins);
  console.log(`gate: the next main-thread edit goes through unblocked (expires in ${mins} min, ${new Date(until).toLocaleTimeString()}).`);
  console.log('Covers the plan, threshold and no-handoff refusals. Single use: the edit after that is subject to them again.');
} else if (action === 'reads' && ['on', 'off'].includes((positional[1] || '').toLowerCase())) {
  const on = positional[1].toLowerCase() === 'on';
  const p = writeProjectConfig({ gate: { reads: on } });
  console.log(`gate reads: ${on ? 'ON' : 'OFF'} (written to ${p})`);
  console.log(on
    ? 'Broad main-thread searches are stopped past the threshold again.'
    : 'Broad searches are observed only: nothing is stopped.');
} else if (action !== 'status') {
  console.log(`unknown action "${action}". Use: status | on | off | once [search] | reads on|off`);
}

const cfg = loadConfig(cwd);
const gateCfg = cfg.gate || { enabled: false, contextTokens: 0, escapeMinutes: 5 };
console.log(describeGate(gateCfg, {
  contextTokens: null, // outside a hook there is no "current" main thread to measure
  hasImplementer: Boolean(cfg.roles && cfg.roles.implementer),
  escapeUntil: escapeUntil(cwd, gateCfg.escapeMinutes),
  searchEscapeUntil: escapeUntil(cwd, gateCfg.escapeMinutes, 'search'),
}));

// What it has done so far, from the ledger: edits (no `kind`) and searches (`kind: 'read'`).
const allRows = readJsonl(gateLedgerPath(cwd));
const rows = allRows.filter((r) => r.kind !== 'read');
const searchRows = allRows.filter((r) => r.kind === 'read');
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
{
  const stopped = searchRows.filter((r) => r.allow === false).length;
  const escaped = searchRows.filter((r) => r.allow === true).length;
  console.log(`searches:        ${stopped} stopped, ${escaped} let through by \`/nxy:gate once search\``);
}

// Broad searches the main thread ran past the threshold (observation only, never blocked).
{
  const sinceRaw = typeof opts.since === 'string' ? opts.since : '30d';
  const sinceMs = parseSince(sinceRaw) ?? Date.now() - 30 * 86_400_000;
  const b = projectBroadSearches(cwd, { sinceMs, projectsDir: cfg.metrics?.projectsDir, threshold: cfg.gate?.contextTokens ?? 0 });
  if (!b.count) {
    console.log(`broad searches:  none recorded (since ${new Date(sinceMs).toISOString().slice(0, 10)})`);
  } else {
    const kinds = ['Grep', 'Glob', 'Bash', 'Read'].filter((t) => b.byKind[t]).map((t) => `${t} ${b.byKind[t]}`);
    for (const t of Object.keys(b.byKind)) if (!['Grep', 'Glob', 'Bash', 'Read'].includes(t)) kinds.push(`${t} ${b.byKind[t]}`);
    console.log(`broad searches that ran above ${b.threshold ? fmtTokens(b.threshold) : '?'} context (since ${new Date(sinceMs).toISOString().slice(0, 10)}): ${b.count} — ${kinds.join(' · ')} · context carried at those searches ${fmtTokens(b.contextTokens)} tokens (sum of main-thread context size: an upper bound of what each call cost, not what a scout would have saved)`);
  }
}
