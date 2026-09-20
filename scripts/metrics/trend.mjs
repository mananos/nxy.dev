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
import { clip, fmtDate, fmtDuration, fmtPct, fmtRatio, fmtTokens, fmtUsdPartial, parseArgs, parseSince, table } from '../lib/format.mjs';
import { readJsonl } from '../lib/jsonl.mjs';
import { nxyProjectDir } from '../lib/paths.mjs';
import { listSessions, parseSession } from '../lib/transcripts.mjs';
import { dailyFromGain, readLedgerRows, sessionSavings } from '../filter/ledger.mjs';
import { engineInfo } from '../filter/engine.mjs';
import { rtkGain } from '../filter/rtk.mjs';

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

// rtk's ledger for the whole range, attributed to sessions by time window + cwd. Without
// node:sqlite only `rtk gain` daily totals exist; they fit the by-day view and nothing else.
const ledgerRows = readLedgerRows({ sinceMs: since });
/** @type {Map<string, {commands: number, input: number, output: number, saved: number}>|null} */
let gainDaily = null;
if (!ledgerRows && by === 'day') {
  const info = engineInfo(cfg, cwd);
  const gain = info.rtkPath ? rtkGain(info.rtkPath, here ? ['--all', '--project'] : ['--all'], cwd) : null;
  gainDaily = gain ? dailyFromGain(gain) : null; // a failed `rtk gain` must show `?`, not zeros
}
/** rtk books its daily totals by UTC date; our periods are local dates. Best available join. */
const utcDay = (ts) => new Date(ts).toISOString().slice(0, 10);

const sessions = [];
for (const ref of refs) {
  const s = parseSession(ref, { cacheBreakThreshold: cfg.metrics.cacheBreakThreshold });
  if (!s.usage.calls) continue;
  const rows = filterRowsFor(s.cwd, s.sessionId);
  const nxy = rows.length > 0 || Object.keys(s.bySkill).some((k) => k.startsWith('nxy:'));
  const rtkCalls = rows.filter((r) => r.engine === 'rtk').length;
  sessions.push({
    sessionId: s.sessionId,
    project: s.project,
    ts: s.firstTs ?? ref.mtimeMs,
    turns: s.turns,
    calls: s.usage.calls,
    mainCalls: s.main.calls,
    mainInput: s.main.totalInput,
    mainEdits: s.tools.filesEditedMain.length,
    rtkSaved: ledgerRows && rtkCalls ? sessionSavings(ledgerRows, s).saved : ledgerRows ? 0 : null,
    input: s.usage.usage.input,
    cacheW: s.usage.usage.cacheWrite5m + s.usage.usage.cacheWrite1h,
    cacheR: s.usage.usage.cacheRead,
    output: s.usage.usage.output,
    totalTokens: s.usage.totalInput + s.usage.usage.output,
    usd: s.usage.usd,
    usdPartial: s.usage.usdPartial,
    unknownModels: s.usage.unknownModels,
    agentTokens: s.subagents.totalInput + s.subagents.usage.output,
    agents: s.agents.length,
    bashLines: s.tools.bash.resultLines,
    bashCalls: s.tools.bash.calls,
    rtkCalls,
    wallMs: s.wallMs,
    nxy,
    firstPrompt: s.prompts[0]?.text || '',
    byModel: s.byModel,
  });
}
sessions.sort((a, b) => a.ts - b.ts);

/** Suma USD de varias filas: la parte conocida más el aviso de que algo quedó sin tarifa. */
function sumUsd(rows) {
  const unknown = new Set();
  for (const r of rows) for (const m of r.unknownModels || []) unknown.add(m);
  return { usd: rows.reduce((n, r) => n + (r.usd || 0), 0), usdPartial: rows.some((r) => r.usdPartial), unknownModels: [...unknown] };
}
const usdCol = (label) => ({ key: 'usd', label, align: /** @type {const} */ ('right'), fmt: (v, r) => fmtUsdPartial(v, r.usdPartial) });
const unknownNote = (t) => (t.usdPartial ? `\n(no pricing for: ${t.unknownModels.join(', ')} — USD shown as $…+? where affected)` : '');

if (by === 'model') {
  const m = new Map();
  for (const sess of sessions) {
    for (const [model, b] of Object.entries(sess.byModel)) {
      const g = m.get(model) || { model, sessions: new Set(), calls: 0, input: 0, cacheW: 0, cacheR: 0, output: 0, thinking: 0, usd: 0, usdPartial: false, unknownModels: new Set() };
      g.sessions.add(sess.sessionId);
      g.calls += b.calls;
      g.input += b.usage.input;
      g.cacheW += b.usage.cacheWrite5m + b.usage.cacheWrite1h;
      g.cacheR += b.usage.cacheRead;
      g.output += b.usage.output;
      g.thinking += b.usage.thinking;
      g.usd += b.usd;
      if (b.usdPartial) g.usdPartial = true;
      for (const u of b.unknownModels) g.unknownModels.add(u);
      m.set(model, g);
    }
  }
  const rows = [...m.values()].map((g) => ({
    ...g, sessions: g.sessions.size, unknownModels: [...g.unknownModels], total: g.input + g.cacheW + g.cacheR + g.output,
    hit: g.input + g.cacheW + g.cacheR ? (g.cacheR / (g.input + g.cacheW + g.cacheR)) * 100 : null,
  })).sort((a, b) => b.usd - a.usd || b.total - a.total);
  const all = rows.reduce((n, r) => n + r.total, 0);
  const total = sumUsd(rows);
  if (opts.json) {
    console.log(JSON.stringify({ since, by, models: rows, total }, null, 2));
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
    usdCol(usdLabelM),
  ])));
  console.log('');
  console.log(`legend: share = share of all tokens in the range · thinking is a subset of output${unknownNote(total)}`);
  console.log(`total: ${fmtTokens(all)} tokens · ${fmtUsdPartial(total.usd, total.usdPartial)}`);
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
    period: key, ts: s.ts, sessions: 0, turns: 0, calls: 0, mainCalls: 0, mainInput: 0, mainEdits: 0, input: 0, cacheW: 0, cacheR: 0, output: 0, totalTokens: 0,
    usd: 0, usdPartial: false, unknownModels: new Set(), agentTokens: 0, agents: 0, bashLines: 0, bashCalls: 0, rtkCalls: 0, rtkSaved: /** @type {number|null} */ (ledgerRows ? 0 : null), wallMs: 0, nxy: 0, project: s.project, firstPrompt: s.firstPrompt,
  };
  g.sessions++;
  for (const k of ['turns', 'calls', 'mainCalls', 'mainInput', 'mainEdits', 'input', 'cacheW', 'cacheR', 'output', 'totalTokens', 'agentTokens', 'agents', 'bashLines', 'bashCalls', 'rtkCalls', 'wallMs']) g[k] += s[k];
  if (g.rtkSaved !== null && s.rtkSaved !== null) g.rtkSaved += s.rtkSaved;
  g.usd += s.usd;
  if (s.usdPartial) g.usdPartial = true;
  for (const u of s.unknownModels) g.unknownModels.add(u);
  if (s.nxy) g.nxy++;
  groups.set(key, g);
}
const rows = [...groups.values()].map((g) => ({
  ...g,
  unknownModels: [...g.unknownModels],
  hit: g.input + g.cacheW + g.cacheR ? (g.cacheR / (g.input + g.cacheW + g.cacheR)) * 100 : null,
  agentPct: g.totalTokens ? (g.agentTokens / g.totalTokens) * 100 : null,
  perTurn: g.turns ? g.totalTokens / g.turns : null,
  callsPerTurn: g.turns ? g.calls / g.turns : null,
  ctxPerCall: g.mainCalls ? g.mainInput / g.mainCalls : null,
  // by day without node:sqlite: rtk's own daily total (global or `-p`), not per session; keyed by UTC day
  rtkSaved: g.rtkSaved !== null ? g.rtkSaved : gainDaily ? (gainDaily.get(utcDay(g.ts))?.saved ?? 0) : null,
  nxyLabel: by === 'session' ? (g.nxy ? 'yes' : 'no') : `${g.nxy}/${g.sessions}`,
}));

const total = sumUsd(rows);
if (opts.json) {
  console.log(JSON.stringify({ since, by, sessions, periods: rows, total }, null, 2));
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
  { key: 'callsPerTurn', label: 'calls/turn', align: 'right', fmt: fmtRatio },
  { key: 'ctxPerCall', label: 'ctx/call', align: 'right', fmt: fmtTokens },
  { key: 'mainEdits', label: 'main edits', align: 'right' },
  { key: 'bashLines', label: 'bash lines', align: 'right', fmt: fmtTokens },
  { key: 'rtkCalls', label: 'rtk', align: 'right' },
  { key: 'rtkSaved', label: 'rtk saved', align: 'right', fmt: fmtTokens },
  usdCol(usdLabel),
  { key: 'nxyLabel', label: 'nxy', align: 'right' },
];
console.log(table(rows, /** @type {any} */ (cols)));
console.log('');
const rtkSavedNote = ledgerRows ? 'rtk saved = tokens rtk cut from Bash output, from its own ledger' : gainDaily ? `rtk saved = rtk's daily total (${here ? 'this project' : 'all projects'}, from rtk gain, booked by UTC day; install Node ≥ 22.13 for per-session figures)` : 'rtk saved = ? (rtk ledger not readable)';
console.log(`legend: tokens = all input (uncached + cache) + output · hit = cache reads / all input · agents = share of tokens spent by subagents · calls/turn = API calls per prompt · ctx/call = average context carried by each main-agent call · main edits = files the main agent edited itself (Edit/Write) · rtk = Bash calls filtered by nxy · ${rtkSavedNote} · nxy = sessions that ran with the plugin${cfg.metrics.subscription ? ' · USD~ = equivalent at API prices (subscription)' : ''}${unknownNote(total)}`);
console.log(`total: ${fmtTokens(rows.reduce((n, r) => n + r.totalTokens, 0))} tokens · ${fmtUsdPartial(total.usd, total.usdPartial)} · ${fmtDuration(rows.reduce((n, r) => n + r.wallMs, 0))} wall`);
