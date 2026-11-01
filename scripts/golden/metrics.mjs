// @ts-check
/**
 * Golden of the 1.x metrics (`core/metrics/aggregate.mjs`, `cache-ttl.mjs`, `timing.mjs` and the
 * cache-break causes of `hosts/claude-code/transcripts.mjs`). Sets: buckets, ttl, breaks, timing.
 * Timestamps are all `Z` / offset ones, so nothing depends on the machine's time zone.
 *
 *   node scripts/golden/metrics.mjs          write testdata/golden/metrics/*.json
 *   node scripts/golden/metrics.mjs --check  fail when they differ
 */
import { fileURLToPath } from 'node:url';
import { toUsage } from '../../core/pricing.mjs';
import { addCall, finishBucket, newBucket } from '../../core/metrics/aggregate.mjs';
import { hourTtlWhatIf, sumTtlWhatIf } from '../../core/metrics/cache-ttl.mjs';
import { agentTiming, quantile, summarizeTiming } from '../../core/metrics/timing.mjs';
import { breakCause, summarizeBreaks } from '../../hosts/claude-code/transcripts.mjs';
import { diffArea, loadCorpus, staleSets, writeArea } from './lib.mjs';
import { fixtureTexts } from './fixtures.mjs';

export const AREA = 'metrics';
const BASE_TS = Date.parse('2026-09-01T10:00:00.000Z');

/**
 * @param {any[]} raw  corpus calls
 * @returns {any[]} Call records, usage normalised
 */
function buildCalls(raw) {
  return raw.map((c, i) => ({
    key: c.key ?? `req_${i}`,
    ts: c.ts ?? BASE_TS + i * 1000,
    model: c.model,
    usage: toUsage(c.usage),
    effort: c.effort ?? null,
    skill: c.skill ?? null,
    promptKey: c.promptKey ?? null,
    agentId: c.agentId ?? null,
    gapMs: c.gapMs ?? null,
    after: c.after ?? null,
  }));
}

/** Deterministic pseudo-random calls: many odd counts over several models, to pin the float sums. */
function lcgScenario() {
  let s = 20260901;
  const next = (/** @type {number} */ n) => {
    s = (Math.imul(s, 1103515245) + 12345) >>> 0;
    return s % n;
  };
  const models = ['claude-opus-5', 'claude-sonnet-5', 'claude-haiku-4-5', 'claude-haiku-5-5', 'claude-opus-4-9', 'claude-future-9', 'claude-opus-5-5'];
  const gaps = [null, 500, 4000, 299999, 300000, 400000, 3599999, 3600000, 9000000];
  const afters = [null, null, null, 'compact', 'subagent'];
  /** @type {any[]} */
  const calls = [];
  for (let i = 0; i < 60; i++) {
    const w5 = next(3) === 0 ? 0 : next(90000);
    const w1 = next(4) === 0 ? next(20000) : 0;
    calls.push({
      model: models[next(models.length)],
      gapMs: i === 0 ? null : gaps[next(gaps.length)],
      after: afters[next(afters.length)],
      usage: {
        input_tokens: next(5000),
        cache_creation_input_tokens: w5 + w1,
        cache_read_input_tokens: next(150000),
        output_tokens: next(4000),
        speed: next(6) === 0 ? 'fast' : 'standard',
        cache_creation: { ephemeral_5m_input_tokens: w5, ephemeral_1h_input_tokens: w1 },
        output_tokens_details: { thinking_tokens: next(500) },
      },
    });
  }
  return calls;
}

/** @param {string} text @returns {any[]} */
const linesOf = (text) => text.split('\n').filter(Boolean).map((l) => JSON.parse(l));

/** @returns {Record<string, any[]>} */
export function generate() {
  /** @type {any} */
  const corpus = loadCorpus('metrics');
  const scenarios = [...corpus.scenarios, { name: 'lcg-60', calls: lcgScenario() }];

  const buckets = [];
  const ttl = [];
  for (const sc of scenarios) {
    const calls = buildCalls(sc.calls);
    const b = newBucket();
    for (const c of calls) addCall(b, c);
    buckets.push({ name: `bucket:${sc.name}`, input: { calls }, output: { bucket: finishBucket(b) } });
    ttl.push({ name: `whatif:${sc.name}`, input: { calls }, output: { whatIf: hourTtlWhatIf(calls) } });
  }
  for (const s of corpus.ttlSums) ttl.push({ name: `sum:${s.name}`, input: { parts: s.parts }, output: { sum: sumTtlWhatIf(s.parts) } });

  const breaks = [];
  for (const after of [null, 'compact', 'subagent']) {
    for (const gapMs of [null, 0, 299999, 300000, 3599999, 3600000, 7200000]) {
      for (const w1h of [0, 5]) {
        const [call] = buildCalls([{ model: 'claude-opus-5', gapMs, after, usage: { input_tokens: 10, cache_creation_input_tokens: w1h, cache_creation: { ephemeral_5m_input_tokens: 0, ephemeral_1h_input_tokens: w1h } } }]);
        breaks.push({ name: `cause:after=${after},gap=${gapMs},1h=${w1h}`, input: { call }, output: { cause: breakCause(call) } });
      }
    }
  }
  for (const l of corpus.breakLists) breaks.push({ name: `summary:${l.name}`, input: { breaks: l.breaks }, output: { summary: summarizeBreaks(l.breaks) } });

  const timing = [];
  for (const t of corpus.timing) timing.push({ name: `events:${t.name}`, input: { events: t.events }, output: { timing: agentTiming(t.events) } });
  corpus.limitTexts.forEach((/** @type {string} */ text, /** @type {number} */ i) => {
    const events = [
      { type: 'user', timestamp: '2026-09-01T10:00:00.000Z', message: { content: 'go' } },
      { type: 'assistant', timestamp: '2026-09-01T10:00:10.000Z', requestId: 'r1', message: { model: '<synthetic>', content: [{ type: 'text', text }] } },
      { type: 'assistant', timestamp: '2026-09-01T10:00:30.000Z', requestId: 'r2', message: { model: 'claude-sonnet-5', content: [] } },
    ];
    timing.push({ name: `events:limit-text:${String(i).padStart(2, '0')}`, input: { events }, output: { timing: agentTiming(events) } });
  });

  const texts = fixtureTexts();
  /** @type {{agentType: string, timing: any}[]} */
  const fixtureRuns = [];
  for (const [rel, text] of Object.entries(texts)) {
    if (!rel.endsWith('.jsonl')) continue;
    const events = linesOf(text);
    const t = agentTiming(events);
    timing.push({ name: `events:fixture:${rel}`, input: { events }, output: { timing: t } });
    const m = /^(.*)\/subagents\/(agent-.*)\.jsonl$/.exec(rel);
    if (m && rel.startsWith('-home-dev-code-my-app/sess-timing/')) {
      const meta = JSON.parse(texts[`${m[1]}/subagents/${m[2]}.meta.json`]);
      fixtureRuns.push({ agentType: meta.agentType, timing: t });
    }
  }
  timing.push({ name: 'runs:fixture-runs', input: { runs: fixtureRuns }, output: { summary: summarizeTiming(fixtureRuns) } });
  for (const r of corpus.timingRuns) timing.push({ name: `runs:${r.name}`, input: { runs: r.runs }, output: { summary: summarizeTiming(r.runs) } });
  for (const q of corpus.quantiles) timing.push({ name: `quantile:${q.name}`, input: { sorted: q.sorted, p: q.p }, output: { quantile: quantile(q.sorted, q.p) } });

  return { buckets, ttl, breaks, timing };
}

/** @returns {string[]} differences, empty when up to date */
export function check() {
  const sets = generate();
  const diffs = [];
  for (const [set, cases] of Object.entries(sets)) diffs.push(...diffArea(AREA, set, cases));
  diffs.push(...staleSets(AREA, Object.keys(sets)));
  return diffs;
}

export function write() {
  return Object.entries(generate()).map(([set, cases]) => writeArea(AREA, set, cases));
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  if (process.argv.includes('--check')) {
    const diffs = check();
    for (const d of diffs) console.error(d);
    if (diffs.length) process.exit(1);
    console.log('golden files up to date');
  } else {
    for (const p of write()) console.log(`wrote ${p}`);
  }
}
