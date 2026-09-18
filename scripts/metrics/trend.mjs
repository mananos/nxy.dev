#!/usr/bin/env node
// @ts-check
/**
 * `/nxy:trend` — how consumption evolves over time, from the transcripts already on disk.
 * Sessions from before nxy was installed appear too, so the effect of using the plugin
 * shows up as the trend moves.
 *
 *   node trend.mjs [--since 30d] [--by day|week|session|model] [--here] [--cwd <dir>] [--json]
 *   Global (all projects) by default; --here restricts to the current project.
 */
import { existsSync } from 'node:fs';
import { join } from 'node:path';
import { loadConfig } from '../lib/config.mjs';
import { clip, fmtDate, fmtDuration, fmtPct, fmtTokens, fmtUsd, parseArgs, parseSince, table } from '../lib/format.mjs';
import { readJsonl } from '../lib/jsonl.mjs';
import { nxyProjectDir } from '../lib/paths.mjs';
import { listSessions, parseSession } from '../lib/transcripts.mjs';

const { opts } = parseArgs(process.argv.slice(2));
const cwd = typeof opts.cwd === 'string' ? opts.cwd : process.cwd();
const cfg = loadConfig(cwd);
const since = parseSince(typeof opts.since === 'string' ? opts.since : '30d');
const by = typeof opts.by === 'string' ? opts.by : 'day';
if (!['day', 'week', 'session', 'model'].includes(by)) {
  console.error('nxy: --by must be day | week | session | model');
  process.exit(1);
}

const here = Boolean(opts.here);
const refs = listSessions({ cwd, all: !here, projectsDir: cfg.metrics.projectsDir, since });
if (!refs.length) {
  console.log(`nxy trend: no sessions since ${fmtDate(since)} for ${here ? cwd : 'any project'}. Try a longer --since.`);
  process.exit(0);
}

/** filter.jsonl rows are per project cwd; cache them per cwd. */
const filterCache = new Map();
function filterRowsFor(sessionCwd, sessionId) {
  const dir = sessionCwd || cwd;
  if (!filterCache.has(dir)) {
    const p = join(nxyProjectDir(dir), 'metrics', 'filter.jsonl');
    filterCache.set(dir, existsSync(p) ? readJsonl(p) : []);
  }
  return filterCache.get(dir).filter((r) => r.session_id === sessionId);
}

const sessions = [];
for (const ref of refs) {
  const s = parseSession(ref, { cacheBreakThreshold: cfg.metrics.cacheBreakThreshold });
  if (!s.usage.calls) continue;
  const rows = filterRowsFor(s.cwd, s.sessionId);
  const nxy = rows.length > 0 || Object.keys(s.bySkill).some((k) => k.startsWith('nxy:'));
  sessions.push({
    sessionId: s.sessionId,
    project: s.project,
    ts: s.firstTs ?? ref.mtimeMs,
    turns: s.turns,
    calls: s.usage.calls,
    input: s.usage.usage.input,
    cacheW: s.usage.usage.cacheWrite5m + s.usage.usage.cacheWrite1h,
    cacheR: s.usage.usage.cacheRead,
    output: s.usage.usage.output,
    totalTokens: s.usage.totalInput + s.usage.usage.output,
    usd: s.usage.usd,
    usdKnown: s.usage.usd !== null,
    agentTokens: s.subagents.totalInput + s.subagents.usage.output,
    agents: s.agents.length,
    bashLines: s.tools.bash.resultLines,
    bashCalls: s.tools.bash.calls,
    rtkCalls: rows.filter((r) => r.engine === 'rtk').length,
    wallMs: s.wallMs,
    nxy,
    firstPrompt: s.prompts[0]?.text || '',
    byModel: s.byModel,
  });
}
sessions.sort((a, b) => a.ts - b.ts);

if (by === 'model') {
  const m = new Map();
  for (const sess of sessions) {
    for (const [model, b] of Object.entries(sess.byModel)) {
      const g = m.get(model) || { model, sessions: new Set(), calls: 0, input: 0, cacheW: 0, cacheR: 0, output: 0, thinking: 0, usd: 0, usdKnown: true };
      g.sessions.add(sess.sessionId);
      g.calls += b.calls;
      g.input += b.usage.input;
      g.cacheW += b.usage.cacheWrite5m + b.usage.cacheWrite1h;
      g.cacheR += b.usage.cacheRead;
      g.output += b.usage.output;
      g.thinking += b.usage.thinking;
      if (b.usd === null) g.usdKnown = false;
      else g.usd += b.usd;
      m.set(model, g);
    }
  }
  const rows = [...m.values()].map((g) => ({
    ...g, sessions: g.sessions.size, total: g.input + g.cacheW + g.cacheR + g.output,
    hit: g.input + g.cacheW + g.cacheR ? (g.cacheR / (g.input + g.cacheW + g.cacheR)) * 100 : null, usd: g.usdKnown ? g.usd : null,
  })).sort((a, b) => (b.usd ?? 0) - (a.usd ?? 0));
  const all = rows.reduce((n, r) => n + r.total, 0);
  if (opts.json) {
    console.log(JSON.stringify({ since, by, models: rows }, null, 2));
    process.exit(0);
  }
  const usdLabelM = cfg.metrics.subscription ? 'USD~' : 'USD';
  console.log(`nxy trend — ${here ? cwd : 'all projects'} · since ${fmtDate(since)} · by model · ${sessions.length} sessions`);
  console.log('');
  console.log(table(rows.map((r) => ({ ...r, share: all ? (r.total / all) * 100 : null })), /** @type {any} */ ([
    { key: 'model', label: 'model' },
    { key: 'sessions', label: 'sess', align: 'right' },
    { key: 'calls', label: 'calls', align: 'right' },
    { key: 'total', label: 'tokens', align: 'right', fmt: fmtTokens },
    { key: 'share', label: 'share', align: 'right', fmt: (v) => fmtPct(v) },
    { key: 'input', label: 'uncached', align: 'right', fmt: fmtTokens },
    { key: 'cacheR', label: 'cache-r', align: 'right', fmt: fmtTokens },
    { key: 'output', label: 'output', align: 'right', fmt: fmtTokens },
    { key: 'thinking', label: 'thinking', align: 'right', fmt: fmtTokens },
    { key: 'hit', label: 'hit', align: 'right', fmt: (v) => fmtPct(v) },
    { key: 'usd', label: usdLabelM, align: 'right', fmt: fmtUsd },
  ])));
  console.log('');
  console.log('legend: share = share of all tokens in the range · thinking is a subset of output');
  process.exit(0);
}

function periodKey(ts) {
  const d = new Date(ts);
  if (by === 'week') {
    const day = (d.getDay() + 6) % 7; // Monday = 0
    d.setDate(d.getDate() - day);
  }
  return fmtDate(d.getTime(), { time: false });
}

const groups = new Map();
for (const s of sessions) {
  const key = by === 'session' ? s.sessionId : periodKey(s.ts);
  const g = groups.get(key) || {
    period: key, ts: s.ts, sessions: 0, turns: 0, calls: 0, input: 0, cacheW: 0, cacheR: 0, output: 0, totalTokens: 0,
    usd: 0, usdKnown: true, agentTokens: 0, agents: 0, bashLines: 0, bashCalls: 0, rtkCalls: 0, wallMs: 0, nxy: 0, project: s.project, firstPrompt: s.firstPrompt,
  };
  g.sessions++;
  for (const k of ['turns', 'calls', 'input', 'cacheW', 'cacheR', 'output', 'totalTokens', 'agentTokens', 'agents', 'bashLines', 'bashCalls', 'rtkCalls', 'wallMs']) g[k] += s[k];
  if (s.usdKnown) g.usd += s.usd;
  else g.usdKnown = false;
  if (s.nxy) g.nxy++;
  groups.set(key, g);
}
const rows = [...groups.values()].map((g) => ({
  ...g,
  usd: g.usdKnown ? g.usd : null,
  hit: g.input + g.cacheW + g.cacheR ? (g.cacheR / (g.input + g.cacheW + g.cacheR)) * 100 : null,
  agentPct: g.totalTokens ? (g.agentTokens / g.totalTokens) * 100 : null,
  perTurn: g.turns ? g.totalTokens / g.turns : null,
  nxyLabel: by === 'session' ? (g.nxy ? 'yes' : 'no') : `${g.nxy}/${g.sessions}`,
}));

if (opts.json) {
  console.log(JSON.stringify({ since, by, sessions, periods: rows }, null, 2));
  process.exit(0);
}

const usdLabel = cfg.metrics.subscription ? 'USD~' : 'USD';
console.log(`nxy trend — ${here ? cwd : 'all projects'} · since ${fmtDate(since)} · by ${by} · ${sessions.length} sessions`);
console.log('');
const cols = [
  { key: 'period', label: by === 'session' ? 'session' : by, fmt: (v) => (by === 'session' ? String(v).slice(0, 8) : v) },
  ...(by === 'session' ? [{ key: 'ts', label: 'started', fmt: (v) => fmtDate(v) }, { key: 'firstPrompt', label: 'first prompt', fmt: (v) => clip(v.replace(/\s+/g, ' '), 28) }] : [{ key: 'sessions', label: 'sess', align: /** @type {const} */ ('right') }]),
  { key: 'turns', label: 'turns', align: 'right' },
  { key: 'totalTokens', label: 'tokens', align: 'right', fmt: fmtTokens },
  { key: 'input', label: 'uncached', align: 'right', fmt: fmtTokens },
  { key: 'cacheR', label: 'cache-r', align: 'right', fmt: fmtTokens },
  { key: 'output', label: 'output', align: 'right', fmt: fmtTokens },
  { key: 'hit', label: 'hit', align: 'right', fmt: (v) => fmtPct(v) },
  { key: 'agentPct', label: 'agents', align: 'right', fmt: (v) => fmtPct(v) },
  { key: 'perTurn', label: 'tok/turn', align: 'right', fmt: fmtTokens },
  { key: 'bashLines', label: 'bash lines', align: 'right', fmt: fmtTokens },
  { key: 'rtkCalls', label: 'rtk', align: 'right' },
  { key: 'usd', label: usdLabel, align: 'right', fmt: fmtUsd },
  { key: 'nxyLabel', label: 'nxy', align: 'right' },
];
console.log(table(rows, /** @type {any} */ (cols)));
console.log('');
console.log(`legend: tokens = all input (uncached + cache) + output · hit = cache reads / all input · agents = share of tokens spent by subagents · rtk = Bash calls filtered by nxy · nxy = sessions that ran with the plugin${cfg.metrics.subscription ? ' · USD~ = equivalent at API prices (subscription)' : ''}`);
const totalUsd = rows.every((r) => r.usd !== null) ? rows.reduce((n, r) => n + (r.usd || 0), 0) : null;
console.log(`total: ${fmtTokens(rows.reduce((n, r) => n + r.totalTokens, 0))} tokens · ${fmtUsd(totalUsd)} · ${fmtDuration(rows.reduce((n, r) => n + r.wallMs, 0))} wall`);
