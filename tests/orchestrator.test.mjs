import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import {
  nextActions, batchPrompt, testerPrompt, reviewerPrompt, ticketDescription, ticketOf,
  handbackText, denyMessage, paneView, COMPOSE_SECTION, MARKER_TTL_MS, ORCH_DIR, MARKER_FILE, TICKET_DIR,
} from '../core/orchestrator.mjs';
import { afterBatch, reviewInstruction } from '../core/verify.mjs';
import * as status from '../core/batch-status.mjs';
import * as verify from '../core/verify.mjs';

const snap = (over = {}) => ({
  hash: 'abc12345',
  batches: [{ n: 1, depends: [] }, { n: 2, depends: [1] }],
  verdicts: {}, launched: {}, continued: [], acked: [],
  stopped: false, pauseAfterBatch: false,
  suite: null, suiteLaunched: false, review: null, reviewLaunched: false,
  reviewNeeded: { needed: true },
  ...over,
});
const pass = (ts = 10) => ({ status: 'pass', ts });
const fail = (ts = 10) => ({ status: 'fail', error: 'boom', ts });
const spawns = (r) => r.actions.filter((a) => a.type === 'spawn').map((a) => a.role === 'implementer' ? `b${a.batch}` : a.role);

test('linear plan: batch 1, then 2 once 1 passes', () => {
  assert.deepEqual(spawns(nextActions(snap())), ['b1']);
  assert.deepEqual(spawns(nextActions(snap({ verdicts: { 1: pass() }, launched: { 1: 5 } }))), ['b2']);
});

test('Depends: independent batches spawn together, dependents wait', () => {
  const batches = [{ n: 1, depends: [] }, { n: 2, depends: [1] }, { n: 3, depends: [1] }, { n: 4, depends: [2, 3] }];
  assert.deepEqual(spawns(nextActions(snap({ batches }))), ['b1']);
  assert.deepEqual(spawns(nextActions(snap({ batches, verdicts: { 1: pass() }, launched: { 1: 5 } }))), ['b2', 'b3']);
  const r = nextActions(snap({ batches, verdicts: { 1: pass(), 2: pass() }, launched: { 1: 5, 2: 11, 3: 11 } }));
  assert.deepEqual(r.actions.map((a) => a.type), ['wait']);
});

test('running means wait; a stale verdict (ts < launched) counts as running', () => {
  const r = nextActions(snap({ verdicts: { 1: fail(10) }, launched: { 1: 20 } }));
  assert.deepEqual(r.actions.map((a) => a.type), ['wait']);
  assert.deepEqual(nextActions(snap({ verdicts: { 1: { status: 'running', ts: 30 } }, launched: { 1: 20 } })).actions.map((a) => a.type), ['wait']);
  assert.deepEqual(nextActions(snap({ launched: { 1: 20 } })).actions.map((a) => a.type), ['wait']);
});

test('red asks only when idle; Retry re-spawns with the failure; Continue unlocks dependents; Stop hands back', () => {
  const red = snap({ verdicts: { 1: fail() }, launched: { 1: 5 } });
  assert.deepEqual(nextActions(red).actions, [{ type: 'ask', kind: 'red', batch: 1 }]);
  // another batch still running: no ask yet
  const par = snap({ batches: [{ n: 1, depends: [] }, { n: 2, depends: [] }], verdicts: { 1: fail() }, launched: { 1: 5, 2: 6 } });
  assert.deepEqual(nextActions(par).actions.map((a) => a.type), ['wait']);
  const retry = nextActions({ ...red, retry: [1] });
  assert.deepEqual(retry.actions, [{ type: 'spawn', role: 'implementer', batch: 1, retry: true, failure: 'boom' }]);
  const cont = nextActions({ ...red, continued: [1] });
  assert.deepEqual(spawns(cont), ['b2']);
  assert.deepEqual(nextActions({ ...red, stopped: true }).actions, [{ type: 'handback', reason: 'stopped' }]);
});

test('pause: off goes on, on asks once per wave, never after the last batch', () => {
  const done1 = { verdicts: { 1: pass() }, launched: { 1: 5 } };
  assert.deepEqual(spawns(nextActions(snap(done1))), ['b2']);
  const paused = snap({ ...done1, pauseAfterBatch: true });
  assert.deepEqual(nextActions(paused).actions, [{ type: 'ask', kind: 'pause', batches: [2] }]);
  assert.deepEqual(spawns(nextActions({ ...paused, acked: [1] })), ['b2']);
  // last batch done: no pause, goes to the suite
  const last = nextActions(snap({ pauseAfterBatch: true, verdicts: { 1: pass(), 2: pass() }, launched: { 1: 5, 2: 6 }, acked: [1] }));
  assert.deepEqual(spawns(last), ['tester']);
  // the first wave is never paused before it runs
  assert.deepEqual(spawns(nextActions(snap({ pauseAfterBatch: true }))), ['b1']);
});

test('all green: tester, reviewer, checkpoint 2', () => {
  const base = { verdicts: { 1: pass(), 2: pass() }, launched: { 1: 5, 2: 6 } };
  assert.deepEqual(spawns(nextActions(snap(base))), ['tester']);
  assert.deepEqual(nextActions(snap({ ...base, suiteLaunched: true })).actions.map((a) => a.type), ['wait']);
  assert.deepEqual(nextActions(snap({ ...base, suite: { status: 'running', ts: 7 } })).actions, [{ type: 'wait', reason: 'full suite running' }]);
  const green = { ...base, suite: { status: 'done', red: [] } };
  assert.deepEqual(spawns(nextActions(snap(green))), ['reviewer']);
  assert.deepEqual(nextActions(snap({ ...green, reviewLaunched: true })).actions.map((a) => a.type), ['wait']);
  const end = nextActions(snap({ ...green, review: { id: 'r1' } }));
  assert.deepEqual(end.actions, [{ type: 'handback', reason: 'checkpoint2' }]);
});

test('suite red and review not needed hand back', () => {
  const base = { verdicts: { 1: pass(), 2: pass() }, launched: { 1: 5, 2: 6 } };
  const red = [{ command: 'npm run check', error: 'Exit code 1' }];
  assert.deepEqual(nextActions(snap({ ...base, suite: { status: 'done', red } })).actions, [{ type: 'handback', reason: 'suite-red' }]);
  assert.deepEqual(nextActions(snap({ ...base, suite: { status: 'done', red: [] }, reviewNeeded: { needed: false, reason: 'docs only' } })).actions,
    [{ type: 'handback', reason: 'review-skipped' }]);
});

test('prompts match what the main thread is told today', () => {
  const hash = 'abc12345';
  const need = reviewInstruction({ hash, project: 'C:\\x\\y', needed: true });
  assert.ok(need.includes(`"${reviewerPrompt(hash, 'C:\\x\\y')}"`));
  const after = afterBatch({ hash, n: 2, verdict: { status: 'pass' }, batches: [1, 2], recorded: { 1: { status: 'pass' } } });
  assert.ok(after.includes(`"${testerPrompt(hash)}"`));
  assert.equal(handbackText('red', { hash, n: 1, verdict: { status: 'fail', error: 'e' }, batches: [1, 2], recorded: {} }),
    afterBatch({ hash, n: 1, verdict: { status: 'fail', error: 'e' }, batches: [1, 2], recorded: {} }));
  assert.equal(handbackText('review-skipped', { hash, reason: 'r' }), reviewInstruction({ hash, project: '', needed: false, reason: 'r' }));
  assert.equal(batchPrompt({ text: 'Batch 1 — x\nAccept: `y`  ' }), 'Batch 1 — x\nAccept: `y`');
  assert.match(batchPrompt({ text: 'Batch 1 — x', failure: 'boom' }), /^Batch 1 — x\n\n.*boom/s);
});

test('failed hand-back names the hash, finished and pending batches, and the by-hand path', () => {
  const t = handbackText('failed', { hash: 'abc12345', batches: [1, 2, 3], recorded: { 1: { status: 'pass' } } });
  assert.match(t, /plan abc12345/);
  assert.match(t, /Finished batches: 1\. Left: 2, 3\./);
  assert.match(t, /by hand/);
});

test('checkpoint 2 carries the reviewer report verbatim', () => {
  const report = 'R1 — something\n  detail';
  assert.ok(handbackText('checkpoint2', { hash: 'h', review: { id: 'r1' }, report }).includes(report));
});

test('tickets round trip', () => {
  const d = ticketDescription('a1B2_c-3', 'batch 2');
  assert.deepEqual(ticketOf(d), { nonce: 'a1B2_c-3', label: 'batch 2' });
  assert.equal(ticketOf('Batch 2 — nothing'), null);
  assert.equal(ticketOf(undefined), null);
});

test('constants, texts and pane view', () => {
  assert.equal(MARKER_TTL_MS, 7200000);
  assert.ok(ORCH_DIR && MARKER_FILE && TICKET_DIR);
  assert.ok(COMPOSE_SECTION.length > 0);
  assert.match(denyMessage('h', 'node x release'), /node x release/);
  const s = snap({ verdicts: { 1: fail() }, launched: { 1: 5 } });
  const v = paneView(s, nextActions(s).actions[0]);
  assert.deepEqual(v.buttons, ['Retry', 'Continue anyway', 'Stop']);
  assert.ok(v.rows.some((r) => r.tone === 'error'));
  assert.deepEqual(paneView(snap(), null).buttons, []);
});

test('verify.mjs still re-exports the shared names', () => {
  for (const k of ['RETRY', 'CONTINUE', 'STOP', 'isRed', 'isDone', 'blockingBatch', 'continueQuestion', 'afterBatch', 'reviewInstruction']) {
    assert.equal(verify[k], status[k], k);
  }
});

test('the module-side sources stay free of node: imports, dynamic import and process', () => {
  for (const f of ['../core/orchestrator.mjs', '../core/batch-status.mjs']) {
    const src = readFileSync(fileURLToPath(new URL(f, import.meta.url)), 'utf8');
    assert.doesNotMatch(src, /from\s+['"]node:/, f);
    assert.doesNotMatch(src, /import\(/, f);
    assert.doesNotMatch(src, /\bprocess\./, f);
  }
});

// ---- always-on panel ----
import {
  wantsSnapshot, statusText, panelView, readModConfig, launcherOf, isPanelAction, LAUNCHER,
} from '../core/orchestrator.mjs';

test('wantsSnapshot: the one table of when node may run', () => {
  assert.equal(wantsSnapshot({ trigger: 'ask' }), true);
  assert.equal(wantsSnapshot({ trigger: 'panel' }), true);
  assert.equal(wantsSnapshot({ trigger: 'turn', asked: false, owned: false }), false);
  assert.equal(wantsSnapshot({ trigger: 'turn', asked: true, owned: false }), true);
  assert.equal(wantsSnapshot({ trigger: 'turn', asked: false, owned: true }), true);
  assert.equal(wantsSnapshot({ trigger: 'agent', asked: true, owned: false }), false);
  assert.equal(wantsSnapshot({ trigger: 'agent', owned: true }), true);
  assert.equal(wantsSnapshot({ trigger: 'press', owned: false }), false);
  assert.equal(wantsSnapshot({ trigger: 'press', owned: true }), true);
});

test('statusText: ready, batch N/M, tester, review, pause, red', () => {
  assert.equal(statusText(null), 'nxy ● ready');
  assert.equal(statusText(snap({ launched: { 1: 5 } })), 'nxy ▶ batch 1/2');
  assert.equal(statusText(snap({ verdicts: { 1: pass() }, launched: { 1: 5 } })), 'nxy ▶ batch 2/2');
  const allPass = { verdicts: { 1: pass(), 2: pass() }, launched: { 1: 5, 2: 5 } };
  assert.equal(statusText(snap(allPass)), 'nxy ▶ tester');
  assert.equal(statusText(snap({ ...allPass, suite: { status: 'done', red: [] } })), 'nxy ▶ review');
  assert.equal(statusText(snap({ ...allPass, suite: { status: 'done', red: [] }, review: { id: 'r' } })), 'nxy ● ready');
  assert.equal(statusText(snap({ stopped: true })), 'nxy ● ready');
  const paused = snap({ pauseAfterBatch: true, verdicts: { 1: pass() }, launched: { 1: 5 } });
  assert.equal(statusText(paused), 'nxy ⏸ paused after batch 1');
  assert.equal(statusText(paused, { type: 'ask', kind: 'pause', batches: [2] }), 'nxy ⏸ paused after batch 1');
  const red = snap({ verdicts: { 1: fail() }, launched: { 1: 5 } });
  assert.equal(statusText(red), 'nxy ✘ batch 1 needs you');
  assert.equal(statusText(red, { type: 'ask', kind: 'red', batch: 1 }), 'nxy ✘ batch 1 needs you');
});

test('launcher: labels map to entries; unknown is null', () => {
  assert.deepEqual(launcherOf('Filter on'), { script: 'filter', args: ['on'] });
  assert.deepEqual(launcherOf('Handoff'), { script: 'mem', args: ['handoff', 'show'] });
  assert.equal(launcherOf('Nope'), null);
  assert.equal(isPanelAction('Refresh'), true);
  assert.equal(isPanelAction('Close'), true);
  assert.equal(isPanelAction('Trend'), true);
  assert.equal(isPanelAction('Retry'), false);
  assert.equal(LAUNCHER.length, 6);
});

test('panelView: plan, handoff, gate, orchestrator, output', () => {
  const now = 10_000_000;
  const cfg = { gate: { enabled: true, contextTokens: 100000 } };
  const none = panelView({ snap: /** @type {any} */ ({ hash: null, batches: [], config: cfg, handoff: null }), usage: { tokens: 46000, percent: 46 }, now });
  assert.deepEqual(none.rows.map((r) => r.text), ['plan: none', 'handoff: none', 'gate: threshold 100000 · context now 46k (46%)']);
  assert.deepEqual(none.buttons, ['Refresh', ...LAUNCHER.map((l) => l.label), 'Close']);
  const withPlan = panelView({ snap: { ...snap({ verdicts: { 1: pass() }, launched: { 1: 5 } }), approved: true, handoff: { updated: now - 12 * 60000 }, config: { gate: { enabled: false }, orchestrator: 'off' } }, now });
  assert.deepEqual(withPlan.rows.map((r) => r.text), ['plan: abc12345 · batch 2 of 2 · approved', 'handoff: updated 12 min ago', 'gate: off', 'orchestrator: off']);
  const notApproved = panelView({ snap: { ...snap(), approved: false, config: cfg }, usage: null, now });
  assert.match(notApproved.rows[0].text, /not approved yet$/);
  assert.equal(notApproved.rows.at(-1)?.text, 'gate: threshold 100000 · context now unknown');
  const long = Array.from({ length: 40 }, (_, i) => (i === 0 ? 'x'.repeat(300) : `l${i}`)).join('\n');
  const out = panelView({ snap: null, output: { label: 'Stats', text: long } });
  const texts = out.rows.map((r) => r.text);
  assert.equal(texts.at(-1), '… (10 more lines)');
  assert.ok(texts.every((t) => t.length <= 160));
});

test('panelView with snap null shows what the Mod already has', () => {
  const v = panelView({ snap: null, fallback: { gate: { enabled: true, contextTokens: 5000 } }, usage: { tokens: 1500, percent: 30 } });
  assert.deepEqual(v.rows.map((r) => r.text), ['plan: not read yet — press Refresh', 'handoff: not read yet', 'gate: threshold 5000 · context now 2k (30%)']);
});

test('readModConfig: defaults, user, project, broken text, ui.panel off', () => {
  const d = JSON.stringify({ ui: { panel: 'auto' }, gate: { enabled: false, contextTokens: 100000 } });
  assert.deepEqual(readModConfig([d]), { panel: 'auto', gate: { enabled: false, contextTokens: 100000 } });
  assert.deepEqual(readModConfig([d, JSON.stringify({ gate: { enabled: true } }), JSON.stringify({ gate: { contextTokens: 5 } })]),
    { panel: 'auto', gate: { enabled: true, contextTokens: 5 } });
  assert.deepEqual(readModConfig([d, '{ nope', null]), { panel: 'auto', gate: { enabled: false, contextTokens: 100000 } });
  assert.equal(readModConfig([d, JSON.stringify({ ui: { panel: 'off' } })]).panel, 'off');
  assert.equal(readModConfig([JSON.stringify({ ui: { panel: 'weird' } })]).panel, 'auto');
  assert.deepEqual(readModConfig(null), { panel: 'auto', gate: { enabled: false, contextTokens: 0 } });
});
