// @ts-check
/**
 * `mem related`: closed tasks that touched the same files, for the planner. What has to hold: ranked
 * by overlap then recency, capped in tasks and in lines, only archived tasks of this project, and
 * the CLI normalizes the paths it is given.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { openStore, saveMemory } from '../core/memory/store.mjs';
import { addEdge, fileNode } from '../core/memory/graph.mjs';
import { projectKey } from '../core/memory/scope.mjs';
import { archiveHandoff, saveHandoff } from '../core/memory/handoff.mjs';
import { MAX_RELATED_LINES, MAX_RELATED_TASKS, formatRelated, relatedTasks } from '../core/memory/related.mjs';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const MEM = join(ROOT, 'hosts', 'claude-code', 'entries', 'mem.mjs');
const PROJECT = 'github.com/example/proj';

const planBody = (goal, decisions) => `route: implementer\n## Done\n- x\n## Next\n- y\n\n## Plan\nGoal: ${goal}\nDecisions:\n${decisions.map((d) => `- ${d}`).join('\n')}\n### Batch 1 — a\n- \`x.mjs\` — y\nAccept: \`npm test\``;

/** Closes a task that touched `files`. */
function closed(db, key, goal, files, decisions, when, project = PROJECT) {
  saveHandoff(db, { project, key, label: goal, body: planBody(goal, decisions) });
  const id = archiveHandoff(db, project, key, when, { branch: 'feature/a', files, root: null });
  assert.ok(id);
  return id;
}

const store = () => openStore({ path: join(mkdtempSync(join(tmpdir(), 'nxy-rel-')), 'memory.db') });

test('relatedTasks: ranked by overlap then recency; only closed tasks of this project', () => {
  const db = store();
  const old = closed(db, 't1', 'Old one', ['a.mjs', 'b.mjs'], ['[repo] Rule one — why'], Date.UTC(2026, 0, 1));
  const fresh = closed(db, 't2', 'Fresh one', ['a.mjs'], ['[task] Only here'], Date.UTC(2026, 5, 1));
  const other = closed(db, 't3', 'Other project', ['a.mjs'], ['[repo] nope'], Date.UTC(2026, 6, 1), 'github.com/example/other');
  saveHandoff(db, { project: PROJECT, key: 'live', label: 'Live', body: planBody('Live', ['[repo] live rule']) });
  addEdge(db, 'handoff-live', fileNode('a.mjs'), 'file');
  saveMemory(db, { scope: 'project', project: PROJECT, area: null, type: 'decision', title: 'plain note', body: 'a.mjs', keywords: '' });

  const list = relatedTasks(db, { project: PROJECT, files: ['a.mjs', 'b.mjs'] });
  assert.deepEqual(list.map((t) => t.id), [old, fresh], 'two files beat one; live, other project and plain memories are out');
  assert.ok(!list.some((t) => t.id === other));
  assert.equal(list[0].overlap, 2);
  assert.deepEqual(list[0].decisions.map((d) => d.scope), ['repo']);
  assert.deepEqual(list[1].decisions.map((d) => d.scope), ['task']);

  const text = formatRelated(list, 'MEMGET');
  assert.match(text, /feature: Old one · feature\/a · 2026-01-01/);
  assert.match(text, /files: 2 \(a\.mjs, b\.mjs\)/);
  assert.match(text, /- \[repo\] Rule one — why/);
  assert.equal(formatRelated([], 'MEMGET'), '');
});

test('relatedTasks: cap of 3 tasks and 40 lines, with a pointer to the rest', () => {
  const db = store();
  const decisions = Array.from({ length: 30 }, (_, i) => `[repo] Rule ${i} — because ${i}`);
  for (let i = 0; i < 5; i++) closed(db, `t${i}`, `Task ${i}`, ['a.mjs'], decisions, Date.UTC(2026, 0, 1 + i));
  const list = relatedTasks(db, { project: PROJECT, files: ['a.mjs'] });
  assert.equal(list.length, MAX_RELATED_TASKS);
  assert.deepEqual(list.map((t) => t.title.slice(9, 15)), ['Task 4', 'Task 3', 'Task 2'], 'newest first on a tie');
  const lines = formatRelated(list, 'MEMGET').split('\n');
  assert.ok(lines.length <= MAX_RELATED_LINES, `${lines.length} lines`);
  assert.ok(lines.some((l) => /^… \d+ more: MEMGET /.test(l)));
});

test('relatedTasks: a legacy archive with body-derived edges is included', () => {
  const db = store();
  saveHandoff(db, { project: PROJECT, key: 'feature/old', body: 'route: implementer\n## Done\n- x\n## Next\n- y' });
  const id = archiveHandoff(db, PROJECT, 'feature/old', Date.UTC(2026, 1, 1), { root: null });
  assert.ok(id);
  addEdge(db, id, fileNode('src/legacy.mjs'), 'file');
  const list = relatedTasks(db, { project: PROJECT, files: ['src/legacy.mjs'] });
  assert.equal(list.length, 1);
  assert.match(list[0].title, /^handoff \(done\)/);
  assert.deepEqual(list[0].decisions, []);
});

test('CLI: related normalizes paths, finds a closed task, and status counts the lookup', () => {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-rel-cli-'));
  const repo = join(dir, 'repo');
  mkdirSync(join(repo, '.git'), { recursive: true });
  writeFileSync(join(repo, '.git', 'HEAD'), 'ref: refs/heads/main\n');
  writeFileSync(join(repo, '.git', 'config'), '[remote "origin"]\n\turl = git@github.com:x/y.git\n');
  const home = join(dir, 'home');
  const env = { ...process.env, NXY_HOME: home, CLAUDE_PROJECT_DIR: repo };
  const mem = (...args) => spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', MEM, ...args, '--cwd', repo], { encoding: 'utf8', env });

  const { key } = projectKey(repo);
  const db = openStore({ path: join(home, 'memory', 'memory.db') });
  const id = closed(db, 't1', 'Add the thing', ['src/a.ts'], ['[repo] Keep it small — cheaper'], Date.UTC(2026, 3, 1), key);
  db.close();

  for (const p of ['src/a.ts', 'src\\a.ts', './src/a.ts', 'src/a.ts:12', join(repo, 'src', 'a.ts')]) {
    const r = mem('related', p);
    assert.equal(r.status, 0, r.stderr);
    assert.match(r.stdout, new RegExp(id), `found through ${p}`);
    assert.match(r.stdout, /- \[repo\] Keep it small — cheaper/);
  }
  assert.match(mem('related', 'src/none.ts').stdout, /no closed task touched these files/);
  assert.match(mem('related', join(dir, 'outside.ts')).stdout, /Usage: mem\.mjs related/, 'paths outside the repo are dropped');

  assert.match(mem('status').stdout, /related 30d: 6 planner lookups, 5 closed tasks delivered/);
});

test('CLI end to end: handoff plan, edit a file the plan names, handoff done, then related returns the task', () => {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-rel-e2e-'));
  const repo = join(dir, 'repo');
  mkdirSync(join(repo, '.git'), { recursive: true });
  writeFileSync(join(repo, '.git', 'HEAD'), 'ref: refs/heads/main\n');
  writeFileSync(join(repo, '.git', 'config'), '[remote "origin"]\n\turl = git@github.com:x/e2e.git\n');
  writeFileSync(join(repo, 'a.js'), 'before\n');
  const env = { ...process.env, NXY_HOME: join(dir, 'home'), CLAUDE_PROJECT_DIR: repo };
  const mem = (args, input) => spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', MEM, ...args, '--cwd', repo], { input, encoding: 'utf8', env, cwd: repo });

  const plan = ['## Plan', 'Goal: Rework a', 'Decisions:', '- [repo] Keep a small — cheaper', '### Batch 1 — api: files', '- `a.js:1` — change it', 'Accept: `node -v`'].join('\n');
  const saved = mem(['handoff', 'plan'], plan);
  assert.equal(saved.status, 0, saved.stdout + saved.stderr);

  writeFileSync(join(repo, 'a.js'), 'after\n');
  const done = mem(['handoff', 'done']);
  assert.equal(done.status, 0, done.stdout + done.stderr);
  assert.match(done.stdout, /archived handoff/);

  const r = mem(['related', 'a.js']);
  assert.equal(r.status, 0, r.stderr);
  assert.match(r.stdout, /Rework a/);
  assert.match(r.stdout, /- \[repo\] Keep a small — cheaper/);
  assert.match(mem(['related', 'other.js']).stdout, /no closed task touched these files/);
});
