#!/usr/bin/env node
// @ts-check
/**
 * Claude Code statusline: one line with model · effort, context %, real tokens and
 * USD-equivalent of the session (main + subagents), cache health and subscription windows.
 *
 * Reads the JSON Claude Code passes on stdin and folds only the bytes appended to the
 * transcripts since the last run (state cached per session in ~/.nxy/cache). Anything
 * missing or unknown renders as `?` — never a made-up number.
 */
import { existsSync, readFileSync, readdirSync, statSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { loadConfig } from '../lib/config.mjs';
import { fmtPct, fmtTokens, fmtUsdPartial, gauge } from '../lib/format.mjs';
import { ensureDir, nxyUserDir } from '../lib/paths.mjs';
import { totalInput } from '../lib/pricing.mjs';
import { newIncrementalState, readIncremental } from '../lib/transcripts.mjs';

const SEP = ' ⟡ ';

function main() {
  let input = {};
  try {
    input = JSON.parse(readFileSync(0, 'utf8'));
  } catch {
    /* render with placeholders */
  }
  const cwd = typeof input.cwd === 'string' ? input.cwd : process.cwd();
  const cfg = loadConfig(cwd);
  const sessionId = typeof input.session_id === 'string' ? input.session_id : 'unknown';
  const transcript = typeof input.transcript_path === 'string' ? input.transcript_path : null;

  // --- session totals (main + subagents), incremental -------------------------------------
  const cacheDir = ensureDir(join(nxyUserDir(), 'cache'));
  const cachePath = join(cacheDir, `statusline-${sessionId.replace(/[^A-Za-z0-9_-]/g, '_')}.json`);
  let cache = { main: newIncrementalState(), agents: /** @type {Record<string, ReturnType<typeof newIncrementalState>>} */ ({}) };
  try {
    if (existsSync(cachePath)) cache = { ...cache, ...JSON.parse(readFileSync(cachePath, 'utf8')) };
  } catch {
    /* start fresh */
  }
  let agentCount = 0;
  if (transcript && existsSync(transcript)) {
    readIncremental(transcript, cache.main);
    const agentsDir = transcript.replace(/\.jsonl$/, '') + '/subagents';
    if (existsSync(agentsDir)) {
      for (const n of readdirSync(agentsDir)) {
        if (!n.startsWith('agent-') || !n.endsWith('.jsonl')) continue;
        agentCount++;
        cache.agents[n] ||= newIncrementalState();
        const st = cache.agents[n];
        if (statSync(join(agentsDir, n)).size !== st.offset) readIncremental(join(agentsDir, n), st);
      }
    }
    try {
      writeFileSync(cachePath, JSON.stringify(cache));
    } catch {
      /* cache is optional */
    }
  }
  const states = [cache.main, ...Object.values(cache.agents)];
  const tokensIn = states.reduce((n, s) => n + totalInput(s.usage), 0);
  const tokensOut = states.reduce((n, s) => n + s.usage.output, 0);
  const usdPartial = states.some((s) => s.usdUnknown > 0);
  const haveData = states.some((s) => s.calls > 0);
  const usd = haveData ? states.reduce((n, s) => n + s.usd, 0) : null;

  // --- segments ---------------------------------------------------------------------------
  const model = input.model?.display_name || input.model?.id || cache.main.model || '?';
  const effort = input.effort?.level;
  const ctxPct = input.context_window?.used_percentage;
  const ctx = typeof ctxPct === 'number' ? `ctx ${gauge(ctxPct)} ${Math.round(ctxPct)}%` : 'ctx ?%';
  const tokens = haveData ? `${fmtTokens(tokensIn)}→${fmtTokens(tokensOut)}${agentCount ? ` (+${agentCount} agent${agentCount > 1 ? 's' : ''})` : ''}` : 'no calls yet';
  const cost = fmtUsdPartial(usd, usdPartial) + (cfg.metrics.subscription && usd !== null ? '~' : ''); // "~" = USD-equivalent under a subscription
  const hitRatio = input.prompt_cache?.hit_ratio;
  const cacheSeg = typeof hitRatio === 'number' ? `cache ${fmtPct(hitRatio * 100)}` : null;
  const rl = input.rate_limits || {};
  const windows = [];
  if (typeof rl.five_hour?.used_percentage === 'number') windows.push(`5h ${Math.round(rl.five_hour.used_percentage)}%`);
  if (typeof rl.seven_day?.used_percentage === 'number') windows.push(`7d ${Math.round(rl.seven_day.used_percentage)}%`);
  if (typeof rl.spend_limit?.used_percentage === 'number') windows.push(`spend ${Math.round(rl.spend_limit.used_percentage)}%`);

  const segments = [
    `${model}${effort ? ` · ${effort}` : ''}`,
    ctx,
    tokens,
    cost,
    cacheSeg,
    windows.length ? windows.join(' · ') : null,
  ].filter(Boolean);
  process.stdout.write(segments.join(SEP));
}

try {
  main();
} catch (err) {
  process.stdout.write('nxy ?');
  if (process.env.NXY_DEBUG) process.stderr.write(`[nxy] ${String((err instanceof Error && err.stack) || err)}\n`);
}
