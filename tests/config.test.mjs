// @ts-check
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { deepMerge, loadConfig } from '../core/config.mjs';
import { toNativePath } from '../core/paths.mjs';
import { projectSlug } from '../hosts/claude-code/paths.mjs';
import { appendJsonl, readJsonl } from '../core/jsonl.mjs';

test('deepMerge recurses objects and replaces arrays/scalars', () => {
  const out = deepMerge({ a: { b: 1, c: [1] }, d: 1 }, { a: { c: [2] }, d: 2 });
  assert.deepEqual(out, { a: { b: 1, c: [2] }, d: 2 });
});

test('loadConfig: defaults, project override and env precedence', () => {
  const cwd = mkdtempSync(join(tmpdir(), 'nxy-cfg-'));
  const base = loadConfig(cwd, {});
  assert.equal(base.modules.filter, true);
  assert.equal(base.filter.engine, 'auto');

  mkdirSync(join(cwd, '.nxy'));
  writeFileSync(join(cwd, '.nxy', 'config.json'), JSON.stringify({ modules: { filter: false } }));
  assert.equal(loadConfig(cwd, {}).modules.filter, false);
  assert.equal(loadConfig(cwd, { NXY_FILTER: '1' }).modules.filter, true, 'env wins over file');
  assert.equal(loadConfig(cwd, { NXY_ENGINE: 'off' }).filter.engine, 'off');

  writeFileSync(join(cwd, '.nxy', 'config.json'), '{not json');
  assert.equal(loadConfig(cwd, {}).modules.filter, true, 'broken override is ignored');
});

test('projectSlug matches Claude Code folder naming', () => {
  assert.equal(projectSlug('C:\\Users\\dev\\code\\my.app'), 'C--Users-dev-code-my-app');
  assert.equal(projectSlug('/home/dev/code/my.app'), '-home-dev-code-my-app');
});

test('toNativePath converts MSYS drive paths only on win32', () => {
  if (process.platform === 'win32') {
    assert.equal(toNativePath('/c/Users/dev/proj'), 'C:/Users/dev/proj');
    assert.equal(toNativePath('/d'), 'D:/');
  } else {
    assert.equal(toNativePath('/c/Users/dev/proj'), '/c/Users/dev/proj');
  }
  assert.equal(toNativePath('C:/Users/dev'), 'C:/Users/dev');
  assert.equal(toNativePath('/home/dev/proj'), '/home/dev/proj');
  assert.equal(toNativePath('/tmp/x'), '/tmp/x');
});

test('jsonl append/read skips malformed lines', () => {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-jsonl-'));
  const p = join(dir, 'sub', 'rows.jsonl');
  appendJsonl(p, { a: 1 });
  appendJsonl(p, { a: 2 });
  writeFileSync(p, 'garbage\n', { flag: 'a' });
  assert.deepEqual(readJsonl(p), [{ a: 1 }, { a: 2 }]);
  assert.deepEqual(readJsonl(p, (r) => r.a === 2), [{ a: 2 }]);
  assert.deepEqual(readJsonl(join(dir, 'missing.jsonl')), []);
});
