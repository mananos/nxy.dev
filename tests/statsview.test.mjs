import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
  METRICS, barGrid, featureSummary, modelBars, money, roleBars, segmentsOf, sessionFigures, tok, trendChart, whatIf1h, windowRows,
} from '../core/statsview.mjs';

/** @param {any} t @param {string} sel @param {number} cw @returns {any} */
const chart = (t, sel, cw) => trendChart(t, sel, cw);
/** @param {any} w @param {any[]} breaks @returns {any} */
const wi = (w, breaks) => whatIf1h(w, breaks);
const b = (calls, usd, hit, input, output) => ({ calls, usd, usdEstimated: false, cacheHitPct: hit, totalInput: input, usage: { output } });
// Shape of `stats --json`, trimmed to what the view reads.
const STATS = {
  usage: b(311, 10.68, 94.87, 25743561, 123081),
  main: b(83, 4.1956, 98.24, 9427433, 50903),
  byModel: { 'claude-opus-5-5': b(83, 4.1956, 98, 9427433, 50903), 'claude-sonnet-5-5': b(215, 6.33, 93, 16064043, 71227), 'claude-haiku-4-5-20251001': b(13, 0.15, 57, 252085, 951) },
  byAgentType: { 'nxy:implementer': b(113, 2.77, 85, 4007860, 63950), 'nxy:tester': b(13, 0.15, 57, 252085, 951), 'nxy:planner': b(87, 3.11, 96, 11186464, 2494) },
  agents: [{}, {}, {}],
  cacheBreaks: [{ cause: 'idle', usd: 1.5 }, { cause: 'subagent', usd: 0.5 }, { cause: 'start', usd: 2 }],
  ttlWhatIf: { actualUsd: 4.2, hourUsd: 2.6, writes5m: 3, rebuildsAvoided: 2 },
};
// Shape of `trend --json --since 7d`, trimmed.
const p = (period, totalTokens, usd, ctxPerCall, rtkSaved) => ({ period, totalTokens, usd, ctxPerCall, rtkSaved });
const TREND = {
  by: 'day',
  periods: [p('2026-09-30', 43368050, 18.6, 183696, 236), p('2026-10-01', 33920351, 17.19, 124052, 20380), p('2026-10-02', 4469152, 2.52, 70299, null), p('2026-10-05', 539619, 0.45, 53489, 0), p('2026-10-06', 109852765, 44.0, 203721, 34032), p('2026-10-07', 59654242, 25.27, 137530, 58786)],
  total: { usd: 108.07 }, rebuilds: { avoidableUsd: 3.06 },
};

test('formatos', () => {
  assert.equal(money(4.123), '$4.12');
  assert.equal(money(null), '—');
  assert.equal(tok(44_000_000), '44M');
  assert.equal(tok(241_000), '241k');
  assert.equal(tok(900), '900');
});

test('sessionFigures', () => {
  const f = Object.fromEntries(sessionFigures(STATS).map((x) => [x.id, x.value]));
  assert.equal(f.cost, '$10.68');
  assert.equal(f.tokens, '26M → 123k');
  assert.equal(f.cache, '95% · 3 cortes');
  assert.equal(f.calls, '311 · 3 agentes');
  assert.deepEqual(sessionFigures({}), []);
});

test('roleBars: sin prefijo nxy, orden por USD, etiqueta nunca cortada', () => {
  const r = roleBars(STATS);
  assert.deepEqual(r.rows.map((x) => x.label), ['principal', 'planner', 'implementer', 'tester']);
  assert.equal(r.labelWidth, 'implementer'.length);
  assert.equal(r.rows[0].ratio, 1);
  assert.ok(r.rows.every((x) => !x.label.startsWith('nxy:')));
  assert.deepEqual(roleBars({}).rows, []);
});

test('modelBars', () => {
  assert.deepEqual(modelBars(STATS).rows.map((x) => x.label), ['sonnet 5.5', 'opus 5.5', 'haiku 4.5']);
});

test('whatIf1h: ahorro solo si >= 1 %, evitable = idle + subagent', () => {
  const w = wi(STATS.ttlWhatIf, STATS.cacheBreaks);
  assert.equal(w.worth, true);
  assert.equal(Math.round(w.savingsPct), 38);
  assert.equal(w.avoidableUsd, 2);
  assert.equal(wi({ ...STATS.ttlWhatIf, hourUsd: 4.21 }, []).savingsPct, null);
  assert.equal(wi({ actualUsd: 4, hourUsd: 4, writes5m: 0 }, []), null);
  assert.equal(wi(undefined, []), null);
});

test('windowRows: vacío sin suscripción', () => {
  const now = 1_791_000_000_000;
  const rows = windowRows([{ kind: 'five_hour', percentUsed: 42, resetsAt: now + 3_600_000 }, { kind: 'seven_day', percentUsed: 95, resetsAt: now + 1000 }], now);
  assert.equal(rows.length, 2);
  assert.equal(rows[0].resetsInMs, 3_600_000);
  assert.equal(rows[1].tone, 'bad');
  assert.deepEqual(windowRows([], now), []);
  assert.deepEqual(windowRows(null, now), []);
});

test('segmentsOf omite lo que no tiene dato', () => {
  assert.deepEqual(segmentsOf({}).items, []);
  const s = segmentsOf({
    model: 'claude-opus-5-5', effort: 'high', usage: { tokens: 120000, percent: 12, window: 1_000_000, costUsd: 4.2 }, turnUsd: 0.31,
    cache: { hitPct: 96, ttlLeftMs: 185000 }, limits: [{ kind: 'five_hour', percentUsed: 30, resetsAt: 5000 }], now: 0,
  });
  const v = Object.fromEntries(s.items.map((x) => [x.id, x.value]));
  assert.equal(v.model, 'opus 5.5 · high');
  assert.equal(v.window, '1.0M');
  assert.equal(v.ctx, '120k · 12%');
  assert.equal(v.turn, '$0.31');
  assert.equal(v.session, '$4.20');
  assert.equal(v.cache, '96% · 3m 05s');
  assert.equal(v.five_hour, '30%');
  assert.equal(v.seven_day, undefined);
  const cold = segmentsOf({ cache: { hitPct: 90, ttlLeftMs: 0, coldCostUsd: 3.86, estimated: true } });
  assert.equal(cold.items[0].value, '❄ fría · $3.86*');
});

test('trendChart: columnas, últimas N y "?"', () => {
  const c = chart(TREND, 'usd', 100);
  assert.equal(c.kind, 'columns');
  assert.equal(c.columns.length, 6);
  assert.equal(c.note, null);
  assert.equal(c.columns[0].label, '30/9');
  assert.equal(c.columns[4].bars.length, 6);
  assert.equal(c.columns[4].bars[0], '█');
  assert.equal(c.totalUsd, 108.07);
  assert.equal(c.avoidableUsd, 3.06);
  const narrow = chart(TREND, 'usd', 20);
  assert.equal(narrow.columns.length, 3);
  assert.equal(narrow.note, 'últimos 3 de 6');
  assert.equal(narrow.columns.at(-1).label, '7/10');
  assert.ok(narrow.columns.at(-1).last);
  const rtk = chart(TREND, 'rtk', 100);
  assert.equal(rtk.columns[2].text, '?');
  assert.ok(rtk.columns[2].bars.every((x) => x === ' '));
  assert.equal(chart({}, 'nope', 50).columns.length, 0);
  assert.deepEqual(Object.keys(METRICS), ['usd', 'tok', 'ctx', 'rtk']);
});

test('trendChart por modelo: filas horizontales, sólo usd y tokens', () => {
  const t = { by: 'model', models: [{ model: 'claude-haiku-4-5-20251001', usd: 1, total: 100 }, { model: 'claude-opus-5-5', usd: 69.3, total: 173406677 }], total: { usd: 70.3 } };
  const c = chart(t, 'ctx', 80);
  assert.equal(c.kind, 'rows');
  assert.equal(c.metric, 'usd');
  assert.deepEqual(c.rows.map((r) => r.label), ['opus 5.5', 'haiku 4.5']);
  assert.equal(chart(t, 'tok', 80).rows[0].text, '173M');
  assert.equal(c.label, METRICS.usd.label);
  assert.equal(c.color, METRICS.usd.color);
  assert.match(c.note, /no se desglosan por modelo/);
  const u = chart(t, 'usd', 80);
  assert.equal(u.note, null);
  assert.equal(chart(t, 'tok', 80).label, METRICS.tok.label);
});

test('barGrid: lleno, parcial y vacío', () => {
  const g = barGrid([10, 5, 0, null], 4);
  assert.deepEqual(g[0], ['█', '█', '█', '█']);
  assert.deepEqual(g[1], [' ', ' ', '█', '█']);
  assert.deepEqual(g[2], [' ', ' ', ' ', ' ']);
  assert.deepEqual(g[3], [' ', ' ', ' ', ' ']);
  assert.ok(barGrid([10, 3], 4)[1].some((ch) => '▁▂▃▄▅▆▇'.includes(ch)));
});

test('featureSummary: costo vs sesión y tiempo por lote', () => {
  const snap = { launched: { 2: 5000 }, verdicts: { 1: { ts: 9000, launched: 4000 }, 2: { ts: 65000 }, 3: { ts: 70000 } } };
  const s = featureSummary({ feature: STATS, session: { usage: { usd: 21.36 } }, approvedAt: 1, sinceKind: 'approval', snap });
  assert.equal(s.pct, 50);
  assert.equal(s.costUsd, 10.68);
  assert.equal(s.roles.rows[0].label, 'principal');
  assert.deepEqual(s.batches.map((x) => x.text), ['5s', '1m 00s', '—']);
  const empty = featureSummary({});
  assert.equal(empty.pct, null);
  assert.deepEqual(empty.batches, []);
});

test('statsview.mjs es puro: sin node:* ni panel.mjs', () => {
  const src = readFileSync(new URL('../core/statsview.mjs', import.meta.url), 'utf8');
  assert.doesNotMatch(src, /from\s+['"]node:/);
  assert.doesNotMatch(src, /import[^;]*panel\.mjs/);
});
