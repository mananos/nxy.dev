// @ts-check
/**
 * Batch verification (0.4.1). What has to hold: a batch is green only if its approved `Accept:`
 * command ran after the batch's last edit and exited cleanly — read from the implementer's
 * transcript, never from its report; a run whose exit status could be someone else's (piped) does
 * not count; an unproven batch sends the implementer back once, never in a loop; a red batch stops
 * the next one until it passes or the user says "Continue anyway"; and once every batch is green the
 * main thread is told to run the full suite once.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { parseAccept, parseBatches } from '../core/plan.mjs';
import {
  CONTINUE, afterBatch, afterReviewer, afterSuiteFix, afterTester, batchOfPrompt, batchVerdict, blockingBatch, continueQuestion, launchNote,
  progressLine, reviewInstruction, runMatches, suiteFixOfPrompt, suiteFixVerdict, unnamedBatchMessage,
} from '../core/verify.mjs';
import { CONTEXT_CLOSE, CONTEXT_OPEN } from '../core/memory/handoff.mjs';
import { claimSendBack, readVerify, recordBatch } from '../hosts/claude-code/verify-state.mjs';
import { ROOT, sandbox as makeSandbox } from './sandbox.mjs';

const ACCEPT1 = './mvnw -q test -Dtest=ClienteServiceTest';
const ACCEPT2 = 'npx vitest run src/cliente.test.ts';
const PLAN = [
  '## Plan',
  'Goal: corporate customers can be created',
  '### Batch 1 — api: validation',
  '- `src/main/java/app/ClienteService.java:88` — call CuitValidator before persisting',
  `Accept: \`${ACCEPT1}\``,
  '### Batch 2 — web: form',
  '- `web/src/cliente.ts` — CUIT field',
  `Accept: \`${ACCEPT2}\``,
  '### Batch 3 — web: look',
  '- `web/src/cliente.css` — spacing',
  'Accept: manual — the CUIT field lines up with the others',
].join('\n');

const cmd = { kind: /** @type {const} */ ('command'), command: ACCEPT1 };
const edit = { kind: /** @type {const} */ ('edit') };
const run = (command, ok = true, error) => ({ kind: /** @type {const} */ ('run'), command, ok, ...(error ? { error } : {}) });

test('accept and batches: a command in backticks, or explicitly manual', () => {
  assert.deepEqual(parseAccept('`npm test` passes'), { kind: 'command', command: 'npm test' });
  assert.deepEqual(parseAccept('manual — look at it'), { kind: 'manual', note: 'look at it' });
  assert.deepEqual(parseAccept('curl returns 201'), { kind: 'none' });
  assert.deepEqual(parseBatches(PLAN).map((b) => [b.n, b.accept.kind]), [[1, 'command'], [2, 'command'], [3, 'manual']]);
});

test('runMatches: the exit status has to be the command\'s own', () => {
  assert.equal(runMatches(ACCEPT1, ACCEPT1), true);
  assert.equal(runMatches(`cd api && ${ACCEPT1}`, ACCEPT1), true, 'a prefix is fine');
  assert.equal(runMatches(`rtk ${ACCEPT1}`, ACCEPT1), true, 'the filter\'s rewrite is fine');
  assert.equal(runMatches(`${ACCEPT1}  2>&1`, ACCEPT1), true);
  assert.equal(runMatches(`${ACCEPT1} && echo ok`, ACCEPT1), true);
  assert.equal(runMatches(`${ACCEPT1} | tail -20`, ACCEPT1), false, 'a pipe reports tail\'s exit status');
  assert.equal(runMatches(`${ACCEPT1}; echo done`, ACCEPT1), false);
  assert.equal(runMatches(`${ACCEPT1} || true`, ACCEPT1), false);
  assert.equal(runMatches('./mvnw -q test', ACCEPT1), false, 'the suite is not the batch\'s test');
});

test('runMatches: equivalent ways of typing the same command', () => {
  const acc = String.raw`venv\Scripts\python.exe -m pytest tests/x.py`;
  const ok = [
    'venv/Scripts/python.exe -m pytest tests/x.py',
    './venv/Scripts/python.exe -m pytest tests/x.py',
    '"venv/Scripts/python.exe" -m pytest tests/x.py',
    'venv/Scripts/python -m pytest tests/x.py',
    '& ./venv/Scripts/python.exe -m pytest tests/x.py',
    String.raw`& .\venv\Scripts\python.exe -m pytest tests/x.py`,
    'cd api && ./venv/Scripts/python.exe -m pytest tests/x.py 2>&1',
  ];
  for (const r of ok) assert.equal(runMatches(r, acc), true, r);
  const bad = [
    'venv/Scripts/python.exe -m pytest tests/x.py | tail',
    'venv/Scripts/python.exe -m pytest tests/x.py; echo done',
    'venv/Scripts/python.exe -m pytest tests/x.py || true',
    'venv/Scripts/python.exe -m pytest tests/y.py',
  ];
  for (const r of bad) assert.equal(runMatches(r, acc), false, r);
  const accept = { kind: /** @type {const} */ ('command'), command: acc };
  assert.deepEqual(batchVerdict([edit, run('./venv/Scripts/python.exe -m pytest tests/x.py')], accept), { status: 'pass' });
  assert.deepEqual(batchVerdict([edit, run('venv/Scripts/python.exe -m pytest tests/x.py | tail')], accept), { status: 'not-run' });
});

test('batchVerdict: the last matching run after the last edit decides', () => {
  assert.deepEqual(batchVerdict([edit, run(ACCEPT1)], cmd), { status: 'pass' });
  assert.deepEqual(batchVerdict([run(ACCEPT1), edit], cmd), { status: 'not-run' }, 'a run before an edit proves nothing');
  assert.deepEqual(batchVerdict([edit, run(ACCEPT1, false, 'Exit code 1')], cmd), { status: 'fail', error: 'Exit code 1' });
  assert.deepEqual(batchVerdict([edit, run(ACCEPT1, false, 'x'), edit, run(ACCEPT1)], cmd), { status: 'pass' }, 'fixed and rerun');
  assert.deepEqual(batchVerdict([edit, run(`${ACCEPT1} | tail`)], cmd), { status: 'not-run' });
  assert.deepEqual(batchVerdict([], { kind: 'manual', note: '' }), { status: 'manual' });
});

test('blockingBatch and batchOfPrompt', () => {
  const rec = { 1: { status: /** @type {const} */ ('fail') }, 2: { status: /** @type {const} */ ('pass') } };
  assert.deepEqual(blockingBatch(rec, [1], () => false), { k: 1, why: 'red' });
  assert.equal(blockingBatch(rec, [], () => false), null, 'no dependencies (the first batch, or a retry of it): always goes');
  assert.equal(blockingBatch(rec, [1, 2], (k) => k === 1), null, 'the user waved it through');
  assert.equal(blockingBatch({ 1: { status: 'manual' } }, [1], () => false), null, 'manual is not red');
  assert.deepEqual(blockingBatch({}, [1], () => false), { k: 1, why: 'pending' }, 'a dependency not carried out yet');

  const prompt = `${CONTEXT_OPEN}\n${PLAN}\n${CONTEXT_CLOSE}\n\nBatch 2 — add the CUIT field`;
  assert.equal(batchOfPrompt(prompt), 2, 'the plan inside nxy\'s block is not the request');
  assert.equal(batchOfPrompt('fix the typo'), null);
});

test('running: dependents wait, progress says so; a suite fix is never a batch', () => {
  assert.deepEqual(blockingBatch({ 1: { status: 'running' } }, [1], () => false), { k: 1, why: 'pending' });
  assert.match(progressLine([{ n: 1 }, { n: 2 }], { 1: { status: 'running' } }), /1 running, 2 pending/);
  assert.equal(suiteFixOfPrompt('Suite fix — batch 3 typecheck'), true);
  assert.equal(batchOfPrompt('Suite fix — batch 3 typecheck'), null);
  assert.equal(suiteFixOfPrompt('Batch 3 — typecheck'), false);
  assert.equal(suiteFixOfPrompt(`${CONTEXT_OPEN}\nSuite fix mentioned here\n${CONTEXT_CLOSE}\nBatch 2 — x`), false);
  assert.equal(batchOfPrompt(`${CONTEXT_OPEN}\nSuite fix mentioned here\n${CONTEXT_CLOSE}\nBatch 2 — x`), 2);
  assert.match(unnamedBatchMessage([1, 2], true), /Suite fix — /);
  assert.doesNotMatch(unnamedBatchMessage([1, 2]), /Suite fix/);
});

test('suiteFixVerdict: every suite command green after the last edit', () => {
  const cmds = ['npm run typecheck', 'npm test'];
  /** @type {any[]} */
  const green = [{ kind: 'edit' }, { kind: 'run', command: 'npm run typecheck', ok: true }, { kind: 'run', command: 'npm test', ok: true }];
  assert.equal(suiteFixVerdict(green, cmds).status, 'pass');
  /** @type {any[]} */
  const stale = [{ kind: 'run', command: 'npm test', ok: true }, { kind: 'edit' }, { kind: 'run', command: 'npm run typecheck', ok: true }];
  const v = suiteFixVerdict(stale, cmds);
  assert.equal(v.status, 'not-run');
  assert.deepEqual(v.failed, ['npm test']);
  /** @type {any[]} */
  const red = [{ kind: 'edit' }, { kind: 'run', command: 'npm test', ok: false, error: 'boom' }, { kind: 'run', command: 'npm run typecheck', ok: true }];
  assert.equal(suiteFixVerdict(red, cmds).status, 'fail');
  assert.equal(suiteFixVerdict(green, []).status, 'none');
});

test('post-suite texts: tester, suite fix, reviewer, launch, review instruction', () => {
  const next = reviewInstruction({ hash: 'abc12345', project: 'C:\\repo', needed: true });
  assert.match(next, /nxy:reviewer.*Review nxy plan abc12345 \(project: C:\/repo\)/);
  assert.match(reviewInstruction({ hash: 'abc12345', project: '/r', needed: false, reason: 'only docs' }), /No review needed.*only docs/);
  assert.equal(afterTester({ hash: 'abc12345', red: [], reviewNext: next }), next);
  const red = afterTester({ hash: 'abc12345', red: [{ command: 'npm test', error: 'boom' }] });
  assert.match(red, /✘.*`npm test` \(boom\)/s);
  assert.match(red, /Suite fix — <what failed>/);
  assert.match(afterSuiteFix({ hash: 'abc12345', verdict: { status: 'pass' }, commands: ['npm test'], reviewNext: next }), /✔.*\n.*nxy:reviewer/s);
  for (const verdict of /** @type {import('../core/verify.mjs').Verdict[]} */ ([{ status: 'none' }, { status: 'pass' }])) {
    const none = afterSuiteFix({ hash: 'abc12345', verdict, commands: [], reviewNext: next });
    assert.match(none, /not verified.*no suite command to re-run — tell the user/);
    assert.doesNotMatch(none, /✔|nxy:reviewer/);
  }
  assert.match(afterSuiteFix({ hash: 'abc12345', verdict: { status: 'fail', error: 'x' }, commands: ['npm test'] }), /✘.*failed after the last edit \(x\)/);
  assert.equal(afterReviewer({ hash: 'abc12345', recorded: true }), '');
  assert.match(afterReviewer({ hash: 'abc12345', recorded: false }), /returned without recording a review of plan abc12345.*again once.*tell the user/);
  assert.match(launchNote('batch', 7), /batch 7 launched in the background.*do not dispatch/);
});

test('afterBatch: ✔, ✘ with the question to ask, and the full suite once all are green', () => {
  const base = { hash: 'abc12345', command: ACCEPT1, batches: [1, 2, 3] };
  const red = afterBatch({ ...base, n: 1, verdict: { status: 'fail', error: 'Exit code 1' }, recorded: {} });
  assert.match(red, /batch 1 ✘ .* failed after the last edit \(Exit code 1\)/);
  assert.ok(red.includes(JSON.stringify(continueQuestion('abc12345', 1))), 'the exact question the dispatch hook looks for');
  assert.match(red, /Batch 2 will not be dispatched/);
  assert.doesNotMatch(afterBatch({ ...base, n: 1, verdict: { status: 'pass' }, recorded: {} }), /nxy:tester/, 'not until every batch is green');
  const last = afterBatch({ ...base, n: 2, verdict: { status: 'pass' }, recorded: { 1: { status: 'pass' }, 3: { status: 'manual' } } });
  assert.match(last, /All 3 batches of plan abc12345 verified[\s\S]*nxy:tester/);
});

const sandbox = () => makeSandbox(PLAN);

test('flow: unproven → sent back once; red stops the next batch until retried green or waved through', () => {
  const s = sandbox();

  // Batch 1: edited, never ran its command. Sent back once with the exact command; then it may stop.
  const lazy = s.agent(1, [{ edit: true }]);
  const first = s.stop('a1', lazy);
  assert.equal(first.status, 2, 'the implementer keeps going');
  assert.ok(first.stderr.includes(`\`${ACCEPT1}\``), 'told exactly what to run');
  assert.equal(s.stop('a1', lazy).status, 0, 'a second stop always goes through: never a loop');
  assert.equal(s.recorded()['1'].status, 'not-run');

  assert.match(s.returned(1), /batch 1 ✘[\s\S]*AskUserQuestion/, 'the main thread sees it, with the question');
  const blocked = s.dispatch('Batch 2 — do it');
  assert.equal(blocked?.permissionDecision, 'deny');
  assert.match(blocked?.permissionDecisionReason, /batch 1 of plan .* did not pass/);
  const waved = s.main({ [continueQuestion(s.hash, 1)]: CONTINUE });
  assert.notEqual(s.dispatch('Batch 2 — do it', waved)?.permissionDecision, 'deny', '"Continue anyway" lets batch 2 through');
  assert.notEqual(s.dispatch('Batch 1 — retry with the failure')?.permissionDecision, 'deny', 'retrying is always allowed');
  assert.equal(s.recorded()['1'].status, 'running', 'the retry is in flight: the old verdict is not read');
  assert.equal(s.dispatch('Batch 2 — do it', waved)?.permissionDecision, 'deny', 'batch 1 re-running: its dependents wait (pending)');

  // The retry: a piped run does not count; the plain one after the fix does.
  const piped = s.agent(1, [{ edit: true }, { run: `${ACCEPT1} | tail -20` }]);
  assert.equal(s.stop('a2', piped).status, 2);
  const fixed = s.agent(1, [{ edit: true }, { run: `${ACCEPT1} | tail -20` }, { run: ACCEPT1, ok: false, out: 'Exit code 1\nTests run: 3, Failures: 1' }, { edit: true }, { run: `cd . && ${ACCEPT1}` }]);
  assert.equal(s.stop('a2', fixed).status, 0);
  assert.equal(s.recorded()['1'].status, 'pass');
  assert.equal(s.dispatch('Batch 2 — do it')?.permissionDecision, 'allow', 'batch 1 green: batch 2 goes (with the handoff attached)');
  assert.match(s.returned(1), /batch 1 ✔/);
});

test('flow: a failing run is recorded with its error; all green → the tester, once', () => {
  const s = sandbox();
  const red = s.agent(2, [{ edit: true }, { run: ACCEPT2, ok: false, out: 'Exit code 1\n1 failed' }]);
  assert.match(s.stop('b1', red).stderr, /failed after your last edit \(Exit code 1\)/);
  s.stop('b1', red);
  assert.deepEqual(s.recorded()['2'], { ...s.recorded()['2'], status: 'fail', error: 'Exit code 1' });

  s.stop('c1', s.agent(1, [{ edit: true }, { run: ACCEPT1 }]));
  s.stop('c2', s.agent(2, [{ edit: true }, { run: ACCEPT2 }]));
  assert.equal(s.stop('c3', s.agent(3, [{ edit: true }])).status, 0, 'a manual batch has nothing to run');
  assert.equal(s.recorded()['3'].status, 'manual');
  assert.match(s.returned(3), /batch 3 – manual[\s\S]*All 3 batches[\s\S]*nxy:tester/);
});

test('silent where it does not apply', () => {
  const s = sandbox();
  assert.deepEqual(s.stop('d1', s.agent(1, [{ edit: true }]), 'nxy:scout'), { status: 0, stderr: '' }, 'only implementers are verified');
  assert.equal(s.stop('d2', s.agent(9, [{ edit: true }])).status, 0, 'a batch the plan does not have');
  const unnamed = s.dispatch('fix the CUIT validator');
  assert.equal(unnamed?.permissionDecision, 'deny');
  assert.match(unnamed?.permissionDecisionReason, /Start the prompt with "Batch N — \.\.\." \(batches: 1, 2, 3\)/);
});

test('parallel batches: verdicts recorded at the same moment are all kept; a send-back is claimed once', async () => {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-verify-race-'));
  const state = pathToFileURL(join(ROOT, 'hosts', 'claude-code', 'verify-state.mjs')).href;
  // Each process is one SubagentStop hook finishing its own batch; all of them at once.
  const script = `import { recordBatch } from ${JSON.stringify(state)};
recordBatch(process.argv[1], 'feature/x', 'abc12345', Number(process.argv[2]), { status: 'pass', ts: Date.now() });`;
  await Promise.all(Array.from({ length: 8 }, (_, i) => new Promise((resolve) => {
    spawn(process.execPath, ['--input-type=module', '-e', script, dir, String(i + 1)]).on('exit', resolve);
  })));
  const batches = readVerify(dir, 'feature/x', 'abc12345').batches;
  assert.deepEqual(Object.keys(batches).sort((a, b) => Number(a) - Number(b)), ['1', '2', '3', '4', '5', '6', '7', '8'], 'no verdict lost');
  assert.equal(batches['5'].status, 'pass');

  recordBatch(dir, 'feature/x', 'abc12345', 5, { status: 'fail', error: 'Exit code 1', ts: Date.now() });
  assert.deepEqual(readVerify(dir, 'feature/x', 'abc12345').batches['5'], { status: 'fail', error: 'Exit code 1', ts: readVerify(dir, 'feature/x', 'abc12345').batches['5'].ts }, 'a retry replaces the verdict');

  assert.equal(claimSendBack(dir, 'abc12345', 'a1:5'), true, 'first stop: sent back');
  assert.equal(claimSendBack(dir, 'abc12345', 'a1:5'), false, 'second stop: never a loop');
  assert.deepEqual(readVerify(dir, 'feature/x', 'abc12345').sentBack, ['a1:5']);
  assert.deepEqual(readVerify(dir, 'feature/x', 'ffff0000').batches, {}, 'another plan starts clean');
  assert.deepEqual(readVerify(dir, 'main', 'abc12345').batches, {}, 'another branch starts clean');
});
