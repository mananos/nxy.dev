import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { roleEffort, roleModel, frontmatterEffort } from '../hosts/claude-code/roles.mjs';

const HOOK = join(dirname(fileURLToPath(import.meta.url)), '..', 'hosts', 'claude-code', 'hooks', 'pretooluse-agent.mjs');

test('roleEffort: configured effort that differs from the frontmatter', () => {
  // reviewer's frontmatter is high
  assert.equal(roleEffort('nxy:reviewer', { roles: { reviewer: { effort: 'low' } } }), 'low');
});

test('roleEffort: equal to the frontmatter is still sent (Claude Code 2.1.300 launches with defaults)', () => {
  assert.equal(roleEffort('nxy:reviewer', { roles: { reviewer: { effort: 'high' } } }), 'high');
  assert.equal(roleEffort('nxy:reviewer', {}), 'high');
});

test('roleModel: the table, else the agent\'s own; a model the dispatch names wins', () => {
  assert.equal(roleModel('nxy:implementer', { roles: { implementer: { model: 'haiku' } } }), 'haiku');
  assert.equal(roleModel('nxy:implementer', {}), 'sonnet');
  assert.equal(roleModel('nxy:implementer', { roles: { implementer: { model: 'gpt-9' } } }), 'sonnet');
  assert.equal(roleModel('nxy:planner', {}, 'sonnet'), null);
  assert.equal(roleModel('Explore', {}), null);
});

test('haiku roles declare effort in the frontmatter', () => {
  assert.equal(frontmatterEffort('scout'), 'low');
  assert.equal(frontmatterEffort('tester'), 'low');
  assert.equal(frontmatterEffort('librarian'), 'medium');
  assert.equal(roleEffort('nxy:scout', { roles: { scout: { effort: 'low' } } }), 'low');
  assert.equal(roleEffort('nxy:scout', { roles: { scout: { effort: 'high' } } }), 'high');
  assert.equal(roleEffort('nxy:librarian', { roles: { librarian: { effort: 'medium' } } }), 'medium');
});

test('roleEffort: the dispatch own effort wins', () => {
  assert.equal(roleEffort('nxy:reviewer', { roles: { reviewer: { effort: 'low' } } }, 'medium'), null);
});

test('roleEffort: unknown role does not emit; an invalid value falls back to the agent\'s own', () => {
  assert.equal(roleEffort('nxy:nope', { roles: { nope: { effort: 'low' } } }), null);
  assert.equal(roleEffort('Explore', { roles: { Explore: { effort: 'low' } } }), null);
  assert.equal(roleEffort('nxy:reviewer', { roles: { reviewer: { effort: 'max' } } }), 'high');
  assert.equal(roleEffort('nxy:reviewer', { roles: { reviewer: { effort: /** @type {any} */ (3) } } }), 'high');
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
    assert.equal(out.updatedInput.model, 'haiku');
    assert.match(out.permissionDecisionReason, /roles\.scout\.model\/effort/);
    assert.equal(run({ subagent_type: 'nxy:scout', prompt: 'x', effort: 'low', model: 'sonnet' }).stdout, '', 'what the dispatch names wins');
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});
