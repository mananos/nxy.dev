// @ts-check
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  parseWorktrees, slugOf, parseIssue, featureNames, defaultBranchOf, baseChoices, addArgv, removeArgv,
  commandText, shellOf, openCommand, branchChoices, rowView, newQuestion, closeQuestion,
} from '../core/featuresview.mjs';

test('parseWorktrees: Windows porcelain', () => {
  const out = parseWorktrees([
    'worktree C:/work/app', 'HEAD aaa', 'branch refs/heads/main', '',
    'worktree C:\\work\\app.worktrees\\algo', 'HEAD bbb', 'branch refs/heads/feature/algo', 'locked reason', '',
    'worktree C:/work/app.worktrees/viejo', 'HEAD ccc', 'detached', '',
  ].join('\r\n'));
  assert.equal(out.length, 3);
  assert.deepEqual([out[0].main, out[0].branch, out[0].path], [true, 'main', 'C:/work/app']);
  assert.equal(out[1].path, 'C:/work/app.worktrees/algo');
  assert.equal(out[1].branch, 'feature/algo');
  assert.equal(out[1].locked, true);
  assert.deepEqual([out[2].branch, out[2].detached, out[2].main], [null, true, false]);
});

test('parseWorktrees: Linux porcelain', () => {
  const out = parseWorktrees('worktree /srv/app\nHEAD aaa\nbranch refs/heads/dev\n\nworktree /srv/app.worktrees/x\nHEAD bbb\ndetached\n');
  assert.equal(out.length, 2);
  assert.equal(out[0].branch, 'dev');
  assert.equal(out[1].branch, null);
  assert.deepEqual(parseWorktrees(''), []);
});

test('parseIssue', () => {
  assert.deepEqual(parseIssue('14'), { ok: true, issue: '14', error: '' });
  assert.equal(parseIssue('#14').issue, '14');
  assert.deepEqual(parseIssue(''), { ok: true, issue: '', error: '' });
  assert.equal(parseIssue('abc').ok, false);
  assert.equal(parseIssue('1 4').ok, false);
  assert.equal(parseIssue('#').ok, false);
  assert.equal(parseIssue('12345678').ok, false);
});

test('slugOf', () => {
  assert.equal(slugOf('Login rápido!'), 'login_rapido');
  assert.equal(slugOf('  --a//b--  '), 'a_b');
  assert.equal(slugOf('x'.repeat(80)).length, 50);
});

test('featureNames: rama nueva', () => {
  const a = featureNames({ issue: '14', name: 'login rápido', base: 'main' }, 'C:/work/app');
  assert.deepEqual([a.ok, a.create, a.slug, a.branch, a.base], [true, true, '14_login_rapido', 'feature/#14_login_rapido', 'main']);
  assert.equal(a.path, 'C:/work/app.worktrees/14_login_rapido');
  assert.equal(a.path, featureNames({ branch: a.branch }, 'C:/work/app').path);
  const b = featureNames({ issue: '', name: '14_algo', base: 'dev' }, '/srv/app');
  assert.equal(b.branch, 'feature/14_algo');
  assert.equal(b.path, '/srv/app.worktrees/14_algo');
  assert.equal(featureNames({ issue: '', name: '', base: 'main' }, '/srv/app').ok, false);
  assert.equal(featureNames({ issue: '', name: '..', base: 'main' }, '/srv/app').ok, false);
  const noBase = featureNames({ issue: '', name: 'algo', base: '' }, '/srv/app');
  assert.equal(noBase.ok, false);
  assert.match(noBase.error, /rama base/);
});

test('featureNames: rama existente', () => {
  const a = featureNames({ branch: 'feature/#14_algo' }, '/srv/app');
  assert.deepEqual([a.ok, a.create, a.base, a.branch], [true, false, null, 'feature/#14_algo']);
  assert.equal(a.path, '/srv/app.worktrees/14_algo');
  assert.equal(featureNames({ branch: 'fix/login' }, '/srv/app').path, '/srv/app.worktrees/fix_login');
});

test('defaultBranchOf', () => {
  assert.equal(defaultBranchOf({ originHead: 'origin/main', mainBranch: 'dev', branches: ['dev', 'main'] }), 'main');
  assert.equal(defaultBranchOf({ originHead: 'origin/main', mainBranch: 'dev', branches: ['dev'] }), 'dev');
  assert.equal(defaultBranchOf({ originHead: null, mainBranch: null, branches: ['x', 'main', 'master'] }), 'main');
  assert.equal(defaultBranchOf({ originHead: null, mainBranch: null, branches: ['x', 'master'] }), 'master');
  assert.equal(defaultBranchOf({ originHead: null, mainBranch: null, branches: ['x'] }), null);
  assert.equal(defaultBranchOf({ originHead: 'upstream/trunk', mainBranch: null, branches: ['trunk', 'main'] }), 'trunk');
});

test('baseChoices', () => {
  assert.deepEqual(baseChoices(['a', 'main', 'b'], 'main'), ['main', 'a', 'b']);
  assert.deepEqual(baseChoices(['a', 'b'], null), ['a', 'b']);
});

test('addArgv / removeArgv / commandText', () => {
  const argv = addArgv('feature/#14_algo', "/tmp/my $dir/o'k", true, 'main');
  assert.deepEqual(argv.slice(0, 5), ['git', 'worktree', 'add', '-b', 'feature/#14_algo']);
  assert.equal(argv[6], 'main');
  assert.equal(commandText(argv), "git worktree add -b 'feature/#14_algo' '/tmp/my $dir/o'\\''k' main");
  assert.equal(commandText(argv, 'powershell'), "git worktree add -b 'feature/#14_algo' '/tmp/my $dir/o''k' main");
  assert.deepEqual(addArgv('fix/login', '/srv/a', false), ['git', 'worktree', 'add', '/srv/a', 'fix/login']);
  assert.throws(() => addArgv('x', '/srv/a', true));
  assert.deepEqual(removeArgv('/srv/a', true), ['git', 'worktree', 'remove', '--force', '/srv/a']);
  assert.deepEqual(removeArgv('/srv/a', false), ['git', 'worktree', 'remove', '/srv/a']);
});

test('branchChoices', () => {
  const r = branchChoices(['main', 'a', 'b'], [{ path: '/srv/app', branch: 'main' }, { path: '/srv/app.worktrees/b', branch: 'b' }, { path: '/srv/x', branch: null }]);
  assert.deepEqual(r.free, ['a']);
  assert.deepEqual(r.busy, [{ name: 'main', path: '/srv/app' }, { name: 'b', path: '/srv/app.worktrees/b' }]);
});

test('shellOf / openCommand', () => {
  assert.equal(shellOf({ platform: 'win32', env: {} }), 'powershell');
  assert.equal(shellOf({ platform: 'win32', env: { SHELL: '/bin/bash' } }), 'posix');
  assert.equal(shellOf({ platform: 'linux', env: {} }), 'posix');
  assert.equal(openCommand("C:/my app/o'k", 'powershell'), "cd 'C:/my app/o''k'; claude");
  assert.equal(openCommand("/srv/my app/o'k", 'posix'), "cd '/srv/my app/o'\\''k' && claude");
});

test('rowView', () => {
  const now = 10 * 3600 * 1000;
  const r = rowView({
    path: '/srv/app.worktrees/algo', branch: 'feature/algo', current: true, dirty: 3, ahead: 2, batchDone: 2, batchTotal: 3,
    reviewDone: true, handoffAt: now - 2 * 3600 * 1000, pausedAt: now - 5 * 60000, running: true, missingDeps: true,
  }, { now });
  assert.equal(r.name, 'algo');
  assert.deepEqual(r.tags, ['esta sesión', 'pausado hace 5 min', 'plan corriendo']);
  assert.ok(r.lines.includes('3 archivos sin commitear'));
  assert.ok(r.lines.includes('lote 2/3 · review hecha'));
  assert.ok(r.lines.includes('handoff hace 2 h'));
  assert.ok(r.lines.includes('2 commits sin pushear'));
  assert.ok(r.lines.some((l) => l.includes('falta node_modules')));
  const m = rowView({ path: '/srv/app', branch: 'main', main: true }, { now });
  assert.deepEqual(m.tags, ['principal']);
  assert.deepEqual(m.lines, ['limpio', 'sin handoff']);
});

test('newQuestion', () => {
  const n = featureNames({ issue: '14', name: 'algo', base: 'dev' }, '/srv/app');
  const q = newQuestion(n, 'dev');
  assert.match(q, /git worktree add -b 'feature\/#14_algo' \/srv\/app\.worktrees\/14_algo dev/);
  assert.match(q, /sale de la rama local dev, tal como está en este equipo: no se baja nada del remoto/);
  assert.match(q, /terminal con Claude/);
  const e = newQuestion(featureNames({ branch: 'fix/login' }, '/srv/app'), null);
  assert.match(e, /git worktree add \/srv\/app\.worktrees\/fix_login fix\/login/);
  assert.doesNotMatch(e, / -b /);
  assert.match(e, /no crea ninguna rama/);
});

test('newQuestion / closeQuestion use the same commandText as the shell', () => {
  const n = featureNames({ issue: '14', name: 'algo', base: 'dev' }, "/srv/o'k");
  for (const shell of /** @type {('posix' | 'powershell')[]} */ (['posix', 'powershell'])) {
    assert.ok(newQuestion(n, 'dev', shell).startsWith(commandText(addArgv(n.branch, n.path, n.create, 'dev'), shell)));
    assert.ok(closeQuestion({ path: n.path, branch: null, dirty: 0 }, shell).startsWith(commandText(removeArgv(n.path, false), shell)));
  }
  assert.notEqual(newQuestion(n, 'dev', 'posix'), newQuestion(n, 'dev', 'powershell'));
  assert.equal(newQuestion(n, 'dev'), newQuestion(n, 'dev', 'posix'));
});

test('closeQuestion', () => {
  const clean = closeQuestion({ path: '/srv/a', branch: 'x', dirty: 0 });
  assert.doesNotMatch(clean, /SE PIERDEN/);
  assert.match(clean, /terminal con Claude/);
  const dirty = closeQuestion({ path: '/srv/a', branch: 'x', dirty: 2 });
  assert.match(dirty, /SE PIERDEN/);
  assert.match(dirty, /La rama no se borra/);
  const unknown = closeQuestion({ path: '/srv/a', branch: 'x', dirty: null });
  assert.match(unknown, /SE PIERDEN/);
  assert.match(unknown, /--force/);
  assert.doesNotMatch(unknown, /Está limpio/);
});

test('rowView: unknown status is not shown as clean', () => {
  const r = rowView({ path: '/srv/app.worktrees/a', branch: 'a', dirty: null }, { now: 0 });
  assert.equal(r.lines[0], 'no se pudo leer el estado');
  assert.ok(!r.lines.includes('limpio'));
});
