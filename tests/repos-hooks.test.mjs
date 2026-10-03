// @ts-check
/**
 * Hooks across repos (1.0.1): an edit or a console write in a declared repo is copied and reviewed as
 * `<repo>/<rel>`; a subagent's shell write is denied in any repo and free outside every repo; a repo
 * the plan did not declare is noted once and shows up as not reviewed. Temp dirs only.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { gitBranch } from '../core/paths.mjs';
import { changedFiles, readBaseline, reviewFiles } from '../hosts/claude-code/baseline.mjs';
import { takeNotes } from '../hosts/claude-code/agent-notes.mjs';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const HOOKS = join(ROOT, 'hosts', 'claude-code', 'hooks');
const MEM = join(ROOT, 'hosts', 'claude-code', 'entries', 'mem.mjs');
const hasGit = spawnSync('git', ['--version'], { encoding: 'utf8' }).status === 0;
const fwd = (p) => p.replace(/\\/g, '/');

/** A (the project, no git), B (declared, a repo), C (undeclared, a repo), a scratch dir. */
function setup(realGit = false) {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-rh-'));
  const [A, B, C, S] = ['A', 'B', 'C', 'S'].map((n) => join(dir, n));
  for (const d of [A, B, C, S]) mkdirSync(d, { recursive: true });
  for (const r of [B, C]) {
    if (realGit) {
      const git = (...a) => execFileSync('git', a, { cwd: r, encoding: 'utf8' });
      git('init', '-q');
      git('config', 'user.name', 'nxy test');
      git('config', 'user.email', 'nxy@example.com');
      writeFileSync(join(r, 'f.js'), 'one\n');
      git('add', '.');
      git('commit', '-q', '-m', 'init');
    } else {
      mkdirSync(join(r, '.git'));
      writeFileSync(join(r, 'f.js'), 'one\n');
    }
  }
  const env = { ...process.env, NXY_HOME: join(dir, 'home'), CLAUDE_PROJECT_DIR: A };
  const node = (args, input) => spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', ...args], { input, encoding: 'utf8', env, cwd: A });
  const hook = (file, o) => execFileSync(process.execPath, ['--disable-warning=ExperimentalWarning', join(HOOKS, file)], {
    input: JSON.stringify({ cwd: A, session_id: 's1', agent_id: 'a1', agent_type: 'nxy:implementer', ...o }), encoding: 'utf8', env,
  });
  const plan = [
    '## Plan', 'Goal: x', '### Repos', '- `.` — main', `- \`${fwd(B)}\` — other`,
    '### Batch 1 — api: files', '- `a.js` — change', 'Accept: `node -v`',
  ].join('\n');
  assert.equal(node([MEM, 'handoff', 'plan', '--cwd', A], plan).status, 0);
  return { dir, A, B, C, S, hook };
}

const bash = (s, command) => s.hook('pretooluse-bash.mjs', { tool_name: 'Bash', tool_input: { command } });

test('hooks: an Edit in a declared repo is copied and reviewed as B/<rel>', () => {
  const s = setup();
  s.hook('pretooluse-edit.mjs', { tool_name: 'Edit', tool_input: { file_path: join(s.B, 'f.js') } });
  assert.ok(readBaseline(s.A, gitBranch(s.A))[resolve(s.B, 'f.js')], 'an entry for the file in B');
  writeFileSync(join(s.B, 'f.js'), 'two\n');
  assert.deepEqual(changedFiles(s.A, gitBranch(s.A)).map((f) => f.path), ['B/f.js']);
});

test('hooks: console writes in B are copied; a subagent sed -i in B is denied, in a scratch dir it is not', () => {
  const s = setup();
  const p = fwd(join(s.B, 'f.js'));
  const out = bash(s, `sed -i s/one/two/ ${p}`);
  assert.match(out, /"permissionDecision":"deny"/, 'subagent shell write in B');
  assert.equal(bash(s, `sed -i s/a/b/ ${fwd(join(s.S, 'x.txt'))}`).trim(), '', 'a scratch dir is free');
  // The main thread's console write is allowed and copied first.
  const main = JSON.stringify({ cwd: s.A, tool_name: 'Bash', tool_input: { command: `echo hi > ${p}` } });
  execFileSync(process.execPath, [join(HOOKS, 'pretooluse-bash.mjs')], { input: main, encoding: 'utf8', env: { ...process.env, NXY_HOME: join(s.dir, 'home'), CLAUDE_PROJECT_DIR: s.A } });
  assert.ok(readBaseline(s.A, gitBranch(s.A))[resolve(s.B, 'f.js')], '`>` in B left a copy');
  const sed = JSON.stringify({ cwd: s.A, tool_name: 'Bash', tool_input: { command: `sed -i s/one/two/ ${p}` } });
  const r = execFileSync(process.execPath, [join(HOOKS, 'pretooluse-bash.mjs')], { input: sed, encoding: 'utf8', env: { ...process.env, NXY_HOME: join(s.dir, 'home'), CLAUDE_PROJECT_DIR: s.A } });
  assert.doesNotMatch(r, /"permissionDecision":"deny"/, 'the main thread is not denied');
});

test('hooks: an edit in an undeclared repo leaves one note, and none in the project leaves no state file', () => {
  const s = setup();
  const edit = (file) => s.hook('pretooluse-edit.mjs', { tool_name: 'Edit', tool_input: { file_path: file } });
  edit(join(s.A, 'a.js'));
  assert.equal(existsSync(join(s.A, '.nxy', 'local', 'plan-extra-repos.json')), false, 'A alone creates nothing');
  edit(join(s.C, 'f.js'));
  assert.match(takeNotes(s.A).join('\n'), /C is not in the plan; its prior uncommitted changes cannot be told apart/);
  edit(join(s.C, 'f.js'));
  assert.deepEqual(takeNotes(s.A), [], 'the second edit adds no note');
  assert.ok(existsSync(join(s.A, '.nxy', 'local', 'plan-extra-repos.json')));
});

test('hooks: an undeclared repo with a file changed shows in Not reviewed', (t) => {
  if (!hasGit) return t.skip('git is not available');
  const s = setup(true);
  s.hook('pretooluse-edit.mjs', { tool_name: 'Edit', tool_input: { file_path: join(s.C, 'f.js') } });
  writeFileSync(join(s.C, 'g.js'), 'dirty, no copy\n');
  const rv = reviewFiles(s.A, gitBranch(s.A));
  assert.ok(rv.notReviewed.some((n) => /^C\/g\.js/.test(typeof n === 'string' ? n : n.path || '')), JSON.stringify(rv.notReviewed));
});
