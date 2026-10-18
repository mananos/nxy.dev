// @ts-check
/**
 * PreToolUse Grep|Glob (and Bash searches): only the planner is nudged once toward locate.mjs;
 * the main thread is never blocked and nothing is recorded (no reads.jsonl).
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { LOCATE_NUDGE_MARK } from '../core/search-scope.mjs';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const HOOKS = join(ROOT, 'hosts', 'claude-code', 'hooks');

function setup(/** @type {any} */ config = { gate: { enabled: true, contextTokens: 10_000 } }) {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-sh-'));
  const repo = join(dir, 'repo');
  mkdirSync(join(repo, '.git'), { recursive: true });
  writeFileSync(join(repo, '.git', 'HEAD'), 'ref: refs/heads/main\n');
  mkdirSync(join(repo, '.nxy'), { recursive: true });
  writeFileSync(join(repo, '.nxy', 'config.json'), JSON.stringify(config));
  writeFileSync(join(repo, 'one.txt'), 'x\n');
  const env = { ...process.env, NXY_HOME: join(dir, 'home'), CLAUDE_PROJECT_DIR: repo };
  /** Main transcript carrying `tokens` of context. */
  const main = (tokens) => {
    const p = join(dir, `main-${Math.random().toString(36).slice(2)}.jsonl`);
    writeFileSync(p, JSON.stringify({ type: 'assistant', message: { usage: { cache_read_input_tokens: tokens } } }) + '\n');
    return p;
  };
  const run = (file, o, raw) => spawnSync(process.execPath, [join(HOOKS, file)], {
    input: raw ?? JSON.stringify({ cwd: repo, session_id: 's1', ...o }), encoding: 'utf8', env,
  });
  const ledger = join(repo, '.nxy', 'local', 'metrics', 'reads.jsonl');
  return { dir, repo, main, run, ledger };
}

const search = (s, tool, tool_input, extra = {}) => s.run('pretooluse-search.mjs', { tool_name: tool, tool_input, ...extra });
const bash = (s, command, extra = {}) => s.run('pretooluse-bash.mjs', { tool_name: 'Bash', tool_input: { command }, ...extra });

test('main thread above the threshold with gate.reads off: nothing is printed and no reads.jsonl is created', () => {
  const s = setup({ gate: { enabled: true, contextTokens: 10_000, reads: false } });
  const t = s.main(50_000);
  for (const [tool, input] of [['Grep', { pattern: 'foo' }], ['Glob', { pattern: '**/*.js' }]]) {
    const r = search(s, tool, input, { transcript_path: t });
    assert.equal(r.status, 0);
    assert.equal(r.stdout, '');
  }
  const r = bash(s, 'rg foo', { transcript_path: t });
  assert.equal(r.status, 0);
  assert.ok(!r.stdout.includes('deny'));
  assert.ok(!existsSync(s.ledger));
});

test('a Read payload exits 0 with empty stdout, even from the planner', () => {
  const s = setup();
  const t = s.main(50_000);
  assert.equal(search(s, 'Read', { file_path: join(s.repo, 'one.txt') }, { transcript_path: t }).stdout, '');
  const r = search(s, 'Read', { file_path: join(s.repo, 'one.txt') }, { agent_id: 'a6', agent_type: 'nxy:planner', transcript_path: t });
  assert.equal(r.status, 0);
  assert.equal(r.stdout, '');
});

test('planner: the first repo-wide grep is denied with the locate command, the next is allowed', () => {
  const s = setup();
  const common = { agent_id: 'a1', agent_type: 'nxy:planner', transcript_path: s.main(1_000) };
  const r1 = search(s, 'Grep', { pattern: 'handleLogin(' }, common);
  const out = JSON.parse(r1.stdout).hookSpecificOutput;
  assert.equal(out.permissionDecision, 'deny');
  assert.ok(out.permissionDecisionReason.includes(LOCATE_NUDGE_MARK));
  assert.match(out.permissionDecisionReason, /locate\.mjs" "handleLogin"/);
  assert.equal(search(s, 'Grep', { pattern: 'handleLogin(' }, common).stdout, '');
  assert.ok(existsSync(join(s.repo, '.nxy', 'local', 'planner-nudge.json')));
});

test('planner that ran locate first is never denied; Bash rg from the planner is nudged', () => {
  const s = setup();
  const a = { agent_id: 'a2', agent_type: 'nxy:planner', transcript_path: s.main(1_000) };
  // (the Bash hook may still rewrite through rtk when it is installed: only the deny matters here)
  assert.ok(!bash(s, 'node "/x/entries/locate.mjs" "foo"', a).stdout.includes('"deny"'));
  assert.equal(search(s, 'Grep', { pattern: 'foo' }, a).stdout, '');
  const b = { agent_id: 'a3', agent_type: 'nxy:planner', transcript_path: s.main(1_000) };
  const r = bash(s, 'rg foo', b);
  assert.equal(JSON.parse(r.stdout).hookSpecificOutput.permissionDecision, 'deny');
  assert.ok(!bash(s, 'rg foo', b).stdout.includes('"deny"'));
});

test('planner identified only through its .meta.json is nudged', () => {
  const s = setup();
  const main = s.main(1_000);
  const subs = join(main.replace(/\.jsonl$/, ''), 'subagents');
  mkdirSync(subs, { recursive: true });
  writeFileSync(join(subs, 'agent-a4.meta.json'), JSON.stringify({ agentType: 'nxy:planner' }));
  const r = search(s, 'Grep', { pattern: 'foo' }, { agent_id: 'a4', transcript_path: main });
  assert.equal(JSON.parse(r.stdout).hookSpecificOutput.permissionDecision, 'deny');
});

test('implementer is never denied, nor a Read', () => {
  const s = setup();
  const t = s.main(50_000);
  assert.equal(search(s, 'Grep', { pattern: 'foo' }, { agent_id: 'a5', agent_type: 'nxy:implementer', transcript_path: t }).stdout, '');
  assert.equal(search(s, 'Read', { file_path: join(s.repo, 'one.txt') }, { agent_id: 'a5', agent_type: 'nxy:planner', transcript_path: t }).stdout, '');
  assert.ok(!existsSync(s.ledger));
});

// --- the read gate (1.3.0) ---
const MARK = 'nxy read gate:';
const gateLedger = (s) => join(s.repo, '.nxy', 'local', 'metrics', 'gate.jsonl');
const escFile = (s, kind) => join(s.repo, '.nxy', 'local', kind === 'search' ? 'gate-escape-search.json' : 'gate-escape.json');
const arm = (s, kind) => {
  mkdirSync(join(s.repo, '.nxy', 'local'), { recursive: true });
  writeFileSync(escFile(s, kind), JSON.stringify({ until: Date.now() + 60_000 }));
};
const reason = (r) => (r.stdout ? JSON.parse(r.stdout).hookSpecificOutput : {});

test('read gate: broad main-thread Grep over the threshold is denied with its own escape named', () => {
  const s = setup();
  const r = search(s, 'Grep', { pattern: 'foo' }, { transcript_path: s.main(50_000) });
  const out = reason(r);
  assert.equal(out.permissionDecision, 'deny');
  assert.ok(out.permissionDecisionReason.startsWith(MARK));
  assert.match(out.permissionDecisionReason, /\/nxy:gate once search/);
  assert.ok(!/\/nxy:gate once(?! search)/.test(out.permissionDecisionReason));
  const rows = readFileSync(gateLedger(s), 'utf8').trim().split('\n').map((l) => JSON.parse(l));
  assert.equal(rows[0].kind, 'read');
  assert.equal(rows[0].allow, false);
});

test('read gate: the reads-off line comes with the first refusal of a session only', () => {
  const s = setup();
  const t = s.main(50_000);
  const msg = (sid) => reason(search(s, 'Grep', { pattern: 'foo' }, { transcript_path: t, session_id: sid })).permissionDecisionReason;
  assert.match(msg('A'), /\/nxy:gate reads off/);
  assert.ok(!/\/nxy:gate reads off/.test(msg('A')));
  assert.match(msg('B'), /\/nxy:gate reads off/);
});

test('read gate: lets through under the threshold, on a file path, off switches, and without a scout', () => {
  const t0 = setup();
  assert.equal(search(t0, 'Grep', { pattern: 'foo' }, { transcript_path: t0.main(1_000) }).stdout, '');
  const f = setup();
  assert.equal(search(f, 'Grep', { pattern: 'foo', path: join(f.repo, 'one.txt') }, { transcript_path: f.main(50_000) }).stdout, '');
  const r = setup({ gate: { enabled: true, contextTokens: 10_000, reads: false } });
  assert.equal(search(r, 'Grep', { pattern: 'foo' }, { transcript_path: r.main(50_000) }).stdout, '');
  const d = setup({ gate: { enabled: false, contextTokens: 10_000 } });
  assert.equal(search(d, 'Grep', { pattern: 'foo' }, { transcript_path: d.main(50_000) }).stdout, '');
  const n = setup({ gate: { enabled: true, contextTokens: 10_000 }, roles: { scout: null } });
  assert.equal(search(n, 'Grep', { pattern: 'foo' }, { transcript_path: n.main(50_000) }).stdout, '');
});

test('read gate: the search escape is consumed once; the edit escape does not let a search through', () => {
  const s = setup();
  const t = s.main(50_000);
  arm(s, 'search');
  assert.equal(search(s, 'Grep', { pattern: 'foo' }, { transcript_path: t }).stdout, '');
  assert.ok(!existsSync(escFile(s, 'search')));
  assert.equal(reason(search(s, 'Grep', { pattern: 'foo' }, { transcript_path: t })).permissionDecision, 'deny');
  arm(s, 'edit');
  assert.equal(reason(search(s, 'Grep', { pattern: 'foo' }, { transcript_path: t })).permissionDecision, 'deny');
  assert.ok(existsSync(escFile(s, 'edit')));
});

test('read gate: an armed search escape does not let a gated edit through', () => {
  const s = setup();
  arm(s, 'search');
  const r = s.run('pretooluse-edit.mjs', { tool_name: 'Edit', tool_input: { file_path: join(s.repo, 'one.txt') }, transcript_path: s.main(50_000) });
  assert.equal(reason(r).permissionDecision, 'deny');
  assert.ok(existsSync(escFile(s, 'search')));
});

test('read gate: Bash rg with no path is denied; with a file or piped, allowed', () => {
  const s = setup();
  const t = s.main(50_000);
  assert.equal(reason(bash(s, 'rg foo', { transcript_path: t })).permissionDecision, 'deny');
  assert.ok(!bash(s, 'rg foo src/a.mjs', { transcript_path: t }).stdout.includes('"deny"'));
  assert.ok(!bash(s, 'cat x | rg foo', { transcript_path: t }).stdout.includes('"deny"'));
});

test('read gate: a narrow main-thread Grep reads nothing (unreadable config, no transcript)', () => {
  const s = setup('{not json');
  const r = search(s, 'Grep', { pattern: 'foo', path: join(s.repo, 'one.txt') }, { transcript_path: join(s.dir, 'missing.jsonl') });
  assert.equal(r.status, 0);
  assert.equal(r.stdout, '');
});

test('malformed stdin: exit 0, empty stdout', () => {
  const s = setup();
  const r = s.run('pretooluse-search.mjs', {}, 'not json');
  assert.equal(r.status, 0);
  assert.equal(r.stdout, '');
});

test('hooks.json wires pretooluse-search under Grep|Glob, and no PreToolUse matcher has Read', () => {
  const h = JSON.parse(readFileSync(join(ROOT, 'hooks', 'hooks.json'), 'utf8'));
  const e = h.hooks.PreToolUse.find((x) => x.matcher === 'Grep|Glob');
  assert.ok(e);
  assert.ok(!h.hooks.PreToolUse.some((x) => /\bRead\b/.test(x.matcher ?? '')));
  assert.match(e.hooks[0].command, /pretooluse-search\.mjs/);
});
