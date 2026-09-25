// @ts-check
/**
 * Conventions sheet, escapes and documenter (0.4.3). What has to hold: the implementer gets the
 * repo's conventions — its area first, capped, the rest by reference; something a review missed is
 * recorded and kept as a convention or a repo lens that every later review applies; the reminder to
 * do so appears once per session and only on a reviewed branch; and the documenter is offered only
 * when some doc names what changed, found by code.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { conventionSheet, pathsIn } from '../core/memory/conventions.mjs';
import { docTerms, docsQuestion, termsPattern, wikiDirFromCi } from '../core/docs.mjs';
import { checkpoint2 } from '../core/review.mjs';
import { APPROVE, approvalQuestion, planHash } from '../core/plan.mjs';
import { findDocs } from '../hosts/claude-code/docs.mjs';
import { findRg } from '../core/rg.mjs';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const HOOKS = join(ROOT, 'hosts', 'claude-code', 'hooks');
const MEM = join(ROOT, 'hosts', 'claude-code', 'entries', 'mem.mjs');
const REVIEW = join(ROOT, 'hosts', 'claude-code', 'entries', 'review.mjs');

test('conventionSheet: the batch\'s area first, then the project\'s, capped with a pointer', () => {
  const conventions = [
    { title: 'Money is BigDecimal', area: 'billing' },
    { title: 'DTOs are records', area: null },
    { title: 'Controllers only map', area: 'api' },
  ];
  const sheet = conventionSheet(conventions, ['api/src/ClienteController.java'], 'LIST');
  assert.deepEqual(sheet.split('\n').slice(1), ['- Controllers only map (api)', '- DTOs are records', '- Money is BigDecimal (billing)']);
  const capped = conventionSheet(conventions, [], 'LIST', 40);
  assert.match(capped, /- DTOs are records\n… and 2 more: LIST$/);
  assert.equal(conventionSheet([], ['x'], 'LIST'), '', 'no conventions, nothing added');
  assert.deepEqual(pathsIn('- `api/src/A.java:12` — x\n- web/src/b.ts (new)'), ['api/src/A.java', 'web/src/b.ts']);
});

test('docs: terms from changed code, the wiki folder from CI, the question', () => {
  assert.deepEqual(docTerms(['api/ClienteService.java', 'web/src/cliente.component.ts', 'src/index.ts', 'README.md', 'src/app.test.ts', 'a/Db.kt']),
    ['ClienteService', 'cliente.component'], 'generic, short, docs and tests are left out');
  const re = new RegExp(termsPattern(['cliente.component']));
  assert.ok(re.test('see cliente.component for the form') && !re.test('clienteXcomponent'), 'dots are literal');

  const pipeline = [
    'steps:',
    '  - task: WikiPublish@1',
    '    inputs:',
    '      wikiSource: docs/functional',
    '      branch: main',
  ].join('\n');
  assert.equal(wikiDirFromCi([pipeline], (rel) => rel === 'docs/functional'), 'docs/functional');
  assert.equal(wikiDirFromCi(['run: npm test'], () => true), null, 'no wiki, no folder');

  const { question, then } = docsQuestion('abc123', ['docs/a.md', 'docs/b.md']);
  const text = checkpoint2({ id: 'abc123', planHash: 'p', findings: [], summary: { files: 1, added: 2, removed: 0 }, batchOf: () => 1, extra: { question, then } });
  const input = JSON.parse(/^\s+(\{.*\})$/m.exec(text)?.[1] || '{}');
  assert.deepEqual(input.questions.map((q) => q.header), ['Docs'], 'no findings to fix: only the docs question');
  assert.match(text, /nxy:documenter subagent with "Docs for nxy review abc123"/);

  const many = Array.from({ length: 20 }, (_, i) => ({
    id: `R${i + 1}`, cause: /** @type {const} */ ('change'), lens: 'base', severity: 'low', file: 'a.ts', line: 1, title: `t${i}`, evidence: '', fix: '',
  }));
  const withFindings = JSON.parse(/^\s+(\{.*\})$/m.exec(checkpoint2({ id: 'x', planHash: 'p', findings: many, summary: { files: 1, added: 1, removed: 0 }, batchOf: () => 1, extra: { question, then } }))?.[1] || '{}');
  assert.deepEqual(withFindings.questions.map((q) => q.header), ['Review', 'Review', 'Review', 'Docs'], 'never more than four questions');
});

test('findDocs: the same answer with rg and without it', () => {
  const root = mkdtempSync(join(tmpdir(), 'nxy-docs-'));
  for (const d of ['docs/functional', 'node_modules/pkg', '.github/workflows']) mkdirSync(join(root, d), { recursive: true });
  writeFileSync(join(root, 'docs', 'functional', 'alta.md'), 'Intro\n\nUsa ClienteService para el alta.\n');
  writeFileSync(join(root, 'docs', 'nada.md'), 'Nada.\n');
  writeFileSync(join(root, 'node_modules', 'pkg', 'README.md'), 'ClienteService in a dependency\n');
  writeFileSync(join(root, '.github', 'workflows', 'wiki.yml'), 'jobs:\n  publish:\n    steps:\n      - name: Sync to Wiki.js\n        with:\n          path: docs/functional\n');
  const expected = { docs: [{ path: 'docs/functional/alta.md', lines: [{ line: 3, text: 'Usa ClienteService para el alta.' }] }], wikiDir: 'docs/functional', terms: ['ClienteService'] };
  assert.deepEqual(findDocs(root, ['api/ClienteService.java'], {}, null), expected, 'without rg: the walk skips node_modules');
  const rg = findRg();
  if (rg) assert.deepEqual(findDocs(root, ['api/ClienteService.java'], {}, rg), expected, 'with rg');
  assert.deepEqual(findDocs(root, ['api/ClienteService.java'], { paths: ['docs/nothing-here'] }, null).docs, [], '`docs.paths` narrows the search');
});

/** A repo with a plan, a doc that names the changed class, and helpers. */
function sandbox() {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-conv-'));
  const repo = join(dir, 'repo');
  for (const d of ['.git', 'api/src', 'docs']) mkdirSync(join(repo, d), { recursive: true });
  writeFileSync(join(repo, '.git', 'HEAD'), 'ref: refs/heads/feature/x\n');
  writeFileSync(join(repo, '.git', 'config'), '[remote "origin"]\n\turl = git@github.com:x/y.git\n');
  writeFileSync(join(repo, 'docs', 'clientes.md'), '# Clientes\n\nEl alta pasa por `ClienteService`, que valida el CUIT.\n');
  writeFileSync(join(repo, 'docs', 'otros.md'), '# Otros\n\nNada que ver.\n');
  const env = { ...process.env, NXY_HOME: join(dir, 'home'), CLAUDE_PROJECT_DIR: repo };
  const node = (args, input) => spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', ...args, '--cwd', repo], { input, encoding: 'utf8', env, cwd: repo });
  const hook = (file, o) => execFileSync(process.execPath, ['--disable-warning=ExperimentalWarning', join(HOOKS, file)], {
    input: JSON.stringify({ cwd: repo, session_id: 's1', ...o }), encoding: 'utf8', env,
  });
  const PLAN = ['## Plan', 'Goal: x', '### Batch 1 — api: service', '- `api/src/ClienteService.java:10` — validate CUIT', 'Accept: `./mvnw -q test -Dtest=ClienteServiceTest`'].join('\n');
  node([MEM, 'handoff', 'plan'], PLAN);
  const hash = planHash(PLAN);
  const main = join(dir, 'main.jsonl');
  const q = approvalQuestion(hash);
  writeFileSync(main, [
    { type: 'assistant', message: { usage: { cache_read_input_tokens: 20_000 } } },
    { type: 'assistant', message: { content: [{ type: 'tool_use', id: 'q1', name: 'AskUserQuestion', input: { questions: [{ question: q }] } }] } },
    { type: 'user', message: { content: [{ type: 'tool_result', tool_use_id: 'q1', content: 'ok' }] }, toolUseResult: { answers: { [q]: APPROVE } } },
  ].map((l) => JSON.stringify(l)).join('\n') + '\n');
  return { dir, repo, node, hook, main, hash };
}

test('the implementer receives the conventions sheet', () => {
  const s = sandbox();
  s.node([MEM, 'save', 'Services validate, controllers only map', '--type', 'convention', '--area', 'api', '--body', 'x']);
  s.node([MEM, 'save', 'DTOs are records', '--type', 'convention', '--body', 'x']);
  const out = JSON.parse(s.hook('pretooluse-agent.mjs', {
    tool_name: 'Agent', transcript_path: s.main, tool_input: { subagent_type: 'nxy:implementer', prompt: 'Batch 1 — validate CUIT' },
  })).hookSpecificOutput;
  assert.equal(out.permissionDecision, 'allow');
  assert.match(out.updatedInput.prompt, /Repo conventions[^\n]*\n- Services validate, controllers only map \(api\)\n- DTOs are records\n<\/nxy-context>/,
    'inside nxy\'s block, the batch\'s area first');
});

test('flow: docs that name the change are offered at checkpoint 2; escapes kept; the reminder once per session', () => {
  const s = sandbox();
  const svc = join(s.repo, 'api', 'src', 'ClienteService.java');
  writeFileSync(svc, 'class ClienteService {\n}\n');
  s.hook('pretooluse-edit.mjs', { tool_name: 'Edit', agent_id: 'a1', tool_input: { file_path: svc } });
  writeFileSync(svc, 'class ClienteService {\n  void validarCuit(String c) {}\n}\n');

  const rec = s.node([REVIEW, 'record'], '[]');
  assert.equal(rec.status, 0, rec.stderr);
  assert.match(rec.stdout, /"header":"Docs"[\s\S]*"description":"docs\/clientes\.md"/, 'offered, with the doc that names ClienteService only');
  const docs = s.node([REVIEW, 'docs']).stdout;
  assert.match(docs, /- docs\/clientes\.md\n {4}docs\/clientes\.md:3: El alta pasa por `ClienteService`/);
  assert.match(docs, /\+ {2}void validarCuit/);
  assert.doesNotMatch(docs, /otros\.md/);

  // An escape kept as a convention, and one as a repo lens that later reviews apply.
  assert.match(s.node([REVIEW, 'escape', 'CUIT validation lives in CuitValidator', '--as', 'convention', '--lens', 'api']).stdout, /saved as convention/);
  assert.match(s.node([MEM, 'list', '--type', 'convention']).stdout, /CUIT validation lives in CuitValidator/);
  s.node([REVIEW, 'escape', 'Every new endpoint has an integration test', '--as', 'lens']);
  const lens = readFileSync(join(s.repo, '.nxy', 'lenses', 'repo.md'), 'utf8');
  assert.match(lens, /^---\nname: repo\n[\s\S]*always: true\n---\n- Every new endpoint has an integration test\n$/);
  assert.match(s.node([REVIEW, 'packet']).stdout, /### This repo — what past reviews missed \(lens: repo\)\n- Every new endpoint has an integration test/);
  assert.match(s.node([REVIEW, 'status']).stdout, /escapes: 2[\s\S]*api\s+0\s+0\s+0\s+1/);
  assert.match(s.node([REVIEW, 'escape', 'x']).stdout, /--as convention\|lens/, 'where it goes is always the user\'s call');

  // The reminder: once per session, only on the reviewed branch.
  const prompt = (session) => s.hook('userpromptsubmit.mjs', { session_id: session, prompt: 'the PR says the CUIT check is in the wrong place' });
  assert.match(JSON.parse(prompt('s9')).hookSpecificOutput.additionalContext, /already reviewed \(review [0-9a-f]{6}\)[\s\S]*escape "<the rule>"/);
  assert.equal(prompt('s9').trim(), '', 'once per session');
  writeFileSync(join(s.repo, '.git', 'HEAD'), 'ref: refs/heads/feature/other\n');
  assert.equal(prompt('s10').trim(), '', 'another branch: nothing');
  assert.ok(existsSync(join(s.repo, '.nxy', 'local', 'review.json')));
});
