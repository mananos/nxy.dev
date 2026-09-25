// @ts-check
/**
 * The plan checkpoint (0.4.0). What has to hold: a plan is shaped enough to be approved and
 * verified; nothing is written — no main-thread edit, no implementer — until the *user* approves
 * that exact plan; the approval is read from what Claude Code recorded, never from the model; and
 * saving the handoff can never quietly drop the plan. And (0.4.0b) a draft with questions for the
 * user is never approved or carried out until they are answered.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import {
  APPROVE, CHANGE, PLAN_TEMPLATE, approvalQuestion, checkpointDenyMessage, checkpointInstruction, decideCheckpoint, extractPlan,
  parseDecisions, parseQuestions, planHash, questionsInstruction, questionsPayload, validatePlan, withPlan,
} from '../core/plan.mjs';
import { subagentContextBlock, validateHandoff } from '../core/memory/handoff.mjs';
import { findApproval } from '../hosts/claude-code/plan-approval.mjs';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const HOOKS = join(ROOT, 'hosts', 'claude-code', 'hooks');
const MEM = join(ROOT, 'hosts', 'claude-code', 'entries', 'mem.mjs');

const PLAN = [
  '## Plan',
  'Goal: corporate customers can be created',
  '### Batch 1 — api: validation',
  '- `src/main/java/app/ClienteService.java:88` — call CuitValidator before persisting',
  'Accept: `./mvnw -q test -Dtest=ClienteServiceTest`',
  '### Batch 2 — api: endpoint',
  '- `src/main/java/app/ClienteController.java` — POST /clientes/corporativos',
  'Accept: `./mvnw -q test -Dtest=ClienteControllerTest#creates201`',
].join('\n');
const HANDOFF = 'route: implementer\n## Done\n- nothing\n## Next\n- run the plan';

test('plan: extract, hash, validate, merge', () => {
  const body = withPlan(HANDOFF, PLAN);
  assert.equal(extractPlan(body), PLAN);
  assert.ok(body.startsWith('route: implementer'), 'the handoff sections stay first');
  assert.equal(extractPlan(withPlan(body, PLAN.replace('201', '202')))?.includes('202'), true, 'a new plan replaces the old one');
  assert.equal(withPlan(body, PLAN).split('## Plan').length, 2, 'never two plans');
  assert.equal(extractPlan(HANDOFF), null);

  assert.equal(planHash(PLAN), planHash(PLAN.replace(/\n/g, '\r\n  ')), 'line endings and indentation do not change the plan');
  assert.notEqual(planHash(PLAN), planHash(PLAN.replace('201', '202')), 'any real change does');

  assert.deepEqual(validatePlan(PLAN), { ok: true, errors: [], batches: 2, questions: 0 });
  assert.equal(validatePlan(PLAN_TEMPLATE).ok, true, 'the template itself passes');
  assert.match(validatePlan('## Plan\nGoal: x\nwe will refactor things').errors.join(), /no batch/);
  assert.match(validatePlan(PLAN.replace('Accept: `./mvnw -q test -Dtest=ClienteControllerTest#creates201`', '')).errors.join(), /batch 2 has no "Accept:"/);
  assert.match(validatePlan(PLAN.replace('`./mvnw -q test -Dtest=ClienteControllerTest#creates201`', 'curl returns 201')).errors.join(),
    /batch 2: "Accept:" needs the command that proves it/, 'prose cannot be verified');
  assert.equal(validatePlan(PLAN.replace('`./mvnw -q test -Dtest=ClienteControllerTest#creates201`', 'manual — the form shows the CUIT field')).ok, true,
    'what no command can prove is said explicitly');
  assert.match(validatePlan('## Plan\n### Batch 1\n- the service\nAccept: `npm test`').errors.join(), /no file path/);

  assert.deepEqual(decideCheckpoint({ isSubagent: false, planHash: null, approved: false }), { block: false, reason: 'no-plan' });
  assert.deepEqual(decideCheckpoint({ isSubagent: false, planHash: 'abc', approved: false }), { block: true, reason: 'plan-pending' });
  assert.deepEqual(decideCheckpoint({ isSubagent: false, planHash: 'abc', approved: true }), { block: false, reason: 'approved' });
  assert.deepEqual(decideCheckpoint({ isSubagent: true, planHash: 'abc', approved: false }), { block: false, reason: 'subagent' });
  assert.match(checkpointDenyMessage('abc12345'), /AskUserQuestion[\s\S]*"Approve nxy plan abc12345\?"[\s\S]*"Approve"[\s\S]*"Change"/);
});

test('handoff and plan together: length warning ignores the plan; the implementer always gets it', () => {
  const long = `${HANDOFF}\n${'- more\n'.repeat(10)}`;
  const withBigPlan = withPlan(long, `${PLAN}\n${'- `src/a.ts` — x\n'.repeat(30)}`);
  assert.deepEqual(validateHandoff(withBigPlan).warnings, [], 'a long plan does not make the handoff "too long"');

  const huge = withPlan(`${HANDOFF}\n${'- filler\n'.repeat(100)}`, PLAN);
  const block = subagentContextBlock(/** @type {any} */ ({ title: 'handoff: x', body: huge, updated: Date.now() }), 'CMD');
  assert.match(block, /more lines: CMD/, 'the rest is cut');
  assert.ok(block.includes(PLAN), 'the plan never is');
});

/** A main transcript with AskUserQuestion exchanges. */
function transcript(dir, exchanges) {
  const lines = [];
  exchanges.forEach(({ hash, answer, sidechain = false }, i) => {
    const q = approvalQuestion(hash);
    lines.push({ type: 'assistant', isSidechain: sidechain, message: { content: [{ type: 'tool_use', id: `tu${i}`, name: 'AskUserQuestion', input: { questions: [{ question: q, header: 'Plan', options: [{ label: APPROVE }, { label: CHANGE }] }] } }] } });
    lines.push({ type: 'user', isSidechain: sidechain, message: { content: [{ type: 'tool_result', tool_use_id: `tu${i}`, content: `User answered: ${answer}` }] }, toolUseResult: { answers: { [q]: answer } } });
  });
  const p = join(dir, `t${Math.random().toString(36).slice(2)}.jsonl`);
  writeFileSync(p, lines.map((l) => JSON.stringify(l)).join('\n') + '\n');
  return p;
}

test('findApproval: the user said Approve to this exact hash, in the main thread', () => {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-plan-'));
  assert.equal(findApproval(transcript(dir, [{ hash: 'aaaa1111', answer: APPROVE }]), 'aaaa1111'), true);
  assert.equal(findApproval(transcript(dir, [{ hash: 'aaaa1111', answer: CHANGE }]), 'aaaa1111'), false);
  assert.equal(findApproval(transcript(dir, [{ hash: 'aaaa1111', answer: 'ok but use Lombok' }]), 'aaaa1111'), false, 'free text is not approval');
  assert.equal(findApproval(transcript(dir, [{ hash: 'bbbb2222', answer: APPROVE }]), 'aaaa1111'), false, 'another plan');
  assert.equal(findApproval(transcript(dir, [{ hash: 'aaaa1111', answer: APPROVE, sidechain: true }]), 'aaaa1111'), false, 'a subagent cannot approve');
  assert.equal(findApproval(transcript(dir, [{ hash: 'aaaa1111', answer: APPROVE }, { hash: 'aaaa1111', answer: CHANGE }]), 'aaaa1111'), false, 'the latest answer wins');
  assert.equal(findApproval(join(dir, 'missing.jsonl'), 'aaaa1111'), false);
});

/** A repo on a branch, a home, and helpers to run the CLI and hooks. */
function sandbox(contextTokens = 20_000) {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-plan-hook-'));
  const repo = join(dir, 'repo');
  mkdirSync(join(repo, '.git'), { recursive: true });
  writeFileSync(join(repo, '.git', 'HEAD'), 'ref: refs/heads/feature/x\n');
  writeFileSync(join(repo, '.git', 'config'), '[remote "origin"]\n\turl = git@github.com:x/y.git\n');
  const env = { ...process.env, NXY_HOME: join(dir, 'home'), CLAUDE_PROJECT_DIR: repo };
  const usage = { type: 'assistant', message: { usage: { cache_read_input_tokens: contextTokens } } };
  const main = join(dir, 'main.jsonl');
  writeFileSync(main, JSON.stringify(usage) + '\n');
  /** @param {string[]} args @param {string} [input] */
  const mem = (args, input) => spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', MEM, ...args, '--cwd', repo], { input, encoding: 'utf8', env });
  /** @param {string} hook @param {object} payload @param {string} [t] */
  const hook = (hook, payload, t = main) => execFileSync(process.execPath, ['--disable-warning=ExperimentalWarning', join(HOOKS, hook)], {
    input: JSON.stringify({ cwd: repo, transcript_path: t, session_id: 's1', ...payload }), encoding: 'utf8', env,
  });
  return { dir, repo, main, mem, hook, usage };
}

test('CLI: plan is validated, saved into the handoff, and survives a handoff save', () => {
  const s = sandbox();
  const bad = s.mem(['handoff', 'plan'], '## Plan\nwe will do things');
  assert.equal(bad.status, 1);
  assert.match(bad.stdout, /plan not saved: .*no batch/);

  const ok = s.mem(['handoff', 'plan'], PLAN);
  const hash = planHash(PLAN);
  assert.equal(ok.status, 0);
  assert.match(ok.stdout, new RegExp(`plan ${hash} saved in the handoff of \`feature/x\` \\(2 batches\\)`));
  assert.match(ok.stdout, /Next \(main thread\): Checkpoint: .*AskUserQuestion/);
  assert.ok(existsSync(join(s.repo, '.nxy', 'local', 'plan.json')));

  const saved = s.mem(['handoff', 'save'], 'route: implementer\n## Done\n- batch 1\n## Next\n- batch 2');
  assert.match(saved.stdout, new RegExp(`plan ${hash} kept`), 'saving the handoff cannot drop the checkpoint');
  assert.match(s.mem(['handoff', 'show']).stdout, new RegExp(`plan ${hash}[\\s\\S]*### Batch 2`));

  s.mem(['handoff', 'done']);
  assert.ok(!existsSync(join(s.repo, '.nxy', 'local', 'plan.json')), 'done clears the pending plan');
});

test('hooks: a pending plan stops edits and the implementer until the user approves it', () => {
  const s = sandbox();
  s.mem(['handoff', 'plan'], PLAN);
  const hash = planHash(PLAN);
  const reason = (out) => JSON.parse(out).hookSpecificOutput.permissionDecisionReason;
  const edit = (t) => s.hook('pretooluse-edit.mjs', { tool_name: 'Edit', tool_input: { file_path: 'src/a.ts' } }, t);
  const dispatch = (t) => s.hook('pretooluse-agent.mjs', { tool_name: 'Agent', tool_input: { subagent_type: 'nxy:implementer', prompt: 'Batch 1' } }, t);

  assert.match(reason(edit()), new RegExp(`plan \\(${hash}\\) has not been approved`), 'even in a cheap session: the checkpoint is not about cost');
  assert.match(reason(dispatch()), /has not been approved/);
  assert.equal(s.hook('pretooluse-edit.mjs', { tool_name: 'Edit', agent_id: 'a1', tool_input: { file_path: 'src/a.ts' } }).trim(), '',
    'a subagent is never the one asking for approval');

  const changed = transcript(s.dir, [{ hash, answer: CHANGE }]);
  writeFileSync(changed, `${JSON.stringify(s.usage)}\n`, { flag: 'a' });
  assert.match(reason(edit(changed)), /has not been approved/, '"Change" is not approval');

  const approved = transcript(s.dir, [{ hash, answer: APPROVE }]);
  writeFileSync(approved, `${JSON.stringify(s.usage)}\n`, { flag: 'a' });
  assert.match(reason(edit(approved)), /plan .* is approved, so its work goes through implementers[\s\S]*batches: 1, 2/,
    'approved: the main thread still does not write — each batch goes to a verified implementer (0.4.4)');
  const out = JSON.parse(dispatch(approved)).hookSpecificOutput;
  assert.equal(out.permissionDecision, 'allow');
  assert.ok(out.updatedInput.prompt.includes('### Batch 1'), 'and the implementer receives the approved plan');

  s.mem(['handoff', 'plan'], PLAN.replace('201', '202'));
  assert.match(reason(edit(approved)), /has not been approved/, 'a changed plan needs its own approval');
});

/** A draft: the same plan, with one decision left to the user. */
const DRAFT = `${PLAN}
### Questions
- Q: New endpoint or extend POST /clientes?
  - Extend POST /clientes (recommended) — one endpoint with a tipo field
  - New POST /clientes/corporativos — separate DTO and validation`;

/** The draft finished with the user's answer. */
const FINAL = PLAN.replace('Goal: corporate customers can be created',
  'Goal: corporate customers can be created\nDecisions:\n- New endpoint for corporate customers — new endpoint or extend POST /clientes?');

/** @param {string} text the JSON input line of an instruction */
const inputOf = (text) => JSON.parse(/^\s+(\{.*\})$/m.exec(text)?.[1] || '{}');

test('questions: parsed for AskUserQuestion, validated, and turned into decisions', () => {
  assert.deepEqual(parseQuestions(DRAFT), [{
    question: 'New endpoint or extend POST /clientes?',
    options: [
      { label: 'Extend POST /clientes', description: 'one endpoint with a tipo field', recommended: true },
      { label: 'New POST /clientes/corporativos', description: 'separate DTO and validation', recommended: false },
    ],
  }]);
  assert.deepEqual(parseQuestions(PLAN), []);
  assert.deepEqual(validatePlan(DRAFT), { ok: true, errors: [], batches: 2, questions: 1 }, 'Questions is not a batch');

  assert.match(validatePlan(DRAFT.replace(/\n {2}- New POST.*$/m, '')).errors.join(), /question 1 has 1 option: it needs 2 to 4/);
  assert.match(validatePlan(DRAFT.replace(' (recommended)', '')).errors.join(), /exactly one option "\(recommended\)" and list it first/);
  assert.match(validatePlan(DRAFT.replace(' (recommended)', '').replace('corporativos —', 'corporativos (recommended) —')).errors.join(), /list it first/);

  const [call] = questionsPayload(parseQuestions(DRAFT));
  assert.equal(call.questions[0].header, 'Plan Q1');
  assert.equal(call.questions[0].options[0].label, 'Extend POST /clientes (Recommended)', 'the host convention for the recommended option');
  const five = Array.from({ length: 5 }, (_, i) => ({
    question: `q${i}?`, options: [{ label: 'a', description: '', recommended: true }, { label: 'b', description: '', recommended: false }],
  }));
  assert.deepEqual(questionsPayload(five).map((c) => c.questions.length), [4, 1], 'at most 4 questions per call');

  assert.deepEqual(parseDecisions(FINAL), [{ rule: 'New endpoint for corporate customers', why: 'new endpoint or extend POST /clientes?' }]);
  assert.deepEqual(parseDecisions(PLAN), []);
  assert.notEqual(planHash(DRAFT), planHash(FINAL), 'the answered plan is another plan');

  assert.deepEqual(decideCheckpoint({ isSubagent: false, planHash: 'abc', questions: 1, approved: true }), { block: true, reason: 'plan-questions' },
    'an approval of a plan with open questions does not count');
  const ask = questionsInstruction('abc12345', parseQuestions(DRAFT));
  assert.match(ask, /Do not show the draft or ask for approval/);
  assert.equal(inputOf(ask).questions[0].header, 'Plan Q1');
  assert.match(ask, /nxy:planner .*"Finish nxy plan abc12345/);
  assert.doesNotMatch(ask, /Approve nxy plan/);
});

test('checkpoint offers the decisions as conventions, in the same AskUserQuestion call', () => {
  assert.doesNotMatch(checkpointInstruction('abc12345'), /Convention/, 'no decisions, no offer');
  const text = checkpointInstruction('abc12345', { offers: parseDecisions(FINAL), saveCmd: 'SAVE' });
  const input = inputOf(text);
  assert.equal(input.questions[0].question, approvalQuestion('abc12345'), 'approval stays the first question');
  assert.deepEqual(input.questions[1].options, [{ label: 'New endpoint for corporate customers', description: 'new endpoint or extend POST /clientes?' }]);
  assert.equal(input.questions[1].multiSelect, true);
  assert.match(text, /SAVE "<rule>" --type convention/);
  const many = Array.from({ length: 20 }, (_, i) => ({ rule: `r${i}`, why: '' }));
  assert.equal(inputOf(checkpointInstruction('x', { offers: many })).questions.length, 4, 'never more than AskUserQuestion takes');
});

test('flow: draft with questions → answers → final plan → approval; known conventions are not offered again', () => {
  const s = sandbox();
  const reason = (out) => JSON.parse(out).hookSpecificOutput.permissionDecisionReason;
  const edit = (t) => s.hook('pretooluse-edit.mjs', { tool_name: 'Edit', tool_input: { file_path: 'src/a.ts' } }, t);
  const dispatch = (t) => s.hook('pretooluse-agent.mjs', { tool_name: 'Agent', tool_input: { subagent_type: 'nxy:implementer', prompt: 'Batch 1' } }, t);

  const draft = s.mem(['handoff', 'plan'], DRAFT);
  const draftHash = planHash(DRAFT);
  assert.equal(draft.status, 0);
  assert.match(draft.stdout, /\(2 batches, 1 open question\)/);
  assert.match(draft.stdout, /Next \(main thread\): Questions first/);
  assert.doesNotMatch(draft.stdout, /Approve nxy plan/, 'no approval question for a draft');

  // Even a transcript that "approves" the draft unlocks nothing.
  const approvedDraft = transcript(s.dir, [{ hash: draftHash, answer: APPROVE }]);
  writeFileSync(approvedDraft, `${JSON.stringify(s.usage)}\n`, { flag: 'a' });
  assert.match(reason(edit(approvedDraft)), /1 open question[\s\S]*handoff next/);
  assert.match(reason(dispatch(approvedDraft)), /1 open question/);
  assert.equal(s.hook('pretooluse-agent.mjs', { tool_name: 'Agent', tool_input: { subagent_type: 'nxy:planner', prompt: `Finish nxy plan ${draftHash}` } }).trim(), '',
    'the planner is dispatched freely to finish its draft');
  assert.match(s.mem(['handoff', 'next']).stdout, /Questions first[\s\S]*"Plan Q1"/, 'next reprints what the draft needs');

  const final = s.mem(['handoff', 'plan'], FINAL);
  const hash = planHash(FINAL);
  assert.match(final.stdout, /Checkpoint:[\s\S]*Approve nxy plan[\s\S]*"header":"Convention"[\s\S]*"New endpoint for corporate customers"/);
  assert.match(reason(edit()), new RegExp(`plan \\(${hash}\\) has not been approved`));
  const approved = transcript(s.dir, [{ hash, answer: APPROVE }]);
  writeFileSync(approved, `${JSON.stringify(s.usage)}\n`, { flag: 'a' });
  assert.notEqual(dispatch(approved)?.length, 0);
  assert.equal(JSON.parse(dispatch(approved)).hookSpecificOutput.permissionDecision, 'allow', 'the final plan, approved: its batches go');

  s.mem(['save', 'New endpoint for corporate customers', '--type', 'convention', '--body', 'x']);
  assert.match(s.mem(['list', '--type', 'convention']).stdout, /convention[\s\S]*New endpoint for corporate customers/);
  assert.doesNotMatch(s.mem(['handoff', 'next']).stdout, /Convention/, 'a decision already kept as a convention is not offered again');
});
