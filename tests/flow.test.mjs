// @ts-check
/**
 * The flow by size (0.4.4). What has to hold: with an approved plan the main thread does not write —
 * every batch goes to an implementer, so every batch is verified — except when the user asks with
 * `/nxy:gate once`; batches that depend only on a contract run in parallel, and a red or unfinished
 * batch stops only what depends on it; `flow.plan: "always"` sends a second file to the planner; the
 * role table's model reaches each dispatch; and the session cut is suggested once, at a pause.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { APPROVE, approvalQuestion, parseBatches, planHash, validatePlan } from '../core/plan.mjs';
import { afterBatch } from '../core/verify.mjs';
import { CONTEXT_CLOSE, CONTEXT_OPEN } from '../core/memory/handoff.mjs';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const HOOKS = join(ROOT, 'hosts', 'claude-code', 'hooks');
const MEM = join(ROOT, 'hosts', 'claude-code', 'entries', 'mem.mjs');
const GATE = join(ROOT, 'hosts', 'claude-code', 'entries', 'gate.mjs');

const PLAN = [
  '## Plan',
  'Goal: corporate customers across api and web',
  '### Batch 1 — contract: ClienteDto',
  '- `contract/src/ClienteDto.java` (new) — the shared DTO',
  'Accept: `./mvnw -q test -Dtest=ClienteDtoTest`',
  '### Batch 2 — api: endpoint',
  '- `api/src/ClienteController.java:40` — POST',
  'Accept: `./mvnw -q test -Dtest=ClienteControllerTest`',
  'Depends: 1',
  '### Batch 3 — web: form',
  '- `web/src/cliente.component.ts` — the form',
  'Accept: `npx vitest run src/cliente.component.spec.ts`',
  'Depends: 1',
  '### Batch 4 — web: wiring',
  '- `web/src/app.routes.ts` — the route',
  'Accept: `npx vitest run src/app.routes.spec.ts`',
].join('\n');

test('Depends: explicit, none, or the batch before; only earlier batches', () => {
  assert.deepEqual(parseBatches(PLAN).map((b) => [b.n, b.depends]), [[1, []], [2, [1]], [3, [1]], [4, [3]]]);
  assert.deepEqual(parseBatches(PLAN.replace('Depends: 1\n### Batch 3', 'Depends: none\n### Batch 3')).map((b) => b.depends)[1], []);
  assert.equal(validatePlan(PLAN).ok, true);
  assert.match(validatePlan(PLAN.replace('Depends: 1\n### Batch 3', 'Depends: 3\n### Batch 3')).errors.join(), /batch 2: "Depends:" can only name batches written before it \(3 is not\)/);
  const red = afterBatch({ hash: 'h', n: 1, verdict: { status: 'fail' }, batches: [1, 2, 3, 4], recorded: {}, dependents: [2, 3] });
  assert.match(red, /Batches 2, 3 will not be dispatched until batch 1 passes/);
});

/** A repo with PLAN approved, a cheap main session, and helpers. */
function sandbox(config = {}) {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-flow-'));
  const repo = join(dir, 'repo');
  mkdirSync(join(repo, '.git'), { recursive: true });
  mkdirSync(join(repo, '.nxy'), { recursive: true });
  writeFileSync(join(repo, '.git', 'HEAD'), 'ref: refs/heads/feature/x\n');
  writeFileSync(join(repo, '.git', 'config'), '[remote "origin"]\n\turl = git@github.com:x/y.git\n');
  writeFileSync(join(repo, '.nxy', 'config.json'), JSON.stringify(config));
  const env = { ...process.env, NXY_HOME: join(dir, 'home'), CLAUDE_PROJECT_DIR: repo };
  const node = (args, input) => spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', ...args, '--cwd', repo], { input, encoding: 'utf8', env, cwd: repo });
  const hook = (file, o) => execFileSync(process.execPath, ['--disable-warning=ExperimentalWarning', join(HOOKS, file)], {
    input: JSON.stringify({ cwd: repo, session_id: 's1', ...o }), encoding: 'utf8', env,
  });
  const main = (contextTokens = 20_000, approve = true) => {
    const p = join(dir, `main-${Math.random().toString(36).slice(2)}.jsonl`);
    const q = approvalQuestion(planHash(PLAN));
    /** @type {object[]} */
    const lines = [{ type: 'assistant', message: { usage: { cache_read_input_tokens: contextTokens } } }];
    if (approve) {
      lines.push({ type: 'assistant', message: { content: [{ type: 'tool_use', id: 'q1', name: 'AskUserQuestion', input: { questions: [{ question: q }] } }] } });
      lines.push({ type: 'user', message: { content: [{ type: 'tool_result', tool_use_id: 'q1', content: 'ok' }] }, toolUseResult: { answers: { [q]: APPROVE } } });
      lines.push({ type: 'assistant', message: { usage: { cache_read_input_tokens: contextTokens } } });
    }
    writeFileSync(p, lines.map((l) => JSON.stringify(l)).join('\n') + '\n');
    return p;
  };
  const out = (text) => (text.trim() ? JSON.parse(text).hookSpecificOutput : null);
  const dispatch = (prompt, agent = 'nxy:implementer', t = main()) => out(hook('pretooluse-agent.mjs', { tool_name: 'Agent', transcript_path: t, tool_input: { subagent_type: agent, prompt } }));
  const edit = (file, t = main(), extra = {}) => out(hook('pretooluse-edit.mjs', { tool_name: 'Edit', transcript_path: t, tool_input: { file_path: join(repo, file) }, ...extra }));
  /** An implementer of batch n that edits and runs its Accept (ok or not). */
  const finish = (n, ok = true) => {
    const accept = parseBatches(PLAN).find((b) => b.n === n)?.accept;
    const t = join(dir, `agent-${n}-${Math.random().toString(36).slice(2)}.jsonl`);
    writeFileSync(t, [
      { type: 'user', message: { content: `${CONTEXT_OPEN}\n${PLAN}\n${CONTEXT_CLOSE}\n\nBatch ${n} — go` } },
      { type: 'assistant', message: { content: [{ type: 'tool_use', id: 'e', name: 'Edit', input: { file_path: 'x' } }] } },
      { type: 'assistant', message: { content: [{ type: 'tool_use', id: 'r', name: 'Bash', input: { command: accept?.kind === 'command' ? accept.command : 'true' } }] } },
      { type: 'user', message: { content: [{ type: 'tool_result', tool_use_id: 'r', is_error: !ok, content: ok ? 'ok' : 'Exit code 1' }] } },
    ].map((l) => JSON.stringify(l)).join('\n') + '\n');
    spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', join(HOOKS, 'subagentstop.mjs')], {
      input: JSON.stringify({ cwd: repo, agent_id: `a${n}${ok}`, agent_type: 'nxy:implementer', agent_transcript_path: t }), encoding: 'utf8', env,
    });
    spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', join(HOOKS, 'subagentstop.mjs')], {
      input: JSON.stringify({ cwd: repo, agent_id: `a${n}${ok}`, agent_type: 'nxy:implementer', agent_transcript_path: t }), encoding: 'utf8', env,
    });
  };
  return { dir, repo, node, hook, main, dispatch, edit, finish, out };
}

test('parallel batches: independent ones go together; red or unfinished stops only its dependents', () => {
  const s = sandbox();
  s.node([MEM, 'handoff', 'plan'], PLAN);
  const pending = s.dispatch('Batch 2 — endpoint');
  assert.equal(pending?.permissionDecision, 'deny');
  assert.match(pending?.permissionDecisionReason, /builds on batch 1, which has not passed yet/, 'the contract first');

  s.finish(1);
  assert.equal(s.dispatch('Batch 2 — endpoint')?.permissionDecision, 'allow');
  assert.equal(s.dispatch('Batch 3 — form')?.permissionDecision, 'allow', 'api and web in parallel');

  s.finish(3, false);
  assert.match(s.dispatch('Batch 4 — wiring')?.permissionDecisionReason, /batch 3 of plan .* did not pass/, 'web wiring waits for the red form');
  s.finish(2);
  assert.equal(s.dispatch('Batch 2 — endpoint again')?.permissionDecision, 'allow', 'the api side is not held by the web side');
});

test('approved plan: the main thread does not edit, unless the user runs /nxy:gate once', () => {
  const s = sandbox();
  s.node([MEM, 'handoff', 'plan'], PLAN);
  const denied = s.edit('api/src/ClienteController.java');
  assert.equal(denied?.permissionDecision, 'deny');
  assert.match(denied?.permissionDecisionReason, /is approved, so its work goes through implementers[\s\S]*batches: 1, 2, 3, 4[\s\S]*the user can run `\/nxy:gate once`/);
  assert.equal(s.edit('api/src/ClienteController.java', s.main(), { agent_id: 'impl' }), null, 'the implementer writes');
  s.node([GATE, 'once']);
  assert.equal(s.edit('api/src/ClienteController.java'), null, 'the user\'s escape: one edit');
  assert.equal(s.edit('api/src/ClienteController.java')?.permissionDecision, 'deny', 'and only one');
  assert.equal(s.edit('README.md', s.main(20_000, false))?.permissionDecision, 'deny', 'a plan pending approval still blocks, as before');
});

test('flow.plan "always": a second file without a plan goes to the planner', () => {
  const s = sandbox({ flow: { plan: 'always' } });
  assert.equal(s.edit('src/a.ts'), null, 'one file: fine');
  assert.equal(s.edit('src/a.ts'), null, 'the same file again: fine');
  const second = s.edit('src/b.ts');
  assert.equal(second?.permissionDecision, 'deny');
  assert.match(second?.permissionDecisionReason, /flow\.plan: "always"[\s\S]*nxy:planner/);
  assert.equal(sandbox().edit('src/b.ts'), null, 'without the setting nothing changes');
});

test('the role table\'s model reaches the dispatch', () => {
  const s = sandbox({ roles: { planner: { model: 'opus' }, scout: { model: 'haiku' }, reviewer: { model: 'gpt-9' } } });
  const planner = s.dispatch('plan this', 'nxy:planner');
  assert.equal(planner?.updatedInput?.model, 'opus');
  assert.equal(planner?.updatedInput?.prompt, 'plan this', 'the rest of the dispatch is untouched');
  assert.equal(s.dispatch('find x', 'nxy:scout'), null, 'same as the agent\'s own: nothing to change');
  assert.equal(s.dispatch('review', 'nxy:reviewer'), null, 'not a model alias: left alone');
  assert.equal(s.dispatch('x', 'Explore'), null, 'not an nxy agent');
  const withModel = s.out(s.hook('pretooluse-agent.mjs', { tool_name: 'Agent', transcript_path: s.main(), tool_input: { subagent_type: 'nxy:planner', prompt: 'x', model: 'sonnet' } }));
  assert.equal(withModel, null, 'a model the main thread named wins');

  const impl = sandbox({ roles: { implementer: { model: 'haiku' } } });
  impl.node([MEM, 'handoff', 'plan'], PLAN);
  const d = impl.dispatch('Batch 1 — contract');
  assert.equal(d?.updatedInput?.model, 'haiku');
  assert.match(d?.updatedInput?.prompt, /<nxy-context/, 'together with the handoff');
});

test('session cut: suggested at a pause, past the statusline warning, once per session', () => {
  const s = sandbox();
  const back = (t, session = 's1') => s.out(s.hook('posttooluse-agent.mjs', { session_id: session, tool_name: 'Agent', transcript_path: t, tool_input: { subagent_type: 'nxy:reviewer', prompt: 'Review' } }));
  assert.equal(back(s.main(60_000)), null, 'light session: nothing');
  assert.match(back(s.main(180_000))?.additionalContext, /carries 180k tokens[\s\S]*warns at 100k[\s\S]*\/clear[\s\S]*handoff show/);
  assert.equal(back(s.main(190_000)), null, 'said once');
  assert.match(back(s.main(190_000), 's2')?.additionalContext, /carries 190k/, 'a new session may hear it again');
  assert.equal(s.out(s.hook('posttooluse-agent.mjs', { session_id: 's3', tool_name: 'Agent', transcript_path: s.main(190_000), tool_input: { subagent_type: 'Explore', prompt: 'x' } })), null,
    'not a pause: an exploration is not a natural point to cut');
  assert.ok(readFileSync(join(s.repo, '.nxy', 'local', 'cut-suggested.json'), 'utf8').includes('s2'));
});
