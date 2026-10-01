// @ts-check
/**
 * The review of a finished plan (0.4.2). What has to hold: the diff is the plan's — each file
 * against the copy kept before the plan's first edit, so the user's own uncommitted changes never
 * show up as the plan's; whether there is a review and which lenses apply is decided by what
 * changed, never by how much; a finding is the change's only if it points into a changed hunk;
 * only those are offered to fix; and a fix never leads to a second suite and review.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { fileDiff } from '../core/diff.mjs';
import {
  batchForFile, checkpoint2, classifyFindings, globToRegExp, mergeLenses, packetOutline, parseFindings, parseLens, reviewNeeded, reviewPacket,
  selectLenses,
} from '../core/review.mjs';
import { afterBatch } from '../core/verify.mjs';
import { recordSuite } from '../hosts/claude-code/verify-state.mjs';
import { APPROVE, approvalQuestion, planHash } from '../core/plan.mjs';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const HOOKS = join(ROOT, 'hosts', 'claude-code', 'hooks');
const MEM = join(ROOT, 'hosts', 'claude-code', 'entries', 'mem.mjs');
const REVIEW = join(ROOT, 'hosts', 'claude-code', 'entries', 'review.mjs');

const LENSES = readdirSync(join(ROOT, 'lenses')).map((n) => parseLens(readFileSync(join(ROOT, 'lenses', n), 'utf8'), n.replace(/\.md$/, '')))
  .filter((l) => l !== null);

test('diff: the hunks and the new-file lines they cover', () => {
  const before = ['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j'].join('\n');
  const after = ['a', 'b', 'c', 'D', 'e', 'f', 'g', 'h', 'i', 'j', 'k'].join('\n');
  const d = fileDiff('x.txt', before, after);
  assert.equal(d.added, 2);
  assert.equal(d.removed, 1);
  assert.match(d.text, /^--- a\/x\.txt\n\+\+\+ b\/x\.txt\n@@ -1,10 \+1,11 @@\n a\n b\n c\n-d\n\+D\n/);
  assert.deepEqual(d.ranges, [[1, 11]], 'close changes share one hunk');
  assert.equal(fileDiff('x', before, before).text, '', 'no change, no diff');
  const created = fileDiff('n.ts', null, 'x\ny\n');
  assert.match(created.text, /^--- \/dev\/null\n\+\+\+ b\/n\.ts\n@@ -1,0 \+1,2 @@/);
  const far = fileDiff('f', Array.from({ length: 40 }, (_, i) => `l${i}`).join('\n'), Array.from({ length: 40 }, (_, i) => (i === 2 || i === 35 ? 'X' : `l${i}`)).join('\n'));
  assert.deepEqual(far.ranges, [[1, 6], [33, 39]], 'distant changes, separate hunks');
});

test('lenses: globs, selection by what changed, repo overrides', () => {
  assert.ok(globToRegExp('**/entity/**').test('src/main/java/app/entity/Cliente.java'));
  assert.ok(globToRegExp('*Controller.java').test('api/src/ClienteController.java'), 'no slash: the file name, anywhere');
  assert.ok(globToRegExp('**/conf/routes').test('play-app/conf/routes'));
  assert.ok(!globToRegExp('*Controller.java').test('api/src/Controllers.md'));

  const pick = (files) => selectLenses(LENSES, files).map((s) => s.lens.name).sort();
  assert.deepEqual(pick([{ path: 'src/app/ClienteService.java', content: 'class ClienteService {}' }]), ['base']);
  assert.deepEqual(pick([{ path: 'src/app/Cliente.java', content: '@Entity\nclass Cliente {}' }]), ['base', 'persistence'], 'content counts, not only names');
  assert.deepEqual(pick([
    { path: 'api/ClienteController.java', content: '' },
    { path: 'web/src/app/cliente/cliente.component.ts', content: '' },
    { path: 'db/migrations/V2__cuit.sql', content: 'ALTER TABLE cliente ADD cuit varchar(11);' },
    { path: 'api/auth/TokenFilter.java', content: '' },
  ]), ['api', 'base', 'database', 'frontend', 'security']);
  assert.deepEqual(pick([{ path: 'docs/auth.md', content: 'password' }]), ['base'], 'documentation never triggers a lens');

  const repoOff = /** @type {any} */ (parseLens('---\nname: frontend\nenabled: false\n---\n', 'x'));
  const repoNew = /** @type {any} */ (parseLens('---\nname: money\ntitle: Money\ncontent: BigDecimal\n---\n- scale 2', 'x'));
  const merged = mergeLenses(LENSES, [repoOff, repoNew]).map((l) => l.name);
  assert.ok(!merged.includes('frontend') && merged.includes('money'), 'a repo turns lenses off and adds its own');
});

test('reviewNeeded: by the kind of files, never by size', () => {
  assert.deepEqual(reviewNeeded(['README.md', 'docs/guide.md']), { needed: false, reason: 'only documentation changed' });
  assert.equal(reviewNeeded(['src/cliente.test.ts', 'README.md']).needed, false);
  assert.equal(reviewNeeded(['src/test/java/app/ClienteServiceTest.java']).needed, false);
  assert.equal(reviewNeeded(['README.md', 'src/app/auth/Login.java']).needed, true, 'two lines of login code are enough');
});

test('findings: parsed, classified by where they point, offered only when they are the change\'s', () => {
  const raw = '```json\n' + JSON.stringify([
    { lens: 'persistence', severity: 'high', file: 'src/app/ClienteRepository.java', line: 12, title: 'N+1 on contactos', evidence: 'findAll then getContactos()', fix: 'fetch join' },
    { lens: 'base', severity: 'low', file: 'src/app/ClienteRepository.java', line: 80, title: 'old unused import' },
    { lens: 'api', severity: 'medium', file: 'src/app/Other.java', line: 3, title: 'missing @Valid' },
    { title: 'no place' },
  ]) + '\n```';
  const { findings, errors } = parseFindings(raw);
  assert.equal(findings.length, 3);
  assert.match(errors.join(), /finding 4: needs file, line/);
  assert.deepEqual(parseFindings('looks fine').errors, ['not JSON: expected an array of findings (or [] when there are none)']);

  const changed = new Map([['src/app/ClienteRepository.java', /** @type {[number, number][]} */ ([[8, 14]])]]);
  const c = classifyFindings(findings, changed);
  assert.deepEqual(c.map((f) => [f.id, f.cause, f.line]), [['R1', 'change', 12], ['R2', 'preexisting', 3], ['R3', 'preexisting', 80]],
    'outside every hunk, or in a file the plan did not touch: preexisting');

  const batches = [{ n: 1, text: 'Batch 1 — api\n- `src/app/ClienteRepository.java:10` — x' }, { n: 2, text: 'Batch 2 — web' }];
  assert.equal(batchForFile(batches, 'src/app/ClienteRepository.java'), 1);
  assert.equal(batchForFile(batches, 'src/unknown.ts'), 2, 'unknown file: the last batch');

  const text = checkpoint2({ id: 'abc123', planHash: 'p1', findings: c, summary: { files: 2, added: 30, removed: 4 }, batchOf: (f) => batchForFile(batches, f.file) });
  assert.match(text, /1 finding from the change \(1 high, 0 medium, 0 low\), 2 preexisting/);
  const input = JSON.parse(/^\s+(\{.*\})$/m.exec(text)?.[1] || '{}');
  assert.equal(input.questions[0].question, 'Fix which findings of nxy review abc123?');
  assert.equal(input.questions[0].multiSelect, true);
  assert.deepEqual(input.questions[0].options.map((o) => o.label), ['R1 N+1 on contactos', 'None'], 'only the change\'s; "None" when one');
  assert.match(text, /R1: "Batch 1 — fix R1 of review abc123: N\+1 on contactos at src\/app\/ClienteRepository\.java:12\. fetch join"/);
  assert.match(checkpoint2({ id: 'x', planHash: 'p', findings: [], summary: { files: 1, added: 1, removed: 0 }, batchOf: () => 1 }), /Nothing from the change to fix/);
});

test('a review fix closes without another suite or review', () => {
  const text = afterBatch({ hash: 'h', n: 1, verdict: { status: 'pass' }, batches: [1], recorded: {}, fix: true, command: 'x' });
  assert.match(text, /One correction round only/);
  assert.doesNotMatch(text, /nxy:tester/);
});

test('packet: code diffs before test diffs, and an outline that stays short for a big diff', () => {
  const mk = (path, n) => {
    const d = fileDiff(path, null, Array.from({ length: n }, (_, i) => `line ${i}`).join('\n'));
    return { path, status: 'new', added: d.added, removed: 0, diff: d.text };
  };
  const files = [mk('tests/a.test.mjs', 500), mk('src/a.mjs', 900), mk('src/b.mjs', 600)];
  const text = reviewPacket({
    planHash: 'abc123', plan: '## Plan\nGoal: x', conventions: [], lenses: [], files, recordCmd: 'rec', maxDiffLines: 5000,
  });
  const at = (p) => text.indexOf(`+++ b/${p}`);
  assert.ok(at('src/a.mjs') < at('src/b.mjs') && at('src/b.mjs') < at('tests/a.test.mjs'), 'tests last');
  const out = packetOutline(text, { path: 'x/review-packet.md' });
  assert.ok(out.split('\n').length < 40);
  assert.match(out, /tests\/a\.test\.mjs \(test\)/);
  assert.doesNotMatch(out, /src\/a\.mjs \(test\)/);
  assert.match(out, /lines \(4 parts\): src\/a\.mjs/);
  const lines = text.split('\n');
  const start = Number(/line (\d+), \d+ lines.*: src\/b\.mjs/.exec(out)?.[1]);
  assert.equal(lines[start - 1], '--- /dev/null');
  assert.equal(lines[start], '+++ b/src/b.mjs');
});

test('flow: copies before the first edit → packet → record → checkpoint 2 → fix counted → done clears', () => {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-review-'));
  const repo = join(dir, 'repo');
  mkdirSync(join(repo, '.git'), { recursive: true });
  mkdirSync(join(repo, 'src', 'app'), { recursive: true });
  writeFileSync(join(repo, '.git', 'HEAD'), 'ref: refs/heads/feature/x\n');
  writeFileSync(join(repo, '.git', 'config'), '[remote "origin"]\n\turl = git@github.com:x/y.git\n');
  const env = { ...process.env, NXY_HOME: join(dir, 'home'), CLAUDE_PROJECT_DIR: repo };
  const node = (args, input) => spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', ...args], { input, encoding: 'utf8', env, cwd: repo });
  const hook = (file, o) => execFileSync(process.execPath, ['--disable-warning=ExperimentalWarning', join(HOOKS, file)], {
    input: JSON.stringify({ cwd: repo, session_id: 's1', ...o }), encoding: 'utf8', env,
  });

  const repoFile = join(repo, 'src', 'app', 'ClienteRepository.java');
  // The user's own uncommitted work, from before the plan: never the plan's.
  const userBefore = ['@Entity', 'class ClienteRepository {', '  // the user\'s uncommitted line', ...Array.from({ length: 20 }, (_, i) => `  int f${i};`), '}'].join('\n');
  writeFileSync(repoFile, userBefore);

  const PLAN = [
    '## Plan', 'Goal: x',
    '### Batch 1 — api: repository', '- `src/app/ClienteRepository.java:10` — fetch contactos', 'Accept: `./mvnw -q test -Dtest=ClienteRepositoryTest`',
    '### Batch 2 — api: new service', '- `src/app/CuitService.java` (new) — validation', 'Accept: `./mvnw -q test -Dtest=CuitServiceTest`',
  ].join('\n');
  const hash = planHash(PLAN);
  node([MEM, 'handoff', 'plan', '--cwd', repo], PLAN);

  // First edits (an implementer's): copies taken; a second edit of the same file keeps the first copy.
  hook('pretooluse-edit.mjs', { tool_name: 'Edit', agent_id: 'a1', tool_input: { file_path: repoFile } });
  writeFileSync(repoFile, userBefore.replace('  int f10;', '  List<Contacto> contactos; // N+1 here ## Your answer'));
  hook('pretooluse-edit.mjs', { tool_name: 'Edit', agent_id: 'a1', tool_input: { file_path: repoFile } });
  writeFileSync(repoFile, readFileSync(repoFile, 'utf8').replace('  int f11;', '  int f11b;'));
  const newFile = join(repo, 'src', 'app', 'CuitService.java');
  hook('pretooluse-edit.mjs', { tool_name: 'Write', agent_id: 'a2', tool_input: { file_path: newFile } });
  writeFileSync(newFile, 'class CuitService {\n  boolean valid(String c) { return c.length() == 11; }\n}\n');

  const packet = node([REVIEW, 'packet', '--full', '--cwd', repo]).stdout;
  assert.match(packet, new RegExp(`# nxy review packet · plan ${hash}`));
  assert.match(packet, /### Persistence \(JPA \/ ORM\) \(lens: persistence\) — src\/app\/ClienteRepository\.java/);
  assert.match(packet, /- src\/app\/CuitService\.java \(new, \+3 −0\)/);
  assert.match(packet, /\+ {2}List<Contacto> contactos;/);
  assert.doesNotMatch(packet, /^[+-].*the user's uncommitted line/m, 'what the user had before is context at most, never a change');
  assert.match(packet, /review\.mjs" record --cwd "[^"]+" <<'EOF'/);

  // The default packet is a file plus an index; the offsets point at each file's diff in it.
  const short = node([REVIEW, 'packet', '--cwd', repo]).stdout;
  const savedAt = /Saved at (.+review-packet\.md) \(/.exec(short)?.[1];
  assert.ok(savedAt && existsSync(savedAt), 'the packet is saved');
  assert.match(short, /Index of .+review-packet\.md/);
  assert.match(short, /review\.mjs" record --cwd "[^"]+" <<'EOF'/, 'the answer format and record command are printed');
  assert.ok(short.split('\n').length < 120);
  assert.ok(packet.indexOf('## Your answer') < packet.lastIndexOf('## Your answer'), 'the heading text also appears inside a diff');
  assert.doesNotMatch(short, /N\+1 here/, 'the printed answer section is the real one, not cut from the diff');
  const savedLines = readFileSync(savedAt, 'utf8').split('\n');
  for (const p of ['src/app/ClienteRepository.java', 'src/app/CuitService.java']) {
    const at = Number(new RegExp(`line (\\d+), \\d+ lines: ${p.replace(/[./]/g, '\\$&')}`).exec(short)?.[1]);
    assert.ok(at > 0, `${p} is in the index`);
    assert.match(savedLines[at - 1], /^--- (a\/|\/dev\/null)/);
    assert.match(savedLines[at], new RegExp(`^\\+\\+\\+ b/${p.replace(/[./]/g, '\\$&')}$`));
  }

  const findings =JSON.stringify([
    { lens: 'persistence', severity: 'high', file: 'src/app/ClienteRepository.java', line: 13, title: 'N+1 on contactos', fix: 'fetch join' },
    { lens: 'base', severity: 'low', file: 'src/app/ClienteRepository.java', line: 3, title: 'stray comment' },
  ]);
  const rec = node([REVIEW, 'record', '--cwd', repo], findings);
  assert.equal(rec.status, 0, rec.stderr);
  assert.match(rec.stdout, /2 files, \+5 −2/);
  assert.match(rec.stdout, /1 finding from the change \(1 high[\s\S]*Preexisting[\s\S]*R2 low \[base\] src\/app\/ClienteRepository\.java:3/);
  const id = /nxy review ([0-9a-f]{6})/.exec(rec.stdout)?.[1];
  assert.match(rec.stdout, new RegExp(`R1: "Batch 1 — fix R1 of review ${id}: N\\+1 on contactos`));
  assert.match(node([REVIEW, 'show', '--cwd', repo]).stdout, new RegExp(`nxy review ${id}`));

  // The user picked R1: its dispatch (plan approved) is counted as chosen.
  const main = join(dir, 'main.jsonl');
  const q = approvalQuestion(hash);
  writeFileSync(main, [
    { type: 'assistant', message: { usage: { cache_read_input_tokens: 20_000 } } },
    { type: 'assistant', message: { content: [{ type: 'tool_use', id: 'q1', name: 'AskUserQuestion', input: { questions: [{ question: q }] } }] } },
    { type: 'user', message: { content: [{ type: 'tool_result', tool_use_id: 'q1', content: 'ok' }] }, toolUseResult: { answers: { [q]: APPROVE } } },
  ].map((l) => JSON.stringify(l)).join('\n') + '\n');
  const out = JSON.parse(hook('pretooluse-agent.mjs', {
    tool_name: 'Agent', transcript_path: main, tool_input: { subagent_type: 'nxy:implementer', prompt: `Batch 1 — fix R1 of review ${id}: N+1 on contactos` },
  })).hookSpecificOutput;
  assert.equal(out.permissionDecision, 'allow');
  assert.match(node([REVIEW, 'status', '--cwd', repo]).stdout, /persistence\s+1\s+1\s+0[\s\S]*base\s+0\s+0\s+1/);

  // After the tester (its suite recorded green): the reviewer is dictated.
  recordSuite(repo, hash, { status: 'done', ts: Date.now(), red: [], ran: [] });
  const afterTester = hook('posttooluse-agent.mjs', { tool_name: 'Agent', tool_input: { subagent_type: 'nxy:tester', prompt: `Full suite for nxy plan ${hash}` } });
  assert.match(JSON.parse(afterTester).hookSpecificOutput.additionalContext, new RegExp(`nxy:reviewer subagent once with "Review nxy plan ${hash} \\(project: `));

  node([MEM, 'handoff', 'done', '--cwd', repo]);
  assert.ok(!existsSync(savedAt), 'the saved packet goes with the task');
  assert.ok(!existsSync(join(repo, '.nxy', 'local', 'baseline')) || !readdirSync(join(repo, '.nxy', 'local', 'baseline')).length, 'the copies go with the task');
});

test('one .nxy root: plan saved from a subdirectory, edit hook at the project dir, packet from either', () => {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-review-root-'));
  const repo = join(dir, 'repo');
  const sub = join(repo, 'sub');
  mkdirSync(join(repo, '.git'), { recursive: true });
  mkdirSync(join(repo, 'src'), { recursive: true });
  mkdirSync(sub, { recursive: true });
  writeFileSync(join(repo, '.git', 'HEAD'), 'ref: refs/heads/feature/y\n');
  writeFileSync(join(repo, '.git', 'config'), '[remote "origin"]\n\turl = git@github.com:x/z.git\n');
  /** @type {NodeJS.ProcessEnv} */
  const base = { ...process.env, NXY_HOME: join(dir, 'home') };
  delete base.CLAUDE_PROJECT_DIR;
  const shell = (args, input, cwd) => spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', ...args], { input, encoding: 'utf8', env: base, cwd });
  const file = join(repo, 'src', 'Svc.java');
  writeFileSync(file, 'class Svc {\n  int a;\n}\n');
  const PLAN = ['## Plan', 'Goal: x', '### Batch 1 — api: svc', '- `src/Svc.java:2` — change a', 'Accept: `./mvnw -q test`'].join('\n');

  assert.equal(shell([MEM, 'handoff', 'plan'], PLAN, sub).status, 0);
  execFileSync(process.execPath, ['--disable-warning=ExperimentalWarning', join(HOOKS, 'pretooluse-edit.mjs')], {
    input: JSON.stringify({ cwd: sub, session_id: 's1', tool_name: 'Edit', agent_id: 'a1', tool_input: { file_path: file } }),
    encoding: 'utf8', env: { ...base, CLAUDE_PROJECT_DIR: repo },
  });
  writeFileSync(file, 'class Svc {\n  int b;\n}\n');

  for (const where of [sub, repo]) {
    const packet = shell([REVIEW, 'packet', '--full'], '', where);
    assert.equal(packet.status, 0, packet.stderr);
    assert.match(packet.stdout, /- src\/Svc\.java \(modified, \+1 −1\)/, `listed from ${where === sub ? 'sub' : 'repo'}`);
    assert.match(packet.stdout, /-  int a;\n\+  int b;/);
    assert.match(packet.stdout, /review\.mjs" record --cwd "/);
  }
  assert.ok(!existsSync(join(sub, '.nxy')), 'no second .nxy under the subdirectory');
  assert.ok(existsSync(join(repo, '.nxy', 'local')));
});
