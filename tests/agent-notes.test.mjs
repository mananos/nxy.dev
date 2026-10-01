// @ts-check
/**
 * Background agents: a launch is recognised in the three shapes the runtime can give, and a note is
 * delivered to exactly one reader even when several hooks look for it at once.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdtempSync, readdirSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { isAsyncLaunch, markAsync, pushNote, takeAsync, takeNotes } from '../hosts/claude-code/agent-notes.mjs';

const MODULE = pathToFileURL(join(dirname(fileURLToPath(import.meta.url)), '..', 'hosts', 'claude-code', 'agent-notes.mjs')).href;
const tmp = () => mkdtempSync(join(tmpdir(), 'nxy-notes-'));

test('isAsyncLaunch: object, status and text shapes', () => {
  assert.equal(isAsyncLaunch({ isAsync: true, agentId: 'a1' }), true);
  assert.equal(isAsyncLaunch({ status: 'async_launched' }), true);
  assert.equal(isAsyncLaunch('Async agent launched successfully. agentId: x'), true);
  assert.equal(isAsyncLaunch([{ type: 'text', text: 'Async agent launched successfully.' }]), true);
  assert.equal(isAsyncLaunch({ content: [{ type: 'text', text: 'Async agent launched successfully.' }] }), true);
  assert.equal(isAsyncLaunch({ status: 'completed', content: [{ type: 'text', text: 'done' }] }), false);
  assert.equal(isAsyncLaunch('All done'), false);
  assert.equal(isAsyncLaunch(null), false);
  assert.equal(isAsyncLaunch(undefined), false);
});

test('markAsync/takeAsync: consumed once, keyed by role identity', () => {
  const cwd = tmp();
  try {
    markAsync(cwd, 'implementer:batch-3');
    assert.equal(takeAsync(cwd, 'implementer:batch-4'), false);
    assert.equal(takeAsync(cwd, 'implementer:batch-3'), true);
    assert.equal(takeAsync(cwd, 'implementer:batch-3'), false);
  } finally {
    rmSync(cwd, { recursive: true, force: true });
  }
});

test('notes: delivered in order, duplicates dropped, empty afterwards', () => {
  const cwd = tmp();
  try {
    assert.deepEqual(takeNotes(cwd), []);
    pushNote(cwd, 'first');
    pushNote(cwd, 'first');
    pushNote(cwd, 'second');
    pushNote(cwd, '  ');
    assert.deepEqual(takeNotes(cwd), ['first', 'second']);
    assert.deepEqual(takeNotes(cwd), []);
  } finally {
    rmSync(cwd, { recursive: true, force: true });
  }
});

test('notes: 8 parallel readers deliver each note exactly once', async () => {
  const cwd = tmp();
  try {
    for (let i = 0; i < 10; i++) pushNote(cwd, `note ${i}`);
    const script = `import(${JSON.stringify(MODULE)}).then((m) => process.stdout.write(JSON.stringify(m.takeNotes(${JSON.stringify(cwd)}))));`;
    const runs = Array.from({ length: 8 }, () => new Promise((resolve) => {
      const child = spawn(process.execPath, ['--disable-warning=ExperimentalWarning', '-e', script], { stdio: ['ignore', 'pipe', 'inherit'] });
      let out = '';
      child.stdout.on('data', (d) => { out += d; });
      child.on('close', () => resolve(JSON.parse(out || '[]')));
    }));
    const all = (await Promise.all(runs)).flat();
    assert.equal(all.length, 10);
    assert.equal(new Set(all).size, 10);
    const left = readdirSync(join(cwd, '.nxy', 'local', 'notes'));
    assert.deepEqual(left, []);
  } finally {
    rmSync(cwd, { recursive: true, force: true });
  }
});
