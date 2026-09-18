// @ts-check
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { loadPermissionRules, matchesAnyRule, originalVerdict, ruleToRegex } from '../scripts/lib/permissions.mjs';
import { detectRtkHook, knownRtkLocations, resolveEngine } from '../scripts/filter/engine.mjs';

test('ruleToRegex forms', () => {
  assert.equal(ruleToRegex('Read'), null);
  assert.ok(ruleToRegex('Bash')?.test('anything at all'));
  assert.ok(ruleToRegex('Bash(*)')?.test('anything'));
  assert.ok(ruleToRegex('Bash(git status)')?.test('git status'));
  assert.equal(ruleToRegex('Bash(git status)')?.test('git status -s'), false);
  assert.ok(ruleToRegex('Bash(git status:*)')?.test('git status'));
  assert.ok(ruleToRegex('Bash(git status:*)')?.test('git status --short'));
  assert.equal(ruleToRegex('Bash(git status:*)')?.test('git statusx'), false);
  assert.ok(ruleToRegex('Bash(npm * test)')?.test('npm run test'));
  assert.ok(ruleToRegex('Bash(mvn test:*)')?.test('mvn test -Dtest=Foo'));
  assert.equal(ruleToRegex('Bash(mvn test:*)')?.test('rtk mvn test'), false, 'the rewritten form does not match — that is why nxy inherits the verdict');
});

test('matchesAnyRule / originalVerdict precedence', () => {
  const rules = { allow: ['Bash(git status:*)', 'Bash(npm test)'], deny: ['Bash(rm -rf:*)', 'Bash(git push:*)'] };
  assert.equal(matchesAnyRule('git status', rules.allow), true);
  assert.equal(originalVerdict('git status --short', rules), 'allow');
  assert.equal(originalVerdict('rm -rf /', rules), 'deny');
  assert.equal(originalVerdict('ls', rules), 'none');
  assert.equal(originalVerdict('npm test', rules), 'allow');
});

test('loadPermissionRules merges files and ignores broken ones', () => {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-perm-'));
  const a = join(dir, 'a.json');
  const b = join(dir, 'b.json');
  writeFileSync(a, JSON.stringify({ permissions: { allow: ['Bash(ls)'], deny: ['Bash(rm:*)'] } }));
  writeFileSync(b, '{broken');
  const rules = loadPermissionRules([a, b, join(dir, 'missing.json')]);
  assert.deepEqual(rules, { allow: ['Bash(ls)'], deny: ['Bash(rm:*)'] });
});

test('detectRtkHook finds rtk hook commands', () => {
  const withHook = { hooks: { PreToolUse: [{ matcher: 'Bash', hooks: [{ type: 'command', command: 'rtk hook claude' }] }] } };
  const withExe = { hooks: { PreToolUse: [{ hooks: [{ type: 'command', command: 'C:\\bin\\rtk.exe hook claude' }] }] } };
  const ours = { hooks: { PreToolUse: [{ hooks: [{ type: 'command', command: 'node "${CLAUDE_PLUGIN_ROOT}/scripts/hooks/pretooluse-bash.mjs"' }] }] } };
  assert.equal(detectRtkHook([withHook]), true);
  assert.equal(detectRtkHook([withExe]), true);
  assert.equal(detectRtkHook([ours, null, {}]), false);
});

test('resolveEngine honors config off and reports missing rtk', () => {
  const cwd = mkdtempSync(join(tmpdir(), 'nxy-eng-'));
  const off = resolveEngine({ filter: { engine: 'off' } }, cwd, { NXY_RTK_PATH: 'definitely-not-a-binary' });
  assert.equal(off.engine, 'off');
  assert.equal(off.reason, 'config');
  // Hermetic: an empty PATH must hide a really-installed rtk (spawn receives this env, not process.env).
  const auto = resolveEngine({ filter: { engine: 'auto' } }, cwd, { PATH: '', Path: '', HOME: cwd, USERPROFILE: cwd, LOCALAPPDATA: cwd });
  assert.equal(auto.engine, 'off');
  assert.equal(auto.reason, 'auto-no-rtk');
  assert.equal(auto.rtkPath, null);
});

test('knownRtkLocations derives from env, never throws', () => {
  const locs = knownRtkLocations({ HOME: '/home/dev', USERPROFILE: 'C:/Users/dev', LOCALAPPDATA: 'C:/Users/dev/AppData/Local' });
  assert.ok(locs.length > 0);
  assert.ok(locs.every((l) => /rtk(.exe)?$/.test(l)));
});

test('readEngineCache: negative verdicts expire fast, positive ones last', async () => {
  process.env.NXY_HOME = mkdtempSync(join(tmpdir(), 'nxy-home-')); // never touch the real ~/.nxy from tests
  const { readEngineCache, writeEngineCache } = await import('../scripts/filter/engine.mjs');
  const base = { rtkPath: null, rtkVersion: null, rtkHookDetected: false, rgAvailable: false };
  writeEngineCache({ ...base, engine: 'off', reason: 'auto-no-rtk', resolvedAt: Date.now() - 5 * 60 * 1000 });
  assert.equal(readEngineCache(), null, 'stale "off" is not trusted after 2 minutes');
  writeEngineCache({ ...base, engine: 'off', reason: 'auto-no-rtk', resolvedAt: Date.now() - 30 * 1000 });
  assert.equal(readEngineCache()?.engine, 'off', 'fresh "off" is trusted');
  writeEngineCache({ ...base, rtkPath: 'rtk', rtkVersion: '0.48.0', engine: 'rtk', reason: 'rtk-found', resolvedAt: Date.now() - 60 * 60 * 1000 });
  assert.equal(readEngineCache()?.engine, 'rtk', '"rtk" verdict lasts hours');
});
