// @ts-check
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { commandHead, commandKind } from '../scripts/filter/kind.mjs';

test('commandKind normalizes tools and subcommands', () => {
  const cases = [
    ['git status -s', 'git-status'],
    ['git -C sub log --oneline', 'git-log'],
    ['rtk git status', 'git-status'],
    ['rtk proxy npm test', 'npm-test'],
    ['npm test', 'npm-test'],
    ['npm run build', 'npm-run'],
    ['FOO=1 npm test', 'npm-test'],
    ['mvn test -Dtest=Foo', 'maven-test'],
    ['./mvnw clean verify', 'maven-clean'],
    ['./gradlew build', 'gradle-build'],
    ['docker ps -a', 'docker-ps'],
    ['ls -la', 'ls'],
    ['cd src && npm test', 'npm-test'],
    ['cd "C:/x" && export JAVA_HOME="/c/j" && rtk mvn test', 'maven-test'],
    ['cd src', 'cd'],
    ['Set-Location "C:/x"; $env:JAVA_HOME = "C:/j"; rtk mvn test', 'maven-test'],
    ['Set-Location "C:/x"\n.\\mvnw.cmd test', 'maven-test'],
    ['$candidates = @()', 'powershell-script'],
    ['export A=1 && rtk ls -la', 'ls'],
    ['python3 -m pytest', 'python'],
    ['', 'unknown'],
  ];
  for (const [cmd, kind] of cases) assert.equal(commandKind(cmd), kind, cmd);
});

test('commandHead masks secrets and truncates', () => {
  assert.equal(commandHead('curl -H "Authorization: Bearer abc123" x'), 'curl -H "Authorization: Bearer ***" x');
  assert.equal(commandHead('mysql --password=hunter2 db'), 'mysql --password=*** db');
  assert.equal(commandHead('API_KEY=sk-live-1 node app.js'), 'API_KEY=*** node app.js');
  assert.equal(commandHead('x'.repeat(100)).length, 80);
});

test('rtk helpers: isRtkInvocation / pinRtkPath / kind with absolute rtk path', async () => {
  const { isRtkInvocation, pinRtkPath } = await import('../scripts/filter/rtk.mjs');
  assert.equal(isRtkInvocation('rtk git status'), true);
  assert.equal(isRtkInvocation('rtk.exe git status'), true);
  assert.equal(isRtkInvocation('"C:/tools/rtk.exe" git status'), true);
  assert.equal(isRtkInvocation('/usr/local/bin/rtk mvn test'), true);
  assert.equal(isRtkInvocation('git status'), false);
  assert.equal(isRtkInvocation('rtkx foo'), false);
  assert.equal(isRtkInvocation('JAVA_HOME="/c/j" rtk mvn test'), true);
  const { usesRtk } = await import('../scripts/filter/rtk.mjs');
  assert.equal(usesRtk('cd x && export A=1 && rtk mvn test'), true);
  assert.equal(usesRtk('cd x && mvn test'), false);
  assert.equal(pinRtkPath('rtk git status', 'rtk'), 'rtk git status');
  assert.equal(pinRtkPath('rtk git status', 'C:/t/rtk.exe'), '"C:/t/rtk.exe" git status');
  assert.equal(pinRtkPath('rtk a && rtk b', '/opt/rtk'), '"/opt/rtk" a && "/opt/rtk" b');
  assert.equal(pinRtkPath('cd x && JAVA_HOME="/c/j" rtk mvn test', '/opt/rtk'), 'cd x && JAVA_HOME="/c/j" "/opt/rtk" mvn test');
  assert.equal(commandKind('"C:/t/rtk.exe" mvn test'), 'maven-test');
});
