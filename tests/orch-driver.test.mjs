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
  await d.press('trend');
  assert.ok(d.panel().output.lines.some((l) => l.includes('Trend failed: boom')));
});

test('a slow launcher does not delay the orchestrator, and a second press is ignored', async () => {
  const w = world();
  const d = w.driver();
  /** @type {() => void} */
  let release = () => {};
  const held = new Promise((r) => { release = () => r(undefined); });
  const inner = w.$.process.run;
  w.$.process.run = async (argv) => {
    if (argv.some((a) => a.endsWith('trend.mjs'))) { w.runs.push(argv); await held; return { stdout: 'trend done', stderr: '' }; }
    return inner(argv);
  };
  const p = d.press('trend');
  assert.equal(d.panel().output.running, true);
  const again = d.press('trend');
  await d.step('ask');
  assert.equal(w.spawns.length, 1, 'the plan moved on while Trend runs');
  release();
  await p;
  await again;
  assert.equal(w.runs.filter((a) => a.some((x) => x.endsWith('trend.mjs'))).length, 1);
  assert.deepEqual(d.panel().output.lines, ['trend done']);
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
  const release = hold(w, 'trend');
  const p = d.press('trend');
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
  const release = hold(w, 'trend');
  w.out = 'stats line';
  const first = d.press('trend');
  const second = d.press('stats');
  assert.equal(d.panel().output.title, 'Stats');
  assert.equal(d.panel().output.running, true);
  release();
  await first;
  assert.equal(d.panel().output.title, 'Stats', 'the first run does not write over the second');
  await second;
  assert.equal(d.panel().output.title, 'Stats');
  assert.deepEqual(d.panel().output.lines, ['stats line']);
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

test('mod files have no node: import and no dynamic import', () => {
  const dirPath = join(dirname(fileURLToPath(import.meta.url)), '..', 'hosts', 'claude-code', 'mod');
  for (const f of readdirSync(dirPath).filter((n) => n.endsWith('.mjs'))) {
    const src = readFileSync(join(dirPath, f), 'utf8');
    assert.doesNotMatch(src, /from\s+['"]node:/, f);
    assert.doesNotMatch(src, /import\s*\(/, f);
  }
});
