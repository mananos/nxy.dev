import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createDriver } from '../hosts/claude-code/mod/driver.mjs';
import { createPane } from '../hosts/claude-code/mod/pane.mjs';

/**
 * A fake `$` (only what the driver touches) and a fake io that records the order of its calls.
 * `agents`: what agent.list answers; `counters`: counted agent.list/session.messages and a controllable pane list.
 * @param {{ agents?: any[], counters?: any }} [o]
 */
function setup({ agents = [], counters = null } = {}) {
  const state = counters ?? { listCalls: 0, msgCalls: 0, isShown: true, isPlaced: true, gone: false, panesAnswer: null, blitResult: {}, blitArgs: [] };
  const files = new Map();
  const $ = {
    plugin: { root: '/plugin' },
    fs: {
      read: async (p) => { if (!files.has(p)) throw new Error('ENOENT'); return files.get(p); },
      write: async (p, t) => { files.set(p, t); },
    },
    process: { run: async () => ({ exitCode: 0, stdout: '{"ok":false}', stderr: '' }) },
    agent: { spawn: async () => ({ model: 'sonnet', agentId: 'ag1' }), list: async () => { state.listCalls++; return agents; } },
    session: { append: async () => {}, send: async () => ({ isDelivered: true }), messages: async () => { state.msgCalls++; return []; } },
    prompt: { submit: async () => {} },
  };
  const driver = createDriver($, { cwd: '/proj', sessionId: 's1', waitTries: 1, waitMs: 0, sleep: async () => {} });
  const calls = [];
  /** @type {any} */
  let pane = null;
  const io = {
    // The engine would call the render handler, which records what it drew.
    invalidate: async () => { calls.push('invalidate'); pane.drawn(driver.panel()); },
    panes: async () => {
      calls.push('panes');
      if (counters) return state.panesAnswer ?? (state.gone ? [] : [{ id: 'nxy', isShown: state.isShown, isPlaced: state.isPlaced }]);
      return [];
    },
    usage: async () => { calls.push('usage'); return null; },
    blit: async (a) => { calls.push('blit'); state.blitArgs.push(a); return state.blitResult; },
    log: () => { calls.push('log'); },
    now: () => Date.now(),
  };
  pane = createPane(io, driver, { id: 'nxy' });
  return { driver, pane, calls, state };
}

test('a press on a visited tab: invalidate is the first engine call and the only one', async () => {
  const { driver, pane, calls } = setup();
  await pane.press('tab:agents'); // visit
  pane.drawn(driver.panel());
  await pane.press('tab:plan');
  calls.length = 0;
  await pane.press('tab:agents');
  assert.equal(calls[0], 'invalidate');
  assert.equal(calls.filter((c) => c === 'invalidate').length, 1);
});

test('first visit to Stats: two invalidates (loading, loaded)', async () => {
  const { driver, pane, calls } = setup();
  pane.drawn(driver.panel());
  calls.length = 0;
  await pane.press('tab:stats');
  assert.equal(calls[0], 'invalidate');
  assert.equal(calls.filter((c) => c === 'invalidate').length, 2);
});

/** setup() plus 3 running foreign agents, a counted agent.list and a controllable pane list. */
async function live() {
  const agents = [1, 2, 3].map((n) => ({ id: `e${n}`, status: 'running', type: 'Explore', description: `d${n}` }));
  const s = setup({ agents, counters: { listCalls: 0, msgCalls: 0, isShown: true, isPlaced: true, gone: false, panesAnswer: null, blitResult: {}, blitArgs: [] } });
  await s.driver.syncAgents();
  s.state.listCalls = 0;
  return s;
}

/** Run `fn` with Date.now() advancing 250 ms per call to `step()`. */
async function withClock(fn) {
  const real = Date.now;
  let t = real();
  Date.now = () => t;
  try { await fn(() => { t += 250; }); } finally { Date.now = real; }
}
const inv = (calls) => calls.filter((c) => c === 'invalidate').length;

test('40 ticks with 3 live agents: Config 0 invalidates, Home at most 10', async () => {
  const { driver, pane, calls } = await live();
  assert.equal(driver.agentsLive(), true);
  await driver.press('tab:config');
  pane.drawn(driver.panel());
  await pane.tick(); // settle
  calls.length = 0;
  for (let i = 0; i < 40; i++) await pane.tick();
  assert.equal(inv(calls), 0);
  await driver.press('tab:home');
  pane.drawn(driver.panel());
  calls.length = 0;
  await withClock(async (step) => {
    for (let i = 0; i < 40; i++) { step(); await pane.tick(); } // 10 s: live times really change
  });
  // Live times differ in phase, but tick redraws are at least 1 s apart.
  assert.ok(inv(calls) > 0 && inv(calls) <= 10);
});

test('a change withheld by the 1 s throttle is drawn at the next allowed tick', async () => {
  const { driver, pane, calls } = await live();
  await driver.press('tab:home');
  await withClock(async (step) => {
    await pane.tick(); // beat 1: first draw
    pane.drawn(null);
    calls.length = 0;
    step(); await pane.tick(); // 250 ms after the last redraw, view differs from "drawn": withheld
    assert.equal(inv(calls), 0);
    for (let i = 0; i < 2; i++) { step(); await pane.tick(); }
    assert.equal(inv(calls), 0);
    step(); await pane.tick(); // 1000 ms
    assert.equal(inv(calls), 1);
  });
});

test('a non-array pane list keeps the state and the tick goes on; a list without the pane stops it', async () => {
  const { pane, state } = await live();
  await pane.tick(); // beat 1: pane check, shown
  state.panesAnswer = 'not a list';
  for (let i = 0; i < 4; i++) assert.equal(await pane.tick(), undefined); // beat 5 sees the bad answer
  state.panesAnswer = { nope: true };
  assert.equal(await pane.tick(), undefined);
  state.panesAnswer = [{ id: 'other', isShown: true, isPlaced: true }];
  let r;
  for (let i = 0; i < 4 && !r; i++) r = await pane.tick();
  assert.deepEqual(r, { stop: true });
});

test('hidden pane: 0 invalidates and 0 polls; hidden to shown gives exactly 1', async () => {
  const { driver, pane, calls, state } = await live();
  await driver.press('tab:home');
  pane.drawn(driver.panel());
  await pane.tick();
  state.isShown = false;
  for (let i = 0; i < 4; i++) await pane.tick(); // the pane check lands
  calls.length = 0;
  state.listCalls = 0;
  for (let i = 0; i < 40; i++) await pane.tick();
  assert.equal(inv(calls), 0);
  assert.equal(state.listCalls, 0);
  state.isShown = true;
  for (let i = 0; i < 40; i++) await pane.tick();
  assert.equal(inv(calls), 1);
});

const blits = (calls) => calls.filter((c) => c === 'blit').length;

test('40 ticks on Agentes with 3 live agents: spinners move by blit, 0 invalidates', async () => {
  const { driver, pane, calls, state } = await live();
  await driver.press('tab:agents');
  pane.drawn(driver.panel());
  await pane.tick(); // settle (beat 1)
  calls.length = 0;
  for (let i = 0; i < 40; i++) await pane.tick();
  assert.equal(inv(calls), 0);
  assert.equal(blits(calls), 20 * (3 + 0)); // 3 spinners, no plan strip here
  const keys = state.blitArgs.slice(0, 3).map((a) => a.key);
  assert.equal(new Set(keys).size, 3);
  assert.ok(state.blitArgs.every((a) => a.requestId === 'nxy' && typeof a.cells === 'string'));
});

test('a blit deny falls back to exactly one redraw', async () => {
  const { driver, pane, calls, state } = await live();
  await driver.press('tab:agents');
  pane.drawn(driver.panel());
  await pane.tick(); // beat 1
  calls.length = 0;
  state.blitResult = { deny: 'not mounted' };
  await pane.tick(); // beat 2: blit, denied
  assert.equal(inv(calls), 1);
  assert.equal(blits(calls), 1);
  assert.equal(pane.perf.denies, 1);
});

test('a gone pane stops the tick', async () => {
  const { pane, state } = await live();
  state.gone = true;
  assert.deepEqual(await pane.tick(), { stop: true });
});

test('a never-resolving poll does not block the next redraw', async () => {
  const { driver, pane, calls } = await live();
  driver.syncAgents = () => new Promise(() => {});
  pane.drawn(null);
  await withClock(async (step) => {
    for (let i = 0; i < 9; i++) { step(); await pane.tick(); } // beat 8 starts the stuck poll
    calls.length = 0;
    pane.drawn(null);
    for (let i = 0; i < 4; i++) { step(); await pane.tick(); } // within the 1 s throttle window
    assert.equal(inv(calls), 1);
  });
});

test('redraw() without a change does not invalidate; a forced one does', async () => {
  const { driver, pane, calls } = setup();
  pane.drawn(driver.panel());
  await pane.redraw();
  assert.equal(calls.length, 0);
  await pane.redraw(true);
  assert.deepEqual(calls, ['invalidate']);
});
