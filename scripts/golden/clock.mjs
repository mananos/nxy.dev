// @ts-check
/**
 * Loaded with `node --import clock.mjs <entry> ...`: Date.now() answers NXY_GOLDEN_NOW (epoch ms),
 * so the 1.x commands that read the clock (stats/trend/statusline, engine cache TTL) are deterministic.
 * `new Date()` without arguments is left alone: none of those commands uses it.
 */
const now = Number(process.env.NXY_GOLDEN_NOW);
if (Number.isFinite(now) && process.env.NXY_GOLDEN_NOW !== '') Date.now = () => now;
