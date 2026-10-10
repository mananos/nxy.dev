import { test } from 'node:test';
import assert from 'node:assert/strict';
import { buildPanel, launcherOf, launcherAvailable, isPanelAction, TABS } from '../core/panel.mjs';

const plan = (action = 'install') => ({
  action, display: [`${action}: winget install rtk-ai.rtk   (fuente: README)`],
});
/** @returns {Record<string, any>} */
const tools = () => ({
  rtk: { found: false, path: null, version: null, conflict: false, plan: plan('install') },
  rg: { found: true, path: '/usr/bin/rg', version: '15.2.0', conflict: false, plan: plan('reinstall') },
  codegraph: { found: true, path: '/x/codegraph', version: '1.0', conflict: 'hooks', plan: { action: 'reinstall', display: ['quita:   claude mcp remove codegraph -s user   (x)', 'reinstalar: install.sh   (fuente: y)'] } },
});
const cfg = { config: { orchestrator: 'on', scopes: { user: {}, repo: {} }, sources: {}, roles: {}, gate: { enabled: true, contextTokens: 1000 }, filter: true, ui: { panel: 'on' } } };
/** @param {any} [over] @returns {any} */
const view = (over = {}) => buildPanel({ snap: cfg, ui: { tab: 'config' }, settings: {}, cfgInfo: { version: '1.0.0', tools: tools() }, ...over });
/** @param {string} id @param {any} ctx @returns {any} */
const L = (id, ctx) => launcherOf(id, ctx);
/** @param {any} v @returns {any[]} */
const cells = (v) => v.blocks.filter((/** @type {any} */ b) => b.type === 'rows').flatMap((/** @type {any} */ b) => b.rows).flatMap((/** @type {any} */ r) => r.cells);

test('label es Instalar o Reinstalar según el plan', () => {
  const ctx = { tools: tools() };
  assert.equal(L('setup-rtk', ctx).label, 'Instalar');
  assert.equal(L('setup-rg', ctx).label, 'Reinstalar');
  assert.deepEqual(L('setup-rg', ctx).args, ['run', 'rg', '--yes']);
});

test('la confirmación trae el comando exacto y lo que se quita', () => {
  const ctx = { tools: tools() };
  assert.match(L('setup-rtk', ctx).confirm, /Instalar rtk con: .*winget install rtk-ai\.rtk.*fuente/);
  const c = L('setup-codegraph', ctx).confirm;
  assert.ok(c.startsWith('Reinstalar codegraph con: quita:'));
  const v = view({ ui: { tab: 'config', confirm: 'setup-rtk' } });
  assert.match(v.ask.question, /winget install rtk-ai\.rtk/);
});

test('sin plan no hay botón ni disponibilidad', () => {
  assert.equal(launcherAvailable(L('setup-rtk', {}), { snap: null }), false);
  assert.equal(launcherAvailable(L('setup-rtk', {}), { snap: null, ctx: { tools: tools() } }), true);
  const bare = view({ cfgInfo: { version: '1.0.0', tools: { rtk: { found: true }, rg: {}, codegraph: {} } } });
  assert.ok(!cells(bare).some((c) => c.press?.startsWith('setup-')));
  assert.ok(!cells(view({ cfgInfo: { failed: true } })).some((c) => c.press?.startsWith('setup-')));
  const full = view();
  assert.deepEqual(cells(full).filter((c) => c.press?.startsWith('setup-')).map((c) => c.press), ['setup-rtk', 'setup-rg', 'setup-codegraph']);
  assert.ok(cells(full).some((c) => c.text === 'choca: hook en settings' && c.tone === 'warn'));
});

test('el texto del choque depende del tipo', () => {
  /** @param {string} name @param {any} conflict @returns {any[]} */
  const warn = (name, conflict) => {
    const t = tools();
    t[name] = { ...t[name], conflict };
    return cells(view({ cfgInfo: { version: '1.0.0', tools: t } })).filter((c) => c.tone === 'warn').map((c) => c.text);
  };
  assert.deepEqual(warn('rtk', true), ['choca: hook de rtk', 'choca: hook en settings']);
  assert.deepEqual(warn('codegraph', 'mcp'), ['choca: servidor MCP de codegraph']);
  assert.deepEqual(warn('codegraph', true), ['choca: hook en settings']);
});

test('hotkeys únicos en cada pestaña y ids aceptados', () => {
  for (const id of ['setup-rtk', 'setup-rg', 'setup-codegraph']) assert.equal(isPanelAction(id), true);
  for (const t of TABS) {
    const v = view({ ui: { tab: t.id } });
    const keys = [
      ...v.tabs.map((/** @type {any} */ x) => x.hotkey), ...v.keys.map((/** @type {any} */ k) => k.hotkey),
      ...v.blocks.filter((/** @type {any} */ b) => b.type === 'actions').flatMap((/** @type {any} */ b) => b.actions.map((/** @type {any} */ a) => a.hotkey)),
      ...cells(v).filter((c) => c.hotkey).map((c) => c.hotkey),
      ...(v.ask?.buttons ?? []).map((/** @type {any} */ b) => b.hotkey),
    ];
    assert.equal(new Set(keys).size, keys.length, t.id);
  }
});
