// @ts-check
/**
 * The chat-rules question (1.2.0): when a task closes, the main thread offers, once, the standing
 * rules the user stated in the chat as repo conventions. Checkpoint 2 carries it (or `handoff done`
 * when there was no review); a hook counts the offers without a model call.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { CHAT_RULES_QUESTION, checkpoint2, chatRulesInstruction } from '../core/review.mjs';
import { claimChatRulesAsk, recallLedgerPath } from '../hosts/claude-code/recall-state.mjs';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const HOOKS = join(ROOT, 'hosts', 'claude-code', 'hooks');
const MEM = join(ROOT, 'hosts', 'claude-code', 'entries', 'mem.mjs');
const REVIEW = join(ROOT, 'hosts', 'claude-code', 'entries', 'review.mjs');

const PLAN = ['## Plan', 'Goal: x', '### Batch 1 — api: files', '- `b.js:2` — change b', 'Accept: `./mvnw -q test`'].join('\n');
const RULES = chatRulesInstruction('MEMSAVE');
const count = (text, needle) => text.split(needle).length - 1;

function sandbox() {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-chatrules-'));
  const repo = join(dir, 'repo');
  mkdirSync(repo, { recursive: true });
  writeFileSync(join(repo, 'b.js'), 'line1\nline2\nline3\n');
  const env = { ...process.env, NXY_HOME: join(dir, 'home'), CLAUDE_PROJECT_DIR: repo };
  const node = (args, input) => spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', ...args], { input, encoding: 'utf8', env, cwd: repo });
  return { dir, repo, env, node };
}

const finding = (id, cause = 'change') => ({ id, severity: 'high', lens: 'base', file: 'b.js', line: 2, title: `t${id}`, fix: '', cause });
const base = (findings, extra) => ({
  id: 'abc123', planHash: 'h', findings, summary: { files: 1, added: 1, removed: 0 }, batchOf: () => 1, extra, chatRules: RULES,
});
const docsExtra = { question: { question: 'Update docs?', header: 'Docs', multiSelect: true, options: [] }, then: 'then docs' };

test('chatRulesInstruction: only rules meant to outlast the task; no false "same call" claim', () => {
  assert.match(RULES, /corrections the user said should always apply/);
  assert.match(RULES, /only if the user said it should outlast this task/);
  assert.match(RULES, /if you are also asking another question/);
});

test('checkpoint2: the chat-rules instruction appears exactly once in every branch', () => {
  const none = checkpoint2(base([]));
  assert.equal(count(none, CHAT_RULES_QUESTION), 1, 'no findings, no docs question');
  const docs = checkpoint2(base([], docsExtra));
  assert.equal(count(docs, CHAT_RULES_QUESTION), 1, 'no findings, docs question');
  const some = checkpoint2(base(Array.from({ length: 12 }, (_, i) => finding(`R${i + 1}`)), docsExtra));
  assert.equal(count(some, CHAT_RULES_QUESTION), 1, 'findings');
  const groups = (some.match(/Fix which findings/g) || []).length;
  assert.equal(groups, 2, 'the cap leaves room for the docs and the rules questions: 4 - 1 - 1');
  assert.equal(count(checkpoint2({ ...base([finding('R1')]), chatRules: null }), CHAT_RULES_QUESTION), 0, 'off when not claimed');
});

test('claimChatRulesAsk: true only the first time for a task', () => {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-claim-'));
  const prev = process.env.NXY_HOME;
  process.env.NXY_HOME = join(dir, 'home');
  try {
    assert.equal(claimChatRulesAsk(dir, 't1'), true);
    assert.equal(claimChatRulesAsk(dir, 't1'), false);
    assert.equal(claimChatRulesAsk(dir, 't2'), true);
  } finally {
    if (prev === undefined) delete process.env.NXY_HOME; else process.env.NXY_HOME = prev;
  }
});

test('review record prints the question once; show does not repeat it', () => {
  const s = sandbox();
  assert.equal(s.node([MEM, 'handoff', 'plan', '--cwd', s.repo], PLAN).status, 0);
  writeFileSync(join(s.repo, 'b.js'), 'line1\nline2 changed\nline3\n');
  const findings = JSON.stringify([{ lens: 'base', severity: 'high', file: 'b.js', line: 2, title: 'bad line', fix: 'x' }]);
  const rec = s.node([REVIEW, 'record', '--cwd', s.repo], findings);
  assert.equal(rec.status, 0, rec.stderr);
  assert.equal(count(rec.stdout, CHAT_RULES_QUESTION), 1);
  const show = s.node([REVIEW, 'show', '--cwd', s.repo]);
  assert.equal(count(show.stdout, CHAT_RULES_QUESTION), 0);
  const done = s.node([MEM, 'handoff', 'done', '--cwd', s.repo]);
  assert.match(done.stdout, /archived handoff/);
  assert.equal(count(done.stdout, CHAT_RULES_QUESTION), 0, 'review already claimed it');
});

test('handoff done without a review prints the question, once', () => {
  const s = sandbox();
  assert.equal(s.node([MEM, 'handoff', 'plan', '--cwd', s.repo], PLAN).status, 0);
  const done = s.node([MEM, 'handoff', 'done', '--cwd', s.repo]);
  assert.match(done.stdout, /archived handoff/);
  assert.equal(count(done.stdout, CHAT_RULES_QUESTION), 1);
  assert.match(done.stdout, /Next \(main thread\): Conventions from the chat/);
  assert.match(done.stdout, /--type convention/);
});

test('handoff done the way the panel runs it (--json) does not claim the question', () => {
  const s = sandbox();
  assert.equal(s.node([MEM, 'handoff', 'plan', '--cwd', s.repo], PLAN).status, 0);
  const panel = s.node([MEM, 'handoff', 'done', '--json', '--cwd', s.repo]);
  assert.equal(panel.status, 0, panel.stderr);
  assert.equal(count(panel.stdout, CHAT_RULES_QUESTION), 0, 'the panel has no main thread to ask');
  assert.equal(s.node([MEM, 'handoff', 'plan', '--cwd', s.repo], PLAN).status, 0);
  const done = s.node([MEM, 'handoff', 'done', '--cwd', s.repo]);
  assert.equal(count(done.stdout, CHAT_RULES_QUESTION), 1, 'a later main-thread done still asks');
});

test('hook: a load error or malformed stdin still exits 0 and prints nothing', () => {
  const s = sandbox();
  // A copy outside the repo: its relative imports do not resolve.
  const lone = join(s.dir, 'pretooluse-ask.mjs');
  writeFileSync(lone, readFileSync(join(HOOKS, 'pretooluse-ask.mjs'), 'utf8'));
  const broken = spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', lone], { input: '{}', encoding: 'utf8', env: s.env });
  assert.equal(broken.status, 0);
  assert.equal(broken.stdout, '');
  const bad = spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', join(HOOKS, 'pretooluse-ask.mjs')], { input: '{not json', encoding: 'utf8', env: s.env });
  assert.equal(bad.status, 0);
  assert.equal(bad.stdout, '');
});

test('hook: one convention-offer row for the rules question, nothing for others, nothing printed', () => {
  const s = sandbox();
  const run = (questions) => spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', join(HOOKS, 'pretooluse-ask.mjs')], {
    input: JSON.stringify({ cwd: s.repo, tool_name: 'AskUserQuestion', tool_input: { questions } }), encoding: 'utf8', env: s.env,
  });
  const other = run([{ question: 'Approve?', options: [{ label: 'a' }] }]);
  assert.equal(other.status, 0);
  const ask = run([{ question: CHAT_RULES_QUESTION, header: 'Conventions', multiSelect: true, options: [{ label: 'Always X' }, { label: 'Never Y' }] }]);
  assert.equal(ask.status, 0);
  assert.equal(ask.stdout, '');
  assert.equal(ask.stderr, '');
  assert.equal(run('not an array').status, 0);
  const prev = process.env.NXY_HOME;
  process.env.NXY_HOME = s.env.NXY_HOME;
  let ledger;
  try { ledger = recallLedgerPath(s.repo); } finally { if (prev === undefined) delete process.env.NXY_HOME; else process.env.NXY_HOME = prev; }
  assert.ok(existsSync(ledger));
  const rows = readFileSync(ledger, 'utf8').split('\n').filter(Boolean).map((l) => JSON.parse(l)).filter((r) => r.ev === 'convention-offer');
  assert.equal(rows.length, 1);
  assert.equal(rows[0].source, 'chat');
  assert.deepEqual(rows[0].rules, ['Always X', 'Never Y']);
});
