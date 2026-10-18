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
import { taskKey } from '../hosts/claude-code/tasks.mjs';
import { taskDir } from '../hosts/claude-code/plan-approval.mjs';
import { changedFiles, readBaseline, reviewFiles } from '../hosts/claude-code/baseline.mjs';
import { takeNotes } from '../hosts/claude-code/agent-notes.mjs';
import { readSuite, readSuiteRepos } from '../hosts/claude-code/verify-state.mjs';
import { sandbox } from './sandbox.mjs';

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
  assert.ok(readBaseline(s.A, taskKey(s.A))[resolve(s.B, 'f.js')], 'an entry for the file in B');
  writeFileSync(join(s.B, 'f.js'), 'two\n');
  assert.deepEqual(changedFiles(s.A, taskKey(s.A)).map((f) => f.path), ['B/f.js']);
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
  assert.ok(readBaseline(s.A, taskKey(s.A))[resolve(s.B, 'f.js')], '`>` in B left a copy');
  const sed = JSON.stringify({ cwd: s.A, tool_name: 'Bash', tool_input: { command: `sed -i s/one/two/ ${p}` } });
  const r = execFileSync(process.execPath, [join(HOOKS, 'pretooluse-bash.mjs')], { input: sed, encoding: 'utf8', env: { ...process.env, NXY_HOME: join(s.dir, 'home'), CLAUDE_PROJECT_DIR: s.A } });
  assert.doesNotMatch(r, /"permissionDecision":"deny"/, 'the main thread is not denied');
});

test('hooks: an edit in an undeclared repo leaves one note, and none in the project leaves no state file', () => {
  const s = setup();
  const edit = (file) => s.hook('pretooluse-edit.mjs', { tool_name: 'Edit', tool_input: { file_path: file } });
  edit(join(s.A, 'a.js'));
  const extra = join(taskDir(s.A, taskKey(s.A)), 'plan-extra-repos.json');
  assert.equal(existsSync(extra), false, 'A alone creates nothing');
  edit(join(s.C, 'f.js'));
  assert.match(takeNotes(s.A).join('\n'), /C is not in the plan; its prior uncommitted changes cannot be told apart/);
  edit(join(s.C, 'f.js'));
  assert.deepEqual(takeNotes(s.A), [], 'the second edit adds no note');
  assert.ok(existsSync(extra));
});

test('hooks: an undeclared repo with a file changed shows in Not reviewed', (t) => {
  if (!hasGit) return t.skip('git is not available');
  const s = setup(true);
  s.hook('pretooluse-edit.mjs', { tool_name: 'Edit', tool_input: { file_path: join(s.C, 'f.js') } });
  writeFileSync(join(s.C, 'g.js'), 'dirty, no copy\n');
  const rv = reviewFiles(s.A, taskKey(s.A));
  assert.ok(rv.notReviewed.some((n) => /^C\/g\.js/.test(typeof n === 'string' ? n : n.path || '')), JSON.stringify(rv.notReviewed));
});

const CHECK_A = 'npm run check-a';
const CHECK_B = 'npm run check-b';

/** A sandbox repo (A) plus a declared second repo (B), each with its own `Suite:` line. */
function twoRepos() {
  const bDir = mkdtempSync(join(tmpdir(), 'nxy-rt-'));
  const B = join(bDir, 'otherrepo');
  mkdirSync(join(B, '.git'), { recursive: true });
  const plan = [
    '## Plan', 'Goal: two repos', '### Repos', '- `.` — main', `- \`${fwd(B)}\` — other`,
    '### Batch 1 — a', '- `a.js` — change', 'Accept: `node -v`',
    `Suite: repo — \`${CHECK_A}\``, `Suite: otherrepo — \`${CHECK_B}\``,
  ].join('\n');
  const s = sandbox(plan);
  const pa = `Full suite for nxy plan ${s.hash} (repo: ${fwd(s.repo)})`;
  const pb = `Full suite for nxy plan ${s.hash} (repo: ${fwd(B)})`;
  const alone = (p) => p.replace(/\)$/, ', alone)');
  const tester = (id, prompt, steps) => s.stop(id, s.agent(prompt, steps), 'nxy:tester');
  return { s, B, pa, pb, alone, tester, dispatch: (p) => s.dispatch(p, undefined, 'nxy:tester'), told: (p) => s.returnedAs(p, 'nxy:tester') };
}
const isDenied = (d) => d?.permissionDecision === 'deny';

test('per repo: two testers; the first stop leaves the suite running, the second finishes it', () => {
  const t = twoRepos();
  assert.ok(!isDenied(t.dispatch(t.pa)));
  assert.ok(!isDenied(t.dispatch(t.pb)));
  assert.equal(readSuiteRepos(t.s.repo, t.s.hash).length, 2);
  assert.ok(isDenied(t.s.dispatch(`Review nxy plan ${t.s.hash}`, undefined, 'nxy:reviewer')), 'suite running');
  t.tester('ta', t.pa, [{ run: CHECK_A }]);
  assert.equal(readSuite(t.s.repo, t.s.hash)?.status, 'running');
  assert.match(t.told(t.pa), /suite of repo passed; waiting for otherrepo/);
  assert.ok(isDenied(t.s.dispatch(`Review nxy plan ${t.s.hash}`, undefined, 'nxy:reviewer')), 'one repo is still running');
  t.tester('tb', t.pb, [{ run: CHECK_B }]);
  const suite = readSuite(t.s.repo, t.s.hash);
  assert.equal(suite?.status, 'done');
  assert.deepEqual(suite?.red, []);
});

test('per repo: a second round drops the other repos\' done records of the first, so the aggregate waits', () => {
  const t = twoRepos();
  t.dispatch(t.pa);
  t.tester('ta', t.pa, [{ run: CHECK_A, ok: false, out: 'Exit code 1' }]);
  t.dispatch(t.pb);
  t.tester('tb', t.pb, [{ run: CHECK_B }]);
  assert.equal(readSuite(t.s.repo, t.s.hash)?.status, 'done');
  assert.equal(readSuite(t.s.repo, t.s.hash)?.red?.length, 1);
  // Round 2: only A is dispatched again. B's done record is round 1's and must not count.
  assert.ok(!isDenied(t.dispatch(t.pa)));
  const repos = readSuiteRepos(t.s.repo, t.s.hash);
  assert.equal(repos.length, 1, 'the old record of the other repo is gone');
  assert.equal(repos[0].status, 'running');
  assert.equal(readSuite(t.s.repo, t.s.hash)?.status, 'running');
  // A passes: round 1's red of B's record (and B's record itself) is gone, not carried into this round.
  t.tester('ta2', t.pa, [{ run: CHECK_A }]);
  assert.equal(readSuiteRepos(t.s.repo, t.s.hash).length, 1);
  assert.deepEqual(readSuite(t.s.repo, t.s.hash)?.red ?? [], []);
  assert.ok(!isDenied(t.dispatch(t.pb)));
  t.tester('tb2', t.pb, [{ run: CHECK_B }]);
  const suite = readSuite(t.s.repo, t.s.hash);
  assert.equal(suite?.status, 'done');
  assert.deepEqual(suite?.red, [], 'round 1\'s red is gone');
});

test('per repo: a red stop that overlapped another tester is rechecked; rerun alone passes -> green, and says so', () => {
  const t = twoRepos();
  t.dispatch(t.pa);
  t.dispatch(t.pb);
  t.tester('ta', t.pa, [{ run: CHECK_A, ok: false, out: 'Exit code 1\n1 failed' }]);
  assert.ok(readSuiteRepos(t.s.repo, t.s.hash).some((r) => r.status === 'recheck' && !/otherrepo/.test(r.repo)));
  assert.equal(readSuite(t.s.repo, t.s.hash)?.status, 'running', 'no red aggregate yet');
  const waiting = t.told(t.pa);
  assert.match(waiting, /failed while otherrepo ran at the same time; it is rerun alone/);
  assert.doesNotMatch(waiting, /Next: dispatch one nxy:tester/, 'the other tester still runs');
  assert.ok(isDenied(t.dispatch(t.alone(t.pa))), 'the rerun waits for the other tester');
  assert.match(t.dispatch(t.alone(t.pa))?.permissionDecisionReason, /wait for otherrepo; the rerun must run alone/);
  t.tester('tb', t.pb, [{ run: CHECK_B }]);
  assert.match(t.told(t.pb), new RegExp(`Next: dispatch one nxy:tester: \`Full suite for nxy plan ${t.s.hash} \\(repo: [^)]*, alone\\)\``));
  assert.ok(!isDenied(t.dispatch(t.alone(t.pa))));
  // The alone dispatch replaces the recheck record with a running one: the orchestrator cannot respawn it.
  const afterDispatch = readSuiteRepos(t.s.repo, t.s.hash);
  assert.ok(!afterDispatch.some((r) => r.status === 'recheck'), 'no recheck record left after the alone dispatch');
  assert.ok(afterDispatch.some((r) => r.status === 'running' && !/otherrepo/.test(r.repo)));
  t.tester('ta2', t.alone(t.pa), [{ run: CHECK_A }]);
  const suite = readSuite(t.s.repo, t.s.hash);
  assert.equal(suite?.status, 'done');
  assert.deepEqual(suite?.red, []);
  assert.match(t.told(t.alone(t.pa)), /repo: failed in parallel, rerun alone — passed \(the first failure was likely contention: `npm run check-a`\)/);
});

test('per repo: the rerun alone failing again is red', () => {
  const t = twoRepos();
  t.dispatch(t.pa);
  t.dispatch(t.pb);
  t.tester('ta', t.pa, [{ run: CHECK_A, ok: false, out: 'Exit code 1' }]);
  t.tester('tb', t.pb, [{ run: CHECK_B }]);
  t.dispatch(t.alone(t.pa));
  t.tester('ta2', t.alone(t.pa), [{ run: CHECK_A, ok: false, out: 'Exit code 1' }]);
  const suite = readSuite(t.s.repo, t.s.hash);
  assert.equal(suite?.status, 'done');
  assert.equal(suite?.red?.length, 1);
  const said = t.told(t.alone(t.pa));
  assert.match(said, /full suite of plan .* ✘/);
  assert.match(said, /repo: failed in parallel, rerun alone — failed again alone: red/);
});

test('per repo: a red stop that overlapped nothing is red at once, no rerun', () => {
  const t = twoRepos();
  t.dispatch(t.pa);
  t.tester('ta', t.pa, [{ run: CHECK_A, ok: false, out: 'Exit code 1' }]);
  assert.ok(readSuiteRepos(t.s.repo, t.s.hash).every((r) => r.status === 'done'));
  assert.doesNotMatch(t.told(t.pa), /rerun alone/);
  t.dispatch(t.pb);
  t.tester('tb', t.pb, [{ run: CHECK_B }]);
  const suite = readSuite(t.s.repo, t.s.hash);
  assert.equal(suite?.status, 'done');
  assert.equal(suite?.red?.length, 1);
  assert.equal(suite?.rerun, undefined);
});
