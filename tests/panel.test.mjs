// @ts-nocheck — the view model is plain data; the tests poke into it freely
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import {
  TABS, LAUNCHER, BUILDERS, NEEDS, launcherOf, launcherAvailable, filterCtx, isPanelAction, buildPanel, panelText, barText, kFmt,
  fitSteps, lifecycle, clipOutput,
} from '../core/panel.mjs';

const NOW = 1_000_000_000;
const snap = (over = {}) => ({
  hash: 'abc12345', approved: true,
  batches: [{ n: 1, depends: [], title: 'First' }, { n: 2, depends: [1], title: 'Second' }],
  verdicts: {}, launched: {}, continued: [], acked: [], stopped: false, pauseAfterBatch: false,
  suite: null, suiteLaunched: false, review: null, reviewLaunched: false, reviewNeeded: { needed: true },
  handoff: null, config: { gate: { enabled: true, contextTokens: 100000 }, filter: false },
  ...over,
});
const pass = (ts = 10) => ({ status: 'pass', ts });
const fail = (ts = 10) => ({ status: 'fail', error: 'boom', ts });
const usage = (tokens) => ({ tokens, percent: 10, window: 1000000, costUsd: 1.234, model: 'sonnet' });
const home = (o) => buildPanel({ snap: snap(), now: NOW, ...o });
const rowOf = (v, label) => v.sections[0].rows.find((r) => r.label === label);
const hotkeys = (v) => [
  ...v.tabs.map((t) => t.hotkey), ...v.keys.map((k) => k.hotkey),
  ...v.sections.flatMap((s) => (s.actions ?? []).map((a) => a.hotkey)),
  ...(v.ask?.buttons ?? []).map((b) => b.hotkey), ...(v.output ? [v.output.dismiss.hotkey] : []),
];

test('TABS: order, builders, active tab', () => {
  assert.deepEqual(TABS.map((t) => t.id), ['home', 'plan']);
  for (const t of TABS) assert.equal(typeof BUILDERS[t.id], 'function');
  assert.deepEqual(home({}).tabs.map((t) => t.active), [true, false]);
  assert.deepEqual(home({ ui: { tab: 'plan' } }).tabs.map((t) => t.active), [false, true]);
  assert.equal(home({ ui: { tab: 'nope' } }).tabs[0].active, true);
});

test('Home with no snapshot: reads as not read yet, still has buttons', () => {
  const v = buildPanel({ snap: null, now: NOW });
  assert.equal(rowOf(v, 'Plan').value, 'sin leer');
  assert.equal(rowOf(v, 'Contexto').value, 'desconocido');
  assert.ok(v.sections[0].actions.some((a) => a.id === 'gate-once'));
  assert.deepEqual(v.header.pill, { glyph: '●', text: 'ready', tone: 'ok' });
});

test('Home with a plan: hash, state, lifecycle', () => {
  const v = home({});
  assert.equal(rowOf(v, 'Plan').value, 'abc12345 · aprobado');
  assert.equal(home({ snap: snap({ approved: false }) }).sections[0].rows[0].value, 'abc12345 · sin aprobar');
  assert.equal(rowOf(home({ snap: snap({ hash: null, batches: [] }) }), 'Plan').value, 'ninguno');
  const steps = rowOf(v, 'Ciclo').steps;
  assert.deepEqual(steps.map((s) => s.label), ['plan', 'aprobado', 'lotes 0/2', 'tester', 'review', 'cierre']);
  assert.deepEqual(steps.map((s) => s.state), ['done', 'done', 'current', 'pending', 'pending', 'pending']);
});

test('lifecycle: red batch, red suite, all done', () => {
  assert.equal(lifecycle(snap({ verdicts: { 1: fail() }, launched: { 1: 5 } }))[2].state, 'red');
  const all = { verdicts: { 1: pass(), 2: pass() }, launched: { 1: 5, 2: 5 } };
  assert.equal(lifecycle(snap({ ...all, suite: { status: 'done', red: [{}] } }))[3].state, 'red');
  const done = lifecycle(snap({ ...all, suite: { status: 'done', red: [] }, review: { id: 'r' } }));
  assert.ok(done.every((s) => s.state === 'done'));
  assert.deepEqual(lifecycle(null), []);
});

test('Context row: gate on bar and thresholds, gate off, unknown', () => {
  const on = rowOf(home({ usage: usage(46000) }), 'Contexto');
  assert.deepEqual(on.bar, { used: 46000, limit: 100000, tone: 'ok' });
  assert.equal(on.value, '46k / 100k');
  assert.equal(on.note, 'gate on · 54k left');
  assert.equal(rowOf(home({ usage: usage(70000) }), 'Contexto').bar.tone, 'running');
  assert.equal(rowOf(home({ usage: usage(69000) }), 'Contexto').bar.tone, 'ok');
  assert.equal(rowOf(home({ usage: usage(90000) }), 'Contexto').bar.tone, 'error');
  const off = rowOf(home({ snap: snap({ config: { gate: { enabled: false }, filter: false } }), usage: usage(46000) }), 'Contexto');
  assert.equal(off.bar.limit, 1000000);
  assert.equal(off.note, 'gate off');
  assert.equal(rowOf(home({}), 'Contexto').value, 'desconocido');
  const fb = rowOf(buildPanel({ snap: null, usage: usage(1500), fallback: { gate: { enabled: true, contextTokens: 5000 } } }), 'Contexto');
  assert.equal(fb.bar.limit, 5000);
});

test('Session row: cost and model', () => {
  assert.equal(rowOf(home({ usage: usage(1) }), 'Sesión').value, '$1.23 · sonnet');
  assert.equal(rowOf(home({}), 'Sesión').value, 'desconocido');
});

test('Handoff row: none, fresh, stale', () => {
  assert.equal(rowOf(home({}), 'Handoff').value, 'ninguno');
  const fresh = rowOf(home({ snap: snap({ handoff: { updated: NOW - 12 * 60000 }, verdicts: { 1: pass(NOW - 30 * 60000) } }) }), 'Handoff');
  assert.deepEqual([fresh.value, fresh.tone], ['hace 12 min', 'info']);
  const stale = rowOf(home({ snap: snap({ handoff: { updated: NOW - 12 * 60000 }, verdicts: { 1: pass(NOW - 5 * 60000) } }) }), 'Handoff');
  assert.deepEqual([stale.value, stale.tone], ['hace 12 min · desactualizado', 'error']);
});

test('plan-done: hidden with no plan or when owned; filter label by state', () => {
  const ids = (v) => v.sections[0].actions.map((a) => a.id);
  assert.ok(ids(home({})).includes('plan-done'));
  assert.ok(!ids(home({ snap: snap({ hash: null, batches: [] }) })).includes('plan-done'));
  assert.ok(ids(home({ snap: snap({ hash: null, batches: [], handoff: { updated: 1 } }) })).includes('plan-done'));
  assert.ok(!ids(home({ owned: true })).includes('plan-done'));
  const label = (v) => v.sections[0].actions.find((a) => a.id === 'filter').label;
  assert.equal(label(home({})), 'Filter ○ off');
  assert.equal(label(home({ snap: snap({ config: { filter: true } }) })), 'Filter ● on');
});

test('filter label: a plain Filter until a snapshot supplies config.filter', () => {
  const label = (v) => v.sections[0].actions.find((a) => a.id === 'filter').label;
  assert.equal(label(buildPanel({ snap: null, now: NOW })), 'Filter');
  assert.equal(label(home({ snap: snap({ config: { gate: { enabled: false } } }) })), 'Filter');
  assert.equal(label(home({ snap: snap({ config: { filter: false } }) })), 'Filter ○ off');
  assert.deepEqual(filterCtx(null), {});
  assert.deepEqual(filterCtx({ config: { filter: true } }), { filter: true });
  assert.equal(launcherOf('filter').label, 'Filter');
  assert.deepEqual(launcherOf('filter').args, ['on']);
});

test('needs: every launcher need is a known check; the Home filter reads it', () => {
  for (const l of LAUNCHER) if (l.needs) assert.equal(typeof NEEDS[l.needs], 'function', l.id);
  const done = launcherOf('plan-done');
  assert.equal(launcherAvailable(done, { snap: snap() }), true);
  assert.equal(launcherAvailable(done, { snap: snap(), owned: true }), false);
  assert.equal(launcherAvailable(done, { snap: null }), false);
  assert.equal(launcherAvailable(done, { snap: snap({ hash: null, handoff: { updated: 1 } }) }), true);
  assert.equal(launcherAvailable(launcherOf('stats'), { snap: null, owned: true }), true);
  const ids = buildPanel({ snap: snap(), owned: true, now: NOW }).sections[0].actions.map((a) => a.id);
  assert.deepEqual(ids, LAUNCHER.filter((l) => l.tab === 'home' && !l.needs).map((l) => l.id));
});

test('launcherOf and isPanelAction', () => {
  assert.deepEqual(launcherOf('filter', { filter: true }).args, ['off']);
  assert.deepEqual(launcherOf('filter', { filter: false }).args, ['on']);
  assert.deepEqual(launcherOf('gate-once'), { ...LAUNCHER[0], args: ['once'] });
  const done = launcherOf('plan-done');
  assert.ok(done.confirm && done.needs === 'plan' && done.after === 'refresh');
  assert.equal(launcherOf('nope'), null);
  for (const id of ['refresh', 'dismiss', 'tab:plan', 'confirm-yes', 'confirm-no', 'stats', 'trend']) assert.equal(isPanelAction(id), true);
  assert.equal(isPanelAction('Retry'), false);
});

test('Plan tab: batches with titles and states, suite and review rows', () => {
  const v = buildPanel({ snap: snap({ verdicts: { 1: pass(), 2: fail() }, launched: { 1: 5, 2: 6 }, continued: [2] }), owned: true, ui: { tab: 'plan' } });
  const sec = v.sections[0];
  assert.deepEqual(sec.batches.map((b) => [b.n, b.title, b.state]), [[1, 'First', 'done'], [2, 'Second', 'red']]);
  assert.equal(sec.batches[1].note, 'failed (continued anyway)');
  assert.equal(sec.rows[0].value, 'pending');
  const run = buildPanel({ snap: snap({ launched: { 1: 5 } }), owned: true, ui: { tab: 'plan' } });
  assert.equal(run.sections[0].batches[0].state, 'running');
  assert.equal(run.sections[0].batches[1].state, 'pending');
  assert.equal(buildPanel({ snap: snap({ hash: null, batches: [] }), ui: { tab: 'plan' } }).sections[0].rows[0].value, 'sin plan');
});

test('asks: red, pause, confirm', () => {
  const red = buildPanel({ snap: snap({ verdicts: { 1: fail() }, launched: { 1: 5 } }), owned: true, ask: { type: 'ask', kind: 'red', batch: 1 } });
  assert.equal(red.ask.tone, 'error');
  assert.deepEqual(red.ask.buttons.map((b) => [b.id, b.hotkey]), [['Retry', 'a'], ['Continue anyway', 'c'], ['Stop', 'x']]);
  assert.deepEqual(red.header.pill, { glyph: '✘', text: 'batch 1 needs you', tone: 'error' });
  const pause = buildPanel({ snap: snap(), owned: true, ask: { type: 'ask', kind: 'pause', batches: [2] } });
  assert.deepEqual(pause.ask.buttons.map((b) => [b.id, b.hotkey]), [['Continue', 'c'], ['Adjust', 'a'], ['Stop', 'x']]);
  const confirm = home({ ui: { confirm: 'plan-done' } });
  assert.deepEqual(confirm.ask.buttons.map((b) => [b.id, b.hotkey]), [['confirm-yes', 'y'], ['confirm-no', 'n']]);
  assert.equal(home({}).ask, null);
  // An ask shows on both tabs.
  assert.ok(buildPanel({ snap: snap(), owned: true, ask: { type: 'ask', kind: 'pause', batches: [2] }, ui: { tab: 'plan' } }).ask);
});

test('output: clipped lines, running placeholder, dismiss key', () => {
  const v = home({ output: { label: 'Stats', text: 'a\nb' } });
  assert.deepEqual(v.output.lines, ['a', 'b']);
  assert.equal(v.output.title, 'Stats');
  assert.equal(home({ output: { label: 'Trend', text: '...', running: true } }).output.running, true);
  assert.equal(clipOutput(Array.from({ length: 40 }, (_, i) => `l${i}`).join('\n')).length, 31);
});

test('hotkeys are unique inside each built view', () => {
  const asks = [null, { type: 'ask', kind: 'red', batch: 1 }, { type: 'ask', kind: 'pause', batches: [2] }];
  for (const tab of ['home', 'plan']) for (const ask of asks) for (const output of [null, { label: 'x', text: 'y' }]) {
    for (const confirm of [null, 'plan-done']) {
      const keys = hotkeys(buildPanel({ snap: snap(), owned: !!ask, ask, ui: { tab, confirm }, output }));
      assert.equal(new Set(keys).size, keys.length, `${tab} ${JSON.stringify(ask)} ${keys}`);
      assert.ok(keys.every((k) => /^[a-z0-9]$/.test(k)));
    }
  }
});

test('barText, kFmt, fitSteps, panelText', () => {
  assert.equal(barText(5, 10, 10), '█████░░░░░');
  assert.equal(barText(50, 10, 6), '██████');
  assert.equal(barText(-1, 10, 4), '░░░░');
  assert.equal(barText(1, 0, 4), '░░░░');
  assert.equal(kFmt(46000), '46k');
  assert.equal(kFmt(950), '950');
  const steps = lifecycle(snap());
  assert.equal(fitSteps(steps, 200).length, 1);
  const narrow = fitSteps(steps, 24);
  assert.ok(narrow.length > 1);
  assert.deepEqual(narrow.flat(), steps);
  const text = panelText(home({ usage: usage(46000), output: { label: 'Stats', text: 'hello' } }));
  assert.ok(Array.isArray(text));
  const joined = text.join('\n');
  for (const s of ['[Home]', 'nxy ● ready', 'Contexto: 46k / 100k', 'gate on', '[e] Terminar plan', 'hello']) assert.ok(joined.includes(s), s);
});

test('panel.mjs stays pure: no node: imports, only orchestrator and batch-status', () => {
  const src = readFileSync(fileURLToPath(new URL('../core/panel.mjs', import.meta.url)), 'utf8');
  assert.ok(!/from\s+['"]node:/.test(src));
  const imports = [...src.matchAll(/from\s+'([^']+)'/g)].map((m) => m[1]);
  assert.deepEqual(imports.sort(), ['./batch-status.mjs', './orchestrator.mjs']);
});
