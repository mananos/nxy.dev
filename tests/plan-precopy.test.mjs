// @ts-check
/**
 * Pre-copy at approval: every file the plan names gets its "before" when the plan is saved, so a
 * change made with no edit hook (a script) still reviews against it. Temp dirs only, no git needed.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { gitBranch } from '../core/paths.mjs';
import { changedFiles, readBaseline, reviewFiles } from '../hosts/claude-code/baseline.mjs';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const MEM = join(ROOT, 'hosts', 'claude-code', 'entries', 'mem.mjs');
const fwd = (p) => p.replace(/\\/g, '/');

function setup() {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-precopy-'));
  const env = { ...process.env, NXY_HOME: join(dir, 'home') };
  const A = join(dir, 'A');
  mkdirSync(A, { recursive: true });
  const plan = (cwd, text) => spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', MEM, 'handoff', 'plan', '--cwd', cwd], {
    input: text, encoding: 'utf8', env: { ...env, CLAUDE_PROJECT_DIR: cwd }, cwd,
  });
  return { dir, A, plan };
}

const planOf = (files, repos = [], extra = []) => [
  '## Plan', 'Goal: x',
  ...(repos.length ? ['### Repos', '- `.` — main', ...repos.map((r) => `- \`${fwd(r)}\` — other`)] : []),
  '### Batch 1 — api: files', ...files.map((f) => `- \`${f}\` — change it`), ...extra, 'Accept: `node -v`',
].join('\n');

test('precopy: a named file has an entry and a copy; a script rewrite reviews as (copy) with the diff', () => {
  const { A, plan } = setup();
  writeFileSync(join(A, 'a.js'), 'one\ntwo\nthree\n');
  const r = plan(A, planOf(['a.js:2']));
  assert.equal(r.status, 0, r.stdout + r.stderr);
  assert.match(r.stdout, /copied the "before" of 1 file\(s\) the plan names/);
  const b = readBaseline(A, gitBranch(A));
  const e = b[resolve(A, 'a.js')];
  assert.ok(e && e.existed && e.copy);
  writeFileSync(join(A, 'a.js'), 'one\nTWO\nthree\n');
  const f = changedFiles(A, gitBranch(A));
  assert.deepEqual(f.map((x) => [x.path, x.status, x.source]), [['a.js', 'modified', 'copy']]);
  assert.match(f[0].diff, /-two/);
  assert.match(f[0].diff, /\+TWO/);
});

test('precopy: a file that does not exist yet is existed:false and reviews as new once a script creates it', () => {
  const { A, plan } = setup();
  const r = plan(A, planOf(['src/new.js']));
  assert.equal(r.status, 0, r.stdout + r.stderr);
  const e = readBaseline(A, gitBranch(A))[resolve(A, 'src', 'new.js')];
  assert.equal(e.existed, false);
  mkdirSync(join(A, 'src'));
  writeFileSync(join(A, 'src', 'new.js'), 'x\n');
  assert.deepEqual(changedFiles(A, gitBranch(A)).map((x) => [x.path, x.status]), [['src/new.js', 'new']]);
});

test('precopy: a file in a declared repo is found; a path outside every root is skipped', () => {
  const { dir, A, plan } = setup();
  const B = join(dir, 'B');
  mkdirSync(join(B, 'lib'), { recursive: true });
  writeFileSync(join(B, 'lib', 'only-b.js'), 'b\n');
  writeFileSync(join(dir, 'outside.js'), 'o\n');
  const r = plan(A, planOf(['lib/only-b.js', '../outside.js'], [B]));
  assert.equal(r.status, 0, r.stdout + r.stderr);
  const b = readBaseline(A, gitBranch(A));
  assert.deepEqual(Object.keys(b), [resolve(B, 'lib', 'only-b.js')]);
  assert.equal(b[resolve(B, 'lib', 'only-b.js')].existed, true);
});

test('precopy: saving again keeps the first copy; a revised plan copies only the new file', () => {
  const { A, plan } = setup();
  writeFileSync(join(A, 'a.js'), 'orig\n');
  writeFileSync(join(A, 'b.js'), 'borig\n');
  plan(A, planOf(['a.js']));
  const first = readBaseline(A, gitBranch(A))[resolve(A, 'a.js')];
  writeFileSync(join(A, 'a.js'), 'edited\n');
  const again = plan(A, planOf(['a.js']));
  assert.doesNotMatch(again.stdout, /copied the "before"/);
  const revised = plan(A, planOf(['a.js', 'b.js'], [], ['Risks: revised']));
  assert.match(revised.stdout, /copied the "before" of 1 file/);
  const b = readBaseline(A, gitBranch(A));
  assert.equal(b[resolve(A, 'a.js')].copy, first.copy);
  assert.ok(b[resolve(A, 'b.js')].copy);
});

const hasGit = spawnSync('git', ['--version'], { encoding: 'utf8' }).status === 0;
const planOfX = (files, extra = []) => planOf(files, [], extra);

test('precopy: revised plan does not swallow a script change git already reports (git)', (t) => {
  if (!hasGit) return t.skip('git is not available');
  const { A, plan } = setup();
  const git = (...a) => spawnSync('git', ['-c', 'user.email=t@t', '-c', 'user.name=t', ...a], { cwd: A, encoding: 'utf8' });
  git('init', '-q');
  writeFileSync(join(A, 'x.js'), 'one\n');
  writeFileSync(join(A, 'y.js'), 'why\n');
  git('add', '.');
  git('commit', '-q', '-m', 'init');
  plan(A, planOfX(['y.js']));
  writeFileSync(join(A, 'x.js'), 'ONE\n'); // a script, no hook
  const r = plan(A, planOfX(['x.js', 'y.js'], ['Risks: revised']));
  assert.doesNotMatch(r.stdout, /copied the "before"/);
  assert.ok(!readBaseline(A, gitBranch(A))[resolve(A, 'x.js')]);
  const f = reviewFiles(A, gitBranch(A)).files.find((x) => x.path === 'x.js');
  assert.ok(f);
  assert.equal(f.source, 'git');
  assert.match(f.diff, /\+ONE/);
});

test('precopy: revised plan does not swallow a script change the inventory reports (no git)', () => {
  const { A, plan } = setup();
  writeFileSync(join(A, 'x.js'), 'one\n');
  writeFileSync(join(A, 'y.js'), 'why\n');
  writeFileSync(join(A, 'z.js'), 'zed\n');
  plan(A, planOfX(['y.js']));
  writeFileSync(join(A, 'x.js'), 'ONE!\n');
  const r = plan(A, planOfX(['x.js', 'z.js', 'y.js'], ['Risks: revised']));
  assert.match(r.stdout, /copied the "before" of 1 file/); // z.js, unchanged: still pre-copied
  const b = readBaseline(A, gitBranch(A));
  assert.ok(!b[resolve(A, 'x.js')]);
  assert.ok(b[resolve(A, 'z.js')]);
  const f = reviewFiles(A, gitBranch(A)).files.find((x) => x.path === 'x.js');
  assert.ok(f);
  assert.equal(f.source, 'inventory');
});
