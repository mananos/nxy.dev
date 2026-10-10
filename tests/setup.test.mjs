import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  TOOLS,
  detectCodegraphWiring,
  packageManager,
  installPlan,
  runPlan,
  describePlan,
} from '../core/setup.mjs';

const displays = (p) => p.steps.map((s) => s.display);

test('TOOLS lists the three tools', () => {
  assert.deepEqual(TOOLS, ['rtk', 'rg', 'codegraph']);
});

test('packageManager per platform and order', () => {
  const only = (...bins) => (b) => bins.includes(b);
  assert.equal(packageManager('win32', only('winget')), 'winget');
  assert.equal(packageManager('win32', only()), null);
  assert.equal(packageManager('darwin', only('brew')), 'brew');
  assert.equal(packageManager('linux', only('brew', 'dnf', 'apt-get')), 'apt-get');
  assert.equal(packageManager('linux', only('brew', 'dnf')), 'dnf');
  assert.equal(packageManager('linux', only('pacman', 'brew')), 'pacman');
  assert.equal(packageManager('linux', only('brew')), 'brew');
  assert.equal(packageManager('linux', only()), null);
});

test('rtk plans by OS', () => {
  assert.deepEqual(displays(installPlan('rtk', { platform: 'win32', pm: 'winget' })), ['winget install rtk-ai.rtk']);
  assert.deepEqual(displays(installPlan('rtk', { platform: 'win32', pm: 'winget', found: true })), ['winget upgrade rtk-ai.rtk']);
  assert.deepEqual(displays(installPlan('rtk', { platform: 'darwin', pm: 'brew' })), ['brew install rtk']);
  assert.deepEqual(displays(installPlan('rtk', { platform: 'darwin', pm: 'brew', found: true })), ['brew upgrade rtk']);
  const l = installPlan('rtk', { platform: 'linux', pm: 'apt-get', found: true });
  assert.equal(l.action, 'reinstall');
  assert.match(l.steps[0].display, /raw\.githubusercontent\.com\/rtk-ai\/rtk\/refs\/heads\/master\/install\.sh -o "\$t" && sh "\$t"/);
  assert.equal(l.steps[0].needsSudo, false);
});

test('rg plans by OS and package manager; sudo steps always use -n', () => {
  assert.deepEqual(displays(installPlan('rg', { platform: 'win32', pm: 'winget' })), ['winget install BurntSushi.ripgrep.MSVC']);
  assert.deepEqual(displays(installPlan('rg', { platform: 'win32', pm: 'winget', found: true })), ['winget upgrade BurntSushi.ripgrep.MSVC']);
  assert.deepEqual(displays(installPlan('rg', { platform: 'darwin', pm: 'brew' })), ['brew install ripgrep']);
  const table = [
    ['apt-get', 'sudo -n apt-get install -y ripgrep', 'sudo apt-get install -y ripgrep'],
    ['dnf', 'sudo -n dnf install -y ripgrep', 'sudo dnf install -y ripgrep'],
    ['pacman', 'sudo -n pacman -S --noconfirm ripgrep', 'sudo pacman -S --noconfirm ripgrep'],
  ];
  for (const [pm, shown, manual] of table) {
    const p = installPlan('rg', { platform: 'linux', pm });
    assert.deepEqual(displays(p), [shown]);
    assert.equal(p.steps[0].needsSudo, true);
    assert.equal(p.steps[0].args[0], '-n');
    assert.deepEqual(p.manual, [manual]);
    assert.ok(!p.manual[0].includes('-n '));
  }
  const b = installPlan('rg', { platform: 'linux', pm: 'brew' });
  assert.deepEqual(displays(b), ['brew install ripgrep']);
  assert.equal(b.steps[0].needsSudo, false);
  assert.deepEqual(b.manual, []);
  assert.deepEqual(installPlan('rg', { platform: 'linux', pm: null }).steps, []);
});

test('codegraph plans by OS', () => {
  const w = installPlan('codegraph', { platform: 'win32', pm: 'winget' });
  assert.equal(w.steps[0].file, 'powershell');
  assert.deepEqual(w.steps[0].args.slice(0, 2), ['-NoProfile', '-Command']);
  assert.match(w.steps[0].args[2], /colbymchenry\/codegraph\/main\/install\.ps1 \| iex$/);
  for (const platform of ['linux', 'darwin']) {
    const p = installPlan('codegraph', { platform, pm: 'brew' });
    assert.match(p.steps[0].display, /colbymchenry\/codegraph\/main\/install\.sh -o "\$t" && sh "\$t"/);
  }
});

test('conflict adds removes; no conflict has none', () => {
  assert.deepEqual(installPlan('rtk', { platform: 'win32', found: true }).removes, []);
  const r = installPlan('rtk', { platform: 'win32', found: true, conflict: true });
  assert.equal(r.action, 'reinstall');
  assert.equal(r.removes[0].display, 'rtk init -g --uninstall');
  for (const scope of ['user', 'project', 'local']) {
    const m = installPlan('codegraph', { platform: 'linux', found: true, conflict: 'mcp', mcpScope: scope });
    assert.equal(m.removes[0].display, `claude mcp remove codegraph -s ${scope}`);
  }
  const noScope = installPlan('codegraph', { platform: 'linux', found: true, conflict: 'mcp' });
  assert.deepEqual(noScope.removes, []);
  assert.equal(noScope.manual.length, 1);
  const h = installPlan('codegraph', { platform: 'linux', found: true, conflict: 'hooks' });
  assert.deepEqual(h.removes, []);
  assert.equal(h.manual.length, 1);
});

test('describePlan shows command, source and sudo flag', () => {
  const lines = describePlan(installPlan('rg', { platform: 'linux', pm: 'apt-get' }));
  assert.ok(lines.some((l) => l.includes('sudo -n apt-get install -y ripgrep') && l.includes('fuente') && l.includes('sudo -n')));
  assert.ok(!lines.some((l) => l.startsWith('a mano:')));
  const withManual = describePlan(installPlan('rg', { platform: 'linux', pm: 'apt-get' }), { manual: true });
  assert.ok(withManual.some((l) => l.startsWith('a mano:') && l.includes('sudo apt-get install -y ripgrep')));
});

test('curl installer fails when the download fails (temp file, not a bare pipe)', () => {
  const s = installPlan('rtk', { platform: 'linux', pm: 'apt-get' }).steps[0];
  assert.equal(s.display, s.args[1]);
  assert.match(s.display, /curl -fsSL \S+ -o "\$t" && sh "\$t"/);
  assert.ok(!/curl[^;]*\| sh/.test(s.display));
});

test('powershell installer stops on error and displays what it runs', () => {
  const s = installPlan('codegraph', { platform: 'win32', pm: 'winget' }).steps[0];
  assert.match(s.args[2], /^\$ErrorActionPreference='Stop'; irm /);
  assert.ok(s.display.includes(s.args[2]));
});

test('rtk remove uses the resolved path; not callable -> manual, no remove', () => {
  const p = installPlan('rtk', { platform: 'win32', found: true, conflict: true, rtkPath: 'C:\\Program Files\\rtk\\rtk.exe' });
  assert.equal(p.removes[0].file, 'C:\\Program Files\\rtk\\rtk.exe');
  assert.deepEqual(p.removes[0].args, ['init', '-g', '--uninstall']);
  const nf = installPlan('rtk', { platform: 'win32', pm: 'winget', found: false, conflict: true });
  assert.deepEqual(nf.removes, []);
  assert.equal(nf.manual.length, 1);
  assert.equal(nf.steps.length, 1);
});

test('rtk hook in project settings goes to manual, not -g', () => {
  const proj = installPlan('rtk', { platform: 'win32', found: true, conflict: true, hookScopes: ['project'] });
  assert.deepEqual(proj.removes, []);
  assert.match(proj.manual[0], /proyecto/);
  const both = installPlan('rtk', { platform: 'win32', found: true, conflict: true, hookScopes: ['user', 'project'] });
  assert.equal(both.removes.length, 1);
  assert.equal(both.manual.length, 1);
});

test('detectCodegraphWiring', () => {
  assert.equal(detectCodegraphWiring([], []), false);
  assert.equal(detectCodegraphWiring([{ hooks: {} }], [null, {}]), false);
  assert.equal(detectCodegraphWiring([], [{ mcpServers: { codegraph: {} } }]), true);
  assert.equal(detectCodegraphWiring([{ mcpServers: { codegraph: {} } }], []), true);
  assert.equal(
    detectCodegraphWiring([{ hooks: { PostToolUse: [{ hooks: [{ command: 'codegraph sync' }] }] } }], []),
    true,
  );
  assert.equal(detectCodegraphWiring([{ hooks: { PreToolUse: [{ hooks: [{ command: 'rtk hook' }] }] } }], []), false);
});

/** @param {Array<{status: number, stdout?: string, stderr?: string}>} results */
function fakeSpawn(results) {
  /** @type {Array<{file: string, args: string[]}>} */
  const calls = [];
  const fn = (file, args) => {
    calls.push({ file, args });
    return results[calls.length - 1] || { status: 0, stdout: '' };
  };
  return { fn, calls };
}

test('runPlan runs removes then steps in order', () => {
  const plan = installPlan('rtk', { platform: 'win32', found: true, conflict: true });
  const { fn, calls } = fakeSpawn([]);
  const r = runPlan(plan, { spawn: fn });
  assert.equal(r.ok, true);
  assert.deepEqual(calls.map((c) => c.file), ['rtk', 'winget']);
  assert.deepEqual(r.ran, ['rtk init -g --uninstall', 'winget upgrade rtk-ai.rtk']);
  assert.equal(r.failedAt, null);
});

test('runPlan stops at the first failure', () => {
  const plan = installPlan('rtk', { platform: 'win32', found: true, conflict: true });
  const { fn, calls } = fakeSpawn([{ status: 1, stderr: 'boom' }]);
  const r = runPlan(plan, { spawn: fn });
  assert.equal(r.ok, false);
  assert.equal(calls.length, 1);
  assert.equal(r.failedAt, 'rtk init -g --uninstall');
  assert.match(r.tail, /boom/);
});

test('runPlan: sudo -n failure returns the manual command', () => {
  const plan = installPlan('rg', { platform: 'linux', pm: 'apt-get' });
  const { fn, calls } = fakeSpawn([{ status: 1, stderr: 'sudo: a password is required' }]);
  const r = runPlan(plan, { spawn: fn });
  assert.equal(r.ok, false);
  assert.deepEqual(calls[0], { file: 'sudo', args: ['-n', 'apt-get', 'install', '-y', 'ripgrep'] });
  assert.deepEqual(r.manual, ['sudo apt-get install -y ripgrep']);
  assert.match(r.tail, /sudo apt-get install -y ripgrep/);
});

test('runPlan: a non-password sudo failure reports the real output, no password hint', () => {
  const plan = installPlan('rg', { platform: 'linux', pm: 'apt-get' });
  const r = runPlan(plan, { spawn: fakeSpawn([{ status: 100, stderr: 'E: Unable to locate package ripgrep' }]).fn });
  assert.equal(r.ok, false);
  assert.match(r.tail, /Unable to locate package/);
  assert.ok(!/contraseña/.test(r.tail));
  const es = runPlan(plan, { spawn: fakeSpawn([{ status: 1, stderr: 'sudo: se requiere una contraseña' }]).fn });
  assert.match(es.tail, /sin terminal/);
});

test('runPlan treats a spawn error as failure', () => {
  const plan = installPlan('rtk', { platform: 'darwin', pm: 'brew' });
  const r = runPlan(plan, { spawn: () => ({ status: null, error: new Error('ENOENT') }) });
  assert.equal(r.ok, false);
  assert.match(r.tail, /ENOENT/);
});
