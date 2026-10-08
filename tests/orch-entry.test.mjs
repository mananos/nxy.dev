// @ts-check
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { copyFileSync, existsSync, mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { test } from 'node:test';
import { ROOT, sandbox } from './sandbox.mjs';
import { recordBatch } from '../hosts/claude-code/verify-state.mjs';
import { projectSlug } from '../hosts/claude-code/paths.mjs';
import { orchDir } from '../hosts/claude-code/orch-state.mjs';

const ORCH = join(ROOT, 'hosts', 'claude-code', 'entries', 'orch.mjs');
const PLAN = [
  '## Plan',
  'Goal: a small change',
  '### Batch 1 — first',
  '- `src/a.mjs` — do a',
  'Accept: `node --test tests/a.test.mjs`',
  '### Batch 2 — second',
  'Depends: 1',
  '- `src/b.mjs` — do b',
  'Accept: `node --test tests/b.test.mjs`',
].join('\n');
const WITH_QUESTIONS = `${PLAN}\n### Questions\n- Q: which one?\n  - A — first\n  - B — second`;

function run(sb, args) {
  const r = spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', ORCH, ...args], { encoding: 'utf8', env: sb.env });
  return JSON.parse(r.stdout.trim().split('\n').pop() ?? '');
}

test('an approved plan is approved and fresh, with its batches', () => {
  const sb = sandbox(PLAN);
  const s = run(sb, ['snapshot', '--cwd', sb.repo, '--transcript', sb.main()]);
  assert.equal(s.ok, true);
  assert.equal(s.hash, sb.hash);
  assert.equal(s.approved, true);
  assert.equal(s.fresh, true);
  assert.equal(s.branch, 'feature/x');
  assert.deepEqual(s.batches.map((b) => [b.n, b.depends, b.accept]), [[1, [], 'command'], [2, [1], 'command']]);
  assert.ok(s.runtimeDir.includes('.nxy') || s.runtimeDir.length > 0);
  assert.deepEqual(s.verdicts, {});
});

test('a recorded verdict makes the plan not fresh', () => {
  const sb = sandbox(PLAN);
  recordBatch(sb.repo, 'feature/x', sb.hash, 1, { status: 'running', ts: Date.now() });
  const s = run(sb, ['snapshot', '--cwd', sb.repo, '--transcript', sb.main()]);
  assert.equal(s.approved, true);
  assert.equal(s.fresh, false);
  assert.equal(s.verdicts['1'].status, 'running');
});

test('no approval in the transcript, or an unknown transcript, is not approved', () => {
  const sb = sandbox(PLAN);
  const empty = join(sb.dir, 'empty.jsonl');
  writeFileSync(empty, '');
  assert.equal(run(sb, ['snapshot', '--cwd', sb.repo, '--transcript', empty]).approved, false);
  const none = sandbox(PLAN);
  assert.equal(run(none, ['snapshot', '--cwd', none.repo]).approved, false);
  assert.equal(run(none, ['snapshot', '--cwd', none.repo, '--transcript', join(none.dir, 'missing.jsonl')]).approved, false);
});

test('a plan with open questions is not approved', () => {
  const sb = sandbox(WITH_QUESTIONS);
  const s = run(sb, ['snapshot', '--cwd', sb.repo, '--transcript', sb.main()]);
  assert.equal(s.approved, false);
  assert.equal(s.fresh, false);
});

test('--session resolves the transcript under the projects dir', () => {
  const sb = sandbox(PLAN);
  const projects = join(sb.dir, 'projects');
  const folder = join(projects, projectSlug(sb.repo));
  mkdirSync(folder, { recursive: true });
  copyFileSync(sb.main(), join(folder, 'sess-1.jsonl'));
  const s = run(sb, ['snapshot', '--cwd', sb.repo, '--session', 'sess-1', '--projects-dir', projects]);
  assert.equal(s.approved, true);
  // a session with no transcript there: not approved (a fresh sandbox, the approval cache is per project)
  const other = sandbox(PLAN);
  assert.equal(run(other, ['snapshot', '--cwd', other.repo, '--session', 'sess-1', '--projects-dir', projects]).approved, false);
});

test('release removes the marker', () => {
  const sb = sandbox(PLAN);
  const dir = orchDir(sb.repo);
  mkdirSync(dir, { recursive: true });
  const marker = join(dir, 'active.json');
  writeFileSync(marker, JSON.stringify({ ts: Date.now() }));
  assert.equal(run(sb, ['release', '--cwd', sb.repo]).ok, true);
  assert.equal(existsSync(marker), false);
});

test('release --snapshot returns the snapshot with titles and the filter state; plain release stays {ok:true}', () => {
  const sb = sandbox(PLAN);
  const s = run(sb, ['release', '--snapshot', '--cwd', sb.repo, '--transcript', sb.main()]);
  assert.equal(s.ok, true);
  assert.equal(s.hash, sb.hash);
  assert.deepEqual(s.batches.map((b) => b.title), ['first', 'second']);
  assert.equal(typeof s.config.filter, 'boolean');
  assert.deepEqual(run(sb, ['release', '--cwd', sb.repo]), { ok: true });
});

test('batch titles: a hyphen separator is stripped, a title with no Batch N prefix is kept raw', () => {
  const plan = [
    '## Plan',
    'Goal: titles',
    '### Batch 1 — first',
    '- `src/a.mjs` — do a',
    'Accept: `node --test tests/a.test.mjs`',
    '### Wire it up',
    '- `src/b.mjs` — do b',
    'Accept: `node --test tests/b.test.mjs`',
    '### Batch 3 - foo',
    '- `src/c.mjs` — do c',
    'Accept: `node --test tests/c.test.mjs`',
  ].join('\n');
  const sb = sandbox(plan);
  const s = run(sb, ['snapshot', '--cwd', sb.repo, '--transcript', sb.main()]);
  assert.deepEqual(s.batches.map((b) => [b.n, b.title]), [[1, 'first'], [2, 'Wire it up'], [3, 'foo']]);
});

test('the default config has the orchestrator on and no pause', () => {
  const sb = sandbox(PLAN);
  const s = run(sb, ['snapshot', '--cwd', sb.repo, '--transcript', sb.main()]);
  assert.deepEqual(s.config, { pauseAfterBatch: false, orchestrator: 'auto', ui: { panel: 'auto' }, gate: { enabled: true, contextTokens: 100000 }, filter: true });
});

test('ui.panel off from the project config, and the handoff age', () => {
  const sb = sandbox(PLAN);
  mkdirSync(join(sb.repo, '.nxy'), { recursive: true });
  writeFileSync(join(sb.repo, '.nxy', 'config.json'), JSON.stringify({ ui: { panel: 'off' } }));
  const s = run(sb, ['snapshot', '--cwd', sb.repo, '--transcript', sb.main()]);
  assert.equal(s.config.ui.panel, 'off');
  assert.equal(typeof s.handoff.updated, 'number');
  // no handoff (an empty NXY_HOME): the early-return branch still carries it, as null
  const none = run({ ...sb, env: { ...sb.env, NXY_HOME: join(sb.dir, 'empty-home') } }, ['snapshot', '--cwd', sb.repo]);
  assert.equal(none.handoff, null);
  assert.equal(none.config.ui.panel, 'off');
});
