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
import { TABS, buildPanel, launcherOf, launcherAvailable, filterCtx, isPanelAction } from '../../../core/panel.mjs';

const CONTINUE_ANYWAY = CONTINUE;
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
  const ui = { tab: 'home', /** @type {string|null} */ confirm: null };
  /** @type {string|null} the plan hash the Plan tab was already shown for */
  let lastOwnedHash = null;
  /** @type {Promise<any>} the launcher's own lane, independent of `chain` */
  let launchChain = Promise.resolve();
  const launching = new Set();
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
    snap = out;
    if (out && out.ok && typeof out.runtimeDir === 'string') runtimeDir = out.runtimeDir;
    return out;
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
    return res ?? {};
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
        return Promise.resolve();
      }
      if (action === 'refresh') return queue(refresh);
      if (action === 'dismiss') { output = null; return Promise.resolve(); }
      if (action === 'confirm-no') { ui.confirm = null; return Promise.resolve(); }
      if (action === 'confirm-yes') {
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
    const s = !ok ? null : owned ? { ...build(), approved: snap.approved, handoff: snap.handoff, config: snap.config } : snap;
    // The first time an owned plan appears the panel shows the Plan tab; afterwards the tab is the user's.
    if (owned && snap.hash && snap.hash !== lastOwnedHash) { lastOwnedHash = snap.hash; ui.tab = 'plan'; }
    return buildPanel({ snap: s, ask: st?.ask ?? null, owned, ui, output, usage, now: Date.now(), fallback: { gate: cfg.gate } });
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
        } else snap = null;
      } catch { snap = null; }
    });
  }

  const setConfig = (c) => { cfg = c && typeof c === 'object' ? c : {}; };
  const panelSetting = () => snap?.config?.ui?.panel ?? cfg.panel ?? 'auto';
  const setUsage = (u) => { usage = u ?? null; };
  const launchCtx = () => filterCtx(snap?.ok ? snap : null);

  /** The entry's `needs` holds for the plan as the driver sees it now (an owned plan fails `plan`). */
  function allowed(id) {
    const e = launcherOf(id);
    return !!e && launcherAvailable(e, { snap: snap?.ok ? snap : null, owned: isOwned() });
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
    const run = launchChain.then(async () => {
      try {
        const script = `${$.plugin.root}/hosts/claude-code/entries/${e.script}.mjs`;
        const res = await $.process.run(['node', '--disable-warning=ExperimentalWarning', script, ...e.args, '--cwd', cwd]);
        finish(`${res?.stdout ?? ''}${res?.stderr ?? ''}`.trimEnd());
      } catch (err) {
        finish(`nxy: ${e.label} failed: ${/** @type {any} */ (err)?.message ?? err}`);
      }
      if (e.after === 'refresh') {
        await queue(async () => {
          if (id === 'plan-done') { if (snap?.hash) finished = snap.hash; st = null; }
          try { await refresh(); } catch { /* the next sync retries */ }
        });
      }
    }).finally(() => { launching.delete(id); });
    launchChain = run.catch(() => {});
    return run;
  }

  return { step, start, onAgentDone, press, view, status, panel, setConfig, panelSetting, setUsage, openPanel: () => queue(refresh), refresh: () => queue(refresh), release: () => queue(() => release()) };
}
