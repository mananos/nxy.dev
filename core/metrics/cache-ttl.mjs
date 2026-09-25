// @ts-check
/**
 * What a conversation would have cost with a one-hour prompt cache instead of a five-minute one,
 * worked out from the calls it already made. It turns "should I pay more per cache write to keep
 * the cache alive through my pauses?" into a number, so nobody has to measure or guess.
 *
 * Per call, in order:
 *  - a 5-minute write becomes a 1-hour write, priced by the table (1.6× the 5-minute rate);
 *  - a call that came 5 to 60 minutes after the previous one found the 5-minute cache gone and wrote
 *    the prefix again. With an hour, what the previous call left would still be cached: those tokens
 *    are read (a tenth of the input price) and only the rest is written.
 * A gap over an hour rebuilds either way, and a compaction is a new prefix either way. Calls that
 * already wrote with the 1-hour TTL, and calls of a model without a price, count the same on both sides.
 *
 * It takes one thread's calls: the TTL is chosen per thread (main conversation vs. subagents).
 */
import { costFor, totalInput } from '../pricing.mjs';

const FIVE_MINUTES = 300_000;
const ONE_HOUR = 3_600_000;

/**
 * @typedef {object} TtlWhatIf
 * @property {number} actualUsd        what the thread cost
 * @property {number} hourUsd          what it would have cost with a one-hour cache
 * @property {number} writes5m         calls that wrote with the five-minute TTL
 * @property {number} rebuildsAvoided  rebuilds a one-hour cache would have turned into reads
 */

/**
 * @param {Pick<import('./aggregate.mjs').Call, 'model'|'usage'|'gapMs'|'after'>[]} calls  one thread, in order
 * @returns {TtlWhatIf}
 */
export function hourTtlWhatIf(calls) {
  const out = { actualUsd: 0, hourUsd: 0, writes5m: 0, rebuildsAvoided: 0 };
  /** @type {number|null} */
  let prevContext = null;
  for (const call of calls) {
    const u = call.usage;
    const context = totalInput(u);
    const actual = costFor(call.model, u);
    if (actual !== null) {
      let hour = actual;
      if (u.cacheWrite5m > 0) {
        out.writes5m++;
        let reread = 0;
        const gap = call.gapMs;
        if (prevContext !== null && typeof gap === 'number' && gap >= FIVE_MINUTES && gap < ONE_HOUR && call.after !== 'compact') {
          // The prefix the previous call sent is what a live cache would have served.
          reread = Math.max(0, Math.min(u.cacheWrite5m, Math.min(prevContext, context) - u.cacheRead));
          if (reread > 0) out.rebuildsAvoided++;
        }
        hour = costFor(call.model, {
          ...u,
          cacheWrite5m: 0,
          cacheWrite1h: u.cacheWrite1h + u.cacheWrite5m - reread,
          cacheRead: u.cacheRead + reread,
        }) ?? actual;
      }
      out.actualUsd += actual;
      out.hourUsd += hour;
    }
    prevContext = context;
  }
  return out;
}

/**
 * Adds several threads' results (a trend over many sessions).
 * @param {TtlWhatIf[]} parts
 * @returns {TtlWhatIf}
 */
export function sumTtlWhatIf(parts) {
  const out = { actualUsd: 0, hourUsd: 0, writes5m: 0, rebuildsAvoided: 0 };
  for (const p of parts) {
    out.actualUsd += p.actualUsd;
    out.hourUsd += p.hourUsd;
    out.writes5m += p.writes5m;
    out.rebuildsAvoided += p.rebuildsAvoided;
  }
  return out;
}
