import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createDriver } from '../hosts/claude-code/mod/driver.mjs';
import { panelText as lines } from '../core/panel.mjs';

const panelText = (/** @type {any} */ p) => /** @type {any[]} */ ([]).concat(lines(p)).join('\n');

/** A fake `$`: process.run scripted by entry name, agent.spawn recorded. No real node, no real spawn. */
function world() {
  /** @type {{ runs: any[], spawns: any[], nextId: number, fail: boolean, denySpawn: string | null, outs: Record<string, any>, $?: any, driver?: any }} */
  const w = { runs: [], spawns: [], nextId: 1, fail: false, denySpawn: null, outs: {} };
  const snap = { ok: true, runtimeDir: '/rt/nxy', projectDir: '/proj', branch: 'main', config: { orchestrator: 'auto', roles: { scout: { model: 'haiku', effort: 'low' }, librarian: { model: 'sonnet' } } } };
  w.$ = {
    plugin: { root: '/plugin' },
    fs: { read: async () => { throw new Error('ENOENT'); }, write: async () => {} },
    process: {
      run: async (argv) => {
        w.runs.push(argv);
        if (w.fail) throw new Error('boom');
        if (argv.includes('--snapshot') || argv.includes('snapshot')) return { exitCode: 0, stdout: JSON.stringify(snap), stderr: '' };
        const name = argv.find((a) => /\/entries\/\w+\.mjs$/.test(a))?.split('/').pop().replace('.mjs', '');
        const sub = argv[argv.indexOf(argv.find((a) => /\/entries\//.test(a))) + 1];
        const out = w.outs[`${name}:${sub}`] ?? w.outs[name] ?? '{}';
        return { exitCode: 0, stdout: typeof out === 'string' ? out : JSON.stringify(out), stderr: '' };
      },
    },
    agent: {
      spawn: async (input) => {
        if (w.denySpawn) return { deny: w.denySpawn };
        const agentId = `ag${w.nextId++}`;
        w.spawns.push({ ...input, agentId });
        return { model: 'claude-haiku', agentId };
      },
    },
  };
  w.driver = async () => { const d = createDriver(w.$, { cwd: '/proj', sessionId: 's1' }); await d.start(); return d; };
  return w;
}

const MEM = { id: 'dec-1', scope: 'repo', area: 'core', type: 'decision', title: 'Use SQLite', updated: '2026-01-01', keywords: ['db'] };

test('search runs mem search with the trimmed query plus --json and --cwd, and lists the result title', async () => {
  const w = world();
  w.outs['mem:search'] = { ok: true, query: 'sqlite', results: [MEM] };
  const d = await w.driver();
  await d.press('tab:memory');
  await d.submitInput('mem-search', ' sqlite ');
  const run = w.runs.find((r) => r.includes('search'));
  assert.ok(run && run.includes('sqlite') && run.includes('--json') && run.includes('--cwd'));
  assert.match(panelText(d.panel()), /Use SQLite/);
});

test('first visit to Memoria loads recent and the handoff; a memory opens and goes back', async () => {
  const w = world();
  w.outs['mem:list'] = { ok: true, query: '', results: [MEM] };
  w.outs['mem:handoff'] = { ok: true, exists: true, label: 'main', body: 'hand text', progress: '' };
  w.outs['mem:get'] = { ok: true, memory: { ...MEM, body: 'the body of the memory', private: false }, edges: [], files: [] };
  const d = await w.driver();
  await d.press('tab:memory');
  assert.ok(w.runs.some((r) => r.includes('list') && r.includes('10')));
  assert.ok(w.runs.some((r) => r.includes('handoff') && r.includes('show')));
  const n = w.runs.length;
  await d.press('tab:memory');
  assert.equal(w.runs.length, n, 'the second visit does not reload');
  await d.press('mem:dec-1');
  assert.match(panelText(d.panel()), /the body of the memory/);
  await d.press('mem-back');
  assert.match(panelText(d.panel()), /Use SQLite/);
});

test('locate shows the hits; an unparsable stdout is an error state, not a throw', async () => {
  const w = world();
  w.outs.locate = { ok: true, question: 'where', hits: [{ path: 'core/a.mjs', line: 7, kind: 'function', name: 'foo', source: 'index' }], terms: [], degraded: [], ms: 12, codegraph: 'x' };
  const d = await w.driver();
  await d.press('tab:memory');
  await d.submitInput('locate', 'where is foo');
  assert.match(panelText(d.panel()), /core\/a\.mjs:7/);
  w.outs.locate = 'not json at all';
  await d.submitInput('locate', 'again');
  assert.doesNotMatch(panelText(d.panel()), /core\/a\.mjs:7/);
});

test('a failing run shows the error', async () => {
  const w = world();
  const d = await w.driver();
  await d.press('tab:memory');
  w.fail = true;
  await d.submitInput('mem-search', 'x');
  assert.match(panelText(d.panel()), /boom/);
});

test('scout launch: spawn args, model from config, shows in Agentes, answer shows as hits', async () => {
  const w = world();
  const d = await w.driver();
  await d.submitInput('launch', 'where is the gate?');
  assert.equal(w.spawns.length, 1);
  const s = w.spawns[0];
  assert.equal(s.subagentType, 'nxy:scout');
  assert.equal(s.model, 'haiku');
  assert.equal(s.prompt, 'where is the gate?');
  assert.match(s.description, /^nxy-panel: /);
  await d.press('tab:agents');
  assert.match(panelText(d.panel()), /● scout · claude-haiku · corriendo/);
  d.noteAgentTurn({ agentId: s.agentId, answer: '`core/gate.mjs:42` evaluateGate decides' });
  assert.match(panelText(d.panel()), /core\/gate\.mjs:42/);
});

test('librarian takes its own role model; a denied spawn shows its message', async () => {
  const w = world();
  const d = await w.driver();
  await d.press('launch:librarian');
  await d.submitInput('launch', 'what did we decide');
  assert.equal(w.spawns[0].subagentType, 'nxy:librarian');
  assert.equal(w.spawns[0].model, 'sonnet');
  const w2 = world();
  w2.denySpawn = 'nope';
  const d2 = await w2.driver();
  await d2.submitInput('launch', 'q');
  await d2.press('tab:agents');
  assert.match(panelText(d2.panel()), /nope/);
});

test('a second launch while one runs is ignored; it works again once it ends', async () => {
  const w = world();
  const d = await w.driver();
  await d.submitInput('launch', 'one');
  await d.submitInput('launch', 'two');
  assert.equal(w.spawns.length, 1);
  d.noteAgentTurn({ agentId: w.spawns[0].agentId, answer: 'nothing parseable' });
  await d.submitInput('launch', 'three');
  assert.equal(w.spawns.length, 2);
});

test('launch-ask spawns the scout with the last locate question', async () => {
  const w = world();
  w.outs.locate = { ok: true, question: 'where is foo', hits: [{ path: 'core/a.mjs', line: 7, kind: 'function', name: 'foo', source: 'index' }], terms: [], degraded: [], ms: 5, codegraph: 'x' };
  const d = await w.driver();
  await d.press('tab:memory');
  await d.submitInput('locate', 'where is foo');
  await d.press('launch-ask');
  assert.equal(w.spawns.length, 1);
  assert.equal(w.spawns[0].subagentType, 'nxy:scout');
  assert.equal(w.spawns[0].prompt, 'where is foo');
  assert.equal(w.spawns[0].model, 'haiku');
});

test('mem:<id> pressed from Agentes goes to Memoria and shows the detail', async () => {
  const w = world();
  w.outs['mem:get'] = { ok: true, memory: { ...MEM, body: 'detail from agentes', private: false }, edges: [], files: [] };
  const d = await w.driver();
  await d.press('tab:agents');
  await d.press('mem:dec-1');
  assert.match(panelText(d.panel()), /detail from agentes/);
});

test('refresh with a memory open reloads it and keeps it open', async () => {
  const w = world();
  w.outs['mem:get'] = { ok: true, memory: { ...MEM, body: 'first body', private: false }, edges: [], files: [] };
  const d = await w.driver();
  await d.press('tab:memory');
  await d.press('mem:dec-1');
  w.outs['mem:get'] = { ok: true, memory: { ...MEM, body: 'second body', private: false }, edges: [], files: [] };
  await d.press('refresh');
  assert.match(panelText(d.panel()), /second body/);
});

test('a launch whose answer lands right after the spawn shows the result', async () => {
  const w = world();
  const orig = w.$.agent.spawn;
  /** @type {any} */ let dd;
  w.$.agent.spawn = async (input) => {
    const res = await orig(input);
    dd.noteSpawn({ id: res.agentId, type: input.subagentType, description: input.description, prompt: input.prompt, spawnedBy: 'nxy' });
    dd.noteAgentTurn({ agentId: res.agentId, answer: '`core/gate.mjs:42` evaluateGate decides' });
    return res;
  };
  dd = await w.driver();
  await dd.submitInput('launch', 'where is the gate?');
  await dd.press('tab:agents');
  assert.match(panelText(dd.panel()), /core\/gate\.mjs:42/);
});

test('a duplicate noteSpawn leaves one row in Agentes', async () => {
  const w = world();
  const d = await w.driver();
  await d.submitInput('launch', 'where is the gate?');
  const s = w.spawns[0];
  d.noteSpawn({ id: s.agentId, type: 'nxy:scout', description: s.description, prompt: s.prompt, spawnedBy: 'nxy' });
  await d.press('tab:agents');
  const n = panelText(d.panel()).split('\n').filter((l) => /^● scout · /.test(l)).length; // list rows are unindented; the launch card is indented
  assert.equal(n, 1);
});

test('empty text is a no-op and a submit bumps the input key', async () => {
  const w = world();
  const d = await w.driver();
  await d.press('tab:memory');
  const n = w.runs.length;
  await d.submitInput('mem-search', '   ');
  await d.submitInput('launch', '');
  await d.submitInput('locate', undefined);
  assert.equal(w.runs.length, n);
  assert.equal(w.spawns.length, 0);
  const before = JSON.stringify(d.panel());
  w.outs['mem:search'] = { ok: true, query: 'q', results: [] };
  await d.submitInput('mem-search', 'q');
  assert.notEqual(JSON.stringify(d.panel()), before);
  const key = (p) => JSON.stringify(p).match(/"key":"mem-search:(\d+)"/)?.[1];
  assert.equal(key(d.panel()), '1');
});
