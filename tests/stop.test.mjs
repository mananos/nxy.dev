// @ts-check
/**
 * The Stop hook and the derived progress (0.4.5). What has to hold: a turn does not end with the
 * task's handoff stale — only with a live handoff, only for edits of this session after the last
 * save, once per state, never when `stop_hook_active`, never for edits that did not happen (denied)
 * or that are plan batch work (whose progress nxy derives itself); and the progress shown by
 * `handoff show` and to the implementer comes from nxy's records, not from the model.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { decideStop } from '../core/memory/handoff.mjs';
import { CONTEXT_CLOSE, CONTEXT_OPEN } from '../core/memory/handoff.mjs';
import { progressLine } from '../core/verify.mjs';
import { pushNote } from '../hosts/claude-code/agent-notes.mjs';
import { APPROVE,approvalQuestion, planHash } from '../core/plan.mjs';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const HOOKS = join(ROOT, 'hosts', 'claude-code', 'hooks');
const MEM = join(ROOT, 'hosts', 'claude-code', 'entries', 'mem.mjs');

test('decideStop: only a live handoff, only newer edits, once per state, never twice in a row', () => {
  const base = { stopHookActive: false, savedTs: 100, lastEditTs: 200, remindedTs: null };
  assert.deepEqual(decideStop(base), { block: true, reason: 'handoff-stale' });
  assert.equal(decideStop({ ...base, stopHookActive: true }).block, false, 'the model may not be able to comply: no loop');
  assert.equal(decideStop({ ...base, savedTs: null }).block, false, 'no handoff: a small task, never');
  assert.equal(decideStop({ ...base, lastEditTs: 90 }).block, false, 'saved after the last edit');
  assert.equal(decideStop({ ...base, lastEditTs: null }).block, false, 'this session edited nothing');
  assert.equal(decideStop({ ...base, remindedTs: 200 }).block, false, 'already asked about this state');
  assert.equal(decideStop({ ...base, remindedTs: 150 }).block, true, 'a newer edit is a new state');
});

test('progressLine: batches and review, from the records', () => {
  const line = progressLine([{ n: 1 }, { n: 2 }, { n: 3 }, { n: 4 }],
    { 1: { status: 'pass' }, 2: { status: 'fail', error: 'Exit code 1' }, 4: { status: 'manual' } },
    { id: 'abc123', change: 2, preexisting: 1, chosen: 1 });
  assert.equal(line, 'Progress (recorded by nxy, not by the model): batch 1 ✔, 2 ✘ (Exit code 1), 3 pending, 4 – manual · review abc123: 2 from the change, 1 chosen to fix, 1 preexisting');
  assert.equal(progressLine([], {}), '');
});

function sandbox() {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-stop-'));
  const repo = join(dir, 'repo');
  mkdirSync(join(repo, '.git'), { recursive: true });
  writeFileSync(join(repo, '.git', 'HEAD'), 'ref: refs/heads/feature/x\n');
  writeFileSync(join(repo, '.git', 'config'), '[remote "origin"]\n\turl = git@github.com:x/y.git\n');
  const env = { ...process.env, NXY_HOME: join(dir, 'home'), CLAUDE_PROJECT_DIR: repo };
  const node = (args, input) => spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', ...args, '--cwd', repo], { input, encoding: 'utf8', env, cwd: repo });
  const main = join(dir, 'main.jsonl');
  writeFileSync(main, JSON.stringify({ type: 'assistant', message: { usage: { cache_read_input_tokens: 20_000 } } }) + '\n');
  const hook = (file, o) => execFileSync(process.execPath, ['--disable-warning=ExperimentalWarning', join(HOOKS, file)], {
    input: JSON.stringify({ cwd: repo, session_id: 's1', transcript_path: main, ...o }), encoding: 'utf8', env,
  });
  const edit = (f, o = {}) => hook('pretooluse-edit.mjs', { tool_name: 'Edit', tool_input: { file_path: join(repo, f) }, ...o });
  const stop = (o = {}) => {
    const r = spawnSync(process.execPath, [join(HOOKS, 'stop.mjs')], { input: JSON.stringify({ cwd: repo, session_id: 's1', stop_hook_active: false, ...o }), encoding: 'utf8', env });
    return { status: r.status, stderr: r.stderr };
  };
  const HANDOFF = 'route: implementer\n## Done\n- started\n## Next\n- the rest';
  return { dir, repo, env, node, hook, edit, stop, main, HANDOFF };
}

test('Stop: a stale handoff stops the turn once; small tasks and saved handoffs never', () => {
  const s = sandbox();
  s.edit('src/a.ts');
  assert.deepEqual(s.stop(), { status: 0, stderr: '' }, 'no handoff: never, whatever was edited');

  s.node([MEM, 'handoff', 'save'], s.HANDOFF);
  assert.equal(s.stop().status, 0, 'saved, nothing edited since');
  s.edit('src/a.ts');
  const first = s.stop();
  assert.equal(first.status, 2, 'edited after the save: the turn continues');
  assert.match(first.stderr, /edited files after the handoff of `feature\/x` was last saved[\s\S]*handoff show[\s\S]*handoff save/);
  assert.equal(s.stop({ stop_hook_active: true }).status, 0, 'Claude Code says the hook already ran: never a loop');
  assert.equal(s.stop().status, 0, 'and this state was already asked about');
  assert.equal(s.stop({ session_id: 's2' }).status, 0, 'another session did not edit anything');

  s.edit('src/b.ts');
  assert.equal(s.stop().status, 2, 'a new edit is a new state');
  s.node([MEM, 'handoff', 'save'], s.HANDOFF);
  assert.equal(s.stop().status, 0, 'saved again: silent');

  s.node([MEM, 'handoff', 'done']);
  s.edit('src/c.ts');
  assert.equal(s.stop().status, 0, 'the task is closed: silent');
});

test('Bash hook: a subagent call leaves the note for the main thread', () => {
  const s = sandbox();
  pushNote(s.repo, 'batch 3 verdict: done');
  const bash = (o = {}) => s.hook('posttooluse-bash.mjs', { tool_name: 'Bash', tool_input: { command: 'echo hi' }, tool_response: { stdout: 'hi', stderr: '' }, ...o });
  assert.equal(bash({ agent_id: 'impl-1' }), '', 'a subagent never consumes it');
  const stopped = s.stop();
  assert.equal(stopped.status, 2, 'Stop still has it for the main thread');
  assert.match(stopped.stderr, /batch 3 verdict: done/);
  assert.equal(s.stop().status, 0, 'delivered once');
});

test('Stop ignores edits that did not happen and plan batch work; progress is derived', () => {
  const s = sandbox();
  const PLAN = ['## Plan', 'Goal: x', '### Batch 1 — a', '- `src/a.ts` — x', 'Accept: `npx vitest run src/a.test.ts`', '### Batch 2 — b', '- `src/b.ts` — y', 'Accept: `npx vitest run src/b.test.ts`'].join('\n');
  s.node([MEM, 'handoff', 'plan'], PLAN);
  const denied = JSON.parse(s.edit('src/a.ts')).hookSpecificOutput;
  assert.equal(denied.permissionDecision, 'deny', 'plan pending approval');
  assert.equal(s.stop().status, 0, 'a denied edit changed nothing');
  s.edit('src/a.ts', { agent_id: 'impl-1' });
  assert.equal(s.stop().status, 0, 'batch work under a plan: its progress is derived, not the model\'s to write');

  // Batch 1 passes, by the implementer's own transcript.
  const t = join(s.dir, 'agent.jsonl');
  writeFileSync(t, [
    { type: 'user', message: { content: `${CONTEXT_OPEN}\n${PLAN}\n${CONTEXT_CLOSE}\n\nBatch 1 — a` } },
    { type: 'assistant', message: { content: [{ type: 'tool_use', id: 'e', name: 'Edit', input: { file_path: 'x' } }] } },
    { type: 'assistant', message: { content: [{ type: 'tool_use', id: 'r', name: 'Bash', input: { command: 'npx vitest run src/a.test.ts' } }] } },
    { type: 'user', message: { content: [{ type: 'tool_result', tool_use_id: 'r', content: 'ok' }] } },
  ].map((l) => JSON.stringify(l)).join('\n') + '\n');
  spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', join(HOOKS, 'subagentstop.mjs')], {
    input: JSON.stringify({ cwd: s.repo, agent_id: 'impl-1', agent_type: 'nxy:implementer', agent_transcript_path: t }), encoding: 'utf8', env: s.env,
  });
  assert.match(s.node([MEM, 'handoff', 'show']).stdout, /Progress \(recorded by nxy, not by the model\): batch 1 ✔, 2 pending$/m);

  const q = approvalQuestion(planHash(PLAN));
  writeFileSync(s.main, [
    { type: 'assistant', message: { usage: { cache_read_input_tokens: 20_000 } } },
    { type: 'assistant', message: { content: [{ type: 'tool_use', id: 'q1', name: 'AskUserQuestion', input: { questions: [{ question: q }] } }] } },
    { type: 'user', message: { content: [{ type: 'tool_result', tool_use_id: 'q1', content: 'ok' }] }, toolUseResult: { answers: { [q]: APPROVE } } },
  ].map((l) => JSON.stringify(l)).join('\n') + '\n');
  const out = JSON.parse(s.hook('pretooluse-agent.mjs', { tool_name: 'Agent', tool_input: { subagent_type: 'nxy:implementer', prompt: 'Batch 2 — b' } })).hookSpecificOutput;
  assert.match(out.updatedInput.prompt, /Progress \(recorded by nxy[^\n]*batch 1 ✔, 2 pending\n<\/nxy-context>/, 'the implementer sees it too');
});

test('Stop ignores edits outside every git root (Claude\'s own memory dir); repo edits still count', () => {
  const s = sandbox();
  s.node([MEM, 'handoff', 'save'], s.HANDOFF);
  // Native path of a file with no .git above it (backslashes + drive letter on Windows).
  const outside = join(s.dir, 'claude-home', 'projects', 'p', 'memory', 'note.md');
  mkdirSync(dirname(outside), { recursive: true });
  writeFileSync(outside, 'x');
  s.hook('pretooluse-edit.mjs', { tool_name: 'Edit', tool_input: { file_path: outside } });
  assert.equal(s.stop().status, 0, 'a file outside the project and every git root is not the task\'s work');
  s.edit('src/a.ts');
  assert.equal(s.stop().status, 2, 'an edit inside the repo still blocks once');
  assert.equal(s.stop().status, 0);
});

// No leak reproduced in this tree (installed hooks identical); the Stop message that started this was legitimate.
test('Stop ignores an MSYS-style path (/c/...) to a file outside every git root', { skip: process.platform !== 'win32' && 'MSYS drive paths only exist on Windows' }, () => {
  const s = sandbox();
  s.node([MEM, 'handoff', 'save'], s.HANDOFF);
  const outside = join(s.dir, 'claude-home', 'memory', 'note.md');
  mkdirSync(dirname(outside), { recursive: true });
  writeFileSync(outside, 'x');
  const msys = outside.replace(/^([A-Za-z]):/, (_, d) => `/${d.toLowerCase()}`).replace(/\\/g, '/');
  s.hook('pretooluse-edit.mjs', { tool_name: 'Edit', tool_input: { file_path: msys } });
  assert.equal(s.stop().status, 0, 'normalised, still outside every git root: not the task\'s work');
});

test('Stop counts an edit in another git repo (not the project) as work: current behaviour, multi-repo', () => {
  const s = sandbox();
  s.node([MEM, 'handoff', 'save'], s.HANDOFF);
  const other = join(s.dir, 'other-repo');
  mkdirSync(join(other, '.git'), { recursive: true });
  const file = join(other, 'src', 'x.ts');
  mkdirSync(dirname(file), { recursive: true });
  writeFileSync(file, 'x');
  s.hook('pretooluse-edit.mjs', { tool_name: 'Edit', tool_input: { file_path: file } });
  assert.equal(s.stop().status, 2, 'a file under an ancestor .git that is not the project is a git root of its own: it counts');
});
