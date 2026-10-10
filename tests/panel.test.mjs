// @ts-nocheck — the view model is plain data; the tests poke into it freely
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import {
  TABS, LAUNCHER, BUILDERS, NEEDS, launcherOf, launcherAvailable, filterCtx, isPanelAction, buildPanel, panelText, barText, kFmt,
  fitSteps, lifecycle, clipOutput, layoutOf, lifecycleLayout,
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
const plan = (o) => buildPanel({ snap: snap(), now: NOW, ui: { tab: 'plan' }, ...o });
const block = (v, type) => v.blocks.find((b) => b.type === type);
const tile = (v, id) => block(v, 'tiles').tiles.find((t) => t.id === id);
const actionIds = (v) => v.blocks.filter((b) => b.type === 'actions').flatMap((b) => b.actions.map((a) => a.id));
const hotkeys = (v) => [
  ...v.tabs.map((t) => t.hotkey), ...v.keys.map((k) => k.hotkey),
  ...v.blocks.filter((b) => b.type === 'actions').flatMap((b) => b.actions.map((a) => a.hotkey)),
  ...v.blocks.filter((b) => b.type === 'rows').flatMap((b) => b.rows.flatMap((r) => r.cells.filter((c) => c.hotkey).map((c) => c.hotkey))),
  ...v.blocks.filter((b) => b.type === 'checkpoint' && b.fix).map((b) => b.fix.hotkey),
  ...v.blocks.filter((b) => b.type === 'agent').map((b) => b.back.hotkey),
  ...(v.ask?.buttons ?? []).map((b) => b.hotkey), ...(v.output ? [v.output.dismiss.hotkey] : []),
];

test('TABS: six tabs, a builder each, active tab', () => {
  assert.deepEqual(TABS.map((t) => t.id), ['home', 'plan', 'agents', 'stats', 'config', 'memory']);
  assert.deepEqual(TABS.map((t) => t.hotkey), ['1', '2', '3', '4', '5', '6']);
  assert.equal(TABS[0].label, 'Inicio');
  for (const t of TABS) assert.equal(typeof BUILDERS[t.id], 'function');
  assert.deepEqual(home({}).tabs.map((t) => t.active), [true, false, false, false, false, false]);
  assert.deepEqual(home({ ui: { tab: 'plan' } }).tabs.map((t) => t.active), [false, true, false, false, false, false]);
  assert.equal(home({ ui: { tab: 'nope' } }).tabs[0].active, true);
  assert.equal(home({}).sections, undefined);
});

test('Home with no snapshot: reads as not read yet, still has buttons', () => {
  const v = buildPanel({ snap: null, now: NOW });
  assert.equal(block(v, 'hero').planValue, 'sin leer');
  assert.equal(tile(v, 'context').value, 'desconocido');
  assert.ok(actionIds(v).includes('gate-once'));
  assert.deepEqual(v.header.pill, { glyph: '●', text: 'ready', tone: 'ok' });
  assert.equal(v.animated, false);
});

test('Home hero: lote N de M, state, lifecycle, meta line', () => {
  const v = home({});
  const hero = block(v, 'hero');
  assert.equal(hero.planValue, 'abc12345 · aprobado');
  assert.equal(hero.headline, 'Lote 1 de 2 · First');
  assert.equal(hero.meta, 'plan abc12345 · aprobado');
  assert.equal(home({ snap: snap({ approved: false }) }).blocks[0].planValue, 'abc12345 · sin aprobar');
  const none = block(home({ snap: snap({ hash: null, batches: [] }) }), 'hero');
  assert.equal(none.planValue, 'ninguno');
  assert.equal(none.headline, 'Sin plan en esta rama');
  assert.deepEqual(hero.steps.map((s) => s.label), ['plan', 'aprobado', 'lotes 0/2', 'tester', 'review', 'cierre']);
  assert.deepEqual(hero.steps.map((s) => s.state), ['done', 'done', 'current', 'pending', 'pending', 'pending']);
  const run = block(home({ snap: snap({ launched: { 1: NOW - 65000 } }) }), 'hero');
  assert.equal(run.batch.n, 1);
  assert.equal(run.elapsedMs, 65000);
});

test('lifecycle: red batch, red suite, all done', () => {
  assert.equal(lifecycle(snap({ verdicts: { 1: fail() }, launched: { 1: 5 } }))[2].state, 'red');
  const all = { verdicts: { 1: pass(), 2: pass() }, launched: { 1: 5, 2: 5 } };
  assert.equal(lifecycle(snap({ ...all, suite: { status: 'done', red: [{}] } }))[3].state, 'red');
  const done = lifecycle(snap({ ...all, suite: { status: 'done', red: [] }, review: { id: 'r' } }));
  assert.ok(done.every((s) => s.state === 'done'));
  assert.deepEqual(lifecycle(null), []);
});

test('Context tile: gate on bar and thresholds, gate off, unknown', () => {
  const on = tile(home({ usage: usage(46000) }), 'context');
  assert.deepEqual(on.bar, { used: 46000, limit: 100000, tone: 'ok' });
  assert.equal(on.value, '46k / 100k');
  assert.deepEqual(on.hints, ['gate on', '54k libres']);
  assert.equal(tile(home({ usage: usage(70000) }), 'context').bar.tone, 'running');
  assert.equal(tile(home({ usage: usage(69000) }), 'context').bar.tone, 'ok');
  assert.equal(tile(home({ usage: usage(90000) }), 'context').bar.tone, 'error');
  const off = tile(home({ snap: snap({ config: { gate: { enabled: false }, filter: false } }), usage: usage(46000) }), 'context');
  assert.equal(off.bar.limit, 1000000);
  assert.deepEqual(off.hints, ['gate off']);
  assert.equal(tile(home({}), 'context').value, 'desconocido');
  const fb = tile(buildPanel({ snap: null, usage: usage(1500), fallback: { gate: { enabled: true, contextTokens: 5000 } } }), 'context');
  assert.equal(fb.bar.limit, 5000);
});

test('Cost tile: session cost with the model as hint', () => {
  const c = tile(home({ usage: usage(1) }), 'cost');
  assert.deepEqual([c.value, c.hints], ['$1.23', ['sonnet']]);
  assert.equal(tile(home({}), 'cost').value, 'desconocido');
});

test('Cache tile: unknown until a cache is passed, then live', () => {
  const unknown = tile(home({}), 'cache');
  assert.deepEqual([unknown.known, unknown.value], [false, 'desconocido']);
  const live = tile(home({ cache: { hitPct: 87.4, ttlLeftMs: 120000, ttlMs: 300000, coldCostUsd: 0.42 } }), 'cache');
  assert.equal(live.known, true);
  assert.equal(live.value, '87%');
  assert.deepEqual(live.bar, { used: 87.4, limit: 100, tone: 'ok' });
  assert.ok(live.hints.includes('en frío: $0.42'));
});

test('Handoff in the hero: none, fresh, stale', () => {
  assert.deepEqual(block(home({}), 'hero').handoff, { value: 'ninguno', tone: 'dim' });
  const fresh = block(home({ snap: snap({ handoff: { updated: NOW - 12 * 60000 }, verdicts: { 1: pass(NOW - 30 * 60000) } }) }), 'hero');
  assert.deepEqual(fresh.handoff, { value: 'hace 12 min', tone: 'info' });
  assert.ok(fresh.meta.endsWith('handoff hace 12 min'));
  const stale = block(home({ snap: snap({ handoff: { updated: NOW - 12 * 60000 }, verdicts: { 1: pass(NOW - 5 * 60000) } }) }), 'hero');
  assert.deepEqual(stale.handoff, { value: 'hace 12 min · desactualizado', tone: 'error' });
});

test('Ahora: one line per agent in flight, the rest as next, empty when idle', () => {
  const idle = block(home({}), 'now');
  assert.deepEqual(idle.agents, []);
  assert.equal(idle.next, 'después: lotes 1–2, la suite y la review');
  const s = snap({ batches: [1, 2, 3, 4].map((n) => ({ n, depends: [], title: `B${n}` })), launched: { 1: NOW - 30000 }, verdicts: {} });
  const v = buildPanel({ snap: s, now: NOW, inFlight: { a1: { role: 'implementer', batch: 1 } } });
  const now = block(v, 'now');
  assert.equal(now.agents.length, 1);
  assert.deepEqual([now.agents[0].role, now.agents[0].batch, now.agents[0].elapsedMs, now.agents[0].live], ['implementer', 1, 30000, true]);
  assert.equal(now.next, 'después: lotes 2–4, la suite y la review');
  assert.equal(v.animated, true);
  assert.ok(panelText(v).some((l) => l.includes('implementer') && l.includes('lote 1')));
  assert.equal(buildPanel({ snap: s, now: NOW, inFlight: [] }).animated, false);
});

test('header: repo, branch, progress per batch', () => {
  const s = snap({ projectDir: 'C:\\work\\my-repo\\', branch: 'feature/x', verdicts: { 1: pass() }, launched: { 1: 5, 2: 20 } });
  assert.deepEqual([buildPanel({ snap: s, now: NOW }).header.repo, buildPanel({ snap: s, now: NOW }).header.branch], ['my-repo', 'feature/x']);
  assert.equal(buildPanel({ snap: snap({ projectDir: '/home/u/repo' }), now: NOW }).header.repo, 'repo');
  assert.deepEqual(buildPanel({ snap: s, now: NOW }).header.progress, ['done', 'running']);
  assert.deepEqual(buildPanel({ snap: null, now: NOW }).header.progress, []);
});

test('plan-done lives on the Plan tab: hidden with no plan or when owned; filter label by state', () => {
  assert.ok(actionIds(plan({})).includes('plan-done'));
  assert.ok(!actionIds(home({})).includes('plan-done'));
  assert.ok(!actionIds(plan({ snap: snap({ hash: null, batches: [] }) })).includes('plan-done'));
  assert.ok(actionIds(plan({ snap: snap({ hash: null, batches: [], handoff: { updated: 1 } }) })).includes('plan-done'));
  assert.ok(!actionIds(plan({ owned: true })).includes('plan-done'));
  const label = (v) => v.blocks.find((b) => b.type === 'actions').actions.find((a) => a.id === 'cfg:modules.filter:toggle').label;
  assert.equal(label(home({})), 'Filter ○ off');
  assert.equal(label(home({ snap: snap({ config: { filter: true } }) })), 'Filter ● on');
});

test('filter label: a plain Filter until a snapshot supplies config.filter', () => {
  const label = (v) => v.blocks.find((b) => b.type === 'actions').actions.find((a) => a.id === 'cfg:modules.filter:toggle').label;
  assert.equal(label(buildPanel({ snap: null, now: NOW })), 'Filter');
  assert.equal(label(home({ snap: snap({ config: { gate: { enabled: false } } }) })), 'Filter');
  assert.equal(label(home({ snap: snap({ config: { filter: false } }) })), 'Filter ○ off');
  assert.deepEqual(filterCtx(null), {});
  assert.deepEqual(filterCtx({ config: { filter: true } }), { filter: true });
  // the Home Filter button is a config action (honours «Guardar en»), not a launcher script
  assert.equal(launcherOf('filter'), null);
  const f = home({}).blocks.find((b) => b.type === 'actions').actions.find((a) => a.id === 'cfg:modules.filter:toggle');
  assert.equal(f.hotkey, 'f');
});

test('needs: every launcher need is a known check; the tab filters read it', () => {
  for (const l of LAUNCHER) if (l.needs) assert.equal(typeof NEEDS[l.needs], 'function', l.id);
  const done = launcherOf('plan-done');
  assert.equal(done.tab, 'plan');
  assert.equal(launcherOf('trend'), null);
  assert.equal(launcherOf('stats'), null);
  assert.equal(launcherAvailable(done, { snap: snap() }), true);
  assert.equal(launcherAvailable(done, { snap: snap(), owned: true }), false);
  assert.equal(launcherAvailable(done, { snap: null }), false);
  assert.equal(launcherAvailable(done, { snap: snap({ hash: null, handoff: { updated: 1 } }) }), true);
  assert.equal(launcherAvailable(launcherOf('gate-once'), { snap: null, owned: true }), true);
  const ids = actionIds(buildPanel({ snap: snap(), owned: true, now: NOW }));
  assert.deepEqual(ids, [...LAUNCHER.filter((l) => l.tab === 'home' && !l.needs).map((l) => l.id), 'cfg:modules.filter:toggle']);
});

test('launcherOf and isPanelAction', () => {
  assert.deepEqual(launcherOf('gate-once'), { ...LAUNCHER[0], args: ['once'] });
  const done = launcherOf('plan-done');
  assert.ok(done.confirm && done.needs === 'plan' && done.after === 'refresh');
  assert.equal(launcherOf('nope'), null);
  for (const id of ['refresh', 'dismiss', 'tab:plan', 'confirm-yes', 'confirm-no', 'batch:2', 'pick:R1', 'fix-picked',
    'metric:usd', 'metric:rtk', 'range:7d', 'range:30d', 'by:day', 'by:week', 'by:model', 'here', 'summary-hide']) {
    assert.equal(isPanelAction(id), true, id);
  }
  for (const id of ['stats', 'trend', 'metric:nope', 'range:1d', 'by:year']) assert.equal(isPanelAction(id), false, id);
  for (const id of ['Retry', 'batch:x', 'pick:', 'batch']) assert.equal(isPanelAction(id), false, id);
});

test('Plan tab: batches with titles and states, suite and review rows', () => {
  const v = plan({ snap: snap({ verdicts: { 1: pass(), 2: fail() }, launched: { 1: 5, 2: 6 }, continued: [2] }), owned: true });
  const batches = block(v, 'batches').items;
  assert.deepEqual(batches.map((b) => [b.n, b.title, b.state]), [[1, 'First', 'done'], [2, 'Second', 'red']]);
  assert.equal(batches[1].note, 'failed (continued anyway)');
  assert.equal(block(v, 'suite').rows[0].value, 'pendiente');
  const run = plan({ snap: snap({ launched: { 1: 5 } }), owned: true });
  assert.equal(block(run, 'batches').items[0].state, 'running');
  assert.equal(block(run, 'batches').items[1].state, 'pending');
  const empty = plan({ snap: snap({ hash: null, batches: [] }) });
  assert.deepEqual(empty.blocks[0], { type: 'note', text: 'Sin plan en esta rama', tone: 'dim' });
});

test('Plan tab: goal, files, accept, verdict, open by default on the running batch', () => {
  const s = snap({
    goal: 'Ship the thing',
    batches: [
      { n: 1, depends: [], title: 'First', files: ['a.mjs', 'b.mjs'], accept: { kind: 'command', command: 'node --test a' } },
      { n: 2, depends: [1], title: 'Second', files: [], accept: { kind: 'manual', note: 'mirar el panel' } },
    ],
    verdicts: { 1: pass(5000) }, launched: { 1: 1000, 2: NOW - 9000 },
  });
  const v = plan({ snap: s, owned: true });
  assert.equal(block(v, 'goal').text, 'Ship the thing');
  const [b1, b2] = block(v, 'batches').items;
  assert.deepEqual([b1.open, b2.open], [false, true]);
  assert.equal(b1.durationMs, 4000);
  assert.equal(b1.accept, 'node --test a');
  assert.deepEqual(b1.files, ['a.mjs', 'b.mjs']);
  assert.equal(b1.verdict.text, '✔ verificado por nxy');
  assert.equal(b2.accept, 'manual — mirar el panel');
  assert.equal(b2.verdict.text, 'implementer trabajando');
  // ui.expanded wins over the default
  const ex = block(plan({ snap: s, owned: true, ui: { tab: 'plan', expanded: 1 } }), 'batches').items;
  assert.deepEqual(ex.map((b) => b.open), [true, false]);
  // -1 = all closed, even the running batch
  const none = block(plan({ snap: s, owned: true, ui: { tab: 'plan', expanded: -1 } }), 'batches').items;
  assert.deepEqual(none.map((b) => b.open), [false, false]);
  // red shows the verdict error, pending waits
  const red = block(plan({ snap: snap({ verdicts: { 1: fail() }, launched: { 1: 5 } }), owned: true }), 'batches').items;
  assert.deepEqual([red[0].verdict.text, red[0].verdict.tone], ['boom', 'error']);
  assert.equal(red[1].verdict.text, 'espera al lote anterior');
  assert.ok(panelText(v).some((l) => l.includes('accept: manual — mirar el panel')));
});

test('Plan tab: finding loc shows just the file when line is null, 0 or missing', () => {
  const reviewDetail = { id: 'rv1', ts: 1, chosen: [], findings: [
    { id: 'R1', file: 'a.mjs', line: null, title: 'x' },
    { id: 'R2', file: 'b.mjs', line: 0, title: 'y' },
    { id: 'R3', file: 'c.mjs', title: 'z' },
    { id: 'R4', file: 'd.mjs', line: 7, title: 'w' },
  ] };
  const cp = block(plan({ snap: snap({ review: { id: 'rv1' }, reviewDetail }) }), 'checkpoint');
  assert.deepEqual(cp.findings.map((f) => f.loc), ['a.mjs', 'b.mjs', 'c.mjs', 'd.mjs:7']);
});

test('Plan tab: checkpoint 2 with picked findings and the Arreglar button', () => {
  const reviewDetail = { id: 'rv1', ts: 1, chosen: [], findings: [
    { id: 'R1', severity: 'high', lens: 'logic', file: 'a.mjs', line: 3, title: 'Bug', cause: 'why' },
    { id: 'R2', severity: 'low', lens: 'style', file: 'b.mjs', line: null, title: 'Nit', cause: 'because' },
  ] };
  const s = snap({ review: { id: 'rv1' }, reviewDetail });
  const none = block(plan({ snap: s }), 'checkpoint');
  assert.equal(none.picked, 0);
  assert.equal(none.fix, null);
  assert.equal(none.findings[0].loc, 'a.mjs:3');
  assert.equal(none.findings[1].loc, 'b.mjs');
  const one = block(plan({ snap: s, ui: { tab: 'plan', picked: ['R2'] } }), 'checkpoint');
  assert.deepEqual(one.findings.map((f) => f.picked), [false, true]);
  assert.deepEqual(one.fix, { id: 'fix-picked', label: 'Arreglar 1', hotkey: 'z' });
  assert.equal(block(plan({ snap: s, owned: true, ui: { tab: 'plan', picked: ['R1'] } }), 'checkpoint').fix, null);
  assert.equal(block(plan({ snap: snap({ reviewDetail: null }) }), 'checkpoint'), undefined);
  assert.ok(actionIds(plan({ snap: s })).includes('plan-done'));
});

test('Cache tile: cold shows the rewrite cost, warm the countdown, estimated marks *', () => {
  const cold = tile(home({ cache: { hitPct: 90, ttlLeftMs: 0, ttlMs: 300000, coldCostUsd: 3.86, coldTokens: 386000 } }), 'cache');
  assert.deepEqual([cold.value, cold.bar], ['❄ fría', null]);
  assert.deepEqual(cold.hints, ['el próximo mensaje reescribe 386k ≈ $3.86']);
  const est = tile(home({ cache: { hitPct: 90, ttlLeftMs: -5, ttlMs: 300000, coldCostUsd: 3.86, coldTokens: 386000, estimated: true } }), 'cache');
  assert.ok(est.hints[0].endsWith('$3.86*'));
  const warm = tile(home({ cache: { hitPct: 80, ttlLeftMs: 185000, ttlMs: 300000, coldCostUsd: 1 } }), 'cache');
  assert.ok(warm.hints.includes('vence en 3m 05s'));
});

test('clockTicks: only on Inicio with a warm cache', () => {
  const warm = { hitPct: 80, ttlLeftMs: 60000, ttlMs: 300000, coldCostUsd: 1 };
  assert.equal(home({ cache: warm }).clockTicks, true);
  assert.equal(home({ cache: { ...warm, ttlLeftMs: 0 } }).clockTicks, false);
  assert.equal(home({}).clockTicks, false);
  assert.equal(buildPanel({ snap: snap(), now: NOW, ui: { tab: 'stats' }, cache: warm }).clockTicks, false);
});

test('Home segments: only what has data', () => {
  assert.equal(block(home({}), 'segments'), undefined);
  const v = home({ usage: usage(50000), live: { model: 'claude-sonnet-4-6', effort: 'high', turnUsd: 0.12 } });
  const seg = block(v, 'segments').items;
  assert.equal(seg.find((i) => i.id === 'model').value, 'sonnet 4.6 · high');
  assert.ok(seg.some((i) => i.id === 'turn') && seg.some((i) => i.id === 'session'));
  assert.ok(panelText(v).some((l) => l.includes('modelo sonnet 4.6')));
});

test('Plan tab: batch duration uses the verdict launched time first', () => {
  const s = snap({ verdicts: { 1: { ...pass(9000), launched: 4000 } }, launched: { 1: 1000 } });
  assert.equal(block(plan({ snap: s, owned: true }), 'batches').items[0].durationMs, 5000);
  const manual = snap({ verdicts: { 1: { ...pass(9000), launched: 4000 } }, launched: {} });
  assert.equal(block(plan({ snap: manual }), 'batches').items[0].durationMs, 5000);
});

const STATS = {
  usage: { usd: 2.5, totalInput: 300000, usage: { output: 9000 }, cacheHitPct: 92, calls: 10 },
  main: { usd: 2, calls: 8 }, byAgentType: { 'nxy:implementer': { usd: 0.5, calls: 2 } },
  byModel: { 'claude-sonnet-4-6': { usd: 2.5, calls: 10 } }, cacheBreaks: [], agents: [],
};
const TREND = {
  by: 'day', total: { usd: 3 },
  periods: [{ period: '2026-10-01', usd: 1, totalTokens: 100 }, { period: '2026-10-02', usd: 2, totalTokens: 200 }],
};
const stats = (o) => buildPanel({ snap: snap(), now: NOW, ui: { tab: 'stats' }, usage: usage(1), ...o });

test('Stats tab: data, loading, error, empty', () => {
  const ok = stats({ stats: { state: 'ok', data: STATS, at: NOW - 12000 }, trend: { state: 'ok', data: TREND } });
  const types = ok.blocks.map((b) => b.type);
  for (const t of ['figures', 'bars', 'selectors', 'chart', 'note']) assert.ok(types.includes(t), t);
  assert.deepEqual(ok.blocks.filter((b) => b.type === 'bars').map((b) => b.title), ['Costo por rol', 'Por modelo']);
  assert.equal(block(ok, 'chart').chart.kind, 'columns');
  const text = panelText(ok).join('\n');
  assert.ok(text.includes('actualizado hace 12s · r actualiza'));
  assert.ok(text.includes('sin suscripción'));
  const lim = stats({ usage: { ...usage(1), limits: [{ kind: 'five_hour', percentUsed: 40, resetsAt: NOW + 60000 }] } });
  assert.equal(block(lim, 'windows').rows[0].label, '5 h');
  assert.ok(panelText(stats({ stats: { state: 'loading' }, trend: { state: 'loading' } })).join('\n').includes('cargando…'));
  assert.ok(panelText(stats({ stats: { state: 'error', error: 'boom' } })).join('\n').includes('error: boom'));
  const err = stats({ trend: { state: 'error', error: 'x' } });
  assert.ok(err.blocks.some((b) => b.type === 'note' && b.tone === 'error'));
  assert.ok(stats({ stats: { state: 'empty' }, trend: { state: 'empty' } }).blocks.length > 0);
});

test('Stats selectors mark the active choice; by:model draws rows', () => {
  const sel = (v) => block(v, 'selectors').groups;
  const def = sel(stats({}));
  assert.deepEqual(def.map((g) => g.id), ['metric', 'range', 'by', 'here']);
  assert.equal(def[0].options.find((o) => o.active).id, 'metric:usd');
  assert.equal(def[1].options.find((o) => o.active).id, 'range:7d');
  const v = stats({ ui: { tab: 'stats', trendSel: { metric: 'tok', range: '30d', by: 'model', here: true } }, trend: { state: 'ok', data: { by: 'model', models: [{ model: 'claude-sonnet-4-6', usd: 2, total: 500 }] } } });
  const g = sel(v);
  assert.deepEqual([g[0].options.find((o) => o.active).id, g[1].options.find((o) => o.active).id, g[2].options.find((o) => o.active).id, g[3].options[0].active], ['metric:tok', 'range:30d', 'by:model', true]);
  assert.equal(block(v, 'chart').chart.kind, 'rows');
});

test('Stats summary block carries the hide button', () => {
  const v = stats({ summary: { feature: STATS, session: STATS, snap: snap({ verdicts: { 1: { ...pass(9000), launched: 4000 } } }) } });
  const s = block(v, 'summary');
  assert.equal(s.hide.id, 'summary-hide');
  assert.equal(s.batches[0].ms, 5000);
});

test('Agents, Stats and Config skeletons build with and without a snapshot', () => {
  for (const tab of ['agents', 'stats', 'config']) {
    for (const sn of [null, snap()]) {
      const v = buildPanel({ snap: sn, now: NOW, ui: { tab }, usage: usage(1) });
      assert.ok(v.blocks.length > 0, tab);
      assert.ok(panelText(v).length > 2, tab);
    }
  }
  const ag = buildPanel({ snap: snap({ launched: { 2: NOW - 5000 } }), now: NOW, ui: { tab: 'agents' }, inFlight: { x: { role: 'implementer', batch: 2 } } });
  assert.deepEqual(block(ag, 'agents').agents.map((a) => [a.role, a.batch, a.elapsedMs]), [['implementer', 2, 5000]]);
  assert.ok(!ag.blocks.some((b) => b.type === 'note' && b.text.includes('fase 3')));
  const st = buildPanel({ snap: snap(), now: NOW, ui: { tab: 'stats' }, usage: usage(1) });
  assert.deepEqual(actionIds(st), []);
  assert.ok(block(st, 'selectors'));
  const cf = buildPanel({ snap: snap(), now: NOW, ui: { tab: 'config' } });
  assert.ok(!cf.blocks.some((b) => b.type === 'kv' || (b.type === 'note' && b.text.includes('fase 4'))));
});

const cfgSnap = (over = {}) => snap({
  config: {
    gate: { enabled: true, contextTokens: 100000 }, filter: false, orchestrator: 'auto', pauseAfterBatch: false, ui: { panel: 'auto' },
    roles: { implementer: { model: 'sonnet', effort: 'high' }, planner: { model: null, effort: null } },
    scopes: { user: { path: 'u', exists: true, broken: false }, repo: { path: 'r', exists: true, broken: false } },
    sources: { 'roles.implementer.model': 'repo', 'roles.implementer.effort': 'default', 'gate.enabled': 'user' },
    ...over,
  },
});
const cfgView = (o = {}) => buildPanel({ snap: cfgSnap(), now: NOW, ui: { tab: 'config' }, ...o });
const rowsOf = (v, title) => v.blocks.find((b) => b.type === 'rows' && b.title === title);
const allCells = (v) => v.blocks.filter((b) => b.type === 'rows').flatMap((b) => b.rows.flatMap((r) => r.cells));

test('Config tab: sections in order, scope defaults to user, with and without snapshot', () => {
  assert.deepEqual(TABS.map((t) => t.id), ['home', 'plan', 'agents', 'stats', 'config', 'memory']);
  const v = cfgView();
  assert.deepEqual(v.blocks.filter((b) => b.type === 'rows').map((b) => b.title),
    ['Roles', 'Flujo', 'Gate', 'Cache', 'Interfaz', 'Statusline', 'Herramientas', 'nxy']);
  const sel = block(v, 'selectors');
  assert.equal(sel.title, 'Guardar en');
  assert.deepEqual(sel.groups[0].options.map((o) => [o.id, o.active]), [['scope:user', true], ['scope:repo', false]]);
  assert.ok(v.blocks.some((b) => b.type === 'note' && b.text.startsWith('~/.nxy/config.json')));
  const r = cfgView({ ui: { tab: 'config', scope: 'repo' } });
  assert.ok(r.blocks.some((b) => b.type === 'note' && b.text.startsWith('.nxy/config.json')));
  assert.equal(block(r, 'selectors').groups[0].options[1].active, true);
  // roles: one row per role, a cycler on each cell
  const roles = rowsOf(v, 'Roles');
  assert.equal(roles.rows.length, 7);
  const impl = roles.rows.find((x) => x.label === 'implementer');
  assert.deepEqual(impl.cells.map((c) => [c.text, c.prev, c.next]), [
    ['sonnet', 'cfg:roles.implementer.model:prev', 'cfg:roles.implementer.model:next'],
    ['high', 'cfg:roles.implementer.effort:prev', 'cfg:roles.implementer.effort:next'],
  ]);
  assert.equal(roles.rows.find((x) => x.label === 'planner').cells[0].text, 'predeterminado');
  // no snapshot: unknown, nothing writes
  const none = buildPanel({ snap: null, now: NOW, ui: { tab: 'config' } });
  assert.ok(allCells(none).every((c) => !c.prev && !c.press));
  assert.ok(allCells(none).some((c) => c.text === 'desconocido' && c.tone === 'dim'));
});

test('NXY_FILTER forces the filter: hint on the Config row and next to the Home button', () => {
  const env = { sources: { 'modules.filter': 'env' }, filter: true };
  const cfgV = cfgView({ snap: cfgSnap(env) });
  const row = rowsOf(cfgV, 'Interfaz').rows.find((x) => x.label === 'Filtro');
  assert.deepEqual([row.hint.text, row.hint.tone], ['forzado por NXY_FILTER', 'warn']);
  const home = buildPanel({ snap: cfgSnap(env), now: NOW, ui: { tab: 'home' } });
  assert.ok(home.blocks.some((b) => b.type === 'note' && b.text.includes('forzado por NXY_FILTER') && b.tone === 'warn'));
  const plain = buildPanel({ snap: cfgSnap(), now: NOW, ui: { tab: 'home' } });
  assert.ok(!plain.blocks.some((b) => b.type === 'note' && b.text.includes('NXY_FILTER')));
});

test('Home filter note: «el repo manda» when saving to user and the repo defines it; env wins', () => {
  const has = (snap, scope) => buildPanel({ snap, now: NOW, ui: { tab: 'home', scope } })
    .blocks.some((b) => b.type === 'note' && b.text.includes('el repo manda') && b.tone === 'warn');
  const repo = cfgSnap({ sources: { 'modules.filter': 'repo' } });
  assert.equal(has(repo, undefined), true);
  assert.equal(has(repo, 'user'), true);
  assert.equal(has(repo, 'repo'), false);
  assert.equal(has(cfgSnap({ sources: { 'modules.filter': 'env' } }), 'user'), false);
});

test('Config tab: «el repo manda» hint, labelWidth, orchestrator disabled while owned, broken file note', () => {
  const hint = (v) => rowsOf(v, 'Roles').rows.filter((x) => x.hint).map((x) => [x.label, x.hint.text, x.hint.tone]);
  assert.deepEqual(hint(cfgView()), [['implementer', 'el repo manda', 'warn']]);
  assert.deepEqual(hint(cfgView({ ui: { tab: 'config', scope: 'repo' } })), []);
  for (const b of cfgView().blocks.filter((x) => x.type === 'rows')) {
    assert.ok(b.labelWidth >= Math.max(...b.rows.map((x) => x.label.length)) + 1, b.title);
  }
  const flow = rowsOf(cfgView(), 'Flujo').rows[0];
  assert.equal(flow.cells[0].press, 'cfg:flow.orchestrator:toggle');
  const owned = rowsOf(cfgView({ owned: true }), 'Flujo').rows[0];
  assert.equal(owned.cells[0].press, undefined);
  assert.equal(owned.cells[0].disabled, true);
  assert.equal(owned.hint.text, 'corriendo un plan');
  const broken = cfgView({ snap: cfgSnap({ scopes: { user: { path: 'u', exists: true, broken: true }, repo: { path: 'r', exists: false, broken: false } } }) });
  assert.ok(broken.blocks.some((b) => b.type === 'note' && b.tone === 'error' && b.text.includes('roto')));
  assert.ok(cfgView({ cfgNote: 'no se pudo' }).blocks.some((b) => b.type === 'note' && b.text === 'no se pudo' && b.tone === 'error'));
});

test('Config tab: launcher buttons follow settings; cache what-if and tools', () => {
  const cell = (v, id) => allCells(v).find((c) => c.press === id);
  assert.equal(cell(cfgView(), 'cache-ttl').text, 'Cache 1 h');
  assert.equal(cell(cfgView({ settings: { promptCacheTtl: '1h' } }), 'cache-ttl').text, 'Cache 1 h ● on');
  assert.equal(cell(cfgView({ settings: {} }), 'cache-ttl').text, 'Cache 1 h ○ off');
  assert.equal(cell(cfgView({ settings: { statusLine: { command: 'node "/h/.nxy/statusline.mjs"' } } }), 'statusline').text, 'Quitar');
  assert.equal(cell(cfgView({ settings: { statusLine: { command: 'other' } } }), 'statusline').text, 'Instalar');
  assert.equal(cell(cfgView(), 'update-nxy').text, 'Actualizar');
  assert.deepEqual(['cache-ttl', 'statusline', 'update-nxy'].map((id) => launcherOf(id).hotkey), ['t', 'l', 'u']);
  assert.deepEqual(launcherOf('cache-ttl', { ttl1h: true }).args, ['cache-ttl', 'off']);
  assert.deepEqual(launcherOf('cache-ttl', { ttl1h: false }).args, ['cache-ttl', '1h']);
  assert.deepEqual(launcherOf('statusline', { statusline: 'nxy' }).args, ['statusline', 'remove']);
  assert.deepEqual(launcherOf('statusline', { statusline: 'none' }).args, ['statusline', 'install']);
  assert.deepEqual(launcherOf('update-nxy').args, ['update']);
  for (const id of ['cache-ttl', 'statusline', 'update-nxy']) {
    const e = launcherOf(id);
    assert.equal(e.tab, 'config'); assert.equal(e.script, 'config'); assert.ok(e.confirm);
  }
  assert.ok(!actionIds(cfgView()).length);
  const tools = rowsOf(cfgView({ cfgInfo: { version: '1.2.3', tools: { rtk: { found: true, path: '/x/rtk', version: '0.1' }, rg: { found: false } } } }), 'Herramientas');
  assert.deepEqual(tools.rows.map((x) => x.cells[0].text), ['encontrada 0.1', 'no encontrada', 'no encontrada']);
  assert.equal(tools.rows[0].hint.text, '/x/rtk');
  assert.ok(allCells(cfgView({ cfgInfo: { version: '1.2.3', tools: {} } })).some((c) => c.text === '1.2.3'));
  assert.ok(rowsOf(cfgView(), 'Cache').rows.every((x) => x.cells.every((c) => c.text)));
  // A failed tools lookup is unknown, not "not found".
  const failed = cfgView({ cfgInfo: { version: null, tools: {}, failed: true } });
  const ft = rowsOf(failed, 'Herramientas');
  assert.ok(ft.rows.every((x) => x.cells[0].tone === 'dim' && x.cells[0].text !== 'no encontrada'));
  assert.ok(!allCells(failed).some((c) => c.text === 'no encontrada'));
});

test('cfg: and scope: actions; panelText of rows; hotkeys in Config', () => {
  for (const id of ['scope:user', 'scope:repo', 'cfg:roles.tester.model:next', 'cfg:gate.contextTokens:prev', 'cfg:modules.filter:toggle']) {
    assert.equal(isPanelAction(id), true, id);
  }
  for (const id of ['scope:team', 'cfg:nope:next', 'cfg:gate.enabled:sideways', 'cfg:roles.x.model:next', 'cfg::toggle', 'cfg:gate.enabled']) {
    assert.equal(isPanelAction(id), false, id);
  }
  assert.ok(['cache-ttl', 'statusline', 'update-nxy'].every(isPanelAction));
  const text = panelText(cfgView({ settings: { promptCacheTtl: '1h' } })).join('\n');
  assert.ok(text.includes('‹ sonnet ›'));
  assert.ok(text.includes('● on') || text.includes('Cache 1 h ● on'));
  assert.ok(text.includes('(el repo manda)'));
  const keys = hotkeys(cfgView({ settings: {} }));
  assert.equal(new Set(keys).size, keys.length);
  assert.ok(['t', 'l', 'u'].every((k) => keys.includes(k)));
});

const row = (id, over = {}) => ({
  id, role: 'implementer', nxy: true, label: `tarea ${id}`, batch: null, state: 'corriendo', tone: 'running', live: true,
  model: 'sonnet', effort: 'medium', modelEffort: 'sonnet · medium', activity: 'Edit core/panel.mjs · hace 2s',
  elapsedMs: 12000, costText: '…', tokensText: '', ...over,
});
const detail = (over = {}) => ({
  card: row('a1'), prompt: 'haz esto', tools: [{ label: 'Edit x', ago: 3 }], result: '', error: '', batch: 2,
  sent: [{ text: 'hola', state: 'en cola', reason: '' }, { text: 'chau', state: 'no entregado', reason: 'sin sesión' }],
  canSend: true, confirm: false, ...over,
});

test('Agentes: lista con cuenta, tope de 10 y Ver todos', () => {
  const rows = [row('a1'), row('a2', { live: false, tone: 'ok', state: 'terminó' }), row('a3', { live: false, tone: 'error', state: 'falló' })];
  const v = buildPanel({ snap: snap(), now: NOW, ui: { tab: 'agents' }, agents: rows });
  const b = block(v, 'agents');
  assert.deepEqual(b.counts, { running: 1, done: 1, failed: 1, stopped: 0 });
  assert.equal(b.more, null);
  assert.equal(v.animated, true);
  assert.ok(panelText(v).some((l) => l.includes('implementer') && l.includes('sonnet · medium')));
  const many = Array.from({ length: 13 }, (_, i) => row(`m${i}`, { live: false, tone: 'ok' }));
  const big = buildPanel({ snap: snap(), now: NOW, ui: { tab: 'agents' }, agents: many });
  assert.equal(block(big, 'agents').agents.length, 10);
  assert.deepEqual(block(big, 'agents').more, { id: 'agents-all', label: 'Ver todos (+3)' });
  assert.equal(big.animated, false);
  const all = buildPanel({ snap: snap(), now: NOW, ui: { tab: 'agents', agentsAll: true }, agents: many });
  assert.equal(block(all, 'agents').agents.length, 13);
  assert.deepEqual(block(all, 'agents').more, { id: 'agents-all', label: 'Ver menos' });
  const few = buildPanel({ snap: snap(), now: NOW, ui: { tab: 'agents', agentsAll: true }, agents: many.slice(0, 4) });
  assert.equal(block(few, 'agents').more, null);
});

test('Agentes: la key del composer sigue cambiando pasados 5 envíos', () => {
  const key = (n) => block(buildPanel({
    snap: snap(), now: NOW, ui: { tab: 'agents', agent: 'a1' }, agents: [row('a1')],
    agentDetail: detail({ sentCount: n, sent: detail().sent }),
  }), 'agent').composer.key;
  assert.equal(key(5), 'a1:5');
  assert.notEqual(key(6), key(5));
});

test('Agentes: detalle, composer con key nueva tras cada envío, volver con b', () => {
  const v = buildPanel({ snap: snap(), now: NOW, ui: { tab: 'agents', agent: 'a1' }, agents: [row('a1')], agentDetail: detail() });
  const d = block(v, 'agent');
  assert.equal(block(v, 'agents'), undefined);
  assert.deepEqual(d.composer, { key: 'a1:2', placeholder: 'mensaje para este agente', submitLabel: 'enviar', value: '' });
  assert.deepEqual(d.back, { id: 'agent-back', label: 'Volver', hotkey: 'b' });
  assert.deepEqual(d.sent.map((s) => s.state), ['en cola', 'no entregado: sin sesión']);
  const more = buildPanel({ snap: snap(), now: NOW, ui: { tab: 'agents', agent: 'a1' }, agentDetail: detail({ sent: [] }) });
  assert.equal(block(more, 'agent').composer.key, 'a1:0');
  const txt = panelText(v);
  assert.ok(txt.some((l) => l.includes('haz esto')) && txt.some((l) => l.includes('Edit x')) && txt.some((l) => l.includes('no entregado: sin sesión')));
  const err = buildPanel({ snap: snap(), now: NOW, ui: { tab: 'agents', agent: 'a1' }, agentDetail: detail({ error: 'boom' }) });
  assert.deepEqual(block(err, 'agent').error, ['boom']);
  // an unknown selection falls back to the list
  assert.ok(block(buildPanel({ snap: snap(), now: NOW, ui: { tab: 'agents', agent: 'zz' }, agents: [row('a1')] }), 'agents'));
});

test('Agentes: watching, Ahora con el libro, isPanelAction', () => {
  const v = buildPanel({ snap: snap(), now: NOW, ui: { tab: 'agents' }, agents: [row('a1'), row('a2')], viewAgentId: 'a2' });
  const w = block(v, 'watching');
  assert.equal(w.card.id, 'a2');
  assert.equal(w.open.id, 'agent:a2');
  assert.ok(isPanelAction(w.open.id) && isPanelAction('agent-back') && isPanelAction('agents-all'));
  assert.ok(!isPanelAction('agent:') && !isPanelAction('agent-x'));
  assert.equal(block(buildPanel({ snap: snap(), now: NOW, ui: { tab: 'agents' }, agents: [row('a1')], viewAgentId: 'nope' }), 'watching'), undefined);
  const hv = buildPanel({ snap: snap(), now: NOW, agents: [row('a1'), row('a2', { live: false, tone: 'ok' })] });
  assert.deepEqual(block(hv, 'now').agents.map((a) => a.id), ['a1']);
  assert.equal(block(hv, 'now').agents[0].activity, 'Edit core/panel.mjs · hace 2s');
});

test('Agentes: confirmación al hablarle al implementer del orquestador', () => {
  const v = buildPanel({ snap: snap(), now: NOW, ui: { tab: 'agents', agent: 'a1', pendingSend: { agentId: 'a1', text: 'hola', batch: 2 } }, agentDetail: detail() });
  assert.equal(v.ask.question, 'Hablarle al implementer del lote 2 puede romper el lote. ¿Mandar igual?');
  assert.deepEqual(v.ask.buttons.map((b) => [b.id, b.hotkey]), [['confirm-yes', 'y'], ['confirm-no', 'n']]);
});

test('layoutOf: widths at 28/40/60/120, the column never passes 78', () => {
  for (const cols of [0, 28, 40, 60, 120, 400]) {
    const l = layoutOf(cols);
    assert.ok(l.CW <= 78 && l.CW >= 1, `${cols}`);
    assert.ok(l.tileW * l.tilesPerRow + (l.tilesPerRow - 1) <= l.CW);
  }
  assert.deepEqual([layoutOf(28).W, layoutOf(28).pad, layoutOf(28).CW], [28, 1, 26]);
  assert.deepEqual([layoutOf(40).narrow, layoutOf(40).stacked, layoutOf(40).tilesPerRow], [true, true, 1]);
  assert.deepEqual([layoutOf(60).wide, layoutOf(60).stacked, layoutOf(60).tilesPerRow], [false, false, 2]);
  assert.deepEqual([layoutOf(120).CW, layoutOf(120).wide, layoutOf(120).tilesPerRow], [78, true, 3]);
});

test('lifecycleLayout: track while every span fits the longest label, then wrap without cutting', () => {
  const steps = lifecycle(snap());
  assert.equal(lifecycleLayout(steps, 78).mode, 'track');
  const narrow = lifecycleLayout(steps, 30);
  assert.equal(narrow.mode, 'wrap');
  assert.deepEqual(narrow.lines.flat(), steps);
  assert.equal(lifecycleLayout([], 60).mode, 'wrap');
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
  // An ask shows on every tab.
  for (const tab of ['plan', 'agents', 'stats', 'config']) {
    assert.ok(buildPanel({ snap: snap(), owned: true, ask: { type: 'ask', kind: 'pause', batches: [2] }, ui: { tab } }).ask, tab);
  }
});

test('output: clipped lines, running placeholder, dismiss key', () => {
  const v = home({ output: { label: 'Stats', text: 'a\nb' } });
  assert.deepEqual(v.output.lines, ['a', 'b']);
  assert.equal(v.output.title, 'Stats');
  assert.equal(home({ output: { label: 'Trend', text: '...', running: true } }).output.running, true);
  assert.equal(clipOutput(Array.from({ length: 40 }, (_, i) => `l${i}`).join('\n')).length, 31);
});

test('hotkeys are unique inside each built view, on the five tabs', () => {
  const asks = [null, { type: 'ask', kind: 'red', batch: 1 }, { type: 'ask', kind: 'pause', batches: [2] }];
  const review = { id: 'rv', findings: [{ id: 'R1', severity: 'high', file: 'a', line: 1, title: 't', cause: 'c' }] };
  for (const tab of TABS.map((t) => t.id)) for (const ask of asks) for (const output of [null, { label: 'x', text: 'y' }]) {
    for (const confirm of [null, 'plan-done']) {
      const keys = hotkeys(buildPanel({
        snap: snap({ review: { id: 'rv' }, reviewDetail: review }), owned: !!ask, ask, ui: { tab, confirm, picked: ['R1'], agent: 'a1' }, output,
        agentDetail: detail(),
      }));
      assert.equal(new Set(keys).size, keys.length, `${tab} ${JSON.stringify(ask)} ${keys}`);
      assert.ok(keys.every((k) => /^[a-z0-9]$/.test(k)));
    }
  }
  // the fix button shows only when the plan is not owned: check it directly
  const keys = hotkeys(buildPanel({ snap: snap({ review: { id: 'rv' }, reviewDetail: review }), ui: { tab: 'plan', picked: ['R1'] } }));
  assert.ok(keys.includes('z') && new Set(keys).size === keys.length);
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
  for (const s of ['[Inicio]', 'nxy ● ready', 'Plan: abc12345 · aprobado', 'Contexto: 46k / 100k', 'gate on', 'Cache: desconocido', 'nada corriendo', 'hello']) {
    assert.ok(joined.includes(s), s);
  }
  assert.ok(panelText(plan({})).join('\n').includes('[e] Terminar plan'));
  assert.ok(panelText(buildPanel({ snap: null, now: NOW })).includes('Plan: sin leer'));
  assert.ok(panelText(home({ snap: snap({ hash: null, batches: [] }) })).includes('Plan: ninguno'));
});

test('panel.mjs stays pure: no node: imports, only orchestrator, batch-status, statsview, configedit and memview', () => {
  const src = readFileSync(fileURLToPath(new URL('../core/panel.mjs', import.meta.url)), 'utf8');
  assert.ok(!/from\s+['"]node:/.test(src));
  const imports = [...src.matchAll(/from\s+'([^']+)'/g)].map((m) => m[1]);
  assert.deepEqual(imports.sort(), ['./batch-status.mjs', './configedit.mjs', './memview.mjs', './orchestrator.mjs', './statsview.mjs']);
});

const mem = (over = {}) => buildPanel({ snap: snap(), now: NOW, ui: { tab: 'memory', mem: over } });
const launchView = (launch, agents = []) => buildPanel({ snap: snap(), now: NOW, ui: { tab: 'agents', launch }, agents });

test('Memoria: sixth tab, keys 1-5 unchanged, isPanelAction for the new ids', () => {
  assert.deepEqual(TABS.at(-1), { id: 'memory', label: 'Memoria', hotkey: '6' });
  assert.deepEqual(TABS.slice(0, 5).map((t) => t.hotkey), ['1', '2', '3', '4', '5']);
  for (const id of ['mem:abc-1', 'mem-back', 'mem-handoff', 'mem-recent', 'launch:scout', 'launch:librarian', 'launch-ask', 'tab:memory']) {
    assert.ok(isPanelAction(id), id);
  }
  assert.ok(!isPanelAction('mem:') && !isPanelAction('launch:other') && !isPanelAction('mem-x'));
});

test('Memoria: search block in idle, loading, error, empty and ok', () => {
  const s = (o) => block(mem(o), 'memsearch');
  assert.equal(s({}).state, 'idle');
  assert.deepEqual(s({}).recent, { id: 'mem-recent', label: 'Recientes' });
  assert.equal(s({}).input.field, 'mem-search');
  assert.equal(s({ state: 'loading' }).state, 'loading');
  assert.match(s({ state: 'error', error: 'boom' }).message, /boom/);
  const none = s({ state: 'ok', query: 'zzz', results: [] });
  assert.equal(none.state, 'empty');
  assert.match(none.message, /zzz/);
  const ok = s({ state: 'ok', results: [{ id: 'a-1', title: 'Titulo', type: 'decision', area: 'core', scope: 'repo' }] });
  assert.equal(ok.state, 'ok');
  assert.equal(ok.recent, null);
  assert.deepEqual(ok.rows[0], { id: 'a-1', press: 'mem:a-1', text: 'Titulo', sub: 'decision · core · repo' });
  assert.notEqual(s({ key: 1 }).input.key, s({ key: 2 }).input.key);
});

test('Memoria: detail replaces the search while open; Volver is b', () => {
  const memory = { id: 'a-1', title: 'Titulo', type: 'decision', scope: 'repo', keywords: ['x'], body: 'uno\ndos' };
  const v = mem({ open: 'a-1', detail: { state: 'ok', data: { memory, edges: [{ kind: 'related', dir: 'out', other: 'b-2' }] } } });
  assert.equal(block(v, 'memsearch'), undefined);
  const d = block(v, 'memdetail');
  assert.deepEqual(d.back, { id: 'mem-back', label: 'Volver', hotkey: 'b' });
  assert.ok(d.lines.includes('→ related b-2') && d.lines.includes('uno'));
  assert.equal(block(mem({ open: 'a-1' }), 'memdetail').state, 'loading');
  assert.match(block(mem({ open: 'a-1', detail: { state: 'error', error: 'boom' } }), 'memdetail').message, /boom/);
  assert.equal(block(mem({ open: 'a-1', detail: { state: 'empty' } }), 'memdetail').state, 'empty');
});

test('Memoria: handoff states and body clipped to 20 lines', () => {
  const h = (o) => block(mem({ handoff: o }), 'handoff');
  assert.equal(block(mem({}), 'handoff').state, 'idle');
  assert.equal(h({ state: 'loading' }).state, 'loading');
  assert.match(h({ state: 'error', error: 'boom' }).text, /boom/);
  assert.match(h({ state: 'ok', data: { exists: false, label: 'feat' } }).text, /feat/);
  const body = Array.from({ length: 40 }, (_, i) => `l${i}`).join('\n');
  const ok = h({ state: 'ok', data: { exists: true, label: 'feat', updated: 'hoy', body } });
  assert.equal(ok.state, 'ok');
  assert.equal(ok.lines.length, 21);
  assert.equal(ok.button.id, 'mem-handoff');
});

test('Memoria: locate states, hits, ms, no-model note and the scout button', () => {
  const l = (o) => block(mem({ locate: o }), 'locate');
  assert.equal(l(undefined).state, 'idle');
  assert.equal(l(undefined).ask, null);
  assert.equal(l({ question: 'q', state: 'loading' }).state, 'loading');
  assert.match(l({ question: 'q', state: 'error', error: 'boom' }).message, /boom/);
  const empty = l({ question: 'nada', state: 'ok', data: { hits: [], ms: 5 } });
  assert.equal(empty.state, 'empty');
  assert.equal(empty.ask.id, 'launch-ask');
  const ok = l({ question: 'q', state: 'ok', data: { hits: [{ path: 'C:/x/y.mjs', line: 12, kind: 'function', name: 'foo' }], ms: 42, degraded: ['rg'] } });
  assert.deepEqual(ok.rows, [{ loc: 'C:/x/y.mjs:12', rest: 'function foo', text: 'C:/x/y.mjs:12  function foo' }]);
  assert.equal(ok.ms, 42);
  assert.equal(ok.note, 'sin modelo · 0 tokens');
  assert.deepEqual(ok.degraded, ['rg']);
  assert.equal(ok.ask.label, 'Preguntarle al scout');
});

test('Agentes: launch block for scout and librarian, running, result, raw and error', () => {
  const idle = block(launchView({}), 'launch');
  assert.equal(idle.kind, 'scout');
  assert.deepEqual(idle.kinds.map((k) => [k.id, k.active]), [['launch:scout', true], ['launch:librarian', false]]);
  assert.equal(idle.input.field, 'launch');
  assert.equal(idle.card, null);
  assert.notEqual(block(launchView({ kind: 'librarian' }), 'launch').input.placeholder, idle.input.placeholder);
  const running = block(launchView({ kind: 'scout', agentId: 'a1', question: 'q' }, [row('a1')]), 'launch');
  assert.equal(running.running, true);
  assert.equal(running.input.submitLabel, 'en curso');
  assert.equal(running.open.id, 'agent:a1');
  const fin = { live: false, tone: 'ok', state: 'terminó' };
  const scout = block(launchView({ kind: 'scout', agentId: 'a1', result: '- `src/a.mjs:10` — foo\n- core\\b.mjs:7 bar' }, [row('a1', fin)]), 'launch');
  assert.deepEqual(scout.hits.map((h) => h.loc), ['src/a.mjs:10', 'core\\b.mjs:7']);
  assert.deepEqual(scout.raw, []);
  const lib = block(launchView({ kind: 'librarian', agentId: 'a1', result: '- note-1 — aplica\nnone' }, [row('a1', fin)]), 'launch');
  assert.deepEqual(lib.mems, [{ id: 'note-1', press: 'mem:note-1', text: 'note-1', sub: 'aplica' }]);
  const raw = block(launchView({ kind: 'scout', agentId: 'a1', result: Array.from({ length: 30 }, (_, i) => `linea ${i}`).join('\n') }, [row('a1', fin)]), 'launch');
  assert.equal(raw.hits.length, 0);
  assert.equal(raw.raw.length, 13);
  assert.equal(block(launchView({ error: 'sin spawn' }), 'launch').error, 'sin spawn');
  // not in the agent detail view
  assert.equal(block(buildPanel({ snap: snap(), now: NOW, ui: { tab: 'agents', agent: 'a1' }, agents: [row('a1')], agentDetail: detail() }), 'launch'), undefined);
});

test('Memoria and launch: panelText prints every block', () => {
  const txt = panelText(buildPanel({
    snap: snap(), now: NOW,
    ui: { tab: 'memory', mem: {
      state: 'ok', results: [{ id: 'a-1', title: 'Titulo' }], handoff: { state: 'ok', data: { exists: true, label: 'feat', body: 'cuerpo' } },
      locate: { question: 'q', state: 'ok', data: { hits: [{ path: 'a.mjs', line: 3, kind: 'function', name: 'f' }], ms: 7 } },
    } },
  })).join('\n');
  for (const s of ['[Memoria]', 'Titulo', 'Handoff · feat', 'cuerpo', 'a.mjs:3  function f', '7 ms', 'sin modelo · 0 tokens', 'Preguntarle al scout']) assert.ok(txt.includes(s), s);
  const lt = panelText(launchView({ kind: 'scout', agentId: 'a1', question: 'donde', result: 'x.mjs:1 y' }, [row('a1', { live: false, tone: 'ok' })])).join('\n');
  for (const s of ['Scout', 'donde', 'x.mjs:1  y', 'Ver agente']) assert.ok(lt.includes(s), s);
});

test('hotkeys stay unique on the Memoria tab in every state', () => {
  for (const mm of [{}, { open: 'a-1' }, { open: 'a-1', detail: { state: 'ok', data: { memory: { id: 'a-1', body: 'x' } } } }]) {
    for (const output of [null, { label: 'x', text: 'y' }]) {
      const v = buildPanel({ snap: snap(), now: NOW, ui: { tab: 'memory', mem: mm }, output });
      const keys = [...hotkeys(v), ...v.blocks.filter((b) => b.type === 'memdetail').map((b) => b.back.hotkey)];
      assert.equal(new Set(keys).size, keys.length, keys.join());
    }
  }
});

test('memory locate: while the launched scout is live the ask button is off and a note says so', () => {
  const mem = { locate: { question: 'dónde está foo', state: 'ok', data: { hits: [{ path: 'a.mjs', line: 3, kind: 'function', name: 'foo' }], ms: 5 } } };
  const view = (live) => buildPanel({
    snap: snap(), now: NOW, ui: { tab: 'memory', mem, launch: { kind: 'scout', agentId: 'ag1', question: 'dónde está foo' } },
    agents: [{ id: 'ag1', role: 'scout', live, state: live ? 'corriendo' : 'listo' }],
  }).blocks.find((b) => b.type === 'locate');
  const on = view(true);
  assert.equal(on.ask.id, '');
  assert.equal(on.ask.disabled, true);
  assert.match(on.askNote, /Agentes/);
  const off = view(false);
  assert.equal(off.ask.id, 'launch-ask');
  assert.equal(off.askNote, '');
});
