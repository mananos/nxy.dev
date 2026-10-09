import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
  parseAgentMeta, roleOf, statusView, toolLabel, costOf, createBook, bookSpawn, bookTool, bookTurn,
  bookList, bookTrail, bookSent, bookRoles, agentRows, agentDetail, needsTrail, needsConfirm, bookLive,
} from '../core/agentview.mjs';

const prices = {
  'claude-sonnet-5': { input: 3, cache_write_5m: 4, cache_write_1h: 6, cache_read: 0.3, output: 15 },
};
const usage = (model = 'claude-sonnet-5') => ({
  model, input_tokens: 1000, output_tokens: 2000, cache_read_input_tokens: 10000, cache_creation_input_tokens: 5000,
});

test('parseAgentMeta lee model y effort del frontmatter', () => {
  assert.deepEqual(parseAgentMeta('---\nname: x\nmodel: sonnet\neffort: medium\n---\ncuerpo'), { model: 'sonnet', effort: 'medium' });
  assert.deepEqual(parseAgentMeta('sin frontmatter'), { model: '', effort: '' });
});

test('roleOf distingue los roles de nxy', () => {
  assert.deepEqual(roleOf('nxy:implementer'), { role: 'implementer', nxy: true });
  assert.deepEqual(roleOf('Explore'), { role: 'Explore', nxy: false });
  assert.deepEqual(roleOf('teammate'), { role: 'teammate', nxy: false });
});

test('statusView cubre cada estado', () => {
  for (const s of ['running', 'waiting', 'pending']) assert.equal(statusView(s).live, true);
  assert.equal(statusView('idle').text, 'terminó');
  assert.equal(statusView('completed').live, false);
  assert.equal(statusView('failed').tone, 'error');
  assert.equal(statusView('completed', 'error').text, 'falló');
  assert.equal(statusView('killed').text, 'detenido');
  assert.equal(statusView('completed', 'aborted').text, 'interrumpido');
});

test('toolLabel arma etiquetas y topa el dato en 120', () => {
  assert.equal(toolLabel('Edit', { file_path: 'core/panel.mjs' }), 'Edit core/panel.mjs');
  assert.equal(toolLabel('Bash', { command: 'npm test\nsegunda' }), 'Bash: npm test');
  assert.equal(toolLabel('Grep', { pattern: 'foo' }), 'Grep foo');
  assert.equal(toolLabel('mcp__x__y', { a: 1 }), 'mcp__x__y');
  assert.equal(toolLabel('Read', {}), 'Read');
  const long = toolLabel('Bash', { command: 'x'.repeat(500) });
  assert.equal(long.length, 'Bash: '.length + 120);
});

test('costOf: precio exacto, estimado y desconocido', () => {
  const exact = costOf(usage(), 'claude-sonnet-5', prices, '5m');
  assert.equal(exact?.estimated, false);
  assert.ok(Math.abs((exact?.usd ?? 0) - (3000 + 3000 + 20000 + 30000) / 1e6) < 1e-9);
  const h1 = costOf(usage(), 'x', prices, '1h');
  assert.ok(Math.abs((h1?.usd ?? 0) - (3000 + 3000 + 30000 + 30000) / 1e6) < 1e-9);
  const est = costOf(usage('claude-sonnet-5-3'), '', prices, '5m');
  assert.equal(est?.estimated, true);
  assert.equal(costOf(usage('gpt-x'), 'gpt-x', prices, '5m'), null);
  assert.equal(costOf(null, 'a', prices), null);
});

test('libro: spawn, costo al terminar el turno, nxy y ajenos', () => {
  const b = createBook();
  bookRoles(b, { implementer: { model: 'sonnet', effort: 'medium' } });
  bookSpawn(b, { id: 'a1', type: 'nxy:implementer', description: 'lote 1', prompt: 'p'.repeat(2000), model: 'claude-sonnet-5', at: 1000 });
  bookList(b, [{ id: 'a2', type: 'Explore', description: 'busca', status: 'running' }], 2000);
  let rows = agentRows(b, { now: 5000 });
  const a1 = rows.find((r) => r.id === 'a1');
  assert.equal(a1?.costText, '…');
  assert.equal(a1?.modelEffort, 'claude-sonnet-5 · medium');
  assert.equal(a1?.nxy, true);
  assert.equal(rows.find((r) => r.id === 'a2')?.modelEffort, '');
  assert.equal(agentDetail(b, 'a1', {})?.prompt.length, 1500);
  bookTurn(b, { agentId: 'a1', usage: usage(), answer: 'listo', reason: 'end_turn', at: 6000 }, { prices, ttl: '5m' });
  rows = agentRows(b, { now: 7000 });
  const done = rows.find((r) => r.id === 'a1');
  assert.equal(done?.costText, '$0.06');
  assert.equal(done?.state, 'terminó');
  assert.equal(done?.elapsedMs, 5000);
  assert.equal(done?.live, false);
  assert.equal(agentDetail(b, 'a1', {})?.result, 'listo');
});

test('costo estimado lleva ~ y turno con error falla', () => {
  const b = createBook();
  bookSpawn(b, { id: 'e1', type: 'general-purpose', at: 0 });
  bookTurn(b, { agentId: 'e1', usage: usage('claude-sonnet-5-3'), reason: 'error', answer: 'boom', at: 10 }, { prices, ttl: '5m' });
  const r = agentRows(b, { now: 20 })[0];
  assert.match(r.costText, /^~\$/);
  assert.equal(r.state, 'falló');
  assert.equal(agentDetail(b, 'e1', {})?.error, 'boom');
});

test('orden: vivos primero, luego recientes', () => {
  const b = createBook();
  bookSpawn(b, { id: 'old', type: 'x', at: 1 });
  bookTurn(b, { agentId: 'old', reason: 'end_turn', at: 10 });
  bookSpawn(b, { id: 'new', type: 'x', at: 2 });
  bookTurn(b, { agentId: 'new', reason: 'end_turn', at: 20 });
  bookSpawn(b, { id: 'live', type: 'x', at: 0 });
  assert.deepEqual(agentRows(b, { now: 30 }).map((r) => r.id), ['live', 'new', 'old']);
});

test('tope de 40: se descartan primero los terminados más viejos', () => {
  const b = createBook();
  bookSpawn(b, { id: 'vivo', type: 'x', at: 0 });
  for (let i = 1; i <= 45; i++) {
    bookSpawn(b, { id: `t${i}`, type: 'x', at: i });
    bookTurn(b, { agentId: `t${i}`, reason: 'end_turn', at: 100 + i });
  }
  assert.equal(b.agents.size, 40);
  assert.ok(b.agents.has('vivo'));
  assert.ok(!b.agents.has('t1'));
  assert.ok(b.agents.has('t45'));
});

test('actividad: última herramienta con hace Ns; sin herramientas, trabajando… Ns', () => {
  const b = createBook();
  bookSpawn(b, { id: 'w', type: 'x', at: 1000 });
  assert.equal(agentRows(b, { now: 13000 })[0].activity, 'trabajando… 12s');
  bookTool(b, 'w', { tool: 'Edit', input: { file_path: 'a.mjs' }, at: 14000 });
  assert.equal(agentRows(b, { now: 17000 })[0].activity, 'Edit a.mjs · hace 3s');
  for (let i = 0; i < 9; i++) bookTool(b, 'w', { tool: 'Read', input: { file_path: `f${i}` }, at: 15000 + i });
  assert.equal(agentDetail(b, 'w', { now: 20000 })?.tools.length, 6);
});

test('needsTrail: vivo y sin herramienta del gancho en 6 s', () => {
  const b = createBook();
  bookSpawn(b, { id: 'n', type: 'x', at: 0 });
  assert.equal(needsTrail(b, 'n', 1000), true);
  bookTool(b, 'n', { tool: 'Read', at: 1000 });
  assert.equal(needsTrail(b, 'n', 5000), false);
  assert.equal(needsTrail(b, 'n', 8000), true);
  bookTurn(b, { agentId: 'n', reason: 'end_turn', at: 9000 });
  assert.equal(needsTrail(b, 'n', 20000), false);
  assert.equal(needsTrail(b, 'nadie', 0), false);
});

test('bookList: firstSeen, estado y agentes ajenos', () => {
  const b = createBook();
  bookList(b, [{ id: 'z', type: 'teammate', description: 'd', status: 'idle' }], 500);
  assert.equal(b.agents.get('z')?.firstSeen, 500);
  assert.equal(agentRows(b, { now: 900 })[0].state, 'terminó');
  bookList(b, [{ id: 'z', type: 'teammate', description: 'd', status: 'running' }], 1000);
  assert.equal(agentRows(b, { now: 1100 })[0].live, true);
});

test('bookTrail llena herramientas, prompt y resultado; ignora {deny}', () => {
  const b = createBook();
  bookList(b, [{ id: 't', type: 'x', description: 'd', status: 'running' }], 0);
  bookTrail(b, 't', { deny: 'no' }, 10);
  assert.equal(b.agents.get('t')?.tools.length, 0);
  bookTrail(b, 't', [
    { role: 'user', text: 'haz esto', toolUses: [] },
    { role: 'assistant', text: 'ok', toolUses: [{ tool: 'Grep', input: { pattern: 'abc' } }] },
  ], 50);
  const d = agentDetail(b, 't', { now: 60 });
  assert.equal(d?.prompt, 'haz esto');
  assert.equal(d?.tools[0].label, 'Grep abc');
  assert.equal(d?.result, 'ok');
  assert.equal(b.agents.get('t')?.hookAt, 0);
});

test('mensajes enviados: en cola, recibido con el próximo evento, no entregado', () => {
  const b = createBook();
  bookSpawn(b, { id: 's', type: 'x', at: 0 });
  bookTurn(b, { agentId: 's', reason: 'end_turn', at: 10 });
  bookSent(b, 's', { text: 'seguí', at: 20, delivered: true });
  assert.equal(agentRows(b, { now: 21 })[0].live, true); // reanuda
  assert.equal(agentDetail(b, 's', {})?.sent[0].state, 'en cola');
  bookTool(b, 's', { tool: 'Read', at: 30 });
  assert.equal(agentDetail(b, 's', {})?.sent[0].state, 'recibido');
  bookSent(b, 's', { text: 'otra', at: 40, delivered: false, reason: 'cerrado' });
  const s = agentDetail(b, 's', {})?.sent;
  assert.equal(s?.[1].state, 'no entregado');
  assert.equal(s?.[1].reason, 'cerrado');
  bookSent(b, 's', { text: 'q', at: 50 });
  bookTurn(b, { agentId: 's', reason: 'end_turn', at: 60 });
  assert.equal(agentDetail(b, 's', {})?.sent[2].state, 'recibido');
  for (let i = 0; i < 6; i++) bookSent(b, 's', { text: `m${i}`, at: 70 + i });
  assert.equal(agentDetail(b, 's', {})?.sent.length, 5);
});

test('lote y needsConfirm salen de inFlight', () => {
  const b = createBook();
  bookSpawn(b, { id: 'i1', type: 'nxy:implementer', at: 0 });
  bookSpawn(b, { id: 'sc', type: 'nxy:scout', at: 0 });
  const inFlight = { i1: { role: 'implementer', batch: 3 } };
  assert.equal(agentRows(b, { inFlight, now: 1 }).find((r) => r.id === 'i1')?.batch, 3);
  const d = agentDetail(b, 'i1', { inFlight, owned: true });
  assert.equal(d?.batch, 3);
  assert.equal(d?.confirm, true);
  assert.equal(needsConfirm({ id: 'i1', type: 'nxy:implementer' }, { owned: true, inFlight }), true);
  assert.equal(needsConfirm({ id: 'i1', type: 'nxy:implementer' }, { owned: false, inFlight }), false);
  assert.equal(needsConfirm({ id: 'sc', type: 'nxy:scout' }, { owned: true, inFlight }), false);
  assert.equal(needsConfirm({ id: 'i2', type: 'nxy:implementer' }, { owned: true, inFlight }), false);
  assert.equal(needsConfirm({ id: 'i1', type: 'general-purpose' }, { owned: true, inFlight }), false);
});

test('sentCount no se recorta con el log de 5', () => {
  const b = createBook();
  bookSpawn(b, { id: 's', type: 'x', at: 0 });
  for (let i = 0; i < 8; i++) bookSent(b, 's', { text: `m${i}`, at: 10 + i });
  const d = agentDetail(b, 's', {});
  assert.equal(d?.sent.length, 5);
  assert.equal(d?.sentCount, 8);
});

test('eventos de ids desconocidos quedan ocultos hasta un spawn o un list', () => {
  const b = createBook();
  bookTool(b, 'ghost', { tool: 'Read', at: 1 });
  bookTurn(b, { agentId: 'ghost2', reason: 'end_turn', at: 2 });
  bookTool(b, 'ghost2', { tool: 'Read', at: 3 });
  assert.deepEqual(agentRows(b, { now: 5 }), []);
  assert.equal(bookLive(b), false);
  bookList(b, [{ id: 'ghost', type: 'x', status: 'running' }], 6);
  assert.deepEqual(agentRows(b, { now: 7 }).map((r) => r.id), ['ghost']);
  assert.equal(bookLive(b), true);
  bookSpawn(b, { id: 'ghost2', type: 'x', at: 8 });
  assert.equal(agentRows(b, { now: 9 }).length, 2);
});

test('bookSpawn no degrada lo que anotó el driver (en ambos órdenes)', () => {
  const mine = { id: 'n1', type: 'nxy:implementer', description: 'lote 1', model: 'sonnet', spawnedBy: 'nxy', at: 5 };
  const hook = { id: 'n1', type: 'general-purpose', description: 'otro', model: 'haiku', spawnedBy: 'other', at: 6 };
  const a = createBook();
  bookSpawn(a, mine);
  bookSpawn(a, hook);
  const ea = a.agents.get('n1');
  assert.equal(ea?.type, 'nxy:implementer');
  assert.equal(ea?.spawnedBy, 'nxy');
  assert.equal(ea?.description, 'lote 1');
  assert.equal(ea?.model, 'sonnet');
  const b = createBook();
  bookSpawn(b, hook);
  bookSpawn(b, mine);
  const eb = b.agents.get('n1');
  assert.equal(eb?.type, 'nxy:implementer');
  assert.equal(eb?.spawnedBy, 'nxy');
  assert.equal(eb?.description, 'lote 1');
  const c = createBook();
  bookSpawn(c, { id: 'm', type: 'nxy:implementer', spawnedBy: 'nxy' });
  bookSpawn(c, { id: 'm', model: 'opus', spawnedBy: 'other' });
  assert.equal(c.agents.get('m')?.model, 'opus');
});

test('el archivo no importa node:*', () => {
  const src = readFileSync(new URL('../core/agentview.mjs', import.meta.url), 'utf8');
  assert.doesNotMatch(src, /from\s+['"]node:/);
  assert.deepEqual([...src.matchAll(/^import .* from '(.+)';/gm)].map((m) => m[1]), ['./price-table.mjs']);
});
