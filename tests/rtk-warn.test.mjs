// @ts-check
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { existsSync, mkdtempSync, statSync, utimesSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { rtkWarnMarkerPath, silenceRtkHookWarning } from '../scripts/filter/rtk.mjs';

test('rtkWarnMarkerPath follows dirs::data_local_dir per platform', () => {
  const home = join('h', 'ome');
  assert.equal(rtkWarnMarkerPath({ LOCALAPPDATA: join('L', 'A') }, 'win32'), join('L', 'A', 'rtk', '.hook_warn_last'));
  assert.equal(rtkWarnMarkerPath({ USERPROFILE: home }, 'win32'), join(home, 'AppData', 'Local', 'rtk', '.hook_warn_last'));
  assert.equal(rtkWarnMarkerPath({ HOME: home }, 'linux'), join(home, '.local', 'share', 'rtk', '.hook_warn_last'));
  assert.equal(rtkWarnMarkerPath({ HOME: home, XDG_DATA_HOME: join('x', 'dg') }, 'linux'), join('x', 'dg', 'rtk', '.hook_warn_last'));
  assert.equal(rtkWarnMarkerPath({ HOME: home }, 'darwin'), join(home, 'Library', 'Application Support', 'rtk', '.hook_warn_last'));
});

test('silenceRtkHookWarning: creates the marker, then touches it only once the window is over', () => {
  const dataDir = mkdtempSync(join(tmpdir(), 'nxy-rtk-warn-'));
  const env = process.platform === 'win32' ? { LOCALAPPDATA: dataDir } : { XDG_DATA_HOME: dataDir };
  const marker = join(dataDir, 'rtk', '.hook_warn_last');
  const now = Date.UTC(2026, 8, 20, 12, 0, 0);

  assert.equal(existsSync(marker), false);
  assert.equal(silenceRtkHookWarning(env, now), true, 'missing marker → created');
  assert.equal(existsSync(marker), true);
  assert.ok(Math.abs(statSync(marker).mtimeMs - now) < 1000);

  assert.equal(silenceRtkHookWarning(env, now + 60_000), false, 'fresh marker → untouched');
  assert.ok(Math.abs(statSync(marker).mtimeMs - now) < 1000);

  const old = new Date(now - 30 * 60 * 60 * 1000);
  utimesSync(marker, old, old);
  assert.equal(silenceRtkHookWarning(env, now), true, 'stale marker → touched');
  assert.ok(Math.abs(statSync(marker).mtimeMs - now) < 1000);
});

test('silenceRtkHookWarning: fails soft when the marker cannot be written', () => {
  const dataDir = mkdtempSync(join(tmpdir(), 'nxy-rtk-warn-'));
  writeFileSync(join(dataDir, 'rtk'), 'not a directory');
  const env = process.platform === 'win32' ? { LOCALAPPDATA: dataDir } : { XDG_DATA_HOME: dataDir };
  assert.equal(silenceRtkHookWarning(env, Date.now()), false);
});
