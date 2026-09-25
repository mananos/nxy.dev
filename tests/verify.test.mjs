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
import { execFileSync, spawn, spawnSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { parseAccept, parseBatches, planHash } from '../core/plan.mjs';
import {
  CONTINUE, afterBatch, batchOfPrompt, batchVerdict, blockingBatch, continueQuestion, runMatches,
} from '../core/verify.mjs';
import { CONTEXT_CLOSE, CONTEXT_OPEN } from '../core/memory/handoff.mjs';
import { APPROVE, approvalQuestion } from '../core/plan.mjs';
import { claimSendBack, readVerify, recordBatch } from '../hosts/claude-code/verify-state.mjs';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const HOOKS = join(ROOT, 'hosts', 'claude-code', 'hooks');
const MEM = join(ROOT, 'hosts', 'claude-code', 'entries', 'mem.mjs');

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

/** A repo on a branch with an approved plan, and helpers to run the hooks as Claude Code would. */
function sandbox() {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-verify-'));
  const repo = join(dir, 'repo');
  mkdirSync(join(repo, '.git'), { recursive: true });
  writeFileSync(join(repo, '.git', 'HEAD'), 'ref: refs/heads/feature/x\n');
  writeFileSync(join(repo, '.git', 'config'), '[remote "origin"]\n\turl = git@github.com:x/y.git\n');
  const env = { ...process.env, NXY_HOME: join(dir, 'home'), CLAUDE_PROJECT_DIR: repo };
  const hash = planHash(PLAN);
  spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', MEM, 'handoff', 'plan', '--cwd', repo], { input: PLAN, encoding: 'utf8', env });

  /** The main transcript: cheap context, the plan approved, and any other answers. */
  const main = (answers = {}) => {
    /** @type {object[]} */
    const lines = [{ type: 'assistant', message: { usage: { cache_read_input_tokens: 20_000 } } }];
    Object.entries({ [approvalQuestion(hash)]: APPROVE, ...answers }).forEach(([q, a], i) => {
      lines.push({ type: 'assistant', message: { content: [{ type: 'tool_use', id: `q${i}`, name: 'AskUserQuestion', input: { questions: [{ question: q }] } }] } });
      lines.push({ type: 'user', message: { content: [{ type: 'tool_result', tool_use_id: `q${i}`, content: 'ok' }] }, toolUseResult: { answers: { [q]: a } } });
    });
    const p = join(dir, `main-${Math.random().toString(36).slice(2)}.jsonl`);
    writeFileSync(p, lines.map((l) => JSON.stringify(l)).join('\n') + '\n');
    return p;
  };

  /** An implementer's transcript: its prompt, then edits and runs ({edit} | {run, ok, out}). */
  const agent = (n, steps) => {
    /** @type {object[]} */
    const lines = [{ type: 'user', isSidechain: true, message: { role: 'user', content: `${CONTEXT_OPEN}\n${PLAN}\n${CONTEXT_CLOSE}\n\nBatch ${n} — do it` } }];
    steps.forEach((s, i) => {
      const tool = s.edit ? { name: 'Edit', input: { file_path: 'x' } } : { name: 'Bash', input: { command: s.run } };
      lines.push({ type: 'assistant', isSidechain: true, message: { content: [{ type: 'tool_use', id: `t${i}`, ...tool }] } });
      lines.push({ type: 'user', isSidechain: true, message: { content: [{ type: 'tool_result', tool_use_id: `t${i}`, is_error: s.ok === false, content: s.out || 'ok' }] } });
    });
    const p = join(dir, `agent-${Math.random().toString(36).slice(2)}.jsonl`);
    writeFileSync(p, lines.map((l) => JSON.stringify(l)).join('\n') + '\n');
    return p;
  };

  const payload = (o) => JSON.stringify({ cwd: repo, session_id: 's1', ...o });
  /** SubagentStop: exit status and what the implementer is told. */
  const stop = (agentId, transcript, agentType = 'nxy:implementer') => {
    const r = spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', join(HOOKS, 'subagentstop.mjs')], {
      input: payload({ hook_event_name: 'SubagentStop', agent_id: agentId, agent_type: agentType, agent_transcript_path: transcript }), encoding: 'utf8', env,
    });
    return { status: r.status, stderr: r.stderr };
  };
  const hook = (file, o) => execFileSync(process.execPath, ['--disable-warning=ExperimentalWarning', join(HOOKS, file)], { input: payload(o), encoding: 'utf8', env });
  const dispatch = (prompt, transcript = main()) => {
    const out = hook('pretooluse-agent.mjs', { tool_name: 'Agent', transcript_path: transcript, tool_input: { subagent_type: 'nxy:implementer', prompt } });
    return out.trim() ? JSON.parse(out).hookSpecificOutput : null;
  };
  const returned = (n) => {
    const out = hook('posttooluse-agent.mjs', { tool_name: 'Agent', tool_input: { subagent_type: 'nxy:implementer', prompt: `Batch ${n} — do it` } });
    return out.trim() ? JSON.parse(out).hookSpecificOutput.additionalContext : '';
  };
  const recorded = () => readVerify(repo, 'feature/x', hash).batches;
  return { hash, main, agent, stop, dispatch, returned, recorded };
}

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
  assert.notEqual(s.dispatch('Batch 1 — retry with the failure')?.permissionDecision, 'deny', 'retrying is always allowed');

  const waved = s.main({ [continueQuestion(s.hash, 1)]: CONTINUE });
  assert.notEqual(s.dispatch('Batch 2 — do it', waved)?.permissionDecision, 'deny', '"Continue anyway" lets batch 2 through');

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
