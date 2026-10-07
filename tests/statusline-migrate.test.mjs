// @ts-check
/** SessionStart statusline migration: shim regeneration + rewrite of a direct nxy cache path. */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { cpSync, existsSync,mkdirSync, mkdtempSync, readFileSync, readdirSync, statSync, utimesSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { isNxyDirectStatusline, migrateStatusline } from '../hosts/claude-code/statusline-migrate.mjs';
import { SHIM_GENERATION, shimSource } from '../hosts/claude-code/entries/statusline-setup.mjs';
import { openStore } from '../core/memory/store.mjs';
import { saveHandoff } from '../core/memory/handoff.mjs';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const HOOK = join(ROOT, 'hosts', 'claude-code', 'hooks', 'sessionstart.mjs');

/** Fake cache install, temp settings and shim paths. */
function sandbox() {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-sl-mig-'));
  const root = join(dir, 'plugins', 'cache', 'nxy-dev', 'nxy', '1.0.2');
  mkdirSync(root, { recursive: true });
  const settingsPath = join(dir, 'settings.json');
  const shimPath = join(dir, 'nxy-home', 'statusline.mjs');
  const write = (obj) => writeFileSync(settingsPath, typeof obj === 'string' ? obj : JSON.stringify(obj, null, 2));
  const read = () => JSON.parse(readFileSync(settingsPath, 'utf8'));
  const backups = () => readdirSync(dir).filter((f) => f.startsWith('settings.json.bak-'));
  const migrate = () => migrateStatusline({ root, settingsPath, shimPath });
  return { dir, root, settingsPath, shimPath, write, read, backups, migrate };
}

const DIRECT = [
  'node "/h/.claude/plugins/cache/nxy-dev/nxy/0.1.2/scripts/metrics/statusline.mjs"',
  'node "C:\\Users\\x\\.claude\\plugins\\cache\\nxy-dev\\nxy\\0.1.3\\scripts\\metrics\\statusline.mjs"',
  'node "/h/.claude/plugins/cache/nxy-dev/nxy/1.0.1/hosts/claude-code/statusline.mjs"',
  'node "C:\\Users\\x\\.claude\\plugins\\cache\\nxy-dev\\nxy\\1.0.1\\hosts\\claude-code\\statusline.mjs"',
];

test('isNxyDirectStatusline matches only versioned nxy cache scripts', () => {
  for (const c of DIRECT) assert.equal(isNxyDirectStatusline(c), true, c);
  assert.equal(isNxyDirectStatusline('node "/h/.nxy/statusline.mjs"'), false);
  assert.equal(isNxyDirectStatusline('/usr/local/bin/my-statusline'), false);
  assert.equal(isNxyDirectStatusline(undefined), false);
  assert.equal(isNxyDirectStatusline('node "C:\\Users\\a b\\.claude\\plugins\\cache\\nxy-dev\\nxy\\1.0.2-rc.1\\hosts\\claude-code\\statusline.mjs"'), true);
  assert.equal(isNxyDirectStatusline("node '/h/a b/cache/nxy-dev/nxy/0.1.2/scripts/metrics/statusline.mjs'"), true);
  assert.equal(isNxyDirectStatusline('node "/h/cache/nxy-dev/nxy/0.1.2/scripts/metrics/statusline.mjs" | x'), false);
});

for (const command of DIRECT) {
  test(`direct path is rewritten to the shim: ${command.slice(0, 60)}`, () => {
    const s = sandbox();
    const original = { theme: 'dark', statusLine: { type: 'command', command, padding: 0, refreshInterval: 30 } };
    s.write(original);
    const notice = s.migrate();
    assert.ok(notice);
    const cur = s.read();
    assert.equal(cur.statusLine.command, `node "${s.shimPath.replace(/\\/g, '/')}"`);
    assert.equal(cur.statusLine.padding, 0);
    assert.equal(cur.statusLine.refreshInterval, 30);
    assert.equal(cur.statusLine.type, 'command');
    assert.equal(cur.theme, 'dark');
    const [bak] = s.backups();
    assert.ok(bak, 'backup written');
    assert.deepEqual(JSON.parse(readFileSync(join(s.dir, bak), 'utf8')), original);
    assert.ok(notice.includes(bak), 'notice names the backup');
    assert.ok(notice.includes('restart Claude Code'));
    assert.ok(readFileSync(s.shimPath, 'utf8').split('\n')[0].includes(`generation=${SHIM_GENERATION}`));

    const mtime = statSync(s.settingsPath).mtimeMs;
    assert.equal(s.migrate(), null, 'never repeats');
    assert.equal(s.backups().length, 1);
    assert.equal(statSync(s.settingsPath).mtimeMs, mtime);
  });
}

test('a user statusline and a command already on the shim are untouched', () => {
  const s = sandbox();
  const p = '/h/.claude/plugins/cache/nxy-dev/nxy/0.1.3/scripts/metrics/statusline.mjs';
  for (const command of [
    '/home/u/bin/my-statusline.sh',
    'node "/h/.nxy/statusline.mjs"',
    `node "${p}" | other-script`,
    `node "${p}" --flag`,
    `node "${p}" && echo hi`,
    `wrapper.sh node "${p}"`,
  ]) {
    s.write({ statusLine: { type: 'command', command } });
    const before = readFileSync(s.settingsPath, 'utf8');
    assert.equal(s.migrate(), null);
    assert.equal(readFileSync(s.settingsPath, 'utf8'), before);
    assert.equal(s.backups().length, 0);
  }
  assert.equal(existsSync(s.shimPath), false, 'no shim created when nothing is nxy-direct');
});

test('stale shims are regenerated silently; a current one is not rewritten', () => {
  const s = sandbox();
  s.write({});
  mkdirSync(dirname(s.shimPath), { recursive: true });
  for (const stale of ["// old 0.1.3 shim\nconst x = 'scripts/metrics';\n", shimSource(s.root).split('\n').slice(1).join('\n')]) {
    writeFileSync(s.shimPath, stale);
    assert.equal(s.migrate(), null, 'silent');
    assert.ok(readFileSync(s.shimPath, 'utf8').split('\n')[0].includes(`generation=${SHIM_GENERATION}`));
    assert.equal(s.backups().length, 0, 'own file: no backup');
  }
  const old = new Date(Date.now() - 60_000);
  utimesSync(s.shimPath, old, old);
  const mtime = statSync(s.shimPath).mtimeMs;
  s.migrate();
  assert.equal(statSync(s.shimPath).mtimeMs, mtime);
});

test('broken or absent settings → null, file untouched', () => {
  const s = sandbox();
  assert.equal(s.migrate(), null);
  assert.equal(existsSync(s.settingsPath), false);
  s.write('{ not json');
  assert.equal(s.migrate(), null);
  assert.equal(readFileSync(s.settingsPath, 'utf8'), '{ not json');
  assert.equal(s.backups().length, 0);
});

test('a checkout root never migrates', () => {
  const s = sandbox();
  s.write({ statusLine: { type: 'command', command: DIRECT[0] } });
  const before = readFileSync(s.settingsPath, 'utf8');
  assert.equal(migrateStatusline({ root: ROOT, settingsPath: s.settingsPath, shimPath: s.shimPath }), null);
  assert.equal(readFileSync(s.settingsPath, 'utf8'), before);
  assert.equal(existsSync(s.shimPath), false);
});

test('hook from a checkout: settings untouched, output still the single handoff JSON', () => {
  const s = sandbox();
  const home = join(s.dir, 'home');
  mkdirSync(join(home, '.claude'), { recursive: true });
  const settings = JSON.stringify({ statusLine: { type: 'command', command: DIRECT[0] } });
  writeFileSync(join(home, '.claude', 'settings.json'), settings);
  const repo = join(s.dir, 'repo');
  mkdirSync(join(repo, '.git'), { recursive: true });
  writeFileSync(join(repo, '.git', 'HEAD'), 'ref: refs/heads/feature/x\n');
  writeFileSync(join(repo, '.git', 'config'), '[remote "origin"]\n\turl = git@github.com:mananos/nxy.dev.git\n');
  const nxyHome = join(s.dir, 'nxyhome');
  const db = openStore({ path: join(nxyHome, 'memory', 'memory.db') });
  saveHandoff(db, { project: 'github.com/mananos/nxy.dev', branch: 'feature/x', body: 'route: implementer\n## Done\n- x\n' });
  db.close();
  const out = execFileSync(process.execPath, ['--disable-warning=ExperimentalWarning', HOOK], {
    input: JSON.stringify({ cwd: repo }), encoding: 'utf8',
    env: { ...process.env, HOME: home, USERPROFILE: home, NXY_HOME: nxyHome, CLAUDE_PROJECT_DIR: repo },
  });
  assert.equal(readFileSync(join(home, '.claude', 'settings.json'), 'utf8'), settings);
  assert.equal(existsSync(join(nxyHome, 'statusline.mjs')), false);
  const parsed = JSON.parse(out);
  assert.equal(parsed.hookSpecificOutput.hookEventName, 'SessionStart');
  assert.ok(parsed.hookSpecificOutput.additionalContext);
});

/** Runs a copy of the hook installed under a fake plugin-cache layout, so the migration does run. */
function runCachedHook({ withHandoff }) {
  const s = sandbox();
  for (const d of ['core', 'hosts']) cpSync(join(ROOT, d), join(s.root, d), { recursive: true });
  cpSync(join(ROOT, 'package.json'), join(s.root, 'package.json'));
  const home = join(s.dir, 'home');
  mkdirSync(join(home, '.claude'), { recursive: true });
  const settingsFile = join(home, '.claude', 'settings.json');
  writeFileSync(settingsFile, JSON.stringify({ statusLine: { type: 'command', command: DIRECT[0] } }));
  const repo = join(s.dir, 'repo');
  mkdirSync(join(repo, '.git'), { recursive: true });
  writeFileSync(join(repo, '.git', 'HEAD'), 'ref: refs/heads/feature/x\n');
  writeFileSync(join(repo, '.git', 'config'), '[remote "origin"]\n\turl = git@github.com:mananos/nxy.dev.git\n');
  const nxyHome = join(s.dir, 'nxyhome');
  if (withHandoff) {
    const db = openStore({ path: join(nxyHome, 'memory', 'memory.db') });
    saveHandoff(db, { project: 'github.com/mananos/nxy.dev', branch: 'feature/x', body: 'route: implementer\n## Done\n- x\n' });
    db.close();
  }
  const out = execFileSync(process.execPath, ['--disable-warning=ExperimentalWarning', join(s.root, 'hosts', 'claude-code', 'hooks', 'sessionstart.mjs')], {
    input: JSON.stringify({ cwd: repo }), encoding: 'utf8',
    env: { ...process.env, HOME: home, USERPROFILE: home, NXY_HOME: nxyHome, CLAUDE_PROJECT_DIR: repo },
  });
  return { out, settingsFile, nxyHome };
}

test('hook under a cache layout: one JSON, notice in systemMessage and pointer in additionalContext', () => {
  const { out, settingsFile, nxyHome } = runCachedHook({ withHandoff: true });
  const parsed = JSON.parse(out);
  assert.match(parsed.systemMessage, /statusline/i);
  assert.ok(parsed.systemMessage.includes(join(nxyHome, 'statusline.mjs').replace(/\\/g, '/')) || parsed.systemMessage.includes('.bak-'));
  assert.equal(parsed.hookSpecificOutput.hookEventName, 'SessionStart');
  assert.match(parsed.hookSpecificOutput.additionalContext, /handoff/i);
  assert.ok(!parsed.hookSpecificOutput.additionalContext.includes('.bak-'));
  assert.ok(!JSON.parse(readFileSync(settingsFile, 'utf8')).statusLine.command.includes('/cache/'));
});

test('hook under a cache layout without handoff: systemMessage only', () => {
  const { out } = runCachedHook({ withHandoff: false });
  const parsed = JSON.parse(out);
  assert.match(parsed.systemMessage, /statusline/i);
  assert.equal(parsed.hookSpecificOutput, undefined);
});
