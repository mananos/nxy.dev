import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createDriver } from '../hosts/claude-code/mod/driver.mjs';
import { ticketOf, MARKER_TTL_MS } from '../core/orchestrator.mjs';
import { panelText } from '../core/panel.mjs';

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
        return { exitCode: 0, stdout: argv.includes('snapshot') || argv.includes('--snapshot') ? JSON.stringify(w.plan) : (w.out ?? '{}'), stderr: '' };
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
  assert.deepEqual(d.panel().ask.buttons.map((b) => b.id), ['Retry', 'Continue anyway', 'Stop']);
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
  assert.deepEqual(d.panel().ask.buttons.map((b) => b.id), ['Continue', 'Adjust', 'Stop']);
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
  assert.deepEqual(d.panel().ask.buttons.map((b) => b.id), ['Retry', 'Continue anyway', 'Stop']);
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
  assert.deepEqual(d.panel().ask.buttons.map((b) => b.id), ['Retry', 'Continue anyway', 'Stop']);
});

test('idle: turns and agent turns without an AskUserQuestion run no node and write nothing', async () => {
  const w = world();
  const d = w.driver();
  for (let i = 0; i < 5; i++) await d.step('turn');
  await d.onAgentDone('ag9', 'x');
  assert.equal(w.runs.length, 0);
  assert.equal(w.files.size, 0);
  assert.equal(w.spawns.length, 0);
});

test("step('ask') snapshots and takes over; later turns move on; a late approval is still seen", async () => {
  const w = world();
  const d = w.driver();
  await d.step('ask');
  assert.equal(w.runs.filter((a) => a.includes('snapshot')).length, 1);
  assert.equal(w.spawns.length, 1);
  w.verdict(1, 'pass');
  await d.step('turn');
  assert.equal(w.spawns.length, 3, 'a main turn moves an owned plan on');

  const late = world({ fresh: false });
  const l = late.driver();
  await l.step('ask');
  assert.equal(late.spawns.length, 0);
  late.plan.fresh = true;
  await l.step('turn');
  assert.equal(late.spawns.length, 1);
});

test('status(): ready, running, paused, ready after the hand-back', async () => {
  const w = world({ config: { pauseAfterBatch: true, orchestrator: 'auto' } });
  const d = w.driver();
  assert.equal(d.status(), '● ready');
  await d.step();
  assert.equal(d.status(), '▶ batch 1/3');
  w.verdict(1, 'pass');
  await d.onAgentDone(w.agentOf('implementer', 1), 'x');
  assert.match(d.status(), /^⏸ /);
  await d.press('Stop');
  assert.equal(d.status(), '● ready');
});

const text = (d) => panelText(d.panel());
const tabOf = (d) => d.panel().tabs.find((t) => t.active).id;

test('panel() before any snapshot uses setConfig and setUsage and runs no node', () => {
  const w = world();
  const d = w.driver();
  d.setConfig({ panel: 'off', gate: { enabled: true, contextTokens: 100000 } });
  d.setUsage({ tokens: 46000, percent: 46 });
  const t = text(d);
  assert.ok(t.some((l) => l.startsWith('Plan: sin leer')));
  assert.ok(t.some((l) => l.startsWith('Contexto: 46k / 100k')));
  assert.equal(w.runs.length, 0);
  assert.equal(d.panelSetting(), 'off');
});

test('openPanel reads the plan: none, then a plan; panelSetting follows the snapshot', async () => {
  const none = world({ hash: null, approved: false, fresh: false, batches: [] });
  const dn = none.driver();
  dn.setConfig({ panel: 'off' });
  await dn.openPanel();
  assert.ok(text(dn).includes('Plan: ninguno'));
  assert.equal(dn.panelSetting(), 'off', 'the snapshot has no ui.panel: the fallback stays');

  const w = world({ config: { pauseAfterBatch: false, orchestrator: 'auto', ui: { panel: 'auto' } } });
  const d = w.driver();
  d.setConfig({ panel: 'off' });
  await d.openPanel();
  assert.ok(text(d).some((l) => l.startsWith(`Plan: ${HASH}`)));
  assert.equal(d.panelSetting(), 'auto');
});

test('start(): one node run fills the panel; a plain release runs no snapshot; a failure leaves it empty', async () => {
  const w = world();
  const d = w.driver();
  await d.start();
  assert.equal(w.runs.length, 1);
  assert.ok(w.runs[0].includes('release') && w.runs[0].includes('--snapshot'));
  assert.deepEqual(w.runs[0].slice(w.runs[0].indexOf('--session'), w.runs[0].indexOf('--session') + 2), ['--session', 's1']);
  assert.ok(text(d).some((l) => l.startsWith(`Plan: ${HASH}`)));
  assert.equal(w.spawns.length, 0, 'start() never takes a plan over');

  const p = world();
  await p.driver().release();
  assert.ok(p.runs.length === 1 && !p.runs[0].includes('--snapshot'));

  const bad = world();
  bad.failRun = true;
  const b = bad.driver();
  await b.start();
  assert.ok(text(b).some((l) => l.startsWith('Plan: sin leer')));
});

test('a tab press runs no node and changes the tab', async () => {
  const w = world();
  const d = w.driver();
  await d.start();
  const before = w.runs.length;
  assert.equal(tabOf(d), 'home');
  await d.press('tab:plan');
  assert.equal(tabOf(d), 'plan');
  assert.equal(w.runs.length, before);
});

const REVIEW = (id) => ({
  id, ts: 1, chosen: [],
  findings: [
    { id: 'R1', severity: 'high', lens: 'x', file: 'a.mjs', line: 3, title: 'one', cause: 'c' },
    { id: 'R2', severity: 'low', lens: 'x', file: 'b.mjs', line: 4, title: 'two', cause: 'c' },
  ],
});
const block = (d, type) => d.panel().blocks.find((b) => b.type === type);

test('batch:N opens and closes a batch, picks tick findings; none of it runs node', async () => {
  const w = world({ reviewDetail: REVIEW('rv1'), approved: true, fresh: false });
  const d = w.driver();
  await d.start();
  await d.press('tab:plan');
  const before = w.runs.length;
  await d.press('batch:2');
  assert.equal(block(d, 'batches').items.find((i) => i.n === 2).open, true);
  await d.press('batch:2');
  assert.equal(block(d, 'batches').items.find((i) => i.n === 2).open, false);
  await d.press('pick:R1');
  assert.deepEqual(block(d, 'checkpoint').findings.filter((f) => f.picked).map((f) => f.id), ['R1']);
  await d.press('pick:R1');
  assert.equal(block(d, 'checkpoint').picked, 0);
  assert.equal(w.runs.length, before);
});

test('fix-picked submits the ticked ids once; with none ticked it does nothing', async () => {
  const w = world({ reviewDetail: REVIEW('rv1'), fresh: false });
  const d = w.driver();
  await d.start();
  await d.press('fix-picked');
  assert.equal(w.submitted.length, 0);
  await d.press('pick:R1');
  await d.press('pick:R2');
  await d.press('fix-picked');
  assert.equal(w.submitted.length, 1);
  assert.equal(w.submitted[0].text, 'Arreglar los hallazgos R1, R2 del review rv1 (marcados en el panel de nxy)');
});

test('the ticks are cleared when another review shows up', async () => {
  const w = world({ reviewDetail: REVIEW('rv1'), fresh: false });
  const d = w.driver();
  await d.start();
  await d.press('tab:plan');
  await d.press('pick:R1');
  assert.equal(block(d, 'checkpoint').picked, 1);
  w.plan.reviewDetail = REVIEW('rv2');
  await d.refresh();
  assert.equal(block(d, 'checkpoint').picked, 0);
  await d.press('fix-picked');
  assert.equal(w.submitted.length, 0);
});

test('launcher buttons run the entry with --cwd and show the output in the panel only', async () => {
  const w = world();
  const d = w.driver();
  w.out = 'gate line one';
  await d.press('gate-once');
  assert.equal(w.runs.length, 1);
  assert.match(w.runs[0].find((a) => a.endsWith('gate.mjs')), /entries[\\/]gate\.mjs$|entries\/gate\.mjs$/);
  assert.deepEqual(w.runs[0].slice(-3), ['once', '--cwd', '/proj']);
  assert.deepEqual(d.panel().output.lines, ['gate line one']);
  assert.equal(d.panel().output.title, 'Gate once');
  assert.equal(w.appended.length, 0);
  assert.equal(w.submitted.length, 0);

  w.out = 'the handoff';
  await d.press('handoff');
  assert.deepEqual(w.runs[1].slice(-4), ['handoff', 'show', '--cwd', '/proj']);
  assert.deepEqual(d.panel().output.lines, ['the handoff']);

  await d.press('dismiss');
  assert.equal(d.panel().output, null);

  w.failRun = true;
  await d.press('gate-once');
  assert.ok(d.panel().output.lines.some((l) => l.includes('Gate once failed: boom')));
});

test('a slow launcher does not delay the orchestrator, and a second press is ignored', async () => {
  const w = world();
  const d = w.driver();
  /** @type {() => void} */
  let release = () => {};
  const held = new Promise((r) => { release = () => r(undefined); });
  const inner = w.$.process.run;
  w.$.process.run = async (argv) => {
    if (argv.some((a) => a.endsWith('gate.mjs'))) { w.runs.push(argv); await held; return { stdout: 'gate done', stderr: '' }; }
    return inner(argv);
  };
  const p = d.press('gate-once');
  assert.equal(d.panel().output.running, true);
  const again = d.press('gate-once');
  await d.step('ask');
  assert.equal(w.spawns.length, 1, 'the plan moved on while Gate runs');
  release();
  await p;
  await again;
  assert.equal(w.runs.filter((a) => a.some((x) => x.endsWith('gate.mjs'))).length, 1);
  assert.deepEqual(d.panel().output.lines, ['gate done']);
});

test('plan-done asks first; no before yes, one run plus a refresh after, cancel runs nothing', async () => {
  const w = world();
  const d = w.driver();
  await d.start();
  const before = w.runs.length;
  await d.press('plan-done');
  assert.equal(w.runs.length, before, 'nothing runs before yes');
  assert.deepEqual(d.panel().ask.buttons.map((b) => b.id), ['confirm-yes', 'confirm-no']);
  await d.press('confirm-no');
  assert.equal(d.panel().ask, null);
  assert.equal(w.runs.length, before);

  await d.press('plan-done');
  await d.press('confirm-yes');
  assert.equal(d.panel().ask, null);
  const added = w.runs.slice(before);
  assert.equal(added.length, 2);
  assert.deepEqual(added[0].slice(-4), ['handoff', 'done', '--cwd', '/proj']);
  assert.ok(added[1].includes('snapshot'));
  await d.step('ask');
  assert.equal(w.spawns.length, 0, 'the closed plan is not taken over again');
});

test('a plan taken over while Terminar plan waits for yes: the ask closes and yes runs nothing', async () => {
  const w = world();
  const d = w.driver();
  await d.start();
  await d.press('plan-done');
  assert.deepEqual(d.panel().ask.buttons.map((b) => b.id), ['confirm-yes', 'confirm-no']);
  await d.step('ask');
  assert.equal(w.spawns.length, 1, 'the orchestrator took the plan over');
  assert.equal(d.panel().ask, null, 'the pending yes/no is gone');
  const before = w.runs.length;
  await d.press('confirm-yes');
  await d.press('plan-done');
  await d.press('confirm-yes');
  assert.equal(w.runs.length, before, 'no handoff done while the plan is owned');
  assert.ok(!w.runs.some((a) => a.includes('done')));
  assert.equal(d.panel().ask, null);
  assert.ok(d.view(), "the plan is still the orchestrator's");
});

/** Holds the given entry's process until `release()`; other runs go through. */
function hold(w, entry) {
  /** @type {() => void} */
  let release = () => {};
  const held = new Promise((r) => { release = () => r(undefined); });
  const inner = w.$.process.run;
  w.$.process.run = async (argv) => {
    if (argv.some((a) => a.endsWith(`${entry}.mjs`))) { w.runs.push(argv); await held; return { stdout: `${entry} done`, stderr: '' }; }
    return inner(argv);
  };
  return () => release();
}

test('a launcher dismissed while it runs leaves no output when it ends', async () => {
  const w = world();
  const d = w.driver();
  const release = hold(w, 'gate');
  const p = d.press('gate-once');
  assert.equal(d.panel().output.running, true);
  await d.press('dismiss');
  assert.equal(d.panel().output, null);
  release();
  await p;
  assert.equal(d.panel().output, null);
});

test('a second launcher started while the first runs keeps its own output', async () => {
  const w = world();
  const d = w.driver();
  const release = hold(w, 'gate');
  w.out = 'handoff line';
  const first = d.press('gate-once');
  const second = d.press('handoff');
  assert.equal(d.panel().output.title, 'Handoff');
  assert.equal(d.panel().output.running, true);
  release();
  await first;
  assert.equal(d.panel().output.title, 'Handoff', 'the first run does not write over the second');
  await second;
  assert.equal(d.panel().output.title, 'Handoff');
  assert.deepEqual(d.panel().output.lines, ['handoff line']);
});

test('the filter toggle runs filter on|off, re-reads the snapshot and flips the label', async () => {
  const w = world();
  w.plan.config = { ...w.plan.config, filter: false };
  const d = w.driver();
  await d.start();
  assert.ok(text(d).some((l) => l.includes('Filter ○ off')));
  w.plan.config = { ...w.plan.config, filter: true };
  const before = w.runs.length;
  await d.press('filter');
  const added = w.runs.slice(before);
  assert.deepEqual(added[0].slice(-3), ['on', '--cwd', '/proj']);
  assert.ok(added[0].some((a) => a.endsWith('filter.mjs')));
  assert.ok(added[1].includes('snapshot'));
  assert.ok(text(d).some((l) => l.includes('Filter ● on')));
  w.plan.config = { ...w.plan.config, filter: false };
  await d.press('filter');
  assert.deepEqual(w.runs.at(-2).slice(-3), ['off', '--cwd', '/proj']);
});

test('the Plan tab opens when a plan is taken over, then the tab is the user\'s choice', async () => {
  const w = world();
  const d = w.driver();
  await d.start();
  assert.equal(tabOf(d), 'home');
  await d.step('ask');
  assert.equal(tabOf(d), 'plan');
  await d.press('tab:home');
  assert.equal(tabOf(d), 'home');
  w.verdict(1, 'pass');
  await d.step('turn');
  assert.equal(tabOf(d), 'home', 'not flipped again for the same plan');
});

test('batch:N closes the running batch that is open by default, and reopens it', async () => {
  const w = world();
  const d = w.driver();
  await d.step('ask');
  const open = () => block(d, 'batches').items.filter((i) => i.open).map((i) => i.n);
  assert.deepEqual(open(), [1], 'default: the running batch is open');
  await d.press('batch:1');
  assert.deepEqual(open(), [], 'closed');
  await d.press('batch:1');
  assert.deepEqual(open(), [1]);
});

test('panel() is read-only: it never changes the tab', async () => {
  const w = world();
  const d = w.driver();
  await d.start();
  await d.step('ask');
  await d.press('tab:home');
  d.panel(); d.panel();
  assert.equal(tabOf(d), 'home');
});

test('with a plan taken over view() is the orchestrator snapshot, and Refresh only refreshes', async () => {
  const w = world();
  const d = w.driver();
  await d.step();
  assert.ok(d.view());
  const before = w.spawns.length;
  await d.press('refresh');
  assert.equal(w.spawns.length, before);
  assert.ok(d.view());
});

const TURN = { input_tokens: 10, cache_read_input_tokens: 90, cache_creation_input_tokens: 0 };

test('noteTurn: only the main thread counts, and the cache shows hit, tokens and age', () => {
  const w = world();
  const d = w.driver();
  d.setUsage({ tokens: 100, percent: 1, model: 'claude-sonnet-4-6' });
  d.noteTurn({ usage: TURN, agentId: 'sub1', now: 1000 });
  assert.equal(d.cachePlan(1000), null, 'a subagent answer sets nothing');
  d.noteTurn({ usage: TURN, now: 5000, costUsd: 1 });
  d.noteTurn({ usage: TURN, now: 6000, costUsd: 1.25 });
  assert.equal(d.cachePlan(6000).expireInMs, 5 * 60_000);
});

test('the observed TTL beats settings and the inferred one', () => {
  const w = world();
  const d = w.driver();
  d.setTtl('1h', 'settings');
  d.setTtl('5m', 'observed');
  d.setTtl('1h', 'settings');
  d.noteTurn({ usage: TURN, now: 0 });
  d.noteTurnStart(19 * 60_000);
  d.noteTurn({ usage: TURN, now: 20 * 60_000 }); // survived a 19 min pause, but the TTL is observed
  assert.equal(d.cachePlan(20 * 60_000).expireInMs, 5 * 60_000);

  const d2 = w.driver();
  d2.noteTurn({ usage: TURN, now: 0 });
  d2.noteTurnStart(19 * 60_000);
  d2.noteTurn({ usage: TURN, now: 20 * 60_000 });
  assert.equal(d2.cachePlan(20 * 60_000).expireInMs, 60 * 60_000, 'with no observed TTL the pause lifts it to 1 h');
  d2.setTtl('5m', 'settings');
  assert.equal(d2.cachePlan(20 * 60_000).expireInMs, 60 * 60_000, 'settings never lowers an inferred 1 h');

  const d3 = w.driver();
  d3.setTtl('1h', 'settings');
  assert.equal(d3.cachePlan(0), null);
  d3.noteTurn({ usage: TURN, now: 0 });
  assert.equal(d3.cachePlan(0).expireInMs, 60 * 60_000, 'settings beats the default');
});

test('a long turn with no idle gap is not a pause; an idle gap is', () => {
  const w = world();
  const d = w.driver();
  d.noteTurn({ usage: TURN, now: 0 });
  d.noteTurnStart(30_000); // started right after the last answer, ran 20 min
  d.noteTurn({ usage: TURN, now: 20 * 60_000 });
  assert.equal(d.cachePlan(20 * 60_000).expireInMs, 5 * 60_000);
  d.noteTurn({ usage: TURN, now: 40 * 60_000 }); // no known start: nothing inferred
  assert.equal(d.cachePlan(40 * 60_000).expireInMs, 5 * 60_000);
  d.noteTurnStart(30 * 60_000 + 30_000 + 20 * 60_000); // 10 min idle after the 40 min answer
  d.noteTurn({ usage: TURN, now: 71 * 60_000 });
  assert.equal(d.cachePlan(71 * 60_000).expireInMs, 60 * 60_000);
});

test('start(): the snapshot TTL is observed, so a later settings value cannot override it', async () => {
  const w = world();
  w.plan.cacheTtl = '1h';
  const d = w.driver();
  await d.start();
  d.setTtl('5m', 'settings');
  d.noteTurn({ usage: TURN, now: 0 });
  assert.equal(d.cachePlan(0).expireInMs, 60 * 60_000);
});

test('cacheNotice: warn near the end, expire after it, nothing after a newer turn', () => {
  const w = world();
  const d = w.driver();
  d.setUsage({ tokens: 100, percent: 1, model: 'claude-sonnet-4-6' });
  w.plan.prices = { 'claude-sonnet-4-6': { input: 3, output: 15, cache_read: 0.3, cache_write_5m: 3.75, cache_write_1h: 6 } };
  d.noteTurn({ usage: { input_tokens: 0, cache_read_input_tokens: 1_000_000, cache_creation_input_tokens: 0 }, now: 0 });
  assert.equal(d.cacheNotice('warn', 60_000), null, 'not yet');
  assert.equal(d.cacheNotice('expire', 60_000), null);
  assert.match(d.cacheNotice('warn', 4 * 60_000 + 1000), /vence en 1 min/);
  assert.match(d.cacheNotice('expire', 5 * 60_000 + 1), /venció/);
  d.noteTurn({ usage: TURN, now: 5 * 60_000 + 2 });
  assert.equal(d.cacheNotice('expire', 5 * 60_000 + 3), null, 'a newer turn renewed it');
  assert.deepEqual(d.drainNotices(), []);
});

test('a model switch leaves the new model cold with the event cost', () => {
  const w = world();
  const d = w.driver();
  d.noteTurn({ usage: TURN, now: Date.now() });
  d.noteModelSwitch({ toModel: 'claude-opus-4-8', tokens: 386000, usd: 3.86, ttl: '1h' });
  assert.equal(d.cachePlan(Date.now()).expireInMs, 0);
  assert.match(d.cacheNotice('expire') ?? '', /386k/);
  assert.match(d.cacheNotice('expire') ?? '', /\$3\.86/);
});

/** Routes stats/trend/summary entries to scripted JSON. */
function routed(w) {
  const inner = w.$.process.run;
  w.json = { stats: { usage: { calls: 3 }, cacheTtl: '1h' }, trend: { periods: [{ period: '2026-10-01', usd: 1, totalTokens: 10 }] } };
  w.$.process.run = async (argv) => {
    const e = ['stats', 'trend'].find((x) => argv.some((a) => a.endsWith(`${x}.mjs`)));
    if (e) { w.runs.push(argv); return { stdout: JSON.stringify(w.json[e]), stderr: '' }; }
    return inner(argv);
  };
  const count = (name) => w.runs.filter((a) => a.some((x) => x.endsWith(`${name}.mjs`))).length;
  return count;
}

test('Stats tab loads once; metric runs no node, a new range does, the same one does not; r reloads', async () => {
  const w = world();
  const count = routed(w);
  const d = w.driver();
  await d.start();
  await d.press('tab:stats');
  assert.equal(count('stats'), 1);
  assert.equal(count('trend'), 1);
  const s = w.runs.find((a) => a.some((x) => x.endsWith('stats.mjs')));
  assert.deepEqual(s.slice(-5), ['--session', 's1', '--json', '--cwd', '/proj']);
  const t = w.runs.find((a) => a.some((x) => x.endsWith('trend.mjs')));
  assert.deepEqual(t.slice(-7), ['--since', '7d', '--by', 'day', '--json', '--cwd', '/proj']);
  assert.equal(d.panel().blocks.some((b) => b.type === 'chart'), true);

  await d.press('tab:home');
  await d.press('tab:stats');
  await d.press('metric:tok');
  assert.equal(count('stats'), 1);
  assert.equal(count('trend'), 1);
  await d.press('range:30d');
  assert.equal(count('trend'), 2);
  await d.press('range:7d');
  assert.equal(count('trend'), 2, 'cached combination');
  await d.press('here');
  assert.ok(w.runs.at(-1).includes('--here'));
  await d.press('refresh');
  assert.equal(count('stats'), 2);
  assert.equal(count('trend'), 4);
});

test('stats errors and empty answers become states, not exceptions', async () => {
  const w = world();
  routed(w);
  w.json.stats = { usage: { calls: 0 } };
  const d = w.driver();
  await d.start();
  await d.press('tab:stats');
  assert.equal(JSON.stringify(d.panel().blocks).includes('Sin datos'), true);
  const inner = w.$.process.run;
  w.$.process.run = async (argv) => { if (argv.some((a) => a.endsWith('trend.mjs'))) throw new Error('boom'); return inner(argv); };
  await d.press('by:week');
  assert.ok(JSON.stringify(d.panel().blocks).includes('boom'));
});

test('a closed plan runs summary once and the notice is drained once; summary-hide hides it', async () => {
  const w = world();
  const d = w.driver();
  await d.start();
  assert.equal(w.plan.approved, true);
  w.plan.hash = null;
  w.out = JSON.stringify({ ok: true, feature: { usage: { calls: 1 } }, session: { usage: { calls: 2 } }, approvedAt: 5, sinceKind: 'approval', cacheTtl: '5m' });
  await d.press('refresh');
  await d.press('refresh');
  const runs = w.runs.filter((a) => a.includes('summary'));
  assert.equal(runs.length, 1);
  assert.deepEqual(runs[0].slice(runs[0].indexOf('summary'), runs[0].indexOf('summary') + 3), ['summary', '--hash', HASH]);
  assert.equal(d.drainNotices().length, 1);
  assert.deepEqual(d.drainNotices(), []);
  await d.press('tab:stats');
  assert.ok(d.panel().blocks.some((b) => b.type === 'summary'));
  await d.press('summary-hide');
  assert.ok(!d.panel().blocks.some((b) => b.type === 'summary'));
});

test('status() carries no nxy prefix and no warning sign', async () => {
  const w = world();
  const d = w.driver();
  await d.step();
  assert.doesNotMatch(d.status() ?? '', /nxy|⚠/);
});

test('mod files have no node: import and no dynamic import', () => {
  const dirPath = join(dirname(fileURLToPath(import.meta.url)), '..', 'hosts', 'claude-code', 'mod');
  for (const f of readdirSync(dirPath).filter((n) => n.endsWith('.mjs'))) {
    const src = readFileSync(join(dirPath, f), 'utf8');
    assert.doesNotMatch(src, /from\s+['"]node:/, f);
    assert.doesNotMatch(src, /import\s*\(/, f);
  }
});
