#!/usr/bin/env node
// @ts-check
/**
 * `/nxy:stats` — real consumption of one session (default: the last one of this project),
 * read from the transcripts Claude Code already writes. Nothing is estimated except the
 * USD-equivalent, which is tokens × the official pricing table.
 *
 *   node stats.mjs [--session last|<id>] [--cwd <dir>] [--all] [--json] [--no-agents]
 */
import { existsSync } from 'node:fs';
import { join } from 'node:path';
import { loadConfig } from '../lib/config.mjs';
import { clip, fmtDate, fmtDuration, fmtPct, fmtTokens, fmtUsd, parseArgs, table } from '../lib/format.mjs';
import { readJsonl } from '../lib/jsonl.mjs';
import { nxyProjectDir } from '../lib/paths.mjs';
import { parseSession, resolveSession } from '../lib/transcripts.mjs';

const { opts, positional } = parseArgs(process.argv.slice(2));
const cwd = typeof opts.cwd === 'string' ? opts.cwd : process.cwd();
const cfg = loadConfig(cwd);
// Inside a live session Claude Code exports CLAUDE_CODE_SESSION_ID → "current" is the default there.
// Accepted: `--session X` or a bare positional (`/nxy:stats last`).
const currentId = process.env.CLAUDE_CODE_SESSION_ID || null;
let selector = typeof opts.session === 'string' ? opts.session : positional[0] || (currentId ? 'current' : 'last');
const wantsCurrent = selector === 'current';
if (wantsCurrent) selector = currentId || 'last';

const ref = resolveSession(selector, { cwd, all: Boolean(opts.all), projectsDir: cfg.metrics.projectsDir });
if (!ref) {
  if (wantsCurrent || selector === currentId) {
    // Brand-new session: Claude Code creates the transcript with the first API response.
    console.log('nxy stats: this session has no API calls yet — the first one is the answer to this command. Run /nxy:stats again on the next turn, or `/nxy:stats last` for the previous session.');
    process.exit(0);
  }
  console.error(`nxy: no session found (${selector}) for ${cwd}. Try --all or --session <id>.`);
  process.exit(1);
}
const s = parseSession(ref, { includeSubagents: !opts['no-agents'], cacheBreakThreshold: cfg.metrics.cacheBreakThreshold });
if (!s.usage.calls && !opts.json) {
  console.log(`nxy stats — session ${s.sessionId.slice(0, 8)}: no API calls recorded yet (${s.turns} prompt${s.turns === 1 ? '' : 's'} sent). Numbers appear from the next turn on.`);
  process.exit(0);
}

// Filter rows for this session (written by the PostToolUse hook).
const filterPath = join(nxyProjectDir(s.cwd || cwd), 'metrics', 'filter.jsonl');
const filterRows = existsSync(filterPath) ? readJsonl(filterPath, (r) => r.session_id === s.sessionId) : [];
const filter = summarizeFilter(filterRows);

if (opts.json) {
  console.log(JSON.stringify({ ...s, filter }, null, 2));
  process.exit(0);
}

const usdLabel = cfg.metrics.subscription ? 'USD-equiv' : 'USD';
const out = [];
out.push(`nxy stats — session ${s.sessionId.slice(0, 8)} · ${s.project}${s.gitBranch ? ` · ${s.gitBranch}` : ''}${s.version ? ` · Claude Code ${s.version}` : ''}`);
out.push(`started ${fmtDate(s.firstTs)} · wall ${fmtDuration(s.wallMs)} · active ${fmtDuration(s.activeMs)} · turns ${s.turns} · API calls ${s.usage.calls} (main ${s.main.calls}, subagents ${s.subagents.calls} in ${s.agents.length} agents)`);
out.push('');

const u = s.usage.usage;
out.push(
  table(
    [
      { k: 'input (uncached)', t: u.input, m: s.main.usage.input, a: s.subagents.usage.input },
      { k: 'cache write 5m', t: u.cacheWrite5m, m: s.main.usage.cacheWrite5m, a: s.subagents.usage.cacheWrite5m },
      { k: 'cache write 1h', t: u.cacheWrite1h, m: s.main.usage.cacheWrite1h, a: s.subagents.usage.cacheWrite1h },
      { k: 'cache read', t: u.cacheRead, m: s.main.usage.cacheRead, a: s.subagents.usage.cacheRead },
      { k: 'output', t: u.output, m: s.main.usage.output, a: s.subagents.usage.output },
      { k: '  of which thinking', t: u.thinking, m: s.main.usage.thinking, a: s.subagents.usage.thinking },
      { k: 'cache hit', t: fmtPct(s.usage.cacheHitPct), m: fmtPct(s.main.cacheHitPct), a: fmtPct(s.subagents.cacheHitPct), raw: true },
      { k: 'context peak', t: s.usage.contextPeak, m: s.main.contextPeak, a: s.subagents.contextPeak },
      { k: usdLabel, t: fmtUsd(s.usage.usd), m: fmtUsd(s.main.usd), a: fmtUsd(s.subagents.usd), raw: true },
    ],
    [
      { key: 'k', label: 'TOKENS' },
      { key: 't', label: 'total', align: 'right', fmt: (v, r) => (r.raw ? v : fmtTokens(v)) },
      { key: 'm', label: 'main', align: 'right', fmt: (v, r) => (r.raw ? v : fmtTokens(v)) },
      { key: 'a', label: 'subagents', align: 'right', fmt: (v, r) => (r.raw ? v : fmtTokens(v)) },
    ],
  ),
);
if (s.unknownModels.length) out.push(`(no pricing for: ${s.unknownModels.join(', ')} — ${usdLabel} shown as ? where affected)`);
out.push('');

const bucketCols = (first) => [
  first,
  { key: 'calls', label: 'calls', align: 'right' },
  { key: 'input', label: 'input', align: 'right', fmt: fmtTokens },
  { key: 'cacheW', label: 'cache-w', align: 'right', fmt: fmtTokens },
  { key: 'cacheR', label: 'cache-r', align: 'right', fmt: fmtTokens },
  { key: 'output', label: 'output', align: 'right', fmt: fmtTokens },
  { key: 'hit', label: 'hit', align: 'right', fmt: (v) => fmtPct(v) },
  { key: 'usd', label: usdLabel, align: 'right', fmt: fmtUsd },
];
const bucketRow = (name, b, extra = {}) => ({
  name, calls: b.calls, input: b.usage.input, cacheW: b.usage.cacheWrite5m + b.usage.cacheWrite1h, cacheR: b.usage.cacheRead, output: b.usage.output, hit: b.cacheHitPct, usd: b.usd, ...extra,
});
const byUsd = (a, b) => (b.usd ?? 0) - (a.usd ?? 0) || b.calls - a.calls;

out.push('BY MODEL');
out.push(table(Object.entries(s.byModel).map(([m, b]) => bucketRow(m, b)).sort(byUsd), bucketCols({ key: 'name', label: 'model' })));
out.push('');

if (Object.keys(s.byAgentType).length) {
  out.push('BY AGENT TYPE');
  out.push(table(Object.entries(s.byAgentType).map(([t, b]) => bucketRow(t, b, { agents: b.agents })).sort(byUsd), [
    { key: 'name', label: 'type' }, { key: 'agents', label: 'agents', align: 'right' }, ...bucketCols({ key: 'name', label: '' }).slice(1),
  ]));
  out.push('');
  out.push('AGENTS');
  out.push(table(s.agents.slice(0, 20).map((a) => ({
    type: a.agentType, desc: clip(a.description, 40), model: a.model || '?', tokens: a.totalInput + a.usage.output, usd: a.usd, dur: a.durationMs,
  })), [
    { key: 'type', label: 'type' }, { key: 'desc', label: 'description' }, { key: 'model', label: 'model' },
    { key: 'tokens', label: 'tokens', align: 'right', fmt: fmtTokens }, { key: 'usd', label: usdLabel, align: 'right', fmt: fmtUsd }, { key: 'dur', label: 'time', align: 'right', fmt: fmtDuration },
  ]));
  out.push('');
}

if (Object.keys(s.bySkill).length) {
  out.push('BY SKILL / COMMAND');
  out.push(table(Object.entries(s.bySkill).map(([k, b]) => bucketRow(k, b)).sort(byUsd), bucketCols({ key: 'name', label: 'skill' })));
  out.push('');
}

const toolList = Object.entries(s.tools.calls).sort((a, b) => b[1] - a[1]).map(([k, v]) => `${k} ${v}`).join(' · ');
out.push(`TOOLS: ${toolList || 'none'}`);
out.push(`  files read (distinct): ${s.tools.filesRead.length} · Bash calls ${s.tools.bash.calls} → ${fmtTokens(s.tools.bash.resultLines)} lines / ${fmtTokens(s.tools.bash.resultChars)} chars back${s.tools.bash.interrupted ? ` · ${s.tools.bash.interrupted} interrupted` : ''}`);
out.push('');

out.push(`FILTER (nxy hook rows for this session): ${filter.rows}`);
if (filter.rows) {
  out.push(`  engine: ${Object.entries(filter.byEngine).map(([k, v]) => `${k} ${v}`).join(' · ')} · Bash output seen by the model: ${fmtTokens(filter.filteredChars)} chars (~${fmtTokens(filter.estTokens)} tokens)`);
  if (filter.skipReasons.length) out.push(`  skip reasons: ${filter.skipReasons.map(([k, v]) => `${k} ${v}`).join(' · ')}`);
  out.push(table(filter.byKind.slice(0, 12), [
    { key: 'kind', label: 'command kind' }, { key: 'rows', label: 'calls', align: 'right' }, { key: 'rtk', label: 'via rtk', align: 'right' },
    { key: 'chars', label: 'chars', align: 'right', fmt: fmtTokens }, { key: 'tokens', label: '~tokens', align: 'right', fmt: fmtTokens },
  ]));
}
out.push('');
if (s.cacheBreaks.length) {
  out.push(`CACHE BREAKS (> ${fmtTokens(cfg.metrics.cacheBreakThreshold)} uncached in one call): ${s.cacheBreaks.length}`);
  for (const cb of s.cacheBreaks.slice(0, 5)) out.push(`  ${fmtDate(cb.ts)} · ${fmtTokens(cb.uncached)} uncached of ${fmtTokens(cb.total)}${cb.agentType ? ` · agent ${cb.agentType}` : ''}`);
} else {
  out.push('CACHE BREAKS: none');
}
console.log(out.join('\n'));

function summarizeFilter(rows) {
  const byEngine = {};
  const skip = {};
  const kinds = {};
  let filteredChars = 0;
  let estTokens = 0;
  for (const r of rows) {
    byEngine[r.engine] = (byEngine[r.engine] || 0) + 1;
    if (r.engine === 'skip') skip[r.reason] = (skip[r.reason] || 0) + 1;
    filteredChars += r.filtered_chars || 0;
    estTokens += r.est_tokens || 0;
    const k = (kinds[r.command_kind] ||= { kind: r.command_kind, rows: 0, rtk: 0, chars: 0, tokens: 0 });
    k.rows++;
    if (r.engine === 'rtk') k.rtk++;
    k.chars += r.filtered_chars || 0;
    k.tokens += r.est_tokens || 0;
  }
  return {
    rows: rows.length,
    byEngine,
    skipReasons: Object.entries(skip).sort((a, b) => b[1] - a[1]),
    filteredChars,
    estTokens,
    byKind: Object.values(kinds).sort((a, b) => b.chars - a.chars),
  };
}
