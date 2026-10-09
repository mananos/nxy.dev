// @ts-check
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { copyFileSync, existsSync, mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { test } from 'node:test';
import { ROOT, sandbox } from './sandbox.mjs';
import { recordBatch } from '../hosts/claude-code/verify-state.mjs';
import { projectSlug } from '../hosts/claude-code/paths.mjs';
import { nxyRuntimeDir } from '../core/paths.mjs';
import { APPROVE, approvalQuestion } from '../core/plan.mjs';
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
  assert.deepEqual(s.batches.map((b) => [b.n, b.depends, b.accept.kind]), [[1, [], 'command'], [2, [1], 'command']]);
  assert.equal(s.goal, 'a small change');
  assert.deepEqual(s.batches.map((b) => b.files), [['src/a.mjs'], ['src/b.mjs']]);
  assert.equal(s.batches[0].accept.command, 'node --test tests/a.test.mjs');
  assert.equal(s.reviewDetail, null);
  assert.ok(s.runtimeDir.includes('.nxy') || s.runtimeDir.length > 0);
  assert.deepEqual(s.verdicts, {});
});

test('the snapshot carries the review in full, and no plan means no goal or review', () => {
  const sb = sandbox(PLAN);
  const dir = nxyRuntimeDir(sb.repo);
  mkdirSync(dir, { recursive: true });
  writeFileSync(join(dir, 'review.json'), JSON.stringify({
    planHash: sb.hash, id: 'r1abc', ts: Date.now() + 1000,
    findings: [{ id: 'R1', severity: 'high', lens: 'bugs', file: 'src/a.mjs', line: 7, title: 't', cause: 'change' }],
  }));
  writeFileSync(join(dir, 'review.jsonl'), `${JSON.stringify({ kind: 'chosen', id: 'r1abc', finding: 'R1' })}\n`);
  const s = run(sb, ['snapshot', '--cwd', sb.repo, '--transcript', sb.main()]);
  assert.equal(s.reviewDetail.id, 'r1abc');
  assert.deepEqual(s.reviewDetail.findings, [{ id: 'R1', severity: 'high', lens: 'bugs', file: 'src/a.mjs', line: 7, title: 't', cause: 'change' }]);
  assert.deepEqual(s.reviewDetail.chosen, ['R1']);
  const none = sandbox('');
  const n = run(none, ['snapshot', '--cwd', none.repo]);
  assert.equal(n.goal, '');
  assert.equal(n.reviewDetail, null);
});

test('a review finding without a line keeps line null', () => {
  const sb = sandbox(PLAN);
  const dir = nxyRuntimeDir(sb.repo);
  mkdirSync(dir, { recursive: true });
  writeFileSync(join(dir, 'review.json'), JSON.stringify({
    planHash: sb.hash, id: 'r2', ts: Date.now() + 1000,
    findings: [{ id: 'R1', severity: 'low', lens: 'bugs', file: 'src/a.mjs', title: 't', cause: 'change' }],
  }));
  const s = run(sb, ['snapshot', '--cwd', sb.repo, '--transcript', sb.main()]);
  assert.equal(s.reviewDetail.findings[0].line, null);
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

/** A session under the sandbox's projects dir with timestamps: 3 calls, the approval between the 1st and the 2nd. */
function timedSession(sb, id = 'sess-t') {
  const projects = join(sb.dir, 'projects');
  const folder = join(projects, projectSlug(sb.repo));
  mkdirSync(folder, { recursive: true });
  const q = approvalQuestion(sb.hash);
  const at = (s) => new Date(Date.UTC(2026, 0, 1, 10, 0, s)).toISOString();
  const call = (n, s, extra = {}) => ({
    type: 'assistant', timestamp: at(s), requestId: `req-${n}`,
    message: { id: `msg_${n}`, model: 'claude-sonnet-4-5', usage: { input_tokens: 10, output_tokens: 20, cache_read_input_tokens: 1000, ...extra } },
  });
  const lines = [
    call(1, 0, { cache_creation_input_tokens: 500, cache_creation: { ephemeral_5m_input_tokens: 500, ephemeral_1h_input_tokens: 0 } }),
    { type: 'assistant', timestamp: at(10), message: { content: [{ type: 'tool_use', id: 'q0', name: 'AskUserQuestion', input: { questions: [{ question: q }] } }] } },
    { type: 'user', timestamp: at(20), message: { content: [{ type: 'tool_result', tool_use_id: 'q0', content: 'ok' }] }, toolUseResult: { answers: { [q]: APPROVE } } },
    call(2, 30),
    call(3, 40, { cache_creation_input_tokens: 700, cache_creation: { ephemeral_5m_input_tokens: 0, ephemeral_1h_input_tokens: 700 } }),
  ];
  writeFileSync(join(folder, `${id}.jsonl`), lines.map((l) => JSON.stringify(l)).join('\n') + '\n');
  return { projects, approvedAt: Date.parse(at(20)), firstCall: Date.parse(at(30)) };
}

test('the snapshot carries the model and effort of the 7 nxy roles', () => {
  const sb = sandbox(PLAN);
  const s = run(sb, ['snapshot', '--cwd', sb.repo, '--transcript', sb.main()]);
  assert.deepEqual(Object.keys(s.roles).sort(), ['documenter', 'implementer', 'librarian', 'planner', 'reviewer', 'scout', 'tester']);
  assert.deepEqual(s.roles.implementer, { model: 'sonnet', effort: 'medium' });
});

test('the snapshot carries the price table and the cache TTL of the session', () => {
  const sb = sandbox(PLAN);
  const t = timedSession(sb);
  const s = run(sb, ['snapshot', '--cwd', sb.repo, '--session', 'sess-t', '--projects-dir', t.projects]);
  assert.equal(s.cacheTtl, '1h');
  assert.equal(typeof s.prices, 'object');
  assert.ok(Object.keys(s.prices).length > 0);
  const none = sandbox('');
  const n = run(none, ['snapshot', '--cwd', none.repo]);
  assert.equal(n.cacheTtl, null);
  assert.ok(Object.keys(n.prices).length > 0, 'prices also without a plan');
});

test('summary: the approval splits the session in two', () => {
  const sb = sandbox(PLAN);
  const t = timedSession(sb);
  const s = run(sb, ['summary', '--hash', sb.hash, '--session', 'sess-t', '--projects-dir', t.projects, '--cwd', sb.repo]);
  assert.equal(s.ok, true);
  assert.equal(s.sinceKind, 'approval');
  assert.equal(s.approvedAt, t.approvedAt);
  assert.equal(s.session.usage.calls, 3);
  assert.equal(s.feature.usage.calls, 2);
  assert.equal(s.feature.firstTs, t.approvedAt, 'the feature starts at the approval line');
  assert.equal(s.session.firstTs < s.feature.firstTs, true);
  assert.equal(s.cacheTtl, '1h');
  assert.equal(typeof s.feature.cacheBreaks.count, 'number');
});

test('summary: without the approval it falls back to --fallback-since, then to the whole session', () => {
  const sb = sandbox(PLAN);
  const t = timedSession(sb);
  const args = ['summary', '--hash', 'ffffff', '--session', 'sess-t', '--projects-dir', t.projects, '--cwd', sb.repo];
  const fb = run(sb, [...args, '--fallback-since', String(t.firstCall + 5_000)]);
  assert.equal(fb.sinceKind, 'first-batch');
  assert.equal(fb.approvedAt, null);
  assert.equal(fb.feature.usage.calls, 1);
  const whole = run(sb, args);
  assert.equal(whole.sinceKind, 'session');
  assert.equal(whole.feature.usage.calls, 3);
});

test('summary: an unknown session is {ok:false}', () => {
  const sb = sandbox(PLAN);
  assert.equal(run(sb, ['summary', '--hash', sb.hash, '--session', 'nope', '--cwd', sb.repo]).ok, false);
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
