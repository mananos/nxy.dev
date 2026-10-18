import { test } from 'node:test';
import assert from 'node:assert/strict';
import { agentTiming, quantile, summarizeTiming, formatAgentTiming } from '../core/metrics/timing.mjs';
import { LOCATE_NUDGE_MARK } from '../core/search-scope.mjs';

const T0 = Date.parse('2026-01-01T00:00:00Z');
const at = (s) => new Date(T0 + s * 1000).toISOString();
/** @returns {any} */
const user = (s) => ({ type: 'user', timestamp: at(s), message: { content: 'go' } });
/** @param {any} [content] @returns {any} */
const asst = (s, req, content = [{ type: 'text', text: 'ok' }]) => ({
  type: 'assistant', timestamp: at(s), requestId: req, message: { id: 'm' + req, model: 'x', content },
});
/** @returns {any} */
const use = (s, req, id, name, input) => asst(s, req, [{ type: 'tool_use', id, name, input }]);
/** @returns {any} */
const result = (s, id, content = 'done') => ({
  type: 'user', timestamp: at(s), message: { content: [{ type: 'tool_result', tool_use_id: id, content }] },
});

test('buckets sum to wall', () => {
  const evs = [
    user(0), asst(10, 'r1'), use(12, 'r2', 't1', 'Bash', { command: './mvnw test' }), result(72, 't1'),
    use(80, 'r3', 't2', 'Read', { file_path: 'a.js' }), result(85, 't2'), asst(100, 'r4'),
  ];
  const t = agentTiming(evs);
  assert.equal(t.modelMs + t.bashMs + t.toolMs + t.waitMs, t.wallMs);
  assert.equal(t.wallMs, 100000);
  assert.equal(t.bashMs, 60000);
  assert.equal(t.toolMs, 5000);
  assert.ok(t.modelMs > 0);
});

test('tool running while the model streams counts once, as command', () => {
  const evs = [user(0), use(10, 'r1', 't1', 'Bash', { command: 'sleep' }), asst(30, 'r1'), result(50, 't1')];
  const t = agentTiming(evs);
  assert.equal(t.bashMs, 40000);
  assert.equal(t.modelMs + t.bashMs + t.toolMs + t.waitMs, t.wallMs);
});

test('usage-limit gap is excluded and counted as one stop', () => {
  const evs = [
    user(0), asst(10, 'r1'),
    { type: 'system', timestamp: at(20), apiError: 'usage_limit_reached' },
    { type: 'assistant', timestamp: at(21), requestId: 'rl', message: { model: '<synthetic>', content: [{ type: 'text', text: 'Session limit reached' }] } },
    user(21 + 300 * 60), asst(21 + 300 * 60 + 10, 'r2'),
  ];
  const t = agentTiming(evs);
  assert.equal(t.limitStops, 1);
  assert.ok(t.limitMs >= 300 * 60 * 1000);
  assert.ok(t.wallMs < 60000);
  assert.equal(t.modelMs + t.bashMs + t.toolMs + t.waitMs, t.wallMs);
});

test('unmatched tool_use is ignored', () => {
  const t = agentTiming([user(0), use(5, 'r1', 'x', 'Bash', { command: 'ls' }), asst(20, 'r2')]);
  assert.equal(t.bashMs, 0);
  assert.deepEqual(t.slowest, []);
});

test('slowest top 5 ordered, locate and nudges counted', () => {
  const evs = [user(0)];
  let s = 1;
  for (let i = 1; i <= 7; i++) {
    evs.push(use(s, 'q' + i, 'c' + i, 'Bash', { command: `cmd${i} ` + 'x'.repeat(100) }));
    evs.push(result(s + i, 'c' + i, i === 2 ? `${LOCATE_NUDGE_MARK} use it` : 'ok'));
    s += i + 1;
  }
  evs.push(use(s, 'ql', 'cl', 'Bash', { command: 'node entries/locate.mjs "foo"' }), result(s + 1, 'cl'));
  const t = agentTiming(evs);
  assert.equal(t.slowest.length, 5);
  assert.deepEqual(t.slowest.map((x) => x.ms), [7000, 6000, 5000, 4000, 3000]);
  assert.equal(t.locateCalls, 1);
  assert.equal(t.nudges, 1);
});

test('quantile nearest rank', () => {
  assert.equal(quantile([], 0.5), 0);
  assert.equal(quantile([1, 2, 3, 4], 0.5), 3);
  assert.equal(quantile([1, 2, 3, 4], 0.9), 4);
  assert.equal(quantile([5], 0.9), 5);
});

test('summarize and format', () => {
  const a = agentTiming([user(0), use(1, 'r1', 't', 'Bash', { command: './mvnw -q test' }), result(61, 't'), asst(70, 'r2')]);
  const b = agentTiming([user(0), use(1, 'r1', 't', 'Bash', { command: 'node locate.mjs foo' }), result(2, 't'), asst(5, 'r2')]);
  const sum = summarizeTiming([
    { agentType: 'nxy:implementer', timing: a },
    { agentType: 'nxy:planner', timing: b },
  ]);
  assert.equal(sum.types.length, 2);
  assert.equal(sum.slowest[0].agentType, 'nxy:implementer');
  const lines = formatAgentTiming(sum);
  const text = lines.join('\n');
  assert.match(text, /nxy:implementer\s+1\s/);
  assert.match(text, /nxy:planner\s+1\s/);
  assert.match(text, /1\/1/);
  assert.match(text, /slowest commands/);
  assert.doesNotMatch(text, /usage-limit/);
  sum.limitStops = 2;
  assert.match(formatAgentTiming(sum).join('\n'), /usage-limit stops excluded: 2/);
});
