// @ts-check
/**
 * Pure views of `stats --json`, `trend --json` and the feature summary, for the Mod's Stats tab and
 * the Home segments. Data in, plain objects out: no drawing, no panel.mjs, no Node built-ins (the
 * Mod bundle imports this file).
 */

import { PALETTE } from './raster.mjs';

/** @typedef {'ok'|'warn'|'bad'|'info'|'dim'} Tone */

/** @param {unknown} n */
const num = (n) => (typeof n === 'number' && Number.isFinite(n) ? n : null);

/** @param {number|null|undefined} n */
export function money(n) {
  const v = num(n);
  if (v === null) return '—';
  return `$${v >= 100 ? Math.round(v) : v.toFixed(2)}`;
}

/** Tokens, compact: 241k, 44M. @param {number|null|undefined} n */
export function tok(n) {
  const v = num(n);
  if (v === null) return '—';
  if (v >= 1e6) return `${v >= 1e7 ? Math.round(v / 1e6) : (v / 1e6).toFixed(1)}M`;
  if (v >= 1e3) return `${v >= 1e4 ? Math.round(v / 1e3) : (v / 1e3).toFixed(1)}k`;
  return String(Math.round(v));
}

/** @param {number} ms */
function span(ms) {
  const s = Math.max(0, Math.round(ms / 1000));
  if (s >= 86400) return `${Math.floor(s / 86400)}d ${Math.floor((s % 86400) / 3600)}h`;
  if (s >= 3600) return `${Math.floor(s / 3600)}h ${String(Math.floor((s % 3600) / 60)).padStart(2, '0')}m`;
  if (s >= 60) return `${Math.floor(s / 60)}m ${String(s % 60).padStart(2, '0')}s`;
  return `${s}s`;
}

/** @param {any} model */
function shortModel(model) {
  const m = /^(?:claude-)?([a-z]+)-(\d+)(?:-(\d{1,2}))?(?:-\d{8})?$/.exec(String(model || ''));
  return m ? `${m[1]} ${m[2]}${m[3] ? `.${m[3]}` : ''}` : String(model || '');
}

/** @param {any} breaks */
function cutsOf(breaks) {
  return Array.isArray(breaks) ? breaks.length : 0;
}

/** The session header figures. @param {any} stats */
export function sessionFigures(stats) {
  const u = stats?.usage;
  if (!u) return [];
  const out = [];
  out.push({ id: 'cost', label: 'costo', value: `${money(u.usd)}${u.usdEstimated ? '*' : ''}`, tone: /** @type {Tone} */ ('info') });
  const o = num(u.usage?.output);
  out.push({ id: 'tokens', label: 'tokens', value: `${tok(u.totalInput)} → ${tok(o)}`, tone: /** @type {Tone} */ ('dim') });
  const hit = num(u.cacheHitPct);
  if (hit !== null) {
    const cuts = cutsOf(stats.cacheBreaks);
    out.push({ id: 'cache', label: 'cache', value: `${Math.round(hit)}% · ${cuts} ${cuts === 1 ? 'corte' : 'cortes'}`, tone: /** @type {Tone} */ (hit >= 90 ? 'ok' : hit >= 70 ? 'warn' : 'bad') });
  }
  const agents = Array.isArray(stats.agents) ? stats.agents.length : num(stats.agents) ?? 0;
  out.push({ id: 'calls', label: 'llamadas', value: `${u.calls ?? 0} · ${agents} ${agents === 1 ? 'agente' : 'agentes'}`, tone: /** @type {Tone} */ ('dim') });
  return out;
}

const BAR_COLORS = [PALETTE.amber, PALETTE.blue, PALETTE.violet, PALETTE.green, PALETTE.pink, PALETTE.cyan];

/** @param {{label: string, usd: number, calls?: number}[]} rows */
function toBars(rows) {
  const sorted = rows.filter((r) => num(r.usd) !== null).sort((a, b) => b.usd - a.usd);
  const max = Math.max(0, ...sorted.map((r) => r.usd));
  const labelWidth = Math.max(0, ...sorted.map((r) => r.label.length));
  return {
    labelWidth,
    rows: sorted.map((r, i) => ({
      label: r.label, usd: r.usd, text: money(r.usd), calls: r.calls ?? null,
      ratio: max > 0 ? r.usd / max : 0, color: BAR_COLORS[i % BAR_COLORS.length],
    })),
  };
}

/** Cost per role: main thread plus one row per agent type. @param {any} stats */
export function roleBars(stats) {
  /** @type {{label: string, usd: number, calls?: number}[]} */
  const rows = [];
  if (num(stats?.main?.usd) !== null) rows.push({ label: 'principal', usd: stats.main.usd, calls: stats.main.calls });
  for (const [type, b] of Object.entries(stats?.byAgentType || {})) {
    const usd = num(/** @type {any} */ (b)?.usd);
    if (usd !== null) rows.push({ label: type.replace(/^nxy:/, ''), usd, calls: /** @type {any} */ (b).calls });
  }
  return toBars(rows);
}

/** @param {any} stats */
export function modelBars(stats) {
  const rows = Object.entries(stats?.byModel || {}).flatMap(([m, b]) => {
    const usd = num(/** @type {any} */ (b)?.usd);
    return usd === null ? [] : [{ label: shortModel(m), usd, calls: /** @type {any} */ (b).calls }];
  });
  return toBars(rows);
}

/**
 * What a 1 h cache would have done to the main thread. Null when there is nothing to compare.
 * @param {any} w stats.ttlWhatIf @param {any[]} [breaks] stats.cacheBreaks
 */
export function whatIf1h(w, breaks) {
  if (!w || !w.writes5m || !(w.actualUsd > 0)) return null;
  const pct = (100 * (w.hourUsd - w.actualUsd)) / w.actualUsd;
  let avoidableUsd = 0;
  for (const b of Array.isArray(breaks) ? breaks : []) {
    if ((b?.cause === 'idle' || b?.cause === 'subagent') && num(b.usd)) avoidableUsd += b.usd;
  }
  return {
    actualUsd: w.actualUsd, hourUsd: w.hourUsd, savingsPct: pct <= -1 ? -pct : null,
    worth: pct <= -1, rebuildsAvoided: w.rebuildsAvoided || 0, avoidableUsd,
    text: `${money(w.actualUsd)} → ${money(w.hourUsd)}${pct <= -1 ? ` (−${Math.round(-pct)}%)` : ''}`,
  };
}

const WINDOW_LABELS = { five_hour: '5 h', seven_day: '7 días' };

/** @param {any} v */
function atMs(v) {
  if (typeof v === 'number' && Number.isFinite(v)) return v < 1e11 ? v * 1000 : v;
  if (typeof v === 'string') { const t = Date.parse(v); return Number.isFinite(t) ? t : null; }
  return null;
}

/** Subscription windows; empty without a subscription. @param {any[]|null|undefined} limits @param {number} now */
export function windowRows(limits, now) {
  const rows = [];
  for (const kind of /** @type {const} */ (['five_hour', 'seven_day'])) {
    const l = (Array.isArray(limits) ? limits : []).find((x) => x?.kind === kind);
    const pct = num(l?.percentUsed);
    if (pct === null) continue;
    const at = atMs(l.resetsAt);
    rows.push({
      kind, label: WINDOW_LABELS[kind], percentUsed: pct, ratio: Math.max(0, Math.min(1, pct / 100)),
      resetsInMs: at === null ? null : Math.max(0, at - now),
      tone: /** @type {Tone} */ (pct >= 90 ? 'bad' : pct >= 70 ? 'warn' : 'ok'),
    });
  }
  return rows;
}

/**
 * Footer segments of Home. A segment without data is left out, never invented.
 * @param {{model?: string|null, effort?: string|null, usage?: any, turnUsd?: number|null, cache?: any, limits?: any[]|null, now?: number}} a
 */
export function segmentsOf({ model, effort, usage, turnUsd, cache, limits, now = 0 }) {
  const items = [];
  const name = model || usage?.model;
  if (name) items.push({ id: 'model', label: 'modelo', value: effort ? `${shortModel(name)} · ${effort}` : shortModel(name), tone: /** @type {Tone} */ ('info') });
  if (num(usage?.window) && usage.window > 0) items.push({ id: 'window', label: 'ventana', value: tok(usage.window), tone: /** @type {Tone} */ ('dim') });
  if (num(usage?.tokens) !== null) {
    const pct = num(usage.percent);
    items.push({ id: 'ctx', label: 'ctx', value: `${tok(usage.tokens)}${pct !== null ? ` · ${Math.round(pct)}%` : ''}`, tone: /** @type {Tone} */ (pct !== null && pct >= 90 ? 'bad' : pct !== null && pct >= 70 ? 'warn' : 'dim') });
  }
  if (num(turnUsd) !== null && /** @type {number} */ (turnUsd) > 0) items.push({ id: 'turn', label: 'turno', value: money(turnUsd), tone: /** @type {Tone} */ ('dim') });
  if (num(usage?.costUsd) !== null) items.push({ id: 'session', label: 'sesión', value: money(usage.costUsd), tone: /** @type {Tone} */ ('info') });
  if (cache) {
    const left = num(cache.ttlLeftMs);
    const hit = num(cache.hitPct);
    if (left !== null && left <= 0) {
      const cold = num(cache.coldCostUsd);
      items.push({ id: 'cache', label: 'cache', value: `❄ fría${cold !== null ? ` · ${money(cold)}${cache.estimated ? '*' : ''}` : ''}`, tone: /** @type {Tone} */ ('warn') });
    } else if (hit !== null) {
      items.push({ id: 'cache', label: 'cache', value: `${Math.round(hit)}%${left !== null ? ` · ${span(left)}` : ''}`, tone: /** @type {Tone} */ ('ok') });
    }
  }
  for (const w of windowRows(limits, now)) {
    items.push({ id: w.kind, label: w.label, value: `${Math.round(w.percentUsed)}%`, tone: w.tone });
  }
  return { items };
}

// ---------------------------------------------------------------------------
// Trend
// ---------------------------------------------------------------------------

/**
 * Metrics of the trend chart. `get` returns null when the period has no data ("?").
 * @type {Record<string, {id: string, label: string, hint: string, color: string, get: (p: any) => number|null, fmt: (v: number) => string}>}
 */
export const METRICS = {
  usd: {
    id: 'usd', label: '$ por día', color: PALETTE.amber, hint: 'lo que costaría a precios de API (con suscripción no lo pagás)',
    get: (p) => num(p?.usd), fmt: (v) => `$${v >= 10 ? Math.round(v) : v.toFixed(1)}`,
  },
  tok: {
    id: 'tok', label: 'tokens', color: PALETTE.blue, hint: 'todo lo enviado + lo generado',
    get: (p) => num(p?.totalTokens), fmt: tok,
  },
  ctx: {
    id: 'ctx', label: 'contexto/llamada', color: PALETTE.violet, hint: 'cuánto arrastra cada llamada: si sube, cortá antes',
    get: (p) => num(p?.ctxPerCall), fmt: tok,
  },
  rtk: {
    id: 'rtk', label: 'ahorro rtk', color: PALETTE.green, hint: 'tokens de salida de comandos que el filtro recortó',
    get: (p) => num(p?.rtkSaved), fmt: tok,
  },
};

const PARTIAL = '▁▂▃▄▅▆▇';

/**
 * Column bars, top row first: full `█`, partial `▁…▇`, blank. A null value has no bars.
 * @param {(number|null)[]} values @param {number} rows @returns {string[][]} one array of `rows` chars per value
 */
export function barGrid(values, rows) {
  const max = Math.max(0.0001, ...values.map((v) => v ?? 0));
  return values.map((v) => {
    const col = [];
    for (let row = 0; row < rows; row++) {
      if (v === null) { col.push(' '); continue; }
      const level = rows - row;
      const hgt = (v / max) * rows;
      if (hgt >= level) col.push('█');
      else if (hgt > level - 1) col.push(PARTIAL[Math.min(6, Math.floor((hgt - (level - 1)) * 7))]);
      else col.push(' ');
    }
    return col;
  });
}

/** `2026-10-01` (or `2026-W40`, or a month) to D/M. @param {any} period */
function dayLabel(period) {
  const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(String(period || ''));
  return m ? `${Number(m[3])}/${Number(m[2])}` : String(period || '?');
}

export const COLUMN_WIDTH = 6;
export const BAR_ROWS = 6;

/**
 * The trend chart as data. Columns come from `periods[]`; with `by: 'model'` it is horizontal rows.
 * @param {any} trend @param {string} sel metric id @param {number} CW available columns
 */
export function trendChart(trend, sel, CW) {
  const metric = METRICS[sel] ?? METRICS.usd;
  const totalUsd = num(trend?.total?.usd);
  const avoidableUsd = num(trend?.rebuilds?.avoidableUsd);
  const base = { metric: metric.id, label: metric.label, hint: metric.hint, color: metric.color, totalUsd, avoidableUsd };
  if (trend?.by === 'model') {
    const sel2 = metric.id === 'tok' ? 'tok' : 'usd';
    const rows = (Array.isArray(trend.models) ? trend.models : []).map((m) => ({
      label: shortModel(m.model), value: sel2 === 'tok' ? num(m.total) : num(m.usd),
    })).filter((r) => r.value !== null);
    rows.sort((a, b) => /** @type {number} */ (b.value) - /** @type {number} */ (a.value));
    const max = Math.max(0, ...rows.map((r) => /** @type {number} */ (r.value)));
    const fmt = sel2 === 'tok' ? tok : money;
    const shownMetric = METRICS[sel2];
    const note = metric.id === sel2 ? null : 'ctx y rtk no se desglosan por modelo: se muestra $';
    return {
      ...base, label: shownMetric.label, hint: shownMetric.hint, color: shownMetric.color, note,
      kind: /** @type {'rows'} */ ('rows'), metric: sel2, labelWidth: Math.max(0, ...rows.map((r) => r.label.length)),
      rows: rows.map((r) => ({ label: r.label, text: fmt(r.value), ratio: max > 0 ? /** @type {number} */ (r.value) / max : 0 })),
      totalTokens: rows.reduce((s, r) => s + (sel2 === 'tok' ? /** @type {number} */ (r.value) : 0), 0) || null,
    };
  }
  const periods = Array.isArray(trend?.periods) ? trend.periods : [];
  const fit = Math.max(1, Math.floor(CW / COLUMN_WIDTH));
  const shown = periods.slice(-fit);
  const values = shown.map((p) => metric.get(p));
  const grid = barGrid(values, BAR_ROWS);
  const totalTokens = periods.reduce((s, p) => s + (num(p?.totalTokens) ?? 0), 0);
  return {
    ...base, kind: /** @type {'columns'} */ ('columns'), totalTokens: totalTokens || null,
    note: shown.length < periods.length ? `últimos ${shown.length} de ${periods.length}` : null,
    columns: shown.map((p, i) => ({
      label: dayLabel(p.period), text: values[i] === null ? '?' : metric.fmt(/** @type {number} */ (values[i])),
      value: values[i], bars: grid[i], last: i === shown.length - 1,
    })),
  };
}

// ---------------------------------------------------------------------------
// Feature summary
// ---------------------------------------------------------------------------

/**
 * What the finished feature cost, against its session.
 * @param {{feature?: any, session?: any, approvedAt?: number|null, sinceKind?: string|null, snap?: any}} a
 */
export function featureSummary({ feature, session, approvedAt = null, sinceKind = null, snap }) {
  const fu = num(feature?.usage?.usd);
  const su = num(session?.usage?.usd);
  const batches = [];
  const verdicts = snap?.verdicts ?? {};
  const nums = new Set([...Object.keys(verdicts), ...Object.keys(snap?.launched ?? {})]);
  for (const n of [...nums].map(Number).filter(Number.isFinite).sort((a, b) => a - b)) {
    const v = verdicts[String(n)];
    const launched = num(v?.launched) ?? num(snap?.launched?.[String(n)]);
    const ts = num(v?.ts);
    const ms = launched !== null && ts !== null ? Math.max(0, ts - launched) : null;
    batches.push({ n, ms, text: ms === null ? '—' : span(ms) });
  }
  return {
    approvedAt, sinceKind,
    costUsd: fu, sessionUsd: su, pct: fu !== null && su !== null && su > 0 ? Math.round((100 * fu) / su) : null,
    figures: sessionFigures(feature),
    tokens: feature?.usage ? `${tok(feature.usage.totalInput)} → ${tok(num(feature.usage.usage?.output))}` : null,
    roles: roleBars(feature),
    batches,
  };
}
