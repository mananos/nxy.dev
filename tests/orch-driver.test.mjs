import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createDriver } from '../hosts/claude-code/mod/driver.mjs';
import { ticketOf, MARKER_TTL_MS } from '../core/orchestrator.mjs';

const RT = '/rt/nxy'; // a made-up runtime dir: the fake fs never touches the disk
const HASH = 'abc12345';

/** A fake `$`: in-memory fs, recorded spawn/append/submit, and a scripted snapshot. */
function world(over = {}) {
  const files = new Map();
  /** @type {any} */
  const plan = {
    ok: true, runtimeDir: RT, projectDir: '/proj', branch: 'main',
    config: { pauseAfterBatch: false, orchestrator: 'auto' },
    hash: HASH, approved: true, fresh: true, questions: 0,
    batches: [
      { n: 1, depends: [], text: 'Batch 1 — one\nAccept: `x`', accept: { command: 'x' } },
      { n: 2, depends: [1], text: 'Batch 2 — two\nAccept: `x`', accept: { command: 'x' } },
      { n: 3, depends: [1], text: 'Batch 3 — three\nAccept: `x`', accept: { command: 'x' } },
    ],
    verdicts: {}, suite: null, review: null, reviewNeeded: { needed: true, reason: '' },
    ...over,
  };
  /** @type {any} */
  const w = { files, plan, spawns: [], appended: [], submitted: [], runs: [], failRun: false, nextId: 1, denySpawn: null };
  w.$ = {
    plugin: { root: '/plugin' },
    fs: {
      read: async (p) => { if (!files.has(p)) throw new Error('ENOENT'); return files.get(p); },
      write: async (p, t) => { files.set(p, t); },
    },
    process: {
      run: async (argv) => {
        w.runs.push(argv);
        if (w.failRun) throw new Error('boom');
        return { exitCode: 0, stdout: argv.includes('snapshot') ? JSON.stringify(w.plan) : '{}', stderr: '' };
      },
    },
    agent: {
      spawn: async (input) => {
        if (w.denySpawn) return { deny: w.denySpawn };
        const agentId = `ag${w.nextId++}`;
        w.spawns.push({ ...input, agentId });
        return { model: 'm', agentId };
      },
    },
    session: { append: async (a) => { w.appended.push(a); } },
    prompt: { submit: async (a) => { w.submitted.push(a); } },
  };
  w.driver = (o = {}) => createDriver(w.$, { cwd: '/proj', sessionId: 's1', waitTries: 1, waitMs: 0, sleep: async () => {}, ...o });
  w.state = () => JSON.parse(files.get(`${RT}/orch/state.json`));
  w.marker = () => JSON.parse(files.get(`${RT}/orch/active.json`));
  /** Records the verdict the SubagentStop hook would have written, after the launch. */
  w.verdict = (n, status, extra = {}) => { w.plan.verdicts[String(n)] = { status, ts: Date.now() + 5, ...extra }; w.plan.fresh = false; };
  w.agentOf = (role, batch) => w.spawns.filter((s) => s.subagentType === `nxy:${role}` && (batch == null || s.description.endsWith(`batch ${batch}`))).at(-1).agentId;
  return w;
}

test('Approve: takes over a fresh approved plan and spawns batch 1 with a ticket', async () => {
  const w = world();
  await w.driver().step();
  assert.equal(w.spawns.length, 1);
  const s = w.spawns[0];
  assert.equal(s.subagentType, 'nxy:implementer');
  assert.match(s.prompt, /^Batch 1 — one/);
  const t = ticketOf(s.description);
  assert.ok(t, 'the description is a sentinel');
  assert.ok(w.files.has(`${RT}/orch/tickets/${t.nonce}`), 'the ticket file exists');
  assert.equal(w.marker().hash, HASH);
  assert.ok(Date.now() - w.marker().ts < MARKER_TTL_MS);
  assert.ok(w.state().launched['1']);
});

test('parallel batches after 1 passes, then tester, reviewer and hand-back with the report', async () => {
  const w = world();
  const d = w.driver();
  await d.step();
  w.verdict(1, 'pass');
  await d.onAgentDone(w.agentOf('implementer', 1), 'done');
  assert.deepEqual(w.spawns.slice(1).map((s) => s.description.split(' ').slice(1).join(' ')), ['batch 2', 'batch 3']);
  w.verdict(2, 'pass');
  await d.onAgentDone(w.agentOf('implementer', 2), 'done');
  assert.equal(w.spawns.length, 3, 'batch 3 still running: wait');
  w.verdict(3, 'pass');
  await d.onAgentDone(w.agentOf('implementer', 3), 'done');
  const tester = w.spawns.at(-1);
  assert.equal(tester.subagentType, 'nxy:tester');
  assert.equal(tester.prompt, `Full suite for nxy plan ${HASH}`);
  w.plan.suite = { status: 'done', ts: 1, red: [] };
  await d.onAgentDone(tester.agentId, 'suite green');
  const reviewer = w.spawns.at(-1);
  assert.equal(reviewer.subagentType, 'nxy:reviewer');
  assert.equal(reviewer.prompt, `Review nxy plan ${HASH} (project: /proj)`);
  w.plan.review = { id: 'r1' };
  await d.onAgentDone(reviewer.agentId, 'REPORT: finding F1');
  assert.equal(w.submitted.length, 1);
  assert.match(w.submitted[0].text, /checkpoint 2/);
  assert.match(w.submitted[0].text, /REPORT: finding F1/);
  assert.equal(w.appended.length, 1);
  assert.equal(w.marker().ts, 0, 'marker released');
  await d.step();
  assert.equal(w.spawns.length, 5, 'a finished plan is never taken over again');
});

test('a red batch asks; Retry re-spawns with the failure', async () => {
  const w = world();
  const d = w.driver();
  await d.step();
  w.verdict(1, 'fail', { error: 'two tests broke' });
  await d.onAgentDone(w.agentOf('implementer', 1), 'x');
  assert.equal(w.spawns.length, 1);
  assert.deepEqual(d.view().buttons, ['Retry', 'Continue anyway', 'Stop']);
  await d.press('Retry');
  assert.equal(w.spawns.length, 2);
  assert.match(w.spawns[1].prompt, /^Batch 1 — one/);
  assert.match(w.spawns[1].prompt, /two tests broke/);
  assert.ok(ticketOf(w.spawns[1].description));
});

test('Continue anyway writes continued and unlocks the dependents', async () => {
  const w = world();
  const d = w.driver();
  await d.step();
  w.verdict(1, 'fail', { error: 'e' });
  await d.onAgentDone(w.agentOf('implementer', 1), 'x');
  await d.press('Continue anyway');
  assert.deepEqual(w.state().continued, [1]);
  assert.equal(w.spawns.length, 3);
});

test('pauseAfterBatch waits for Continue', async () => {
  const w = world({ config: { pauseAfterBatch: true, orchestrator: 'auto' } });
  const d = w.driver();
  await d.step();
  w.verdict(1, 'pass');
  await d.onAgentDone(w.agentOf('implementer', 1), 'x');
  assert.equal(w.spawns.length, 1);
  assert.deepEqual(d.view().buttons, ['Continue', 'Adjust', 'Stop']);
  await d.press('Continue');
  assert.equal(w.spawns.length, 3);
  assert.deepEqual(w.state().acked, [1]);
});

test('Adjust and Stop hand back and clear the marker', async () => {
  for (const [button, re] of /** @type {[string, RegExp][]} */ ([['Adjust', /adjust/], ['Stop', /stopped/]])) {
    const w = world({ config: { pauseAfterBatch: true, orchestrator: 'auto' } });
    const d = w.driver();
    await d.step();
    w.verdict(1, 'pass');
    await d.onAgentDone(w.agentOf('implementer', 1), 'x');
    await d.press(button);
    assert.equal(w.submitted.length, 1);
    assert.match(w.submitted[0].text, re);
    assert.equal(w.marker().ts, 0);
    assert.ok(w.runs.some((a) => a.includes('release')));
    assert.equal(w.spawns.length, 1);
  }
});

test('a verdict that never lands is treated as not-run', async () => {
  const w = world();
  const d = w.driver();
  await d.step();
  await d.onAgentDone(w.agentOf('implementer', 1), 'x');
  assert.equal(w.spawns.length, 1);
  assert.deepEqual(d.view().buttons, ['Retry', 'Continue anyway', 'Stop']);
});

test('headless: a red batch hands back with the afterBatch text instead of asking', async () => {
  const w = world();
  const d = w.driver({ headless: true });
  await d.step();
  w.verdict(1, 'fail', { error: 'e' });
  await d.onAgentDone(w.agentOf('implementer', 1), 'x');
  assert.equal(w.submitted.length, 1);
  assert.match(w.submitted[0].text, /batch 1 ✘/);
  assert.equal(w.marker().ts, 0);
});

test('a plan already under way is not taken over; orchestrator off does nothing', async () => {
  const under = world({ fresh: false, verdicts: { 1: { status: 'pass', ts: 1 } } });
  await under.driver().step();
  assert.equal(under.spawns.length, 0);
  assert.equal(under.files.size, 0);

  const unapproved = world({ approved: false, fresh: false });
  await unapproved.driver().step();
  assert.equal(unapproved.spawns.length, 0);

  const off = world({ config: { pauseAfterBatch: false, orchestrator: 'off' } });
  await off.driver().step();
  assert.equal(off.spawns.length, 0);
  assert.equal(off.files.size, 0);
});

test('a throwing process.run releases the marker', async () => {
  const w = world();
  const d = w.driver();
  await d.step();
  assert.ok(w.marker().ts > 0);
  w.failRun = true;
  await d.step();
  assert.equal(w.marker().ts, 0);
  assert.equal(w.submitted.length, 1);
  assert.match(w.submitted[0].text, /orchestrator failed while running plan abc12345/);
  assert.match(w.submitted[0].text, /Left: 1, 2, 3/);
  assert.equal(w.appended.length, 1);
  await d.step();
  assert.equal(w.submitted.length, 1, 'a second failure does not hand back again');
});

test('a refused spawn records the batch as not run', async () => {
  const w = world();
  w.denySpawn = 'gate says no';
  const d = w.driver();
  await d.step();
  assert.equal(w.spawns.length, 0);
  assert.deepEqual(d.view().buttons, ['Retry', 'Continue anyway', 'Stop']);
});

test('mod files have no node: import and no dynamic import', () => {
  const dirPath = join(dirname(fileURLToPath(import.meta.url)), '..', 'hosts', 'claude-code', 'mod');
  for (const f of readdirSync(dirPath).filter((n) => n.endsWith('.mjs'))) {
    const src = readFileSync(join(dirPath, f), 'utf8');
    assert.doesNotMatch(src, /from\s+['"]node:/, f);
    assert.doesNotMatch(src, /import\s*\(/, f);
  }
});
