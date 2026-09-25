// @ts-check
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { FIXTURE, writeFixture } from './fixtures/make-transcript.mjs';
import { breakCause, formatBreaks, listSessions, newIncrementalState, parseSession, readIncremental, resolveSession, summarizeBreaks } from '../hosts/claude-code/transcripts.mjs';
import { costFor, emptyUsage, isSyntheticModel, isZeroUsage, normalizeModel, toUsage } from '../core/pricing.mjs';

const close = (a, b, msg) => assert.ok(Math.abs(a - b) < 1e-9, `${msg}: ${a} vs ${b}`);

test('pricing: normalizeModel covers direct API, Bedrock and Vertex ids', () => {
  // direct API
  assert.equal(normalizeModel('claude-opus-5-20260401[1m]'), 'claude-opus-5');
  // A point release must not collapse into its base model: Opus 5.5 is priced differently from
  // Opus 5 ($4/$20 vs $5/$25), so truncating it here would silently misprice every call.
  assert.equal(normalizeModel('claude-opus-5-5'), 'claude-opus-5-5');
  assert.equal(normalizeModel('claude-opus-5-5-20260922'), 'claude-opus-5-5');
  assert.equal(normalizeModel('us.anthropic.claude-opus-5-5-v1:0'), 'claude-opus-5-5');
  assert.equal(normalizeModel('claude-sonnet-5'), 'claude-sonnet-5');
  assert.equal(normalizeModel('claude-sonnet-4-5-20250929'), 'claude-sonnet-4-5');
  assert.equal(normalizeModel('claude-opus-4-1-latest'), 'claude-opus-4-1');
  assert.equal(normalizeModel('Claude-Opus-5'), 'claude-opus-5', 'case-insensitive');
  // Bedrock: region/scope prefix + anthropic. + -vN:M suffix
  assert.equal(normalizeModel('us.anthropic.claude-sonnet-4-5-20250929-v1:0'), 'claude-sonnet-4-5');
  assert.equal(normalizeModel('eu.anthropic.claude-haiku-4-5-20251001-v1:0'), 'claude-haiku-4-5');
  assert.equal(normalizeModel('apac.anthropic.claude-sonnet-4-20250514-v1:0'), 'claude-sonnet-4');
  assert.equal(normalizeModel('global.anthropic.claude-opus-4-6-v1:0'), 'claude-opus-4-6');
  assert.equal(normalizeModel('anthropic.claude-opus-4-1-20250805-v2:0'), 'claude-opus-4-1');
  assert.equal(normalizeModel('anthropic.claude-3-5-haiku-20241022-v1:0'), 'claude-3-5-haiku', 'unknown key stays unknown, no guess');
  // Vertex: model@date
  assert.equal(normalizeModel('claude-sonnet-4-5@20250929'), 'claude-sonnet-4-5');
  assert.equal(normalizeModel('claude-opus-4-1@20250805'), 'claude-opus-4-1');
  // synthetic: untouched, still not a pricing key
  assert.equal(normalizeModel('<synthetic>'), '<synthetic>');
  assert.equal(normalizeModel(''), '');
  assert.equal(isSyntheticModel('<synthetic>'), true);
  assert.equal(isSyntheticModel('<anything-else>'), true);
  assert.equal(isSyntheticModel('claude-opus-5'), false);
  assert.equal(isSyntheticModel(''), false);
});

test('pricing: zero usage costs $0 for any model; unknown model with tokens is null', () => {
  const zero = toUsage({ input_tokens: 0, output_tokens: 0 });
  assert.equal(isZeroUsage(zero), true);
  assert.equal(isZeroUsage(toUsage({ cache_read_input_tokens: 1 })), false);
  assert.equal(costFor('<synthetic>', zero), 0, 'synthetic + zero usage → $0');
  assert.equal(costFor('claude-future-9', zero), 0, 'unknown model + zero usage → $0');
  assert.equal(costFor('claude-opus-5', zero), 0);
  assert.equal(costFor('<synthetic>', toUsage({ output_tokens: 5 })), null, 'synthetic with tokens: never priced');
  assert.equal(costFor('claude-future-9', toUsage({ input_tokens: 10 })), null, 'unknown model with tokens: never priced');
  close(costFor('us.anthropic.claude-sonnet-4-5-20250929-v1:0', toUsage({ input_tokens: 1000, output_tokens: 100 })) ?? -1, (1000 * 3 + 100 * 15) / 1e6, 'Bedrock id priced as claude-sonnet-4-5');
  close(costFor('claude-haiku-4-5@20251001', toUsage({ input_tokens: 1000 })) ?? -1, 0.001, 'Vertex id priced as claude-haiku-4-5');
});

test('pricing: cache-aware costFor', () => {
  const u = toUsage({ input_tokens: 1000, cache_creation_input_tokens: 20000, cache_read_input_tokens: 0, output_tokens: 40, cache_creation: { ephemeral_5m_input_tokens: 20000, ephemeral_1h_input_tokens: 0 } });
  close(costFor('claude-opus-5', u) ?? -1, 0.131, 'opus 5 with 5m writes');
  const u1h = toUsage({ input_tokens: 0, cache_creation_input_tokens: 1000, cache_read_input_tokens: 0, output_tokens: 0, cache_creation: { ephemeral_5m_input_tokens: 0, ephemeral_1h_input_tokens: 1000 } });
  close(costFor('claude-opus-5', u1h) ?? -1, 0.01, '1h writes at 2x');
  // Opus 5.5 is cheaper than Opus 5 and its cache reads are 5% of input, not 10%.
  const cached = toUsage({ input_tokens: 1000, cache_read_input_tokens: 100_000, output_tokens: 500 });
  close(costFor('claude-opus-5-5', cached) ?? -1, (1000 * 4 + 100_000 * 0.2 + 500 * 20) / 1e6, 'opus 5.5 rates');
  assert.notEqual(costFor('claude-opus-5-5', cached), costFor('claude-opus-5', cached), 'and it is not priced as Opus 5');
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

  assert.equal(s.usage.calls, 7, 'streamed duplicate collapsed, <synthetic> zero-usage line not counted');
  assert.equal(s.usage.usage.input, 6010);
  assert.equal(s.usage.usage.cacheWrite5m, 20500);
  assert.equal(s.usage.usage.cacheRead, 45100);
  assert.equal(s.usage.usage.output, 760);
  assert.equal(s.usage.usage.thinking, 15);
  // one unknown-priced call → the total is the known sum, flagged partial (never a bare ?)
  close(s.usage.usd, 0.159125 + 0.006 + 0.00506, 'known part of the total');
  assert.equal(s.usage.usdPartial, true);
  assert.deepEqual(s.usage.unknownModels, ['claude-future-9']);
  assert.deepEqual(s.unknownModels, ['claude-future-9'], '<synthetic> never listed as unknown');
  assert.equal(s.main.usdPartial, true, 'the unknown call is in the main transcript');
  assert.equal(s.subagents.usdPartial, false);
  assert.deepEqual(s.subagents.unknownModels, []);

  assert.ok(!('<synthetic>' in s.byModel), '<synthetic> with zero usage is not a model row');
  close(s.byModel['claude-opus-5'].usd, 0.159125, 'opus usd');
  assert.equal(s.byModel['claude-opus-5'].usdPartial, false);
  close(s.byModel['claude-sonnet-5'].usd, 0.006, 'sonnet usd');
  close(s.byModel['claude-haiku-4-5'].usd, 0.00506, 'haiku usd');
  assert.equal(s.byModel['claude-future-9'].usd, 0, 'nothing known for the unknown model');
  assert.equal(s.byModel['claude-future-9'].usdPartial, true);
  assert.equal(s.agents[0].usdPartial, false);

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
  assert.deepEqual(s.tools.filesEdited.sort(), ['/home/dev/code/my.app/src/app.js', '/home/dev/code/my.app/src/health.js', '/home/dev/code/my.app/tests/health.test.js'], 'distinct files, main + subagent');
  assert.deepEqual(s.tools.filesEditedMain.sort(), ['/home/dev/code/my.app/src/app.js', '/home/dev/code/my.app/src/health.js'], 'two Edits on app.js count once');
  assert.equal(s.tools.calls.Edit, 2);
  assert.equal(s.tools.calls.Write, 2);
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
  assert.equal(st.calls, 5, '<synthetic> zero-usage line is not a call');
  assert.equal(st.usage.output, 410);
  assert.equal(st.usage.input, 3010);
  assert.equal(st.usdUnknown, 1, 'only claude-future-9 stays unpriced');
  close(st.usd, 0.159125 + 0.006, 'known part keeps accumulating');
  assert.equal(st.model, 'claude-future-9', '<synthetic> never becomes the displayed model');
  const offset = st.offset;
  readIncremental(mainPath, st);
  assert.equal(st.offset, offset, 'nothing new → no change');
  assert.equal(st.calls, 5);
});

test('readIncremental tracks turns, the current turn cost and the last call timestamp', () => {
  const root = mkdtempSync(join(tmpdir(), 'nxy-tx-'));
  const { mainPath } = writeFixture(root);
  const st = newIncrementalState();
  readIncremental(mainPath, st);
  assert.equal(st.turns, 2, 'two human prompts; tool results do not count');
  // second turn = req_4 (sonnet, priced) + req_5 (unknown) — the first turn's cost is not carried over
  close(st.turnUsd, 0.006, 'turn cost restarts at each human prompt');
  assert.equal(st.turnUsdUnknown, 1);
  assert.equal(st.lastTs, Date.parse('2026-09-01T10:00:12.000Z'), 'last real call, not the <synthetic> line');

  // a subagent transcript has a sidechain prompt: never a turn
  const sub = newIncrementalState();
  readIncremental(join(root, FIXTURE.slug, FIXTURE.sessionId, 'subagents', `agent-${FIXTURE.agentId}.jsonl`), sub);
  assert.equal(sub.turns, 0);
  assert.equal(sub.calls, 2);

  // subagent attributed to the parent's turn: the fixture agent ran at t=5.1s, during turn 1
  // (prompt at t=0); turn 2 opened at t=10s → nothing of the agent belongs to turn 2
  const agentPath = join(root, FIXTURE.slug, FIXTURE.sessionId, 'subagents', `agent-${FIXTURE.agentId}.jsonl`);
  const ag = newIncrementalState();
  readIncremental(agentPath, ag, { turnSinceTs: st.turnStartTs });
  assert.equal(st.turnStartTs, Date.parse('2026-09-01T10:00:10.000Z'), 'turn start = timestamp of the last human prompt');
  assert.equal(ag.calls, 2, 'the whole agent history is still folded into the session totals');
  assert.equal(ag.turnUsd, 0, 'but none of it is charged to the current turn (rebuilt cache never over-charges)');
  const agTurn1 = newIncrementalState();
  readIncremental(agentPath, agTurn1, { turnSinceTs: Date.parse('2026-09-01T10:00:00.000Z') });
  assert.ok(agTurn1.turnUsd > 0, 'same agent seen from turn 1 is charged to turn 1');
  // parent's turn changes → agent's turn spend restarts even with nothing new to read
  readIncremental(agentPath, agTurn1, { turnSinceTs: st.turnStartTs });
  assert.equal(agTurn1.turnUsd, 0, 'new parent turn resets the agent turn spend');

  // caches written by 0.1.1 lack the new fields: merging over a fresh state keeps them defined
  const legacy = { offset: 0, partial: '', seen: {}, usage: st.usage, usd: 0, usdUnknown: 0, model: null, calls: 0 };
  const merged = { ...newIncrementalState(), ...legacy };
  assert.equal(merged.turns, 0);
  assert.equal(merged.lastTs, null);
  assert.equal(merged.turnStartTs, null);
});

test('cache breaks: attributed to idle, a slow subagent, a compaction, or other', () => {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-breaks-'));
  const t0 = Date.parse('2026-09-01T10:00:00Z');
  const at = (min) => new Date(t0 + min * 60_000).toISOString();
  const call = (id, min, usage, model = 'claude-sonnet-4-6') => ({
    type: 'assistant', uuid: `a${id}`, timestamp: at(min), requestId: `r${id}`,
    message: { id: `msg_${id}`, model, usage: { input_tokens: 0, output_tokens: 10, cache_read_input_tokens: 0, cache_creation_input_tokens: 0, ...usage } },
  });
  const write1h = (n) => ({ cache_creation_input_tokens: n, cache_creation: { ephemeral_1h_input_tokens: n, ephemeral_5m_input_tokens: 0 } });
  const write5m = (n) => ({ cache_creation_input_tokens: n, cache_creation: { ephemeral_1h_input_tokens: 0, ephemeral_5m_input_tokens: n } });
  const lines = [
    call(1, 0, write5m(200_000)),                                                    // start
    { type: 'assistant', uuid: 'a1b', timestamp: at(0), message: { id: 'msg_x', model: 'claude-sonnet-4-6', content: [{ type: 'tool_use', id: 'tu1', name: 'Agent', input: { subagent_type: 'nxy:implementer' } }] } },
    { type: 'user', uuid: 'u1', timestamp: at(9), message: { content: [{ type: 'tool_result', tool_use_id: 'tu1' }] }, toolUseResult: { agentId: 'ag1' } },
    call(2, 9, write5m(210_000)),                                                    // waited 9 min on a subagent, 5m TTL
    call(3, 10, { cache_read_input_tokens: 210_000 }),                               // warm
    call(4, 190, write1h(220_000)),                                                  // 3h pause, even the 1h TTL died
    { type: 'system', subtype: 'compact_boundary', uuid: 's1', timestamp: at(191) },
    call(5, 192, write5m(150_000)),                                                  // compaction: new prefix
    call(6, 193, write5m(160_000)),                                                  // cache alive, prefix changed
    call(7, 194, write5m(170_000)),                                                  // same: other
  ];
  const path = join(dir, 'sess.jsonl');
  writeFileSync(path, lines.map((l) => JSON.stringify(l)).join('\n') + '\n');
  const s = parseSession({ sessionId: 'sess', path, project: 'p', agentsDir: join(dir, 'sess', 'subagents'), mtimeMs: 0, size: 0 }, { cacheBreakThreshold: 100_000 });
  assert.deepEqual(s.cacheBreaks.map((b) => b.cause), ['start', 'subagent', 'idle', 'compact', 'other', 'other']);
  assert.deepEqual(s.cacheBreaks.map((b) => b.ttl), ['5m', '5m', '1h', '5m', '5m', '5m']);
  assert.ok(s.cacheBreaks.every((b) => typeof b.usd === 'number' && b.usd > 0), 'each rebuild is priced');

  const sum = summarizeBreaks(s.cacheBreaks);
  assert.equal(sum.byCause.subagent.count, 1);
  assert.equal(sum.byCause.idle.tokens, 220_000);
  close(sum.avoidableUsd, sum.byCause.idle.usd + sum.byCause.subagent.usd, 'avoidable = idle + subagent');
  const text = formatBreaks(sum, 100, (n) => String(n)).join('\n');
  assert.match(text, /idle 1 · 220000/);
  assert.match(text, /5m ×5 · 1h ×1/);
  assert.match(text, /% of cost\)/);

  assert.equal(breakCause({ gapMs: 4 * 60_000, after: 'subagent', usage: { ...emptyUsage(), cacheWrite5m: 1 } }), 'other',
    'a short subagent did not outlive the cache: waiting on it is not why it broke');
});
