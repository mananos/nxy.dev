// @ts-check
/** The light inventory of repos without git (1.0.1): taken once per task, content-hashed, diffed at review. */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, utimesSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { gitBranch, MAX_COPY_BYTES } from '../core/paths.mjs';
import { baselineDir, clearBaseline, reviewFiles, snapshot } from '../hosts/claude-code/baseline.mjs';
import { INVENTORY_CAP, takeInventory } from '../hosts/claude-code/inventory.mjs';
import { resolveRepos } from '../hosts/claude-code/repos.mjs';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const MEM = join(ROOT, 'hosts', 'claude-code', 'entries', 'mem.mjs');
const hasGit = spawnSync('git', ['--version'], { encoding: 'utf8' }).status === 0;
const fwd = (p) => p.replace(/\\/g, '/');

const planWith = (repos = [], file = 'a.js', extra = '') => [
  '## Plan', `Goal: x${extra}`,
  ...(repos.length ? ['### Repos', '- `.` — main', ...repos.map((r) => `- \`${fwd(r)}\` — other`)] : []),
  '### Batch 1 — api: files', `- \`${file}:1\` — change it`, 'Accept: `node -v`',
].join('\n');

function setup() {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-inv-'));
  const env = { ...process.env, NXY_HOME: join(dir, 'home') };
  const A = join(dir, 'A');
  mkdirSync(A, { recursive: true });
  const plan = (cwd, text) => spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', MEM, 'handoff', 'plan', '--cwd', cwd], {
    input: text, encoding: 'utf8', env: { ...env, CLAUDE_PROJECT_DIR: cwd }, cwd,
  });
  return { dir, A, plan };
}
const invFile = (cwd) => join(baselineDir(cwd, gitBranch(cwd)), 'inventory.json');
const readInv = (cwd) => JSON.parse(readFileSync(invFile(cwd), 'utf8'));
const old = (p) => utimesSync(p, new Date(2020, 0, 1), new Date(2020, 0, 1));
const later = (p, s = 60) => utimesSync(p, new Date(Date.now() + s * 1000), new Date(Date.now() + s * 1000));
const review = (cwd) => reviewFiles(cwd, gitBranch(cwd));
const byPath = (rv) => Object.fromEntries(rv.files.map((f) => [f.path, f]));

test('inventory: new, modified and deleted files outside the copies; unchanged and skipped folders absent', () => {
  const { A, plan } = setup();
  for (const f of ['a.js', 'keep.js', 'gone.js', 'mod.js']) writeFileSync(join(A, f), `${f}\n`);
  mkdirSync(join(A, 'node_modules'));
  writeFileSync(join(A, 'node_modules', 'm.js'), 'm\n');
  assert.equal(plan(A, planWith()).status, 0);
  writeFileSync(join(A, 'mod.js'), 'changed content\n');
  writeFileSync(join(A, 'fresh.js'), 'one\ntwo\n');
  rmSync(join(A, 'gone.js'));
  writeFileSync(join(A, 'node_modules', 'm.js'), 'm2\n');
  writeFileSync(join(A, '.nxy', 'local', 'x.txt'), 'x');
  const f = byPath(review(A));
  assert.deepEqual(Object.keys(f).sort(), ['fresh.js', 'gone.js', 'mod.js']);
  assert.equal(f['fresh.js'].status, 'new');
  assert.equal(f['fresh.js'].source, 'inventory');
  assert.equal(f['fresh.js'].content, 'one\ntwo\n');
  assert.equal(f['mod.js'].status, "changed outside the plan's copies; no before");
  assert.equal(f['mod.js'].diff, '');
  assert.equal(f['gone.js'].status, 'deleted (no before)');
  const rv = review(A);
  assert.equal(rv.coverage.length, 1);
  assert.equal(rv.coverage[0].skipped, null);
});

test('inventory: a file with a copy shows once, as the copy', () => {
  const { A, plan } = setup();
  writeFileSync(join(A, 'a.js'), 'before\n');
  assert.equal(plan(A, planWith()).status, 0);
  writeFileSync(join(A, 'a.js'), 'after\n');
  const rv = review(A);
  assert.deepEqual(rv.files.map((f) => [f.path, f.source]), [['a.js', 'copy']]);
});

test('inventory: content hash makes touch and same-bytes rewrites noise; real changes are reported', () => {
  const { A, plan } = setup();
  for (const f of ['touch.js', 'same.js', 'size.js', 'len.js']) writeFileSync(join(A, f), 'abcd\n'), old(join(A, f));
  assert.equal(plan(A, planWith([], 'z.js')).status, 0);
  const inv = readInv(A).roots[0];
  assert.equal(typeof inv.files['touch.js'][2], 'string');
  later(join(A, 'touch.js'));
  writeFileSync(join(A, 'same.js'), 'abcd\n');
  later(join(A, 'same.js'), 120);
  writeFileSync(join(A, 'size.js'), 'wxyz\n');
  later(join(A, 'size.js'));
  writeFileSync(join(A, 'len.js'), 'abcdef\n');
  assert.deepEqual(Object.keys(byPath(review(A))).sort(), ['len.js', 'size.js']);
});

test('inventory: a file over the copy limit keeps size and mtime only; a touch reports it', () => {
  const { A, plan } = setup();
  const big = join(A, 'big.bin');
  writeFileSync(big, Buffer.alloc(MAX_COPY_BYTES + 10, 97));
  old(big);
  assert.equal(plan(A, planWith([], 'z.js')).status, 0);
  assert.equal(readInv(A).roots[0].files['big.bin'][2], null);
  assert.deepEqual(review(A).files, []);
  later(big);
  const f = byPath(review(A));
  assert.equal(f['big.bin'].status, 'changed (large, not diffed)');
});

test('inventory: a cap records "too many files" and the review lists no inventory files', () => {
  const { A } = setup();
  for (const f of ['1', '2', '3', '4', '5']) writeFileSync(join(A, `${f}.js`), f);
  const repos = resolveRepos(A, []);
  takeInventory(A, null, repos, { cap: 3 });
  const r = readInv(A).roots[0];
  assert.equal(r.skipped, 'too many files');
  assert.equal(r.files, null);
  assert.ok(INVENTORY_CAP >= 20000);
});

test('inventory: a git repo takes none', (t) => {
  if (!hasGit) return t.skip('git is not available');
  const { A, plan } = setup();
  execFileSync('git', ['init', '-q'], { cwd: A });
  writeFileSync(join(A, 'a.js'), 'x\n');
  assert.equal(plan(A, planWith()).status, 0);
  assert.equal(existsSync(invFile(A)), false);
  assert.deepEqual(review(A).coverage, []);
});

test('inventory: a declared non-git repo B is walked and labelled B/<rel>', () => {
  const { dir, A, plan } = setup();
  const B = join(dir, 'B');
  mkdirSync(B);
  writeFileSync(join(B, 'k.js'), 'k\n');
  assert.equal(plan(A, planWith([B])).status, 0);
  assert.equal(readInv(A).roots.length, 2);
  writeFileSync(join(B, 'n.js'), 'n\n');
  const rv = review(A);
  assert.deepEqual(rv.files.map((f) => f.path), ['B/n.js']);
  assert.equal(rv.coverage.length, 2);
});

test('inventory: one per task; a revised plan only adds new roots; handoff done removes it', () => {
  const { dir, A, plan } = setup();
  const B = join(dir, 'B');
  mkdirSync(B);
  writeFileSync(join(B, 'k.js'), 'k\n');
  writeFileSync(join(A, 'm.js'), 'm\n');
  assert.equal(plan(A, planWith()).status, 0);
  const ts = readInv(A).ts;
  writeFileSync(join(A, 'm.js'), 'm changed\n');
  assert.equal(plan(A, planWith([], 'a.js', ' revised')).status, 0);
  assert.deepEqual(review(A).files.map((f) => f.path), ['m.js']);
  assert.equal(readInv(A).roots.length, 1);
  assert.equal(plan(A, planWith([B], 'a.js', ' revised again')).status, 0);
  const inv = readInv(A);
  assert.equal(inv.roots.length, 2);
  assert.equal(inv.ts, ts);
  assert.deepEqual(review(A).files.map((f) => f.path), ['m.js']);
  clearBaseline(A, gitBranch(A));
  assert.equal(existsSync(invFile(A)), false);
});

test('inventory: a snapshot copy of a file wins over the inventory', () => {
  const { A, plan } = setup();
  writeFileSync(join(A, 'c.js'), 'before\n');
  assert.equal(plan(A, planWith([], 'z.js')).status, 0);
  snapshot(A, gitBranch(A), join(A, 'c.js'));
  writeFileSync(join(A, 'c.js'), 'after\n');
  assert.deepEqual(review(A).files.map((f) => [f.path, f.source]), [['c.js', 'copy']]);
});
