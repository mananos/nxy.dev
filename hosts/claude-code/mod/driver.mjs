// @ts-check
/**
 * The orchestrator's driver (1.1.0): the logic the Mod's module runs, testable without Claude Code.
 * It reads the plan through `entries/orch.mjs snapshot` (the module cannot open SQLite), asks the pure
 * state machine (core/orchestrator.mjs) what comes next and executes it with `$`: marker and tickets
 * on disk, agents through `$.agent.spawn`, the hand-back through `$.session.append` + `$.prompt.submit`.
 *
 * Runs inside the module: no `node:*`, no dynamic import; its only import is the pure core file.
 * `$` is the engine interface, typed `any` here.
 */
import {
  ORCH_DIR, MARKER_FILE, TICKET_DIR, PAUSE_CONTINUE, PAUSE_ADJUST, PAUSE_STOP,
  RETRY, CONTINUE, STOP, finishedBatches,
  nextActions, batchPrompt, testerPrompt, reviewerPrompt, ticketDescription, handbackText,
  wantsSnapshot, statusText,
} from '../../../core/orchestrator.mjs';
import { TABS, buildPanel, launcherOf, launcherAvailable, configCtx, isPanelAction } from '../../../core/panel.mjs';
import { nextValue } from '../../../core/configedit.mjs';
import { parseIssue, featureNames, openCommand } from '../../../core/featuresview.mjs';
import { cacheOf, coldCost, expiryPlan, hitOf, pickTtl, ttlMsOf } from '../../../core/cache.mjs';
import { money, tok } from '../../../core/statsview.mjs';
import {
  createBook, bookSpawn, bookTool, bookTurn, bookList, bookTrail, bookSent, bookRoles,
  agentRows, agentDetail, bookLive, needsTrail, needsConfirm, statusView,
} from '../../../core/agentview.mjs';

/** The `session.messages` fallback asks about one agent at most this often. */
const TRAIL_EVERY_MS = 2000;

const CONTINUE_ANYWAY = CONTINUE;
const MAX_TRENDS = 12;
/** A warn timer may fire a little late; past this the cache is no longer "about to expire". */
const WARN_WINDOW_MS = 65_000;
const isTtl = (t) => t === '5m' || t === '1h';
const defaultSleep = (ms) => new Promise((r) => setTimeout(r, ms));

function newNonce() {
  const c = globalThis.crypto;
  if (c && typeof c.randomUUID === 'function') return c.randomUUID().replace(/-/g, '');
  return `${Date.now().toString(36)}${Math.random().toString(36).slice(2)}${Math.random().toString(36).slice(2)}`;
}

const freshState = (hash) => ({
  hash, ours: true, done: false, phase: 'batches',
  launched: {}, continued: [], acked: [], retry: [], forced: {},
  inFlight: {}, suiteLaunched: false, reviewLaunched: false,
  forcedSuite: null, forcedReview: null, testerAnswer: '', reviewAnswer: '',
  ask: null, stopped: false, adjust: false, adjustBatch: null,
});

/**
 * @param {any} $ the engine interface
 * @param {{cwd: string, sessionId?: string, headless?: boolean, waitTries?: number, waitMs?: number, sleep?: (ms: number) => Promise<void>}} opts
 */
export function createDriver($, opts) {
  const { cwd, sessionId } = opts;
  const headless = opts.headless === true;
  const waitTries = opts.waitTries ?? 10;
  const waitMs = opts.waitMs ?? 300;
  const sleep = opts.sleep ?? defaultSleep;

  /** @type {any} */ let snap = null;
  /** @type {any} */ let st = null;
  /** @type {string|null} */ let runtimeDir = null;
  /** @type {Promise<any>} */ let chain = Promise.resolve();
  /** @type {string|null} plan hashes this driver already handed back: never taken over again */
  let finished = null;

  /** @type {any} */ let cfg = {};
  /** @type {{tokens: number, percent: number, window?: number|null, costUsd?: number|null, model?: string|null}|null} */ let usage = null;
  /**
   * The launcher's output box. `id` and `token` name the run that wrote it: a run that ends only
   * writes over its own placeholder (not over a dismiss, nor over a later launcher's box).
   * @type {{id: string, token: number, label: string, text: string, running?: boolean}|null}
   */
  let output = null;
  let runToken = 0;
  const ui = {
    tab: 'home', /** @type {'user'|'repo'} where the Config tab saves */ scope: 'user', /** @type {string|null} */ confirm: null,
    /** @type {number|undefined} the open batch of the Plan tab */ expanded: undefined,
    /** @type {Set<string>} findings ticked in checkpoint 2 */ picked: new Set(),
    /** @type {{metric: string, range: string, by: string, here: boolean}} the Stats tab's trend selection */
    trendSel: { metric: 'usd', range: '7d', by: 'day', here: false },
    /** @type {string|null} the agent the Agents tab shows in detail */ agent: null,
    agentsAll: false,
    /** @type {{agentId: string, text: string, batch: number|null}|null} a message waiting for the user's yes */
    pendingSend: null,
    /** @type {any} the Memoria tab: search, one memory, the branch handoff, the model-free locate */
    mem: {
      query: '', results: [], state: 'empty', error: '', open: null, key: 0,
      detail: { state: 'loading', data: null, error: '' },
      handoff: { state: 'loading', data: null, error: '' },
      locate: { question: '', state: 'empty', data: null, error: '', key: 0 },
    },
    /** @type {any} the Features tab: the worktree list (features.mjs list) and the "new" steps */
    features: {
      state: 'idle', data: null, error: '', at: 0, mode: '', step: '', issue: '', name: '', base: '', branch: '', key: 0, target: '', force: false,
    },
    /** @type {any} the Agentes launcher (scout/librarian) */
    launch: { kind: 'scout', agentId: null, question: '', key: 0, result: '', error: '' },
  };
  /** @type {Set<string>} input fields with a run in flight */
  const inputBusy = new Set();
  let memStarted = false;
  let featStarted = false;
  const book = createBook();
  /** @type {string|null} the agent the main thread's task list is looking at */
  let viewAgentId = null;
  /** @type {Map<string, number>} when `session.messages` last answered for an agent */
  const trailAt = new Map();
  let syncing = false;
  /** @type {string|null} the review the ticks belong to */
  let pickedFor = null;
  /** Ticks belong to one review: another review clears them. */
  function syncPicked() {
    const id = snap?.ok && snap.reviewDetail ? String(snap.reviewDetail.id ?? '') : null;
    if (id !== pickedFor) { pickedFor = id; ui.picked.clear(); }
  }
  /**
   * The main thread's cache as the module saw it: when its last answer came, how much of the input
   * it served, how big the context was, and the TTL with where it came from.
   * @type {{lastTs: number|null, hitPct: number|null, tokens: number|null, ttl: '5m'|'1h', ttlSource: 'default'|'settings'|'inferred'|'observed',
   *   turnUsd: number|null, lastCost: number|null, effort: string|null, cold: {usd: number, tokens: number|null}|null}}
   */
  const cache = { lastTs: null, hitPct: null, tokens: null, ttl: '5m', ttlSource: 'default', turnUsd: null, lastCost: null, effort: null, cold: null };
  /** @type {{observed: any, settings: any, inferred: boolean}} what `pickTtl` is fed */
  const ttlIn = { observed: null, settings: null, inferred: false };
  /** @type {number|null} when the current turn began */
  let turnStart = null;
  /** @type {string[]} toasts waiting for the module ("resumen listo") */
  let notices = [];
  /** @type {any} the closed feature's summary input, or null */
  let summary = null;
  /** @type {{[n: string]: number}|null} the last owned plan's launch times (st is nulled when it closes) */
  let launchedMemo = null;
  let columns = 78;
  /** @type {{state: string, data?: any, at?: number, error?: string}|null} */
  let stats = null;
  /** @type {Map<string, {state: string, data?: any, at?: number, error?: string}>} */
  const trends = new Map();
  /** @type {string|null} the plan hash the Plan tab was already shown for */
  let lastOwnedHash = null;
  /** @type {Promise<any>} the launcher's own lane, independent of `chain` */
  let launchChain = Promise.resolve();
  const launching = new Set();
  /** @type {{ttl1h: boolean, statusline: string}|null} what ~/.claude/settings.json says (null while unknown) */
  let settingsView = null;
  /** @type {{version?: string|null, platform?: string|null, tools: any, failed?: boolean}|null} the Config tab's tools and installed version */
  let cfgInfo = null;
  /** @type {{text: string, tone: string}|null} why the last config write failed */
  let cfgNote = null;
  let toolsStarted = false;
  /** @type {Set<string>} fields whose write has not come back yet */
  const cfgBusy = new Set();
  let asked = false;
  const isOwned = () => !!st && st.ours === true && !st.done;

  const dir = () => `${runtimeDir}/${ORCH_DIR}`;
  const entry = () => `${$.plugin.root}/hosts/claude-code/entries/orch.mjs`;

  async function runNode(args) {
    const res = await $.process.run(['node', '--disable-warning=ExperimentalWarning', entry(), ...args, '--cwd', cwd]);
    return res;
  }

  async function refresh() {
    const args = ['snapshot'];
    if (sessionId) args.push('--session', sessionId);
    const res = await runNode(args);
    const out = JSON.parse(String(res?.stdout ?? '').trim().split('\n').pop() || '{}');
    const prev = snap;
    snap = out;
    if (out && out.ok && typeof out.runtimeDir === 'string') runtimeDir = out.runtimeDir;
    if (out?.ok) setTtl(out.cacheTtl, 'observed');
    if (out?.ok && out.roles) bookRoles(book, out.roles);
    // An approved plan that no longer has a hash was closed (Terminar plan, or `handoff done` in the chat).
    if (prev?.ok && prev.hash && prev.approved && out?.ok && !out.hash) await runSummary(prev);
    return out;
  }

  /** Runs `orch.mjs summary` once for a plan that just closed and keeps its result for the Stats tab. */
  async function runSummary(prev) {
    if (summaryFor === prev.hash) return;
    summaryFor = prev.hash;
    try {
      const launched = { ...(prev.launched ?? {}), ...(launchedMemo ?? {}), ...(st?.launched ?? {}) };
      const times = [...Object.values(launched), ...Object.values(prev.verdicts ?? {}).map((v) => /** @type {any} */ (v)?.launched)]
        .filter((t) => Number.isFinite(t));
      const args = ['summary', '--hash', prev.hash];
      if (sessionId) args.push('--session', sessionId);
      if (times.length) args.push('--fallback-since', String(Math.min(...times)));
      const res = await runNode(args);
      const out = JSON.parse(String(res?.stdout ?? '').trim().split('\n').pop() || '{}');
      if (!out?.ok) return;
      setTtl(out.cacheTtl, 'observed');
      summary = { feature: out.feature, session: out.session, approvedAt: out.approvedAt, sinceKind: out.sinceKind, snap: { ...prev, launched } };
      notices.push('Resumen del feature listo: abrí la pestaña Stats');
    } catch { /* the summary is a nicety */ }
  }
  /** @type {string|null} the plan hash a summary was already asked for */
  let summaryFor = null;

  /**
   * Sets the TTL. An observed one (read from the transcript, or from a model-switch event) is never
   * overwritten by `settings`; anything else only fills in while nothing was observed.
   * @param {any} ttl @param {'observed'|'settings'} source
   */
  function setTtl(ttl, source) {
    if (!isTtl(ttl)) return;
    if (source === 'observed') ttlIn.observed = ttl; else ttlIn.settings = ttl;
    applyTtl(null);
  }

  /** The one precedence rule lives in `pickTtl`; the driver only feeds it what it has seen. */
  function applyTtl(infer) {
    const r = pickTtl({ observed: ttlIn.observed, settings: ttlIn.settings, infer });
    if (r.source === 'inferred') ttlIn.inferred = true;
    else if (ttlIn.inferred && r.source !== 'observed' && r.ttl === '5m') { cache.ttl = '1h'; cache.ttlSource = 'inferred'; return; }
    cache.ttl = r.ttl;
    cache.ttlSource = r.source;
  }

  /** The user's prompt reached the model: the start of a turn, where the idle pause before it ends. */
  function noteTurnStart(now = Date.now()) { turnStart = now; }

  /**
   * One answer of the MAIN thread (an `agentId` means a subagent: ignored). Sets when it came, the
   * hit, the context size and the turn's cost (delta of the session cost); without an observed TTL,
   * a cache that outlived a pause longer than 5 min lifts the TTL to 1 h.
   * @param {{usage?: any, agentId?: string|null, now?: number, costUsd?: number|null}} a
   */
  function noteTurn({ usage: u, agentId, now = Date.now(), costUsd = null } = {}) {
    if (agentId || !u) return;
    const n = (v) => (typeof v === 'number' && Number.isFinite(v) ? v : 0);
    const read = n(u.cache_read_input_tokens);
    const tokens = n(u.input_tokens) + read + n(u.cache_creation_input_tokens);
    // The pause is the idle time before the turn began; with no known start, a long turn could
    // pass as a pause, so nothing is inferred.
    if (cache.lastTs != null && turnStart != null && turnStart >= cache.lastTs) {
      applyTtl({ gapMs: turnStart - cache.lastTs, readTokens: read, prevTokens: cache.tokens ?? 0 });
    }
    turnStart = null;
    cache.lastTs = now;
    cache.hitPct = hitOf(u);
    cache.tokens = tokens;
    cache.cold = null;
    if (typeof costUsd === 'number' && Number.isFinite(costUsd)) {
      cache.turnUsd = cache.lastCost != null ? Math.max(0, costUsd - cache.lastCost) : null;
      cache.lastCost = costUsd;
    }
  }

  /** @param {string|null|undefined} level */
  const noteEffort = (level) => { if (typeof level === 'string' && level) cache.effort = level; };

  /**
   * A model switch: the new model starts with a cold cache, and the event carries the exact cost of
   * rewriting it.
   * @param {{toModel?: string|null, tokens?: number|null, usd?: number|null, ttl?: string|null}} a
   */
  function noteModelSwitch({ toModel, tokens, usd, ttl } = {}) {
    setTtl(ttl, 'observed');
    cache.lastTs = 0;
    cache.hitPct = null;
    if (Number.isFinite(tokens)) cache.tokens = /** @type {number} */ (tokens);
    cache.cold = typeof usd === 'number' && Number.isFinite(usd)
      ? { usd, tokens: Number.isFinite(tokens) ? /** @type {number} */ (tokens) : null } : null;
    if (toModel && usage) usage = { ...usage, model: toModel };
  }

  /** The cost of rewriting the context cold, from the snapshot's price table. */
  function coldNow() {
    if (cache.cold) return { usd: cache.cold.usd, estimated: false };
    const model = usage?.model;
    if (!model || !snap?.prices || cache.tokens == null) return null;
    return coldCost({ tokens: cache.tokens, model, ttl: cache.ttl, prices: snap.prices });
  }

  /**
   * When to warn and when the cache expires, from the last main-thread answer. Null with no answer yet.
   * @param {number} [now]
   */
  function cachePlan(now = Date.now()) {
    const ttlMs = ttlMsOf(cache.ttl);
    if (cache.lastTs == null || ttlMs == null) return null;
    return expiryPlan({ lastTs: cache.lastTs, ttlMs, now, coldUsd: coldNow()?.usd ?? null });
  }

  /**
   * The toast text for a cache timer that fired, or null when it no longer applies (a newer turn
   * renewed the cache, or the cost rounds to nothing).
   * @param {'warn'|'expire'} kind @param {number} [now]
   */
  function cacheNotice(kind, now = Date.now()) {
    const plan = cachePlan(now);
    if (!plan || plan.silent) return null;
    const cost = coldNow();
    const rewrite = cost && cache.tokens != null
      ? `el próximo mensaje reescribe ${tok(cache.tokens)} ≈ ${money(cost.usd)}${cost.estimated ? '*' : ''}` : 'el próximo mensaje reescribe el contexto';
    if (kind === 'warn') {
      if (plan.expireInMs <= 0 || plan.expireInMs > WARN_WINDOW_MS) return null;
      return `La cache vence en 1 min: ${rewrite}`;
    }
    if (plan.expireInMs > 0) return null;
    return `La cache venció: ${rewrite}`;
  }

  /** The pending toasts ("resumen listo"), handed over once. Cache toasts come from `cacheNotice`. */
  function drainNotices() {
    const out = notices;
    notices = [];
    return out;
  }

  /** Work on the launcher's own lane. @template T @param {() => Promise<T>} fn */
  function lane(fn) {
    const run = launchChain.then(fn);
    launchChain = run.catch(() => {});
    return run;
  }

  /** Runs a stats/trend entry and parses its whole stdout. */
  async function runJson(script, args) {
    try {
      const res = await $.process.run(['node', '--disable-warning=ExperimentalWarning', `${$.plugin.root}/hosts/claude-code/entries/${script}.mjs`, ...args, '--json', '--cwd', cwd]);
      const text = String(res?.stdout ?? '').trim();
      let data;
      try { data = JSON.parse(text); } catch { return { state: 'empty', at: Date.now(), error: text.split('\n')[0] || undefined }; }
      return { state: 'ok', data, at: Date.now() };
    } catch (err) {
      return { state: 'error', at: Date.now(), error: String(/** @type {any} */ (err)?.message ?? err) };
    }
  }

  function loadStats() {
    stats = { state: 'loading', data: stats?.data, at: stats?.at };
    return lane(async () => {
      const r = await runJson('stats', sessionId ? ['--session', sessionId] : []);
      if (r.state === 'ok' && !(r.data?.usage?.calls > 0)) r.state = 'empty';
      stats = r;
      if (r.state === 'ok') setTtl(r.data?.cacheTtl, 'observed');
    });
  }

  /** Runs a config.mjs action and parses the last line of its stdout (one JSON line). */
  async function runConfig(args) {
    const res = await $.process.run(['node', '--disable-warning=ExperimentalWarning', `${$.plugin.root}/hosts/claude-code/entries/config.mjs`, ...args, '--cwd', cwd]);
    return JSON.parse(String(res?.stdout ?? '').trim().split('\n').pop() || '{}');
  }

  /** The installed version and the tools found, once per visit (`r` in Config reloads). */
  function loadTools() {
    toolsStarted = true;
    return lane(async () => {
      try {
        const out = await runConfig(['tools']);
        // The entry emits rtk/rg/codegraph at the top level next to version.
        cfgInfo = out && out.ok !== false
          ? { version: out.version ?? null, platform: out.platform ?? null, tools: out.tools ?? { rtk: out.rtk, rg: out.rg, codegraph: out.codegraph } }
          : { version: null, tools: {}, failed: true };
      } catch { cfgInfo = { version: null, tools: {}, failed: true }; }
    });
  }

  /** The value a Config field has now, from the snapshot (what ‹ › cycles from). */
  function configValue(fieldId) {
    const c = snap?.config;
    const m = /^roles\.([^.]+)\.(model|effort)$/.exec(fieldId);
    if (m) return c?.roles?.[m[1]]?.[m[2]] ?? null;
    switch (fieldId) {
      case 'flow.orchestrator': return c?.orchestrator;
      case 'flow.pauseAfterBatch': return c?.pauseAfterBatch;
      case 'gate.enabled': return c?.gate?.enabled;
      case 'gate.contextTokens': return c?.gate?.contextTokens;
      case 'modules.filter': return c?.filter;
      case 'ui.panel': return c?.ui?.panel;
      default: return undefined;
    }
  }

  /** `cfg:<field>:<dir>`: one `config.mjs set` on the launcher lane; a second press on the field waits for the first. */
  function pressCfg(action) {
    const m = /^cfg:([^:]+):(next|prev|toggle)$/.exec(action);
    if (!m || !snap?.ok || !snap.config) return Promise.resolve();
    const field = m[1];
    if (field === 'flow.orchestrator' && isOwned()) return Promise.resolve();
    if (cfgBusy.has(field)) return Promise.resolve();
    const cur = configValue(field);
    // sin valor guardado, lo que se ve (p. ej. el effort del frontmatter) es el «predeterminado»: no se cicla a un null que no cambia nada
    const fallback = snap.config.sources?.[field] === 'default' ? cur : null;
    const value = nextValue(field, cur, /** @type {'next'|'prev'|'toggle'} */ (m[2]), fallback);
    const scope = ui.scope;
    cfgBusy.add(field);
    return lane(async () => {
      try {
        cfgNote = null;
        const out = await runConfig(['set', '--scope', scope, '--key', field, '--json', JSON.stringify(value)]);
        if (out?.ok && out.config) { if (snap) snap = { ...snap, config: out.config }; }
        else cfgNote = { text: String(out?.reason ?? 'no se pudo guardar'), tone: 'error' };
      } catch (err) {
        cfgNote = { text: `no se pudo guardar: ${/** @type {any} */ (err)?.message ?? err}`, tone: 'error' };
      } finally { cfgBusy.delete(field); }
    });
  }

  /** `$.settings.read()` as the Config tab needs it (no node). Also feeds the TTL's `settings` source. */
  function setSettings(s) {
    if (!s || typeof s !== 'object') return;
    setTtl(s.promptCacheTtl, 'settings');
    const c = configCtx(null, s);
    settingsView = { ttl1h: c.ttl1h === true, statusline: c.statusline ?? 'none' };
  }

  const trendKey = (sel) => `${sel.range}|${sel.by}|${sel.here ? 'here' : 'all'}`;

  /** Loads the trend of the selection unless that combination is cached (or loading). @param {boolean} [force] */
  function loadTrend(sel = ui.trendSel, force = false) {
    const key = trendKey(sel);
    const have = trends.get(key);
    if (have && !force && have.state !== 'error') return Promise.resolve();
    if (!have) while (trends.size >= MAX_TRENDS) trends.delete(/** @type {string} */ (trends.keys().next().value));
    trends.set(key, { state: 'loading', data: have?.data, at: have?.at });
    const args = ['--since', sel.range, '--by', sel.by];
    if (sel.here) args.push('--here');
    return lane(async () => {
      const r = await runJson('trend', args);
      if (r.state === 'ok' && !(r.data?.periods?.length || r.data?.models?.length)) r.state = 'empty';
      trends.set(key, r);
    });
  }

  async function loadState(hash) {
    if (st && st.hash === hash) return st;
    st = null;
    try {
      const s = JSON.parse(await $.fs.read(`${dir()}/state.json`));
      if (s && s.hash === hash) st = { ...freshState(hash), ...s };
    } catch { /* none yet */ }
    return st;
  }

  async function saveState() {
    if (runtimeDir && st) await $.fs.write(`${dir()}/state.json`, JSON.stringify(st));
  }

  async function writeMarker(hash, ts = Date.now()) {
    if (runtimeDir) await $.fs.write(`${dir()}/${MARKER_FILE}`, JSON.stringify({ ts, hash }));
  }

  /** Releases the marker (no delete in `$.fs`: a marker with ts 0 is expired) and clears the rest where node can. */
  async function release(hash = null) {
    try { await writeMarker(hash ?? snap?.hash ?? st?.hash ?? '', 0); } catch { /* best-effort */ }
    try { await runNode(['release']); } catch { /* best-effort */ }
  }

  /** The snapshot the state machine reads: the plan's view plus the module's own state. */
  function build() {
    const verdicts = { ...(snap.verdicts ?? {}) };
    for (const [k, f] of Object.entries(st.forced)) {
      const v = verdicts[k];
      const launched = st.launched[k];
      if (!v || v.status === 'running' || (launched != null && (v.ts ?? 0) < launched)) verdicts[k] = f;
    }
    return {
      hash: snap.hash,
      batches: snap.batches ?? [],
      verdicts,
      launched: st.launched,
      continued: st.continued,
      acked: st.acked,
      retry: st.retry,
      stopped: st.stopped,
      adjust: st.adjust,
      pauseAfterBatch: snap.config?.pauseAfterBatch === true,
      suite: snap.suite ?? st.forcedSuite ?? null,
      suiteLaunched: st.suiteLaunched,
      review: snap.review ?? st.forcedReview ?? null,
      reviewLaunched: st.reviewLaunched,
      reviewNeeded: snap.reviewNeeded,
    };
  }

  async function spawn(role, label, prompt, view) {
    const nonce = newNonce();
    await $.fs.write(`${dir()}/${TICKET_DIR}/${nonce}`, String(Date.now()));
    const res = await $.agent.spawn({
      prompt,
      subagentType: `nxy:${role}`,
      description: ticketDescription(nonce, label),
    });
    // The engine skips the hook that launched an agent, so the driver notes its own spawns.
    if (res?.agentId) bookSpawn(book, { id: res.agentId, type: `nxy:${role}`, description: label, prompt, model: res.model, spawnedBy: 'nxy' });
    return res ?? {};
  }

  // ── agents tab ──────────────────────────────────────────────────────────

  /** @param {{id: string, type?: string, description?: string, prompt?: string, model?: string, parentId?: string, spawnedBy?: string, at?: number}} a */
  const noteSpawn = (a) => bookSpawn(book, a);
  /** @param {string} agentId @param {{tool: string, input?: any, at?: number}} a */
  const noteTool = (agentId, a) => bookTool(book, agentId, a);
  /** A subagent's turn ended (`turn.complete` with an `agentId`). @param {any} e */
  function noteAgentTurn(e) {
    if (!e?.agentId) return;
    bookTurn(book, { agentId: e.agentId, usage: e.usage, answer: e.answer ?? e.text, reason: e.reason, at: e.at }, { prices: snap?.prices, ttl: cache.ttl });
    syncLaunch();
  }

  /** The launched agent's answer, from the book (no node). */
  function syncLaunch() {
    const id = ui.launch.agentId;
    if (!id) return;
    const d = agentDetail(book, id, {});
    if (d && typeof d.result === 'string') ui.launch.result = d.result;
  }

  // ── memoria tab ─────────────────────────────────────────────────────────

  /** @param {any} r a `runJson` result @returns {{state: string, error: string, data: any}} */
  function memOutcome(r) {
    if (r.state === 'error') return { state: 'error', error: r.error ?? 'error', data: null };
    if (r.state === 'empty') return { state: r.error ? 'error' : 'empty', error: r.error ?? '', data: null };
    if (r.data && r.data.ok === false) return { state: 'empty', error: String(r.data.reason ?? ''), data: r.data };
    return { state: 'ok', error: '', data: r.data };
  }

  /**
   * One `mem.mjs` read on the launcher lane. kind: recent | search | get | handoff. Never throws.
   * @param {'recent'|'search'|'get'|'handoff'} kind @param {string} [arg]
   */
  function loadMem(kind, arg = '') {
    const m = ui.mem;
    if (kind === 'handoff') {
      m.handoff = { state: 'loading', data: m.handoff?.data ?? null, error: '' };
      return lane(async () => {
        const o = memOutcome(await runJson('mem', ['handoff', 'show']));
        m.handoff = { state: o.state === 'ok' ? 'ok' : o.state, data: o.data, error: o.error };
      });
    }
    if (kind === 'get') {
      m.open = arg;
      m.detail = { state: 'loading', data: null, error: '' };
      return lane(async () => {
        const o = memOutcome(await runJson('mem', ['get', arg]));
        if (m.open !== arg) return; // the user went back or opened another
        m.detail = { state: o.state, data: o.state === 'ok' ? { memory: o.data.memory, edges: o.data.edges ?? [] } : null, error: o.error };
      });
    }
    m.state = 'loading';
    m.error = '';
    m.open = null;
    const args = kind === 'recent' ? ['list', '--limit', '10'] : ['search', arg];
    return lane(async () => {
      const o = memOutcome(await runJson('mem', args));
      m.results = o.state === 'ok' && Array.isArray(o.data?.results) ? o.data.results : [];
      m.state = o.state === 'ok' && !m.results.length ? 'empty' : o.state;
      m.error = o.error;
    });
  }

  /** @param {string} question */
  function loadLocate(question) {
    const l = ui.mem.locate;
    l.question = question;
    l.state = 'loading';
    l.error = '';
    return lane(async () => {
      let r;
      try { r = await runJson('locate', [question]); } catch (err) { r = { state: 'error', error: String(/** @type {any} */ (err)?.message ?? err) }; }
      const o = memOutcome(r);
      if (l.question !== question) return;
      if (o.state === 'ok') {
        const d = o.data;
        l.data = { hits: Array.isArray(d.hits) ? d.hits : [], ms: d.ms, degraded: Array.isArray(d.degraded) ? d.degraded : [], codegraph: d.codegraph };
        l.state = l.data.hits.length ? 'ok' : 'empty';
      } else { l.data = null; l.state = o.state; l.error = o.error; }
    });
  }

  /** Launches the scout or the librarian from the panel; the answer lands via `noteAgentTurn`. @param {'scout'|'librarian'} kind @param {string} question */
  async function launchAgent(kind, question) {
    const l = ui.launch;
    const role = snap?.config?.roles?.[kind];
    const model = typeof role?.model === 'string' && role.model && role.model !== 'default' ? role.model : undefined;
    l.kind = kind;
    l.question = question;
    l.error = '';
    l.result = '';
    l.agentId = null;
    l.key = (l.key ?? 0) + 1;
    try {
      /** @type {any} */ const args = { prompt: question, subagentType: `nxy:${kind}`, description: `nxy-panel: ${question.slice(0, 40)}` };
      if (model) args.model = model;
      const res = await $.agent.spawn(args);
      if (!res || res.deny || !res.agentId) { l.error = `no se pudo lanzar el ${kind}: ${res?.deny ?? 'sin agente'}`; return; }
      l.agentId = res.agentId;
      // The engine skips the hook that launched an agent, so the driver notes its own spawns.
      // (an answer that already landed must not be reset to running by a second note)
      if (!book.agents.get(res.agentId)?.endedAt) bookSpawn(book, { id: res.agentId, type: `nxy:${kind}`, description: `nxy-panel: ${question.slice(0, 40)}`, prompt: question, model: res.model ?? model, spawnedBy: 'nxy' });
      syncLaunch(); // the answer may have landed before agentId was stored
    } catch (err) {
      l.error = `no se pudo lanzar el ${kind}: ${/** @type {any} */ (err)?.message ?? err}`;
    }
  }

  const launchLive = () => {
    const e = ui.launch.agentId ? book.agents.get(ui.launch.agentId) : null;
    return !!e && statusView(e.status, e.reason).live;
  };

  // ── features tab ────────────────────────────────────────────────────────

  /** One `features.mjs list` on the launcher lane. Never throws. */
  function loadFeatures() {
    const f = ui.features;
    f.state = 'loading';
    f.error = '';
    return lane(async () => {
      const r = await runJson('features', ['list']);
      f.at = r.at ?? Date.now();
      if (r.state === 'ok' && r.data && r.data.ok !== false) { f.data = r.data; f.state = 'ok'; f.error = ''; }
      else { f.state = 'error'; f.error = r.state === 'ok' ? String(r.data?.reason ?? 'error') : String(r.error ?? 'error'); }
    });
  }

  /** Back to the initial state of the "new" steps (nothing runs, git is not touched). */
  function resetFeatures() {
    const f = ui.features;
    f.mode = ''; f.step = ''; f.issue = ''; f.name = ''; f.base = ''; f.branch = ''; f.error = '';
    f.key += 1;
    if (ui.confirm === 'feat-new') ui.confirm = null;
  }

  const sameDir = (/** @type {string} */ a, /** @type {string} */ b) => String(a).replace(/\\/g, '/').toLowerCase() === String(b).replace(/\\/g, '/').toLowerCase();

  /** Opens the yes/no of `feat-new` once the choice is complete. */
  function askNew() {
    if (allowed('feat-new')) ui.confirm = /** @type {any} */ ('feat-new');
  }

  /** The base chosen in the Select (step base). Anything outside `data.bases` is ignored. @param {string} value */
  function pickBase(value) {
    const f = ui.features;
    const v = String(value ?? '');
    if (f.mode !== 'new' || f.step !== 'base' || !v || !f.data) return;
    if (!Array.isArray(f.data.bases) || !f.data.bases.includes(v)) return;
    const names = featureNames({ issue: f.issue, name: f.name, base: v }, f.data.main ?? '');
    if (!names.ok) { f.error = names.error; return; }
    f.error = '';
    f.base = v;
    f.branch = '';
    askNew();
  }

  /** The existing branch chosen in the Select. A busy or unknown branch is ignored. @param {string} value */
  function pickBranch(value) {
    const f = ui.features;
    const v = String(value ?? '');
    if (f.mode !== 'existing' || !v || !f.data) return;
    if (!Array.isArray(f.data.branches?.free) || !f.data.branches.free.includes(v)) return;
    const names = featureNames({ branch: v }, f.data.main ?? '');
    if (!names.ok) { f.error = names.error; return; }
    if ((f.data.worktrees ?? []).some((/** @type {any} */ w) => sameDir(w.path, names.path))) { f.error = `ya hay un worktree en ${names.path}`; return; }
    f.error = '';
    f.name = ''; f.base = ''; f.issue = '';
    f.branch = v;
    askNew();
  }

  /** The two text steps of "Rama nueva". @param {string} field @param {string} text */
  function submitFeature(field, text) {
    const f = ui.features;
    if (f.mode !== 'new' || !f.data) return;
    const t = String(text ?? '').trim();
    if (field === 'feature-issue' && f.step === 'issue') {
      const p = parseIssue(t);
      if (!p.ok) { f.error = p.error; return; }
      f.issue = p.issue; f.step = 'name'; f.error = ''; f.key += 1;
    } else if (field === 'feature-name' && f.step === 'name') {
      if (!t) return;
      const names = featureNames({ issue: f.issue, name: t, base: '-' }, f.data.main ?? '');
      if (!names.ok) { f.error = names.error; return; }
      if ((f.data.worktrees ?? []).some((/** @type {any} */ w) => sameDir(w.path, names.path))) { f.error = `ya hay un worktree en ${names.path}`; return; }
      f.name = t; f.step = 'base'; f.error = ''; f.key += 1;
    }
  }

  /** The Features buttons. Returns a promise that settles when the work is done. @param {string} action */
  function pressFeature(action) {
    const f = ui.features;
    if (action === 'feat:mode:cancel') { resetFeatures(); return Promise.resolve(); }
    if (action === 'feat:mode:new' || action === 'feat:mode:existing') {
      resetFeatures();
      f.mode = action === 'feat:mode:new' ? 'new' : 'existing';
      if (f.mode === 'new') f.step = 'issue';
      return f.data ? Promise.resolve() : loadFeatures();
    }
    const m = /^feat:(open|pause|close):(\d+)$/.exec(action);
    if (!m) return Promise.resolve();
    const row = f.data?.worktrees?.[Number(m[2])];
    if (!row || typeof row.path !== 'string') return Promise.resolve();
    if (m[1] === 'open') {
      if (row.current) return Promise.resolve();
      const shell = f.data?.shell === 'powershell' ? 'powershell' : 'posix';
      const other = shell === 'powershell' ? 'posix' : 'powershell';
      const text = openCommand(row.path, shell);
      const token = ++runToken;
      output = { id: 'feat-open', token, label: 'Abrir', text: '...', running: true };
      return Promise.resolve().then(async () => {
        let r;
        try { r = await $.ui.copy({ text }); } catch (err) { r = { isCopied: false, reason: String(/** @type {any} */ (err)?.message ?? err) }; }
        const head = r?.isCopied ? 'Copiado. Pegalo en una terminal nueva:' : `No se pudo copiar${r?.reason ? ` (${r.reason})` : ''}. Pegalo a mano en una terminal nueva:`;
        if (output && output.id === 'feat-open' && output.token === token) {
          output = { id: 'feat-open', token, label: 'Abrir', text: `${head}\n${text}\nEn ${other === 'powershell' ? 'PowerShell' : 'bash'}:\n${openCommand(row.path, other)}` };
        }
      });
    }
    if (m[1] === 'pause') { f.target = row.path; return runLauncher('feat-pause'); }
    // close: the main one and this session's are never closed; fresh numbers first, then the question.
    if (row.main || row.current || row.running) return Promise.resolve();
    const path = row.path;
    f.target = path; f.force = false;
    return loadFeatures().then(() => {
      const fresh = (f.data?.worktrees ?? []).find((/** @type {any} */ w) => w.path === path);
      if (!fresh || fresh.main || fresh.current || fresh.running) { f.target = ''; return; }
      f.force = fresh.dirty == null || fresh.dirty > 0; // unknown state counts as dirty
      if (allowed('feat-close')) ui.confirm = /** @type {any} */ ('feat-close');
    });
  }

  /** The launcher's output for the features script: the entry's JSON as text. @param {string} id @param {string} raw */
  function featuresText(id, raw) {
    let d;
    try { d = JSON.parse(String(raw).trim().split('\n').pop() || ''); } catch { return raw; }
    if (!d || typeof d !== 'object') return raw;
    if (d.ok === false) return `No se pudo: ${d.reason ?? 'error'}`;
    if (id === 'feat-new') return `Creado.\n${d.command ?? ''}\nPara abrirlo, usá Abrir en su fila.`;
    if (id === 'feat-close') return 'Worktree cerrado. La rama sigue en la lista de ramas libres.';
    if (id === 'feat-pause') {
      const parts = [d.paused ? `Pausado${d.branch ? ` (${d.branch})` : ''}.` : 'Reanudado.'];
      if (d.paused && d.handoff) parts.push(`Handoff existente:\n${d.handoff}${d.truncated ? '\n…' : ''}`);
      if (d.paused && d.advice) parts.push(d.advice);
      return parts.join('\n');
    }
    return raw;
  }

  /** Text from an input of the panel. Empty text is ignored (except the issue step); one run per field. @param {string} field @param {string} text */
  async function submitInput(field, text) {
    const key = String(field).split(':')[0];
    if (key === 'feature-issue' || key === 'feature-name') { submitFeature(key, text); return; }
    const t = String(text ?? '').trim();
    if (!t || inputBusy.has(field)) return;
    if (field === 'launch' && launchLive()) return;
    if (field !== 'mem-search' && field !== 'locate' && field !== 'launch') return;
    inputBusy.add(field);
    try {
      if (field === 'mem-search') { ui.mem.query = t; ui.mem.key += 1; await loadMem('search', t); }
      else if (field === 'locate') { ui.mem.locate.key += 1; await loadLocate(t); }
      else await launchAgent(ui.launch.kind === 'librarian' ? 'librarian' : 'scout', t);
    } finally { inputBusy.delete(field); }
  }
  const setViewAgent = (id) => { viewAgentId = typeof id === 'string' && id ? id : null; };

  /** Is any agent (the orchestrator's or another) working? Decides whether the panel animates and polls. */
  function agentsLive() {
    if (st?.inFlight && Object.keys(st.inFlight).length && isOwned()) return true;
    return bookLive(book);
  }

  /**
   * `$.agent.list()` into the book; then, for the live agents nobody saw a tool call from (the
   * orchestrator's, the selected one, the one being watched), the `session.messages` fallback.
   * Never throws.
   */
  async function syncAgents() {
    if (syncing) return;
    syncing = true;
    try {
      const now = Date.now();
      try { bookList(book, await $.agent?.list?.(), now); } catch { /* the list is a nicety */ }
      const ids = new Set([...Object.keys(st?.inFlight ?? {}), ui.agent, viewAgentId].filter(Boolean));
      for (const id of /** @type {Set<string>} */ (ids)) {
        if (!book.agents.get(id)?.confirmed || !needsTrail(book, id, now)) continue;
        if (now - (trailAt.get(id) ?? 0) < TRAIL_EVERY_MS) continue;
        trailAt.set(id, now);
        try { bookTrail(book, id, await $.session?.messages?.({ agentId: id }), now); } catch { /* {deny} or a failure: degraded, not broken */ }
      }
      // The throttle map only keeps agents still in the book and still working.
      for (const id of [...trailAt.keys()]) {
        const e = book.agents.get(id);
        if (!e || !statusView(e.status, e.reason).live) trailAt.delete(id);
      }
    } finally { syncing = false; }
  }

  /** Sends a message to an agent; an ended one resumes. @param {string} id @param {string} text */
  async function sendTo(id, text) {
    const at = Date.now();
    try {
      const r = await $.session.send({ to: { agentId: id }, text });
      const delivered = !!r && r.isDelivered !== false && !r.deny;
      bookSent(book, id, { text, at, delivered, reason: delivered ? undefined : String(r?.reason ?? r?.deny ?? 'sin respuesta') });
    } catch (err) {
      bookSent(book, id, { text, at, delivered: false, reason: String(/** @type {any} */ (err)?.message ?? err) });
    }
  }

  /** The composer's text for the selected agent. An orchestrator's implementer waits for a yes. @param {string} text */
  async function submitMessage(text) {
    const id = ui.agent;
    const t = String(text ?? '').trim();
    if (!id || !t) return;
    const flight = st?.inFlight?.[id];
    if (needsConfirm(book.agents.get(id), { owned: isOwned(), inFlight: st?.inFlight })) {
      ui.pendingSend = { agentId: id, text: t, batch: flight?.batch ?? null };
      ui.confirm = null; // one question open at a time
      return;
    }
    await sendTo(id, t);
  }

  async function handback(reason, ctx) {
    const hash = snap.hash;
    st.ours = false;
    st.done = true;
    st.ask = null;
    finished = hash;
    await saveState();
    await release(hash);
    const text = handbackText(reason, { hash, project: snap.projectDir, ...ctx });
    try {
      await $.session.append({ message: { type: 'system', content: [{ type: 'text', text: `nxy orchestrator: plan ${hash} handed back to the main thread (${reason}).` }] } });
    } catch { /* the notice is cosmetic */ }
    // Not awaited: it resolves when the main thread runs the turn.
    Promise.resolve($.prompt.submit({ text })).catch(() => {});
  }

  function lastFinished(view) {
    const f = finishedBatches(view);
    return f.length ? f[f.length - 1] : null;
  }

  let refusedOnce = false;
  async function execute(view, actions) {
    let spawned = false;
    for (const a of actions) {
      if (a.type === 'spawn') {
        spawned = true;
        const now = Date.now();
        if (a.role === 'implementer') {
          const n = a.batch;
          const b = (snap.batches ?? []).find((x) => x.n === n);
          st.launched[String(n)] = now;
          delete st.forced[String(n)];
          st.retry = st.retry.filter((k) => k !== n);
          const prompt = batchPrompt({ text: b?.text ?? `Batch ${n}`, failure: a.failure });
          await saveState();
          await writeMarker(snap.hash);
          const res = await spawn('implementer', `batch ${n}`, prompt, view);
          if (res.deny || !res.agentId) st.forced[String(n)] = { status: 'not-run', detail: `dispatch refused: ${res.deny ?? 'no agent'}`, ts: now };
          else st.inFlight[res.agentId] = { role: 'implementer', batch: n };
        } else if (a.role === 'tester') {
          st.suiteLaunched = now;
          await saveState();
          await writeMarker(snap.hash);
          const res = await spawn('tester', 'full suite', testerPrompt(snap.hash), view);
          if (res.deny || !res.agentId) { st.forcedSuite = { status: 'fail', red: true }; st.testerAnswer = `dispatch refused: ${res.deny ?? 'no agent'}`; }
          else st.inFlight[res.agentId] = { role: 'tester' };
        } else {
          st.reviewLaunched = now;
          await saveState();
          await writeMarker(snap.hash);
          const res = await spawn('reviewer', 'review', reviewerPrompt(snap.hash, snap.projectDir ?? ''), view);
          if (res.deny || !res.agentId) { st.forcedReview = { id: '' }; st.reviewAnswer = `dispatch refused: ${res.deny ?? 'no agent'}`; }
          else st.inFlight[res.agentId] = { role: 'reviewer' };
        }
      } else if (a.type === 'ask') {
        if (headless) {
          if (a.kind === 'red') {
            const b = (snap.batches ?? []).find((x) => x.n === a.batch);
            const verdict = view.verdicts[String(a.batch)] ?? { status: 'not-run' };
            await handback('red', {
              n: a.batch, command: b?.accept?.command, verdict,
              batches: view.batches.map((x) => x.n), recorded: view.verdicts,
            });
          } else {
            const n = lastFinished(view);
            await handback('pause', {
              n, verdict: view.verdicts[String(n)] ?? { status: 'pass' },
              batches: view.batches.map((x) => x.n), recorded: view.verdicts, dependents: a.batches,
            });
          }
          return;
        }
        st.ask = a;
        await saveState();
        await writeMarker(snap.hash);
      } else if (a.type === 'handback') {
        await handback(a.reason, {
          n: st.adjustBatch ?? undefined,
          tester: st.testerAnswer,
          report: st.reviewAnswer,
          review: snap.review ?? undefined,
          reason: snap.reviewNeeded?.reason,
        });
        return;
      } else if (a.type === 'wait') {
        await writeMarker(snap.hash);
      }
    }
    if (spawned) { st.ask = null; await saveState(); }
    // A refused dispatch recorded a verdict at once: decide again (red asks, a red suite hands back).
    if (spawned && (Object.keys(st.forced).length || st.forcedSuite || st.forcedReview) && !refusedOnce) {
      refusedOnce = true;
      const v = build();
      if (!Object.keys(st.inFlight).length) await execute(v, nextActions(v).actions);
      refusedOnce = false;
    }
  }

  async function stepInner() {
    if (!snap || !snap.ok) { await refresh(); }
    const s = snap;
    if (!s || !s.ok || !s.hash) return;
    if (s.config?.orchestrator === 'off') return;
    if (finished === s.hash) return;
    await loadState(s.hash);
    const ours = !!st && st.ours === true && !st.done;
    if (st && st.done) return;
    if (!ours) {
      if (!(s.approved && s.fresh)) return;
      st = freshState(s.hash);
      // A yes/no asked before the take-over (Terminar plan) no longer applies.
      if (ui.confirm && !allowed(ui.confirm)) ui.confirm = null;
    }
    // The first time an owned plan appears the panel shows the Plan tab; afterwards the tab is the user's.
    if (s.hash !== lastOwnedHash) { lastOwnedHash = s.hash; ui.tab = 'plan'; }
    const view = build();
    st.phase = nextActions(view).phase;
    const { actions } = nextActions(view);
    await execute(view, actions);
  }

  /** Serializes the driver's work: events can overlap, state must not. */
  function queue(fn) {
    const run = chain.then(async () => {
      try {
        return await fn();
      } catch (err) {
        try { await release(); } catch { /* ignore */ }
        const wasOurs = !!st?.ours && !st.done;
        if (st) { st.ours = false; }
        if (wasOurs && snap?.hash && finished !== snap.hash) {
          // The mod failed after taking over: tell the main thread, once, so the plan goes on by hand.
          try {
            const v = build();
            await handback('failed', { batches: v.batches.map((x) => x.n), recorded: v.verdicts });
          } catch { /* the hand-back itself failed: stay silent, never loop */ }
        }
        return undefined;
      }
    });
    chain = run.catch(() => {});
    return run;
  }

  /** Looks at the plan and acts: takes over a fresh approved plan, or moves ours forward. */
  /** @param {'ask'|'turn'|'agent'|'press'|'panel'} [trigger] */
  function step(trigger = 'ask') {
    if (trigger === 'ask') asked = true;
    return queue(async () => {
      if (!wantsSnapshot({ trigger, asked, owned: isOwned() })) return;
      await refresh();
      await stepInner();
    });
  }

  /** An agent the module spawned answered. */
  function onAgentDone(agentId, answer) {
    if (!wantsSnapshot({ trigger: 'agent', asked, owned: isOwned() })) return Promise.resolve();
    return queue(async () => {
      await refresh();
      if (!snap?.ok || !snap.hash) return;
      await loadState(snap.hash);
      const info = st?.inFlight?.[agentId];
      if (!st || !info || st.done) return;
      delete st.inFlight[agentId];
      if (info.role === 'tester') st.testerAnswer = String(answer ?? '');
      if (info.role === 'reviewer') st.reviewAnswer = String(answer ?? '');
      const landed = () => {
        if (info.role === 'implementer') {
          const v = snap.verdicts?.[String(info.batch)];
          return !!v && v.status !== 'running' && (v.ts ?? 0) >= (st.launched[String(info.batch)] ?? 0);
        }
        return info.role === 'tester' ? snap.suite?.status === 'done' : !!snap.review;
      };
      for (let i = 0; i < waitTries && !landed(); i++) {
        await sleep(waitMs);
        await refresh();
      }
      if (!landed()) {
        const now = Date.now();
        if (info.role === 'implementer') st.forced[String(info.batch)] = { status: 'not-run', detail: 'the verdict was not recorded', ts: now };
        else if (info.role === 'tester') st.forcedSuite = { status: 'fail', red: true };
        else st.forcedReview = { id: '' };
      }
      await saveState();
      await stepInner();
    });
  }

  /** The user pressed a button of the pane. Returns a promise that settles when its work is done. */
  function press(action) {
    if (isPanelAction(action)) {
      if (action.startsWith('tab:')) {
        const id = action.slice(4);
        if (TABS.some((t) => t.id === id)) ui.tab = id;
        // First time on Stats: load it (later visits show what is cached; `r` reloads).
        if (id === 'stats' && !stats) return Promise.all([loadStats(), loadTrend()]).then(() => {});
        // First time on Config: the tools and the version, and Stats for the 1 h what-if.
        if (id === 'config') {
          const jobs = [];
          if (!toolsStarted) jobs.push(loadTools());
          if (!stats) jobs.push(loadStats());
          return Promise.all(jobs).then(() => {});
        }
        if (id === 'memory' && !memStarted) { memStarted = true; return Promise.all([loadMem('recent'), loadMem('handoff')]).then(() => {}); }
        if (id === 'features' && !featStarted) { featStarted = true; return loadFeatures(); }
        return Promise.resolve();
      }
      if (action.startsWith('feat:')) return pressFeature(action);
      if (action.startsWith('mem:')) {
        // From Agentes (librarian result) too: go to the Memoria tab so the detail is visible.
        ui.tab = 'memory';
        const jobs = [];
        if (!memStarted) { memStarted = true; jobs.push(loadMem('recent'), loadMem('handoff')); }
        // The list load clears `open`; queue the detail after it (same lane, in order).
        jobs.push(loadMem('get', action.slice(4)));
        return Promise.all(jobs).then(() => {});
      }
      if (action === 'mem-back') { ui.mem.open = null; return Promise.resolve(); }
      if (action === 'mem-handoff') return loadMem('handoff');
      if (action === 'mem-recent') { ui.mem.query = ''; ui.mem.key += 1; return loadMem('recent'); }
      if (action === 'launch:scout' || action === 'launch:librarian') { ui.launch.kind = action.slice(7); return Promise.resolve(); }
      if (action === 'launch-ask') {
        const q = ui.mem.locate.question;
        if (!q || launchLive() || inputBusy.has('launch')) return Promise.resolve();
        inputBusy.add('launch');
        return launchAgent('scout', q).finally(() => { inputBusy.delete('launch'); });
      }
      if (action === 'scope:user' || action === 'scope:repo') { ui.scope = action === 'scope:repo' ? 'repo' : 'user'; return Promise.resolve(); }
      if (action.startsWith('cfg:')) return pressCfg(action);
      if (action.startsWith('metric:')) { ui.trendSel = { ...ui.trendSel, metric: action.slice(7) }; return Promise.resolve(); }
      if (action.startsWith('range:') || action.startsWith('by:') || action === 'here') {
        ui.trendSel = action === 'here' ? { ...ui.trendSel, here: !ui.trendSel.here }
          : action.startsWith('range:') ? { ...ui.trendSel, range: action.slice(6) } : { ...ui.trendSel, by: action.slice(3) };
        return loadTrend();
      }
      if (action === 'summary-hide') { summary = null; return Promise.resolve(); }
      if (action.startsWith('batch:')) {
        const n = Number(action.slice(6));
        if (Number.isInteger(n) && n > 0) {
          // undefined = default (the running batch open); -1 = all closed.
          const running = panel().blocks?.find((x) => x.type === 'batches')?.items?.find((x) => x.open)?.n;
          const cur = ui.expanded === undefined ? running : ui.expanded;
          ui.expanded = cur === n ? -1 : n;
        }
        return Promise.resolve();
      }
      if (action.startsWith('pick:')) {
        syncPicked();
        const id = action.slice(5);
        if (id) { if (ui.picked.has(id)) ui.picked.delete(id); else ui.picked.add(id); }
        return Promise.resolve();
      }
      if (action === 'fix-picked') {
        syncPicked();
        const rid = snap?.ok ? snap.reviewDetail?.id : null;
        if (!ui.picked.size || isOwned() || !rid) return Promise.resolve();
        const ids = [...ui.picked].join(', ');
        const text = `Arreglar los hallazgos ${ids} del review ${rid} (marcados en el panel de nxy)`;
        // Not awaited: it resolves when the main thread runs the turn.
        Promise.resolve($.prompt.submit({ text })).catch(() => {});
        return Promise.resolve();
      }
      if (action === 'refresh') {
        if (ui.tab === 'features') { featStarted = true; return loadFeatures(); }
        if (ui.tab === 'memory') {
          memStarted = true;
          // With a memory open, reload it (and keep it open) instead of the list.
          const openId = ui.mem.open;
          const first = openId ? loadMem('get', openId) : ui.mem.query ? loadMem('search', ui.mem.query) : loadMem('recent');
          return Promise.all([first, loadMem('handoff')]).then(() => {});
        }
        if (ui.tab === 'config') return Promise.all([queue(refresh), loadTools()]).then(() => {});
        if (ui.tab !== 'stats') return queue(refresh);
        trends.clear();
        return Promise.all([queue(refresh), loadStats(), loadTrend()]).then(() => {});
      }
      if (action === 'dismiss') { output = null; return Promise.resolve(); }
      if (action.startsWith('agent:')) {
        const next = action.slice(6);
        if (next !== ui.agent) ui.pendingSend = null;
        ui.agent = next;
        return syncAgents();
      }
      if (action === 'agent-back') { ui.agent = null; ui.pendingSend = null; return Promise.resolve(); }
      if (action === 'agents-all') { ui.agentsAll = !ui.agentsAll; return Promise.resolve(); }
      if (action === 'confirm-no') { ui.confirm = null; ui.pendingSend = null; return Promise.resolve(); }
      if (action === 'confirm-yes') {
        if (ui.pendingSend) {
          const p = ui.pendingSend;
          ui.pendingSend = null;
          return sendTo(p.agentId, p.text);
        }
        const id = ui.confirm;
        ui.confirm = null;
        return id ? runLauncher(id) : Promise.resolve();
      }
      const e = launcherOf(action, launchCtx());
      if (!e || !allowed(action)) return Promise.resolve();
      if (e.confirm) { ui.confirm = action; return Promise.resolve(); }
      return runLauncher(action);
    }
    return queue(async () => {
      if (!st || st.done || !st.ask || !snap?.ok) return;
      const ask = st.ask;
      const view = build();
      const ackFinished = () => { st.acked = [...new Set([...st.acked, ...finishedBatches(view)])]; };
      if (ask.kind === 'red') {
        if (action === RETRY) st.retry.push(ask.batch);
        else if (action === CONTINUE_ANYWAY) { st.continued.push(ask.batch); ackFinished(); st.acked = [...new Set([...st.acked, ask.batch])]; }
        else if (action === STOP) { st.stopped = true; st.adjustBatch = ask.batch; }
        else return;
      } else {
        if (action === PAUSE_CONTINUE) ackFinished();
        else if (action === PAUSE_ADJUST) { st.adjust = true; st.adjustBatch = lastFinished(view); }
        else if (action === PAUSE_STOP) { st.stopped = true; st.adjustBatch = lastFinished(view); }
        else return;
      }
      st.ask = null;
      await saveState();
      await refresh();
      await stepInner();
    });
  }

  /** The plan the orchestrator owns right now (its snapshot), or null. */
  function view() {
    if (!snap?.ok || !st || !st.ours || st.done) return null;
    return build();
  }

  /** The status line under the prompt, from the cached snapshot (no node). */
  function status() {
    return statusText(isOwned() && snap?.ok ? build() : null, st?.ask ?? null);
  }

  /** The panel's view: the plan the orchestrator owns (with the snapshot's extras), or the idle snapshot. */
  function panel() {
    const ok = !!snap?.ok;
    const owned = ok && isOwned();
    syncPicked();
    syncLaunch();
    const s = !ok ? null : owned
      ? {
        ...build(), approved: snap.approved, handoff: snap.handoff, config: snap.config,
        goal: snap.goal, reviewDetail: snap.reviewDetail, projectDir: snap.projectDir, branch: snap.branch,
        inFlight: st?.inFlight ?? {},
      }
      : { ...snap, launched: st?.launched ?? snap.launched, inFlight: st?.inFlight ?? {} };
    const uiView = { ...ui, scope: /** @type {'user'|'repo'} */ (ui.scope), picked: [...ui.picked], expanded: ui.expanded };
    const now = Date.now();
    const co = cacheOf({ lastTs: cache.lastTs, ttl: cache.ttl, hitPct: cache.hitPct, tokens: cache.tokens, model: usage?.model, prices: snap?.prices, now });
    const cacheView = co && { ...co, coldTokens: co.coldTokens ?? undefined };
    if (cacheView && cache.cold) { cacheView.coldCostUsd = cache.cold.usd; cacheView.estimated = false; if (cache.cold.tokens != null) cacheView.coldTokens = cache.cold.tokens; }
    const inFlight = st?.inFlight ?? {};
    const actx = { now, inFlight, snap: s, owned };
    return buildPanel({
      agents: agentRows(book, actx), agentDetail: ui.agent ? agentDetail(book, ui.agent, actx) : null, viewAgentId,
      snap: s, ask: st?.ask ?? null, owned, ui: uiView, output, usage, cache: cacheView, now, fallback: { gate: cfg.gate },
      stats, trend: trends.get(trendKey(ui.trendSel)) ?? null, summary,
      settings: settingsView, cfgInfo, cfgNote,
      live: { model: usage?.model ?? null, effort: cache.effort, turnUsd: cache.turnUsd }, bodyColumns: columns,
    });
  }

  /** Reads the snapshot in the same node that clears the session start. Never throws: a failure leaves no snapshot. */
  function start() {
    return queue(async () => {
      try {
        const args = ['release', '--snapshot'];
        if (sessionId) args.push('--session', sessionId);
        const res = await runNode(args);
        const out = JSON.parse(String(res?.stdout ?? '').trim().split('\n').pop() || '{}');
        if (out && out.ok) {
          snap = out;
          if (typeof out.runtimeDir === 'string') runtimeDir = out.runtimeDir;
          setTtl(out.cacheTtl, 'observed');
          bookRoles(book, out.roles);
        } else snap = null;
      } catch { snap = null; }
    });
  }

  const setConfig = (c) => { cfg = c && typeof c === 'object' ? c : {}; };
  const panelSetting = () => snap?.config?.ui?.panel ?? cfg.panel ?? 'auto';
  const setUsage = (u) => { usage = u ?? null; };
  const launchCtx = () => {
    const f = ui.features;
    return {
      ...configCtx(snap?.ok ? snap : null, settingsView, cfgInfo && !cfgInfo.failed ? cfgInfo.tools : null),
      features: { name: f.name, issue: f.issue, base: f.base, branch: f.branch, main: f.data?.main ?? '', shell: f.data?.shell, target: f.target, force: f.force, rows: f.data?.worktrees ?? [] },
    };
  };

  /** The entry's `needs` holds for the plan as the driver sees it now (an owned plan fails `plan`). */
  function allowed(id) {
    const ctx = launchCtx();
    const e = launcherOf(id, ctx);
    return !!e && launcherAvailable(e, { snap: snap?.ok ? snap : null, owned: isOwned(), ctx });
  }

  /** Runs a launcher entry on its own lane: a slow Trend never holds the orchestrator's queue. */
  function runLauncher(id) {
    const e = launcherOf(id, launchCtx());
    if (!e || launching.has(id) || !allowed(id)) return Promise.resolve();
    launching.add(id);
    const token = ++runToken;
    output = { id, token, label: e.label, text: '...', running: true };
    /** Writes the run's output only over its own placeholder. @param {string} text */
    const finish = (text) => {
      if (output && output.id === id && output.token === token && output.running) output = { id, token, label: e.label, text };
    };
    let ranOk = false;
    const run = launchChain.then(async () => {
      try {
        const script = `${$.plugin.root}/hosts/claude-code/entries/${e.script}.mjs`;
        const res = await $.process.run(['node', '--disable-warning=ExperimentalWarning', script, ...e.args, '--cwd', cwd], e.timeoutMs ? { timeoutMs: e.timeoutMs } : undefined);
        finish(e.script === 'features' && res?.exitCode === 0 ? featuresText(id, String(res?.stdout ?? '')) : `${res?.stdout ?? ''}${res?.stderr ?? ''}`.trimEnd());
        ranOk = res?.exitCode === 0;
        // A finished update: load the new version now instead of asking for a restart. Not awaited: it resolves when the turn runs.
        if (id === 'update-nxy' && ranOk) Promise.resolve($.prompt.submit({ text: '/reload-plugins' })).catch(() => {});
        // settings.json changed: show it now (the next session.start reads the real value).
        if (e.after === 'settings' && res?.exitCode === 0 && settingsView) {
          if (id === 'cache-ttl') {
            settingsView = { ...settingsView, ttl1h: e.args[1] === '1h' };
            if (e.args[1] === '1h') setTtl('1h', 'settings');
          } else if (id === 'statusline') settingsView = { ...settingsView, statusline: e.args[1] === 'install' ? 'nxy' : 'none' };
        }
      } catch (err) {
        finish(`nxy: ${e.label} failed: ${/** @type {any} */ (err)?.message ?? err}`);
      }
      if (e.after === 'refresh') {
        await queue(async () => {
          if (id === 'plan-done') { if (snap?.hash) finished = snap.hash; if (st?.launched) launchedMemo = st.launched; st = null; }
          try { await refresh(); } catch { /* the next sync retries */ }
        });
      }
    }).finally(() => { launching.delete(id); });
    launchChain = run.catch(() => {});
    // A tool was installed or reinstalled: ask for the tools again so the row changes by itself (one node, queued after the run on the same lane).
    return run.then(() => {
      if (e.after === 'features') {
        // Only a successful Nuevo clears the form; Pausar and Cerrar leave a half-filled one alone.
        if (id === 'feat-new' && ranOk) resetFeatures();
        ui.features.target = ''; ui.features.force = false;
        return loadFeatures();
      }
      return e.after === 'tools' && ranOk ? loadTools() : undefined;
    });
  }

  /** The panel body's width in cells (`e.props.bodyColumns`); the module sets it before each `panel()`. */
  const setColumns = (n) => { if (Number.isFinite(n) && n > 0) columns = Math.floor(n); };

  return {
    noteSpawn, noteTool, noteAgentTurn, syncAgents, agentsLive, setViewAgent, submitMessage, sendTo, submitInput, pickBase, pickBranch,
    noteTurn, noteTurnStart, noteEffort, noteModelSwitch, setTtl, setColumns, cachePlan, cacheNotice, drainNotices, loadStats, loadTrend,
    step, start, onAgentDone, press, view, status, panel, setConfig, setSettings, panelSetting, setUsage, openPanel: () => queue(refresh), refresh: () => queue(refresh), release: () => queue(() => release()) };
}
