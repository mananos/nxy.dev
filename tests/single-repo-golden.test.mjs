// @ts-check
/**
 * What a single-repo user sees, pinned as it was in 1.2.0 (before tester and reviewer went per repo):
 * the after-batch / review instructions, the tester and reviewer prompts, the suite state, and the
 * review packet and record without `--repo`. Later changes must leave every string here untouched.
 * Temp dirs only; paths and clock-dependent values are normalized.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, readFileSync, realpathSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { afterBatch, reviewInstruction } from '../core/batch-status.mjs';
import { reviewerPrompt, testerPrompt } from '../core/orchestrator.mjs';
import { readSuite, recordSuite } from '../hosts/claude-code/verify-state.mjs';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const HOOKS = join(ROOT, 'hosts', 'claude-code', 'hooks');
const MEM = join(ROOT, 'hosts', 'claude-code', 'entries', 'mem.mjs');
const REVIEW = join(ROOT, 'hosts', 'claude-code', 'entries', 'review.mjs');
const fwd = (p) => p.replace(/\\/g, '/');

test('afterBatch: the last batch points at one tester; a middle batch says only that it passed', () => {
  assert.equal(
    afterBatch({ hash: 'abc123', n: 2, command: 'npm test', verdict: { status: 'pass' }, batches: [1, 2], recorded: { 1: { status: 'pass' } } }),
    'nxy verify: batch 2 ✔ `npm test` passed.\nAll 2 batches of plan abc123 verified. Next: dispatch the nxy:tester subagent once with "Full suite for nxy plan abc123" — it runs the suite of every repo the plan touched.',
  );
  assert.equal(
    afterBatch({ hash: 'abc123', n: 1, command: 'npm test', verdict: { status: 'pass' }, batches: [1, 2], recorded: {} }),
    'nxy verify: batch 1 ✔ `npm test` passed.',
  );
});

test('reviewInstruction, testerPrompt and reviewerPrompt', () => {
  assert.equal(
    reviewInstruction({ hash: 'abc123', project: 'C:\\x\\proj', needed: true }),
    'nxy: full suite done. Next: dispatch the nxy:reviewer subagent once with "Review nxy plan abc123 (project: C:/x/proj)" — it reviews the plan\'s diff and returns the findings for checkpoint 2.',
  );
  assert.equal(
    reviewInstruction({ hash: 'abc123', project: 'p', needed: false, reason: 'only docs changed' }),
    'nxy: full suite done. No review needed for plan abc123: only docs changed.',
  );
  assert.equal(testerPrompt('abc123'), 'Full suite for nxy plan abc123');
  assert.equal(reviewerPrompt('abc123', 'C:\\x\\proj'), 'Review nxy plan abc123 (project: C:/x/proj)');
});

test('readSuite on suite.json: running, done, red', () => {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-golden-'));
  const cwd = join(dir, 'repo');
  mkdirSync(cwd, { recursive: true });
  assert.equal(readSuite(cwd, 'abc123'), null);
  const red = [{ command: 'npm test', error: 'exit 1' }];
  for (const rec of [
    { status: 'running', ts: 1 },
    { status: 'done', ts: 2, ran: ['npm test'] },
    { status: 'done', ts: 3, red, ran: ['npm test'] },
  ]) {
    recordSuite(cwd, 'abc123', /** @type {any} */ (rec));
    assert.deepEqual(readSuite(cwd, 'abc123'), rec);
  }
});

/** One repo, one approved plan, one edited file. */
function setup() {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-golden-'));
  const repo = join(dir, 'repo');
  mkdirSync(join(repo, '.git'), { recursive: true });
  writeFileSync(join(repo, '.git', 'HEAD'), 'ref: refs/heads/feature/x\n');
  writeFileSync(join(repo, '.git', 'config'), '[remote "origin"]\n\turl = git@github.com:x/golden.git\n');
  const env = { ...process.env, NXY_HOME: join(dir, 'home'), CLAUDE_PROJECT_DIR: repo };
  const node = (args, input) => spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', ...args, '--cwd', repo], { input, encoding: 'utf8', env, cwd: repo });
  const plan = ['## Plan', 'Goal: golden', '### Batch 1 — change a', '- `a.js` — edit', 'Accept: `node -v`'].join('\n');
  assert.equal(node([MEM, 'handoff', 'plan'], plan).status, 0);
  node([MEM, 'save', 'Golden convention', '--type', 'convention', '--body', 'x']);
  const file = join(repo, 'a.js');
  writeFileSync(file, 'one\n');
  execFileSync(process.execPath, ['--disable-warning=ExperimentalWarning', join(HOOKS, 'pretooluse-edit.mjs')], {
    input: JSON.stringify({ cwd: repo, session_id: 's1', tool_name: 'Edit', agent_id: 'a1', agent_type: 'nxy:implementer', tool_input: { file_path: file } }), encoding: 'utf8', env,
  });
  writeFileSync(file, 'two\nthree\n');
  const roots = [...new Set([dir, realpathSync(dir)].map(fwd))];
  const norm = (s) => roots.reduce((t, r) => t.split(r).join('<TMP>'), fwd(s).split(fwd(ROOT)).join('<NXY>'))
    .replace(/\r\n/g, '\n')
    .replace(/\/baseline\/[^/]+\//g, '/baseline/<KEY>/')
    .replace(/\(\d+ lines, \d+ KB\)/g, '(<N> lines, <K> KB)');
  return { repo, node, norm };
}

const FINDINGS = JSON.stringify([{ file: 'a.js', line: 2, lens: 'base', severity: 'high', title: 'Golden finding', evidence: 'why', fix: 'do it' }]);

const ANSWER = [
  '## Your answer',
  'A JSON array, one object per finding, [] when there is none:',
  '  {"lens": "<lens name>", "severity": "high|medium|low", "file": "<path as listed above>", "line": <line in the file as it is now>, "title": "<a few words>", "evidence": "<what you saw>", "fix": "<the change, one line>"}',
  'Record it — nxy decides which findings are the change\'s — with:',
  'node --disable-warning=ExperimentalWarning "<NXY>/hosts/claude-code/entries/review.mjs" record --cwd "<TMP>/repo" <<\'EOF\'',
  '[ ... ]',
  'EOF',
];
const GIT_NOTE = 'Git: git not available; only the files nxy copied before the plan touched them were reviewed.';

const GOLDEN_PACKET = [
  '# nxy review packet · plan 4851c8c6',
  '',
  'Saved at <TMP>/repo/.nxy/local/baseline/<KEY>/review-packet.md (<N> lines, <K> KB).',
  '',
  'Index of <TMP>/repo/.nxy/local/baseline/<KEY>/review-packet.md (41 lines):',
  '  line 3, 1 lines: ## The plan the user approved',
  '  line 4, 5 lines: ## Plan',
  '  line 10, 9 lines: ## Lenses (what to look for)',
  '  line 20, 2 lines: ## Repo conventions (the user\'s decisions: a violation is a finding)',
  '  line 23, 2 lines: ## Changed files',
  '  line 26, 1 lines: ## Diff (against each file as it was before the plan\'s first edit)',
  '  line 27, 5 lines: a.js',
  '  line 33, 9 lines: ## Your answer',
  'Read it with Read offset/limit, at most 300 lines per part, code first, tests last. Never print it through Bash.',
  '',
  GIT_NOTE,
  '',
  ...ANSWER,
  '',
].join('\n');

const GOLDEN_FULL = [
  '# nxy review packet · plan 4851c8c6',
  '',
  '## The plan the user approved',
  '## Plan',
  'Goal: golden',
  '### Batch 1 — change a',
  '- `a.js` — edit',
  'Accept: `node -v`',
  '',
  '## Lenses (what to look for)',
  '### Base — always (lens: base)',
  '- **Scope**: every change serves the plan\'s goal and batches. Code the plan did not ask for (extra endpoints, refactors "while there", new config) is a finding.',
  '- **Acceptance**: each batch\'s `Accept:` test actually exercises the behaviour it names — not a test that passes whatever the code does.',
  '- **Reuse**: nothing re-implements a DTO, service, helper or component that already exists in the repo.',
  '- **Conventions**: the repo\'s conventions listed in this packet hold, and so does the style of the surrounding code (naming, layering, error handling, comment density).',
  '- **Tests**: new behaviour and each fixed bug have a test; edge cases the change introduces (empty, null, limits) are covered or named.',
  '- **Design**: SOLID/KISS/DRY where it matters — one responsibility per class/function, no speculative abstraction, no copy-pasted blocks.',
  '- **Leftovers**: no debug output, commented-out code, TODOs without an owner, or unused imports and parameters.',
  '',
  '## Repo conventions (the user\'s decisions: a violation is a finding)',
  '- Golden convention: x',
  '',
  '## Changed files',
  '- a.js (new, +2 −0)',
  '',
  '## Diff (against each file as it was before the plan\'s first edit)',
  '--- /dev/null',
  '+++ b/a.js',
  '@@ -1,0 +1,2 @@',
  '+two',
  '+three',
  '',
  ...ANSWER,
  '',
  GIT_NOTE,
  '',
].join('\n');

const GOLDEN_RECORD = [
  'nxy review <ID> of plan 4851c8c6 · 1 file, +2 −0',
  '1 finding from the change (1 high, 0 medium, 0 low), 0 preexisting.',
  '',
  'R1 high [base] a.js:2 — Golden finding',
  '    fix: do it',
  '',
  'Checkpoint 2: show the user the findings above and ask with AskUserQuestion, with exactly this input:',
  '  {"questions":[{"question":"Fix which findings of nxy review <ID>?","header":"Review","multiSelect":true,"options":[{"label":"R1 Golden finding","description":"high · base · a.js:2"},{"label":"None","description":"leave it as is"}]}]}',
  'For each finding the user picks, dispatch nxy:implementer with its line below (its batch\'s Accept is verified again):',
  '  R1: "Batch 1 — fix R1 of review <ID>: Golden finding at a.js:2. do it" — Accept (batch 1): `node -v`, exactly as written after the last edit; a new test is run separately, before it.',
  'One round only: no second review. What is left after it is the user\'s call.',
  '',
  'Conventions from the chat: look back over this task\'s conversation for standing rules the user stated (corrections the user said should always apply; "siempre / nunca / prefiero / no me gusta / de ahora en adelante"; "always / never / I prefer / from now on").',
  'Keep only rules a future task in this repo should follow, and only if the user said it should outlast this task: not personal style (that stays in the native Claude Code memory), not this task\'s own decisions, not the plan\'s `Decisions:`.',
  'None found: ask nothing and say nothing about it.',
  'Otherwise ask ONE AskUserQuestion question, with {"question":"Keep as repo conventions?","header":"Conventions","multiSelect":true} and 1-4 options (label = the rule in a few words, description = the rule as the user said it); if you are also asking another question in this step, put it in that same AskUserQuestion call.',
  'For each rule the user picks, run: node --disable-warning=ExperimentalWarning "<NXY>/hosts/claude-code/entries/mem.mjs" save "<label>" --type convention --body "<the rule as the user said it>"',
  '',
].join('\n');

test('review packet and record without --repo', () => {
  const s = setup();
  const packet = s.node([REVIEW, 'packet']);
  assert.equal(packet.status, 0, packet.stderr);
  assert.equal(s.norm(packet.stdout), GOLDEN_PACKET);
  const full = s.node([REVIEW, 'packet', '--full']);
  assert.equal(s.norm(full.stdout), GOLDEN_FULL);
  const rec = s.node([REVIEW, 'record'], FINDINGS);
  assert.equal(rec.status, 0, rec.stderr);
  const state = JSON.parse(readFileSync(join(s.repo, '.nxy', 'local', 'review.json'), 'utf8'));
  assert.equal(s.norm(rec.stdout.split(state.id).join('<ID>')), GOLDEN_RECORD);
  assert.deepEqual({ ...state, id: '<ID>', ts: '<TS>', branch: '<KEY>' }, {
    docs: { docs: [], wikiDir: null, terms: [] },
    id: '<ID>',
    planHash: '4851c8c6',
    branch: '<KEY>',
    ts: '<TS>',
    summary: { files: 1, added: 2, removed: 0 },
    findings: [{ lens: 'base', severity: 'high', file: 'a.js', line: 2, title: 'Golden finding', evidence: 'why', fix: 'do it', cause: 'change', id: 'R1' }],
    chosen: [],
  });
});
