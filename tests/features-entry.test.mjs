// @ts-check
/**
 * The Features entry (hosts/claude-code/entries/features.mjs) against a real temporary git repo:
 * list, new (new branch from an explicit base / existing branch), pause, resume and close.
 * Never touches the real repo; NXY_HOME is temporary.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, realpathSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { commandText, shellOf } from '../core/featuresview.mjs';
import { planHash } from '../core/plan.mjs';
import { recordBatch } from '../hosts/claude-code/verify-state.mjs';
import { writeOrchFile } from '../hosts/claude-code/orch-state.mjs';
import { MARKER_FILE } from '../core/orchestrator.mjs';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const FEATURES = join(ROOT, 'hosts', 'claude-code', 'entries', 'features.mjs');
const MEM = join(ROOT, 'hosts', 'claude-code', 'entries', 'mem.mjs');
const hasGit = spawnSync('git', ['--version'], { encoding: 'utf8' }).status === 0;
const slash = (/** @type {string} */ p) => p.replace(/\\/g, '/');
const shell = shellOf({ platform: process.platform, env: process.env });

const PLAN = ['## Plan', 'Goal: x', '### Batch 1 — api: files', '- `b.js:2` — change b', 'Accept: `./mvnw -q test`'].join('\n');

/** @returns {{dir: string, repo: string, wts: string, git: (...a: string[]) => string, gitIn: (cwd: string, ...a: string[]) => string, run: (args: string[], cwd?: string) => any, mem: (cwd: string, args: string[]) => string}} */
function setup() {
  const dir = realpathSync.native(mkdtempSync(join(tmpdir(), 'nxy-feat-')));
  const repo = join(dir, 'repo');
  mkdirSync(repo, { recursive: true });
  // On Windows a fresh object file in .git/objects can be briefly locked (AV/indexer): retry only that error.
  // Any other failure, and the last transient one, throws at once.
  const gitIn = (/** @type {string} */ cwd, /** @type {string[]} */ ...a) => {
    for (let n = 1; ; n++) {
      try {
        return execFileSync('git', a, { cwd, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] });
      } catch (e) {
        const err = /** @type {any} */ (e);
        if (n >= 3 || !/Permission denied|unable to write/.test(String(err?.stderr || ''))) throw e;
        Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, 100);
      }
    }
  };
  const git = (/** @type {string[]} */ ...a) => gitIn(repo, ...a);
  git('init', '-q', '-b', 'main');
  git('config', 'user.name', 'nxy test');
  git('config', 'user.email', 'nxy@example.com');
  git('config', 'core.autocrlf', 'false');
  writeFileSync(join(repo, 'a.txt'), 'A\n');
  git('add', '.');
  git('commit', '-q', '-m', 'A');
  const env = { ...process.env, NXY_HOME: join(dir, 'home') };
  const node = (/** @type {string} */ file, /** @type {string[]} */ args) =>
    spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', file, ...args], { encoding: 'utf8', env });
  const run = (/** @type {string[]} */ args, cwd = repo) => {
    const r = node(FEATURES, [...args, '--cwd', cwd, '--json']);
    return JSON.parse(r.stdout.trim().split('\n').pop() || '{}');
  };
  const mem = (/** @type {string} */ cwd, /** @type {string[]} */ args) => node(MEM, [...args, '--cwd', cwd]).stdout;
  return { dir, repo, wts: `${repo}.worktrees`, git, gitIn, run, mem };
}

/** @param {string} dir */
const clean = (dir) => rmSync(dir, { recursive: true, force: true });
const norm = (/** @type {string} */ p) => slash(realpathSync.native(p));

test('list: only the main worktree, free branches and bases', { skip: !hasGit }, async () => {
  const t = setup();
  try {
    t.git('branch', 'otra');
    const l = t.run(['list']);
    assert.equal(l.ok, true);
    assert.equal(l.worktrees.length, 1);
    assert.equal(l.worktrees[0].main, true);
    assert.equal(l.worktrees[0].current, true);
    assert.equal(l.worktrees[0].branch, 'main');
    assert.ok(l.branches.free.includes('otra'));
    assert.deepEqual(l.branches.busy.map((/** @type {any} */ b) => b.name), ['main']);
    assert.equal(l.defaultBranch, 'main');
    assert.deepEqual(l.bases, ['main', 'otra']);
    assert.equal(l.repoName, 'repo');
    assert.equal(l.platform, process.platform);
    assert.equal(l.shell, shell);
  } finally {
    clean(t.dir);
  }
});

test('list: defaultBranch follows origin/HEAD without network', { skip: !hasGit }, async () => {
  const t = setup();
  try {
    t.git('checkout', '-q', '-b', 'trabajo');
    assert.equal(t.run(['list']).defaultBranch, 'trabajo');
    t.git('update-ref', 'refs/remotes/origin/main', 'HEAD');
    t.git('symbolic-ref', 'refs/remotes/origin/HEAD', 'refs/remotes/origin/main');
    const l = t.run(['list']);
    assert.equal(l.defaultBranch, 'main');
    assert.equal(l.bases[0], 'main');
    assert.deepEqual([...l.bases].sort(), ['main', 'trabajo']);
  } finally {
    clean(t.dir);
  }
});

test('new --name --base creates the worktree and branch, with and without issue', { skip: !hasGit }, async () => {
  const t = setup();
  try {
    const a = t.run(['new', '--name', 'algo', '--issue', '14', '--base', 'main']);
    assert.equal(a.ok, true, JSON.stringify(a));
    assert.equal(a.branch, 'feature/#14_algo');
    assert.equal(slash(a.path), slash(join(t.wts, '14_algo')));
    assert.equal(a.command, commandText(['git', 'worktree', 'add', '-b', 'feature/#14_algo', slash(join(t.wts, '14_algo')), 'main'], shell));
    assert.ok(existsSync(join(t.wts, '14_algo', 'a.txt')));
    const b = t.run(['new', '--name', 'login rápido', '--base', 'main']);
    assert.equal(b.ok, true, JSON.stringify(b));
    assert.equal(b.branch, 'feature/login_rapido');
    assert.match(t.git('branch', '--list', 'feature/login_rapido'), /feature\/login_rapido/);
    assert.equal(t.run(['list']).worktrees.length, 3);
  } finally {
    clean(t.dir);
    clean(`${t.dir}/repo.worktrees`);
  }
});

test('new: the base is explicit, not the session HEAD', { skip: !hasGit }, async () => {
  const t = setup();
  try {
    t.git('checkout', '-q', '-b', 'base-x');
    writeFileSync(join(t.repo, 'b.txt'), 'B\n');
    t.git('add', '.');
    t.git('commit', '-q', '-m', 'B');
    t.git('checkout', '-q', 'main');
    t.git('checkout', '-q', '-b', 'sesion');
    writeFileSync(join(t.repo, 'c.txt'), 'C\n');
    t.git('add', '.');
    t.git('commit', '-q', '-m', 'C');
    const r = t.run(['new', '--name', 'x', '--base', 'base-x']);
    assert.equal(r.ok, true, JSON.stringify(r));
    const sha = (/** @type {string} */ ref) => t.git('rev-parse', ref).trim();
    const isAnc = (/** @type {string} */ a, /** @type {string} */ b) =>
      spawnSync('git', ['merge-base', '--is-ancestor', a, b], { cwd: t.repo }).status === 0;
    assert.ok(isAnc(sha('base-x'), 'feature/x'));
    assert.ok(!isAnc(sha('sesion'), 'feature/x'));
  } finally {
    clean(t.dir);
  }
});

test('new: invalid inputs are rejected without creating anything', { skip: !hasGit }, async () => {
  const t = setup();
  try {
    t.git('branch', 'ocupada');
    assert.equal(t.run(['new', '--name', 'algo']).ok, false);
    const noBase = t.run(['new', '--name', 'algo', '--base', 'nope']);
    assert.equal(noBase.ok, false);
    assert.match(noBase.reason, /nope/);
    assert.ok(!existsSync(join(t.wts, 'algo')));
    assert.equal(t.git('branch', '--list', 'feature/algo').trim(), '');
    assert.equal(t.run(['new', '--branch', 'ocupada', '--base', 'main']).ok, false);
    assert.equal(t.run(['new', '--name', 'algo', '--branch', 'ocupada']).ok, false);
    assert.equal(t.run(['new']).ok, false);
    assert.equal(t.run(['new', '--name', 'algo', '--issue', 'abc', '--base', 'main']).ok, false);
    assert.equal(t.run(['new', '--name', '..', '--base', 'main']).ok, false);
    assert.ok(!existsSync(t.wts));
  } finally {
    clean(t.dir);
  }
});

test('new: a base busy in another worktree works; path and branch collisions are rejected', { skip: !hasGit }, async () => {
  const t = setup();
  try {
    assert.equal(t.run(['new', '--name', 'uno', '--base', 'main']).ok, true);
    const fromBusy = t.run(['new', '--name', 'dos', '--base', 'feature/uno']);
    assert.equal(fromBusy.ok, true, JSON.stringify(fromBusy));
    const again = t.run(['new', '--name', 'uno', '--base', 'main']);
    assert.equal(again.ok, false);
    t.git('branch', 'feature/tres');
    const dup = t.run(['new', '--name', 'tres', '--base', 'main']);
    assert.equal(dup.ok, false);
    assert.match(dup.reason, /existente/);
    assert.ok(!existsSync(join(t.wts, 'tres')));
  } finally {
    clean(t.dir);
    clean(`${t.dir}/repo.worktrees`);
  }
});

test('new --branch: an existing free branch opens without -b; busy or missing ones are rejected', { skip: !hasGit }, async () => {
  const t = setup();
  try {
    t.git('branch', 'fix/login');
    const r = t.run(['new', '--branch', 'fix/login']);
    assert.equal(r.ok, true, JSON.stringify(r));
    assert.equal(r.command, commandText(['git', 'worktree', 'add', slash(join(t.wts, 'fix_login')), 'fix/login'], shell));
    assert.ok(!r.command.includes('-b'));
    assert.equal(t.run(['new', '--branch', 'fix/login']).ok, false);
    assert.equal(t.run(['new', '--branch', 'main']).ok, false);
    const none = t.run(['new', '--branch', 'no-existe']);
    assert.equal(none.ok, false);
    assert.ok(!existsSync(join(t.wts, 'no_existe')));
    const l = t.run(['list']);
    const busy = l.branches.busy.find((/** @type {any} */ b) => b.name === 'fix/login');
    assert.equal(norm(busy.path), norm(join(t.wts, 'fix_login')));
    assert.ok(!l.branches.free.includes('fix/login'));
  } finally {
    clean(t.dir);
    clean(`${t.dir}/repo.worktrees`);
  }
});

test('list: counts dirty files, unpushed commits, progress, pause and missing deps', { skip: !hasGit }, async () => {
  const t = setup();
  try {
    t.git('update-ref', 'refs/remotes/origin/main', 'HEAD');
    assert.equal(t.run(['new', '--name', 'p', '--base', 'main']).ok, true);
    const wt = join(t.wts, 'p');
    writeFileSync(join(wt, 'package.json'), '{}\n');
    writeFileSync(join(wt, 'nuevo.txt'), 'x\n');
    t.gitIn(wt, 'add', 'nuevo.txt');
    t.gitIn(wt, 'config', 'user.name', 'nxy test');
    t.gitIn(wt, 'commit', '-q', '-m', 'ahead');
    writeFileSync(join(wt, 'b.js'), '1\n2\n3\n');
    t.mem(wt, ['handoff', 'plan', '--body', PLAN]);
    recordBatch(wt, 'feature/p', planHash(PLAN), 1, { status: 'pass', ts: Date.now() });
    const paused = t.run(['pause', '--path', wt]);
    assert.equal(paused.ok, true, JSON.stringify(paused));
    const row = t.run(['list']).worktrees.find((/** @type {any} */ w) => !w.main);
    assert.equal(row.ahead, 1);
    assert.ok(row.dirty >= 2);
    assert.equal(row.batchTotal, 1);
    assert.equal(row.batchDone, 1);
    assert.equal(typeof row.handoffAt, 'number');
    assert.equal(typeof row.pausedAt, 'number');
    assert.equal(row.missingDeps, true);
    assert.equal(row.current, false);
    assert.equal(t.run(['list']).worktrees[0].ahead, 0);
    const resumed = t.run(['resume', '--path', wt]);
    assert.equal(resumed.ok, true);
    assert.equal(t.run(['list']).worktrees.find((/** @type {any} */ w) => !w.main).pausedAt, null);
  } finally {
    clean(t.dir);
    clean(`${t.dir}/repo.worktrees`);
  }
});

test('pause without a handoff says what to run', { skip: !hasGit }, async () => {
  const t = setup();
  try {
    assert.equal(t.run(['new', '--name', 'q', '--base', 'main']).ok, true);
    const p = t.run(['pause', '--path', join(t.wts, 'q')]);
    assert.equal(p.ok, true);
    assert.equal(p.exists, false);
    assert.match(p.advice, /handoff save/);
    assert.equal(t.run(['pause', '--path', join(t.dir, 'ajeno')]).ok, false);
  } finally {
    clean(t.dir);
    clean(`${t.dir}/repo.worktrees`);
  }
});

test('close: a worktree with a running plan is refused', { skip: !hasGit }, async () => {
  const t = setup();
  try {
    assert.equal(t.run(['new', '--name', 'r1', '--base', 'main']).ok, true);
    const r1 = join(t.wts, 'r1');
    writeOrchFile(r1, MARKER_FILE, { ts: Date.now() });
    assert.equal(t.run(['list']).worktrees.find((/** @type {any} */ w) => !w.main).running, true);
    const refused = t.run(['close', '--path', r1, '--force']);
    assert.equal(refused.ok, false);
    assert.match(refused.reason, /plan corriendo/);
    assert.ok(existsSync(r1));
  } finally {
    clean(t.dir);
    clean(`${t.dir}/repo.worktrees`);
  }
});

/** The 8.3 short form of an existing path on Windows, or the path itself. */
function shortPath(/** @type {string} */ p) {
  if (process.platform !== 'win32') return p;
  const r = spawnSync('cmd', ['/d', '/s', '/c', `"for %I in ("${p}") do @echo %~sI"`], { encoding: 'utf8', windowsVerbatimArguments: true });
  return r.status === 0 && r.stdout.trim() ? r.stdout.trim() : p;
}

test('8.3 paths: list and close match worktrees by real path', { skip: !hasGit }, async (tc) => {
  const t = setup();
  try {
    assert.equal(t.run(['new', '--name', 's1', '--base', 'main']).ok, true);
    const s1 = join(t.wts, 's1');
    const shortRepo = shortPath(t.repo);
    const shortWt = shortPath(s1);
    if (shortWt === s1 && shortRepo === t.repo) {
      tc.skip('no distinct 8.3 path here');
      return;
    }
    const l = t.run(['list'], shortRepo);
    assert.equal(l.ok, true);
    assert.equal(l.worktrees.find((/** @type {any} */ w) => w.main).current, true);
    const r = t.run(['close', '--path', shortWt], shortRepo);
    assert.equal(r.ok, true, JSON.stringify(r));
    assert.ok(!existsSync(s1));
  } finally {
    clean(t.dir);
    clean(`${t.dir}/repo.worktrees`);
  }
});

test('list: no remotes means no unpushed count', { skip: !hasGit }, async () => {
  const t = setup();
  try {
    assert.equal(t.run(['list']).worktrees[0].ahead, 0);
  } finally {
    clean(t.dir);
  }
});

test('list: a worktree whose status cannot be read reports dirty null, not clean', { skip: !hasGit }, async () => {
  const t = setup();
  try {
    assert.equal(t.run(['new', '--name', 'roto', '--base', 'main']).ok, true);
    const wt = join(t.wts, 'roto');
    rmSync(join(wt, '.git'), { force: true });
    writeFileSync(join(wt, '.git'), `gitdir: ${join(t.dir, 'no-existe')}\n`);
    const row = t.run(['list']).worktrees.find((/** @type {any} */ w) => w.path.endsWith('/roto'));
    assert.ok(row, 'the broken worktree is still listed');
    assert.equal(row.dirty, null);
  } finally {
    clean(t.dir);
    clean(`${t.dir}/repo.worktrees`);
  }
});

test('close: clean removes the folder and keeps the branch; dirty needs --force; main and session are refused', { skip: !hasGit }, async () => {
  const t = setup();
  try {
    assert.equal(t.run(['new', '--name', 'c1', '--base', 'main']).ok, true);
    assert.equal(t.run(['new', '--name', 'c2', '--base', 'main']).ok, true);
    const c1 = join(t.wts, 'c1');
    const c2 = join(t.wts, 'c2');
    const r1 = t.run(['close', '--path', c1]);
    assert.equal(r1.ok, true, JSON.stringify(r1));
    assert.ok(!existsSync(c1));
    assert.match(t.git('branch', '--list', 'feature/c1'), /feature\/c1/);
    assert.ok(t.run(['list']).branches.free.includes('feature/c1'));

    writeFileSync(join(c2, 'sucio.txt'), 'x\n');
    const refused = t.run(['close', '--path', c2]);
    assert.equal(refused.ok, false);
    assert.ok(existsSync(c2));
    assert.equal(t.run(['close', '--path', c2, '--force']).ok, true);
    assert.ok(!existsSync(c2));

    assert.equal(t.run(['close', '--path', t.repo]).ok, false);
    assert.equal(t.run(['new', '--name', 'c3', '--base', 'main']).ok, true);
    const c3 = join(t.wts, 'c3');
    assert.equal(t.run(['close', '--path', c3, '--force'], c3).ok, false);
    assert.ok(existsSync(c3));
    assert.equal(t.run(['close', '--path', join(t.dir, 'ajeno')]).ok, false);
  } finally {
    clean(t.dir);
    clean(`${t.dir}/repo.worktrees`);
  }
});
