// @ts-check
/**
 * Conventions, lenses and docs from every touched repo (1.0.1): a convention saved for B reaches the
 * implementer and the reviewer, a lens in B's `.nxy/lenses` applies to B's files, a doc in B that names
 * the changed file is found, and a repo the task never touched changes nothing. Temp dirs only.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { APPROVE, approvalQuestion, planForRepo, planHash } from '../core/plan.mjs';
import { afterBatch, reviewInstruction } from '../core/batch-status.mjs';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const HOOKS = join(ROOT, 'hosts', 'claude-code', 'hooks');
const MEM = join(ROOT, 'hosts', 'claude-code', 'entries', 'mem.mjs');
const REVIEW = join(ROOT, 'hosts', 'claude-code', 'entries', 'review.mjs');
const fwd = (p) => p.replace(/\\/g, '/');

/** A (the project) and B (declared when `declare`), both fake git repos with their own remote; C never touched. */
function setup({ declare = true, planOf = /** @type {((b: string) => string) | null} */ (null), prep = /** @type {((b: string) => void) | null} */ (null) } = {}) {
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
  if (prep) prep(B);
  const env = { ...process.env, NXY_HOME: join(dir, 'home'), CLAUDE_PROJECT_DIR: A };
  const node = (args, input, cwd = A) => spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', ...args, '--cwd', cwd], { input, encoding: 'utf8', env, cwd });
  const hook = (file, o) => execFileSync(process.execPath, ['--disable-warning=ExperimentalWarning', join(HOOKS, file)], {
    input: JSON.stringify({ cwd: A, session_id: 's1', ...o }), encoding: 'utf8', env,
  });
  const target = fwd(join(B, 'api', 'BillingService.java'));
  const plan = planOf ? planOf(B) : [
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
  return { dir, A, B, C, node, hook, main, plan };
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

/** Two repos with a batch each (marker titles), a file edited in each, and the plan swapped in. */
function twoRepoReview() {
  const s = setup({
    planOf: (B) => [
      '## Plan', 'Goal: GOAL-MARK', '### Repos', '- `.` — main', `- \`${fwd(B)}\` — billing`,
      '### Batch 1 — ALPHA-TITLE', '- `a.js` — alpha', 'Accept: `node -v`',
      '### Batch 2 — BRAVO-TITLE', `- \`${fwd(join(B, 'api', 'BillingService.java'))}\` — charge`, 'Accept: `node -v`',
      'Risks: RISK-MARK',
    ].join('\n'),
  });
  mkdirSync(join(s.A, '.nxy', 'lenses'), { recursive: true });
  writeFileSync(join(s.A, '.nxy', 'lenses', 'ay.md'), '---\nname: ay\ntitle: Ay lens\nalways: true\n---\n- AY-LENS-RULE\n');
  const file = join(s.A, 'a.js');
  writeFileSync(file, 'one\n');
  s.hook('pretooluse-edit.mjs', { tool_name: 'Edit', agent_id: 'a1', agent_type: 'nxy:implementer', tool_input: { file_path: file } });
  writeFileSync(file, 'two\nthree\n');
  editInB(s);
  const hash = planHash(s.plan);
  const dispatch = (repo) => s.hook('pretooluse-agent.mjs', {
    tool_name: 'Agent', transcript_path: s.main,
    tool_input: { subagent_type: 'nxy:reviewer', prompt: `Review nxy plan ${hash} (project: ${fwd(s.A)}) (repo: ${fwd(repo)})` },
  });
  return { ...s, hash, dispatch };
}

test('one reviewer per repo: parts print no ids, the last one prints checkpoint 2 with unique ids', () => {
  const s = twoRepoReview();
  s.dispatch(s.A);
  s.dispatch(s.B);
  const fa = JSON.stringify([{ file: 'a.js', line: 2, severity: 'high', title: 'A finding', fix: 'x' }]);
  const fb = JSON.stringify([{ file: 'api/BillingService.java', line: 2, severity: 'high', title: 'B finding', fix: 'y' }]);
  const first = s.node([REVIEW, 'record', '--repo', s.A], fa);
  assert.equal(first.status, 0, first.stderr);
  assert.match(first.stdout, /\(1 of 2\)/);
  assert.doesNotMatch(first.stdout, /\bR\d+\b/);
  const second = s.node([REVIEW, 'record', '--repo', s.B], fb);
  assert.equal(second.status, 0, second.stderr);
  assert.match(second.stdout, /Checkpoint 2/);
  assert.match(second.stdout, /\bR1\b.*finding/);
  assert.match(second.stdout, /\bR2\b.*finding/);
  assert.doesNotMatch(second.stdout, /\bR3\b/);
});

test('the packet of repo A has the goal, its batch, convention and lens, and nothing of B', () => {
  const s = twoRepoReview();
  const packet = s.node([REVIEW, 'packet', '--full', '--repo', s.A]).stdout;
  assert.match(packet, /GOAL-MARK/);
  assert.match(packet, /ALPHA-TITLE/);
  assert.match(packet, /A owns the gateway/);
  assert.match(packet, /AY-LENS-RULE/);
  assert.match(packet, /record --cwd .* --repo /);
  assert.doesNotMatch(packet, /BRAVO-TITLE|BillingService|Money is BigDecimal|BEE-LENS-RULE|CEE|RISK-MARK/);
  const b = s.node([REVIEW, 'packet', '--full', '--repo', s.B]).stdout;
  assert.match(b, /BRAVO-TITLE/);
  assert.match(b, /Money is BigDecimal/);
  assert.doesNotMatch(b, /ALPHA-TITLE|A owns the gateway|AY-LENS-RULE|a\.js/);
});

test('the packet of repo A carries no Not reviewed or Coverage note of B', () => {
  const s = setup({
    planOf: (B) => [
      '## Plan', 'Goal: GOAL-MARK', '### Repos', '- `.` — main', `- \`${fwd(B)}\` — billing`,
      '### Batch 1 — ALPHA-TITLE', '- `a.js` — alpha', 'Accept: `node -v`',
      '### Batch 2 — BRAVO-TITLE', `- \`${fwd(join(B, 'api', 'BillingService.java'))}\` — charge`, 'Accept: `node -v`',
    ].join('\n'),
    // B is a real git repo with a dirty file nothing copied, before the plan is approved.
    prep: (B) => {
      rmSync(join(B, '.git'), { recursive: true, force: true });
      const git = (...a) => execFileSync('git', ['-c', 'user.name=t', '-c', 'user.email=t@t', ...a], { cwd: B, stdio: 'ignore' });
      git('init', '-q');
      git('add', '-A');
      git('commit', '-q', '-m', 'init');
      writeFileSync(join(B, 'DIRTY-B-MARK.txt'), 'x\n');
    },
  });
  const file = join(s.A, 'a.js');
  writeFileSync(file, 'one\n');
  s.hook('pretooluse-edit.mjs', { tool_name: 'Edit', agent_id: 'a1', agent_type: 'nxy:implementer', tool_input: { file_path: file } });
  writeFileSync(file, 'two\nthree\n');
  const b = s.node([REVIEW, 'packet', '--full', '--repo', s.B]).stdout;
  const all = s.node([REVIEW, 'packet', '--full']).stdout;
  assert.match(all, /DIRTY-B-MARK/, 'the unfiltered packet lists it, so the filter is what is tested');
  assert.match(b, /DIRTY-B-MARK/, 'B\'s own note stays');
  const a = s.node([REVIEW, 'packet', '--full', '--repo', s.A]).stdout;
  assert.doesNotMatch(a, /DIRTY-B-MARK|## Not reviewed/);
});

test('planForRepo keeps the goal and only the batches of that repo', () => {
  const plan = [
    '## Plan', 'Goal: G', 'Decisions:', '- [task] D-MARK', '### Repos', '- `/w/a` — a', '- `/w/b` — b',
    '### Batch 1 — one', '- `src/x.js` — x', 'Accept: `npm test`',
    '### Batch 2 — two', '- `/w/b/y.js` — y', 'Accept: `make`', 'Risks: R-MARK',
  ].join('\n');
  const a = planForRepo(plan, '/w/a', ['/w/a', '/w/b']);
  assert.match(a, /^Goal: G/);
  assert.match(a, /### Batch 1 — one[\s\S]*Accept: `npm test`/);
  assert.doesNotMatch(a, /two|D-MARK|R-MARK|Repos/);
  const b = planForRepo(plan, '/w/b', ['/w/a', '/w/b']);
  assert.match(b, /### Batch 2 — two[\s\S]*Accept: `make`/);
  assert.doesNotMatch(b, /one|R-MARK|D-MARK/);
});

test('afterBatch with two repos lists one tester per repo, all in one message', () => {
  const out = afterBatch({
    hash: 'abc123', n: 2, command: 'npm test', verdict: { status: 'pass' }, batches: [1, 2], recorded: { 1: { status: 'pass' } },
    repos: [{ label: 'A', root: '/w/a' }, { label: 'B', root: '/w/b' }],
  });
  assert.match(out, /one nxy:tester per repo, all in one message/);
  assert.ok(out.includes('Full suite for nxy plan abc123 (repo: /w/a)'));
  assert.ok(out.includes('Full suite for nxy plan abc123 (repo: /w/b)'));
  const rev = reviewInstruction({ hash: 'abc123', project: '/w/a', needed: true, repos: [{ label: 'A', root: '/w/a' }, { label: 'B', root: '/w/b' }] });
  assert.match(rev, /one nxy:reviewer per repo, all in one message/);
  assert.ok(rev.includes('Review nxy plan abc123 (project: /w/a) (repo: /w/b)'));
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
