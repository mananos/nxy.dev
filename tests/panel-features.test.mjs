// @ts-check
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { TABS, launcherOf, launcherAvailable, LAUNCHER, buildPanel, panelText, isPanelAction } from '../core/panel.mjs';

const NOW = 1_000_000_000;
const MAIN = '/fake/repo';
/** @type {any[]} */
const NO_ROWS = [];
const WTS = [
  { path: MAIN, branch: 'main', main: true, current: false, dirty: 0, ahead: 0 },
  { path: '/fake/repo.worktrees/algo', branch: 'feature/algo', current: true, dirty: 2, ahead: 1, handoffAt: NOW - 7_200_000, batchDone: 1, batchTotal: 3 },
  { path: '/fake/repo.worktrees/otro', branch: 'feature/otro', dirty: 0, pausedAt: NOW - 600_000, running: true, missingDeps: true },
];
const DATA = {
  ok: true, worktrees: WTS, main: MAIN, repoName: 'repo', shell: 'posix', platform: 'linux',
  branches: { free: ['dev', 'fix/x'], busy: [{ name: 'feature/otro', path: '/fake/repo.worktrees/otro' }] },
  defaultBranch: 'main', bases: ['main', 'dev', 'feature/otro'],
};

/** @param {any} [f] @param {any} [extra] */
const view = (f = {}, extra = {}) => buildPanel({
  snap: { hash: null, batches: null }, now: NOW, ui: { tab: 'features', features: { state: 'ok', data: DATA, ...f } }, ...extra,
});
/** @param {any} v @returns {any} */
const wt = (v) => v.blocks.find((/** @type {any} */ b) => b.type === 'worktrees');

test('features tab is the seventh', () => {
  assert.deepEqual(TABS.at(-1), { id: 'features', label: 'Features', hotkey: '7' });
  for (const id of ['feat:open:0', 'feat:pause:2', 'feat:close:1', 'feat:mode:new', 'feat:mode:existing', 'feat:mode:cancel']) assert.ok(isPanelAction(id), id);
  assert.ok(!isPanelAction('feat:mode:nope'));
});

test('worktrees block: three rows with tags and disabled buttons', () => {
  const b = wt(view());
  assert.equal(b.rows.length, 3);
  assert.deepEqual(b.rows[0].tags, ['principal']);
  assert.ok(b.rows[1].tags.includes('esta sesión'));
  assert.ok(b.rows[2].tags.includes('plan corriendo'));
  assert.ok(b.rows[2].tags.some((/** @type {string} */ t) => t.startsWith('pausado')));
  const close = (/** @type {number} */ i) => b.rows[i].buttons.find((/** @type {any} */ x) => x.id === `feat:close:${i}`);
  for (const i of [0, 1, 2]) assert.equal(close(i).disabled, true);
  assert.equal(b.rows[1].buttons[0].disabled, true);
  assert.equal(b.rows[0].buttons[0].disabled, undefined);
  assert.equal(b.rows[2].buttons[1].label, 'Reanudar');
  assert.equal(b.rows[0].buttons[1].label, 'Pausar');
  assert.ok(b.rows[2].lines.some((/** @type {string} */ l) => l.includes('node_modules')));
});

test('no mode: two branch buttons, no input, no select', () => {
  const b = wt(view());
  assert.deepEqual(b.modes.map((/** @type {any} */ m) => m.id), ['feat:mode:new', 'feat:mode:existing']);
  assert.equal(b.input, null);
  assert.equal(b.select, null);
});

test('new: issue and name steps share one input with different field and key', () => {
  const a = wt(view({ mode: 'new', step: 'issue' }));
  const n = wt(view({ mode: 'new', step: 'name', issue: '14' }));
  assert.equal(a.input.field, 'feature-issue');
  assert.equal(n.input.field, 'feature-name');
  assert.notEqual(a.input.key, n.input.key);
  assert.notEqual(a.input.placeholder, n.input.placeholder);
  for (const b of [a, n]) {
    assert.equal(b.select, null);
    assert.deepEqual(b.modes.map((/** @type {any} */ m) => m.id), ['feat:mode:cancel']);
  }
});

test('new: base step has the select, the main first and preselected, no input', () => {
  const b = wt(view({ mode: 'new', step: 'base', issue: '14', name: 'algo' }));
  assert.equal(b.input, null);
  assert.equal(b.select.pick, 'base');
  assert.equal(b.select.value, 'main');
  assert.deepEqual(b.select.options.map((/** @type {any} */ o) => o.value), ['main', 'dev', 'feature/otro']);
  assert.match(b.select.options[0].label, /principal/);
  assert.ok(b.info.some((/** @type {string} */ l) => l.includes('feature/#14_algo')));
  assert.equal(b.modes[0].id, 'feat:mode:cancel');
  const none = wt(view({ mode: 'new', step: 'base', name: 'algo' }, {}));
  assert.ok(none.select);
  const empty = wt(buildPanel({
    snap: { hash: null, batches: null }, now: NOW,
    ui: { tab: 'features', features: { state: 'ok', data: { ...DATA, bases: [], defaultBranch: null }, mode: 'new', step: 'base', name: 'algo' } },
  }));
  assert.equal(empty.select, null);
  assert.ok(empty.info.includes('no hay ramas locales'));
});

test('existing: select with free branches, busy ones as lines', () => {
  const b = wt(view({ mode: 'existing' }));
  assert.equal(b.select.pick, 'branch');
  assert.deepEqual(b.select.options.map((/** @type {any} */ o) => o.value), ['', 'dev', 'fix/x']);
  assert.equal(b.select.value, '');
  assert.equal(b.busy.length, 1);
  assert.match(b.busy[0], /feature\/otro.*\/fake\/repo\.worktrees\/otro.*no se puede elegir/);
  assert.equal(b.modes[0].id, 'feat:mode:cancel');
  const nofree = wt(buildPanel({
    snap: { hash: null, batches: null }, now: NOW,
    ui: { tab: 'features', features: { state: 'ok', data: { ...DATA, branches: { free: [], busy: [] } }, mode: 'existing' } },
  }));
  assert.equal(nofree.select, null);
  assert.ok(nofree.info.includes('no hay ramas libres'));
});

test('loading and error states', () => {
  assert.equal(wt(view({ state: 'loading', data: null })).state, 'loading');
  const e = wt(view({ state: 'error', data: null, error: 'boom' }));
  assert.equal(e.state, 'error');
  assert.match(e.message, /boom/);
});

test('feat-close: dirty says it is lost, clean does not', () => {
  const rows = WTS;
  const dirty = launcherOf('feat-close', { features: { rows, target: WTS[1].path, force: true } });
  assert.match(dirty?.confirm ?? '', /SE PIERDEN/);
  assert.deepEqual(dirty?.args, ['close', '--path', WTS[1].path, '--force']);
  const clean = launcherOf('feat-close', { features: { rows, target: WTS[2].path, force: false } });
  assert.doesNotMatch(clean?.confirm ?? '', /SE PIERDEN/);
  assert.deepEqual(clean?.args, ['close', '--path', WTS[2].path]);
});

test('feat-new: exact git worktree add with -b and the base named, or without -b', () => {
  const nw = launcherOf('feat-new', { features: { main: MAIN, name: 'algo', issue: '14', base: 'dev' } });
  assert.match(nw?.confirm ?? '', /git worktree add -b 'feature\/#14_algo' \/fake\/repo\.worktrees\/14_algo dev/);
  assert.match(nw?.confirm ?? '', /rama local dev/);
  assert.deepEqual(nw?.args, ['new', '--name', 'algo', '--issue', '14', '--base', 'dev']);
  const ex = launcherOf('feat-new', { features: { main: MAIN, branch: 'fix/x' } });
  assert.match(ex?.confirm ?? '', /git worktree add \/fake\/repo\.worktrees\/fix_x fix\/x/);
  assert.doesNotMatch(ex?.confirm ?? '', / -b /);
  assert.deepEqual(ex?.args, ['new', '--branch', 'fix/x']);
});

test('feat-new / feat-close confirm follow data.shell', () => {
  const main = "/fake/o'k";
  const nw = launcherOf('feat-new', { features: { main, name: 'algo', base: 'dev', shell: 'powershell' } });
  assert.match(nw?.confirm ?? '', /o''k\.worktrees/);
  const px = launcherOf('feat-new', { features: { main, name: 'algo', base: 'dev' } });
  assert.match(px?.confirm ?? '', /o'\\''k\.worktrees/);
  const cl = launcherOf('feat-close', { features: { rows: [], target: "/fake/o'k", force: false, shell: 'powershell' } });
  assert.match(cl?.confirm ?? '', /o''k/);
});

test('buildPanel carries data.shell into the drawn confirmation', () => {
  const main = "/fake/o'k";
  const data = (/** @type {string} */ shell) => ({ ...DATA, main, shell, worktrees: [{ path: main, branch: 'main', main: true, dirty: 0 }, { path: "/fake/o'k.worktrees/a", branch: 'feature/a', dirty: 0 }] });
  const ask = (/** @type {string} */ confirm, /** @type {string} */ shell, /** @type {any} */ f) => buildPanel({
    snap: { hash: null, batches: null }, now: NOW, ui: { tab: 'features', confirm, features: { state: 'ok', data: data(shell), ...f } },
  }).ask?.question ?? '';
  const nw = { name: 'algo', base: 'dev' };
  assert.match(ask('feat-new', 'powershell', nw), /o''k\.worktrees/);
  assert.match(ask('feat-new', 'posix', nw), /o'\\''k\.worktrees/);
  const cl = { target: "/fake/o'k.worktrees/a" };
  assert.match(ask('feat-close', 'powershell', cl), /o''k/);
  assert.doesNotMatch(ask('feat-close', 'powershell', cl), /o'\\''k/);
});

test('feat-new availability needs the ctx', () => {
  const e = LAUNCHER.find((l) => l.id === 'feat-new');
  assert.ok(e);
  const snap = { hash: null };
  assert.equal(launcherAvailable(e, { snap, ctx: { features: { name: 'a', base: 'main' } } }), true);
  assert.equal(launcherAvailable(e, { snap, ctx: { features: { name: 'a' } } }), false);
  assert.equal(launcherAvailable(e, { snap, ctx: { features: { branch: 'x' } } }), true);
  assert.equal(launcherAvailable(e, { snap, ctx: {} }), false);
  assert.equal(launcherAvailable(e, { snap, ctx: null }), false);
});

test('hotkeys stay unique with a feat confirm open, and the text dump works', () => {
  for (const confirm of ['feat-new', 'feat-pause', 'feat-close', null]) {
    for (const tab of TABS.map((t) => t.id)) {
      const v = buildPanel({ snap: { hash: null, batches: null }, now: NOW, ui: { tab, confirm, features: { state: 'ok', data: DATA } } });
      const keys = [
        ...v.tabs.map((t) => t.hotkey), ...v.keys.map((k) => k.hotkey),
        ...v.blocks.filter((/** @type {any} */ b) => b.type === 'actions').flatMap((/** @type {any} */ b) => b.actions.map((/** @type {any} */ a) => a.hotkey)),
        ...(v.ask?.buttons ?? []).map((b) => b.hotkey),
      ].filter(Boolean);
      assert.equal(new Set(keys).size, keys.length, `${tab}/${confirm}`);
    }
  }
  const text = panelText(view({ mode: 'new', step: 'base', issue: '14', name: 'algo' })).join('\n');
  assert.match(text, /sale de: main \(principal\) · elegí una de: main, dev, feature\/otro/);
  assert.match(panelText(view({ mode: 'existing' })).join('\n'), /rama: elegí una de: dev, fix\/x/);
});
