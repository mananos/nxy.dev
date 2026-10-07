// @ts-check
import assert from 'node:assert/strict';
import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { test } from 'node:test';
import { ticketDescription, TICKET_DIR } from '../core/orchestrator.mjs';
import { orchDir, writeOrchFile } from '../hosts/claude-code/orch-state.mjs';
import { recordBatch } from '../hosts/claude-code/verify-state.mjs';
import { sandbox } from './sandbox.mjs';

const PLAN = [
  '## Plan',
  'Goal: orchestrated',
  '### Batch 1 — api',
  '- `src/a.ts` — change',
  'Accept: manual — looks right',
  '### Batch 2 — web',
  '- `src/b.ts` — change',
  'Accept: manual — looks right',
].join('\n');

const marker = (s, ts = Date.now()) => writeOrchFile(s.repo, 'active.json', { ts });
const ticket = (s, nonce) => {
  mkdirSync(join(orchDir(s.repo), TICKET_DIR), { recursive: true });
  writeFileSync(join(orchDir(s.repo), TICKET_DIR, nonce), '1');
  return ticketDescription(nonce, 'x');
};
const pre = (s, subagent_type, prompt, description) => {
  const out = s.hook('pretooluse-agent.mjs', { tool_name: 'Agent', transcript_path: s.main(), tool_input: { subagent_type, prompt, ...(description ? { description } : {}) } });
  return out.trim() ? JSON.parse(out).hookSpecificOutput : null;
};

test('marker: main-thread implementer, tester and reviewer are denied; other agents pass', () => {
  const s = sandbox(PLAN);
  marker(s);
  for (const a of ['nxy:implementer', 'nxy:tester', 'nxy:reviewer']) {
    const r = pre(s, a, 'Batch 1 — go');
    assert.equal(r?.permissionDecision, 'deny', a);
    assert.match(r?.permissionDecisionReason, /orchestrator is running plan/);
    assert.match(r?.permissionDecisionReason, /release/);
  }
  for (const a of ['nxy:planner', 'nxy:scout', 'Explore']) assert.notEqual(pre(s, a, 'look')?.permissionDecision, 'deny', a);
});

test('a valid ticket is allowed with the handoff, once', () => {
  const s = sandbox(PLAN);
  marker(s);
  const d = ticket(s, 'abc123');
  const r = pre(s, 'nxy:implementer', 'Batch 1 — go', d);
  assert.equal(r?.permissionDecision, 'allow');
  assert.match(r?.updatedInput.prompt, /Batch 1 — go/);
  assert.match(r?.updatedInput.prompt, /Goal: orchestrated/, 'handoff attached');
  assert.equal(pre(s, 'nxy:implementer', 'Batch 1 — go', d)?.permissionDecision, 'deny', 'second use');
});

test('a marker past its TTL does not deny; no marker leaves the dispatch untouched', () => {
  const s = sandbox(PLAN);
  marker(s, Date.now() - 3 * 60 * 60 * 1000);
  assert.equal(pre(s, 'nxy:implementer', 'Batch 1 — go')?.permissionDecision, 'allow');
  const t = sandbox(PLAN);
  assert.equal(pre(t, 'nxy:implementer', 'Batch 1 — go', ticketDescription('zzz', 'x'))?.permissionDecision, 'allow');
});

test('ticketed batch 2 behind a red batch 1: denied until the pane waved it through', () => {
  const s = sandbox(PLAN);
  recordBatch(s.repo, 'feature/x', s.hash, 1, { status: 'fail', error: 'accept-failed', ts: Date.now() });
  marker(s);
  assert.equal(pre(s, 'nxy:implementer', 'Batch 2 — go', ticket(s, 'n1'))?.permissionDecision, 'deny');
  writeOrchFile(s.repo, 'state.json', { continued: [1], acked: [], launched: {} });
  assert.equal(pre(s, 'nxy:implementer', 'Batch 2 — go', ticket(s, 'n2'))?.permissionDecision, 'allow');
});

test('PostToolUse on a ticket sentinel prints nothing', () => {
  const s = sandbox(PLAN);
  const out = s.hook('posttooluse-agent.mjs', {
    tool_name: 'Agent',
    tool_input: { subagent_type: 'nxy:implementer', prompt: 'Batch 1 — go', description: ticketDescription('q1', 'b1') },
    tool_response: { isAsync: true, status: 'async_launched' },
  });
  assert.equal(out.trim(), '');
});
