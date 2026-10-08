// @ts-check
/**
 * The nxy panel's view model (1.1.0): pure data in, pure data out. The Mod's pane (register.tsx)
 * only draws what `buildPanel` returns; the headless fallback prints `panelText` of the same view.
 *
 * A tab's body is a list of typed blocks (`view.blocks`): hero, tiles, now, actions, goal, batches,
 * suite, checkpoint, note, kv, agents. The renderer only draws blocks.
 * Adding a tab = one entry in TABS plus one builder in BUILDERS (it returns `{blocks}`). Adding a
 * button = one entry in LAUNCHER (with its own `tab`).
 *
 * Pure: no `node:*`, no `process`. Its imports are orchestrator.mjs, batch-status.mjs and statsview.mjs only.
 */
import {
  effective, suiteDone, suiteIsRed, stateOf, nextActions,
  PAUSE_CONTINUE, PAUSE_ADJUST, PAUSE_STOP,
} from './orchestrator.mjs';
import { RETRY, CONTINUE, STOP, isRed, isDone } from './batch-status.mjs';
import {
  sessionFigures, roleBars, modelBars, whatIf1h, windowRows, segmentsOf, trendChart, featureSummary, METRICS, money,
} from './statsview.mjs';

/**
 * Clip a launcher's output for the pane: at most `maxLines` lines, long lines cut at 160 columns.
 * @param {string} text @param {number} [maxLines]
 */
export function clipOutput(text, maxLines = 30) {
  const lines = String(text ?? '').replace(/\s+$/, '').split(/\r?\n/).map((l) => (l.length > 160 ? `${l.slice(0, 159)}…` : l));
  if (lines.length <= maxLines) return lines;
  return [...lines.slice(0, maxLines), `… (${lines.length - maxLines} more lines)`];
}

/** @typedef {'ok' | 'error' | 'running' | 'info' | 'dim'} Tone */

export const TABS = [
  { id: 'home', label: 'Inicio', hotkey: '1' },
  { id: 'plan', label: 'Plan', hotkey: '2' },
  { id: 'agents', label: 'Agentes', hotkey: '3' },
  { id: 'stats', label: 'Stats', hotkey: '4' },
  { id: 'config', label: 'Config', hotkey: '5' },
];

/** The hotkey of the checkpoint's "Arreglar N" button: not a/c/x/y/n/e/r/d, not a launcher's, not a tab digit. */
const FIX_HOTKEY = 'z';

/**
 * What a launcher entry's `needs` asks of the panel's state ({snap, owned}).
 * `plan`: a plan or a handoff the orchestrator is not running (ending it under the orchestrator would
 * pull the plan out from under it).
 * @type {Record<'plan', (s: {snap: any, owned: boolean}) => boolean>}
 */
export const NEEDS = {
  plan: ({ snap, owned }) => (!!snap?.hash || snap?.handoff != null) && !owned,
};

/**
 * True when the entry's `needs` (if any) holds for this state: the buttons and the driver's
 * presses both ask this.
 * @param {{needs?: keyof typeof NEEDS}} entry @param {{snap: any, owned?: boolean}} state
 */
export const launcherAvailable = (entry, { snap, owned = false }) => !entry.needs || NEEDS[entry.needs]({ snap, owned });

/**
 * The launcher: a button -> an nxy entry script. `label`/`args` may be functions of the context.
 * `after: 'refresh'` re-reads the snapshot once the process ends; `needs` names a NEEDS check: the
 * button is hidden, and its press ignored, while the check fails; `confirm` asks yes/no first.
 * @type {{id: string, tab: string, label: string | ((ctx: any) => string), hotkey: string, script: string,
 *   args: string[] | ((ctx: any) => string[]), after?: 'refresh', needs?: keyof typeof NEEDS, confirm?: string}[]}
 */
export const LAUNCHER = [
  { id: 'gate-once', tab: 'home', label: 'Gate once', hotkey: 'g', script: 'gate', args: ['once'] },
  { id: 'handoff', tab: 'home', label: 'Handoff', hotkey: 'h', script: 'mem', args: ['handoff', 'show'] },
  {
    id: 'plan-done', tab: 'plan', label: 'Terminar plan', hotkey: 'e', script: 'mem', args: ['handoff', 'done'],
    needs: 'plan', after: 'refresh', confirm: 'Terminar el plan y archivar el handoff?',
  },
  {
    // Unknown state (no snapshot yet): a plain 'Filter', never a guessed 'off'.
    id: 'filter', tab: 'home', label: (ctx) => (typeof ctx?.filter !== 'boolean' ? 'Filter' : ctx.filter ? 'Filter ● on' : 'Filter ○ off'),
    hotkey: 'f', script: 'filter', args: (ctx) => (ctx?.filter === true ? ['off'] : ['on']), after: 'refresh',
  },
];

const PANEL_IDS = ['refresh', 'dismiss', 'confirm-yes', 'confirm-no', 'fix-picked', 'here', 'summary-hide'];

/** The trend selectors: metric ids come from METRICS. */
export const TREND_RANGES = ['7d', '30d'];
export const TREND_BYS = ['day', 'week', 'model'];
const DEFAULT_TREND_SEL = { metric: 'usd', range: '7d', by: 'day', here: false };

/**
 * A launcher entry resolved against the context ({filter}; `filter` absent while unknown).
 * @param {string} id @param {{filter?: boolean}} [ctx]
 * @returns {{id: string, tab: string, label: string, hotkey: string, script: string, args: string[], after?: 'refresh', needs?: keyof typeof NEEDS, confirm?: string} | null}
 */
export function launcherOf(id, ctx = {}) {
  const e = LAUNCHER.find((l) => l.id === id);
  if (!e) return null;
  return {
    ...e,
    label: typeof e.label === 'function' ? e.label(ctx) : e.label,
    args: typeof e.args === 'function' ? e.args(ctx) : [...e.args],
  };
}

/**
 * The launcher context of a snapshot: `filter` only once the snapshot carries `config.filter`.
 * @param {any} snap @returns {{filter?: boolean}}
 */
export const filterCtx = (snap) => (typeof snap?.config?.filter === 'boolean' ? { filter: snap.config.filter } : {});

/** @param {string} id */
export const isPanelAction = (id) => PANEL_IDS.includes(id) || id.startsWith('tab:') || /^batch:\d+$/.test(id)
  || /^pick:\S+$/.test(id) || LAUNCHER.some((l) => l.id === id)
  || (id.startsWith('metric:') && Object.hasOwn(METRICS, id.slice(7))) || /^range:(7d|30d)$/.test(id) || /^by:(day|week|model)$/.test(id);

// ---- layout (pure: the renderer asks it for widths) ----

/**
 * The column plan for a body `bodyColumns` wide: a centred column of at most 78 cells with a margin.
 * @param {number} bodyColumns
 */
export function layoutOf(bodyColumns) {
  const W = Math.max(28, Math.floor(Number(bodyColumns) || 0));
  const pad = W >= 50 ? 2 : 1;
  const CW = Math.min(78, W - 2 * pad);
  const wide = CW >= 64;
  const narrow = CW < 44;
  const stacked = CW < 52;
  const tilesPerRow = stacked ? 1 : wide ? 3 : 2;
  const tileW = Math.floor((CW - (tilesPerRow - 1)) / tilesPerRow);
  return { W, pad, CW, wide, narrow, stacked, tilesPerRow, tileW };
}

/**
 * How the lifecycle draws in `CW` cells: `track` (one node per step, labels aligned under their span)
 * when every span fits the longest label plus a space, else `wrap` (the steps flow over lines).
 * Labels are never cut.
 * @param {{label: string, state: string}[]} steps @param {number} CW
 * @returns {{mode: 'track', span: number} | {mode: 'wrap', lines: any[][]}}
 */
export function lifecycleLayout(steps, CW) {
  const n = steps.length;
  const longest = Math.max(0, ...steps.map((s) => s.label.length));
  const span = n ? Math.floor(CW / n) : 0;
  if (n && span >= longest + 1) return { mode: 'track', span };
  return { mode: 'wrap', lines: fitSteps(/** @type {any} */ (steps), CW) };
}

// ---- helpers the renderer shares ----

/** @param {number} used @param {number} limit @param {number} [width] */
export function barText(used, limit, width = 10) {
  const w = Math.max(1, Math.floor(width));
  const ratio = limit > 0 ? used / limit : 0;
  const filled = Math.min(w, Math.max(0, Math.round((Number.isFinite(ratio) ? ratio : 0) * w)));
  return '█'.repeat(filled) + '░'.repeat(w - filled);
}

/** @param {number} n */
export const kFmt = (n) => (Math.abs(n) < 1000 ? String(Math.round(n)) : `${Math.round(n / 1000)}k`);

const STEP_GLYPH = { done: '✔', current: '◉', pending: '○', red: '✘' };
const stepText = (/** @type {{label: string, state: keyof typeof STEP_GLYPH}} */ s) => `${STEP_GLYPH[s.state] ?? '○'} ${s.label}`;

/**
 * Wrap lifecycle steps into lines no wider than `columns` (a step is never split).
 * @param {{label: string, state: 'done' | 'current' | 'pending' | 'red'}[]} steps @param {number} columns
 * @returns {{label: string, state: 'done' | 'current' | 'pending' | 'red'}[][]}
 */
export function fitSteps(steps, columns) {
  const sep = ' › ';
  /** @type {any[][]} */
  const lines = [];
  let cur = /** @type {any[]} */ ([]);
  let width = 0;
  for (const s of steps) {
    const w = stepText(s).length;
    if (cur.length && width + sep.length + w > columns) { lines.push(cur); cur = []; width = 0; }
    width += (cur.length ? sep.length : 0) + w;
    cur.push(s);
  }
  if (cur.length) lines.push(cur);
  return lines;
}

/** @param {number} ms */
function agoEs(ms) {
  const min = Math.max(0, Math.floor(ms / 60000));
  if (min < 1) return 'hace un momento';
  if (min < 60) return `hace ${min} min`;
  const h = Math.floor(min / 60);
  if (h < 48) return `hace ${h} h`;
  return `hace ${Math.floor(h / 24)} días`;
}

/** A duration for the pane: "42s", "3m 05s", "1h 02m". @param {number} ms */
export function durText(ms) {
  const s = Math.max(0, Math.floor(ms / 1000));
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${String(s % 60).padStart(2, '0')}s`;
  return `${Math.floor(m / 60)}h ${String(m % 60).padStart(2, '0')}m`;
}

/** @param {number} used @param {number} limit @returns {Tone} */
const barTone = (used, limit) => (limit > 0 && used / limit >= 0.9 ? 'error' : limit > 0 && used / limit >= 0.7 ? 'running' : 'ok');

// ---- lifecycle ----

/**
 * The plan's life as steps: plan, aprobado, lotes N/M, tester, review, cierre.
 * @param {any} snap @returns {{label: string, state: 'done' | 'current' | 'pending' | 'red'}[]}
 */
export function lifecycle(snap) {
  if (!snap?.hash) return [];
  const eff = effective(snap);
  const total = snap.batches.length;
  const dn = snap.batches.filter(({ n }) => isDone(eff[String(n)]?.status)).length;
  const anyRed = snap.batches.some(({ n }) => isRed(eff[String(n)]?.status) && !(snap.continued ?? []).includes(n));
  const batchesOver = total > 0 && snap.batches.every(({ n }) => {
    const s = eff[String(n)]?.status;
    return s != null && (isDone(s) || (isRed(s) && (snap.continued ?? []).includes(n)));
  });
  const suite = snap.suite;
  /** @type {{label: string, state: 'done' | 'current' | 'pending' | 'red'}[]} */
  const steps = [
    { label: 'plan', state: 'done' },
    { label: 'aprobado', state: snap.approved ? 'done' : 'pending' },
    { label: `lotes ${dn}/${total}`, state: batchesOver ? 'done' : anyRed ? 'red' : 'pending' },
    { label: 'tester', state: suite && suiteDone(suite) ? (suiteIsRed(suite) ? 'red' : 'done') : 'pending' },
    { label: 'review', state: snap.review ? 'done' : 'pending' },
    { label: 'cierre', state: snap.review ? 'done' : 'pending' },
  ];
  // The first step that is not done is the one in progress, unless it is red.
  const next = steps.find((s) => s.state !== 'done');
  if (next && next.state === 'pending') next.state = 'current';
  return steps;
}

// ---- block builders ----

/** @param {any} inFlight @returns {{role: string, batch?: number}[]} */
const agentsOf = (inFlight) => (Array.isArray(inFlight) ? inFlight : Object.values(inFlight ?? {})).filter(Boolean);

/** Running-agent rows with time since their batch launched. @param {any} snap @param {{role: string, batch?: number}[]} agents @param {number} now */
const agentRows = (snap, agents, now) => agents.map((a) => ({
  role: a.role, batch: a.batch ?? null,
  sinceMs: a.batch != null && typeof snap?.launched?.[String(a.batch)] === 'number' ? Math.max(0, now - snap.launched[String(a.batch)]) : null,
}));

/** Batch state by number. @param {any} snap @returns {{n: number, state: 'done' | 'running' | 'red' | 'pending', status?: string}[]} */
function batchStates(snap) {
  if (!snap?.hash || !snap.batches?.length) return [];
  const eff = effective(snap);
  return snap.batches.map((/** @type {any} */ b) => {
    const s = eff[String(b.n)]?.status;
    return { n: b.n, status: s, state: !s ? 'pending' : s === 'running' ? 'running' : isRed(s) ? 'red' : 'done' };
  });
}

/** "lotes 3–5", "lote 4", "lotes 2, 4" @param {number[]} ns */
function batchRange(ns) {
  const s = [...ns].sort((a, b) => a - b);
  if (s.length === 1) return `lote ${s[0]}`;
  const contiguous = s.every((n, i) => i === 0 || n === s[i - 1] + 1);
  return contiguous ? `lotes ${s[0]}–${s[s.length - 1]}` : `lotes ${s.join(', ')}`;
}

/** @param {any} usage @param {any} gate */
function contextTile(usage, gate) {
  if (!usage) return { id: 'context', label: 'Contexto', value: 'desconocido', tone: /** @type {Tone} */ ('dim'), bar: null, hints: /** @type {string[]} */ ([]) };
  if (gate?.enabled && gate.contextTokens > 0) {
    const left = Math.max(0, gate.contextTokens - usage.tokens);
    const tone = barTone(usage.tokens, gate.contextTokens);
    return {
      id: 'context', label: 'Contexto', value: `${kFmt(usage.tokens)} / ${kFmt(gate.contextTokens)}`, tone,
      bar: { used: usage.tokens, limit: gate.contextTokens, tone }, hints: ['gate on', `${kFmt(left)} libres`],
    };
  }
  if (usage.window > 0) {
    const tone = barTone(usage.tokens, usage.window);
    return {
      id: 'context', label: 'Contexto', value: `${kFmt(usage.tokens)} / ${kFmt(usage.window)}`, tone,
      bar: { used: usage.tokens, limit: usage.window, tone }, hints: ['gate off'],
    };
  }
  return { id: 'context', label: 'Contexto', value: kFmt(usage.tokens), tone: /** @type {Tone} */ ('info'), bar: null, hints: ['gate off'] };
}

/** @param {any} usage */
function costTile(usage) {
  if (!usage || (usage.costUsd == null && !usage.model)) {
    return { id: 'cost', label: 'Costo', value: 'desconocido', tone: /** @type {Tone} */ ('dim'), hints: /** @type {string[]} */ ([]) };
  }
  return {
    id: 'cost', label: 'Costo', value: usage.costUsd != null ? `$${Number(usage.costUsd).toFixed(2)}` : '—',
    tone: /** @type {Tone} */ ('info'), hints: usage.model ? [String(usage.model)] : [],
  };
}

/** The cache card: unknown without `cache`; cold (❄) once the TTL ran out. @param {any} cache */
function cacheTile(cache) {
  if (!cache) return { id: 'cache', label: 'Cache', known: false, value: 'desconocido', tone: /** @type {Tone} */ ('dim'), bar: null, hints: /** @type {string[]} */ ([]) };
  const hint = [];
  const cold = typeof cache.ttlLeftMs === 'number' && cache.ttlLeftMs <= 0;
  if (cold) {
    if (typeof cache.coldCostUsd === 'number') {
      const toks = typeof cache.coldTokens === 'number' ? ` ${kFmt(cache.coldTokens)}` : '';
      hint.push(`el próximo mensaje reescribe${toks} ≈ $${cache.coldCostUsd.toFixed(2)}${cache.estimated ? '*' : ''}`);
    }
    return { id: 'cache', label: 'Cache', known: true, value: '❄ fría', tone: /** @type {Tone} */ ('running'), bar: null, hints: hint };
  }
  if (typeof cache.ttlLeftMs === 'number') hint.push(`vence en ${durText(cache.ttlLeftMs)}`);
  if (typeof cache.coldCostUsd === 'number') hint.push(`en frío: $${cache.coldCostUsd.toFixed(2)}${cache.estimated ? '*' : ''}`);
  const pct = Math.max(0, Math.min(100, Number(cache.hitPct) || 0));
  return {
    id: 'cache', label: 'Cache', known: true, value: `${Math.round(pct)}%`, tone: /** @type {Tone} */ (cache.ttlLeftMs > 0 ? 'ok' : 'running'),
    bar: { used: pct, limit: 100, tone: cache.ttlLeftMs > 0 ? 'ok' : 'running' }, hints: hint,
  };
}

/** The launcher buttons of a tab, resolved and filtered by `needs`. */
function tabActions(tab, snap, owned) {
  const ctx = filterCtx(snap);
  return LAUNCHER.filter((l) => l.tab === tab && launcherAvailable(l, { snap, owned }))
    .map((l) => {
      const e = /** @type {NonNullable<ReturnType<typeof launcherOf>>} */ (launcherOf(l.id, ctx));
      return { id: e.id, label: e.label, hotkey: e.hotkey };
    });
}

/** @param {string} tab @param {any} c */
const actionsBlock = (tab, c) => {
  const actions = tabActions(tab, c.snap, c.owned);
  return actions.length ? [{ type: 'actions', actions }] : [];
};

/** @param {any} c */
function homeBlocks(c) {
  const { snap, owned, usage, now, gate, cache } = c;
  const inFlight = agentsOf(c.inFlight ?? snap?.inFlight);
  /** @type {any} */
  const hero = { type: 'hero', status: 'plan', planValue: '', headline: '', batch: null, elapsedMs: null, steps: [], meta: '', handoff: null };
  if (!snap) {
    Object.assign(hero, { status: 'unread', planValue: 'sin leer', headline: 'Leyendo el plan…', meta: '', handoff: { value: 'sin leer', tone: 'dim' } });
  } else {
    const upd = snap.handoff?.updated;
    if (typeof upd !== 'number') hero.handoff = { value: 'ninguno', tone: 'dim' };
    else {
      const newest = Math.max(0, ...Object.values(snap.verdicts ?? {}).map((v) => /** @type {any} */ (v)?.ts ?? 0));
      const stale = newest > upd;
      hero.handoff = { value: `${agoEs(now - upd)}${stale ? ' · desactualizado' : ''}`, tone: stale ? 'error' : 'info' };
    }
    if (!snap.hash) Object.assign(hero, { status: 'none', planValue: 'ninguno', headline: 'Sin plan en esta rama' });
    else {
      const states = batchStates(snap);
      const dn = states.filter((b) => b.state === 'done').length;
      const state = owned ? `lotes ${dn}/${snap.batches.length}` : snap.approved ? 'aprobado' : 'sin aprobar';
      hero.planValue = `${snap.hash} · ${state}`;
      const cur = states.find((b) => b.state === 'running') ?? states.find((b) => b.state !== 'done');
      if (cur) {
        const b = snap.batches.find((/** @type {any} */ x) => x.n === cur.n);
        hero.batch = { n: cur.n, total: snap.batches.length, title: b?.title || `Batch ${cur.n}`, state: cur.state };
        const launched = snap.launched?.[String(cur.n)];
        if (cur.state === 'running' && typeof launched === 'number') hero.elapsedMs = Math.max(0, now - launched);
        hero.headline = `Lote ${cur.n} de ${snap.batches.length} · ${hero.batch.title}`;
      } else hero.headline = 'Todos los lotes terminaron';
      hero.steps = lifecycle(snap);
      hero.meta = ['plan ' + snap.hash, snap.approved ? 'aprobado' : 'sin aprobar',
        typeof upd === 'number' ? `handoff ${hero.handoff.value}` : null].filter(Boolean).join(' · ');
    }
  }
  /** @type {any} */
  const now_ = { type: 'now', agents: agentRows(snap, inFlight, now), next: '' };
  if (snap?.hash) {
    const running = new Set(inFlight.map((a) => a.batch).filter((n) => n != null));
    const states = batchStates(snap);
    const left = states.filter((b) => b.state === 'pending' && !running.has(b.n)).map((b) => b.n);
    const bits = [];
    if (left.length) bits.push(batchRange(left));
    if (!(snap.suite && suiteDone(snap.suite))) bits.push('la suite');
    if (!snap.review) bits.push('la review');
    if (bits.length) now_.next = `después: ${bits.length > 1 ? `${bits.slice(0, -1).join(', ')} y ${bits[bits.length - 1]}` : bits[0]}`;
  }
  const seg = segmentsOf({
    model: c.live?.model, effort: c.live?.effort, usage, turnUsd: c.live?.turnUsd, cache, limits: usage?.limits, now,
  });
  return [
    hero,
    { type: 'tiles', tiles: [contextTile(usage, gate), costTile(usage), cacheTile(cache)] },
    ...(seg.items.length ? [{ type: 'segments', items: seg.items }] : []),
    now_,
    ...actionsBlock('home', { snap, owned }),
  ];
}

/** Plan tab. @param {any} c */
function planBlocks(c) {
  const { snap, owned, ui, now } = c;
  const actions = actionsBlock('plan', { snap, owned });
  if (!snap?.hash || !snap.batches?.length) return [{ type: 'note', text: 'Sin plan en esta rama', tone: 'dim' }, ...actions];
  const eff = effective(snap);
  const continued = snap.continued ?? [];
  const states = batchStates(snap);
  const runningN = states.find((b) => b.state === 'running')?.n;
  // undefined/null = default (the running batch open); -1 = all closed; n > 0 = that batch.
  const expanded = Number(ui.expanded) > 0 ? Number(ui.expanded) : Number(ui.expanded) === -1 ? -1 : null;
  const items = snap.batches.map((/** @type {any} */ b, /** @type {number} */ i) => {
    const s = eff[String(b.n)]?.status;
    const state = states[i].state;
    const verdict = snap.verdicts?.[String(b.n)];
    const launched = verdict?.launched ?? snap.launched?.[String(b.n)];
    const note = state === 'red' ? `${s === 'not-run' ? 'accept not run' : 'failed'}${continued.includes(b.n) ? ' (continued anyway)' : ''}` : undefined;
    const durationMs = state !== 'pending' && typeof launched === 'number' && typeof verdict?.ts === 'number' && state !== 'running'
      ? Math.max(0, verdict.ts - launched) : state === 'running' && typeof launched === 'number' ? Math.max(0, now - launched) : null;
    const acc = b.accept;
    const accept = typeof acc === 'string' ? acc
      : acc?.kind === 'command' && acc.command ? acc.command : `manual — ${acc?.note ?? ''}`.trimEnd();
    /** @type {{text: string, tone: Tone}} */
    const v = state === 'done' ? { text: '✔ verificado por nxy', tone: 'ok' }
      : state === 'running' ? { text: 'implementer trabajando', tone: 'running' }
        : state === 'red' ? { text: verdict?.error ? String(verdict.error) : 'no pasó el accept', tone: 'error' }
          : { text: 'espera al lote anterior', tone: 'dim' };
    return {
      n: b.n, title: b.title || `Batch ${b.n}`, state, ...(note ? { note } : {}), durationMs,
      open: expanded != null ? expanded === b.n : runningN === b.n,
      files: Array.isArray(b.files) ? b.files : [], accept, verdict: v,
    };
  });
  const suite = snap.suite && suiteDone(snap.suite)
    ? (suiteIsRed(snap.suite) ? { value: '✘ roja', tone: 'error' } : { value: '✔ verde', tone: 'ok' })
    : snap.suite || snap.suiteLaunched ? { value: 'corriendo…', tone: 'running' } : { value: 'pendiente', tone: 'dim' };
  const review = snap.review ? { value: '✔ hecha', tone: 'ok' }
    : snap.reviewLaunched ? { value: 'corriendo…', tone: 'running' } : { value: 'pendiente', tone: 'dim' };
  /** @type {any[]} */
  const blocks = [];
  if (snap.goal) blocks.push({ type: 'goal', text: String(snap.goal) });
  blocks.push({ type: 'batches', items });
  blocks.push({ type: 'suite', rows: [{ label: 'Suite completa', ...suite }, { label: 'Review', ...review }] });
  const rd = snap.reviewDetail;
  if (rd?.findings?.length) {
    const picked = new Set(Array.isArray(ui.picked) ? ui.picked : []);
    const findings = rd.findings.map((/** @type {any} */ f) => ({
      id: f.id, severity: f.severity, lens: f.lens, file: f.file, line: f.line,
      loc: f.file ? `${f.file}${Number.isInteger(f.line) && f.line > 0 ? `:${f.line}` : ''}` : '', title: f.title, cause: f.cause, picked: picked.has(f.id),
    }));
    const n = findings.filter((/** @type {any} */ f) => f.picked).length;
    blocks.push({
      type: 'checkpoint', id: rd.id, findings, picked: n,
      fix: n > 0 && !owned ? { id: 'fix-picked', label: `Arreglar ${n}`, hotkey: FIX_HOTKEY } : null,
    });
  }
  return [...blocks, ...actions];
}

/** @param {any} c */
function agentsBlocks(c) {
  const { snap, now } = c;
  const list = agentRows(snap, agentsOf(c.inFlight ?? snap?.inFlight), now);
  return [
    { type: 'agents', agents: list },
    { type: 'note', text: 'lista completa, tiempo y mensajes: llega en la fase 3', tone: 'dim' },
  ];
}

/** @param {any} c */
function statsBlocks(c) {
  const { usage, stats, trend, summary, now, ui } = c;
  /** @type {any[]} */
  const blocks = [];
  if (summary) blocks.push({ type: 'summary', ...featureSummary(summary), hide: { id: 'summary-hide', label: 'Ocultar' } });
  const d = stats?.state === 'ok' ? stats.data : null;
  if (d) {
    const figs = sessionFigures(d);
    if (figs.length) blocks.push({ type: 'figures', title: 'Esta sesión', items: figs });
    const roles = roleBars(d);
    if (roles.rows.length) blocks.push({ type: 'bars', title: 'Costo por rol', ...roles });
    const models = modelBars(d);
    if (models.rows.length) blocks.push({ type: 'bars', title: 'Por modelo', ...models });
    const w = whatIf1h(d.ttlWhatIf, d.cacheBreaks);
    if (w) {
      const items = [{ id: 'whatif', label: 'con cache 1 h', value: w.text, tone: /** @type {Tone} */ (w.worth ? 'ok' : 'dim') }];
      if (w.avoidableUsd > 0) items.push({ id: 'avoidable', label: 'evitable', value: money(w.avoidableUsd), tone: 'dim' });
      blocks.push({ type: 'figures', title: 'Si la cache durara 1 h', items });
    }
  } else if (!stats || stats.state === 'empty') {
    blocks.push({ type: 'note', text: 'Sin datos de esta sesión todavía', tone: 'dim' });
  }
  const wins = windowRows(usage?.limits, now);
  if (wins.length) blocks.push({ type: 'windows', rows: wins });
  else blocks.push({ type: 'note', text: 'Ventanas 5h/7d: sin suscripción, no hay datos', tone: 'dim' });
  // Trend: the selectors always show; the chart follows the state.
  const sel = { ...DEFAULT_TREND_SEL, ...(ui?.trendSel ?? {}) };
  const opt = (/** @type {string} */ id, /** @type {string} */ label, /** @type {boolean} */ active) => ({ id, label, active });
  blocks.push({
    type: 'selectors', title: 'Tendencia',
    groups: [
      { id: 'metric', options: Object.values(METRICS).map((m) => ({ ...opt(`metric:${m.id}`, m.label, sel.metric === m.id), color: m.color })) },
      { id: 'range', options: TREND_RANGES.map((r) => opt(`range:${r}`, r === '7d' ? '7 días' : '30 días', sel.range === r)) },
      { id: 'by', options: TREND_BYS.map((b) => opt(`by:${b}`, b === 'day' ? 'por día' : b === 'week' ? 'por semana' : 'por modelo', sel.by === b)) },
      { id: 'here', options: [opt('here', 'sólo este repo', !!sel.here)] },
    ],
  });
  if (trend?.state === 'ok' && trend.data) {
    blocks.push({ type: 'chart', chart: trendChart(trend.data, sel.metric, layoutOf(c.bodyColumns ?? 78).CW) });
  } else if (trend?.state === 'error') blocks.push({ type: 'note', text: `Tendencia: ${trend.error ?? 'no se pudo cargar'}`, tone: 'error' });
  else if (trend?.state === 'empty') blocks.push({ type: 'note', text: 'Tendencia: sin datos en este rango', tone: 'dim' });
  else if (trend?.state === 'loading') blocks.push({ type: 'note', text: 'Tendencia: cargando…', tone: 'running' });
  // status line
  const st = stats?.state;
  const status = st === 'loading' ? { text: 'cargando…', tone: 'running' }
    : st === 'error' ? { text: `error: ${stats.error ?? 'no se pudo leer'}`, tone: 'error' }
      : st === 'ok' && typeof stats.at === 'number' ? { text: `actualizado hace ${durText(now - stats.at)} · r actualiza`, tone: 'dim' }
        : { text: 'r actualiza', tone: 'dim' };
  blocks.push({ type: 'note', ...status });
  return blocks;
}

/** @param {any} c */
function configBlocks(c) {
  const cfg = c.snap?.config;
  const unknown = { value: 'desconocido', tone: /** @type {Tone} */ ('dim') };
  const onOff = (/** @type {any} */ b) => (typeof b === 'boolean' ? { value: b ? 'on' : 'off', tone: /** @type {Tone} */ ('info') } : unknown);
  const g = cfg?.gate ?? c.gate;
  const gate = g && typeof g.enabled === 'boolean'
    ? { value: g.enabled ? `on · umbral ${g.contextTokens > 0 ? kFmt(g.contextTokens) : '—'}` : 'off', tone: /** @type {Tone} */ ('info') } : unknown;
  const text = (/** @type {any} */ v) => (v == null ? unknown : { value: String(v), tone: /** @type {Tone} */ ('info') });
  return [
    { type: 'kv', rows: [
      { label: 'Gate', ...gate },
      { label: 'Orquestador', ...text(cfg?.orchestrator) },
      { label: 'Pausa entre lotes', ...onOff(cfg?.pauseAfterBatch) },
      { label: 'Panel', ...text(cfg?.ui?.panel) },
      { label: 'Filtro', ...onOff(cfg?.filter) },
    ] },
    { type: 'note', text: 'cambiar desde acá: llega en la fase 4; hoy .nxy/config.json', tone: 'dim' },
  ];
}

/** One entry per tab id; each returns the tab's `{blocks}`. */
export const BUILDERS = {
  home: (/** @type {any} */ c) => ({ blocks: homeBlocks(c) }),
  plan: (/** @type {any} */ c) => ({ blocks: planBlocks(c) }),
  agents: (/** @type {any} */ c) => ({ blocks: agentsBlocks(c) }),
  stats: (/** @type {any} */ c) => ({ blocks: statsBlocks(c) }),
  config: (/** @type {any} */ c) => ({ blocks: configBlocks(c) }),
};

// ---- the view ----

/**
 * @param {any} snap @param {any} ask @param {{tab?: string, confirm?: string | null}} ui
 * @returns {null | {tone: 'error' | 'info', question: string, buttons: {id: string, label: string, hotkey: string}[]}}
 */
function askOf(snap, ask, ui) {
  if (ui.confirm) {
    const e = launcherOf(ui.confirm);
    if (e?.confirm) {
      return { tone: 'info', question: e.confirm, buttons: [
        { id: 'confirm-yes', label: 'Yes', hotkey: 'y' }, { id: 'confirm-no', label: 'No', hotkey: 'n' }] };
    }
  }
  const open = ask && ask.type === 'ask' ? ask : snap?.batches ? nextActions(snap).actions.find((a) => a.type === 'ask') ?? null : null;
  if (!open || open.type !== 'ask') return null;
  if (open.kind === 'red') {
    return { tone: 'error', question: `Batch ${open.batch} did not pass — how to continue?`, buttons: [
      { id: RETRY, label: RETRY, hotkey: 'a' }, { id: CONTINUE, label: CONTINUE, hotkey: 'c' }, { id: STOP, label: STOP, hotkey: 'x' }] };
  }
  return { tone: 'info', question: `Next: batch${open.batches.length === 1 ? '' : 'es'} ${open.batches.join(', ')} — continue?`, buttons: [
    { id: PAUSE_CONTINUE, label: PAUSE_CONTINUE, hotkey: 'c' }, { id: PAUSE_ADJUST, label: PAUSE_ADJUST, hotkey: 'a' }, { id: PAUSE_STOP, label: PAUSE_STOP, hotkey: 'x' }] };
}

/** Last path segment, either separator. @param {unknown} p */
const lastSegment = (p) => (typeof p === 'string' ? p.replace(/[\\/]+$/, '').split(/[\\/]/).pop() ?? '' : '');

/**
 * @param {{
 *   snap: any, ask?: any, owned?: boolean,
 *   ui?: {tab?: string, confirm?: string | null, expanded?: number | null, picked?: string[]},
 *   output?: {label: string, text: string, running?: boolean} | null,
 *   usage?: {tokens: number, percent?: number, window?: number | null, costUsd?: number | null, model?: string | null} | null,
 *   now?: number, fallback?: {gate?: {enabled?: boolean, contextTokens?: number}},
 *   inFlight?: Record<string, {role: string, batch?: number}> | {role: string, batch?: number}[],
 *   cache?: {hitPct: number, ttlLeftMs: number, ttlMs: number, coldCostUsd: number, coldTokens?: number, estimated?: boolean} | null,
 *   stats?: {state: string, data?: any, at?: number, error?: string} | null,
 *   trend?: {state: string, data?: any, at?: number, error?: string} | null,
 *   summary?: any, live?: {model?: string | null, effort?: string | null, turnUsd?: number | null} | null,
 *   bodyColumns?: number,
 * }} o `owned`: the orchestrator runs the plan in `snap` (the pill and the lotes count follow it).
 * `ui.trendSel` = {metric, range, by, here}; `summary` = featureSummary's input.
 */
export function buildPanel({
  snap, ask = null, owned = false, ui = {}, output = null, usage = null, now = Date.now(), fallback = {}, inFlight, cache = null,
  stats = null, trend = null, summary = null, live = null, bodyColumns = 78,
}) {
  const tab = TABS.some((t) => t.id === ui.tab) ? /** @type {string} */ (ui.tab) : 'home';
  const gate = snap?.config?.gate ?? fallback.gate;
  const ownedSnap = owned && snap?.hash ? snap : null;
  const build = BUILDERS[/** @type {keyof typeof BUILDERS} */ (tab)];
  const running = agentsOf(inFlight ?? snap?.inFlight);
  const progress = batchStates(snap).map((b) => b.state);
  return {
    tab,
    tabs: TABS.map((t) => ({ ...t, active: t.id === tab })),
    header: {
      title: 'nxy', pill: stateOf(ownedSnap, ask),
      repo: lastSegment(snap?.projectDir), branch: typeof snap?.branch === 'string' ? snap.branch : '', progress,
    },
    ask: askOf(ownedSnap, ask, ui),
    output: output
      ? {
        title: output.label, lines: output.running ? [output.text] : clipOutput(output.text, 30),
        ...(output.running ? { running: true } : {}), dismiss: { id: 'dismiss', hotkey: 'd' },
      }
      : null,
    keys: [{ id: 'refresh', hotkey: 'r' }],
    animated: running.length > 0,
    clockTicks: tab === 'home' && typeof cache?.ttlLeftMs === 'number' && cache.ttlLeftMs > 0,
    blocks: build({ snap, owned, usage, now, gate, ui, inFlight, cache, stats, trend, summary, live, bodyColumns }).blocks,
  };
}

const GLYPH = { done: '✔', running: '●', red: '✘', pending: '○' };

/** Plain-text dump of a view: the headless run and the unplaced pane. @param {ReturnType<typeof buildPanel>} view */
export function panelText(view) {
  const out = [];
  out.push(view.tabs.map((t) => (t.active ? `[${t.label}]` : t.label)).join(' '));
  out.push(`${view.header.title} ${view.header.pill.glyph} ${view.header.pill.text}`);
  for (const b of /** @type {any[]} */ (view.blocks)) {
    switch (b.type) {
      case 'hero':
        out.push(`Plan: ${b.planValue}`);
        if (b.status === 'plan') {
          out.push(`  ${b.headline}${b.elapsedMs != null ? ` · ${durText(b.elapsedMs)}` : ''}`);
          if (b.steps.length) out.push(`  ${b.steps.map(stepText).join(' › ')}`);
        } else if (b.status === 'none') out.push(`  ${b.headline}`);
        if (b.handoff) out.push(`Handoff: ${b.handoff.value}`);
        break;
      case 'tiles':
        for (const t of b.tiles) {
          out.push(`${t.label}: ${t.value}`);
          if (t.bar) out.push(`  ${barText(t.bar.used, t.bar.limit, 10)}`);
          if (t.hints.length) out.push(`  ${t.hints.join(' · ')}`);
        }
        break;
      case 'now':
        out.push('Ahora');
        if (!b.agents.length) out.push('  nada corriendo');
        for (const a of b.agents) out.push(`  ● ${a.role}${a.batch != null ? ` · lote ${a.batch}` : ''}${a.sinceMs != null ? ` · ${durText(a.sinceMs)}` : ''}`);
        if (b.next) out.push(`  ${b.next}`);
        break;
      case 'actions':
        out.push(b.actions.map((a) => `[${a.hotkey}] ${a.label}`).join(' '));
        break;
      case 'goal':
        out.push(`Objetivo: ${b.text}`);
        break;
      case 'batches':
        for (const x of b.items) {
          out.push(`${GLYPH[x.state] ?? '○'} ${x.title}${x.note ? ` (${x.note})` : ''}`);
          if (x.open) {
            for (const f of x.files) out.push(`    ${f}`);
            out.push(`    accept: ${x.accept}`, `    ${x.verdict.text}`);
          }
        }
        break;
      case 'suite':
        for (const r of b.rows) out.push(`${r.label}: ${r.value}`);
        break;
      case 'checkpoint':
        out.push(`Review ${b.id}`);
        for (const f of b.findings) out.push(`${f.picked ? '[x]' : '[ ]'} ${f.id} ${f.severity ?? ''} ${f.loc} ${f.title ?? ''}`.replace(/ +/g, ' ').trimEnd());
        if (b.fix) out.push(`[${b.fix.hotkey}] ${b.fix.label}`);
        break;
      case 'agents':
        if (!b.agents.length) out.push('Agentes: ninguno corriendo');
        for (const a of b.agents) out.push(`● ${a.role}${a.batch != null ? ` · lote ${a.batch}` : ''}${a.sinceMs != null ? ` · ${durText(a.sinceMs)}` : ''}`);
        break;
      case 'kv':
        for (const r of b.rows) out.push(`${r.label}: ${r.value}`);
        break;
      case 'note':
        out.push(b.text);
        break;
      case 'segments':
        out.push(b.items.map((i) => `${i.label} ${i.value}`).join(' · '));
        break;
      case 'figures':
        out.push(b.title);
        for (const i of b.items) out.push(`  ${i.label}: ${i.value}`);
        break;
      case 'bars':
        out.push(b.title);
        for (const r of b.rows) out.push(`  ${r.label.padEnd(b.labelWidth)} ${barText(r.ratio, 1, 10)} ${r.text}`);
        break;
      case 'windows':
        for (const r of b.rows) out.push(`${r.label}: ${barText(r.ratio, 1, 10)} ${Math.round(r.percentUsed)}%${r.resetsInMs != null ? ` · resetea en ${durText(r.resetsInMs)}` : ''}`);
        break;
      case 'selectors':
        out.push(b.title);
        for (const g of b.groups) out.push(`  ${g.options.map((o) => (o.active ? `[${o.label}]` : o.label)).join(' ')}`);
        break;
      case 'chart': {
        const ch = b.chart;
        out.push(`${ch.label}${ch.note ? ` (${ch.note})` : ''}`);
        if (ch.kind === 'rows') for (const r of ch.rows) out.push(`  ${r.label.padEnd(ch.labelWidth)} ${barText(r.ratio, 1, 10)} ${r.text}`);
        else out.push(`  ${ch.columns.map((x) => `${x.label} ${x.text}`).join(' | ')}`);
        break;
      }
      case 'summary':
        out.push(`Feature: ${money(b.costUsd)}${b.pct != null ? ` (${b.pct}% de la sesión)` : ''}${b.tokens ? ` · ${b.tokens}` : ''}`);
        for (const x of b.batches) out.push(`  lote ${x.n}: ${x.text}`);
        break;
      default:
    }
  }
  if (view.ask) out.push(view.ask.question, view.ask.buttons.map((b) => `[${b.hotkey}] ${b.label}`).join(' '));
  if (view.output) {
    out.push(`— ${view.output.title}`);
    out.push(...view.output.lines);
  }
  return out;
}
