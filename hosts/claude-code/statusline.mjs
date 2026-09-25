#!/usr/bin/env node
// @ts-check
/**
 * Claude Code statusline: directory · branch, model · effort, context (gauge, %, absolute tokens,
 * coloured by token thresholds), the cost of the current turn (the hand-off signal), the
 * session (USD, tokens, subagents), prompt-cache health — or, once the cache has gone cold,
 * what the next request will re-cache and what that costs — and subscription windows.
 *
 * Reads the JSON Claude Code passes on stdin and folds only the bytes appended to the
 * transcripts since the last run (state cached per session in ~/.nxy/cache). Anything
 * missing or unknown renders as `?` — never a made-up number.
 *
 * Presentation is role-based (like gentle-shell's bar): every piece is painted by a role,
 * presets choose a palette, `theme` overrides any role, `layout` picks one or two lines.
 */
import { existsSync, readFileSync, readdirSync, realpathSync, renameSync, statSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { loadConfig } from '../../core/config.mjs';
import { colorize, fmtDuration, fmtPct, fmtTokens, fmtUsd, fmtUsdPartial, gauge, severity } from '../../core/format.mjs';
import { ensureDir, gitBranch, nxyUserDir, toNativePath } from '../../core/paths.mjs';
import { costFor, emptyUsage, totalInput } from '../../core/pricing.mjs';
import { newIncrementalState, readIncremental } from './transcripts.mjs';

/**
 * Presets: a palette per role. `label`/`value` are appended to a segment's own style, so a
 * segment hue (vivid) combines with bold values; classic keeps labels muted and values bold.
 * Styles are strings for {@link colorize}: names, `bold`/`dim`, palette 0–255, `#rrggbb`,
 * `bg:<colour>`.
 */
export const PRESETS = {
  classic: {
    brand: 'bold 80', separator: 'dim', path: 'muted', branch: 'bold', model: 'text', effort: '110',
    where: '', ctx: '', turn: '', session: '', cache: '', limits: '',
    label: 'muted', value: 'bold', gauge: '80', gaugeEmpty: 'dim', warn: 'bold yellow', crit: 'bold red',
    pad: '', gap: ' ',
  },
  vivid: {
    brand: 'bold 213', separator: 'dim', path: '114', branch: 'bold 114', model: '141', effort: 'bold 141',
    where: '114', ctx: '75', turn: '252', session: '80', cache: '176', limits: '245',
    label: '', value: 'bold', gauge: '75', gaugeEmpty: 'dim', warn: 'bold 214', crit: 'bold 203',
    pad: '', gap: ' ',
  },
  powerline: {
    brand: 'bold 16 bg:80', separator: '', path: '253 bg:238', branch: 'bold 255 bg:238', model: '253 bg:60', effort: 'bold 255 bg:60',
    where: 'bg:238', ctx: '253 bg:24', turn: '253 bg:94', session: '253 bg:23', cache: '253 bg:53', limits: '250 bg:236',
    label: '', value: 'bold', gauge: '117', gaugeEmpty: '245', warn: 'bold 220', crit: 'bold 203',
    pad: ' ', gap: ' ',
  },
};
export const DEFAULT_THEME = PRESETS.classic;

/**
 * @typedef {object} SessionCache
 * @property {import('./transcripts.mjs').IncrementalState} main
 * @property {Record<string, import('./transcripts.mjs').IncrementalState>} agents
 */

/** @returns {SessionCache} */
function loadCache(path) {
  /** @type {SessionCache} */
  const cache = { main: newIncrementalState(), agents: {} };
  try {
    if (existsSync(path)) {
      const saved = JSON.parse(readFileSync(path, 'utf8'));
      // caches written by older versions lack the newer fields: fill them from a fresh state
      cache.main = { ...cache.main, ...(saved.main || {}) };
      for (const [k, v] of Object.entries(saved.agents || {})) cache.agents[k] = { ...newIncrementalState(), ...v };
    }
  } catch {
    /* start fresh */
  }
  return cache;
}

/**
 * Folds the new transcript bytes into the cache. A turn is a human prompt, which only the
 * main transcript has; each subagent keeps its own `turnUsd` for the parent's current turn
 * (calls at or after `main.turnStartTs`), so the turn total is main + Σ agents and survives
 * a cache rebuild without over-charging.
 * @param {string} transcript
 * @param {SessionCache} cache
 * @returns {number} number of subagent transcripts seen
 */
function fold(transcript, cache) {
  readIncremental(transcript, cache.main);
  let agentCount = 0;
  const agentsDir = transcript.replace(/\.jsonl$/, '') + '/subagents';
  if (!existsSync(agentsDir)) return 0;
  for (const n of readdirSync(agentsDir)) {
    if (!n.startsWith('agent-') || !n.endsWith('.jsonl')) continue;
    agentCount++;
    cache.agents[n] ||= newIncrementalState();
    const st = cache.agents[n];
    let size = 0;
    try {
      size = statSync(join(agentsDir, n)).size;
    } catch {
      continue; // the agent file can vanish between readdir and stat
    }
    if (size === st.offset && st.turnStartTs === cache.main.turnStartTs) continue;
    readIncremental(join(agentsDir, n), st, { turnSinceTs: cache.main.turnStartTs });
    if (st.lastTs !== null && (cache.main.lastTs === null || st.lastTs > cache.main.lastTs)) cache.main.lastTs = st.lastTs;
  }
  return agentCount;
}

/** Whole-file replace so a script cancelled mid-write never leaves truncated JSON behind. */
function writeAtomic(path, data) {
  const tmp = `${path}.${process.pid}.tmp`;
  writeFileSync(tmp, data);
  renameSync(tmp, path);
}

/**
 * Pure renderer: everything I/O-dependent is already in `input` and `cache`.
 * @param {any} input                 JSON Claude Code passes on stdin
 * @param {SessionCache} cache
 * @param {{agentCount: number, branch: string|null, now: number}} ctx
 * @param {import('../../core/config.mjs').NxyConfig} cfg
 */
export function render(input, cache, ctx, cfg) {
  const sl = cfg.metrics.statusline;
  const color = sl.color !== false;
  const preset = PRESETS[sl.preset] || PRESETS.classic;
  const theme = { ...preset, ...(sl.theme || {}) };
  const twoLine = sl.layout === 'two-line';
  const cells = twoLine ? 16 : 8;

  /**
   * A segment paints its parts with its own hue plus the label/value role; severity replaces
   * the value hue. `seg` is the segment role (where, ctx, turn…).
   * @param {string} seg
   */
  const painter = (seg) => ({
    /** @param {string} t */
    label: (t) => colorize(t, `${theme[seg] || ''} ${theme.label}`, color),
    /** @param {string} t @param {'warn'|'crit'|null} [sev] */
    value: (t, sev = null) => colorize(t, sev ? `${theme[seg] || ''} ${theme[sev]}` : `${theme[seg] || ''} ${theme.value}`, color),
    /** @param {string} role @param {string} t @param {'warn'|'crit'|null} [sev] */
    role: (role, t, sev = null) => colorize(t, sev ? `${theme[seg] || ''} ${theme[sev]}` : `${theme[seg] || ''} ${theme[role]}`, color),
    /** @param {string} t */
    raw: (t) => colorize(t, theme[seg] || '', color),
  });
  const dotIn = (p) => p.label(' · ');
  /** wrap a finished segment with the preset's padding (powerline pills) */
  const wrap = (seg, text) => (theme.pad ? `${colorize(theme.pad, theme[seg] || '', color)}${text}${colorize(theme.pad, theme[seg] || '', color)}` : text);

  const states = [cache.main, ...Object.values(cache.agents)];
  const tokensIn = states.reduce((n, s) => n + totalInput(s.usage), 0);
  const tokensOut = states.reduce((n, s) => n + s.usage.output, 0);
  const usdPartial = states.some((s) => s.usdUnknown > 0);
  const haveData = states.some((s) => s.calls > 0);
  const usd = haveData ? states.reduce((n, s) => n + s.usd, 0) : null;
  const sub = cfg.metrics.subscription ? '~' : ''; // "~" = USD-equivalent under a subscription
  const modelId = input.model?.id || cache.main.model || null;

  // --- brand · where · model -------------------------------------------------------------
  const brand = sl.brand ? wrap('brand', colorize(sl.brand, theme.brand, color)) : null;
  const cwd = typeof input.cwd === 'string' ? input.cwd : null;
  const pw = painter('where');
  // last path segment on either separator: node's basename only knows the host's own
  const dirName = cwd ? cwd.split(/[\\/]/).filter(Boolean).pop() || cwd : null;
  const where = cwd ? wrap('where', `${pw.role('path', dirName)}${ctx.branch ? `${pw.raw(' ')}${pw.role('branch', ctx.branch)}` : ''}`) : null;
  const pm = painter('model');
  const modelName = input.model?.display_name || modelId || '?';
  const effort = input.effort?.level;
  const modelSeg = wrap('model', `${pm.role('model', modelName)}${effort ? `${dotIn(pm)}${pm.role('effort', effort)}` : ''}`);

  // --- context: gauge + % + absolute tokens, severity by absolute tokens -------------------
  // Thresholds are absolute tokens, not a percentage: every call re-sends the whole context,
  // so the cost of a turn follows the token count regardless of the window size (200k or 1M).
  const cw = input.context_window || {};
  const ctxPct = typeof cw.used_percentage === 'number' ? cw.used_percentage : null;
  let ctxTokens = typeof cw.total_input_tokens === 'number' ? cw.total_input_tokens : null;
  if (ctxTokens === null && ctxPct !== null && typeof cw.context_window_size === 'number') ctxTokens = Math.round((ctxPct / 100) * cw.context_window_size);
  const pc = painter('ctx');
  let ctxSeg = `${pc.label('ctx')}${pc.raw(' ')}${pc.value('?%')}`;
  if (ctxPct !== null) {
    const sev = severity(ctxTokens, sl.ctxWarnTokens, sl.ctxCritTokens);
    const bar = gauge(ctxPct, cells);
    const filled = bar.replace(/▱+$/, '');
    const painted = pc.role('gauge', filled, sev) + pc.role('gaugeEmpty', bar.slice(filled.length));
    ctxSeg = `${pc.label('ctx')}${pc.raw(' ')}${painted}${pc.raw(' ')}${pc.value(`${Math.round(ctxPct)}%`, sev)}${ctxTokens !== null ? `${dotIn(pc)}${pc.value(fmtTokens(ctxTokens), sev)}` : ''}`;
  }
  ctxSeg = wrap('ctx', ctxSeg);

  // --- current turn: the number that says when to hand off and /clear --------------------
  const pt = painter('turn');
  let turnSeg = null;
  if (cache.main.turns > 0 && haveData) {
    const turnUsd = states.reduce((n, s) => n + (s.turnUsd || 0), 0);
    const turnUnknown = states.reduce((n, s) => n + (s.turnUsdUnknown || 0), 0);
    const sev = turnUnknown > 0 ? null : severity(turnUsd, sl.turnWarnUsd, sl.turnCritUsd);
    turnSeg = wrap('turn', `${pt.label('turno')}${pt.raw(' ')}${pt.value(fmtUsdPartial(turnUsd, turnUnknown > 0) + sub, sev)}`);
  }

  // --- session: cost + tokens + subagents ------------------------------------------------
  const ps = painter('session');
  const agents = ctx.agentCount ? `${dotIn(ps)}${ps.label(`+${ctx.agentCount} agent${ctx.agentCount > 1 ? 's' : ''}`)}` : '';
  const sessionSeg = wrap(
    'session',
    haveData
      ? `${ps.label('sesión')}${ps.raw(' ')}${ps.value(fmtUsdPartial(usd, usdPartial) + sub)}${dotIn(ps)}${ps.value(`${fmtTokens(tokensIn)}→${fmtTokens(tokensOut)}`)}${agents}`
      : ps.label('no calls yet'),
  );

  // --- prompt cache ----------------------------------------------------------------------
  // Claude Code reports the cache state itself (`warm`, `expires_at`, `recache_tokens_if_cold`)
  // and re-runs this script when the cache expires. Cold → show what the next request will
  // re-cache and what that costs (the price of resuming). Older Claude Code without
  // `prompt_cache`: fall back to "last call older than the TTL".
  const pk = painter('cache');
  const pcache = input.prompt_cache || null;
  const nowSec = ctx.now / 1000;
  let cold = false;
  let idleMs = null;
  if (pcache && typeof pcache.warm === 'boolean') {
    cold = !pcache.warm && pcache.caching_observed !== false;
    if (typeof pcache.expires_at === 'number') idleMs = Math.max(0, ctx.now - pcache.expires_at * 1000);
  } else if (cache.main.lastTs !== null) {
    idleMs = ctx.now - cache.main.lastTs;
    cold = idleMs >= Math.max(0, sl.promptCacheTtlMin) * 60_000;
  }
  let cacheSeg = null;
  if (cold) {
    const recache = typeof pcache?.recache_tokens_if_cold === 'number' ? pcache.recache_tokens_if_cold : ctxTokens;
    // the snowflake is drawn two cells wide by most terminal fonts: no space after it
    const parts = [];
    if (recache !== null) {
      parts.push(pk.value(`❄${fmtTokens(recache)}`, 'crit'));
      const ttl1h = pcache?.ttl === '1h';
      const price = modelId ? costFor(modelId, { ...emptyUsage(), [ttl1h ? 'cacheWrite1h' : 'cacheWrite5m']: recache }) : null;
      if (price !== null) parts.push(pk.value(fmtUsd(price) + sub, 'crit'));
    } else parts.push(pk.value(`❄${idleMs !== null ? fmtDuration(idleMs) : ''}`, 'crit'));
    cacheSeg = wrap('cache', parts.join(pk.raw(' ')));
  } else if (pcache && typeof pcache.hit_ratio === 'number') {
    const left = typeof pcache.expires_at === 'number' ? Math.max(0, pcache.expires_at - nowSec) * 1000 : null;
    cacheSeg = wrap('cache', `${pk.label('cache')}${pk.raw(' ')}${pk.value(fmtPct(pcache.hit_ratio * 100))}${left !== null && left >= 60_000 ? `${dotIn(pk)}${pk.value(fmtDuration(left))}` : ''}`);
  }

  // --- subscription windows --------------------------------------------------------------
  const pl = painter('limits');
  const rl = input.rate_limits || {};
  const windows = [];
  if (typeof rl.five_hour?.used_percentage === 'number') windows.push(`${pl.label('5h')}${pl.raw(' ')}${pl.value(`${Math.round(rl.five_hour.used_percentage)}%`)}`);
  if (typeof rl.seven_day?.used_percentage === 'number') windows.push(`${pl.label('7d')}${pl.raw(' ')}${pl.value(`${Math.round(rl.seven_day.used_percentage)}%`)}`);
  if (typeof rl.spend_limit?.used_percentage === 'number') windows.push(`${pl.label('spend')}${pl.raw(' ')}${pl.value(`${Math.round(rl.spend_limit.used_percentage)}%`)}`);
  const limitsSeg = windows.length ? wrap('limits', windows.join(dotIn(pl))) : null;

  // --- layout ----------------------------------------------------------------------------
  const sepText = sl.separator ?? (theme.pad ? '' : '⟡');
  const sep = sepText ? `${theme.gap}${colorize(sepText, theme.separator, color)}${theme.gap}` : theme.gap;
  const line = (segs) => segs.filter(Boolean).join(sep);
  if (twoLine) return [line([brand, where, modelSeg, cacheSeg, limitsSeg]), line([ctxSeg, turnSeg, sessionSeg])].filter((l) => l).join('\n');
  return line([brand, where, modelSeg, ctxSeg, turnSeg, sessionSeg, cacheSeg, limitsSeg]);
}

/** Entry point; also called by the ~/.nxy/statusline.mjs shim that `/nxy:statusline --apply` writes. */
export function main() {
  let input = {};
  try {
    input = JSON.parse(readFileSync(0, 'utf8'));
  } catch {
    /* render with placeholders */
  }
  const cwd = typeof input.cwd === 'string' ? toNativePath(input.cwd) : process.cwd();
  const cfg = loadConfig(cwd);
  const sessionId = typeof input.session_id === 'string' ? input.session_id : 'unknown';
  const transcript = typeof input.transcript_path === 'string' ? toNativePath(input.transcript_path) : null;

  const cacheDir = ensureDir(join(nxyUserDir(), 'cache'));
  const cachePath = join(cacheDir, `statusline-${sessionId.replace(/[^A-Za-z0-9_-]/g, '_')}.json`);
  const cache = loadCache(cachePath);
  try {
    // last payload Claude Code sent, for support ('why is X not showing?') and a future nxy doctor
    writeAtomic(join(cacheDir, 'statusline-input.json'), JSON.stringify(input, null, 2));
  } catch {
    /* optional */
  }
  let agentCount = 0;
  if (transcript && existsSync(transcript)) {
    agentCount = fold(transcript, cache);
    try {
      writeAtomic(cachePath, JSON.stringify(cache));
    } catch {
      /* cache is optional */
    }
  }
  process.stdout.write(render(input, cache, { agentCount, branch: gitBranch(cwd), now: Date.now() }, cfg));
}

// run only when executed directly — the ~/.nxy/statusline.mjs shim imports `main` instead
function runDirectly() {
  if (!process.argv[1]) return false;
  try {
    return import.meta.url === pathToFileURL(realpathSync(process.argv[1])).href;
  } catch {
    return false;
  }
}
if (runDirectly()) {
  try {
    main();
  } catch (err) {
    process.stdout.write('nxy ?');
    if (process.env.NXY_DEBUG) process.stderr.write(`[nxy] ${String((err instanceof Error && err.stack) || err)}\n`);
  }
}
