import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { roleEffort, frontmatterEffort } from '../hosts/claude-code/roles.mjs';

const HOOK = join(dirname(fileURLToPath(import.meta.url)), '..', 'hosts', 'claude-code', 'hooks', 'pretooluse-agent.mjs');

test('roleEffort: configured effort that differs from the frontmatter', () => {
  // reviewer's frontmatter is high
  assert.equal(roleEffort('nxy:reviewer', { roles: { reviewer: { effort: 'low' } } }), 'low');
});

test('roleEffort: equal to the frontmatter does not emit', () => {
  assert.equal(roleEffort('nxy:reviewer', { roles: { reviewer: { effort: 'high' } } }), null);
});

test('haiku roles declare effort in the frontmatter', () => {
  assert.equal(frontmatterEffort('scout'), 'low');
  assert.equal(frontmatterEffort('tester'), 'low');
  assert.equal(frontmatterEffort('librarian'), 'medium');
  assert.equal(roleEffort('nxy:scout', { roles: { scout: { effort: 'low' } } }), null);
  assert.equal(roleEffort('nxy:scout', { roles: { scout: { effort: 'high' } } }), 'high');
  assert.equal(roleEffort('nxy:librarian', { roles: { librarian: { effort: 'medium' } } }), null);
});

test('roleEffort: the dispatch own effort wins', () => {
  assert.equal(roleEffort('nxy:reviewer', { roles: { reviewer: { effort: 'low' } } }, 'medium'), null);
});

test('roleEffort: unknown role or invalid value does not emit', () => {
  assert.equal(roleEffort('nxy:nope', { roles: { nope: { effort: 'low' } } }), null);
  assert.equal(roleEffort('Explore', { roles: { Explore: { effort: 'low' } } }), null);
  assert.equal(roleEffort('nxy:reviewer', { roles: { reviewer: { effort: 'max' } } }), null);
  assert.equal(roleEffort('nxy:reviewer', { roles: { reviewer: { effort: /** @type {any} */ (3) } } }), null);
  assert.equal(roleEffort('nxy:reviewer', {}), null);
});

test('hook: configured effort arrives in updatedInput', () => {
  const root = mkdtempSync(join(tmpdir(), 'nxy-roles-'));
  try {
    const home = join(root, 'home');
    const repo = join(root, 'repo');
    mkdirSync(home, { recursive: true });
    mkdirSync(join(repo, '.git'), { recursive: true });
    writeFileSync(join(repo, '.git', 'HEAD'), 'ref: refs/heads/main\n');
    mkdirSync(join(repo, '.nxy'), { recursive: true });
    writeFileSync(join(repo, '.nxy', 'config.json'), JSON.stringify({ roles: { scout: { effort: 'high' } } }));
    const env = { ...process.env, HOME: home, USERPROFILE: home, NXY_HOME: join(home, '.nxy'), CLAUDE_PROJECT_DIR: repo };
    const run = (toolInput) => spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', HOOK], {
      input: JSON.stringify({ tool_name: 'Agent', tool_input: toolInput, cwd: repo }), encoding: 'utf8', env, cwd: repo,
    });
    const out = JSON.parse(run({ subagent_type: 'nxy:scout', prompt: 'find x' }).stdout).hookSpecificOutput;
    assert.equal(out.updatedInput.effort, 'high');
    assert.equal(out.updatedInput.prompt, 'find x');
    assert.match(out.permissionDecisionReason, /roles\.scout\.effort/);
    assert.equal(run({ subagent_type: 'nxy:scout', prompt: 'x', effort: 'low' }).stdout, '');
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});
