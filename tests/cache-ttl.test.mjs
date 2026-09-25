// @ts-check
/**
 * The one-hour cache what-if. What has to hold: a pause of 5–60 minutes that rebuilt a 5-minute cache
 * becomes a read of what the previous call left; every other 5-minute write is simply repriced at the
 * 1-hour rate; a pause over an hour, a compaction, an account already on the hour, and a model without
 * a price change nothing. And the verdict line recommends the setting only when it saves.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { hourTtlWhatIf, sumTtlWhatIf } from '../core/metrics/cache-ttl.mjs';
import { formatTtlWhatIf } from '../hosts/claude-code/transcripts.mjs';

// claude-sonnet-4-6, USD per MTok: input 3 · write 5m 3.75 · write 1h 6 · read 0.3 · output 15
const MODEL = 'claude-sonnet-4-6';
const MIN = 60_000;
const usage = (o) => ({ input: 0, cacheWrite5m: 0, cacheWrite1h: 0, cacheRead: 0, output: 0, thinking: 0, ...o });
/** @param {number|null} gapMs @param {object} u @param {'subagent'|'compact'|null} [after] */
const call = (gapMs, u, after = null) => ({ model: MODEL, gapMs, after, usage: usage(u) });
const near = (a, b) => assert.ok(Math.abs(a - b) < 1e-9, `${a} ≠ ${b}`);

test('a 20-minute pause: the rebuild becomes a read of the previous prefix', () => {
  const w = hourTtlWhatIf([
    call(null, { input: 10, cacheWrite5m: 150_000, output: 1_000 }),
    call(20 * MIN, { input: 10, cacheWrite5m: 152_000, output: 1_000 }),
  ]);
  // actual: (30 + 150000×3.75 + 15000) + (30 + 152000×3.75 + 15000)
  near(w.actualUsd, (30 + 562_500 + 15_000 + 30 + 570_000 + 15_000) / 1e6);
  // hour: first write at 6; second reads the 150,010 the first call sent and writes only 1,990 new
  near(w.hourUsd, (30 + 900_000 + 15_000 + 30 + 1_990 * 6 + 150_010 * 0.3 + 15_000) / 1e6);
  assert.equal(w.rebuildsAvoided, 1);
  assert.equal(w.writes5m, 2);
  assert.ok(w.hourUsd < w.actualUsd);
});

test('no pauses: every 5-minute write only gets dearer', () => {
  const w = hourTtlWhatIf([
    call(null, { cacheWrite5m: 20_000, output: 500 }),
    call(1 * MIN, { cacheWrite5m: 2_000, cacheRead: 20_000, output: 500 }),
  ]);
  near(w.hourUsd - w.actualUsd, (22_000 * (6 - 3.75)) / 1e6);
  assert.equal(w.rebuildsAvoided, 0);
});

test('what an hour cannot save: a pause over an hour, a compaction', () => {
  const overHour = hourTtlWhatIf([call(null, { cacheWrite5m: 100_000 }), call(90 * MIN, { cacheWrite5m: 100_000 })]);
  assert.equal(overHour.rebuildsAvoided, 0);
  near(overHour.hourUsd - overHour.actualUsd, (200_000 * 2.25) / 1e6);
  const compact = hourTtlWhatIf([call(null, { cacheWrite5m: 100_000 }), call(20 * MIN, { cacheWrite5m: 30_000 }, 'compact')]);
  assert.equal(compact.rebuildsAvoided, 0);
});

test('already on the hour, or no price: both sides the same', () => {
  const hour = hourTtlWhatIf([call(null, { cacheWrite1h: 100_000 }), call(20 * MIN, { cacheWrite1h: 5_000, cacheRead: 100_000 })]);
  assert.equal(hour.writes5m, 0);
  near(hour.hourUsd, hour.actualUsd);
  const unknown = hourTtlWhatIf([{ model: 'claude-future-9', gapMs: null, after: null, usage: usage({ cacheWrite5m: 100_000 }) }]);
  assert.deepEqual(unknown, { actualUsd: 0, hourUsd: 0, writes5m: 0, rebuildsAvoided: 0 }, 'never an invented price');
});

test('sumTtlWhatIf adds sessions', () => {
  const a = { actualUsd: 1, hourUsd: 0.5, writes5m: 3, rebuildsAvoided: 1 };
  assert.deepEqual(sumTtlWhatIf([a, a]), { actualUsd: 2, hourUsd: 1, writes5m: 6, rebuildsAvoided: 2 });
});

test('formatTtlWhatIf: recommends the setting only when it saves', () => {
  assert.equal(formatTtlWhatIf({ actualUsd: 10, hourUsd: 10, writes5m: 0, rebuildsAvoided: 0 }), null, 'already on the hour: nothing to say');
  const worth = formatTtlWhatIf({ actualUsd: 10, hourUsd: 7.5, writes5m: 40, rebuildsAvoided: 3 });
  assert.match(String(worth), /\$10\.00 → \$7\.50 \(-25\.0%\), 3 rebuilds would have been reads · worth it: set "promptCacheTtl": "1h"/);
  assert.match(String(formatTtlWhatIf({ actualUsd: 10, hourUsd: 10.4, writes5m: 40, rebuildsAvoided: 0 })), /\(\+4\.0%\) · not worth it here: keep the 5-minute cache/);
  assert.match(String(formatTtlWhatIf({ actualUsd: 10, hourUsd: 9.95, writes5m: 40, rebuildsAvoided: 1 })), /about the same: no need to change/);
});
