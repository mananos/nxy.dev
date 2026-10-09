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

test('README documents the panel, its command and how to turn it off', () => {
  const readme = read('README.md');
  assert.ok(readme.includes('/nxy-panel'));
  assert.ok(readme.includes('"plan":"always"'));
  assert.ok(readme.includes('● ready'));
  assert.ok(readme.includes('Terminar plan'));
  assert.ok(readme.includes('Gate once'));
  assert.ok(readme.includes('"panel": "off"'));
  assert.ok(readme.includes('claude -p "/nxy-panel"'));
});

test('README names the five panel tabs and the fix button', () => {
  const readme = read('README.md');
  for (const tab of ['Inicio', 'Plan', 'Agentes', 'Stats', 'Config']) assert.ok(readme.includes(`**${tab}**`), tab);
  assert.ok(readme.includes('Arreglar'));
});

test('README documents Stats, the Cache card, the cold mark and the feature summary', () => {
  const readme = read('README.md');
  assert.ok(readme.includes('**Stats**'));
  assert.ok(readme.includes('**Cache**'));
  assert.ok(readme.includes('❄'));
  assert.match(readme, /resumen/i);
});

test('README documents the agent list, the message field and the implementer confirmation', () => {
  const readme = read('README.md');
  assert.ok(readme.includes('**Agentes.**'));
  assert.match(readme, /Ver todos/);
  assert.match(readme, /mandarle un mensaje/);
  assert.ok(readme.includes('¿Mandar igual?'));
  assert.ok(readme.includes('no entregado'));
});

test('SKILL.md tells the main thread to wait for the orchestrator hand-back', () => {
  const skill = read('skills/nxy-workflow/SKILL.md');
  assert.match(skill, /orchestrator/i);
  assert.match(skill, /hands? back/i);
});
