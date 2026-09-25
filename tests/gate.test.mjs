// @ts-check
/**
 * The write gate. These tests pin down the two things that decide whether it helps or annoys:
 * *when* it blocks (context size, not change size) and *when it must not* (subagents, unknown
 * context, no implementer, an armed escape). A gate that fails closed would make the plugin
 * unusable the first time a transcript format changes, so fail-open is tested explicitly.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { decideGate, describeGate } from '../core/gate.mjs';
import { isSubagentTranscript, lastContextTokens } from '../hosts/claude-code/context.mjs';
import { armEscape, clearEscape, consumeEscape, escapeUntil } from '../hosts/claude-code/gate-state.mjs';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const HOOK = join(ROOT, 'hosts', 'claude-code', 'hooks', 'pretooluse-edit.mjs');

const CFG = { enabled: true, contextTokens: 100_000, escapeMinutes: 5 };
const base = { isSubagent: false, hasImplementer: true, escapeActive: false };

/** A transcript holding one assistant message with the given usage split. */
function transcript(dir, name, { input = 0, cacheRead = 0, cacheCreation = 0 }) {
  const p = join(dir, name);
  mkdirSync(dirname(p), { recursive: true });
  writeFileSync(p, JSON.stringify({
    type: 'assistant',
    message: { usage: { input_tokens: input, cache_read_input_tokens: cacheRead, cache_creation_input_tokens: cacheCreation } },
  }) + '\n', 'utf8');
  return p;
}

test('decideGate: blocks on context size, and only on context size', () => {
  assert.equal(decideGate({ ...base, contextTokens: 20_000 }, CFG).allow, true, 'small session edits freely');
  assert.equal(decideGate({ ...base, contextTokens: 99_999 }, CFG).allow, true, 'just under the threshold');

  const blocked = decideGate({ ...base, contextTokens: 153_000, file: 'src/a.ts' }, CFG);
  assert.equal(blocked.allow, false);
  assert.equal(blocked.reason, 'over-threshold');
  assert.match(blocked.message || '', /153k > 100k/, 'the reason states both numbers');
  assert.match(blocked.message || '', /implementer/, 'and names what to do instead');
  assert.match(blocked.message || '', /src\/a\.ts/, 'and which file it refused');
  assert.match(blocked.message || '', /\/nxy:gate once/, 'and how to override it');

  // The threshold is a boundary, not a range: exactly at it, it blocks.
  assert.equal(decideGate({ ...base, contextTokens: 100_000 }, CFG).allow, false);
});

test('decideGate: every fail-open path', () => {
  /** @type {{input: any, cfg: any, reason: string, why: string}[]} */
  const cases = [
    { input: { ...base, contextTokens: 153_000, isSubagent: true }, cfg: CFG, reason: 'subagent', why: 'the implementer must be able to write' },
    { input: { ...base, contextTokens: 153_000, hasImplementer: false }, cfg: CFG, reason: 'no-implementer', why: 'blocking with nowhere to delegate is just nagging' },
    { input: { ...base, contextTokens: 153_000, escapeActive: true }, cfg: CFG, reason: 'escape', why: 'an armed escape lets one edit through' },
    { input: { ...base, contextTokens: null }, cfg: CFG, reason: 'context-unknown', why: 'unreadable context never blocks' },
    { input: { ...base, contextTokens: NaN }, cfg: CFG, reason: 'context-unknown', why: 'NaN is not a context size' },
    { input: { ...base, contextTokens: 153_000 }, cfg: { ...CFG, enabled: false }, reason: 'disabled', why: 'config off means off' },
    { input: { ...base, contextTokens: 153_000 }, cfg: { ...CFG, contextTokens: 0 }, reason: 'no-threshold', why: 'a zero threshold is not "block everything"' },
  ];
  for (const { input, cfg, reason, why } of cases) {
    const v = decideGate(input, cfg);
    assert.equal(v.allow, true, `${why}: expected allow`);
    assert.equal(v.reason, reason, why);
  }
  assert.equal(decideGate(/** @type {any} */ ({ ...base, contextTokens: 153_000 }), /** @type {any} */ (null)).allow, true, 'missing config never blocks');
});

test('context: reads the newest usage, and says null rather than guessing', () => {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-ctx-'));
  assert.equal(lastContextTokens(join(dir, 'nope.jsonl')), null, 'missing file → null');
  assert.equal(lastContextTokens(null), null);
  assert.equal(lastContextTokens(transcript(dir, 'empty.jsonl', {})), null, 'zero usage is not a context size');

  assert.equal(lastContextTokens(transcript(dir, 'one.jsonl', { input: 1200, cacheRead: 151_000, cacheCreation: 800 })), 153_000,
    'input + cache_read + cache_creation, same as the statusline');

  // Several messages: the last one with usage wins, and non-assistant lines are ignored.
  const many = join(dir, 'many.jsonl');
  writeFileSync(many, [
    JSON.stringify({ type: 'assistant', message: { usage: { input_tokens: 10, cache_read_input_tokens: 90 } } }),
    JSON.stringify({ type: 'user', message: { content: 'hi' } }),
    JSON.stringify({ type: 'assistant', message: { usage: { input_tokens: 500, cache_read_input_tokens: 40_000 } } }),
    '{ broken json',
    '',
  ].join('\n'), 'utf8');
  assert.equal(lastContextTokens(many), 40_500, 'newest usage wins; malformed lines are skipped');

  assert.equal(isSubagentTranscript('/p/sess/subagents/agent-1.jsonl'), true);
  assert.equal(isSubagentTranscript('C:\\p\\sess\\subagents\\agent-1.jsonl'), true, 'windows separators too');
  assert.equal(isSubagentTranscript('/p/sess.jsonl'), false);
  assert.equal(isSubagentTranscript(undefined), false);
});

test('escape: one use, expires, and never silently stays open', () => {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-esc-'));
  assert.equal(escapeUntil(dir), null, 'nothing armed → nothing to consume');
  assert.equal(consumeEscape(dir), false);

  armEscape(dir, 5);
  assert.ok(escapeUntil(dir), 'armed');
  assert.equal(consumeEscape(dir), true, 'first edit uses it');
  assert.equal(consumeEscape(dir), false, 'the second edit does not — it is one-shot');

  // A stale marker must not act as a permanent gate-off switch.
  armEscape(dir, 5);
  writeFileSync(join(dir, '.nxy', 'local', 'gate-escape.json'), JSON.stringify({ until: Date.now() - 1000 }), 'utf8');
  assert.equal(escapeUntil(dir), null, 'expired marker is ignored');
  assert.equal(consumeEscape(dir), false);

  armEscape(dir, 5);
  clearEscape(dir);
  assert.equal(escapeUntil(dir), null, '/nxy:gate on clears any armed escape');

  // A marker claiming a far-future expiry is clamped to the configured window.
  writeFileSync(join(dir, '.nxy', 'local', 'gate-escape.json'), JSON.stringify({ until: Date.now() + 999 * 60_000 }), 'utf8');
  const until = escapeUntil(dir, 5);
  assert.ok(until && until <= Date.now() + 5 * 60_000 + 1000, 'an escape cannot outlive its own window');
});

test('describeGate: states threshold, implementer availability and escape', () => {
  const text = describeGate(CFG, { contextTokens: 153_000, hasImplementer: true, escapeUntil: null });
  assert.match(text, /threshold 100k/);
  assert.match(text, /153k/);
  assert.match(text, /implementer:\s+available/);
  const off = describeGate({ ...CFG, enabled: false }, { contextTokens: null, hasImplementer: false, escapeUntil: null });
  assert.match(off, /DISABLED/);
  assert.match(off, /never blocks/, 'without an implementer it says the gate is inert');
});

test('hook end to end: allows, denies, and never blocks a subagent', () => {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-hook-'));
  // The gate reads config from the repo; this temp dir has none, so plugin defaults apply.
  const run = (transcriptPath, tool = 'Edit') => execFileSync(process.execPath, [HOOK], {
    input: JSON.stringify({ tool_name: tool, cwd: dir, transcript_path: transcriptPath, tool_input: { file_path: 'x.ts' } }),
    encoding: 'utf8',
    env: { ...process.env, NXY_HOME: dir },
  });

  assert.equal(run(transcript(dir, 'small.jsonl', { cacheRead: 19_000 })).trim(), '',
    'under the threshold the hook writes nothing at all — zero tokens on the happy path');

  const denied = JSON.parse(run(transcript(dir, 'big.jsonl', { cacheRead: 153_000 })));
  assert.equal(denied.hookSpecificOutput.permissionDecision, 'deny');
  assert.match(denied.hookSpecificOutput.permissionDecisionReason, /implementer/);

  assert.equal(run(transcript(dir, 'sess/subagents/agent-1.jsonl', { cacheRead: 153_000 })).trim(), '',
    'the implementer edits with the same context that blocks the main thread');

  assert.equal(run(transcript(dir, 'big.jsonl', { cacheRead: 153_000 }), 'Read').trim(), '',
    'only write tools are gated');

  assert.equal(execFileSync(process.execPath, [HOOK], { input: 'not json', encoding: 'utf8', env: { ...process.env, NXY_HOME: dir } }).trim(), '',
    'garbage in → fail open, exit 0');
});
