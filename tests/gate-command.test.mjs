// @ts-check
/**
 * `/nxy:gate` entry: `reads on|off`, `once` vs `once search`, and the `searches:` counts.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const ENTRY = join(ROOT, 'hosts', 'claude-code', 'entries', 'gate.mjs');

function repo() {
  const cwd = mkdtempSync(join(tmpdir(), 'nxy-gatecmd-'));
  mkdirSync(join(cwd, '.nxy'), { recursive: true });
  writeFileSync(join(cwd, '.nxy', 'config.json'), JSON.stringify({ metrics: { projectsDir: join(cwd, 'no-transcripts') } }));
  return cwd;
}

function run(cwd, ...args) {
  const r = spawnSync(process.execPath, [ENTRY, ...args, '--cwd', cwd], { encoding: 'utf8', env: { ...process.env, CLAUDE_CODE_SESSION_ID: '' } });
  assert.equal(r.status, 0, r.stderr);
  return r.stdout;
}

const local = (cwd, f) => join(cwd, '.nxy', 'local', f);

test('gate reads off/on writes the repo config and status shows it', () => {
  const cwd = repo();
  assert.match(run(cwd, 'status'), /reads:\s+on/);
  const off = run(cwd, 'reads', 'off');
  assert.match(off, /observed only/);
  assert.equal(JSON.parse(readFileSync(join(cwd, '.nxy', 'config.json'), 'utf8')).gate.reads, false);
  assert.match(run(cwd, 'status'), /reads:\s+off/);
  assert.match(run(cwd, 'reads', 'on'), /stopped past the threshold again/);
  assert.equal(JSON.parse(readFileSync(join(cwd, '.nxy', 'config.json'), 'utf8')).gate.reads, true);
  assert.match(run(cwd, 'status'), /reads:\s+on/);
  assert.match(run(cwd, 'reads'), /unknown action[^\n]*once \[search\][^\n]*reads on\|off/);
});

test('gate once keeps its 1.2.0 text; once search arms only the search file', () => {
  const cwd = repo();
  const once = run(cwd, 'once');
  assert.match(once, /the next main-thread edit goes through unblocked/);
  assert.match(once, /Covers the plan, threshold and no-handoff refusals\./);
  assert.ok(existsSync(local(cwd, 'gate-escape.json')));
  assert.ok(!existsSync(local(cwd, 'gate-escape-search.json')));

  const cwd2 = repo();
  const s = run(cwd2, 'once', 'search');
  assert.match(s, /the next broad main-thread search goes through unstopped \(expires in 5 min/);
  assert.ok(existsSync(local(cwd2, 'gate-escape-search.json')));
  assert.ok(!existsSync(local(cwd2, 'gate-escape.json')));
  assert.match(run(cwd2, 'status'), /search escape:\s+armed/);
});

test('status prints the searches counts apart from the edit decisions', () => {
  const cwd = repo();
  assert.match(run(cwd, 'status'), /searches:\s+0 stopped, 0 let through/);
  const now = Date.now();
  const rows = [
    { ts: now, tool: 'Edit', allow: false, reason: 'over-threshold', contextTokens: 150_000, threshold: 100_000 },
    { ts: now, tool: 'Grep', kind: 'read', allow: false, reason: 'broad-search', contextTokens: 150_000, threshold: 100_000 },
    { ts: now, tool: 'Grep', kind: 'read', allow: false, reason: 'broad-search', contextTokens: 150_000, threshold: 100_000 },
    { ts: now, tool: 'Bash', kind: 'read', allow: true, reason: 'escape', contextTokens: 150_000, threshold: 100_000 },
  ];
  mkdirSync(join(cwd, '.nxy', 'local', 'metrics'), { recursive: true });
  writeFileSync(local(cwd, join('metrics', 'gate.jsonl')), rows.map((r) => JSON.stringify(r)).join('\n') + '\n');
  const out = run(cwd, 'status');
  assert.match(out, /searches:\s+2 stopped, 1 let through by `\/nxy:gate once search`/);
  assert.match(out, /decisions:\s+1 blocked of 1 edits/);
});
