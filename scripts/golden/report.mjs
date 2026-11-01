// @ts-check
/**
 * Golden of the 1.x text helpers and ledger maths that `internal/report` ports: formatBreaks,
 * formatTtlWhatIf, formatAgentTiming, summarizeBroadSearches, the rtk ledger helpers (path
 * normalisation, underProject, sumSavings, savedPct, sessionSavings, dailyFromGain) and the
 * rtk history db path. Sets: breaks, ttlline, timing, broad, ledger, dailyfromgain, dbpath.
 * The ledger helpers that depend on the platform run for win32 and linux, passed explicitly (or
 * through process.platform, switched while sessionSavings runs), so the files do not depend on
 * the OS that generates them. The db paths are written with forward slashes for the same reason.
 *
 *   node scripts/golden/report.mjs          write testdata/golden/report/*.json
 *   node scripts/golden/report.mjs --check  fail when they differ
 */
import { fileURLToPath } from 'node:url';
import { fmtTokens } from '../../core/format.mjs';
import { formatAgentTiming, summarizeTiming } from '../../core/metrics/timing.mjs';
import { dailyFromGain, normalizeProjectPath, rtkHistoryDbPath, savedPct, sessionSavings, sumSavings, underProject } from '../../core/filter/ledger.mjs';
import { formatBreaks, formatTtlWhatIf, summarizeBreaks } from '../../hosts/claude-code/transcripts.mjs';
import { summarizeBroadSearches } from '../../hosts/claude-code/search-history.mjs';
import { render } from '../../hosts/claude-code/statusline.mjs';
import { newIncrementalState } from '../../hosts/claude-code/transcripts.mjs';
import { diffArea, loadCorpus, staleSets, writeArea } from './lib.mjs';
import { generate as generateMetrics } from './metrics.mjs';

export const AREA = 'report';
/** @type {NodeJS.Platform[]} */
const PLATFORMS = ['win32', 'linux'];

const idx = (/** @type {number} */ i) => String(i).padStart(3, '0');

/**
 * Runs fn with process.platform replaced (sessionSavings reads it as a default parameter).
 * @template T
 * @param {string} platform
 * @param {() => T} fn
 * @returns {T}
 */
function onPlatform(platform, fn) {
  const real = Object.getOwnPropertyDescriptor(process, 'platform');
  Object.defineProperty(process, 'platform', { value: platform, configurable: true });
  try {
    return fn();
  } finally {
    if (real) Object.defineProperty(process, 'platform', real);
  }
}

/** @returns {Record<string, any[]>} */
export function generate() {
  /** @type {any} */
  const c = loadCorpus('report');
  /** @type {any} */
  const mc = loadCorpus('metrics');
  const metrics = generateMetrics();

  const breaks = [...mc.breakLists, ...c.breakLists].map((/** @type {any} */ l) => ({
    name: `breaks:${l.name}`,
    input: { breaks: l.breaks, totals: c.breakTotals },
    output: { lines: c.breakTotals.map((/** @type {number} */ t) => formatBreaks(summarizeBreaks(l.breaks), t, fmtTokens)) },
  }));

  /** @type {{name: string, w: any}[]} */
  const whatIfs = [...c.ttlWhatIf];
  for (const t of metrics.ttl) if (t.output.whatIf) whatIfs.push({ name: t.name, w: t.output.whatIf });
  const ttlline = whatIfs.map((x, i) => ({ name: `ttlline:${idx(i)}:${x.name}`, input: { w: x.w }, output: { line: formatTtlWhatIf(x.w) } }));

  /** @type {{name: string, summary: any}[]} */
  const summaries = [
    { name: 'null', summary: null },
    { name: 'no-types', summary: { types: [], slowest: [], limitStops: 0 } },
  ];
  for (const t of [...mc.timingRuns, ...c.timing]) summaries.push({ name: t.name, summary: summarizeTiming(t.runs) });
  for (const t of metrics.timing) if (t.name.startsWith('runs:')) summaries.push({ name: t.name, summary: t.output.summary });
  const timing = summaries.map((s, i) => ({ name: `timing:${idx(i)}:${s.name}`, input: { summary: s.summary }, output: { lines: formatAgentTiming(s.summary) } }));

  const broad = c.broad.map((/** @type {any} */ b) => ({
    name: `broad:${b.name}`,
    input: { rows: b.rows, thresholds: c.broadThresholds },
    output: { summaries: c.broadThresholds.map((/** @type {number} */ th) => summarizeBroadSearches(b.rows, th)) },
  }));

  /** @type {any[]} */
  const ledger = [
    {
      name: 'norm',
      input: { paths: c.ledgerPaths },
      output: Object.fromEntries(PLATFORMS.map((p) => [p, c.ledgerPaths.map((/** @type {string} */ s) => normalizeProjectPath(s, p))])),
    },
    {
      name: 'under',
      input: { pairs: c.underPairs },
      output: Object.fromEntries(PLATFORMS.map((p) => [p, c.underPairs.map((/** @type {[string, string]} */ [row, cwd]) => underProject(row, cwd, p))])),
    },
  ];
  for (const l of c.ledgerRows) {
    const sum = sumSavings(l.rows);
    ledger.push({ name: `sum:${l.name}`, input: { rows: l.rows }, output: { sum, pct: savedPct(sum) } });
    for (const s of c.sessions) {
      ledger.push({
        name: `session:${l.name}:${s.name}`,
        input: { rows: l.rows, session: { cwd: s.cwd, firstTs: s.firstTs, lastTs: s.lastTs } },
        output: Object.fromEntries(PLATFORMS.map((p) => [p, onPlatform(p, () => sessionSavings(l.rows, { cwd: s.cwd, firstTs: s.firstTs, lastTs: s.lastTs }))])),
      });
    }
  }

  const dailyfromgain = c.gains.map((/** @type {any} */ g) => ({
    name: `gain:${g.name}`,
    input: { gain: g.gain },
    output: { daily: [...dailyFromGain(g.gain)] },
  }));

  const slashes = (/** @type {string} */ p) => p.replace(/\\/g, '/');
  const dbpath = [];
  for (const e of c.dbEnvs) {
    dbpath.push({
      name: `dbpath:${e.name}`,
      input: { env: e.env, platforms: c.dbPlatforms },
      output: { paths: c.dbPlatforms.map((/** @type {NodeJS.Platform} */ p) => slashes(rtkHistoryDbPath(e.env, p))) },
    });
  }

  const sl = statuslineCases();
  return { breaks, ttlline, timing, broad, ledger, dailyfromgain, dbpath, statusline: sl.cases, 'statusline-bases': sl.bases };
}

const NOW = 1_800_000_000_000;
const SL_DEFAULTS = { color: true, preset: 'vivid', layout: 'line', brand: '◆ nxy', ctxWarnTokens: 100000, ctxCritTokens: 200000, turnWarnUsd: 1, turnCritUsd: 3, promptCacheTtlMin: 5, theme: {} };
const U = (/** @type {any} */ o) => ({ input: 0, cacheWrite5m: 0, cacheWrite1h: 0, cacheRead: 0, output: 0, thinking: 0, ...o });
/** state overrides on top of newIncrementalState (the Go side fills the same defaults) */
const ST = (/** @type {any} */ o) => ({ usage: U({ input: 1200, cacheWrite5m: 30000, cacheRead: 250000, output: 4100 }), usd: 1.2345, calls: 12, turns: 3, turnUsd: 0.42, lastTs: NOW - 30_000, turnStartTs: NOW - 90_000, model: 'claude-opus-5', ...o });
const BASE_INPUT = {
  cwd: '/srv/u/proj',
  model: { id: 'claude-opus-5', display_name: 'Opus 5' },
  effort: { level: 'high' },
  context_window: { used_percentage: 42, total_input_tokens: 84000, context_window_size: 200000 },
  prompt_cache: { warm: true, hit_ratio: 0.93, expires_at: NOW / 1000 + 240 },
  rate_limits: { five_hour: { used_percentage: 37.5 }, seven_day: { used_percentage: 12 } },
};

/** the statusline renderer over scenarios x presets/layouts, then config variants */
function statuslineCases() {
  const noPc = (/** @type {any} */ o = {}) => ({ ...BASE_INPUT, prompt_cache: undefined, ...o });
  /** @type {{name: string, input: any, main?: any, agents?: Record<string, any>, agentCount?: number, branch?: string|null}[]} */
  const scenarios = [
    { name: 'warm', input: BASE_INPUT, branch: 'main' },
    { name: 'cold-recache', input: { ...BASE_INPUT, prompt_cache: { warm: false, recache_tokens_if_cold: 84000, expires_at: NOW / 1000 - 300 } }, branch: 'feat/x' },
    { name: 'cold-1h', input: { ...BASE_INPUT, prompt_cache: { warm: false, ttl: '1h', recache_tokens_if_cold: 350000 } } },
    { name: 'cold-ctx-tokens', input: { ...BASE_INPUT, prompt_cache: { warm: false } } },
    { name: 'cold-idle-only', input: { ...BASE_INPUT, context_window: undefined, prompt_cache: { warm: false, expires_at: NOW / 1000 - 4000 } } },
    { name: 'cold-nothing', input: { ...BASE_INPUT, context_window: undefined, prompt_cache: { warm: false } } },
    { name: 'cold-unobserved', input: { ...BASE_INPUT, prompt_cache: { warm: false, caching_observed: false } } },
    { name: 'cold-unknown-model', input: { ...BASE_INPUT, model: { id: 'mystery-model' }, prompt_cache: { warm: false, recache_tokens_if_cold: 5000 } }, main: { model: 'mystery-model', usdUnknown: 2 } },
    { name: 'warm-short', input: { ...BASE_INPUT, prompt_cache: { warm: true, hit_ratio: 0.5, expires_at: NOW / 1000 + 30 } } },
    { name: 'warm-no-hit', input: { ...BASE_INPUT, prompt_cache: { warm: true } } },
    { name: 'hit-no-warm', input: { ...BASE_INPUT, prompt_cache: { hit_ratio: 0.1234, expires_at: NOW / 1000 + 3700 } } },
    { name: 'legacy-idle', input: noPc(), main: { lastTs: NOW - 10 * 60_000 } },
    { name: 'legacy-fresh', input: noPc(), main: { lastTs: NOW - 60_000 } },
    { name: 'legacy-edge', input: noPc(), main: { lastTs: NOW - 5 * 60_000 } },
    { name: 'legacy-no-ts', input: noPc(), main: { lastTs: null } },
    { name: 'no-data', input: BASE_INPUT, main: { usage: U({}), usd: 0, calls: 0, turns: 0, lastTs: null, turnStartTs: null, model: null } },
    { name: 'no-turn', input: BASE_INPUT, main: { turns: 0 } },
    { name: 'partial', input: BASE_INPUT, main: { usdUnknown: 2, turnUsdUnknown: 1 } },
    { name: 'partial-zero', input: BASE_INPUT, main: { usd: 0, usdUnknown: 1 } },
    { name: 'estimated', input: BASE_INPUT, main: { usdEstimated: 3, turnUsd: 3.5 } },
    { name: 'turn-warn', input: BASE_INPUT, main: { turnUsd: 1.5 } },
    { name: 'turn-crit', input: BASE_INPUT, main: { turnUsd: 12.3456 } },
    { name: 'one-agent', input: BASE_INPUT, agentCount: 1, agents: { 'agent-a.jsonl': ST({ usd: 0.1, turnUsd: 0.05, calls: 2, turns: 0 }) } },
    {
      name: 'agents',
      input: BASE_INPUT,
      agentCount: 3,
      agents: {
        'agent-a.jsonl': ST({ usd: 0.1, turnUsd: 0.2, calls: 2, turns: 0, usdEstimated: 1 }),
        'agent-b.jsonl': ST({ usd: 0.0007, turnUsd: 0.1, calls: 1, turns: 0, usage: U({ input: 3, output: 9 }) }),
        'agent-c.jsonl': ST({ usd: 0.3, turnUsd: 0, usdUnknown: 1, calls: 4, turns: 0 }),
      },
    },
    { name: 'ctx-warn', input: { ...BASE_INPUT, context_window: { used_percentage: 75, total_input_tokens: 150000 } } },
    { name: 'ctx-crit', input: { ...BASE_INPUT, context_window: { used_percentage: 99.5, total_input_tokens: 1250000 } } },
    { name: 'ctx-full', input: { ...BASE_INPUT, context_window: { used_percentage: 100, total_input_tokens: 2000000 } } },
    { name: 'ctx-zero', input: { ...BASE_INPUT, context_window: { used_percentage: 0, total_input_tokens: 0 } } },
    { name: 'ctx-pct-size', input: { ...BASE_INPUT, context_window: { used_percentage: 62.5, context_window_size: 200000 } } },
    { name: 'ctx-pct-only', input: { ...BASE_INPUT, context_window: { used_percentage: 33.3 } } },
    { name: 'ctx-none', input: { ...BASE_INPUT, context_window: undefined } },
    { name: 'ctx-null-pct', input: { ...BASE_INPUT, context_window: { used_percentage: null, total_input_tokens: 5000 } } },
    { name: 'no-cwd', input: { ...BASE_INPUT, cwd: undefined } },
    { name: 'cwd-windows', input: { ...BASE_INPUT, cwd: 'C:\\Work\\x\\my proj\\' }, branch: 'dev' },
    { name: 'cwd-root', input: { ...BASE_INPUT, cwd: '/' }, branch: 'a1b2c3d' },
    { name: 'model-from-cache', input: { ...BASE_INPUT, model: undefined, effort: undefined } },
    { name: 'model-no-name', input: { ...BASE_INPUT, model: { id: 'claude-sonnet-4-5' }, effort: { level: '' } } },
    { name: 'model-none', input: { ...BASE_INPUT, model: undefined, effort: undefined }, main: { model: null } },
    { name: 'no-rates', input: { ...BASE_INPUT, rate_limits: undefined } },
    { name: 'spend', input: { ...BASE_INPUT, rate_limits: { five_hour: { used_percentage: 99.5 }, spend_limit: { used_percentage: 4.4 } } } },
    { name: 'rates-odd', input: { ...BASE_INPUT, rate_limits: { five_hour: { used_percentage: '9' }, seven_day: null } } },
    { name: 'empty', input: {} },
  ];
  /** @type {any[]} */
  const out = [];
  /** @type {Map<string, any>} */
  const bases = new Map();
  const render1 = (/** @type {string} */ name, /** @type {any} */ sc, /** @type {any} */ sl, /** @type {any} */ extra = {}) => {
    const cfg = JSON.parse(JSON.stringify({ metrics: { subscription: false, statusline: { ...SL_DEFAULTS, ...sl }, ...extra } }));
    const input = JSON.parse(JSON.stringify(sc.input));
    const main = sc.main ? JSON.parse(JSON.stringify(ST(sc.main))) : JSON.parse(JSON.stringify(ST({})));
    const agents = JSON.parse(JSON.stringify(sc.agents || {}));
    const cache = { main: { ...newIncrementalState(), ...main }, agents: Object.fromEntries(Object.entries(agents).map(([k, v]) => [k, { ...newIncrementalState(), ...(/** @type {any} */ (v)) }])) };
    const ctx = { agentCount: sc.agentCount || 0, branch: sc.branch ?? null, now: NOW };
    // the scenario (input, cache, ctx) is stored once, by name; the case keeps only the config delta
    if (!bases.has(sc.name)) bases.set(sc.name, { name: `base:${sc.name}`, input: { input, cache: { main, agents }, ctx }, output: {} });
    const slFull = JSON.parse(JSON.stringify({ ...SL_DEFAULTS, ...sl }));
    /** @type {Record<string, any>} */
    const set = {};
    /** @type {string[]} */
    const unset = [];
    for (const k of Object.keys(SL_DEFAULTS)) if (!(k in slFull)) unset.push(k);
    for (const [k, v] of Object.entries(slFull)) if (!(k in SL_DEFAULTS) || JSON.stringify(v) !== JSON.stringify(/** @type {any} */ (SL_DEFAULTS)[k])) set[k] = v;
    const extraJson = JSON.parse(JSON.stringify(extra));
    out.push({ name: `statusline:${name}`, input: { base: sc.name, set, unset, extra: extraJson }, output: { line: render(input, cache, ctx, cfg) } });
  };
  for (const sc of scenarios) {
    for (const [preset, layout] of [['vivid', 'line'], ['powerline', 'two-line'], ['classic', 'two-line']]) render1(`${sc.name}:${preset}:${layout}`, sc, { preset, layout });
  }
  render1('warm:nope:line', scenarios[0], { preset: 'nope' });
  render1('warm:classic:line', scenarios[0], { preset: 'classic' });
  render1('warm:powerline:line', scenarios[0], { preset: 'powerline' });
  // config variants over four scenarios
  const variants = {
    'color-off': { color: false },
    'color-null': { color: null },
    'brand-empty': { brand: '' },
    'brand-null': { brand: null },
    'sep-empty': { separator: '' },
    'sep-null': { separator: null },
    'sep-false': { separator: false },
    'sep-bar': { separator: '|' },
    'theme-path': { theme: { path: '#ff8800', model: 'bg:red white', effort: '300' } },
    'theme-pad': { theme: { pad: ' ', gap: '' } },
    'theme-gap': { theme: { gap: '  ', separator: '38' } },
    'theme-null': { theme: null },
    'layout-odd': { layout: 5 },
    'warn-null': { ctxWarnTokens: null, turnWarnUsd: null },
    'warn-string': { ctxWarnTokens: '5', ctxCritTokens: '90000', turnWarnUsd: '0.1', turnCritUsd: ' 0.4 ' },
    'warn-missing': { ctxWarnTokens: undefined, ctxCritTokens: undefined, turnWarnUsd: undefined, turnCritUsd: undefined },
    'warn-bad': { ctxWarnTokens: 'abc', turnCritUsd: true },
    'ttl-zero': { promptCacheTtlMin: 0 },
    'ttl-string': { promptCacheTtlMin: '1' },
    'ttl-null': { promptCacheTtlMin: null },
    'ttl-negative': { promptCacheTtlMin: -4 },
    'ttl-missing': { promptCacheTtlMin: undefined },
  };
  const pick = (/** @type {string} */ n) => /** @type {any} */ (scenarios.find((s) => s.name === n));
  for (const [vn, sl] of Object.entries(variants)) {
    render1(`var:${vn}:warm:line`, pick('warm'), { ...sl, layout: 'line' });
    render1(`var:${vn}:cold-recache:two-line`, pick('cold-recache'), { ...sl, layout: 'two-line' });
    render1(`var:${vn}:legacy-idle:line`, pick('legacy-idle'), { ...sl, layout: 'line' });
  }
  for (const preset of ['classic', 'vivid', 'powerline']) {
    for (const n of ['warm', 'cold-recache', 'agents']) render1(`sub:${preset}:${n}`, pick(n), { preset }, { subscription: true });
  }
  const defaults = { name: 'defaults', input: { statusline: SL_DEFAULTS }, output: {} };
  return { cases: out, bases: [defaults, ...bases.values()] };
}

/** Every set is written one case per line. */
const COMPACT = true;

/** @returns {string[]} differences, empty when up to date */
export function check() {
  const sets = generate();
  const diffs = [];
  for (const [set, cases] of Object.entries(sets)) diffs.push(...diffArea(AREA, set, cases, COMPACT));
  diffs.push(...staleSets(AREA, Object.keys(sets)));
  return diffs;
}

export function write() {
  return Object.entries(generate()).map(([set, cases]) => writeArea(AREA, set, cases, COMPACT));
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
