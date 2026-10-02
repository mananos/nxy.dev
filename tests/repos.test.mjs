// @ts-check
/**
 * Several repos in one task (1.0.1): the plan declares them under `### Repos`, the git snapshot is
 * taken once per task, labels are unique per repo, and an undeclared repo is "Not reviewed".
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { gitBranch } from '../core/paths.mjs';
import { repoLabel, repoNames, repoOf, noteUndeclaredRepo, undeclaredRepos } from '../hosts/claude-code/repos.mjs';
import { changedFiles, reviewFiles, snapshot } from '../hosts/claude-code/baseline.mjs';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const MEM = join(ROOT, 'hosts', 'claude-code', 'entries', 'mem.mjs');
const hasGit = spawnSync('git', ['--version'], { encoding: 'utf8' }).status === 0;
const fwd = (p) => p.replace(/\\/g, '/');
const lines = (tag) => ['line1', `line2 ${tag}`, 'line3', ''].join('\n');

function mkRepo(dir, files = ['x.js', 'y.js']) {
  mkdirSync(dir, { recursive: true });
  const git = (...args) => execFileSync('git', args, { cwd: dir, encoding: 'utf8' });
  git('init', '-q');
  git('config', 'user.name', 'nxy test');
  git('config', 'user.email', 'nxy@example.com');
  git('config', 'core.autocrlf', 'false');
  for (const f of files) writeFileSync(join(dir, f), lines('orig'));
  git('add', '.');
  git('commit', '-q', '-m', 'init');
  return git;
}

const planWith = (repos, file = 'a.js') => [
  '## Plan', 'Goal: x',
  ...(repos.length ? ['### Repos', '- `.` — main', ...repos.map((r) => `- \`${fwd(r)}\` — other`)] : []),
  '### Batch 1 — api: files', `- \`${file}:1\` — change it`, 'Accept: `node -v`',
].join('\n');

function setup() {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-repos-'));
  const env = { ...process.env, NXY_HOME: join(dir, 'home') };
  const A = join(dir, 'A');
  const node = (cwd, args, input) => spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', ...args], {
    input, encoding: 'utf8', env: { ...env, CLAUDE_PROJECT_DIR: cwd }, cwd,
  });
  const plan = (cwd, text) => node(cwd, [MEM, 'handoff', 'plan', '--cwd', cwd], text);
  return { dir, A, node, plan };
}
const dirtyOf = (cwd) => JSON.parse(readFileSync(join(cwd, '.nxy', 'local', 'plan-dirty.json'), 'utf8'));

test('repos: a declared repo is snapshotted at approval; its own changes are git, the dirty ones not reviewed', (t) => {
  if (!hasGit) return t.skip('git is not available');
  const { dir, A, plan } = setup();
  const B = join(dir, 'B');
  mkRepo(A, ['a.js']);
  mkRepo(B);
  writeFileSync(join(B, 'x.js'), lines('user'));
  const r = plan(A, planWith([B]));
  assert.equal(r.status, 0, r.stdout + r.stderr);
  const rec = dirtyOf(A);
  assert.equal(rec.others.length, 1);
  assert.equal(rec.others[0].name, 'B');
  assert.deepEqual(rec.others[0].files, ['x.js']);

  writeFileSync(join(B, 'y.js'), lines('plan'));
  writeFileSync(join(B, 'x.js'), lines('user and plan'));
  const rv = reviewFiles(A, gitBranch(A));
  assert.deepEqual(rv.files.map((f) => [f.path, f.source]), [['B/y.js', 'git']]);
  assert.deepEqual(rv.notReviewed.map((n) => n.path), ['B/x.js']);
});

test('repos: an undeclared repo goes to Not reviewed; a declared path that is not a repo warns', (t) => {
  if (!hasGit) return t.skip('git is not available');
  const { dir, A, plan } = setup();
  const C = join(dir, 'C');
  const plain = join(dir, 'plain');
  mkRepo(A, ['a.js']);
  mkRepo(C);
  mkdirSync(plain);
  const r = plan(A, planWith([plain]));
  assert.match(r.stdout, /warning: .*plain is not a git repo; its changes are not reviewed through git/);
  assert.equal(noteUndeclaredRepo(A, C), true);
  assert.equal(noteUndeclaredRepo(A, C), false, 'warn once');
  assert.deepEqual(undeclaredRepos(A), [C]);
  writeFileSync(join(C, 'x.js'), lines('plan'));
  const rv = reviewFiles(A, gitBranch(A));
  assert.deepEqual(rv.files, []);
  assert.deepEqual(rv.notReviewed.map((n) => n.path), ['C/x.js']);
  assert.match(rv.notReviewed[0].reason, /C is not in the plan; its prior uncommitted changes cannot be told apart/);
});

test('repos: project without git, declared git repo B: copies plus B through git', (t) => {
  if (!hasGit) return t.skip('git is not available');
  const { dir, A, plan } = setup();
  const B = join(dir, 'B');
  mkdirSync(A);
  writeFileSync(join(A, 'a.js'), lines('orig'));
  mkRepo(B);
  assert.equal(plan(A, planWith([B])).status, 0);
  assert.equal(dirtyOf(A).available, false);
  assert.equal(dirtyOf(A).others.length, 1);
  snapshot(A, null, join(A, 'a.js'));
  writeFileSync(join(A, 'a.js'), lines('plan'));
  writeFileSync(join(B, 'y.js'), lines('plan'));
  const rv = reviewFiles(A, null);
  assert.deepEqual(rv.files.map((f) => [f.path, f.source]), [['a.js', 'copy'], ['B/y.js', 'git']]);
  assert.equal(rv.note, 'not a git repo');
});

test('repos: names are unique per repo with no configuration', () => {
  const t = mkdtempSync(join(tmpdir(), 'nxy-names-'));
  const x = join(t, 'clientX', 'api');
  const y = join(t, 'clientY', 'api');
  const z = join(t, 'web');
  const names = repoNames([x, y, z]);
  assert.equal(names.get(x), 'clientX/api');
  assert.equal(names.get(y), 'clientY/api');
  assert.equal(names.get(z), 'web');
  assert.equal(repoLabel({ root: x }, join(x, 'src', 'a.js'), names), 'clientX/api/src/a.js');
  assert.equal(repoLabel({ root: x, primary: true }, join(x, 'src', 'a.js'), names), 'src/a.js');
  assert.equal(repoLabel(null, join(t, 'z.js'), names), fwd(join(t, 'z.js')));
});

test('repos: a declared repo with the primary\'s basename; the primary stays bare', (t) => {
  if (!hasGit) return t.skip('git is not available');
  const dir = mkdtempSync(join(tmpdir(), 'nxy-same-'));
  const W = join(dir, 'clientW', 'api');
  const Z = join(dir, 'clientZ', 'api');
  const U = join(dir, 'clientU', 'api');
  mkRepo(W, ['a.js']);
  mkRepo(Z, ['a.js']);
  mkRepo(U, ['a.js']);
  const { node } = setup();
  assert.equal(node(W, [MEM, 'handoff', 'plan', '--cwd', W], planWith([Z])).status, 0);
  noteUndeclaredRepo(W, U);
  snapshot(W, gitBranch(W), join(W, 'a.js'));
  snapshot(W, gitBranch(W), join(Z, 'a.js'));
  snapshot(W, gitBranch(W), join(U, 'a.js'));
  for (const d of [W, Z, U]) writeFileSync(join(d, 'a.js'), lines('plan'));
  assert.deepEqual(changedFiles(W, gitBranch(W)).map((f) => f.path), [
    'a.js', 'clientU/api/a.js', 'clientZ/api/a.js',
  ]);
  assert.equal(repoOf(W, join(Z, 'a.js'), [Z])?.primary, false);
});

test('repos: one snapshot per task; a revised plan only adds new repos; done clears; another branch replaces', (t) => {
  if (!hasGit) return t.skip('git is not available');
  const { dir, A, plan, node } = setup();
  const B = join(dir, 'B');
  const gitA = mkRepo(A, ['a.js', 'b.js']);
  mkRepo(B);
  writeFileSync(join(A, 'a.js'), lines('user'));
  assert.equal(plan(A, planWith([])).status, 0);
  assert.deepEqual(dirtyOf(A).files, ['a.js']);

  writeFileSync(join(A, 'b.js'), lines('plan'));
  writeFileSync(join(B, 'x.js'), lines('dirty at 2nd approval'));
  assert.equal(plan(A, planWith([B], 'a.js')).status, 0, 'revised plan, new hash');
  const rec = dirtyOf(A);
  assert.deepEqual(rec.files, ['a.js'], 'b.js is not "already dirty"');
  assert.deepEqual(rec.others.map((o) => [o.name, o.files]), [['B', ['x.js']]]);
  assert.ok(reviewFiles(A, gitBranch(A)).files.some((f) => f.path === 'b.js'));

  writeFileSync(join(A, '.nxy', 'local', 'plan-extra-repos.json'), JSON.stringify({ roots: [B] }));
  node(A, [MEM, 'handoff', 'done', '--cwd', A]);
  assert.ok(!existsSync(join(A, '.nxy', 'local', 'plan-dirty.json')));
  assert.ok(!existsSync(join(A, '.nxy', 'local', 'plan-extra-repos.json')));

  // A new branch replaces a record of the old one.
  assert.equal(plan(A, planWith([])).status, 0);
  gitA('checkout', '-q', '-b', 'other');
  writeFileSync(join(A, 'b.js'), lines('on other'));
  assert.equal(plan(A, planWith([], 'b.js')).status, 0);
  assert.equal(dirtyOf(A).branch, 'other');
  assert.deepEqual(dirtyOf(A).files, ['a.js', 'b.js'], 'retaken on the new branch');
});
