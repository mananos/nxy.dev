import { test } from 'node:test';
import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import { spawnSync } from 'node:child_process';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const hooksDir = join(root, 'hooks');
const manifest = JSON.parse(readFileSync(join(hooksDir, 'hooks.json'), 'utf8'));

const commands = () => Object.values(manifest.hooks).flat().flatMap((g) => g.hooks).map((h) => h.command);

test('hooks.json names one module beside it that re-exports register', () => {
  assert.equal(manifest.modules.length, 1);
  const file = resolve(hooksDir, manifest.modules[0]);
  assert.ok(existsSync(file), file);
  // The engine refuses a bare re-export, so the file wraps the real register.
  const src = readFileSync(file, 'utf8');
  assert.match(src, /from\s*'\.\.\/hosts\/claude-code\/mod\/register\.tsx'/);
  assert.match(src, /export const register\b/);
  assert.ok(existsSync(join(root, 'hosts/claude-code/mod/register.tsx')));
});

test('every command hook script exists and no command carries a personal path', () => {
  for (const cmd of commands()) {
    const m = cmd.match(/\$\{CLAUDE_PLUGIN_ROOT\}\/([^"]+\.mjs)/);
    assert.ok(m, cmd);
    assert.ok(existsSync(join(root, m[1])), m[1]);
    assert.doesNotMatch(cmd, /[A-Za-z]:[\\/]|\/home\/|\/Users\//);
  }
});

test('register.tsx wires the status entry, /nxy-panel and the idle-free turn step', () => {
  const src = readFileSync(join(root, 'hosts/claude-code/mod/register.tsx'), 'utf8');
  assert.match(src, /\$\.command\.register/);
  assert.match(src, /'command\.run'/);
  assert.match(src, /\$\.ui\.status/);
  assert.match(src, /step\('turn'\)/);
  assert.match(src, /on\('command\.run'[\s\S]*?\}\)\.catch\(/);
  assert.match(src, /on\('command\.run'[\s\S]*?if \(headless\) return \{ text:[\s\S]*?\}\)\.catch\(/);
  assert.match(src, /panelSetting\(\) === 'auto'[\s\S]{0,120}\$\.ui\.open/);
  assert.doesNotMatch(src, /from\s+'node:/);
});

const hasClaude = spawnSync('claude', ['--version'], { shell: true, encoding: 'utf8' }).status === 0;
test('every <Button in register.tsx has an onPress (the engine skips the render otherwise)', () => {
  const src = readFileSync(join(root, 'hosts/claude-code/mod/register.tsx'), 'utf8');
  const buttons = src.match(/<Button\b[^>]*?\/>|<Button\b[^>]*?>/gs) ?? [];
  assert.ok(buttons.length > 0);
  for (const b of buttons) assert.match(b, /onPress=/, b);
});

test('claude plugin validate accepts the plugin', { skip: !hasClaude && 'claude not on PATH' }, () => {
  const r = spawnSync('claude', ['plugin', 'validate', root], { shell: true, encoding: 'utf8' });
  assert.equal(r.status, 0, `${r.stdout}\n${r.stderr}`);
});
