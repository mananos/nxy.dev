// @ts-check
import { test } from 'node:test';
import assert from 'node:assert/strict';

// One entry per 1.x golden generator of the usage / transcripts / metrics area; each exports check().
const generators = ['pricing', 'jscompat', 'metrics', 'transcripts'];

for (const name of generators) {
  test(`golden ${name} matches what the 1.x produces now`, async () => {
    const mod = await import(`../scripts/golden/${name}.mjs`);
    assert.deepEqual(mod.check(), [], `golden files are stale (node scripts/golden/${name}.mjs)`);
  });
}
