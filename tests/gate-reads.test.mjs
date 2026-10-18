// @ts-check
/**
 * Broad main-thread searches counted from the session transcripts: `/nxy:gate status --since`
 * (whole project) and `/nxy:stats` (the session itself). No ledger, no hook.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { projectSlug } from '../hosts/claude-code/paths.mjs';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const DAY = 86_400_000;
const CONFIG = (projectsDir) => ({ gate: { contextTokens: 100_000 }, metrics: { projectsDir } });

let seq = 0;
/**
 * One assistant entry carrying `tools` (name, input) at a context of `ctx` tokens.
 * @param {string} cwd
 * @param {{ageDays?: number, ctx: number, tools: any[], sidechain?: boolean, requestId?: string, ids?: string[]}} o
 */
function entry(cwd, o) {
  const { ageDays = 0, ctx, tools, sidechain = false, requestId, ids } = o;
  const n = ++seq;
  return {
    isSidechain: sidechain,
    type: 'assistant',
    uuid: `u${n}`,
    requestId: requestId ?? `req_${n}`,
    timestamp: new Date(Date.now() - ageDays * DAY).toISOString(),
    cwd,
    message: {
      id: `msg_${n}`,
      model: 'claude-sonnet-4-5',
      role: 'assistant',
      usage: { input_tokens: 10, output_tokens: 5, cache_read_input_tokens: ctx - 10, cache_creation_input_tokens: 0 },
      content: tools.map(([name, input], i) => ({ type: 'tool_use', id: ids?.[i] ?? `tu_${n}_${i}`, name, input })),
    },
  };
}

/** A temp cwd with its config and one main transcript per entry list. */
function setup(sessions) {
  const cwd = mkdtempSync(join(tmpdir(), 'nxy-reads-'));
  const projectsDir = mkdtempSync(join(tmpdir(), 'nxy-reads-tx-'));
  mkdirSync(join(cwd, '.nxy'), { recursive: true });
  writeFileSync(join(cwd, '.nxy', 'config.json'), JSON.stringify(CONFIG(projectsDir)));
  writeFileSync(join(cwd, 'present.txt'), 'x');
  const dir = join(projectsDir, projectSlug(cwd));
  mkdirSync(dir, { recursive: true });
  sessions.forEach((make, i) => {
    writeFileSync(join(dir, `sess-${i}.jsonl`), make(cwd).map((e) => JSON.stringify(e)).join('\n') + '\n');
  });
  return cwd;
}

function run(script, args) {
  const r = spawnSync(process.execPath, [join(ROOT, 'hosts', 'claude-code', 'entries', script), ...args], { encoding: 'utf8', env: { ...process.env, CLAUDE_CODE_SESSION_ID: '' } });
  assert.equal(r.status, 0, `${script} ${args.join(' ')} failed:\n${r.stderr}`);
  return r.stdout;
}

test('gate status --since counts only the window and sums its tokens', () => {
  const cwd = setup([(c) => [
    entry(c, { ageDays: 1, ctx: 150_000, tools: [['Grep', { pattern: 'foo' }]] }),
    entry(c, { ageDays: 2, ctx: 160_000, tools: [['Bash', { command: 'rg foo' }]] }),
    entry(c, { ageDays: 20, ctx: 170_000, tools: [['Glob', { pattern: '**/*.mjs' }]] }),
  ]]);
  const out = run('gate.mjs', ['status', '--since', '7d', '--cwd', cwd]);
  assert.match(out, /broad searches above 100k context[^\n]*: 2 — Grep 1 · Bash 1 · context carried at those searches 310k tokens/);
  const wide = run('gate.mjs', ['status', '--since', '30d', '--cwd', cwd]);
  assert.match(wide, /: 3 — Grep 1 · Glob 1 · Bash 1 · context carried at those searches 480k tokens/);
});

test('below the threshold, targeted reads, files and sidechains are not counted', () => {
  const cwd = setup([(c) => [
    entry(c, { ctx: 50_000, tools: [['Grep', { pattern: 'foo' }]] }),
    entry(c, { ctx: 150_000, tools: [['Read', { file_path: 'present.txt', offset: 1, limit: 20 }]] }),
    entry(c, { ctx: 150_000, tools: [['Grep', { pattern: 'foo', path: 'present.txt' }]] }),
    entry(c, { ctx: 150_000, sidechain: true, tools: [['Grep', { pattern: 'foo' }]] }),
  ]]);
  assert.match(run('gate.mjs', ['status', '--cwd', cwd]), /broad searches:\s+none recorded/);
});

test('a missing path is a file only when it has an extension', () => {
  const cwd = setup([(c) => [
    entry(c, { ctx: 150_000, tools: [['Read', { file_path: 'gone.mjs', offset: 1 }]] }),
    entry(c, { ctx: 150_000, tools: [['Read', { file_path: 'gone.mjs' }]] }),
    entry(c, { ctx: 150_000, tools: [['Grep', { pattern: 'foo', path: 'gone.mjs' }]] }),
    entry(c, { ctx: 150_000, tools: [['Grep', { pattern: 'foo', path: 'srcdir' }]] }),
  ]]);
  const out = run('gate.mjs', ['status', '--cwd', cwd]);
  assert.match(out, /: 2 — Grep 1 · Read 1 · /);
});

test('a streamed duplicate (same requestId and tool_use id) counts once', () => {
  const cwd = setup([(c) => [
    entry(c, { ctx: 150_000, requestId: 'req_dup', ids: ['tu_dup'], tools: [['Grep', { pattern: 'foo' }]] }),
    entry(c, { ctx: 150_000, requestId: 'req_dup', ids: ['tu_dup'], tools: [['Grep', { pattern: 'foo' }]] }),
  ]]);
  assert.match(run('gate.mjs', ['status', '--cwd', cwd]), /: 1 — Grep 1 · /);
});

test('a streamed chunk with zeroed usage takes the context of the first chunk of its requestId', () => {
  const cwd = setup([(c) => {
    const first = entry(c, { ctx: 150_000, requestId: 'req_stream', ids: ['tu_a'], tools: [['Grep', { pattern: 'foo' }]] });
    const second = entry(c, { ctx: 150_000, requestId: 'req_stream', ids: ['tu_b'], tools: [['Grep', { pattern: 'bar' }]] });
    second.message.usage = { input_tokens: 0, output_tokens: 0, cache_read_input_tokens: 0, cache_creation_input_tokens: 0 };
    return [first, second];
  }]);
  assert.match(run('gate.mjs', ['status', '--cwd', cwd]), /: 2 — Grep 2 · context carried at those searches 300k tokens/);
});

test('gate status on an empty project says none recorded', () => {
  const cwd = setup([]);
  assert.match(run('gate.mjs', ['status', '--cwd', cwd]), /broad searches:\s+none recorded/);
});

test('stats counts the broad searches of its own session', () => {
  const cwd = setup([(c) => [entry(c, { ctx: 150_000, tools: [['Grep', { pattern: 'foo' }]] })]]);
  const out = run('stats.mjs', ['--session', 'last', '--cwd', cwd]);
  assert.match(out, /broad searches in the main thread[^\n]*: 1 — Grep 1/);
  const json = JSON.parse(run('stats.mjs', ['--session', 'last', '--cwd', cwd, '--json']));
  assert.equal(json.broadSearches.count, 1);

  const none = setup([(c) => [entry(c, { ctx: 150_000, tools: [['Read', { file_path: 'present.txt', limit: 5 }]] })]]);
  assert.doesNotMatch(run('stats.mjs', ['--session', 'last', '--cwd', none]), /broad searches in the main thread/);
});
