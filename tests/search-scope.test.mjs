// @ts-check
/** core/search-scope.mjs: broad vs targeted searches, from tool calls and console commands. */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { classifySearch, isBroadMainSearch, isLocateCommand, LOCATE_NUDGE_MARK } from '../core/search-scope.mjs';

test('isBroadMainSearch: grep/glob/find searches without a file path are broad; reads and files are not', () => {
  const B = (t, i) => isBroadMainSearch(t, i);
  assert.equal(B('Grep', { pattern: 'x' }), true);
  assert.equal(B('Grep', { pattern: 'x', path: 'src' }), true);
  assert.equal(B('Grep', { pattern: 'x', path: 'src/a.js' }), false);
  assert.equal(B('Glob', { pattern: '**/*.js' }), true);
  assert.equal(B('Read', { file_path: 'a.js' }), false);
  assert.equal(B('Bash', { command: 'rg foo' }), true);
  assert.equal(B('Bash', { command: 'rg foo src/a.js' }), false);
  assert.equal(B('Bash', { command: 'grep -rn foo .' }), true);
  assert.equal(B('Bash', { command: 'find . -name "*.js"' }), true);
  assert.equal(B('Bash', { command: 'cat x | rg foo' }), false);
  assert.equal(B('Bash', { command: 'npm test' }), false);
  assert.equal(B('PowerShell', { command: 'gci -r' }), true);
  assert.equal(B('Bash', null), false);
});

const FILES = new Set(['src/a.js', 'C:\\proj\\a.cs', 'C:/proj/b.cs', 'README.md']);
const isFile = (p) => FILES.has(p);
const bash = (command) => classifySearch('Bash', { command }, { isFile });
const ps = (command) => classifySearch('PowerShell', { command }, { isFile });

test('Grep: broad unless path is a single file', () => {
  assert.equal(classifySearch('Grep', { pattern: 'x' }, { isFile }).broad, true);
  assert.equal(classifySearch('Grep', { pattern: 'x', path: 'src' }, { isFile }).broad, true);
  const one = classifySearch('Grep', { pattern: 'x', path: 'src/a.js' }, { isFile });
  assert.deepEqual([one.kind, one.broad, one.target, one.pattern], ['grep', false, 'src/a.js', 'x']);
  assert.equal(classifySearch('Grep', { pattern: 'x' }).broad, true);
});

test('Glob is always broad', () => {
  const g = classifySearch('Glob', { pattern: '**/*.js' });
  assert.deepEqual([g.kind, g.broad, g.pattern], ['glob', true, '**/*.js']);
});

test('Read: broad only without offset/limit; pages is targeted', () => {
  assert.equal(classifySearch('Read', { file_path: 'a' }).broad, true);
  assert.equal(classifySearch('Read', { file_path: 'a', offset: 10 }).broad, false);
  assert.equal(classifySearch('Read', { file_path: 'a', limit: 50 }).broad, false);
  assert.equal(classifySearch('Read', { file_path: 'a.pdf', pages: '1-3' }).broad, false);
  assert.equal(classifySearch('Read', { file_path: 'a' }).kind, 'read');
});

test('rg: broad unless every path is a file; -e consumes the pattern', () => {
  assert.equal(bash('rg foo').broad, true);
  assert.equal(bash('rg foo src').broad, true);
  assert.equal(bash('rg foo src/a.js').broad, false);
  assert.equal(bash('rg foo src/a.js README.md').broad, false);
  assert.equal(bash('rg foo src/a.js src').broad, true);
  const e = bash('rg -e foo -e bar src/a.js');
  assert.deepEqual([e.broad, e.pattern, e.target], [false, 'foo', 'src/a.js']);
  assert.equal(bash('rg -g "*.js" foo src/a.js').broad, false);
  assert.equal(bash('rg --regexp=foo').broad, true);
});

test('rtk, sudo and VAR= prefixes are skipped', () => {
  assert.equal(bash('rtk rg foo').broad, true);
  assert.equal(bash('FOO=1 rtk rg foo src/a.js').broad, false);
  assert.equal(bash('sudo grep -r foo .').kind, 'grep');
});

test('grep only counts when recursive', () => {
  assert.equal(bash('grep foo src/a.js').kind, null);
  assert.equal(bash('grep -rn foo .').broad, true);
  assert.equal(bash('grep -R foo src').broad, true);
  assert.equal(bash('grep --recursive foo').broad, true);
  assert.equal(bash('grep --include=*.js foo').broad, true);
  assert.equal(bash('grep -r foo src/a.js').broad, false);
});

test('pipes: cat f | grep x is not a search; a quoted | is not a split', () => {
  assert.equal(bash('cat f | grep x').kind, null);
  assert.equal(bash('cat f | rg x').kind, null);
  const q = bash("rg 'a|b' src");
  assert.deepEqual([q.kind, q.broad, q.pattern], ['grep', true, 'a|b']);
  assert.equal(bash("rg 'a|b' src/a.js").broad, false);
});

test('pipes: a piped rg with an explicit path (even ".") is a tree search', () => {
  const r = bash('echo x | rg foo .');
  assert.deepEqual([r.kind, r.broad], ['grep', true]);
});

test('chains and lines: the broad one wins', () => {
  assert.equal(bash('rg foo src/a.js && rg bar').broad, true);
  assert.equal(bash('echo hi\nrg foo\n').broad, true);
  assert.equal(bash('rg foo src/a.js; ls').broad, false);
});

test('git grep, find, fd', () => {
  assert.equal(bash('git grep foo').kind, 'grep');
  assert.equal(bash('git grep foo').broad, true);
  assert.equal(bash('git grep foo -- src/a.js').broad, false);
  assert.equal(bash('git status').kind, null);
  const f = bash('find . -name "*.js"');
  assert.deepEqual([f.kind, f.broad, f.pattern], ['find', true, '*.js']);
  assert.equal(bash('find src/a.js -type f').broad, false);
  assert.equal(bash('fd foo').broad, true);
  assert.equal(bash('fd foo src/a.js').broad, false);
});

test('PowerShell: Get-ChildItem -Recurse, Select-String, Windows paths', () => {
  assert.equal(ps('Get-ChildItem -Recurse -Filter *.cs').broad, true);
  assert.equal(ps('gci C:\\proj -Recurse').broad, true);
  assert.equal(ps('ls -Recurse').kind, 'find');
  assert.equal(ps('Get-ChildItem C:\\proj').kind, null);
  assert.equal(ps('Select-String -Pattern foo -Path C:\\proj\\a.cs').broad, false);
  assert.equal(ps('Select-String foo C:\\proj\\*.cs').broad, true);
  assert.equal(ps('sls foo C:\\proj\\src').broad, true);
  assert.equal(ps('Get-Content x | Select-String foo').broad, false);
  assert.equal(bash('rg foo C:/proj/b.cs').broad, false);
  assert.equal(bash('rg foo C:\\proj\\a.cs').broad, false);
});

test('non-search tools and bad input give kind null', () => {
  assert.equal(bash('npm test').kind, null);
  assert.equal(classifySearch('Edit', {}).kind, null);
  assert.equal(classifySearch('Bash', null).kind, null);
  assert.equal(classifySearch('Bash', { command: 5 }).kind, null);
});

test('isLocateCommand and the nudge mark', () => {
  assert.equal(isLocateCommand('node "/p/hosts/claude-code/entries/locate.mjs" "x"'), true);
  assert.equal(isLocateCommand('rg locate'), false);
  assert.ok(LOCATE_NUDGE_MARK.length > 5);
});
