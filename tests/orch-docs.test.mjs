import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const read = (rel) => readFileSync(new URL(`../${rel}`, import.meta.url), 'utf8');

test('README documents the orchestrator config and its refusal message', () => {
  const readme = read('README.md');
  assert.match(readme, /flow\.pauseAfterBatch|pauseAfterBatch/);
  assert.match(readme, /flow\.orchestrator|"orchestrator"/);
  assert.ok(readme.includes('the orchestrator is running plan'));
  assert.ok(readme.includes('orch.mjs release'));
});

test('SKILL.md tells the main thread to wait for the orchestrator hand-back', () => {
  const skill = read('skills/nxy-workflow/SKILL.md');
  assert.match(skill, /orchestrator/i);
  assert.match(skill, /hands? back/i);
});
