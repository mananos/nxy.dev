// @ts-check
/**
 * Review coverage B (git status at approval, read-only): files the plan changed without a baseline
 * copy (a script, `rm`, `xargs`...) are found through git and marked `(git)`; files already dirty
 * at approval with no copy are named as not reviewed; nothing is ever written into .git.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { planHash } from '../core/plan.mjs';
import { recordSuite } from '../hosts/claude-code/verify-state.mjs';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const HOOKS = join(ROOT, 'hosts', 'claude-code', 'hooks');
const MEM = join(ROOT, 'hosts', 'claude-code', 'entries', 'mem.mjs');
const REVIEW = join(ROOT, 'hosts', 'claude-code', 'entries', 'review.mjs');

const hasGit = spawnSync('git', ['--version'], { encoding: 'utf8' }).status === 0;

const PLAN = [
  '## Plan', 'Goal: x',
  '### Batch 1 — api: files', '- `b.js:2` — change b', 'Accept: `./mvnw -q test`',
].join('\n');
const lines = (tag) => ['line1', `line2 ${tag}`, 'line3', ''].join('\n');

function setup() {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-review-git-'));
  const repo = join(dir, 'repo');
  mkdirSync(repo, { recursive: true });
  const git = (...args) => execFileSync('git', args, { cwd: repo, encoding: 'utf8' });
  git('init', '-q');
  git('config', 'user.name', 'nxy test');
  git('config', 'user.email', 'nxy@example.com');
  git('config', 'core.autocrlf', 'false');
  git('remote', 'add', 'origin', 'git@github.com:x/gitreview.git');
  for (const f of ['a.js', 'b.js', 'd.js']) writeFileSync(join(repo, f), lines('orig'));
  git('add', '.');
  git('commit', '-q', '-m', 'init');
  const env = { ...process.env, NXY_HOME: join(dir, 'home'), CLAUDE_PROJECT_DIR: repo };
  const node = (args, input) => spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', ...args], { input, encoding: 'utf8', env, cwd: repo });
  const hook = (file, o) => execFileSync(process.execPath, ['--disable-warning=ExperimentalWarning', join(HOOKS, file)], {
    input: JSON.stringify({ cwd: repo, session_id: 's1', ...o }), encoding: 'utf8', env,
  });
  return { dir, repo, git, node, hook };
}

test('git: files changed without a copy are found, dirty-at-approval ones are named, .git is untouched', (t) => {
  if (!hasGit) return t.skip('git is not available');
  const { repo, git, node, hook } = setup();
  // The user's own uncommitted work, from before the plan is approved.
  writeFileSync(join(repo, 'a.js'), lines('user'));
  assert.equal(node([MEM, 'handoff', 'plan', '--cwd', repo], PLAN).status, 0);
  const dirtyFile = join(repo, '.nxy', 'local', 'plan-dirty.json');
  assert.deepEqual(JSON.parse(readFileSync(dirtyFile, 'utf8')).files, ['a.js']);

  // After the plan: b.js and c.js change with no hook; d.js through the Edit hook (a copy).
  writeFileSync(join(repo, 'b.js'), lines('plan'));
  writeFileSync(join(repo, 'c.js'), 'brand new\n');
  hook('pretooluse-edit.mjs', { tool_name: 'Edit', agent_id: 'a1', tool_input: { file_path: join(repo, 'd.js') } });
  writeFileSync(join(repo, 'd.js'), lines('edited'));
  // a.js changes again with no copy: cannot be told from the user's part.
  writeFileSync(join(repo, 'a.js'), lines('user and plan'));

  git('status', '--porcelain'); // warm-up: after it the index needs no refresh
  const indexPath = join(repo, '.git', 'index');
  const indexBefore = readFileSync(indexPath);
  const out = node([REVIEW, 'packet', '--full', '--cwd', repo]);
  assert.equal(out.status, 0, out.stderr);
  const packet = out.stdout;
  assert.ok(readFileSync(indexPath).equals(indexBefore), '.git/index is byte-identical after the packet');

  assert.match(packet, /- b\.js \(modified \(git\), \+1 −1\)/);
  assert.match(packet, /- c\.js \(new \(git\), \+1 −0\)/);
  assert.match(packet, /- d\.js \(modified, \+1 −1\)/, 'a copy, not marked (git)');
  assert.doesNotMatch(packet, /- a\.js \(/, 'a.js is not in the changed list');
  assert.match(packet, /-line2 orig\n\+line2 plan/, 'b.js against HEAD');
  assert.match(packet, /--- \/dev\/null\n\+\+\+ b\/c\.js/, 'c.js whole');
  assert.match(packet, /## Not reviewed\n- a\.js: already modified before the plan was approved/);
  assert.doesNotMatch(packet, /^- \.nxy/m, 'nothing from .nxy/');

  // A second save carrying the same plan does not re-take the list.
  const body = ['route: implementer', '## Done', '- b.js changed', '## Next', '- run batch 2'].join('\n');
  const saved = node([MEM, 'handoff', 'save', '--cwd', repo], `${body}\n\n${PLAN}\n`);
  assert.equal(saved.status, 0, saved.stdout + saved.stderr);
  assert.deepEqual(JSON.parse(readFileSync(dirtyFile, 'utf8')).files, ['a.js'], 'b.js is not "already dirty"');

  // record: a finding in the (git) hunk is the change's.
  const findings = JSON.stringify([{ lens: 'base', severity: 'high', file: 'b.js', line: 2, title: 'bad line', fix: 'x' }]);
  const rec = node([REVIEW, 'record', '--cwd', repo], findings);
  assert.equal(rec.status, 0, rec.stderr);
  assert.match(rec.stdout, /1 finding from the change \(1 high/);

  node([MEM, 'handoff', 'done', '--cwd', repo]);
  assert.ok(!existsSync(dirtyFile), 'the list goes with the plan');
});

test('git: the tester hook dispatches the reviewer when only a git-found file changed', (t) => {
  if (!hasGit) return t.skip('git is not available');
  const { repo, node, hook } = setup();
  const body = ['route: implementer', '## Done', '- none', '## Next', '- review'].join('\n');
  assert.equal(node([MEM, 'handoff', 'save', '--cwd', repo], `${body}\n\n${PLAN}\n`).status, 0);
  // No copy exists: b.js changes with no hook.
  writeFileSync(join(repo, 'b.js'), lines('plan'));
  recordSuite(repo, planHash(PLAN), { status: 'done', ts: Date.now(), red: [], ran: [] });
  const out = hook('posttooluse-agent.mjs', { tool_name: 'Agent', tool_input: { subagent_type: 'nxy:tester', prompt: 'run the full suite' } });
  assert.match(out, /dispatch the nxy:reviewer subagent/);
});

test('git: not a git repo, the packet says so and reviews the copies', (t) => {
  if (!hasGit) return t.skip('git is not available');
  const dir = mkdtempSync(join(tmpdir(), 'nxy-review-nogit-'));
  const plain = join(dir, 'plain');
  mkdirSync(plain, { recursive: true });
  const env = { ...process.env, NXY_HOME: join(dir, 'home'), CLAUDE_PROJECT_DIR: plain };
  const node = (args, input) => spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', ...args], { input, encoding: 'utf8', env, cwd: plain });
  writeFileSync(join(plain, 'b.js'), lines('orig'));
  assert.equal(node([MEM, 'handoff', 'plan', '--cwd', plain], PLAN).status, 0);
  execFileSync(process.execPath, ['--disable-warning=ExperimentalWarning', join(HOOKS, 'pretooluse-edit.mjs')], {
    input: JSON.stringify({ cwd: plain, session_id: 's1', tool_name: 'Edit', agent_id: 'a1', tool_input: { file_path: join(plain, 'b.js') } }),
    encoding: 'utf8', env,
  });
  writeFileSync(join(plain, 'b.js'), lines('plan'));
  const packet = node([REVIEW, 'packet', '--full', '--cwd', plain]).stdout;
  assert.match(packet, /- b\.js \(modified, \+1 −1\)/);
  assert.match(packet, /Git: not a git repo/);
});
