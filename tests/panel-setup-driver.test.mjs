import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createDriver } from '../hosts/claude-code/mod/driver.mjs';
import { panelText as lines } from '../core/panel.mjs';

const panelText = (/** @type {any} */ p) => /** @type {any[]} */ ([]).concat(lines(p)).join('\n');

const row = (/** @type {boolean} */ found, /** @type {string} */ action, /** @type {string[]} */ display) =>
  ({ found, path: found ? '/bin/x' : null, version: found ? '1.0.0' : null, conflict: false, plan: { action, display } });

/** A fake `$`: process.run records every command and NEVER installs anything. */
function world() {
  /** @type {{ runs: string[][], tools: any, $?: any, driver?: any }} */
  const w = { runs: [], tools: null };
  w.tools = {
    ok: true, version: '1.0.2', platform: 'linux',
    rtk: row(false, 'install', ['sudo -n true-rtk-install']),
    rg: row(true, 'reinstall', ['sudo -n apt-get install -y ripgrep']),
    codegraph: row(false, 'install', ['curl -fsSL https://example.test/codegraph.sh | sh']),
  };
  const snap = { ok: true, runtimeDir: '/rt/nxy', projectDir: '/proj', branch: 'main', config: { orchestrator: 'auto', roles: {} } };
  w.$ = {
    plugin: { root: '/plugin' },
    fs: { read: async () => { throw new Error('ENOENT'); }, write: async () => {} },
    process: {
      run: async (/** @type {string[]} */ argv) => {
        w.runs.push(argv);
        if (argv.includes('--snapshot')) return { exitCode: 0, stdout: JSON.stringify(snap), stderr: '' };
        if (argv.some((a) => /entries\/config\.mjs$/.test(a))) return { exitCode: 0, stdout: JSON.stringify(w.tools), stderr: '' };
        if (argv.some((a) => /entries\/setup\.mjs$/.test(a))) return { exitCode: 0, stdout: 'instalado (stub)', stderr: '' };
        return { exitCode: 0, stdout: '{}', stderr: '' };
      },
    },
    agent: { spawn: async () => ({ model: 'm', agentId: 'a1' }) },
  };
  w.driver = async () => { const d = createDriver(w.$, { cwd: '/proj', sessionId: 's1' }); await d.start(); return d; };
  return w;
}

const setupRuns = (/** @type {any} */ w) => w.runs.filter((r) => r.some((a) => /entries\/setup\.mjs$/.test(a)));
const toolsRuns = (/** @type {any} */ w) => w.runs.filter((r) => r.some((a) => /entries\/config\.mjs$/.test(a)) && r.includes('tools'));

test('press before the tools are loaded is ignored', async () => {
  const w = world();
  const d = await w.driver();
  await d.press('setup-rtk');
  assert.equal(setupRuns(w).length, 0);
  assert.doesNotMatch(panelText(d.panel()), /true-rtk-install/);
});

test('press asks with the exact command and runs nothing; confirm-no still runs nothing', async () => {
  const w = world();
  const d = await w.driver();
  await d.press('tab:config');
  await d.press('setup-rtk');
  assert.match(panelText(d.panel()), /sudo -n true-rtk-install/);
  assert.equal(setupRuns(w).length, 0);
  await d.press('confirm-no');
  assert.equal(setupRuns(w).length, 0);
  assert.doesNotMatch(panelText(d.panel()), /sudo -n true-rtk-install/);
});

test('confirm-yes runs setup.mjs run <tool> --yes --cwd once and asks for the tools again', async () => {
  const w = world();
  const d = await w.driver();
  await d.press('tab:config');
  const before = toolsRuns(w).length;
  await d.press('setup-rtk');
  w.tools = { ...w.tools, rtk: row(true, 'reinstall', ['x']) };
  await d.press('confirm-yes');
  const runs = setupRuns(w);
  assert.equal(runs.length, 1);
  const a = runs[0];
  const i = a.findIndex((x) => /entries\/setup\.mjs$/.test(x));
  assert.deepEqual(a.slice(i + 1), ['run', 'rtk', '--yes', '--cwd', '/proj']);
  assert.equal(toolsRuns(w).length, before + 1);
  assert.match(panelText(d.panel()), /instalado \(stub\)/);
  await d.press('confirm-yes');
  assert.equal(setupRuns(w).length, 1, 'a second confirm-yes without a question runs nothing');
});

test('noteTurn and panel() run no node', async () => {
  const w = world();
  const d = await w.driver();
  await d.press('tab:config');
  const n = w.runs.length;
  d.noteTurn({ usage: { input: 10, output: 5, cacheRead: 0, cacheWrite: 0 }, now: 1000 });
  d.panel();
  assert.equal(w.runs.length, n);
});
