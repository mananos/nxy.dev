// @ts-check
/**
 * Background agents and the post-suite fix, at hook level. What has to hold: a background launch says
 * only that it was launched (the verdict is not known yet); the verdict reaches the main thread as a
 * note when the agent ends; a dispatch in flight is never judged by its previous verdict; the reviewer
 * waits for the full suite; a "Suite fix" has its own slot, verified by re-running the failed commands;
 * and a reviewer that recorded nothing is sent back once and then reported.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdirSync, utimesSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { packetPath } from '../hosts/claude-code/baseline.mjs';
import { recordSuite } from '../hosts/claude-code/verify-state.mjs';
import { HOOKS, sandbox } from './sandbox.mjs';

const A1 = 'node check-one.js';
const A2 = 'node check-two.js';
const SUITE = 'npm run check';
const PLAN = [
  '## Plan',
  'Goal: two batches and a suite',
  '### Batch 1 — first',
  '- `src/a.js` — change',
  `Accept: \`${A1}\``,
  '### Batch 2 — second',
  'Depends: 1',
  '- `src/b.js` — change',
  `Accept: \`${A2}\``,
  `Suite: nxy — \`${SUITE}\``,
].join('\n');

/** What `review.mjs packet` leaves when a review is needed: the saved packet (written after the launch). */
function savePacket(s) {
  const p = packetPath(s.repo, 'feature/x');
  mkdirSync(dirname(p), { recursive: true });
  writeFileSync(p, '# packet\n');
}

const ASYNC = { isAsync: true, status: 'async_launched', agentId: 'x1' };
const denied = (d) => d?.permissionDecision === 'deny';

/** Both batches verified. */
function verified(s) {
  s.stop('p1', s.agent(1, [{ edit: true }, { run: A1 }]));
  s.stop('p2', s.agent(2, [{ edit: true }, { run: A2 }]));
  assert.equal(s.recorded()['1'].status, 'pass');
  assert.equal(s.recorded()['2'].status, 'pass');
}

test('async launch: only the launch note; the verdict arrives as a note when the agent ends, once', () => {
  const s = sandbox(PLAN);
  assert.equal(s.dispatch('Batch 1 — do it')?.permissionDecision, 'allow');
  const launch = s.returnedAs('Batch 1 — do it', 'nxy:implementer', ASYNC);
  assert.match(launch, /batch 1 launched in the background/);
  assert.doesNotMatch(launch, /✔|✘|nxy:tester/, 'no verdict at launch');

  assert.equal(s.stop('a1', s.agent(1, [{ edit: true }, { run: A1 }])).status, 0);
  const note = s.returnedAs('look around', 'nxy:scout');
  assert.match(note, /batch 1 ✔/, 'the next hook on the main thread delivers the verdict');
  assert.equal(s.returnedAs('look around', 'nxy:scout'), '', 'delivered once');

  // The same shapes the runtime may use: a status only, and the launch text.
  assert.match(s.returnedAs('Batch 2 — do it', 'nxy:implementer', { status: 'async_launched' }), /batch 2 launched/);
  assert.match(s.returnedAs('Batch 2 — do it', 'nxy:implementer', 'Async agent launched successfully'), /batch 2 launched/);
});

test('sync agents are judged as before; a stale verdict is never read for a run in flight', () => {
  const s = sandbox(PLAN);
  s.dispatch('Batch 1 — do it');
  s.stop('a1', s.agent(1, [{ edit: true }, { run: A1 }]));
  assert.match(s.returned(1), /batch 1 ✔/, 'sync: the verdict right away');

  // Batch 1 again: its old ✔ is gone while it runs, so batch 2 waits.
  assert.equal(s.dispatch('Batch 1 — again')?.permissionDecision, 'allow');
  assert.equal(s.recorded()['1'].status, 'running');
  assert.ok(denied(s.dispatch('Batch 2 — do it')));
  assert.equal(s.returned(1), '', 'a verdict still running says nothing');
});

test('the reviewer waits for the full suite; a red suite goes to its own fix slot, then the review', () => {
  const s = sandbox(PLAN);
  assert.ok(denied(s.dispatch('Suite fix — typecheck', undefined, 'nxy:implementer')), 'no suite fix with batches unverified');
  assert.doesNotMatch(s.dispatch('fix something')?.permissionDecisionReason, /Suite fix —/, 'not offered before');

  verified(s);
  assert.match(s.dispatch('fix something')?.permissionDecisionReason, /Suite fix —/, 'offered once every batch is verified');
  s.dispatch('Full suite for nxy plan x', undefined, 'nxy:tester');
  assert.ok(denied(s.dispatch('Review nxy plan x', undefined, 'nxy:reviewer')), 'the suite is running');
  assert.ok(denied(s.dispatch('Suite fix — typecheck')), 'nor a fix on a suite still running');

  // The tester ends with the suite red.
  s.stop('t1', s.agent('Full suite', [{ run: SUITE, ok: false, out: 'Exit code 1\n3 failed' }]), 'nxy:tester');
  const red = s.returnedAs('Full suite', 'nxy:tester');
  assert.match(red, /full suite of plan .* ✘[\s\S]*npm run check[\s\S]*Suite fix —/);
  assert.doesNotMatch(red, /nxy:reviewer subagent/);

  assert.equal(s.dispatch('Suite fix — typecheck failed')?.permissionDecision, 'allow');
  // Edited but never re-ran the suite: sent back once, then reported.
  const lazy = s.agent('Suite fix — typecheck failed', [{ edit: true }]);
  assert.equal(s.stop('f1', lazy).status, 2);
  assert.equal(s.stop('f1', lazy).status, 0);
  assert.match(s.returnedAs('Suite fix — typecheck failed'), /suite fix ✘[\s\S]*npm run check/);

  // A run before the last edit proves nothing; the one after does.
  const stale = s.agent('Suite fix — typecheck failed', [{ run: SUITE }, { edit: true }]);
  assert.equal(s.stop('f2', stale).status, 2);
  const good = s.agent('Suite fix — typecheck failed', [{ edit: true }, { run: SUITE }]);
  assert.equal(s.stop('f3', good).status, 0);
  const fixed = s.returnedAs('Suite fix — typecheck failed');
  assert.match(fixed, /suite fix ✔[\s\S]*npm run check[\s\S]*full suite done/);
  assert.ok(!denied(s.dispatch('Review nxy plan x', undefined, 'nxy:reviewer')), 'the review goes ahead');
});

test('a reviewer that recorded nothing is sent back once, then reported', () => {
  const s = sandbox(PLAN);
  verified(s);
  assert.ok(!denied(s.dispatch('Review nxy plan x', undefined, 'nxy:reviewer')));
  savePacket(s);
  const silent = s.agent('Review nxy plan x', []);
  const first = s.stop('r1', silent, 'nxy:reviewer');
  assert.equal(first.status, 2);
  assert.match(first.stderr, /record command/);
  assert.equal(s.stop('r1', silent, 'nxy:reviewer').status, 0, 'never a loop');
  assert.match(s.returnedAs('Review nxy plan x', 'nxy:reviewer'), /returned without recording a review[\s\S]*nxy:reviewer again once/);
});

/** Runs a hook script as the main thread (or a subagent, with `agent_id`) and returns status, stdout, stderr. */
function runHook(s, file, o) {
  const r = spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', join(HOOKS, file)], {
    input: JSON.stringify({ cwd: s.repo, session_id: 's1', ...o }), encoding: 'utf8', env: s.env,
  });
  return { status: r.status, stdout: r.stdout, stderr: r.stderr };
}

/** A note waiting: a batch launched in the background and ended. */
function noteWaiting(s) {
  s.dispatch('Batch 1 — do it');
  s.returnedAs('Batch 1 — do it', 'nxy:implementer', ASYNC);
  s.stop('a1', s.agent(1, [{ edit: true }, { run: A1 }]));
}

test('a completion note is delivered once: by Stop, by the next prompt, or by the next Bash call', () => {
  // Stop (exit 2), then nothing for any other hook.
  let s = sandbox(PLAN);
  noteWaiting(s);
  const sub = runHook(s, 'stop.mjs', { agent_id: 'sub1', stop_hook_active: false });
  assert.deepEqual([sub.status, sub.stderr], [0, ''], 'a subagent own Stop does not take the note');
  const st = runHook(s, 'stop.mjs', { stop_hook_active: false });
  assert.equal(st.status, 2);
  assert.match(st.stderr, /batch 1 ✔/);
  assert.equal(runHook(s, 'stop.mjs', { stop_hook_active: false }).status, 0, 'consumed: no loop');
  assert.equal(runHook(s, 'userpromptsubmit.mjs', { prompt: '/help' }).stdout, '');
  assert.equal(runHook(s, 'posttooluse-bash.mjs', { tool_name: 'Bash', tool_input: { command: 'ls' } }).stdout, '');

  // The next prompt.
  s = sandbox(PLAN);
  noteWaiting(s);
  const up = runHook(s, 'userpromptsubmit.mjs', { prompt: '/status' });
  assert.match(JSON.parse(up.stdout).hookSpecificOutput.additionalContext, /batch 1 ✔/);
  assert.equal(runHook(s, 'stop.mjs', { stop_hook_active: false }).status, 0, 'already delivered');

  // The next Bash call.
  s = sandbox(PLAN);
  noteWaiting(s);
  const bash = runHook(s, 'posttooluse-bash.mjs', { tool_name: 'Bash', tool_input: { command: 'ls' }, tool_response: { stdout: 'x' } });
  assert.match(JSON.parse(bash.stdout).hookSpecificOutput.additionalContext, /batch 1 ✔/);
  assert.equal(runHook(s, 'posttooluse-bash.mjs', { tool_name: 'Bash', tool_input: { command: 'ls' } }).stdout, '', 'once');
  assert.equal(runHook(s, 'stop.mjs', { stop_hook_active: false }).status, 0);
});

test('a background reviewer: the report waits for its end, and is left as a note', () => {
  const s = sandbox(PLAN);
  verified(s);
  s.dispatch('Review nxy plan x', undefined, 'nxy:reviewer');
  savePacket(s);
  assert.match(s.returnedAs('Review nxy plan x', 'nxy:reviewer', ASYNC), /the review launched in the background/);
  const silent = s.agent('Review nxy plan x', []);
  assert.equal(s.stop('r2', silent, 'nxy:reviewer').status, 2);
  assert.equal(s.returnedAs('look', 'nxy:scout'), '', 'a send-back leaves no note');
  assert.equal(s.stop('r2', silent, 'nxy:reviewer').status, 0);
  assert.match(s.returnedAs('look', 'nxy:scout'), /returned without recording a review/);
});

test('R2: a tester whose suite is still running or unrecorded is not reported as green', () => {
  const s = sandbox(PLAN);
  verified(s);
  s.dispatch('Full suite for nxy plan x', undefined, 'nxy:tester');
  const running = s.returnedAs('Full suite', 'nxy:tester');
  assert.match(running, /no recorded result[\s\S]*still running[\s\S]*dispatch nxy:tester again/);
  assert.doesNotMatch(running, /full suite done|nxy:reviewer subagent/);
});

test('R3: a suite marked running for over 6 hours no longer blocks, and the deny says how to reset it', () => {
  const s = sandbox(PLAN);
  verified(s);
  s.dispatch('Full suite for nxy plan x', undefined, 'nxy:tester');
  const d = s.dispatch('Review nxy plan x', undefined, 'nxy:reviewer');
  assert.ok(denied(d));
  assert.match(d.permissionDecisionReason, /dispatching nxy:tester again resets it/);
  recordSuite(s.repo, s.hash, { status: 'running', ts: Date.now() - 7 * 3_600_000 });
  assert.ok(!denied(s.dispatch('Review nxy plan x', undefined, 'nxy:reviewer')), 'a stale running mark is ignored');
});

test('R4: a background agent that ends with nothing recorded still leaves a note', () => {
  const s = sandbox(PLAN);
  verified(s);
  s.dispatch('Full suite for nxy plan x', undefined, 'nxy:tester');
  s.returnedAs('Full suite', 'nxy:tester', ASYNC);
  assert.equal(s.stop('t9', undefined, 'nxy:tester').status, 0, 'no transcript to read');
  assert.match(s.returnedAs('look', 'nxy:scout'), /no recorded result/, 'the promised report is not silent');
});

test('R5: the tester red list drops a command that later passed and keeps only the suite commands', () => {
  const s = sandbox(PLAN);
  verified(s);
  s.dispatch('Full suite for nxy plan x', undefined, 'nxy:tester');
  const flaky = s.agent('Full suite', [{ run: SUITE, ok: false }, { run: 'node probe.js', ok: false }, { run: SUITE }]);
  s.stop('t1', flaky, 'nxy:tester');
  assert.match(s.returnedAs('Full suite', 'nxy:tester'), /full suite done|nxy:reviewer/);
  s.dispatch('Full suite again', undefined, 'nxy:tester');
  s.stop('t2', s.agent('Full suite', [{ run: 'node probe.js', ok: false }, { run: SUITE, ok: false }]), 'nxy:tester');
  const red = s.returnedAs('Full suite', 'nxy:tester');
  assert.match(red, /✘[\s\S]*npm run check/);
  assert.doesNotMatch(red, /probe\.js/);
});

test('R7: a reviewer that returns with no packet (no review needed) is not sent back', () => {
  const s = sandbox(PLAN);
  verified(s);
  s.dispatch('Review nxy plan x', undefined, 'nxy:reviewer');
  const quiet = s.agent('Review nxy plan x', []);
  assert.equal(s.stop('r7', quiet, 'nxy:reviewer').status, 0);
  const line = s.returnedAs('Review nxy plan x', 'nxy:reviewer');
  assert.match(line, /saved no review packet/);
  assert.doesNotMatch(line, /without recording/);
  // A packet from before the launch does not count either.
  savePacket(s);
  const old = new Date(Date.now() - 3_600_000);
  utimesSync(packetPath(s.repo, 'feature/x'), old, old);
  s.dispatch('Review nxy plan x', undefined, 'nxy:reviewer');
  assert.equal(s.stop('r8', quiet, 'nxy:reviewer').status, 0);
});

test('R7b: a reviewer that never ran the packet, with changed files, is sent back once', () => {
  const s = sandbox(PLAN);
  verified(s);
  // A copy is kept before the first edit, then the file changes: the plan's review is needed.
  const file = join(s.repo, 'src', 'Svc.java');
  mkdirSync(dirname(file), { recursive: true });
  writeFileSync(file, 'class Svc {\n  int a;\n}\n');
  s.hook('pretooluse-edit.mjs', { tool_name: 'Edit', agent_id: 'a1', tool_input: { file_path: file } });
  writeFileSync(file, 'class Svc {\n  int b;\n}\n');
  s.dispatch('Review nxy plan x', undefined, 'nxy:reviewer');
  const silent = s.agent('Review nxy plan x', []);
  const first = s.stop('r9', silent, 'nxy:reviewer');
  assert.equal(first.status, 2, 'no packet, no record, files changed: sent back');
  assert.equal(s.stop('r9', silent, 'nxy:reviewer').status, 0, 'never a loop');
  assert.match(s.returnedAs('Review nxy plan x', 'nxy:reviewer'), /without recording/);
});

test('R9: a stale async marker does not make a later sync run deliver its verdict twice', () => {
  const s = sandbox(PLAN);
  s.dispatch('Batch 1 — do it');
  s.returnedAs('Batch 1 — do it', 'nxy:implementer', ASYNC); // marked, never ended
  s.dispatch('Batch 1 — again'); // a new dispatch starts clean
  s.stop('a1', s.agent(1, [{ edit: true }, { run: A1 }]));
  assert.match(s.returned(1), /batch 1 ✔/, 'sync: the verdict right away');
  assert.equal(s.returnedAs('look', 'nxy:scout'), '', 'no second copy as a note');
});

test('a suite fix with no command to re-run is not verified, not sent back and does not open the review', () => {
  const s = sandbox(PLAN.replace(/\nSuite: .*$/, ''));
  verified(s);
  assert.equal(s.dispatch('Suite fix — typecheck')?.permissionDecision, 'allow');
  s.returnedAs('Suite fix — typecheck', 'nxy:implementer', ASYNC);
  assert.equal(s.stop('f0', s.agent('Suite fix — typecheck', [{ edit: true }])).status, 0, 'never a loop');
  const note = s.returnedAs('look', 'nxy:scout');
  assert.match(note, /suite fix not verified[\s\S]*tell the user/);
  assert.doesNotMatch(note, /✔|nxy:reviewer subagent/);
});
