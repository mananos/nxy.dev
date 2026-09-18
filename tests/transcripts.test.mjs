// @ts-check
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { FIXTURE, writeFixture } from './fixtures/make-transcript.mjs';
import { listSessions, newIncrementalState, parseSession, readIncremental, resolveSession } from '../scripts/lib/transcripts.mjs';
import { costFor, normalizeModel, toUsage } from '../scripts/lib/pricing.mjs';

const close = (a, b, msg) => assert.ok(Math.abs(a - b) < 1e-9, `${msg}: ${a} vs ${b}`);

test('pricing: normalizeModel and cache-aware costFor', () => {
  assert.equal(normalizeModel('claude-opus-5-20260401[1m]'), 'claude-opus-5');
  assert.equal(normalizeModel('claude-sonnet-5'), 'claude-sonnet-5');
  const u = toUsage({ input_tokens: 1000, cache_creation_input_tokens: 20000, cache_read_input_tokens: 0, output_tokens: 40, cache_creation: { ephemeral_5m_input_tokens: 20000, ephemeral_1h_input_tokens: 0 } });
  close(costFor('claude-opus-5', u) ?? -1, 0.131, 'opus 5 with 5m writes');
  const u1h = toUsage({ input_tokens: 0, cache_creation_input_tokens: 1000, cache_read_input_tokens: 0, output_tokens: 0, cache_creation: { ephemeral_5m_input_tokens: 0, ephemeral_1h_input_tokens: 1000 } });
  close(costFor('claude-opus-5', u1h) ?? -1, 0.01, '1h writes at 2x');
  const fast = toUsage({ input_tokens: 1000, output_tokens: 100, speed: 'fast' });
  close(costFor('claude-opus-5', fast) ?? -1, (1000 * 10 + 100 * 50) / 1e6, 'fast mode');
  assert.equal(costFor('claude-future-9', u), null, 'unknown model → null, never a guess');
  const legacy = toUsage({ input_tokens: 0, cache_creation_input_tokens: 500, output_tokens: 0 });
  assert.equal(legacy.cacheWrite5m, 500, 'no TTL breakdown → assume 5m');
});

test('listSessions / resolveSession', () => {
  const root = mkdtempSync(join(tmpdir(), 'nxy-tx-'));
  writeFixture(root);
  const all = listSessions({ projectsDir: root, all: true });
  assert.equal(all.length, 1);
  assert.equal(all[0].sessionId, FIXTURE.sessionId);
  assert.equal(listSessions({ projectsDir: root, cwd: FIXTURE.cwd }).length, 1);
  assert.equal(listSessions({ projectsDir: root, cwd: '/elsewhere' }).length, 0);
  assert.equal(resolveSession('last', { projectsDir: root, cwd: FIXTURE.cwd })?.sessionId, FIXTURE.sessionId);
  assert.equal(resolveSession('sess-00', { projectsDir: root })?.sessionId, FIXTURE.sessionId, 'prefix');
  assert.equal(resolveSession('nope', { projectsDir: root }), null);
});

test('parseSession: dedupe, subagents, skills, tools, cost', () => {
  const root = mkdtempSync(join(tmpdir(), 'nxy-tx-'));
  writeFixture(root);
  const ref = resolveSession('last', { projectsDir: root, cwd: FIXTURE.cwd });
  assert.ok(ref);
  const s = parseSession(ref, { cacheBreakThreshold: 15000 });

  assert.equal(s.usage.calls, 7, 'streamed duplicate collapsed');
  assert.equal(s.usage.usage.input, 6010);
  assert.equal(s.usage.usage.cacheWrite5m, 20500);
  assert.equal(s.usage.usage.cacheRead, 45100);
  assert.equal(s.usage.usage.output, 760);
  assert.equal(s.usage.usage.thinking, 15);
  assert.equal(s.usage.usd, null, 'unknown model poisons the total');
  assert.deepEqual(s.unknownModels, ['claude-future-9']);

  close(s.byModel['claude-opus-5'].usd ?? -1, 0.159125, 'opus usd');
  close(s.byModel['claude-sonnet-5'].usd ?? -1, 0.006, 'sonnet usd');
  close(s.byModel['claude-haiku-4-5'].usd ?? -1, 0.00506, 'haiku usd');

  assert.equal(s.main.calls, 5);
  assert.equal(s.subagents.calls, 2);
  assert.equal(s.byAgentType.Explore.calls, 2);
  assert.equal(s.byAgentType.Explore.agents, 1);
  assert.equal(s.agents.length, 1);
  assert.equal(s.agents[0].agentType, 'Explore');
  assert.equal(s.agents[0].description, 'Find tests');
  assert.equal(s.agents[0].model, 'claude-haiku-4-5');
  assert.equal(s.agents[0].durationMs, 300);

  assert.equal(s.bySkill['nxy:stats'].calls, 2, 'slash command attributes following calls');
  assert.equal(s.turns, 2);
  assert.equal(s.prompts[1].command, 'nxy:stats');

  assert.equal(s.tools.calls.Read, 1);
  assert.equal(s.tools.calls.Agent, 1);
  assert.equal(s.tools.calls.Bash, 2);
  assert.deepEqual(s.tools.filesRead, ['/home/dev/code/my.app/src/app.js']);
  assert.equal(s.tools.bash.calls, 2);
  assert.equal(s.tools.bash.resultLines, 5);

  assert.equal(s.main.contextPeak, 21500);
  assert.equal(s.cacheBreaks.length, 1);
  assert.equal(s.cacheBreaks[0].uncached, 21000);
  assert.equal(s.wallMs, 12000);
  assert.equal(s.cwd, FIXTURE.cwd);
  assert.equal(s.version, '2.1.274');

  const noSub = parseSession(ref, { includeSubagents: false });
  assert.equal(noSub.usage.calls, 5);
});

test('readIncremental folds streamed growth by delta and resumes from offset', () => {
  const root = mkdtempSync(join(tmpdir(), 'nxy-tx-'));
  const { mainPath } = writeFixture(root);
  const st = newIncrementalState();
  readIncremental(mainPath, st);
  assert.equal(st.calls, 5);
  assert.equal(st.usage.output, 410);
  assert.equal(st.usage.input, 3010);
  assert.equal(st.usdKnown, false);
  assert.equal(st.model, 'claude-future-9');
  const offset = st.offset;
  readIncremental(mainPath, st);
  assert.equal(st.offset, offset, 'nothing new → no change');
  assert.equal(st.calls, 5);
});
