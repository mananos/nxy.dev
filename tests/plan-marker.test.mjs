// Batch 3: one approval marker (and dirty-at-approval, extra repos) per task key.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { nxyRuntimeDir } from '../core/paths.mjs';
import { readMarker, taskDir, writeMarker } from '../hosts/claude-code/plan-approval.mjs';
import { clearDirtyAtApproval, readDirtyAtApproval } from '../hosts/claude-code/gitstate.mjs';
import { noteUndeclaredRepo, undeclaredRepos } from '../hosts/claude-code/repos.mjs';

const proj = () => mkdtempSync(join(tmpdir(), 'nxy-marker-'));
const put = (p, v) => { mkdirSync(join(p, '..'), { recursive: true }); writeFileSync(p, JSON.stringify(v), 'utf8'); };

test('dot-only and traversal keys never resolve outside the tasks dir', () => {
  const cwd = proj();
  const root = join(nxyRuntimeDir(cwd), 'tasks');
  writeMarker(cwd, 'keep', 'h', 0, [cwd]);
  for (const k of ['.', '..', '', '../x']) {
    const d = taskDir(cwd, k);
    assert.equal(join(d, '..'), root, `key ${JSON.stringify(k)} stays one level under tasks/`);
    writeMarker(cwd, k, 'h', 0, [cwd]);
    writeMarker(cwd, k, null);
  }
  assert.equal(readMarker(cwd, 'keep')?.hash, 'h', 'removing a hostile key leaves other tasks alone');
  assert.ok(existsSync(root));
});

test('markers, dirty and extra repos of two keys are independent', () => {
  const cwd = proj();
  const R = join(cwd, 'repoX');
  writeMarker(cwd, 'a', 'hash-a', 0, [cwd]);
  writeMarker(cwd, 'b', 'hash-b', 2, [cwd, R]);
  assert.equal(readMarker(cwd, 'a')?.hash, 'hash-a');
  assert.equal(readMarker(cwd, 'b')?.questions, 2);
  assert.equal(readMarker(cwd, 'nope'), null, 'no file for an unknown key');

  put(join(taskDir(cwd, 'a'), 'plan-dirty.json'), { branch: 'a', files: ['x'], others: [] });
  put(join(taskDir(cwd, 'b'), 'plan-dirty.json'), { branch: 'b', files: ['y'], others: [] });
  assert.equal(noteUndeclaredRepo(cwd, 'a', R), true);
  assert.deepEqual(undeclaredRepos(cwd, 'b'), []);

  writeMarker(cwd, 'a', null);
  clearDirtyAtApproval(cwd, 'a');
  assert.equal(readMarker(cwd, 'a'), null);
  assert.equal(readDirtyAtApproval(cwd, 'a'), null);
  assert.deepEqual(undeclaredRepos(cwd, 'a'), []);
  assert.equal(readMarker(cwd, 'b')?.hash, 'hash-b', 'clearing A leaves B intact');
  assert.deepEqual(readDirtyAtApproval(cwd, 'b')?.files, ['y']);
});

test('a legacy plan.json saved with branch null belongs to the key (no branch)', () => {
  const cwd = proj();
  const rt = nxyRuntimeDir(cwd);
  put(join(rt, 'plan.json'), { branch: null, hash: 'hn', questions: 0, repos: [cwd] });
  assert.equal(readMarker(cwd, '(no branch)')?.hash, 'hn');
  assert.equal(readMarker(cwd, 'other'), null);
  writeMarker(cwd, '(no branch)', 'hn2');
  assert.ok(!existsSync(join(rt, 'plan.json')), 'legacy file migrated');
  assert.equal(readMarker(cwd, '(no branch)')?.hash, 'hn2');
});

test('a legacy single plan.json is read for its own branch only and migrated on the next write', () => {
  const cwd = proj();
  const rt = nxyRuntimeDir(cwd);
  const R = join(cwd, 'repoX');
  put(join(rt, 'plan.json'), { branch: 'old', hash: 'h1', questions: 0, repos: [cwd] });
  put(join(rt, 'plan-dirty.json'), { branch: 'old', files: ['f'], others: [] });
  put(join(rt, 'plan-extra-repos.json'), { roots: [R] });

  assert.equal(readMarker(cwd, 'old')?.hash, 'h1');
  assert.equal(readMarker(cwd, 'other'), null);
  assert.deepEqual(readDirtyAtApproval(cwd, 'old')?.files, ['f']);
  assert.equal(readDirtyAtApproval(cwd, 'other'), null);
  assert.deepEqual(undeclaredRepos(cwd, 'old'), [R]);
  assert.deepEqual(undeclaredRepos(cwd, 'other'), []);

  writeMarker(cwd, 'other', 'h2');
  assert.ok(existsSync(join(rt, 'plan.json')), 'another key does not touch the legacy files');

  writeMarker(cwd, 'old', 'h3');
  for (const n of ['plan.json', 'plan-dirty.json', 'plan-extra-repos.json']) assert.ok(!existsSync(join(rt, n)), n);
  assert.equal(readMarker(cwd, 'old')?.hash, 'h3');
  assert.deepEqual(readDirtyAtApproval(cwd, 'old')?.files, ['f'], 'dirty moved into the task dir');
  assert.deepEqual(undeclaredRepos(cwd, 'old'), [R]);
  assert.equal(JSON.parse(readFileSync(join(taskDir(cwd, 'old'), 'plan.json'), 'utf8')).hash, 'h3');
});
