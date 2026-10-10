import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createDriver } from '../hosts/claude-code/mod/driver.mjs';

const RT = '/rt/nxy';
const HASH = 'abc12345';
const PRICES = { sonnet: { input: 3, output: 15, cache_read: 0.3, cache_write_5m: 3.75, cache_write_1h: 6 } };

/** A fake `$` with agent.list, session.send and session.messages. */
function world(over = {}) {
  const files = new Map();
  const plan = {
    ok: true, runtimeDir: RT, projectDir: '/proj', branch: 'main',
    config: { pauseAfterBatch: false, orchestrator: 'auto' },
    hash: HASH, approved: true, fresh: true, questions: 0, prices: PRICES,
    roles: { implementer: { model: 'sonnet', effort: 'medium' } },
    batches: [{ n: 1, depends: [], text: 'Batch 1 — one\nAccept: `x`', accept: { command: 'x' } }],
    verdicts: {}, suite: null, review: null, reviewNeeded: { needed: true, reason: '' },
    ...over,
  };
  /** @type {any} */
  const w = { files, plan, list: [], sent: [], sendResult: { isDelivered: true }, msgCalls: [], messages: [], nextId: 1 };
  w.$ = {
    plugin: { root: '/plugin' },
    fs: {
      read: async (p) => { if (!files.has(p)) throw new Error('ENOENT'); return files.get(p); },
      write: async (p, t) => { files.set(p, t); },
    },
    process: { run: async (argv) => ({ exitCode: 0, stdout: argv.includes('snapshot') || argv.includes('--snapshot') ? JSON.stringify(w.plan) : '{}', stderr: '' }) },
    agent: {
      spawn: async () => ({ model: 'sonnet', agentId: `ag${w.nextId++}` }),
      list: async () => w.list,
    },
    session: {
      append: async () => {},
      send: async (a) => { w.sent.push(a); return w.sendResult; },
      messages: async (a) => { w.msgCalls.push(a.agentId); return w.messages; },
    },
    prompt: { submit: async () => {} },
  };
  w.driver = () => createDriver(w.$, { cwd: '/proj', sessionId: 's1', waitTries: 1, waitMs: 0, sleep: async () => {} });
  return w;
}

const blocksOf = (d) => { d.press('tab:agents'); return d.panel().blocks; };
const agentsBlock = (d) => blocksOf(d).find((b) => b.type === 'agents');
const detailBlock = (d) => blocksOf(d).find((b) => b.type === 'agent');

test('lists nxy and foreign agents; cost shows when the turn ends', async () => {
  const w = world();
  const d = w.driver();
  await d.step(); // takes over: spawns the implementer
  w.list = [
    { id: 'ag1', status: 'running', type: 'nxy:implementer' },
    { id: 'ext1', status: 'running', type: 'Explore', description: 'buscar' },
  ];
  await d.syncAgents();
  let rows = agentsBlock(d).agents;
  assert.deepEqual(rows.map((r) => r.id).sort(), ['ag1', 'ext1']);
  const impl = rows.find((r) => r.id === 'ag1');
  assert.equal(impl.role, 'implementer');
  assert.equal(impl.costText, '…');
  assert.equal(impl.batch, 1);
  assert.equal(d.agentsLive(), true);
  d.noteAgentTurn({ agentId: 'ag1', usage: { model: 'sonnet', input_tokens: 1_000_000, output_tokens: 0 }, answer: 'listo', reason: 'end_turn' });
  w.list = [{ id: 'ag1', status: 'completed' }, { id: 'ext1', status: 'running' }];
  await d.syncAgents();
  rows = agentsBlock(d).agents;
  assert.equal(rows.find((r) => r.id === 'ag1').costText, '$3.00');
  assert.equal(rows.find((r) => r.id === 'ag1').live, false);
});

test('select shows the detail; a message to a finished agent resumes it', async () => {
  const w = world({ approved: false, hash: '' });
  const d = w.driver();
  await d.start();
  w.list = [{ id: 'x1', status: 'completed', type: 'general-purpose', description: 'hizo algo' }];
  await d.press('agent:x1');
  assert.equal(detailBlock(d).id, 'x1');
  await d.submitMessage('seguí con esto');
  assert.deepEqual(w.sent[0], { to: { agentId: 'x1' }, text: 'seguí con esto' });
  const b = detailBlock(d);
  assert.equal(b.sent[0].state, 'en cola');
  assert.equal(b.card.live, true, 'optimistic running until the next list');
  d.noteTool('x1', { tool: 'Edit', input: { file_path: 'a.js' } });
  assert.equal(detailBlock(d).sent[0].state, 'recibido');
  await d.press('agent-back');
  assert.equal(detailBlock(d), undefined);
});

test('a refused delivery is visible; a thrown send too', async () => {
  const w = world({ approved: false, hash: '' });
  const d = w.driver();
  await d.start();
  w.list = [{ id: 'x1', status: 'completed' }];
  await d.press('agent:x1');
  w.sendResult = { isDelivered: false, reason: 'agente cerrado' };
  await d.submitMessage('hola');
  w.$.session.send = async () => { throw new Error('boom'); };
  await d.submitMessage('otra');
  const sent = detailBlock(d).sent;
  assert.match(sent[0].state, /^no entregado/);
  assert.match(sent[0].state, /agente cerrado/);
  assert.match(sent[1].state, /boom/);
});

test('"en cola" becomes "recibido" with the next agent turn', async () => {
  const w = world({ approved: false, hash: '' });
  const d = w.driver();
  await d.start();
  w.list = [{ id: 'x1', status: 'completed' }];
  await d.press('agent:x1');
  await d.submitMessage('hola');
  d.noteAgentTurn({ agentId: 'x1', usage: { model: 'sonnet', input_tokens: 10 }, answer: 'ok' });
  assert.equal(detailBlock(d).sent[0].state, 'recibido');
});

test('the fallback asks session.messages only for live agents the hook does not see, at most every 2 s', async () => {
  const w = world({ approved: false, hash: '' });
  const d = w.driver();
  await d.start();
  w.list = [
    { id: 'seen', status: 'running' }, { id: 'blind', status: 'running' }, { id: 'done', status: 'completed' },
  ];
  d.noteTool('seen', { tool: 'Read', input: { file_path: 'a' } });
  await d.press('agent:seen');
  await d.press('agent:blind');
  await d.press('agent:done');
  assert.deepEqual(w.msgCalls, ['blind'], 'only the live agent without hook events');
  await d.syncAgents();
  assert.deepEqual(w.msgCalls, ['blind'], 'not again within 2 s');
});

test('the fallback fills tools from messages and tolerates {deny}', async () => {
  const w = world({ approved: false, hash: '' });
  const d = w.driver();
  await d.start();
  w.list = [{ id: 'blind', status: 'running' }];
  w.messages = { deny: 'no' };
  await d.press('agent:blind');
  assert.match(detailBlock(d).card.activity, /^trabajando/);
  const d2 = w.driver();
  await d2.start();
  w.messages = [{ role: 'assistant', toolUses: [{ tool: 'Bash', input: { command: 'npm test' } }] }];
  await d2.press('agent:blind');
  assert.match(detailBlock(d2).card.activity, /^Bash: npm test/);
});

test('an implementer of the orchestrator waits for a yes', async () => {
  const w = world();
  const d = w.driver();
  await d.step();
  w.list = [{ id: 'ag1', status: 'running', type: 'nxy:implementer' }];
  await d.press('agent:ag1');
  await d.submitMessage('cambiá el enfoque');
  assert.equal(w.sent.length, 0);
  await d.press('confirm-no');
  assert.equal(w.sent.length, 0);
  await d.submitMessage('cambiá el enfoque');
  await d.press('confirm-yes');
  assert.equal(w.sent.length, 1);
  assert.equal(w.sent[0].to.agentId, 'ag1');
});

test('selecting another agent drops a pending confirmation', async () => {
  const w = world();
  const d = w.driver();
  await d.step();
  w.list = [{ id: 'ag1', status: 'running', type: 'nxy:implementer' }, { id: 'ag2', status: 'running', type: 'general-purpose' }];
  await d.press('agent:ag1');
  await d.submitMessage('cambiá el enfoque');
  await d.press('agent:ag2');
  assert.equal(d.panel().ask ?? null, null, 'no question left open');
  await d.press('confirm-yes');
  assert.equal(w.sent.length, 0);
});

test('an unknown id with tool events does not make agentsLive() true', async () => {
  const w = world({ approved: false, hash: '' });
  const d = w.driver();
  await d.start();
  d.noteTool('ghost', { tool: 'Read', input: { file_path: 'a' } });
  assert.equal(d.agentsLive(), false);
});

test('the session.messages throttle is forgotten once the agent finishes', async () => {
  const w = world({ approved: false, hash: '' });
  const d = w.driver();
  await d.start();
  w.list = [{ id: 'blind', status: 'running' }];
  await d.press('agent:blind');
  w.list = [{ id: 'blind', status: 'completed' }];
  await d.syncAgents();
  w.list = [{ id: 'blind', status: 'running' }];
  await d.syncAgents();
  assert.deepEqual(w.msgCalls, ['blind', 'blind'], 'no stale 2 s throttle after it ended');
});

test('the book caps at 40 agents', async () => {
  const w = world({ approved: false, hash: '' });
  const d = w.driver();
  await d.start();
  w.list = Array.from({ length: 55 }, (_, i) => ({ id: `a${i}`, status: 'completed' }));
  await d.syncAgents();
  await d.press('agents-all');
  assert.equal(agentsBlock(d).agents.length, 40);
});
