// @ts-check
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, readdirSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';
import { ROOT } from './sandbox.mjs';

const SETUP = join(ROOT, 'hosts', 'claude-code', 'entries', 'setup.mjs');

function box() {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-setup-'));
  const repo = join(dir, 'repo');
  mkdirSync(repo, { recursive: true });
  const home = join(dir, 'home');
  mkdirSync(join(home, '.claude'), { recursive: true });
  /** @type {Record<string, string|undefined>} */
  const env = { ...process.env, NXY_HOME: join(home, '.nxy'), HOME: home, USERPROFILE: home };
  delete env.NXY_FILTER;
  delete env.NXY_ENGINE;
  return (/** @type {string[]} */ args) =>
    spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', SETUP, ...args, '--cwd', repo], { encoding: 'utf8', env });
}

test('run without --yes refuses and prints the command', () => {
  const r = box()(['run', 'rg']);
  assert.equal(r.status, 1);
  assert.match(r.stdout, /Falta --yes/);
});

test('run --yes --dry-run prints the steps and exits 0 without running anything', () => {
  const r = box()(['run', 'codegraph', '--yes', '--dry-run']);
  assert.equal(r.status, 0, r.stdout + r.stderr);
  assert.match(r.stdout, /dry-run/);
  assert.match(r.stdout, /install\.(sh|ps1)/);
  assert.doesNotMatch(r.stdout, /listo/);
});

test('run with an unknown tool fails', () => {
  assert.equal(box()(['run', 'nope', '--yes', '--dry-run']).status, 1);
});

test('status --json lists the three tools with a plan', () => {
  const r = box()(['status', '--json']);
  const j = JSON.parse(r.stdout.trim());
  assert.equal(j.ok, true);
  for (const t of ['rtk', 'rg', 'codegraph']) {
    assert.equal(typeof j.tools[t].found, 'boolean');
    assert.ok(['install', 'reinstall'].includes(j.tools[t].plan.action));
  }
});

test('status in text says nothing was installed', () => {
  const r = box()(['status']);
  assert.equal(r.status, 0);
  assert.match(r.stdout, /Nada se instaló/);
});

test('no hook calls setup', () => {
  const files = ['hooks/hooks.json', ...readdirSync(join(ROOT, 'hosts/claude-code/hooks')).map((f) => `hosts/claude-code/hooks/${f}`)];
  for (const f of files) {
    assert.doesNotMatch(readFileSync(join(ROOT, f), 'utf8'), /entries\/setup|setup\.mjs|setup-state|core\/setup|nxy:setup/i, f);
  }
});
