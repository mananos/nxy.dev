// @ts-check
/**
 * The nxy panel's view model (1.1.0): pure data in, pure data out. The Mod's pane (register.tsx)
 * only draws what `buildPanel` returns; the headless fallback prints `panelText` of the same view.
 *
 * A tab's body is a list of typed blocks (`view.blocks`): hero, tiles, now, actions, goal, batches,
 * suite, checkpoint, note, agents, selectors, rows, plus the Memoria tab's blocks and the Agentes launcher:
 * `memsearch` {input, state, message, query, rows: [{id, press, text, sub}], recent}, `memdetail` {id, state, message, lines, back},
 * `handoff` {state, text, lines, button}, `locate` {input, state, message, question, rows: [{loc, rest, text}], ms, note, degraded, ask},
 * `launch` {kinds, kind, input, running, card, hits, mems, raw, error, open}. An `input` is {field, key, placeholder, submitLabel, value,
 * disabled?}: the driver changes `key` after each submit so the field clears (like the agent composer). The renderer
 * draws a `loc` in cyan and the `rest` muted. Hotkeys there: only memdetail's Volver `b`; the other buttons are pressed
 * with the mouse. The renderer only draws blocks.
 * `rows` = {title, right?, labelWidth, rows: [{label, cells: [{text, tone, prev?, next?, press?, on?, hotkey?, disabled?}], hint?: {text, tone}}]}:
 * a cell with prev/next is a cycler (‹ ›), a cell with press is a button (`on` = ● / ○ state), a plain cell is text;
 * the renderer gives every label `labelWidth` cells and never cuts it.
 * Adding a tab = one entry in TABS plus one builder in BUILDERS (it returns `{blocks}`). Adding a
 * button = one entry in LAUNCHER (with its own `tab`).
 *
 * Pure: no `node:*`, no `process`. Its imports are orchestrator.mjs, batch-status.mjs, statsview.mjs, configedit.mjs, memview.mjs and featuresview.mjs only.
 */
import {
  effective, suiteDone, suiteIsRed, stateOf, nextActions,
  PAUSE_CONTINUE, PAUSE_ADJUST, PAUSE_STOP,
} from './orchestrator.mjs';
import { RETRY, CONTINUE, STOP, isRed, isDone } from './batch-status.mjs';
import {
  sessionFigures, roleBars, modelBars, whatIf1h, windowRows, segmentsOf, trendChart, featureSummary, METRICS, money,
} from './statsview.mjs';
import { isField, ROLES } from './configedit.mjs';
import { clipOutput, parseHits, parseMemIds, memResultRows, memDetailLines, hitRows } from './memview.mjs';
import { featureNames, rowView, newQuestion, closeQuestion } from './featuresview.mjs';

// A launcher's output is clipped for the pane by memview's clipOutput (at most N lines, long lines cut at 160 columns).
export { clipOutput };

/** @typedef {'ok' | 'error' | 'running' | 'info' | 'dim' | 'warn'} Tone */

export const TABS = [
  { id: 'home', label: 'Inicio', hotkey: '1' },
  { id: 'plan', label: 'Plan', hotkey: '2' },
  { id: 'agents', label: 'Agentes', hotkey: '3' },
  { id: 'stats', label: 'Stats', hotkey: '4' },
  { id: 'config', label: 'Config', hotkey: '5' },
  { id: 'memory', label: 'Memoria', hotkey: '6' },
  { id: 'features', label: 'Features', hotkey: '7' },
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
 * @param {{needs?: keyof typeof NEEDS, tool?: string, needsCtx?: (ctx: any) => boolean}} entry @param {{snap: any, owned?: boolean, ctx?: any}} state
 */
export const launcherAvailable = (entry, { snap, owned = false, ctx = null }) =>
  (!entry.needs || NEEDS[entry.needs]({ snap, owned }))
  && (!entry.needsCtx || entry.needsCtx(ctx))
  // An entry with a tool is only available once its install plan is loaded: the command is shown before anything runs.
  && (!entry.tool || Array.isArray(ctx?.tools?.[entry.tool]?.plan?.display));

/**
 * The launcher: a button -> an nxy entry script. `label`/`args` may be functions of the context.
 * `after: 'refresh'` re-reads the snapshot once the process ends; `needs` names a NEEDS check: the
 * button is hidden, and its press ignored, while the check fails; `confirm` asks yes/no first.
 * @type {{id: string, tab: string, label: string | ((ctx: any) => string), hotkey: string, script: string,
 *   args: string[] | ((ctx: any) => string[]), after?: 'refresh' | 'settings' | 'tools' | 'features', needs?: keyof typeof NEEDS, tool?: string,
 *   needsCtx?: (ctx: any) => boolean, confirm?: string | ((ctx: any) => string)}[]}
 */
export const LAUNCHER = [
  { id: 'gate-once', tab: 'home', label: 'Gate once', hotkey: 'g', script: 'gate', args: ['once'] },
  { id: 'handoff', tab: 'home', label: 'Handoff', hotkey: 'h', script: 'mem', args: ['handoff', 'show'] },
  {
    id: 'plan-done', tab: 'plan', label: 'Terminar plan', hotkey: 'e', script: 'mem', args: ['handoff', 'done'],
    needs: 'plan', after: 'refresh', confirm: 'Terminar el plan y archivar el handoff?',
  },
  // Config tab: the buttons that act outside nxy's config file (drawn inside the Config rows, not as an Actions block).
  {
    id: 'cache-ttl', tab: 'config', hotkey: 't', script: 'config', after: 'settings',
    label: (ctx) => (typeof ctx?.ttl1h !== 'boolean' ? 'Cache 1 h' : ctx.ttl1h ? 'Cache 1 h ● on' : 'Cache 1 h ○ off'),
    args: (ctx) => (ctx?.ttl1h === true ? ['cache-ttl', 'off'] : ['cache-ttl', '1h']),
    confirm: 'Escribir promptCacheTtl en ~/.claude/settings.json (con backup)?',
  },
  {
    id: 'statusline', tab: 'config', hotkey: 'l', script: 'config', after: 'settings',
    label: (ctx) => (ctx?.statusline === 'nxy' ? 'Quitar' : 'Instalar'),
    args: (ctx) => (ctx?.statusline === 'nxy' ? ['statusline', 'remove'] : ['statusline', 'install']),
    confirm: 'Cambiar la statusline en ~/.claude/settings.json (con backup)?',
  },
  {
    id: 'update-nxy', tab: 'config', label: 'Actualizar', hotkey: 'u', script: 'config', args: ['update'],
    confirm: 'Actualizar nxy con claude plugin update nxy@nxy-dev? Después hay que reiniciar Claude Code.',
  },
  // An install downloads (codegraph's bundle is ~50 MB): the engine's 30 s default killed it mid-way, so the engine's maximum.
  ...[['rtk', 'i'], ['rg', 'p'], ['codegraph', 'k']].map(([tool, hotkey]) => ({
    id: `setup-${tool}`, tab: 'config', tool, hotkey, script: 'setup', after: /** @type {'tools'} */ ('tools'), timeoutMs: 600000,
    label: (/** @type {any} */ ctx) => (ctx?.tools?.[tool]?.plan?.action === 'reinstall' ? 'Reinstalar' : 'Instalar'),
    args: ['run', tool, '--yes'],
    confirm: (/** @type {any} */ ctx) => {
      const plan = ctx?.tools?.[tool]?.plan;
      const lines = Array.isArray(plan?.display) ? plan.display : [];
      return `${plan?.action === 'reinstall' ? 'Reinstalar' : 'Instalar'} ${tool} con: ${lines.join(' · ')} ¿Correr?`;
    },
  })),
  // Features tab: ctx.features = {name, issue, base, branch, main, target, force, rows}; the buttons are drawn by featuresBlocks (no hotkey).
  {
    id: 'feat-new', tab: 'features', label: 'Crear', hotkey: '', script: 'features', after: 'features',
    needsCtx: (ctx) => !!(ctx?.features?.name && ctx.features.base) || !!ctx?.features?.branch,
    args: (ctx) => {
      const f = ctx?.features ?? {};
      return f.branch ? ['new', '--branch', f.branch]
        : ['new', '--name', f.name, ...(f.issue ? ['--issue', f.issue] : []), '--base', f.base];
    },
    confirm: (ctx) => {
      const f = ctx?.features ?? {};
      const names = featureNames(f.branch ? { branch: f.branch } : { issue: f.issue ?? '', name: f.name ?? '', base: f.base ?? '' }, f.main ?? '');
      return newQuestion(names, f.branch ? null : f.base, f.shell);
    },
  },
  {
    id: 'feat-pause', tab: 'features', hotkey: '', script: 'features', after: 'features',
    needsCtx: (ctx) => !!ctx?.features?.target,
    label: (ctx) => (featRow(ctx)?.pausedAt ? 'Reanudar' : 'Pausar'),
    args: (ctx) => [featRow(ctx)?.pausedAt ? 'resume' : 'pause', '--path', ctx?.features?.target ?? ''],
  },
  {
    id: 'feat-close', tab: 'features', label: 'Cerrar', hotkey: '', script: 'features', after: 'features',
    needsCtx: (ctx) => !!ctx?.features?.target,
    args: (ctx) => ['close', '--path', ctx?.features?.target ?? '', ...(ctx?.features?.force ? ['--force'] : [])],
    confirm: (ctx) => closeQuestion(featRow(ctx) ?? { path: ctx?.features?.target ?? '', branch: null, dirty: ctx?.features?.force ? 1 : 0 }, ctx?.features?.shell),
  },
];

/** The row of `ctx.features.target` in `ctx.features.rows`. @param {any} ctx */
function featRow(ctx) {
  const f = ctx?.features;
  return Array.isArray(f?.rows) ? f.rows.find((/** @type {any} */ r) => r.path === f.target) ?? null : null;
}

/** The Home filter button is a config action (it honours «Guardar en»), not a launcher script. */
const FILTER_ACTION_ID = 'cfg:modules.filter:toggle';
const ENV_FILTER_HINT = 'forzado por NXY_FILTER';

const PANEL_IDS = ['refresh', 'dismiss', 'confirm-yes', 'confirm-no', 'fix-picked', 'here', 'summary-hide',
  'mem-back', 'mem-handoff', 'mem-recent', 'launch:scout', 'launch:librarian', 'launch-ask'];

/** The trend selectors: metric ids come from METRICS. */
export const TREND_RANGES = ['7d', '30d'];
export const TREND_BYS = ['day', 'week', 'model'];
const DEFAULT_TREND_SEL = { metric: 'usd', range: '7d', by: 'day', here: false };

/**
 * A launcher entry resolved against the context ({filter}; `filter` absent while unknown).
 * @param {string} id @param {{filter?: boolean, ttl1h?: boolean, statusline?: string, tools?: Record<string, any>, features?: any}} [ctx]
 * @returns {{id: string, tab: string, label: string, hotkey: string, script: string, args: string[], after?: 'refresh' | 'settings' | 'tools' | 'features', needs?: keyof typeof NEEDS, tool?: string, confirm?: string, timeoutMs?: number} | null}
 */
export function launcherOf(id, ctx = {}) {
  const e = LAUNCHER.find((l) => l.id === id);
  if (!e) return null;
  const { confirm, needsCtx, ...rest } = e;
  return {
    ...rest,
    label: typeof e.label === 'function' ? e.label(ctx) : e.label,
    args: typeof e.args === 'function' ? e.args(ctx) : [...e.args],
    ...(confirm ? { confirm: typeof confirm === 'function' ? confirm(ctx) : confirm } : {}),
  };
}

/**
 * The launcher context of a snapshot: `filter` only once the snapshot carries `config.filter`.
 * @param {any} snap @returns {{filter?: boolean}}
 */
export const filterCtx = (snap) => (typeof snap?.config?.filter === 'boolean' ? { filter: snap.config.filter } : {});

/**
 * The launcher context of the Config tab: the snapshot's filter plus what ~/.claude/settings.json says.
 * `settings` = {promptCacheTtl, statusLine} as read (or already {ttl1h, statusline}); a field stays absent while unknown.
 * `tools` = config.mjs tools' per-tool info (with `plan`), as the install buttons need it.
 * @param {any} snap @param {any} [settings] @param {Record<string, any> | null} [tools]
 * @returns {{filter?: boolean, ttl1h?: boolean, statusline?: 'nxy' | 'other' | 'none', tools?: Record<string, any>}}
 */
export function configCtx(snap, settings, tools = null) {
  const ctx = /** @type {any} */ ({ ...filterCtx(snap) });
  if (tools && typeof tools === 'object') ctx.tools = tools;
  if (settings && typeof settings === 'object') {
    if (typeof settings.ttl1h === 'boolean') ctx.ttl1h = settings.ttl1h;
    else ctx.ttl1h = settings.promptCacheTtl === '1h';
    if (['nxy', 'other', 'none'].includes(settings.statusline)) ctx.statusline = settings.statusline;
    else {
      const cmd = settings.statusLine?.command;
      ctx.statusline = typeof cmd === 'string' ? (/nxy.*statusline\.mjs|statusline\.mjs.*nxy/i.test(cmd) ? 'nxy' : 'other') : settings.statusLine ? 'other' : 'none';
    }
  }
  return ctx;
}

/** `cfg:<fieldId>:next|prev|toggle`, only for a field configedit knows. @param {string} id */
function isCfgAction(id) {
  const m = /^cfg:([^:]+):(next|prev|toggle)$/.exec(id);
  return !!m && isField(m[1]);
}

/** @param {string} id */
export const isPanelAction =(id) => PANEL_IDS.includes(id) || /^scope:(user|repo)$/.test(id) || isCfgAction(id) ||
  id.startsWith('tab:') || /^batch:\d+$/.test(id)
  || /^feat:(open|pause|close):\d+$/.test(id) || /^feat:mode:(new|existing|cancel)$/.test(id)
  || /^pick:\S+$/.test(id) || /^mem:\S+$/.test(id) || /^agent:\S+$/.test(id) || id === 'agent-back' || id === 'agents-all' || LAUNCHER.some((l) => l.id === id)
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

/**
 * Agent rows in the shape `agentview.agentRows` builds. Without the driver's rows (`c.agents`), the
 * orchestrator's in-flight agents stand in, with the time since their batch launched.
 * @param {any} c
 * @returns {any[]}
 */
function rowsOf(c) {
  if (Array.isArray(c.agents)) return c.agents;
  const { snap, now } = c;
  return agentsOf(c.inFlight ?? snap?.inFlight).map((a, i) => {
    const t = a.batch != null ? snap?.launched?.[String(a.batch)] : null;
    const elapsedMs = typeof t === 'number' ? Math.max(0, now - t) : null;
    return {
      id: `flight-${i}`, role: a.role, nxy: true, label: a.role, batch: a.batch ?? null, state: 'corriendo', tone: 'running', live: true,
      model: '', effort: '', modelEffort: '', activity: elapsedMs != null ? `trabajando… ${Math.round(elapsedMs / 1000)}s` : 'trabajando…',
      elapsedMs, costText: '', tokensText: '',
    };
  });
}

/** One text line for an agent row. @param {any} a */
const rowText = (a) => [
  `${a.live ? '●' : a.tone === 'error' ? '✘' : a.tone === 'dim' ? '■' : '✔'} ${a.role}${a.batch != null ? ` · lote ${a.batch}` : ''}`,
  a.modelEffort, a.state, a.elapsedMs != null ? durText(a.elapsedMs) : '', a.costText, a.activity,
].filter(Boolean).join(' · ');

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
  const actions = LAUNCHER.filter((l) => l.tab === tab && launcherAvailable(l, { snap, owned }))
    .map((l) => {
      const e = /** @type {NonNullable<ReturnType<typeof launcherOf>>} */ (launcherOf(l.id, ctx));
      return { id: e.id, label: e.label, hotkey: e.hotkey };
    });
  if (tab === 'home') {
    // Unknown state (no snapshot yet): a plain 'Filter', never a guessed 'off'.
    actions.push({ id: FILTER_ACTION_ID, hotkey: 'f', label: typeof ctx.filter !== 'boolean' ? 'Filter' : ctx.filter ? 'Filter ● on' : 'Filter ○ off' });
  }
  return actions;
}

/** @param {string} tab @param {any} c */
const actionsBlock = (tab, c) => {
  const actions = tabActions(tab, c.snap, c.owned);
  const f = c.snap?.config?.sources?.['modules.filter'];
  // The Home button honours «Guardar en»: env always wins; the repo shadows a user-scoped write.
  const userScope = c.ui?.scope !== 'repo';
  const hint = tab === 'home' && f === 'env' ? [{ type: 'note', text: `Filter: ${ENV_FILTER_HINT}`, tone: 'warn' }]
    : tab === 'home' && f === 'repo' && userScope ? [{ type: 'note', text: 'Filter: el repo manda', tone: 'warn' }] : [];
  return actions.length ? [{ type: 'actions', actions }, ...hint] : [];
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
  const now_ = { type: 'now', agents: rowsOf(c).filter((a) => a.live), next: '' };
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
    ...actionsBlock('home', { snap, owned, ui: c.ui }),
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
  const { ui } = c;
  const rows = rowsOf(c);
  const d = ui.agent && c.agentDetail ? c.agentDetail : null;
  if (d) {
    return [{
      type: 'agent', id: ui.agent, card: d.card, prompt: d.prompt ? clipOutput(d.prompt, 12) : [], tools: d.tools,
      result: d.result ? clipOutput(d.result, 20) : [], error: d.error ? clipOutput(d.error, 12) : [], batch: d.batch,
      sent: d.sent.map((/** @type {any} */ s) => ({ text: s.text, state: s.state === 'no entregado' && s.reason ? `no entregado: ${s.reason}` : s.state })),
      canSend: d.canSend, confirm: d.confirm,
      composer: { key: `${ui.agent}:${d.sentCount ?? d.sent.length}`, placeholder: 'mensaje para este agente', submitLabel: 'enviar', value: '' },
      back: { id: 'agent-back', label: 'Volver', hotkey: 'b' },
    }];
  }
  /** @type {any[]} */
  const blocks = [];
  const watched = c.viewAgentId ? rows.find((a) => a.id === c.viewAgentId) : null;
  if (watched) blocks.push({ type: 'watching', card: watched, open: { id: `agent:${watched.id}`, label: 'Ver' } });
  const running = rows.filter((a) => a.live).length;
  const failed = rows.filter((a) => !a.live && a.tone === 'error').length;
  const stopped = rows.filter((a) => !a.live && a.tone === 'dim').length;
  const shown = ui.agentsAll ? rows : rows.slice(0, 10);
  blocks.push({
    type: 'agents', agents: shown, counts: { running, done: rows.length - running - failed - stopped, failed, stopped },
    more: shown.length < rows.length ? { id: 'agents-all', label: `Ver todos (+${rows.length - shown.length})` }
      : ui.agentsAll && rows.length > 10 ? { id: 'agents-all', label: 'Ver menos' } : null,
  });
  blocks.push(launchBlock(c, rows));
  return blocks;
}

/**
 * The Agentes launcher: ask the scout (code, by path:line) or the librarian (memory, by meaning).
 * `ui.launch` = {kind, agentId, question, key?, result?, error?}; the driver fills `result` from the book once the agent ends.
 * @param {any} c @param {any[]} rows
 */
function launchBlock(c, rows) {
  const l = c.ui?.launch ?? {};
  const kind = l.kind === 'librarian' ? 'librarian' : 'scout';
  const card = l.agentId ? rows.find((a) => a.id === l.agentId) ?? null : null;
  const running = !!card?.live;
  const result = typeof l.result === 'string' ? l.result : '';
  const done = !!card && !running && result.trim() !== '';
  const hits = done && kind === 'scout' ? hitRows(parseHits(result)) : [];
  const mems = done && kind === 'librarian'
    ? parseMemIds(result).map((m) => ({ id: m.id, press: `mem:${m.id}`, text: m.id, sub: m.why })) : [];
  return {
    type: 'launch',
    title: 'Preguntarle a un agente',
    kinds: [
      { id: 'launch:scout', label: 'Scout', active: kind === 'scout' },
      { id: 'launch:librarian', label: 'Librarian', active: kind === 'librarian' },
    ],
    kind,
    hint: kind === 'scout' ? 'el scout busca en el código (Haiku, cuesta tokens)' : 'el librarian busca en la memoria por sentido (Haiku, cuesta tokens)',
    input: {
      field: 'launch', key: `launch:${l.key ?? 0}`, value: '', disabled: running,
      placeholder: kind === 'scout' ? 'dónde está… / cómo funciona…' : 'qué decidimos sobre…',
      submitLabel: running ? 'en curso' : 'preguntar',
    },
    running,
    question: typeof l.question === 'string' ? l.question : '',
    card,
    hits,
    mems,
    raw: done && !hits.length && !mems.length ? clipOutput(result, 12) : [],
    error: typeof l.error === 'string' && l.error ? l.error : '',
    open: card ? { id: `agent:${card.id}`, label: 'Ver agente' } : null,
  };
}

/**
 * Memoria tab: search, one memory, the branch handoff and the model-free locate.
 * `ui.mem` = {query, results, state, error, open, detail: {state, data: {memory, edges}, error},
 * handoff: {state, data, error}, locate: {question, state, data: {hits, ms, degraded}, error}, key?, locate.key?}.
 * @param {any} c
 */
function memoryBlocks(c) {
  const m = c.ui?.mem ?? {};
  /** @type {any[]} */
  const blocks = [];
  const input = (/** @type {string} */ field, /** @type {any} */ key, /** @type {string} */ placeholder, /** @type {string} */ label) =>
    ({ field, key: `${field}:${key ?? 0}`, placeholder, submitLabel: label, value: '' });

  if (m.open) {
    const d = m.detail ?? {};
    const back = { id: 'mem-back', label: 'Volver', hotkey: 'b' };
    if (d.state === 'ok' && d.data?.memory) {
      blocks.push({ type: 'memdetail', id: m.open, state: 'ok', message: '', lines: memDetailLines(d.data.memory, d.data.edges), back });
    } else if (d.state === 'error') {
      blocks.push({ type: 'memdetail', id: m.open, state: 'error', message: `No se pudo abrir la memoria: ${d.error ?? 'error desconocido'}`, lines: [], back });
    } else if (d.state === 'empty') {
      blocks.push({ type: 'memdetail', id: m.open, state: 'empty', message: 'Esa memoria ya no existe. Volvé y buscá de nuevo.', lines: [], back });
    } else {
      blocks.push({ type: 'memdetail', id: m.open, state: 'loading', message: 'Abriendo la memoria…', lines: [], back });
    }
  } else {
    const rows = memResultRows(m.results);
    const q = typeof m.query === 'string' ? m.query : '';
    /** @type {{state: string, message: string}} */
    const st = m.state === 'loading' ? { state: 'loading', message: 'Buscando…' }
      : m.state === 'error' ? { state: 'error', message: `No se pudo buscar: ${m.error ?? 'error desconocido'}` }
        : rows.length ? { state: 'ok', message: '' }
          : m.state === 'ok' || m.state === 'empty' ? {
            state: 'empty',
            message: q ? `Nada coincide con «${q}». Probá con las palabras que usaría la nota; el librarian busca por sentido.` : 'Todavía no hay memorias guardadas en este repo.',
          }
            : { state: 'idle', message: 'Escribí palabras de la nota, o mirá las recientes.' };
    blocks.push({
      type: 'memsearch', input: input('mem-search', m.key, 'palabras de la nota', 'buscar'), ...st, query: q, rows,
      recent: rows.length ? null : { id: 'mem-recent', label: 'Recientes' },
    });
  }

  const h = m.handoff ?? {};
  const hd = h.data;
  /** @type {{state: string, text: string}} */
  const hs = h.state === 'loading' ? { state: 'loading', text: 'Handoff: cargando…' }
    : h.state === 'error' ? { state: 'error', text: `Handoff: no se pudo leer (${h.error ?? 'error desconocido'})` }
      : hd && hd.exists === false ? { state: 'empty', text: `Sin handoff en la rama ${hd.label ?? ''}`.trimEnd() }
        : hd ? {
          state: 'ok',
          text: ['Handoff', hd.label, hd.updated ? `actualizado ${hd.updated}` : '', hd.shared ? 'compartido' : '', typeof hd.progress === 'string' ? hd.progress : '']
            .filter(Boolean).join(' · '),
        }
          : { state: 'idle', text: 'Handoff: sin leer todavía' };
  blocks.push({
    type: 'handoff', ...hs, lines: hs.state === 'ok' && typeof hd?.body === 'string' ? clipOutput(hd.body, 20) : [],
    button: { id: 'mem-handoff', label: 'Handoff' },
  });

  const lc = m.locate ?? {};
  const q = typeof lc.question === 'string' ? lc.question : '';
  const hits = hitRows(lc.data?.hits);
  const lau = c.ui?.launch ?? {};
  const askLive = !!(lau.agentId && rowsOf(c).find((a) => a.id === lau.agentId)?.live);
  /** @type {{state: string, message: string}} */
  const ls = lc.state === 'loading' ? { state: 'loading', message: 'Buscando en el código…' }
    : lc.state === 'error' ? { state: 'error', message: `No se pudo buscar en el código: ${lc.error ?? 'error desconocido'}` }
      : hits.length ? { state: 'ok', message: '' }
        : lc.state === 'ok' || lc.state === 'empty' ? { state: 'empty', message: `No encontré nada para «${q}». Probá con otro nombre, o preguntale al scout.` }
          : { state: 'idle', message: 'Escribí qué buscás en el código: sin modelo, 0 tokens.' };
  blocks.push({
    type: 'locate', input: input('locate', lc.key, 'dónde está… (un nombre, una idea)', 'ubicar'), ...ls, question: q, rows: hits,
    ms: typeof lc.data?.ms === 'number' ? lc.data.ms : null,
    note: 'sin modelo · 0 tokens',
    degraded: Array.isArray(lc.data?.degraded) ? lc.data.degraded.map(String) : [],
    ask: q && (ls.state === 'ok' || ls.state === 'empty')
      ? { id: askLive ? '' : 'launch-ask', label: askLive ? 'Scout en curso' : 'Preguntarle al scout', disabled: askLive } : null,
    askNote: askLive ? 'Scout en curso, mirá Agentes' : '',
  });
  return blocks;
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
  const scope = c.ui?.scope === 'repo' ? 'repo' : 'user';
  const ctx = configCtx(c.snap, c.settings, c.cfgInfo && !c.cfgInfo.failed ? c.cfgInfo.tools : null);
  const src = (/** @type {string} */ id) => cfg?.sources?.[id];
  /** @type {{text: string, tone: Tone}} */
  const unknown = { text: 'desconocido', tone: 'dim' };
  const cell = (/** @type {string} */ text, /** @type {Tone} */ tone = 'info', /** @type {any} */ extra = {}) => ({ text, tone, ...extra });
  const cycler = (/** @type {string} */ id, /** @type {string} */ text, /** @type {Tone} */ tone = 'info') =>
    cell(text, tone, { prev: `cfg:${id}:prev`, next: `cfg:${id}:next` });
  const toggler = (/** @type {string} */ id, /** @type {string} */ text, /** @type {boolean} */ on) =>
    cell(text, 'info', { press: `cfg:${id}:toggle`, on });
  const button = (/** @type {string} */ id) => {
    const e = launcherOf(id, ctx);
    return e ? cell(e.label, 'info', { press: e.id, hotkey: e.hotkey }) : unknown;
  };
  /** A row; the «el repo manda» hint shows when we save to the user file and the repo defines one of its fields. */
  const row = (/** @type {string} */ label, /** @type {any[]} */ cells, /** @type {string[]} */ fields = [], /** @type {any} */ hint = null) => {
    const forced = fields.some((f) => src(f) === 'env');
    const shadowed = scope === 'user' && fields.some((f) => src(f) === 'repo');
    return {
      label, cells,
      ...(forced ? { hint: { text: ENV_FILTER_HINT, tone: 'warn' } } : shadowed ? { hint: { text: 'el repo manda', tone: 'warn' } } : hint ? { hint } : {}),
    };
  };
  const block = (/** @type {string} */ title, /** @type {any[]} */ rows, /** @type {string} */ right = '') => {
    const labelWidth = Math.max(0, ...rows.map((r) => r.label.length)) + 1;
    return { type: 'rows', title, ...(right ? { right } : {}), labelWidth, rows };
  };
  const optional = (/** @type {string} */ id, /** @type {any} */ v) => {
    const set = typeof v === 'string' && v;
    return cycler(id, set ? v : 'predeterminado', set && src(id) !== 'default' ? 'info' : 'dim');
  };

  /** @type {any[]} */
  const blocks = [];
  const opt = (/** @type {string} */ id, /** @type {string} */ label, /** @type {boolean} */ active) => ({ id, label, active });
  blocks.push({
    type: 'selectors', title: 'Guardar en',
    groups: [{ id: 'scope', options: [opt('scope:user', 'Usuario', scope === 'user'), opt('scope:repo', 'Repo', scope === 'repo')] }],
  });
  const sc = cfg?.scopes?.[scope];
  blocks.push({ type: 'note', text: scope === 'user' ? '~/.nxy/config.json · vale para todos tus repos' : '.nxy/config.json · se commitea con el repo', tone: 'dim' });
  if (sc?.broken) blocks.push({ type: 'note', text: 'Ese archivo está roto: nxy no lo toca hasta que lo arregles', tone: 'error' });
  if (c.cfgNote) {
    const n = typeof c.cfgNote === 'string' ? { text: c.cfgNote, tone: 'error' } : c.cfgNote;
    if (n.text) blocks.push({ type: 'note', text: n.text, tone: n.tone ?? 'error' });
  }

  const g = cfg?.gate ?? c.gate;
  const known = !!cfg;
  blocks.push(block('Roles', ROLES.map((r) => {
    const mid = `roles.${r}.model`;
    const eid = `roles.${r}.effort`;
    return row(r, known
      ? [optional(mid, cfg.roles?.[r]?.model), optional(eid, cfg.roles?.[r]?.effort)]
      : [cell(unknown.text, 'dim')], [mid, eid]);
  })));
  blocks.push(block('Flujo', [
    row('Orquestador', typeof cfg?.orchestrator !== 'string' ? [cell(unknown.text, 'dim')]
      : c.owned ? [cell(cfg.orchestrator, 'dim', { disabled: true })] : [toggler('flow.orchestrator', cfg.orchestrator, cfg.orchestrator !== 'off')],
    ['flow.orchestrator'], c.owned ? { text: 'corriendo un plan', tone: 'dim' } : null),
    row('Pausa entre lotes', typeof cfg?.pauseAfterBatch !== 'boolean' ? [cell(unknown.text, 'dim')]
      : [toggler('flow.pauseAfterBatch', cfg.pauseAfterBatch ? 'on' : 'off', cfg.pauseAfterBatch)], ['flow.pauseAfterBatch']),
  ]));
  blocks.push(block('Gate', [
    row('Gate', known && typeof g?.enabled === 'boolean' ? [toggler('gate.enabled', g.enabled ? 'on' : 'off', g.enabled)] : [cell(unknown.text, 'dim')], ['gate.enabled']),
    row('Umbral', known && g ? [cycler('gate.contextTokens', g.contextTokens > 0 ? kFmt(g.contextTokens) : '—')] : [cell(unknown.text, 'dim')], ['gate.contextTokens']),
  ]));

  const d = c.stats?.state === 'ok' ? c.stats.data : null;
  const w = d ? whatIf1h(d.ttlWhatIf, d.cacheBreaks) : null;
  const cacheRows = [
    row('TTL observado', [typeof c.cache?.ttlMs === 'number' && c.cache.ttlMs > 0
      ? cell(`${durText(c.cache.ttlMs)}${c.cache.estimated ? ' (estimado)' : ''}`) : cell('sin datos', 'dim')]),
    row('Settings', [typeof ctx.ttl1h !== 'boolean' ? cell(unknown.text, 'dim') : cell(ctx.ttl1h ? '1 h' : 'predeterminado', ctx.ttl1h ? 'info' : 'dim')]),
    row('Con 1 h', [w ? cell(w.text, w.worth ? 'ok' : 'dim') : cell('sin datos de Stats', 'dim')]),
  ];
  if (known) cacheRows.push(row('Cache 1 h', [button('cache-ttl')], [], { text: 'Claude Code la toma en la próxima sesión', tone: 'dim' }));
  blocks.push(block('Cache', cacheRows));

  blocks.push(block('Interfaz', [
    row('Filtro', typeof cfg?.filter !== 'boolean' ? [cell(unknown.text, 'dim')] : [toggler('modules.filter', cfg.filter ? 'on' : 'off', cfg.filter)], ['modules.filter']),
    row('Panel', typeof cfg?.ui?.panel !== 'string' ? [cell(unknown.text, 'dim')] : [toggler('ui.panel', cfg.ui.panel, cfg.ui.panel !== 'off')], ['ui.panel']),
  ]));

  const slText = ctx.statusline === 'nxy' ? 'nxy' : ctx.statusline === 'other' ? 'otra' : ctx.statusline === 'none' ? 'ninguna' : unknown.text;
  blocks.push(block('Statusline', [
    row('Estado', [cell(slText, ctx.statusline ? 'info' : 'dim'), ...(known && ctx.statusline ? [button('statusline')] : [])]),
  ]));

  const info = c.cfgInfo;
  const toolRow = (/** @type {string} */ name) => {
    const t = info?.tools?.[name];
    if (!info) return row(name, [cell('cargando…', 'dim')]);
    if (info.failed) return row(name, [cell(unknown.text, 'dim')]);
    const entry = LAUNCHER.find((l) => l.tool === name);
    const btn = entry && launcherAvailable(entry, { snap: c.snap, owned: c.owned, ctx }) ? [button(entry.id)] : [];
    const state = t?.found ? cell(`encontrada${t.version ? ` ${t.version}` : ''}`, 'ok') : cell('no encontrada', 'dim');
    const clashText = name === 'rtk' ? 'choca: hook de rtk' : t?.conflict === 'mcp' ? 'choca: servidor MCP de codegraph' : 'choca: hook en settings';
    const clash = t?.conflict ? [cell(clashText, 'warn')] : [];
    // A tool found on PATH has its bare name as path: repeating `rtk` under `rtk` said nothing.
    const where = !t?.found || !t.path ? null : /[\\/]/.test(t.path) ? t.path : 'en el PATH';
    return row(name, [state, ...clash, ...btn], [], where ? { text: where, tone: 'dim' } : null);
  };
  blocks.push(block('Herramientas', ['rtk', 'rg', 'codegraph'].map(toolRow)));
  blocks.push(block('nxy', [
    row('Versión', [info?.version ? cell(String(info.version)) : cell(info ? unknown.text : 'cargando…', 'dim'), ...(known ? [button('update-nxy')] : [])]),
  ]));
  return blocks;
}

/**
 * Features tab: the worktrees of the repo and the "new" steps.
 * `ui.features` = {state, data, error, at, mode, step, issue, name, base, key, target, force, note}; `data` = features.mjs list.
 * Block `worktrees` = {title, state, message, rows: [{id, index, name, branch, tags, lines, buttons: [{id, label, disabled?, reason?}]}],
 * modes: button[], input?, select?, busy: string[], info: string[], cancel?, note}. No button has a hotkey (focus ring).
 * @param {any} c
 */
function featuresBlocks(c) {
  const f = c.ui?.features ?? {};
  const data = f.data && f.data.ok !== false ? f.data : null;
  const now = typeof c.now === 'number' ? c.now : Date.now();
  const key = (/** @type {string} */ field) => `${field}:${f.key ?? 0}`;
  /** @type {string} */
  let state = 'ok';
  let message = '';
  if (f.state === 'loading' && !data) { state = 'loading'; message = 'Leyendo los worktrees…'; }
  else if (f.state === 'error' || (f.data && f.data.ok === false)) {
    state = 'error';
    message = `No se pudieron leer los worktrees: ${f.error ?? f.data?.reason ?? 'error desconocido'}`;
  } else if (!data) { state = 'loading'; message = 'Leyendo los worktrees…'; }
  const rows = (data?.worktrees ?? []).map((/** @type {any} */ wt, /** @type {number} */ i) => {
    const v = rowView(wt, { now });
    const block = wt.main ? 'el principal no se cierra' : wt.current ? 'es la sesión actual' : wt.running ? 'tiene un plan corriendo' : '';
    return {
      id: wt.path, index: i, ...v,
      buttons: [
        { id: `feat:open:${i}`, label: 'Abrir', ...(wt.current ? { disabled: true, reason: 'ya estás acá' } : {}) },
        { id: `feat:pause:${i}`, label: wt.pausedAt ? 'Reanudar' : 'Pausar' },
        { id: `feat:close:${i}`, label: 'Cerrar', ...(block ? { disabled: true, reason: block } : {}) },
      ],
    };
  });
  /** @type {any[]} */
  const modes = [];
  /** @type {any} */
  let input = null;
  /** @type {any} */
  let select = null;
  /** @type {string[]} */
  const busy = [];
  /** @type {string[]} */
  const info = [];
  if (data) {
    if (!f.mode) {
      modes.push({ id: 'feat:mode:new', label: 'Rama nueva' }, { id: 'feat:mode:existing', label: 'Rama existente' });
    } else {
      modes.push({ id: 'feat:mode:cancel', label: 'Cancelar' });
      if (f.mode === 'new') {
        if (f.step === 'name') {
          input = { field: 'feature-name', key: key('feature-name'), placeholder: 'nombre del feature', submitLabel: 'seguir', value: '' };
        } else if (f.step === 'base') {
          const names = featureNames({ issue: f.issue ?? '', name: f.name ?? '', base: '-' }, data.main ?? '');
          info.push(`rama ${names.branch} — falta elegir de qué rama sale`);
          const bases = Array.isArray(data.bases) ? data.bases : [];
          if (!bases.length) info.push('no hay ramas locales');
          else {
            select = {
              pick: 'base', key: key('feature-base'), label: 'sale de', value: data.defaultBranch ?? bases[0],
              options: bases.map((/** @type {string} */ b) => ({ value: b, label: b === (data.defaultBranch ?? bases[0]) ? `${b} (principal)` : b })),
            };
          }
        } else {
          input = { field: 'feature-issue', key: key('feature-issue'), placeholder: 'n.º de issue, Enter vacío = sin issue', submitLabel: 'seguir', value: '' };
        }
      } else {
        const free = data.branches?.free ?? [];
        for (const b of data.branches?.busy ?? []) busy.push(`rama ${b.name} — abierta en ${b.path}, no se puede elegir`);
        if (!free.length) info.push('no hay ramas libres');
        else {
          select = {
            pick: 'branch', key: key('feature-branch'), label: 'rama', value: '',
            options: [{ value: '', label: 'elegí una rama…' }, ...free.map((/** @type {string} */ b) => ({ value: b, label: b }))],
          };
        }
      }
    }
  }
  const err = typeof f.error === 'string' && f.error && state === 'ok' ? f.error : '';
  return [{
    type: 'worktrees', title: 'Worktrees', state, message, rows, modes, input, select, busy, info, error: err,
    note: typeof f.note === 'string' && f.note ? f.note
      : 'Una sesión de Claude por worktree. Abrir copia el comando; nxy nunca commitea ni pushea.',
  }];
}

/** One entry per tab id; each returns the tab's `{blocks}`. */
export const BUILDERS = {
  home: (/** @type {any} */ c) => ({ blocks: homeBlocks(c) }),
  plan: (/** @type {any} */ c) => ({ blocks: planBlocks(c) }),
  agents: (/** @type {any} */ c) => ({ blocks: agentsBlocks(c) }),
  stats: (/** @type {any} */ c) => ({ blocks: statsBlocks(c) }),
  config: (/** @type {any} */ c) => ({ blocks: configBlocks(c) }),
  memory: (/** @type {any} */ c) => ({ blocks: memoryBlocks(c) }),
  features: (/** @type {any} */ c) => ({ blocks: featuresBlocks(c) }),
};

// ---- the view ----

/**
 * @param {any} snap @param {any} ask @param {{tab?: string, confirm?: string | null, pendingSend?: {agentId: string, text: string, batch?: number | null} | null}} ui
 * @returns {null | {tone: 'error' | 'info', at?: 'bottom', question: string, buttons: {id: string, label: string, hotkey: string}[]}}
 */
function askOf(snap, ask, ui, ctx = {}) {
  if (ui.pendingSend) {
    const n = ui.pendingSend.batch;
    return { tone: 'info', question: `Hablarle al implementer${n != null ? ` del lote ${n}` : ''} puede romper el lote. ¿Mandar igual?`, buttons: [
      { id: 'confirm-yes', label: 'Yes', hotkey: 'y' }, { id: 'confirm-no', label: 'No', hotkey: 'n' }] };
  }
  if (ui.confirm) {
    const e = launcherOf(ui.confirm, ctx);
    if (e?.confirm) {
      // `at: 'bottom'`: drawn next to the output box, near the button. Config's buttons sit at the end of a
      // long tab: a question at the top was off-screen, so Actualizar seemed to do nothing (2026-10-10).
      return { tone: 'info', at: 'bottom', question: e.confirm, buttons: [
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
 *   ui?: {tab?: string, scope?: 'user' | 'repo', confirm?: string | null, expanded?: number | null, picked?: string[], agent?: string | null, agentsAll?: boolean,
 *     pendingSend?: {agentId: string, text: string, batch?: number | null} | null, mem?: any, features?: any,
 *     launch?: {kind?: string, agentId?: string | null, question?: string, key?: number | string, result?: string, error?: string} | null},
 *   agents?: any[] | null, agentDetail?: any, viewAgentId?: string | null,
 *   output?: {label: string, text: string, running?: boolean} | null,
 *   usage?: {tokens: number, percent?: number, window?: number | null, costUsd?: number | null, model?: string | null} | null,
 *   now?: number, fallback?: {gate?: {enabled?: boolean, contextTokens?: number}},
 *   inFlight?: Record<string, {role: string, batch?: number}> | {role: string, batch?: number}[],
 *   cache?: {hitPct: number, ttlLeftMs: number, ttlMs: number, coldCostUsd: number, coldTokens?: number, estimated?: boolean} | null,
 *   stats?: {state: string, data?: any, at?: number, error?: string} | null,
 *   trend?: {state: string, data?: any, at?: number, error?: string} | null,
 *   summary?: any, live?: {model?: string | null, effort?: string | null, turnUsd?: number | null} | null,
 *   bodyColumns?: number, settings?: any, cfgInfo?: {version?: string | null, failed?: boolean, tools?: Record<string, {found?: boolean, path?: string | null, version?: string | null, conflict?: boolean | string, plan?: {action: string, display: string[]}}>} | null,
 *   cfgNote?: string | {text: string, tone?: string} | null,
 * }} o `owned`: the orchestrator runs the plan in `snap` (the pill and the lotes count follow it).
 * `ui.trendSel` = {metric, range, by, here}; `summary` = featureSummary's input.
 */
export function buildPanel({
  snap, ask = null, owned = false, ui = {}, output = null, usage = null, now = Date.now(), fallback = {}, inFlight, cache = null,
  stats = null, trend = null, summary = null, live = null, bodyColumns = 78, agents = null, agentDetail = null, viewAgentId = null,
  settings = null, cfgInfo = null, cfgNote = null,
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
    ask: askOf(ownedSnap, ask, ui, {
      ...configCtx(snap, settings, cfgInfo && !cfgInfo.failed ? cfgInfo.tools : null),
      features: {
        name: ui.features?.name ?? '', issue: ui.features?.issue ?? '', base: ui.features?.base ?? '', branch: ui.features?.branch ?? '',
        main: ui.features?.data?.main ?? '', target: ui.features?.target ?? '', force: !!ui.features?.force, rows: ui.features?.data?.worktrees ?? [],
        shell: ui.features?.data?.shell ?? null,
      },
    }),
    output: output
      ? {
        title: output.label, lines: output.running ? [output.text] : clipOutput(output.text, 30),
        ...(output.running ? { running: true } : {}), dismiss: { id: 'dismiss', hotkey: 'd' },
      }
      : null,
    keys: [{ id: 'refresh', hotkey: 'r' }],
    animated: running.length > 0 || (Array.isArray(agents) && agents.some((a) => a.live)),
    clockTicks: tab === 'home' && typeof cache?.ttlLeftMs === 'number' && cache.ttlLeftMs > 0,
    blocks: build({ snap, owned, usage, now, gate, ui, inFlight, cache, stats, trend, summary, live, bodyColumns, agents, agentDetail, viewAgentId, settings, cfgInfo, cfgNote }).blocks,
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
        for (const a of b.agents) out.push(`  ${rowText(a)}`);
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
        if (!b.agents.length) out.push('Agentes: ninguno');
        else out.push(`Agentes: ${b.counts.running} corriendo · ${b.counts.done} terminaron · ${b.counts.failed} fallaron${b.counts.stopped ? ` · ${b.counts.stopped} detenidos` : ''}`);
        for (const a of b.agents) out.push(rowText(a));
        if (b.more) out.push(b.more.label);
        break;
      case 'watching':
        out.push(`Mirando: ${rowText(b.card)}`);
        break;
      case 'agent':
        out.push(`Agente: ${rowText(b.card)}`);
        if (b.batch != null) out.push(`  lote ${b.batch} del plan`);
        for (const l of b.prompt) out.push(`  > ${l}`);
        for (const t of b.tools) out.push(`  ${t.label} · hace ${t.ago}s`);
        for (const l of b.result) out.push(`  ${l}`);
        for (const l of b.error) out.push(`  ✘ ${l}`);
        for (const s of b.sent) out.push(`  ✉ ${s.text} (${s.state})`);
        out.push(`[${b.back.hotkey}] ${b.back.label}`);
        break;
      case 'rows':
        out.push(b.title);
        for (const r of b.rows) {
          const cells = r.cells.map((x) => (x.prev ? `‹ ${x.text} ›` : x.press ? `${x.on === true ? '● ' : x.on === false ? '○ ' : ''}${x.text}${x.hotkey ? ` [${x.hotkey}]` : ''}` : x.text)).join('  ');
          out.push(`  ${r.label.padEnd(b.labelWidth)}${cells}${r.hint ? `  (${r.hint.text})` : ''}`);
        }
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
      case 'memsearch':
        out.push(`Memoria${b.query ? `: «${b.query}»` : ''}`);
        if (b.message) out.push(`  ${b.message}`);
        for (const r of b.rows) out.push(`  ${r.text}${r.sub ? ` (${r.sub})` : ''} [${r.id}]`);
        if (b.recent) out.push(`  [${b.recent.label}]`);
        break;
      case 'memdetail':
        out.push(`Memoria ${b.id}`);
        if (b.message) out.push(`  ${b.message}`);
        for (const l of b.lines) out.push(`  ${l}`);
        out.push(`[${b.back.hotkey}] ${b.back.label}`);
        break;
      case 'handoff':
        out.push(b.text);
        for (const l of b.lines) out.push(`  ${l}`);
        break;
      case 'locate':
        out.push(`Ubicar${b.question ? `: «${b.question}»` : ''} · ${b.note}${b.ms != null ? ` · ${b.ms} ms` : ''}`);
        if (b.message) out.push(`  ${b.message}`);
        for (const r of b.rows) out.push(`  ${r.text}`);
        if (b.degraded.length) out.push(`  (degradado: ${b.degraded.join(', ')})`);
        if (b.ask) out.push(`  [${b.ask.label}]`);
        break;
      case 'launch':
        out.push(`${b.title}: ${b.kinds.map((k) => (k.active ? `[${k.label}]` : k.label)).join(' ')}`);
        out.push(`  ${b.hint}`);
        if (b.question) out.push(`  > ${b.question}`);
        if (b.card) out.push(`  ${rowText(b.card)}${b.running ? ' · en curso' : ''}`);
        for (const r of b.hits) out.push(`  ${r.text}`);
        for (const r of b.mems) out.push(`  ${r.text}${r.sub ? ` — ${r.sub}` : ''} [${r.id}]`);
        for (const l of b.raw) out.push(`  ${l}`);
        if (b.error) out.push(`  ✘ ${b.error}`);
        if (b.open) out.push(`  [${b.open.label}]`);
        break;
      case 'worktrees':
        out.push(b.title);
        if (b.message) out.push(`  ${b.message}`);
        for (const r of b.rows) {
          out.push(`  ${r.name} (${r.branch})${r.tags.length ? ` · ${r.tags.join(' · ')}` : ''}`);
          for (const l of r.lines) out.push(`    ${l}`);
          out.push(`    ${r.buttons.map((x) => `[${x.label}]${x.disabled ? ` (${x.reason})` : ''}`).join(' ')}`);
        }
        if (b.modes.length) out.push(`  ${b.modes.map((x) => `[${x.label}]`).join(' ')}`);
        if (b.input) out.push(`  ${b.input.placeholder} [${b.input.submitLabel}]`);
        for (const l of b.info) out.push(`  ${l}`);
        if (b.select) {
          const vals = b.select.options.filter((/** @type {any} */ o) => o.value !== '');
          out.push(b.select.pick === 'base'
            ? `  ${b.select.label}: ${b.select.options.find((/** @type {any} */ o) => o.value === b.select.value)?.label ?? b.select.value} · elegí una de: ${vals.map((/** @type {any} */ o) => o.value).join(', ')}`
            : `  ${b.select.label}: elegí una de: ${vals.map((/** @type {any} */ o) => o.value).join(', ')}`);
        }
        for (const l of b.busy) out.push(`  ${l}`);
        if (b.error) out.push(`  ✘ ${b.error}`);
        out.push(`  ${b.note}`);
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
