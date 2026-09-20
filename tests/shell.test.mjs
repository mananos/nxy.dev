// @ts-check
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  splitTopLevel, hasHeredoc, hasSubstitution, hasRedirect, isBackgrounded, firstToken, leadingAssignments,
} from '../scripts/lib/shell.mjs';

test('splitTopLevel splits on && || ; | outside quotes', () => {
  assert.deepEqual(splitTopLevel('echo a && ls -la || true; pwd | wc -l'), ['echo a', 'ls -la', 'true', 'pwd', 'wc -l']);
  assert.deepEqual(splitTopLevel('echo "a && b" && echo \'c; d\''), ['echo "a && b"', "echo 'c; d'"]);
  assert.deepEqual(splitTopLevel('git commit -m "fix: a|b"'), ['git commit -m "fix: a|b"']);
  assert.deepEqual(splitTopLevel('  '), []);
});

test('hasHeredoc', () => {
  assert.equal(hasHeredoc("cat <<'X'\nhi\nX"), true);
  assert.equal(hasHeredoc('cat <<< "x"'), true);
  assert.equal(hasHeredoc('echo "<<not>>"'), false);
  assert.equal(hasHeredoc('ls'), false);
});

test('hasSubstitution', () => {
  assert.equal(hasSubstitution('echo $(date)'), true);
  assert.equal(hasSubstitution('echo `date`'), true);
  assert.equal(hasSubstitution('echo "${HOME}"'), true);
  assert.equal(hasSubstitution("echo '$(date)'"), false);
  assert.equal(hasSubstitution('echo $HOME'), false);
});

test('hasRedirect', () => {
  assert.equal(hasRedirect('ls > out.txt'), true);
  assert.equal(hasRedirect('ls 2>/dev/null'), false, 'discarding stderr writes no file');
  assert.equal(hasRedirect('ls 2> /dev/null; ls b &>/dev/null'), false);
  assert.equal(hasRedirect('ls >/dev/null && echo done'), false);
  assert.equal(hasRedirect('ls 2>/dev/null > out.txt'), true, 'a real target after the discard still counts');
  assert.equal(hasRedirect('ls > /dev/nullx'), true);
  assert.equal(hasRedirect('mvn test 2>&1'), false, '2>&1 keeps everything in the tool output');
  assert.equal(hasRedirect('mvn test 2>&1 | tail -n 150'), false);
  assert.equal(hasRedirect('ls >> log'), true);
  assert.equal(hasRedirect('sort < in.txt'), true);
  assert.equal(hasRedirect('echo "a > b"'), false);
  assert.equal(hasRedirect("cat <<'X'"), false, 'heredoc is not a redirect');
  assert.equal(hasRedirect('cat <<< "x"'), false, 'herestring is not a redirect');
});

test('isBackgrounded', () => {
  assert.equal(isBackgrounded('npm start &'), true);
  assert.equal(isBackgrounded('a && b'), false);
  assert.equal(isBackgrounded('a &&'), false);
});

test('firstToken skips env prefixes and wrappers', () => {
  assert.equal(firstToken('FOO=1 BAR=2 npm test'), 'npm');
  assert.equal(firstToken('time mvn test'), 'mvn');
  assert.equal(firstToken('./gradlew build'), 'gradlew');
  assert.equal(firstToken('rtk.exe git status'), 'rtk');
  assert.equal(firstToken('./mvnw.cmd test'), 'mvnw');
  assert.equal(firstToken('.\\gradlew.bat build'), 'gradlew');
  assert.equal(firstToken('   '), '');
});

test('stripTrailingLimit', async () => {
  const { stripTrailingLimit } = await import('../scripts/lib/shell.mjs');
  assert.equal(stripTrailingLimit('mvn test 2>&1 | tail -n 150'), 'mvn test 2>&1');
  assert.equal(stripTrailingLimit('npm test | head -50'), 'npm test');
  assert.equal(stripTrailingLimit('npm test | tail'), 'npm test');
  assert.equal(stripTrailingLimit('git log | grep x | tail -n 5'), 'git log | grep x');
  assert.equal(stripTrailingLimit('ls -la'), 'ls -la');
  assert.equal(stripTrailingLimit('tail -n 5 file'), 'tail -n 5 file');
});

test('leadingAssignments', () => {
  assert.deepEqual(leadingAssignments('NXY_RAW=1 mvn test'), { NXY_RAW: '1' });
  assert.deepEqual(leadingAssignments('mvn test NXY_RAW=1'), {});
});
