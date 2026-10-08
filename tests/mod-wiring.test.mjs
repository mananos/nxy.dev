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

test('register.tsx draws the console: resolved elements, width, hotkeys, no Close, status order, start()', () => {
  const src = readFileSync(join(root, 'hosts/claude-code/mod/register.tsx'), 'utf8');
  assert.match(src, /\$\.ui\.resolve/);
  assert.match(src, /props\??\.bodyColumns/);
  assert.doesNotMatch(src, /\be\.bodyColumns/);
  assert.match(src, /borderStyle/);
  assert.match(src, /hotkey=/);
  assert.doesNotMatch(src, /PANEL_CLOSE/);
  assert.doesNotMatch(src, /label="Close"|'Close'/);
  assert.match(src, /async function showStatus[\s\S]*?await \$\.ui\.status\([\s\S]*?lastStatus = text/);
  assert.match(src, /on\('session\.start'[\s\S]*?driver\.start\(\)/);
});

test('register.tsx draws the approved design: Raster, layoutOf, no truncation, the clock outside the render', () => {
  const src = readFileSync(join(root, 'hosts/claude-code/mod/register.tsx'), 'utf8');
  assert.match(src, /from\s*'\.\.\/\.\.\/\.\.\/core\/raster\.mjs'/);
  assert.match(src, /\blayoutOf\b[^\n]*from\s*'\.\.\/\.\.\/\.\.\/core\/panel\.mjs'/);
  assert.match(src, /<Raster\b/);
  assert.doesNotMatch(src, /truncate/);
  // The ui.render handler: from its `on(` to the next top-level hook; it never starts the clock.
  const handler = src.match(/on\('ui\.render'[\s\S]*?\n {2}\}\)\n/)?.[0] ?? '';
  assert.ok(handler.length > 0);
  assert.doesNotMatch(handler, /clock\.every|startTick/);
  // `draw` (the render's body) does not either; the tick starts from session.start, command.run and open.
  const draw = src.slice(src.indexOf('function draw('));
  assert.doesNotMatch(draw, /clock\.every|startTick/);
  assert.match(src, /\$\.clock\.every\(250/);
  assert.match(src, /async function openPane[\s\S]*?\$\.ui\.open[\s\S]*?startTick/);
  // `h` is the JSX factory: no variable may shadow it.
  assert.doesNotMatch(src, /\b(?:const|let|var)\s+h\b|[(,]\s*h\s*(?::[^,)]*)?\)\s*=>|\(\s*h\s*[,:]/);
  assert.doesNotMatch(src, /from\s+'node:/);
});

test('register.tsx: one tick timer at module level, cancelled before a new one; batches and findings are focusable Buttons', () => {
  const src = readFileSync(join(root, 'hosts/claude-code/mod/register.tsx'), 'utf8');
  assert.match(src, /^let tickTimer\b/m);
  assert.match(src, /function startTick[\s\S]*?stopTick\(\)[\s\S]*?\$\.clock\.every\(250/);
  assert.match(src, /function stopTick[\s\S]*?\.cancel\(\)/);
  // No hotkey on these: the focus ring (Tab / arrows) and Enter reach them, so ids never clash.
  assert.match(src, /<Button plain label=\{x\.title\}[^>]*onPress=\{press\(`batch:\$\{x\.n\}`\)\}/);
  assert.match(src, /<Button plain label=\{f\.picked[^>]*onPress=\{press\(`pick:\$\{f\.id\}`\)\}/);
});

test('register.tsx: cache events with their own catch, two module-level timers, no turn.step, no clock in the render', () => {
  const src = readFileSync(join(root, 'hosts/claude-code/mod/register.tsx'), 'utf8');
  for (const ev of ['prompt.submit', 'classic.Stop', 'classic.PreModelSwitch', 'classic.PostModelSwitch']) {
    const at = src.indexOf(`on('${ev}'`);
    assert.ok(at >= 0, ev);
    const body = src.slice(at).match(/^[\s\S]*?\n {2}\}\)([^\n]*)/)?.[0] ?? '';
    assert.match(body, /return next\(e\)/, ev);
    assert.match(body, /\}\)\.catch\(\(\$, e, next\) => next\(e\)\)$/, ev);
  }
  assert.doesNotMatch(src, /turn\.step/);
  assert.match(src, /^let cacheTimers\b/m);
  assert.match(src, /function scheduleCache[\s\S]*?\$\.clock\.after\(/);
  const handler = src.match(/on\('ui\.render'[\s\S]*?\n {2}\}\)\n/)?.[0] ?? '';
  assert.doesNotMatch(handler, /clock\.after|scheduleCache/);
  assert.doesNotMatch(src.slice(src.indexOf('function draw(')), /clock\.after|scheduleCache/);
  assert.doesNotMatch(src, /truncate/);
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
