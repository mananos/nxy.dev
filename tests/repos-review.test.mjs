// @ts-check
/**
 * Conventions, lenses and docs from every touched repo (1.0.1): a convention saved for B reaches the
 * implementer and the reviewer, a lens in B's `.nxy/lenses` applies to B's files, a doc in B that names
 * the changed file is found, and a repo the task never touched changes nothing. Temp dirs only.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { APPROVE, approvalQuestion, planHash } from '../core/plan.mjs';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const HOOKS = join(ROOT, 'hosts', 'claude-code', 'hooks');
const MEM = join(ROOT, 'hosts', 'claude-code', 'entries', 'mem.mjs');
const REVIEW = join(ROOT, 'hosts', 'claude-code', 'entries', 'review.mjs');
const fwd = (p) => p.replace(/\\/g, '/');

/** A (the project) and B (declared when `declare`), both fake git repos with their own remote; C never touched. */
function setup({ declare = true } = {}) {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-rr-'));
  const [A, B, C] = ['A', 'B', 'C'].map((n) => join(dir, n));
  for (const [r, remote] of [[A, 'ay'], [B, 'bee'], [C, 'cee']]) {
    mkdirSync(join(r, '.git'), { recursive: true });
    writeFileSync(join(r, '.git', 'HEAD'), 'ref: refs/heads/feature/x\n');
    writeFileSync(join(r, '.git', 'config'), `[remote "origin"]\n\turl = git@github.com:x/${remote}.git\n`);
  }
  mkdirSync(join(B, 'api'), { recursive: true });
  mkdirSync(join(B, 'docs'), { recursive: true });
  mkdirSync(join(B, '.nxy', 'lenses'), { recursive: true });
  mkdirSync(join(C, '.nxy', 'lenses'), { recursive: true });
  writeFileSync(join(B, 'api', 'BillingService.java'), 'class BillingService {\n}\n');
  writeFileSync(join(B, 'docs', 'billing.md'), '# Billing\n\nEl cobro pasa por `BillingService`.\n');
  writeFileSync(join(B, '.nxy', 'lenses', 'bee.md'), '---\nname: bee\ntitle: Bee lens\npaths: **/*BillingService.java\n---\n- BEE-LENS-RULE\n');
  writeFileSync(join(C, '.nxy', 'lenses', 'cee.md'), '---\nname: cee\ntitle: Cee lens\nalways: true\n---\n- CEE-LENS-RULE\n');
  const env = { ...process.env, NXY_HOME: join(dir, 'home'), CLAUDE_PROJECT_DIR: A };
  const node = (args, input, cwd = A) => spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', ...args, '--cwd', cwd], { input, encoding: 'utf8', env, cwd });
  const hook = (file, o) => execFileSync(process.execPath, ['--disable-warning=ExperimentalWarning', join(HOOKS, file)], {
    input: JSON.stringify({ cwd: A, session_id: 's1', ...o }), encoding: 'utf8', env,
  });
  const target = fwd(join(B, 'api', 'BillingService.java'));
  const plan = [
    '## Plan', 'Goal: x', ...(declare ? ['### Repos', '- `.` — main', `- \`${fwd(B)}\` — billing`] : []),
    '### Batch 1 — api: billing', `- \`${target}\` — charge`, 'Accept: `node -v`',
  ].join('\n');
  assert.equal(node([MEM, 'handoff', 'plan'], plan).status, 0);
  node([MEM, 'save', 'Money is BigDecimal', '--type', 'convention', '--body', 'x'], undefined, B);
  node([MEM, 'save', 'Never touch the CEE ledger', '--type', 'convention', '--body', 'x'], undefined, C);
  node([MEM, 'save', 'A owns the gateway', '--type', 'convention', '--body', 'x'], undefined, A);
  const main = join(dir, 'main.jsonl');
  const q = approvalQuestion(planHash(plan));
  writeFileSync(main, [
    { type: 'assistant', message: { usage: { cache_read_input_tokens: 20_000 } } },
    { type: 'assistant', message: { content: [{ type: 'tool_use', id: 'q1', name: 'AskUserQuestion', input: { questions: [{ question: q }] } }] } },
    { type: 'user', message: { content: [{ type: 'tool_result', tool_use_id: 'q1', content: 'ok' }] }, toolUseResult: { answers: { [q]: APPROVE } } },
  ].map((l) => JSON.stringify(l)).join('\n') + '\n');
  return { dir, A, B, C, node, hook, main };
}

/** The implementer edits the file in B (the hook copies it), then the file changes. */
function editInB(s) {
  const file = join(s.B, 'api', 'BillingService.java');
  s.hook('pretooluse-edit.mjs', { tool_name: 'Edit', agent_id: 'a1', agent_type: 'nxy:implementer', tool_input: { file_path: file } });
  writeFileSync(file, 'class BillingService {\n  void charge() {}\n}\n');
}

test('a convention of B reaches the implementer and the review packet; A\'s still does', () => {
  const s = setup();
  const out = JSON.parse(s.hook('pretooluse-agent.mjs', {
    tool_name: 'Agent', transcript_path: s.main, tool_input: { subagent_type: 'nxy:implementer', prompt: 'Batch 1 — charge' },
  })).hookSpecificOutput;
  assert.equal(out.permissionDecision, 'allow');
  assert.match(out.updatedInput.prompt, /- Money is BigDecimal/, 'B\'s convention');
  assert.match(out.updatedInput.prompt, /- A owns the gateway/, 'the primary\'s convention');
  assert.doesNotMatch(out.updatedInput.prompt, /CEE ledger/, 'C was never touched');
  editInB(s);
  const packet = s.node([REVIEW, 'packet', '--full']).stdout;
  assert.match(packet, /Money is BigDecimal/);
  assert.match(packet, /A owns the gateway/);
  assert.doesNotMatch(packet, /CEE ledger/);
});

test('a lens in B\'s .nxy/lenses applies to a B file; C\'s, never touched, does not', () => {
  const s = setup();
  editInB(s);
  const packet = s.node([REVIEW, 'packet', '--full']).stdout;
  assert.match(packet, /B\/api\/BillingService\.java/);
  assert.match(packet, /BEE-LENS-RULE/);
  assert.doesNotMatch(packet, /CEE-LENS-RULE/);
});

test('a doc in B that names the changed file is found, by absolute path', () => {
  const s = setup();
  editInB(s);
  const rec = s.node([REVIEW, 'record'], '[]');
  assert.equal(rec.status, 0, rec.stderr);
  assert.ok(rec.stdout.includes(fwd(join(s.B, 'docs', 'billing.md'))), rec.stdout);
});

test('a B the plan does not declare and nobody touched changes nothing for A', () => {
  const s = setup({ declare: false });
  const file = join(s.A, 'a.js');
  writeFileSync(file, 'one\n');
  s.hook('pretooluse-edit.mjs', { tool_name: 'Edit', agent_id: 'a1', agent_type: 'nxy:implementer', tool_input: { file_path: file } });
  writeFileSync(file, 'two\nthree\n');
  const packet = s.node([REVIEW, 'packet', '--full']).stdout;
  assert.match(packet, /a\.js/);
  assert.match(packet, /A owns the gateway/);
  assert.doesNotMatch(packet, /Money is BigDecimal|BEE-LENS-RULE|CEE-LENS-RULE|CEE ledger/);
});
