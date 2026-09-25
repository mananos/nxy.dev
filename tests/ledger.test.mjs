// @ts-check
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync } from 'node:fs';
import { createRequire } from 'node:module';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import {
  dailyFromGain, ledgerUnavailableReason, normalizeProjectPath, readLedgerRows, rtkHistoryDbPath, savedPct, sessionSavings, sumSavings, underProject,
} from '../core/filter/ledger.mjs';

test('normalizeProjectPath strips the Windows extended prefix and unifies slashes', () => {
  assert.equal(normalizeProjectPath('\\\\?\\C:\\Users\\u\\proj\\', 'win32'), 'c:/users/u/proj');
  assert.equal(normalizeProjectPath('/home/u/proj/', 'linux'), '/home/u/proj');
  assert.equal(normalizeProjectPath('', 'linux'), '');
});

test('underProject matches the project dir and its subdirs only', () => {
  assert.equal(underProject('c:/users/u/proj', 'C:\\Users\\u\\proj', 'win32'), true);
  assert.equal(underProject('c:/users/u/proj/sub', 'C:/Users/u/proj', 'win32'), true);
  assert.equal(underProject('c:/users/u/proj2', 'C:/Users/u/proj', 'win32'), false);
  assert.equal(underProject('/home/u/other', '/home/u/proj', 'linux'), false);
  assert.equal(underProject('/anything', '', 'linux'), true, 'unknown cwd → keep every row');
});

test('rtkHistoryDbPath: RTK_DB_PATH wins, else next to the warning marker', () => {
  assert.equal(rtkHistoryDbPath({ RTK_DB_PATH: join('x', 'h.db') }, 'linux'), join('x', 'h.db'));
  assert.equal(rtkHistoryDbPath({ XDG_DATA_HOME: join('d', 'ata') }, 'linux'), join('d', 'ata', 'rtk', 'history.db'));
});

test('sumSavings / savedPct / dailyFromGain', () => {
  const s = sumSavings([{ ts: 1, project: '', input: 100, output: 40, saved: 60 }, { ts: 2, project: '', input: 50, output: 50, saved: 0 }]);
  assert.deepEqual(s, { commands: 2, input: 150, output: 90, saved: 60 });
  assert.equal(savedPct(s), 40);
  assert.equal(savedPct(sumSavings([])), null);
  const daily = dailyFromGain({ daily: [{ date: '2026-09-17', commands: 3, input_tokens: 10, output_tokens: 4, saved_tokens: 6 }, { nope: 1 }] });
  assert.deepEqual([...daily.entries()], [['2026-09-17', { commands: 3, input: 10, output: 4, saved: 6 }]]);
});

test('readLedgerRows: missing db → null, and the reason says so', () => {
  const dbPath = join(mkdtempSync(join(tmpdir(), 'nxy-ledger-')), 'none.db');
  assert.equal(readLedgerRows({ dbPath }), null);
  assert.equal(ledgerUnavailableReason(dbPath), 'no-db');
});

let sqlite = null;
try {
  sqlite = createRequire(import.meta.url)('node:sqlite');
} catch {
  /* Node without node:sqlite: the fixture test below is skipped */
}

test('readLedgerRows + sessionSavings on a fixture db', { skip: !sqlite && 'node:sqlite unavailable' }, () => {
  const dbPath = join(mkdtempSync(join(tmpdir(), 'nxy-ledger-')), 'history.db');
  const db = new sqlite.DatabaseSync(dbPath);
  db.exec(`CREATE TABLE commands (id INTEGER PRIMARY KEY, timestamp TEXT NOT NULL, original_cmd TEXT NOT NULL, rtk_cmd TEXT NOT NULL,
    input_tokens INTEGER NOT NULL, output_tokens INTEGER NOT NULL, saved_tokens INTEGER NOT NULL, savings_pct REAL NOT NULL, exec_time_ms INTEGER DEFAULT 0, project_path TEXT DEFAULT '')`);
  const ins = db.prepare('INSERT INTO commands (timestamp, original_cmd, rtk_cmd, input_tokens, output_tokens, saved_tokens, savings_pct, project_path) VALUES (?, ?, ?, ?, ?, ?, ?, ?)');
  const proj = process.platform === 'win32' ? '\\\\?\\C:\\w\\proj' : '/w/proj';
  const sub = process.platform === 'win32' ? '\\\\?\\C:\\w\\proj\\sub' : '/w/proj/sub';
  const other = process.platform === 'win32' ? '\\\\?\\C:\\w\\other' : '/w/other';
  // rtk writes 9-digit fractions and +00:00; both must parse
  ins.run('2026-09-20T10:00:00.123456789+00:00', 'ls', 'rtk ls', 1000, 300, 700, 70, proj);
  ins.run('2026-09-20T10:05:00.5+00:00', 'grep x', 'rtk grep', 400, 100, 300, 75, sub);
  ins.run('2026-09-20T10:06:00+00:00', 'git status', 'rtk git', 200, 50, 150, 75, other);
  ins.run('2026-09-20T12:00:00+00:00', 'ls', 'rtk ls', 900, 900, 0, 0, proj);
  db.close();

  const all = readLedgerRows({ dbPath });
  assert.ok(all);
  assert.equal(all.length, 4);
  assert.equal(all[0].ts, Date.UTC(2026, 8, 20, 10, 0, 0, 123));

  const windowed = readLedgerRows({ dbPath, sinceMs: Date.UTC(2026, 8, 20, 10, 4), untilMs: Date.UTC(2026, 8, 20, 11) });
  assert.deepEqual(windowed?.map((r) => r.input), [400, 200]);

  const cwd = process.platform === 'win32' ? 'C:/w/proj' : '/w/proj';
  const session = { cwd, firstTs: Date.UTC(2026, 8, 20, 9, 59, 58), lastTs: Date.UTC(2026, 8, 20, 10, 5, 30) };
  const s = sessionSavings(all, session);
  assert.deepEqual(s, { commands: 2, input: 1400, output: 400, saved: 1000 }, 'proj + sub inside the window; other cwd and the noon row excluded');
  assert.deepEqual(sessionSavings(all, { cwd, firstTs: null, lastTs: null }), { commands: 0, input: 0, output: 0, saved: 0 });
});
