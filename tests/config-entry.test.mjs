// @ts-check
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';
import { ROOT } from './sandbox.mjs';
import { claudeMissing } from '../hosts/claude-code/update-check.mjs';

test('claudeMissing: only ENOENT, 127 or the win32 shell message with empty stdout', () => {
  assert.equal(claudeMissing({ error: { code: 'ENOENT' } }, 'linux'), true);
  assert.equal(claudeMissing({ status: 127, stderr: '' }, 'linux'), true);
  assert.equal(claudeMissing({ status: 1, stdout: '', stderr: "'claude' is not recognized as an internal command" }, 'win32'), true);
  assert.equal(claudeMissing({ status: 1, stdout: '', stderr: "'claude' is not recognized" }, 'linux'), false);
  assert.equal(claudeMissing({ status: 1, stdout: 'x', stderr: 'not recognized' }, 'win32'), false);
  assert.equal(claudeMissing({ status: 1, stdout: '', stderr: 'Plugin nxy@nxy-dev not found' }, 'win32'), false);
  assert.equal(claudeMissing({ status: 1, stdout: '', stderr: 'Plugin not found' }, 'linux'), false);
});

const CONFIG = join(ROOT, 'hosts', 'claude-code', 'entries', 'config.mjs');

function box() {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-cfg-'));
  const repo = join(dir, 'repo');
  mkdirSync(join(repo, '.git'), { recursive: true });
  writeFileSync(join(repo, '.git', 'HEAD'), 'ref: refs/heads/main\n');
  const home = join(dir, 'home');
  mkdirSync(join(home, '.claude'), { recursive: true });
  /** @type {Record<string, string|undefined>} */
  const env = { ...process.env, NXY_HOME: join(home, '.nxy'), HOME: home, USERPROFILE: home };
  delete env.NXY_FILTER;
  delete env.NXY_ENGINE;
  const run = (args) => spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', CONFIG, ...args, '--cwd', repo], { encoding: 'utf8', env });
  const set = (scope, key, value) => {
    const r = run(['set', '--scope', scope, '--key', key, '--json', JSON.stringify(value)]);
    return { status: r.status, json: JSON.parse(r.stdout.trim().split('\n').pop() ?? '') };
  };
  return {
    dir, repo, home, run, set,
    userCfg: join(home, '.nxy', 'config.json'),
    repoCfg: join(repo, '.nxy', 'config.json'),
    settings: join(home, '.claude', 'settings.json'),
  };
}

test('set writes only that key in the user file and leaves no temp files', () => {
  const b = box();
  const r = b.set('user', 'roles.implementer.model', 'opus');
  assert.equal(r.status, 0);
  assert.equal(r.json.ok, true);
  assert.deepEqual(JSON.parse(readFileSync(b.userCfg, 'utf8')), { roles: { implementer: { model: 'opus' } } });
  assert.equal(r.json.config.roles.implementer.model, 'opus');
  assert.equal(r.json.bytes, readFileSync(b.userCfg).length);
  assert.deepEqual(readdirSync(join(b.home, '.nxy')), ['config.json']);
  assert.equal(existsSync(b.repoCfg), false);
});

test('null deletes the key and prunes empty objects; other keys survive', () => {
  const b = box();
  b.set('user', 'ui.panel', 'off');
  b.set('user', 'roles.tester.effort', 'low');
  const r = b.set('user', 'roles.tester.effort', null);
  assert.equal(r.json.ok, true);
  assert.deepEqual(JSON.parse(readFileSync(b.userCfg, 'utf8')), { ui: { panel: 'off' } });
});

test('a broken file is left byte for byte and the exit is 1', () => {
  const b = box();
  mkdirSync(join(b.home, '.nxy'), { recursive: true });
  writeFileSync(b.userCfg, '{ not json');
  const r = b.set('user', 'ui.panel', 'off');
  assert.equal(r.status, 1);
  assert.equal(r.json.ok, false);
  assert.equal(readFileSync(b.userCfg, 'utf8'), '{ not json');
  assert.deepEqual(readdirSync(join(b.home, '.nxy')), ['config.json']);
});

test('invalid keys and values are refused without writing', () => {
  const b = box();
  assert.equal(b.set('user', 'nope.nope', 1).json.ok, false);
  assert.equal(b.set('user', 'roles.planner.model', 'gpt').json.ok, false);
  assert.equal(b.set('user', 'roles.planner.effort', 'max').status, 1);
  assert.equal(existsSync(b.userCfg), false);
});

test('NXY_FILTER marks modules.filter as env in the config view', () => {
  const b = box();
  b.set('repo', 'modules.filter', true);
  const prev = process.env.NXY_FILTER;
  process.env.NXY_FILTER = '0';
  try {
    const r = spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', CONFIG, 'set', '--scope', 'user', '--key', 'ui.panel', '--json', '"off"', '--cwd', b.repo],
      { encoding: 'utf8', env: { ...process.env, NXY_HOME: join(b.home, '.nxy'), HOME: b.home, USERPROFILE: b.home } });
    const json = JSON.parse(r.stdout.trim().split('\n').pop() ?? '');
    assert.equal(json.config.sources['modules.filter'], 'env');
    assert.equal(json.config.sources['ui.panel'], 'user');
  } finally {
    if (prev === undefined) delete process.env.NXY_FILTER; else process.env.NXY_FILTER = prev;
  }
});

test('repo scope writes .nxy/config.json and sources say the repo overrides the user', () => {
  const b = box();
  b.set('user', 'gate.contextTokens', 50000);
  const r = b.set('repo', 'gate.contextTokens', 200000);
  assert.equal(r.json.ok, true);
  assert.deepEqual(JSON.parse(readFileSync(b.repoCfg, 'utf8')), { gate: { contextTokens: 200000 } });
  assert.equal(r.json.config.gate.contextTokens, 200000);
  assert.equal(r.json.config.sources['gate.contextTokens'], 'repo');
  assert.equal(r.json.config.sources['ui.panel'], 'default');
  assert.equal(r.json.config.scopes.repo.exists, true);
  assert.equal(r.json.config.scopes.user.broken, false);
  const u = b.set('user', 'ui.panel', 'off');
  assert.equal(u.json.config.sources['ui.panel'], 'user');
});

test('cache-ttl backs up with its own prefix, keeps 3 and never touches foreign backups', () => {
  const b = box();
  writeFileSync(b.settings, JSON.stringify({ theme: 'dark' }));
  const foreign = `${b.settings}.bak-123`;
  writeFileSync(foreign, 'x');
  for (let i = 0; i < 6; i++) {
    const r = b.run(['cache-ttl', i % 2 === 0 ? '1h' : 'off']);
    assert.equal(r.status, 0, r.stdout);
  }
  const names = readdirSync(join(b.home, '.claude'));
  assert.equal(names.filter((n) => n.startsWith('settings.json.bak-nxy-')).length, 3);
  assert.equal(existsSync(foreign), true);
  assert.deepEqual(JSON.parse(readFileSync(b.settings, 'utf8')), { theme: 'dark' });
  b.run(['cache-ttl', '1h']);
  assert.equal(JSON.parse(readFileSync(b.settings, 'utf8')).promptCacheTtl, '1h');
});

test('cache-ttl aborts on a broken settings.json', () => {
  const b = box();
  writeFileSync(b.settings, '{ broken');
  const r = b.run(['cache-ttl', '1h']);
  assert.equal(r.status, 1);
  assert.equal(readFileSync(b.settings, 'utf8'), '{ broken');
  assert.deepEqual(readdirSync(join(b.home, '.claude')), ['settings.json']);
});

test('statusline remove refuses a foreign statusline and removes the nxy one', () => {
  const b = box();
  writeFileSync(b.settings, JSON.stringify({ statusLine: { type: 'command', command: 'echo hi' } }));
  const r = b.run(['statusline', 'remove']);
  assert.equal(r.status, 1);
  assert.equal(JSON.parse(readFileSync(b.settings, 'utf8')).statusLine.command, 'echo hi');
  const i = b.run(['statusline', 'install']);
  assert.equal(i.status, 0, i.stdout + i.stderr);
  assert.match(JSON.parse(readFileSync(b.settings, 'utf8')).statusLine.command, /statusline\.mjs/);
  assert.equal(existsSync(join(b.home, '.nxy', 'statusline.mjs')), true);
  const rm = b.run(['statusline', 'remove']);
  assert.equal(rm.status, 0, rm.stdout);
  assert.equal('statusLine' in JSON.parse(readFileSync(b.settings, 'utf8')), false);
});

test('tools reports the version and the three tools', () => {
  const b = box();
  const r = b.run(['tools']);
  const j = JSON.parse(r.stdout.trim());
  assert.equal(j.ok, true);
  assert.equal(j.version, JSON.parse(readFileSync(join(ROOT, '.claude-plugin', 'plugin.json'), 'utf8')).version);
  for (const t of ['rtk', 'rg', 'codegraph']) assert.equal(typeof j[t].found, 'boolean');
});
