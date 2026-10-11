import test from 'node:test';
import assert from 'node:assert/strict';
import { tickPlan, viewSig, spinKey, spinnersOf } from '../core/tick.mjs';

test('spinnersOf: the spinners the active tab draws, unique keys, the strip only while a batch runs', () => {
  const card = { id: 'a1', live: true };
  const home = {
    header: { progress: ['done', 'running', 'pending'] },
    blocks: [
      { type: 'hero', status: 'plan', batch: { state: 'running' } },
      { type: 'now', agents: [card, { id: 'a2', live: true }] },
    ],
  };
  const h = spinnersOf(home);
  assert.deepEqual(h.spinners.map((s) => s.key), [spinKey('hero', 0), spinKey('now', 0), spinKey('now', 1)]);
  assert.equal(h.spinners[2].k, 5);
  assert.equal(h.progress, true);

  const plan = spinnersOf({ header: { progress: ['done', 'done'] }, blocks: [{ type: 'batches', items: [
    { n: 1, state: 'done' }, { n: 2, state: 'running', open: true }, { n: 3, state: 'running', open: false }] }] });
  assert.deepEqual(plan.spinners.map((s) => s.key), [spinKey('bt', 2), spinKey('bv', 2), spinKey('bt', 3)]);
  assert.equal(plan.spinners[0].band, true);
  assert.equal(plan.spinners[2].band, undefined);
  assert.equal(plan.progress, false);

  const agents = spinnersOf({ blocks: [
    { type: 'watching', card },
    { type: 'agents', agents: [card, { id: 'a3', live: false }] },
    { type: 'launch', card },
  ] });
  assert.deepEqual(agents.spinners.map((s) => s.key), [spinKey('watch', 0), spinKey('ag', 'a1'), spinKey('launch', 0)]);

  const mem = spinnersOf({ blocks: ['memsearch', 'handoff', 'locate'].map((type) => ({ type, state: 'loading' })).concat([{ type: 'memdetail', state: 'ok' }]) });
  assert.deepEqual(mem.spinners.map((s) => s.key), [spinKey('ms', 0), spinKey('ho', 0), spinKey('lo', 0)]);
  const keys = [...h.spinners, ...plan.spinners, ...agents.spinners, ...mem.spinners].map((s) => s.key);
  assert.equal(new Set(keys).size, keys.length);
});

test('spinnersOf: Stats and an empty view draw none', () => {
  assert.deepEqual(spinnersOf({ blocks: [{ type: 'tiles' }, { type: 'chart' }] }), { spinners: [], progress: false });
  assert.deepEqual(spinnersOf(null), { spinners: [], progress: false });
});

test('viewSig: ms changes inside one second give the same sig', () => {
  const a = { tab: 'home', agents: [{ id: 'a', elapsedMs: 5100, text: 'x' }] };
  const b = { tab: 'home', agents: [{ id: 'a', elapsedMs: 5900, text: 'x' }] };
  assert.equal(viewSig(a), viewSig(b));
});

test('viewSig: differs across a second boundary or a text change', () => {
  const a = { elapsedMs: 5900, text: 'x' };
  assert.notEqual(viewSig(a), viewSig({ elapsedMs: 6000, text: 'x' }));
  assert.notEqual(viewSig(a), viewSig({ elapsedMs: 5900, text: 'y' }));
});

test('viewSig: tolerates null and undefined, only touches keys ending in Ms', () => {
  assert.equal(typeof viewSig(null), 'string');
  assert.equal(viewSig(undefined), viewSig(null));
  assert.notEqual(viewSig({ count: 1001 }), viewSig({ count: 1002 }));
});

const base = { beat: 1, shown: true, wasShown: true, sigChanged: false, agentsLive: true };

test('tickPlan: hidden pane never polls or redraws', () => {
  const p = tickPlan({ ...base, beat: 8, shown: false, wasShown: false, sigChanged: true });
  assert.equal(p.poll, false);
  assert.equal(p.redraw, false);
});

test('tickPlan: hidden to shown forces one redraw', () => {
  assert.equal(tickPlan({ ...base, wasShown: false, sigChanged: false }).redraw, true);
});

test('tickPlan: unchanged view while shown does not redraw; a change does', () => {
  assert.equal(tickPlan(base).redraw, false);
  assert.equal(tickPlan({ ...base, sigChanged: true }).redraw, true);
});

test('tickPlan: poll only on beat%8===0 with agents live and shown', () => {
  assert.equal(tickPlan({ ...base, beat: 8 }).poll, true);
  assert.equal(tickPlan({ ...base, beat: 16 }).poll, true);
  assert.equal(tickPlan({ ...base, beat: 4 }).poll, false);
  assert.equal(tickPlan({ ...base, beat: 8, agentsLive: false }).poll, false);
});

test('tickPlan: checkPanes on beat%4===1', () => {
  assert.equal(tickPlan({ ...base, beat: 1 }).checkPanes, true);
  assert.equal(tickPlan({ ...base, beat: 5 }).checkPanes, true);
  assert.equal(tickPlan({ ...base, beat: 2 }).checkPanes, false);
});
