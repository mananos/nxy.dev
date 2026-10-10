// @ts-check
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';
import { ROOT } from './sandbox.mjs';

const LOCATE = join(ROOT, 'hosts', 'claude-code', 'entries', 'locate.mjs');

function box() {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-loc-'));
  const repo = join(dir, 'repo');
  mkdirSync(join(repo, '.git'), { recursive: true });
  mkdirSync(join(repo, 'src'), { recursive: true });
  writeFileSync(join(repo, '.git', 'HEAD'), 'ref: refs/heads/main\n');
  writeFileSync(join(repo, 'src', 'billing.js'), '// billing\n\nexport function computeInvoiceTotal(items) {\n  return items.length;\n}\n');
  const home = join(dir, 'home');
  mkdirSync(home, { recursive: true });
  /** @type {Record<string, string|undefined>} */
  const env = {
    ...process.env,
    NXY_HOME: join(home, '.nxy'),
    HOME: home,
    USERPROFILE: home,
    NXY_CODEGRAPH_PATH: join(dir, 'no-such-codegraph'),
  };
  const run = (/** @type {string[]} */ args) =>
    spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', LOCATE, ...args, '--cwd', repo], { encoding: 'utf8', env });
  return { run };
}

test('locate --json: one JSON line with hits holding the known path:line', () => {
  const { run } = box();
  const r = run(['computeInvoiceTotal', '--json']);
  assert.equal(r.status, 0, r.stderr);
  const lines = r.stdout.trim().split('\n');
  assert.equal(lines.length, 1);
  const j = JSON.parse(lines[0]);
  assert.equal(j.ok, true);
  assert.equal(j.question, 'computeInvoiceTotal');
  assert.ok(Array.isArray(j.degraded));
  assert.ok(Array.isArray(j.terms));
  assert.equal(typeof j.ms, 'number');
  assert.equal(typeof j.codegraph, 'string');
  assert.ok(j.hits.length > 0 && j.hits.length <= 25);
  const hit = j.hits.find((/** @type {any} */ h) => h.path.replace(/\\/g, '/').endsWith('src/billing.js') && h.line === 3);
  assert.ok(hit, JSON.stringify(j.hits));
  assert.ok(['index', 'rg'].includes(hit.source));
  const keys = j.hits.map((/** @type {any} */ h) => `${h.path}:${h.line}`);
  assert.equal(new Set(keys).size, keys.length);
});

test('locate --json: --json before the question still keeps the question', () => {
  const { run } = box();
  const r = run(['--json', 'computeInvoiceTotal']);
  const j = JSON.parse(r.stdout.trim().split('\n').pop() ?? '');
  assert.equal(j.ok, true);
  assert.equal(j.question, 'computeInvoiceTotal');
});

test('locate --json without a question: ok false with a reason', () => {
  const { run } = box();
  const r = run(['--json']);
  assert.equal(r.status, 0, r.stderr);
  const j = JSON.parse(r.stdout.trim());
  assert.equal(j.ok, false);
  assert.equal(typeof j.reason, 'string');
});
