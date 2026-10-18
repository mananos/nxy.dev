// @ts-check
/**
 * Tasks end to end through `mem handoff plan` (1.2.0): a plan is born as a task that follows the checkout,
 * a second plan never silently replaces a live one, and the legacy branch-keyed handoff keeps working.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { baselineDir } from '../hosts/claude-code/baseline.mjs';
import { taskDir } from '../hosts/claude-code/plan-approval.mjs';
import { pinActiveTask, readTasks, taskKey } from '../hosts/claude-code/tasks.mjs';
import { nxyRuntimeDir } from '../core/paths.mjs';
import { armEscape } from '../hosts/claude-code/gate-state.mjs';
import { sandbox } from './sandbox.mjs';

const MEM = join(dirname(fileURLToPath(import.meta.url)), '..', 'hosts', 'claude-code', 'entries', 'mem.mjs');

const planFor = (goal, file = 'a.js') => ['## Plan', `Goal: ${goal}`, '### Batch 1 — api: files', `- \`${file}:1\` — change it`, 'Accept: `node -v`'].join('\n');

function setup() {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-tflow-'));
  const repo = join(dir, 'repo');
  mkdirSync(join(repo, '.git'), { recursive: true });
  writeFileSync(join(repo, 'a.js'), 'a\n');
  writeFileSync(join(repo, '.git', 'config'), '[remote "origin"]\n\turl = git@github.com:x/tflow.git\n');
  const env = { ...process.env, NXY_HOME: join(dir, 'home'), CLAUDE_PROJECT_DIR: repo };
  const branch = (b) => writeFileSync(join(repo, '.git', 'HEAD'), `ref: refs/heads/${b}\n`);
  branch('develop');
  const mem = (args, input) => spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', MEM, ...args, '--cwd', repo], { input, encoding: 'utf8', env, cwd: repo });
  const plan = (goal, ...extra) => mem(['handoff', 'plan', ...extra], planFor(goal));
  const show = (...extra) => JSON.parse(mem(['handoff', 'show', '--json', ...extra]).stdout);
  const tasksFile = join(nxyRuntimeDir(repo), 'tasks.json');
  const marker = (key) => join(taskDir(repo, key), 'plan.json');
  return { dir, repo, branch, mem, plan, show, tasksFile, marker };
}

test('tasks-flow: a plan is a task that follows the checkout until pinned', () => {
  const s = setup();
  const r = s.plan('Add corporate customers');
  assert.equal(r.status, 0, r.stderr);
  assert.match(r.stdout, /task add-corporate-customers-[0-9a-f]{6}\./);
  assert.match(r.stdout, /follows the checkout until the first edit/);
  const id = taskKey(s.repo);
  assert.match(id, /^add-corporate-customers-[0-9a-f]{6}$/);
  const first = s.show();
  assert.equal(first.task.id, id);
  assert.equal(first.task.goal, 'Add corporate customers');
  assert.equal(first.task.pinned, false);
  assert.ok(first.planHash);
  assert.ok(existsSync(s.marker(id)));
  assert.ok(existsSync(baselineDir(s.repo, id)), 'the plan\'s files were copied under the task');

  s.branch('feature/a');
  assert.equal(taskKey(s.repo), id, 'same task on another branch');
  const there = s.show();
  assert.equal(there.task.id, id);
  assert.equal(there.planHash, first.planHash, 'same plan');
  assert.ok(existsSync(s.marker(id)), 'same marker');
  assert.match(s.mem(['handoff', 'show']).stdout, new RegExp(`task ${id}`));
});

test('tasks-flow: a second plan with another Goal is refused and writes nothing', () => {
  const s = setup();
  assert.equal(s.plan('First goal').status, 0);
  const id = taskKey(s.repo);
  const before = {
    tasks: readFileSync(s.tasksFile, 'utf8'),
    marker: readFileSync(s.marker(id), 'utf8'),
    handoff: s.mem(['handoff', 'show', '--json']).stdout,
  };
  const r = s.plan('Something else entirely');
  assert.equal(r.status, 1);
  assert.match(r.stdout, /task in progress: "First goal"/);
  assert.ok(r.stdout.includes(id));
  assert.match(r.stdout, /handoff done --task/);
  assert.equal(readFileSync(s.tasksFile, 'utf8'), before.tasks);
  assert.equal(readFileSync(s.marker(id), 'utf8'), before.marker);
  assert.equal(s.mem(['handoff', 'show', '--json']).stdout, before.handoff);
  assert.equal(readTasks(s.repo).state.unpinned?.id, id);
});

test('tasks-flow: the same Goal revises; --task with a new Goal revises and renames', () => {
  const s = setup();
  assert.equal(s.plan('Same goal').status, 0);
  const id = taskKey(s.repo);
  const h1 = s.show().planHash;
  assert.equal(s.mem(['handoff', 'plan'], planFor('  same   GOAL ', 'b.js')).status, 0, 'same Goal ignoring case and spacing');
  assert.equal(taskKey(s.repo), id);
  const h2 = s.show().planHash;
  assert.notEqual(h2, h1, 'new plan, new hash');

  const r = s.mem(['handoff', 'plan', '--task', id], planFor('A revised goal'));
  assert.equal(r.status, 0, r.stdout);
  assert.equal(taskKey(s.repo), id);
  assert.equal(readTasks(s.repo).state.unpinned?.goal, 'A revised goal');
  assert.equal(s.show().task.goal, 'A revised goal');

  const unknown = s.mem(['handoff', 'plan', '--task', 'nope-123456'], planFor('x'));
  assert.equal(unknown.status, 1);
  assert.match(unknown.stdout, /no task with that id/);
});

test('tasks-flow: on a branch with a pinned task a plan revises it; the unpinned one is untouched', () => {
  const s = setup();
  s.branch('feature/a');
  assert.equal(s.plan('Task A').status, 0);
  const a = taskKey(s.repo);
  pinActiveTask(s.repo);
  const aMarker = readFileSync(s.marker(a), 'utf8');

  s.branch('develop');
  assert.equal(taskKey(s.repo), 'develop', 'a pinned task does not follow the checkout');
  assert.equal(s.plan('Task B').status, 0);
  const b = taskKey(s.repo);
  assert.notEqual(b, a);
  assert.equal(readFileSync(s.marker(a), 'utf8'), aMarker, 'planning B leaves the marker of A intact');

  s.branch('feature/a');
  assert.equal(taskKey(s.repo), a, 'the pinned task wins on its own branch');
  const r = s.plan('Task A, again', '--task', a);
  assert.equal(r.status, 0, r.stdout);
  const t = readTasks(s.repo).state;
  assert.equal(t.branches['feature/a'].id, a);
  assert.equal(t.unpinned?.id, b);
  assert.equal(t.unpinned?.goal, 'Task B');
  assert.ok(existsSync(s.marker(b)));

  const same = s.plan('task a, again');
  assert.equal(same.status, 0, 'same Goal on its branch revises the pinned task');
  assert.equal(readTasks(s.repo).state.unpinned?.id, b, 'B still untouched');
});

test('tasks-flow: a different Goal on a pinned task is refused without --task and leaves it byte-identical', () => {
  const s = setup();
  s.branch('feature/a');
  assert.equal(s.plan('Task A').status, 0);
  const a = taskKey(s.repo);
  pinActiveTask(s.repo);
  const before = [readFileSync(s.tasksFile, 'utf8'), readFileSync(s.marker(a), 'utf8'), s.show().body];
  const r = s.plan('Something else entirely');
  assert.equal(r.status, 1, r.stdout);
  assert.match(r.stdout, /Task A/);
  assert.match(r.stdout, new RegExp(`handoff plan --task ${a}`));
  assert.deepEqual([readFileSync(s.tasksFile, 'utf8'), readFileSync(s.marker(a), 'utf8'), s.show().body], before);
  const ok = s.plan('Something else entirely', '--task', a);
  assert.equal(ok.status, 0, ok.stdout);
  assert.equal(readTasks(s.repo).state.branches['feature/a'].goal, 'Something else entirely');
});

test('tasks-flow: a corrupt tasks.json keeps the branch key, stays untouched and is noted once', () => {
  const s = setup();
  mkdirSync(dirname(s.tasksFile), { recursive: true });
  writeFileSync(s.tasksFile, '{ not json');
  const r = s.plan('Whatever');
  assert.equal(r.status, 0, r.stderr);
  assert.match(r.stdout, /tasks\.json is unreadable/);
  assert.equal(taskKey(s.repo), 'develop');
  assert.ok(existsSync(s.marker('develop')), 'saved under the branch key');
  assert.equal(readFileSync(s.tasksFile, 'utf8'), '{ not json');
  const again = s.plan('Another goal');
  assert.equal(again.status, 0, 'no refusal without a readable tasks.json');
  assert.doesNotMatch(again.stdout, /unreadable/, 'noted once');
  assert.equal(readFileSync(s.tasksFile, 'utf8'), '{ not json');
});

test('tasks-flow: a legacy branch handoff with a plan is revised in place', () => {
  const s = setup();
  mkdirSync(dirname(s.tasksFile), { recursive: true });
  writeFileSync(s.tasksFile, 'garbage');
  assert.equal(s.plan('Legacy goal').status, 0);
  writeFileSync(s.tasksFile, '{}'); // readable and empty: tasks exist again, the handoff stays the branch's
  const refused = s.plan('A different goal');
  assert.equal(refused.status, 1, 'a different Goal is not a silent overwrite');
  assert.match(refused.stdout, /Legacy goal/);
  assert.match(s.show().body, /Goal: Legacy goal/);
  const r = s.plan('A different goal', '--task', 'develop');
  assert.equal(r.status, 0, r.stdout);
  assert.equal(taskKey(s.repo), 'develop', 'still keyed by the branch');
  assert.match(s.show().body, /Goal: A different goal/);
  assert.equal(readTasks(s.repo).state.unpinned, null, 'no task was born');
});

test('tasks-flow: a plan-less legacy branch handoff is absorbed into the new task', () => {
  const s = setup();
  const saved = s.mem(['handoff', 'save'], 'route: implementer\n## Done\n- spiked the parser\n## Next\n- plan it');
  assert.equal(saved.status, 0, saved.stdout);
  assert.equal(s.plan('Parser rewrite').status, 0);
  const id = taskKey(s.repo);
  assert.notEqual(id, 'develop');
  const body = s.show().body;
  assert.match(body, /spiked the parser/);
  assert.match(body, /Goal: Parser rewrite/);
  assert.equal(s.show('--task', 'develop').exists, false, 'the branch-keyed handoff is gone');
});

// ---- pin on first edit; done / delete close the task (batch 7) ----

const HOOKS = join(dirname(MEM), '..', 'hooks');
const hookIn = (s, file, input) => spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', join(HOOKS, file)], {
  input: JSON.stringify({ cwd: s.repo, ...input }), encoding: 'utf8', env: { ...process.env, NXY_HOME: join(s.dir, 'home'), CLAUDE_PROJECT_DIR: s.repo }, cwd: s.repo,
}).stdout;
/** Edit by a subagent (the approval checkpoint is for the main thread) or by the unapproved main thread (denied). */
const subEdit = (s) => hookIn(s, 'pretooluse-edit.mjs', { tool_name: 'Edit', agent_id: 'a1', tool_input: { file_path: join(s.repo, 'a.js') } });
const mainEdit = (s) => hookIn(s, 'pretooluse-edit.mjs', { tool_name: 'Edit', tool_input: { file_path: join(s.repo, 'a.js') } });

test('tasks-flow: an allowed edit pins the task; a denied one does not', () => {
  const s = setup();
  s.branch('feature/a');
  assert.equal(s.plan('Pin me').status, 0);
  const id = taskKey(s.repo);
  assert.match(mainEdit(s), /deny/, 'unapproved plan: the main thread edit is denied');
  assert.equal(readTasks(s.repo).state.unpinned?.id, id, 'a denied edit does not pin');
  assert.equal(subEdit(s).trim(), '');
  assert.equal(readTasks(s.repo).state.branches['feature/a']?.id, id, 'an allowed edit pins');
  assert.equal(readTasks(s.repo).state.unpinned, null);
  s.branch('develop');
  assert.equal(s.show().task.id, 'develop', 'no plan on another branch');
  assert.equal(s.show().planHash, null);
  s.branch('feature/a');
  assert.equal(s.show().task.id, id);
  assert.ok(s.show().planHash);
});

test('tasks-flow: a console write to a project file pins the task', () => {
  const s = setup();
  s.branch('feature/a');
  assert.equal(s.plan('Pin by bash').status, 0);
  const id = taskKey(s.repo);
  hookIn(s, 'pretooluse-bash.mjs', { tool_name: 'Bash', tool_input: { command: 'echo x > a.js' } });
  assert.equal(readTasks(s.repo).state.branches['feature/a']?.id, id);
});

test('tasks-flow: done archives by Goal with its files and closes the task; a new plan is born', () => {
  const s = setup();
  s.branch('feature/a');
  assert.equal(s.plan('Ship the thing').status, 0);
  const id = taskKey(s.repo);
  subEdit(s);
  writeFileSync(join(s.repo, 'a.js'), 'changed\n');
  const r = s.mem(['handoff', 'done']);
  assert.equal(r.status, 0, r.stderr);
  const date = new Date().toISOString().slice(0, 10);
  assert.ok(r.stdout.includes(`feature: Ship the thing · feature/a · ${date}`), r.stdout);
  const archivedId = /as (\S+)/.exec(r.stdout)?.[1] || '';
  const got = JSON.parse(s.mem(['get', archivedId, '--json']).stdout);
  assert.ok(got.files.includes('a.js'), `file edge: ${got.files}`);
  assert.deepEqual(readTasks(s.repo).state.branches, {});
  assert.equal(readTasks(s.repo).state.unpinned, null);
  assert.ok(!existsSync(s.marker(id)));
  assert.equal(s.plan('A brand new goal').status, 0, 'no refusal after done');
});

test('tasks-flow: handoff delete --task removes that task only; mem delete of a live handoff closes it', () => {
  const s = setup();
  s.branch('feature/a');
  assert.equal(s.plan('Task A').status, 0);
  const a = taskKey(s.repo);
  subEdit(s); // pins A
  s.branch('develop');
  assert.equal(s.plan('Task B').status, 0);
  const b = taskKey(s.repo);
  const aMarker = readFileSync(s.marker(a), 'utf8');
  const aHandoff = s.show('--task', a).body;
  const r = s.mem(['handoff', 'delete', '--task', b]);
  assert.equal(r.status, 0, r.stderr);
  assert.equal(s.show('--task', b).exists, false);
  assert.equal(readTasks(s.repo).state.unpinned, null);
  assert.ok(!existsSync(s.marker(b)));
  assert.equal(readFileSync(s.marker(a), 'utf8'), aMarker, 'A marker intact');
  assert.equal(s.show('--task', a).body, aHandoff, 'A handoff intact');
  assert.ok(existsSync(baselineDir(s.repo, a)), 'A baseline intact');

  const live = s.mem(['list', '--type', 'handoff', '--json']);
  const row = JSON.parse(live.stdout).results.find((m) => m.title.includes('Task A'));
  assert.ok(row, live.stdout);
  assert.match(r.stdout, /other worktrees is not/);
  const del = s.mem(['delete', row.id]);
  assert.equal(del.status, 0);
  assert.match(del.stdout, /other worktrees is not/);
  assert.equal(readTasks(s.repo).state.branches['feature/a'], undefined, 'the task is gone with its handoff');
  assert.ok(!existsSync(s.marker(a)));
});

// ---- SessionStart offers a task whose branch is gone, once (batch 8) ----

const startHook = (s) => {
  const home = join(s.dir, 'uhome');
  mkdirSync(home, { recursive: true });
  const out = spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', join(HOOKS, 'sessionstart.mjs')], {
    input: JSON.stringify({ cwd: s.repo }), encoding: 'utf8', cwd: s.repo,
    env: { ...process.env, HOME: home, USERPROFILE: home, NXY_HOME: join(s.dir, 'home'), CLAUDE_PROJECT_DIR: s.repo },
  }).stdout;
  return out.trim() ? JSON.parse(out).hookSpecificOutput?.additionalContext || '' : '';
};
const pinnedTo = (s, b) => {
  s.branch(b);
  assert.equal(s.plan('Gone task').status, 0);
  const id = taskKey(s.repo);
  subEdit(s);
  assert.equal(readTasks(s.repo).state.branches[b]?.id, id);
  return id;
};

test('tasks-flow: sessionstart offers a task whose branch is gone, once', () => {
  const s = setup();
  const id = pinnedTo(s, 'feature/a'); // no refs/heads/feature/a in the fake repo: the branch is gone
  s.branch('develop');
  const first = startHook(s);
  assert.match(first, /Gone task/);
  assert.ok(first.includes(`handoff done --task ${id}`) && first.includes(`handoff delete --task ${id}`), first);
  assert.doesNotMatch(startHook(s), /handoff done --task/, 'offered once');
  assert.equal(readTasks(s.repo).state.branches['feature/a']?.offered, true);
});

test('tasks-flow: sessionstart offers nothing when the ref exists or memory is manual', () => {
  const s = setup();
  pinnedTo(s, 'feature/a');
  mkdirSync(join(s.repo, '.git', 'refs', 'heads', 'feature'), { recursive: true });
  writeFileSync(join(s.repo, '.git', 'refs', 'heads', 'feature', 'a'), 'abc\n');
  assert.doesNotMatch(startHook(s), /handoff done --task/);

  const m = setup();
  pinnedTo(m, 'feature/a');
  mkdirSync(join(m.repo, '.nxy'), { recursive: true });
  writeFileSync(join(m.repo, '.nxy', 'config.json'), JSON.stringify({ memory: { mode: 'manual' } }));
  assert.doesNotMatch(startHook(m), /handoff done --task/);
  assert.equal(readTasks(m.repo).state.branches['feature/a']?.offered, undefined, 'not marked offered either');
});

test('tasks-flow: sessionstart notes a corrupt tasks.json once', () => {
  const s = setup();
  mkdirSync(dirname(s.tasksFile), { recursive: true });
  writeFileSync(s.tasksFile, '{ not json');
  assert.match(startHook(s), /tasks\.json is unreadable/);
  assert.doesNotMatch(startHook(s), /unreadable/);
});

test('tasks-flow: an approved main-thread edit pins the unpinned task', () => {
  const sb = sandbox(planFor('Main thread pin')); // plans on feature/x
  const id = sb.key;
  assert.equal(readTasks(sb.repo).state.unpinned?.id, id);
  const edit = (transcript) => sb.hook('pretooluse-edit.mjs', { tool_name: 'Edit', transcript_path: transcript, tool_input: { file_path: join(sb.repo, 'a.js') } });
  assert.match(edit(join(sb.dir, 'none.jsonl')), /deny/, 'not approved: denied');
  assert.equal(readTasks(sb.repo).state.unpinned?.id, id);
  assert.match(edit(sb.main()), /deny/, 'approved: the main thread goes through implementers');
  assert.equal(readTasks(sb.repo).state.unpinned?.id, id, 'still not pinned');
  armEscape(sb.repo); // the user\'s /nxy:gate once
  assert.equal(edit(sb.main()).trim(), '', 'approved through the transcript + gate once: allowed');
  assert.equal(readTasks(sb.repo).state.branches['feature/x']?.id, id);
  assert.equal(readTasks(sb.repo).state.unpinned, null);
});
