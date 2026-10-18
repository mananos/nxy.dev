// @ts-check
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, existsSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import {
  emptyTasks, planDecision, refuseMessage, resolveTask, sameGoal, staleTaskHint, staleTasks, taskIdFromGoal,
  withOffered, withPinned, withUnpinned, withoutTask, withGoal, normalizeTasks, findTask,
} from '../core/tasks.mjs';
import { branchExists, nxyRuntimeDir } from '../core/paths.mjs';
import {
  activeTask, corruptNotice, forgetTask, pinActiveTask, readTasks, setTaskGoal, startTask, takeStaleTasks, taskById,
  taskKey, updateTasks,
} from '../hosts/claude-code/tasks.mjs';

const tmp = () => mkdtempSync(join(tmpdir(), 'nxy-tasks-'));
function fakeRepo(branch = 'develop') {
  const root = tmp();
  mkdirSync(join(root, '.git', 'refs', 'heads'), { recursive: true });
  setHead(root, branch);
  return root;
}
const setHead = (root, branch) => writeFileSync(join(root, '.git', 'HEAD'), `ref: refs/heads/${branch}\n`);
const mk = (id, goal = id) => ({ id, goal, created: 1 });

test('taskIdFromGoal: slug, fallback and stability', () => {
  const id = taskIdFromGoal('Add Login Flow!', 'p', 5);
  assert.match(id, /^add-login-flow-[0-9a-f]{6}$/);
  assert.equal(id, taskIdFromGoal('Add Login Flow!', 'p', 5));
  assert.notEqual(id, taskIdFromGoal('Add Login Flow!', 'p', 6));
  assert.match(taskIdFromGoal('???', 'p', 1), /^task-[0-9a-f]{6}$/);
  assert.ok(taskIdFromGoal('x'.repeat(100), 'p', 1).length <= 47);
});

test('normalizeTasks drops malformed entries', () => {
  const s = normalizeTasks({ unpinned: { id: 5 }, branches: { a: mk('a1'), b: null, c: { goal: 'x' } } });
  assert.equal(s.unpinned, null);
  assert.deepEqual(Object.keys(s.branches), ['a']);
  assert.deepEqual(normalizeTasks('junk'), emptyTasks());
});

test('resolveTask precedence: pinned > unpinned > branch fallback', () => {
  let s = emptyTasks();
  assert.deepEqual(resolveTask(s, 'dev'), { id: 'dev', label: 'dev', goal: '', branch: 'dev', pinned: false, source: 'branch' });
  assert.equal(resolveTask(s, null).label, '(no branch)');
  s = withUnpinned(s, { id: 'u1', goal: 'Do U', now: 1 });
  assert.equal(resolveTask(s, 'dev').source, 'unpinned');
  s = { ...s, branches: { x: mk('p1', 'Do P') } };
  assert.equal(resolveTask(s, 'x').id, 'p1');
  assert.equal(resolveTask(s, 'y').id, 'u1');
});

test('withPinned moves the unpinned task once; no-op on an already pinned branch', () => {
  let s = withUnpinned(emptyTasks(), { id: 'u1', goal: 'U', now: 1 });
  const p = withPinned(s, 'a', 9);
  assert.equal(p.unpinned, null);
  assert.equal(p.branches.a.id, 'u1');
  assert.equal(p.branches.a.pinned, 9);
  const s2 = withUnpinned(p, { id: 'u2', goal: 'V', now: 2 });
  assert.equal(withPinned(s2, 'a', 10), s2);
  assert.equal(withPinned(emptyTasks(), 'a', 1).unpinned, null);
  assert.ok(withPinned(s, null, 1).branches['(no branch)']);
});

test('planDecision: birth, revise, refuse', () => {
  const none = emptyTasks();
  assert.deepEqual(planDecision(none, 'dev', 'G'), { action: 'birth' });
  const s = withUnpinned(none, { id: 'u1', goal: 'Add  Thing', now: 1 });
  assert.deepEqual(planDecision(s, 'dev', ' add thing '), { action: 'revise', id: 'u1' });
  const r = planDecision(s, 'dev', 'Other');
  assert.equal(r.action, 'refuse');
  assert.equal(r.task?.id, 'u1');
  assert.deepEqual(planDecision(s, 'dev', 'Other', 'u1'), { action: 'revise', id: 'u1' });
  assert.deepEqual(planDecision(s, 'dev', 'Other', 'nope'), { action: 'refuse', task: null });
  const pinned = { ...s, branches: { a: mk('p1') } };
  assert.deepEqual(planDecision(pinned, 'a', ' P1 '), { action: 'revise', id: 'p1' });
  const rp = planDecision(pinned, 'a', 'Anything');
  assert.equal(rp.action, 'refuse');
  assert.equal(rp.task?.id, 'p1');
  assert.deepEqual(planDecision(pinned, 'a', 'Anything', 'p1'), { action: 'revise', id: 'p1' });
  assert.equal(planDecision(pinned, 'b', 'Anything').action, 'refuse');
});

test('refuseMessage and staleTaskHint name the task and the commands', () => {
  const t = { id: 'u1', goal: 'Add Thing', created: Date.UTC(2026, 9, 17), branch: null };
  const m = refuseMessage(t, 'MEM');
  for (const part of ['Add Thing', 'u1', '2026-10-17', 'MEM handoff done --task u1', 'MEM handoff delete --task u1', 'Features', 'MEM handoff plan --task u1']) assert.ok(m.includes(part), part);
  const h = staleTaskHint([{ ...t, branch: 'feature/a' }], 'MEM');
  assert.ok(h.includes('feature/a') && h.includes('MEM handoff done --task u1') && h.includes('delete --task u1'));
});

test('sameGoal, withGoal, withoutTask, findTask, goal cap', () => {
  assert.ok(sameGoal(' A  b ', 'a B'));
  assert.ok(!sameGoal('a', 'b'));
  let s = withUnpinned(emptyTasks(), { id: 'u1', goal: 'x'.repeat(300), now: 1 });
  assert.equal(s.unpinned?.goal.length, 120);
  s = withGoal(s, 'u1', 'new');
  assert.equal(findTask(s, 'u1')?.goal, 'new');
  assert.equal(findTask(s, 'u1')?.branch, null);
  assert.equal(withoutTask(s, 'u1').unpinned, null);
});

test('staleTasks and withOffered', () => {
  const s = { unpinned: null, branches: { gone: mk('g'), here: mk('h'), '(no branch)': mk('n'), abcdef1: mk('d') } };
  const stale = staleTasks(s, (b) => b === 'here');
  assert.deepEqual(stale.map((t) => t.id), ['g']);
  assert.deepEqual(staleTasks(withOffered(s, ['g']), (b) => b === 'here').map((t) => t.id), []);
});

test('host: unpinned task follows HEAD, pin fixes it, forget removes it', () => {
  const repo = fakeRepo('develop');
  assert.equal(taskKey(repo), 'develop');
  assert.ok(startTask(repo, { id: 't-1', goal: 'Goal one', now: 1 }));
  assert.equal(taskKey(repo), 't-1');
  setHead(repo, 'feature/a');
  assert.equal(activeTask(repo).id, 't-1');
  assert.ok(pinActiveTask(repo));
  assert.equal(activeTask(repo).pinned, true);
  assert.equal(taskKey(repo), 't-1');
  setHead(repo, 'develop');
  assert.equal(taskKey(repo), 'develop');
  assert.equal(taskById(repo, 't-1').branch, 'feature/a');
  assert.equal(taskById(repo, 'unknown').branch, 'unknown');
  assert.ok(forgetTask(repo, 't-1'));
  assert.equal(existsSync(join(nxyRuntimeDir(repo), 'tasks.json')), false);
  setHead(repo, 'feature/a');
  assert.equal(taskKey(repo), 'feature/a');
  rmSync(repo, { recursive: true, force: true });
});

test('host: startTask refuses a second unpinned task; setTaskGoal updates', () => {
  const repo = fakeRepo();
  assert.ok(startTask(repo, { id: 'a', goal: 'A' }));
  assert.equal(startTask(repo, { id: 'b', goal: 'B' }), false);
  assert.equal(readTasks(repo).state.unpinned?.id, 'a');
  assert.ok(setTaskGoal(repo, 'a', 'A2'));
  assert.equal(activeTask(repo).goal, 'A2');
  rmSync(repo, { recursive: true, force: true });
});

test('host: stale task is offered once', () => {
  const repo = fakeRepo('develop');
  startTask(repo, { id: 't-1', goal: 'G' });
  setHead(repo, 'feature/a');
  pinActiveTask(repo);
  setHead(repo, 'develop');
  assert.deepEqual(takeStaleTasks(repo).map((t) => t.id), ['t-1']);
  assert.deepEqual(takeStaleTasks(repo), []);
  rmSync(repo, { recursive: true, force: true });
});

test('host: corrupt tasks.json falls back to the branch, stays byte-identical, notice once', () => {
  const repo = fakeRepo('develop');
  const file = join(nxyRuntimeDir(repo), 'tasks.json');
  mkdirSync(nxyRuntimeDir(repo), { recursive: true });
  writeFileSync(file, '{not json');
  assert.equal(readTasks(repo).corrupt, true);
  assert.equal(taskKey(repo), 'develop');
  assert.equal(updateTasks(repo, (s) => withUnpinned(s, { id: 'x', goal: 'x', now: 1 })), false);
  assert.equal(startTask(repo, { id: 'x', goal: 'x' }), false);
  assert.equal(readFileSync(file, 'utf8'), '{not json');
  assert.match(String(corruptNotice(repo)), /tasks\.json/);
  assert.equal(corruptNotice(repo), null);
  assert.deepEqual(takeStaleTasks(repo), []);
  rmSync(repo, { recursive: true, force: true });
});

test('branchExists: loose ref, packed ref, worktree + commondir, missing, non-repo', () => {
  const repo = fakeRepo();
  mkdirSync(join(repo, '.git', 'refs', 'heads', 'feature'), { recursive: true });
  writeFileSync(join(repo, '.git', 'refs', 'heads', 'feature', 'loose'), 'abc\n');
  writeFileSync(join(repo, '.git', 'packed-refs'), '# pack-refs\nabc123 refs/heads/packed/one\n^def456\n');
  assert.ok(branchExists(repo, 'feature/loose'));
  assert.ok(branchExists(repo, 'packed/one'));
  assert.ok(!branchExists(repo, 'packed'));
  assert.ok(!branchExists(repo, 'missing'));

  const wt = tmp();
  const wtGit = join(repo, '.git', 'worktrees', 'wt');
  mkdirSync(wtGit, { recursive: true });
  writeFileSync(join(wtGit, 'HEAD'), 'ref: refs/heads/feature/loose\n');
  writeFileSync(join(wtGit, 'commondir'), '../..\n');
  writeFileSync(join(wt, '.git'), `gitdir: ${wtGit}\n`);
  assert.ok(branchExists(wt, 'feature/loose'));
  assert.ok(branchExists(wt, 'packed/one'));
  assert.ok(!branchExists(wt, 'missing'));

  assert.ok(branchExists(tmp(), 'anything'));
  rmSync(repo, { recursive: true, force: true });
  rmSync(wt, { recursive: true, force: true });
});
