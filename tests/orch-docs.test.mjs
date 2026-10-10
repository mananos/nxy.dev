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

test('README names the seven panel tabs and the fix button', () => {
  const readme = read('README.md');
  for (const tab of ['Inicio', 'Plan', 'Agentes', 'Stats', 'Config', 'Memoria', 'Features'])assert.ok(readme.includes(`**${tab}**`), tab);
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

test('README documents the editable Config tab, where it saves and the per-role effort', () => {
  const readme = read('README.md');
  assert.ok(readme.includes('**Config.**'));
  assert.ok(!readme.includes('esqueleto'));
  assert.ok(readme.includes('Guardar en'));
  assert.ok(readme.includes('el repo manda'));
  assert.ok(readme.includes('roles.<rol>.effort'));
});

test('README documents the Memoria tab and the roadmap marks 4b-1 done', () => {
  const readme = read('README.md');
  assert.ok(readme.includes('**Memoria.**'));
  assert.ok(readme.includes('`6`'));
  assert.ok(readme.includes('Preguntarle al scout'));
  assert.match(read('docs/roadmap-debate.md'), /4b-1: Memoria[^\n]*hecha \(2026-10-09\)/);
});

test('README documents /nxy:setup and the tool conflict, and the roadmap marks 4b-2 done', () => {
  const readme = read('README.md');
  assert.ok(readme.includes('/nxy:setup'));
  assert.ok(readme.includes('choca: hook de rtk') && readme.includes('choca: servidor MCP de codegraph') && readme.includes('choca: hook en settings'));
  assert.ok(readme.includes('sudo -n'));
  assert.match(read('docs/roadmap-debate.md'), /4b-2: \/nxy:setup, hecha \(2026-10-09\)/);
});

test('SKILL.md tells the main thread to wait for the orchestrator hand-back', () => {
  const skill = read('skills/nxy-workflow/SKILL.md');
  assert.match(skill, /orchestrator/i);
  assert.match(skill, /hands? back/i);
});
