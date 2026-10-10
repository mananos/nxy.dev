import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
  MODELS, EFFORTS, GATE_THRESHOLDS, ROLES, FIELDS, isField, nextValue, setPath, validate, sourcesOf,
} from '../core/configedit.mjs';

test('constantes y campos', () => {
  assert.deepEqual(MODELS, ['haiku', 'sonnet', 'opus', 'fable']);
  assert.deepEqual(EFFORTS, ['low', 'medium', 'high']);
  assert.equal(ROLES.length, 7);
  assert.ok(GATE_THRESHOLDS.includes(100000));
  assert.ok(isField('roles.planner.model') && isField('gate.contextTokens') && isField('ui.panel'));
  assert.ok(!isField('roles.nadie.model') && !isField(42));
  assert.deepEqual(FIELDS['roles.tester.effort'].path, ['roles', 'tester', 'effort']);
});

test('nextValue cicla con predeterminado (null) y envuelve', () => {
  assert.equal(nextValue('roles.planner.model', undefined, 'next'), 'haiku');
  assert.equal(nextValue('roles.planner.model', 'fable', 'next'), null);
  assert.equal(nextValue('roles.planner.model', null, 'prev'), 'fable');
  assert.equal(nextValue('roles.planner.effort', 'low', 'prev'), null);
  assert.equal(nextValue('roles.planner.effort', 'high', 'next'), null);
});

test('nextValue salta el null que no cambia lo que se ve (effort del frontmatter)', () => {
  assert.equal(nextValue('roles.planner.effort', 'high', 'next', 'high'), 'low');
  assert.equal(nextValue('roles.planner.effort', 'high', 'prev', 'high'), 'medium');
  assert.equal(nextValue('roles.planner.effort', 'low', 'prev', 'high'), null);
  assert.equal(nextValue('roles.planner.effort', 'high', 'next'), null);
});

test('nextValue de toggles', () => {
  assert.equal(nextValue('flow.orchestrator', 'auto', 'toggle'), 'off');
  assert.equal(nextValue('flow.orchestrator', 'off', 'toggle'), 'auto');
  assert.equal(nextValue('gate.enabled', true, 'toggle'), false);
  assert.equal(nextValue('gate.enabled', false, 'toggle'), true);
});

test('nextValue del umbral: lista y fuera de lista', () => {
  assert.equal(nextValue('gate.contextTokens', 100000, 'next'), 150000);
  assert.equal(nextValue('gate.contextTokens', 100000, 'prev'), 75000);
  assert.equal(nextValue('gate.contextTokens', 300000, 'next'), 50000);
  assert.equal(nextValue('gate.contextTokens', 120000, 'next'), 150000);
  assert.equal(nextValue('gate.contextTokens', 120000, 'prev'), 100000);
  assert.equal(nextValue('gate.contextTokens', 999999, 'next'), 300000);
  assert.equal(nextValue('gate.contextTokens', 10, 'prev'), 50000);
});

test('setPath: copia, null borra y poda', () => {
  const a = { roles: { planner: { model: 'opus' } }, gate: { enabled: true } };
  const b = setPath(a, ['roles', 'planner', 'effort'], 'high');
  assert.deepEqual(b.roles.planner, { model: 'opus', effort: 'high' });
  assert.equal(a.roles.planner.effort, undefined);
  const c = setPath(a, ['roles', 'planner', 'model'], null);
  assert.deepEqual(c, { gate: { enabled: true } });
  assert.deepEqual(setPath({}, ['ui', 'panel'], 'off'), { ui: { panel: 'off' } });
  assert.deepEqual(setPath({}, ['ui', 'panel'], null), {});
});

test('validate rechaza claves, modelos y efforts desconocidos', () => {
  assert.equal(validate('roles.planner.model', 'opus').ok, true);
  assert.equal(validate('roles.planner.model', null).ok, true);
  assert.equal(validate('roles.planner.model', 'gpt').ok, false);
  assert.equal(validate('roles.planner.effort', 'max').ok, false);
  assert.equal(validate('nada.nada', 'x').ok, false);
  assert.equal(validate('gate.contextTokens', 123).ok, false);
  assert.equal(validate('gate.contextTokens', 150000).ok, true);
  assert.equal(validate('gate.enabled', 'yes').ok, false);
});

test('sourcesOf: el repo pisa al usuario', () => {
  const user = { gate: { enabled: false }, ui: { panel: 'off' } };
  const repo = { gate: { contextTokens: 50000, enabled: true } };
  assert.equal(sourcesOf('gate.enabled', { user, repo }), 'repo');
  assert.equal(sourcesOf('ui.panel', { user, repo }), 'user');
  assert.equal(sourcesOf('flow.orchestrator', { user, repo }), 'default');
  assert.equal(sourcesOf('gate.enabled', { user }), 'user');
  assert.equal(sourcesOf('nada', {}), 'default');
  // NXY_FILTER=0|1 pisa todo, pero sólo a modules.filter
  assert.equal(sourcesOf('modules.filter', { repo: { modules: { filter: true } } }, { NXY_FILTER: '0' }), 'env');
  assert.equal(sourcesOf('modules.filter', { user: { modules: { filter: true } } }, { NXY_FILTER: '1' }), 'env');
  assert.equal(sourcesOf('modules.filter', { user: { modules: { filter: true } } }, { NXY_FILTER: 'x' }), 'user');
  assert.equal(sourcesOf('gate.enabled', { user }, { NXY_FILTER: '1' }), 'user');
});

test('configedit.mjs es puro: no importa node:*', () => {
  const src = readFileSync(new URL('../core/configedit.mjs', import.meta.url), 'utf8');
  assert.ok(!/node:/.test(src));
});
