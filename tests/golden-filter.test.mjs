// @ts-check
import { before, test } from 'node:test';
import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { generate } from '../scripts/golden/filter.mjs';
import { CORPUS_DIR, GOLDEN_DIR, loadCorpus } from '../scripts/golden/lib.mjs';
import { haveGo } from '../scripts/golden/hook.mjs';

// one generation for the whole file: it replays every case and spawns the hook per case
/** @type {ReturnType<typeof generate>} */
let result;
before(() => {
  result = generate({ check: true });
});

test('golden files match what the 1.x filter produces now', () => {
  const r = result;
  assert.deepEqual(r.missing, [], 'corpus cases without an rtk answer (node scripts/golden/filter.mjs --record)');
  assert.deepEqual(r.differences, [], 'golden files are stale (node scripts/golden/filter.mjs)');
});

test('hook corpus avoids payloads that trigger the subagent / read-gate paths', () => {
  const cases = loadCorpus('hook-pretooluse-bash');
  assert.ok(cases.length > 0);
  for (const c of cases) {
    for (const k of ['agent_id', 'agent_type', 'transcript_path', 'cwd']) {
      assert.ok(!(k in c.payload), `${c.name} carries ${k}`);
    }
  }
});

test('hook-level golden is checked when go is available', (t) => {
  if (!haveGo()) return t.skip('go not available');
  const r = result;
  assert.equal(r.hookSkipped, null);
  assert.deepEqual(r.differences.filter((d) => d.startsWith('hook-pretooluse-bash')), []);
});

/** @param {string} dir @returns {string[]} */
function jsonFiles(dir) {
  const out = [];
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    const p = join(dir, e.name);
    if (e.isDirectory()) out.push(...jsonFiles(p));
    else if (e.name.endsWith('.json')) out.push(p);
  }
  return out;
}

test('corpus and golden files carry no personal path', () => {
  const personal = [/[A-Za-z]:[\\/]Users[\\/]/, /\/c\/Users\//, /\/home\/[^/~]+\//];
  const files = [...jsonFiles(CORPUS_DIR), ...jsonFiles(GOLDEN_DIR)];
  assert.ok(files.length > 0);
  for (const f of files) {
    const text = readFileSync(f, 'utf8');
    // check decoded strings too: a path may hide behind JSON escapes (`C:\\Users\\x`)
    const decoded = JSON.stringify(JSON.parse(text), null, 1).replace(/\\\\/g, '\\');
    for (const re of personal) {
      assert.ok(!re.test(text) && !re.test(decoded), `${f} matches ${re}`);
    }
  }
});
