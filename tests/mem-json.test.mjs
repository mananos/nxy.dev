// @ts-check
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, readdirSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';
import { ROOT } from './sandbox.mjs';

const MEM = join(ROOT, 'hosts', 'claude-code', 'entries', 'mem.mjs');

function box() {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-memj-'));
  const repo = join(dir, 'repo');
  mkdirSync(join(repo, '.git'), { recursive: true });
  writeFileSync(join(repo, '.git', 'HEAD'), 'ref: refs/heads/main\n');
  const home = join(dir, 'home');
  mkdirSync(join(home, '.claude'), { recursive: true });
  /** @type {Record<string, string|undefined>} */
  const env = { ...process.env, NXY_HOME: join(home, '.nxy'), HOME: home, USERPROFILE: home };
  delete env.NXY_FILTER;
  delete env.NXY_ENGINE;
  const run = (args, input) => spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', MEM, ...args, '--cwd', repo], { encoding: 'utf8', env, input });
  const json = (args) => {
    const r = run(args);
    assert.equal(r.status, 0, r.stderr);
    const lines = r.stdout.trim().split('\n');
    assert.equal(lines.length, 1, `one JSON line expected, got: ${r.stdout}`);
    return JSON.parse(lines[0]);
  };
  return { dir, repo, run, json };
}

/** @param {string} d @returns {string[]} */
function walk(d) {
  const out = [];
  for (const e of readdirSync(d, { withFileTypes: true })) {
    out.push(join(d, e.name));
    if (e.isDirectory()) out.push(...walk(join(d, e.name)));
  }
  return out;
}

test('mem --json: search, list, get and handoff show print one JSON line', () => {
  const b = box();
  try {
    b.run(['save', 'Use pgbouncer pooling', '--body', 'Pool connections with pgbouncer in transaction mode.', '--keywords', 'database pooling']);
    b.run(['save', 'Retry policy for webhooks', '--body', 'Webhooks retry three times with backoff.', '--type', 'convention']);

    const s = b.json(['search', 'pgbouncer', '--json']);
    assert.equal(s.ok, true);
    assert.equal(s.query, 'pgbouncer');
    assert.equal(s.results.length, 1);
    const r0 = s.results[0];
    for (const k of ['id', 'scope', 'area', 'type', 'title', 'updated', 'keywords']) assert.ok(k in r0, k);
    assert.equal(r0.title, 'Use pgbouncer pooling');

    const l = b.json(['list', '--json']);
    assert.equal(l.ok, true);
    assert.equal(l.results.length, 2);
    assert.equal(b.json(['list', '--limit', '1', '--json']).results.length, 1);

    const g = b.json(['get', r0.id, '--json']);
    assert.equal(g.ok, true);
    assert.equal(g.memory.id, r0.id);
    assert.equal(g.memory.private, false);
    assert.match(g.memory.body, /transaction mode/);
    assert.ok(Array.isArray(g.edges));
    assert.ok(Array.isArray(g.files));

    const none = b.json(['handoff', 'show', '--json']);
    assert.equal(none.ok, true);
    assert.equal(none.exists, false);
    assert.equal(none.label, 'main');

    const saved = b.run(['handoff', 'save'], 'route: implementer\n## Done\n- x\n## Next\n- y\n');
    assert.equal(saved.status, 0, saved.stderr);
    const h = b.json(['handoff', 'show', '--json']);
    assert.equal(h.ok, true);
    assert.equal(h.exists, true);
    assert.equal(h.label, 'main');
    assert.match(h.body, /## Next/);
    assert.equal(typeof h.updated, 'number');
    assert.equal(h.planHash, null);
  } finally {
    rmSync(b.dir, { recursive: true, force: true });
  }
});

test('mem get --json: unknown id is ok:false and browsing leaves no recall state', () => {
  const b = box();
  try {
    const saved = b.run(['save', 'Some note', '--body', 'body text']);
    const id = /saved: (\S+)/.exec(saved.stdout)?.[1] ?? '';
    assert.ok(id);
    const miss = b.json(['get', 'nope-123', '--json']);
    assert.deepEqual(miss, { ok: false, reason: 'no memory with id nope-123' });
    const g = b.json(['get', id, '--json']);
    assert.equal(g.ok, true);
    assert.deepEqual(walk(b.dir).filter((p) => /recall/i.test(p)), []);
  } finally {
    rmSync(b.dir, { recursive: true, force: true });
  }
});
