// @ts-check
/**
 * The nxy panel's view model (1.1.0): pure data in, pure data out. The Mod's pane (register.tsx)
 * only draws what `buildPanel` returns; the headless fallback prints `panelText` of the same view.
 *
 * Adding a tab = one entry in TABS plus one section builder in BUILDERS. Adding a button = one entry
 * in LAUNCHER (with its own `tab`).
 *
 * Pure: no `node:*`, no `process`. Its imports are orchestrator.mjs and batch-status.mjs only.
 */
import {
  effective, suiteDone, suiteIsRed, stateOf, nextActions,
  PAUSE_CONTINUE, PAUSE_ADJUST, PAUSE_STOP,
} from './orchestrator.mjs';
import { RETRY, CONTINUE, STOP, isRed, isDone } from './batch-status.mjs';

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
  { id: 'home', label: 'Home', hotkey: '1' },
  { id: 'plan', label: 'Plan', hotkey: '2' },
];

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
 * True when the entry's `needs` (if any) holds for this state: the Home buttons and the driver's
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
    id: 'plan-done', tab: 'home', label: 'Terminar plan', hotkey: 'e', script: 'mem', args: ['handoff', 'done'],
    needs: 'plan', after: 'refresh', confirm: 'Terminar el plan y archivar el handoff?',
  },
  {
    // Unknown state (no snapshot yet): a plain 'Filter', never a guessed 'off'.
    id: 'filter', tab: 'home', label: (ctx) => (typeof ctx?.filter !== 'boolean' ? 'Filter' : ctx.filter ? 'Filter ● on' : 'Filter ○ off'),
    hotkey: 'f', script: 'filter', args: (ctx) => (ctx?.filter === true ? ['off'] : ['on']), after: 'refresh',
  },
  { id: 'stats', tab: 'home', label: 'Stats', hotkey: 's', script: 'stats', args: [] },
  { id: 'trend', tab: 'home', label: 'Trend', hotkey: 't', script: 'trend', args: [] },
];

const PANEL_IDS = ['refresh', 'dismiss', 'confirm-yes', 'confirm-no'];

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
export const isPanelAction = (id) => PANEL_IDS.includes(id) || id.startsWith('tab:') || LAUNCHER.some((l) => l.id === id);

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

// ---- section builders ----

/** @param {any} usage @param {any} gate */
function contextRow(usage, gate) {
  if (!usage) return { label: 'Contexto', value: 'desconocido', tone: /** @type {Tone} */ ('dim') };
  if (gate?.enabled && gate.contextTokens > 0) {
    const left = Math.max(0, gate.contextTokens - usage.tokens);
    const tone = barTone(usage.tokens, gate.contextTokens);
    return {
      label: 'Contexto', value: `${kFmt(usage.tokens)} / ${kFmt(gate.contextTokens)}`, tone,
      bar: { used: usage.tokens, limit: gate.contextTokens, tone }, note: `gate on · ${kFmt(left)} left`,
    };
  }
  if (usage.window > 0) {
    const tone = barTone(usage.tokens, usage.window);
    return {
      label: 'Contexto', value: `${kFmt(usage.tokens)} / ${kFmt(usage.window)}`, tone,
      bar: { used: usage.tokens, limit: usage.window, tone }, note: 'gate off',
    };
  }
  return { label: 'Contexto', value: kFmt(usage.tokens), tone: /** @type {Tone} */ ('info'), note: 'gate off' };
}

/** @param {any} c */
function homeSection({ snap, owned, usage, now, gate }) {
  /** @type {any[]} */
  const rows = [];
  if (!snap) {
    rows.push({ label: 'Plan', value: 'sin leer', tone: 'dim' });
    rows.push({ label: 'Handoff', value: 'sin leer', tone: 'dim' });
  } else {
    if (!snap.hash) rows.push({ label: 'Plan', value: 'ninguno', tone: 'dim' });
    else {
      const eff = effective(snap);
      const dn = snap.batches.filter(({ n }) => isDone(eff[String(n)]?.status)).length;
      const state = owned ? `lotes ${dn}/${snap.batches.length}` : snap.approved ? 'aprobado' : 'sin aprobar';
      rows.push({ label: 'Plan', value: `${snap.hash} · ${state}`, tone: 'info' });
      rows.push({ label: 'Ciclo', value: '', tone: 'info', steps: lifecycle(snap) });
    }
    const upd = snap.handoff?.updated;
    if (typeof upd !== 'number') rows.push({ label: 'Handoff', value: 'ninguno', tone: 'dim' });
    else {
      const newest = Math.max(0, ...Object.values(snap.verdicts ?? {}).map((v) => /** @type {any} */ (v)?.ts ?? 0));
      const stale = newest > upd;
      rows.push({
        label: 'Handoff', value: `${agoEs(now - upd)}${stale ? ' · desactualizado' : ''}`, tone: stale ? 'error' : 'info',
      });
    }
  }
  rows.push(contextRow(usage, gate));
  if (!usage || (usage.costUsd == null && !usage.model)) rows.push({ label: 'Sesión', value: 'desconocido', tone: 'dim' });
  else {
    const bits = [usage.costUsd != null ? `$${Number(usage.costUsd).toFixed(2)}` : null, usage.model || null].filter(Boolean);
    rows.push({ label: 'Sesión', value: bits.join(' · '), tone: 'info' });
  }
  const ctx = filterCtx(snap);
  const actions = LAUNCHER.filter((l) => l.tab === 'home' && launcherAvailable(l, { snap, owned }))
    .map((l) => {
      const e = /** @type {NonNullable<ReturnType<typeof launcherOf>>} */ (launcherOf(l.id, ctx));
      return { id: e.id, label: e.label, hotkey: e.hotkey };
    });
  return { rows, actions };
}

/** @param {any} c */
function planSection({ snap }) {
  if (!snap?.hash || !snap.batches?.length) return { rows: [{ label: 'Plan', value: 'sin plan', tone: 'dim' }] };
  const eff = effective(snap);
  const continued = snap.continued ?? [];
  const batches = snap.batches.map((/** @type {any} */ b) => {
    const s = eff[String(b.n)]?.status;
    /** @type {'done' | 'running' | 'red' | 'pending'} */
    const state = !s ? 'pending' : s === 'running' ? 'running' : isRed(s) ? 'red' : 'done';
    const note = state === 'red' ? `${s === 'not-run' ? 'accept not run' : 'failed'}${continued.includes(b.n) ? ' (continued anyway)' : ''}` : undefined;
    return { n: b.n, title: b.title || `Batch ${b.n}`, state, ...(note ? { note } : {}) };
  });
  const suite = snap.suite && suiteDone(snap.suite)
    ? (suiteIsRed(snap.suite) ? { value: '✘ red', tone: 'error' } : { value: '✔ green', tone: 'ok' })
    : snap.suite || snap.suiteLaunched ? { value: 'running…', tone: 'running' } : { value: 'pending', tone: 'dim' };
  const review = snap.review ? { value: '✔ done', tone: 'ok' }
    : snap.reviewLaunched ? { value: 'running…', tone: 'running' } : { value: 'pending', tone: 'dim' };
  return {
    rows: [{ label: 'Full suite', ...suite }, { label: 'Review', ...review }],
    batches,
  };
}

/** One entry per tab id; each returns the tab's section. */
export const BUILDERS = { home: homeSection, plan: planSection };

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

/**
 * @param {{
 *   snap: any, ask?: any, owned?: boolean, ui?: {tab?: string, confirm?: string | null},
 *   output?: {label: string, text: string, running?: boolean} | null,
 *   usage?: {tokens: number, percent?: number, window?: number | null, costUsd?: number | null, model?: string | null} | null,
 *   now?: number, fallback?: {gate?: {enabled?: boolean, contextTokens?: number}},
 * }} o `owned`: the orchestrator runs the plan in `snap` (the pill and the lotes count follow it).
 */
export function buildPanel({ snap, ask = null, owned = false, ui = {}, output = null, usage = null, now = Date.now(), fallback = {} }) {
  const tab = TABS.some((t) => t.id === ui.tab) ? /** @type {string} */ (ui.tab) : 'home';
  const gate = snap?.config?.gate ?? fallback.gate;
  const ownedSnap = owned && snap?.hash ? snap : null;
  const build = /** @type {Record<string, (c: any) => any>} */ (BUILDERS)[tab];
  return {
    tabs: TABS.map((t) => ({ ...t, active: t.id === tab })),
    header: { title: 'nxy', pill: stateOf(ownedSnap, ask) },
    ask: askOf(ownedSnap, ask, ui),
    output: output
      ? {
        title: output.label, lines: output.running ? [output.text] : clipOutput(output.text, 30),
        ...(output.running ? { running: true } : {}), dismiss: { id: 'dismiss', hotkey: 'd' },
      }
      : null,
    keys: [{ id: 'refresh', hotkey: 'r' }],
    sections: [build({ snap, owned, usage, now, gate })],
  };
}

/** Plain-text dump of a view: the headless run and the unplaced pane. @param {ReturnType<typeof buildPanel>} view */
export function panelText(view) {
  const out = [];
  out.push(view.tabs.map((t) => (t.active ? `[${t.label}]` : t.label)).join(' '));
  out.push(`${view.header.title} ${view.header.pill.glyph} ${view.header.pill.text}`);
  for (const sec of view.sections) {
    if (sec.title) out.push(sec.title);
    for (const r of sec.rows ?? []) {
      out.push(`${r.label}: ${r.value ?? ''}`.trimEnd());
      if (r.bar) out.push(`  ${barText(r.bar.used, r.bar.limit, 10)}`);
      if (r.note) out.push(`  ${r.note}`);
      if (r.steps) out.push(`  ${r.steps.map(stepText).join(' › ')}`);
    }
    for (const b of sec.batches ?? []) {
      const g = b.state === 'done' ? '✔' : b.state === 'running' ? '●' : b.state === 'red' ? '✘' : '○';
      out.push(`${g} ${b.title}${b.note ? ` (${b.note})` : ''}`);
    }
    if (sec.actions?.length) out.push(sec.actions.map((a) => `[${a.hotkey}] ${a.label}`).join(' '));
  }
  if (view.ask) out.push(view.ask.question, view.ask.buttons.map((b) => `[${b.hotkey}] ${b.label}`).join(' '));
  if (view.output) {
    out.push(`— ${view.output.title}`);
    out.push(...view.output.lines);
  }
  return out;
}
