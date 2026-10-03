// @ts-check
/** The `## Coverage` section and the `(inventory)` marks of a review packet. */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { gitBranch } from '../core/paths.mjs';
import { clearBaseline, readBaseline, reviewFiles } from '../hosts/claude-code/baseline.mjs';
import { coverageLines, inventoryWhy } from '../hosts/claude-code/coverage.mjs';
import { takeInventory } from '../hosts/claude-code/inventory.mjs';
import { resolveRepos } from '../hosts/claude-code/repos.mjs';
import { reviewPacket } from '../core/review.mjs';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const MEM = join(ROOT, 'hosts', 'claude-code', 'entries', 'mem.mjs');
const hasGit = spawnSync('git', ['--version'], { encoding: 'utf8' }).status === 0;
const PLAN = ['## Plan', 'Goal: x', '### Batch 1 — api: files', '- `a.js:1` — change it', 'Accept: `node -v`'].join('\n');

function setup() {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-cov-'));
  const A = join(dir, 'A');
  mkdirSync(A, { recursive: true });
  const env = { ...process.env, NXY_HOME: join(dir, 'home'), CLAUDE_PROJECT_DIR: A };
  writeFileSync(join(A, 'a.js'), 'before\n');
  const r = spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', MEM, 'handoff', 'plan', '--cwd', A], { input: PLAN, encoding: 'utf8', env, cwd: A });
  assert.equal(r.status, 0, r.stderr);
  return { A };
}
const packet = (A, rv) => reviewPacket({
  planHash: 'h', plan: PLAN, conventions: [], lenses: [], files: rv.files, recordCmd: 'rec',
  coverage: coverageLines(rv.coverage, rv, Object.keys(readBaseline(A, gitBranch(A)))),
});

test('packet of a non-git project: (inventory) file explained, ## Coverage names copies, inventory and what is not covered', () => {
  const { A } = setup();
  writeFileSync(join(A, 'a.js'), 'after\n');
  writeFileSync(join(A, 'fresh.js'), 'one\n');
  const rv = reviewFiles(A, gitBranch(A));
  const text = packet(A, rv);
  assert.match(text, /- fresh\.js \(new \(inventory\)/);
  assert.match(text, /\(inventory\) files changed with no copy and no git/);
  const cov = text.slice(text.indexOf('## Coverage'), text.indexOf('## Your answer'));
  assert.match(cov, /copies: 1 file/);
  assert.match(cov, /inventory: \d+ file\(s\) at approval/);
  assert.match(cov, /Not covered: files in skipped folders/);
  assert.match(cov, /over the copy limit/);
});

test('packet: inventory skipped says too many files and only copies cover the repo', () => {
  const { A } = setup();
  clearBaseline(A, gitBranch(A));
  writeFileSync(join(A, 'b.js'), 'b\n');
  writeFileSync(join(A, 'c.js'), 'c\n');
  takeInventory(A, gitBranch(A), resolveRepos(A, []).map((r) => r.root), { cap: 2 });
  const rv = reviewFiles(A, gitBranch(A), 'h');
  const lines = coverageLines(rv.coverage, rv, []);
  assert.match(lines.join('\n'), /inventory skipped: too many files/);
});

test('packet of a git repo has no ## Coverage; emptyWhy shows the inventory line', { skip: !hasGit }, () => {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-cov-git-'));
  execFileSync('git', ['init', '-q'], { cwd: dir });
  const text = reviewPacket({ planHash: 'h', plan: PLAN, conventions: [], lenses: [], files: [], recordCmd: 'rec', coverage: coverageLines([], {}, []) });
  assert.doesNotMatch(text, /## Coverage/);
  assert.equal(inventoryWhy([]), null);
  assert.match(inventoryWhy([{ root: dir, name: 'A', files: 5, now: 6, skipped: null }]) ?? '', /^inventory: A 5 at approval, 6 now/);
});
