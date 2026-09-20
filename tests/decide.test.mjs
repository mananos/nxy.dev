// @ts-check
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { decide } from '../scripts/filter/decide.mjs';

/** Fake engine: prefixes with `rtk ` and returns the exit code chosen per command. */
function fakeRewrite(codes = {}) {
  return (cmd) => ({ code: codes[cmd] ?? 0, stdout: `rtk ${cmd}` });
}

const base = { engine: /** @type {const} */ ('rtk'), rewrite: fakeRewrite() };

/** @type {Array<[string, string, Partial<import('../scripts/filter/decide.mjs').DecideContext>?]>} */
const SKIP_CASES = [
  ['', 'empty'],
  ['   ', 'empty'],
  ['git status', 'engine-off', { engine: 'off' }],
  ['NXY_RAW=1 mvn test', 'escape-env'],
  ['cd api && NXY_RAW=1 grep -n foo README.md', 'escape-env'],
  ['mvn test # raw', 'escape-comment'],
  ['mvn test #nxy:raw', 'escape-comment'],
  ['rtk git status', 'already-wrapped'],
  ['rtk.exe git status', 'already-wrapped'],
  ['cd src && rtk npm test', 'already-wrapped'],
  ['curl -s https://x', 'excluded', { excludeCommands: ['curl'] }],
  ['npm test && curl x', 'excluded', { excludeCommands: ['curl'] }],
  ['ls', 'not-in-only', { onlyCommands: ['npm', 'mvn'] }],
  ["cat <<'X'\nhi\nX", 'heredoc'],
  ['cat <<< "hi"', 'heredoc'],
  ['echo $(date)', 'substitution'],
  ['echo `date`', 'substitution'],
  ['echo "${HOME}"', 'substitution'],
  ['ls > out.txt', 'redirect'],
  ['npm test > out.log 2>&1', 'redirect'],
  ['sort < in.txt', 'redirect'],
  ['npm start &', 'background'],
  // a small cap already bounds the output; rtk's headers would eat part of it
  ['grep -rn "nxy" README.md | head -3', 'capped'],
  ['npm test | tail -20', 'capped'],
  ['ls -la | tail', 'capped'],
  ['git log --oneline | head -n 10', 'capped'],
  ['cd src', 'interactive-or-stateful'],
  ['export A=1', 'interactive-or-stateful'],
  ['cd src && export A=1', 'interactive-or-stateful'],
  ['sudo apt install x', 'interactive-or-stateful'],
  ['vim file.txt', 'interactive-or-stateful'],
  ['ssh host', 'interactive-or-stateful'],
  ['node', 'interactive-or-stateful'],
  ['python3', 'interactive-or-stateful'],
  ['cd src && node', 'interactive-or-stateful'],
  ['node -e "console.log(1)"', 'opaque'],
  ['cd src && node --test tests/x.test.mjs', 'opaque'],
  ['python3 tool.py --flag', 'opaque'],
  ['git commit -m "x"', 'interactive-or-stateful'],
  ['git -C . commit -m "x"', 'interactive-or-stateful'],
  ['git push origin main', 'interactive-or-stateful'],
  ['git rebase -i HEAD~3', 'interactive-or-stateful'],
  ['git add -p', 'interactive-or-stateful'],
  ['git checkout main', 'interactive-or-stateful'],
  ['docker exec -it web sh', 'interactive-or-stateful'],
  ['ls && git commit -m x', 'interactive-or-stateful'],
  ['cat .nxy/metrics/filter.jsonl', 'nxy-internal'],
  ['cat "./.nxy/config.json"', 'nxy-internal'],
  ['git status', 'rtk-hook-present', { rtkHookDetected: true }],
  ['git status', 'no-engine', { rewrite: undefined }],
  ['weird-cmd', 'rtk-none', { rewrite: fakeRewrite({ 'weird-cmd': 1 }) }],
  ['rm -rf /', 'rtk-deny', { rewrite: fakeRewrite({ 'rm -rf /': 2 }) }],
  ['git status', 'rtk-error', { rewrite: () => { throw new Error('boom'); } }],
  ['git status', 'rtk-unchanged', { rewrite: () => ({ code: 0, stdout: 'git status' }) }],
  ['git status', 'rtk-unchanged', { rewrite: () => ({ code: 0, stdout: '' }) }],
];

for (const [cmd, reason, over] of SKIP_CASES) {
  test(`skip: ${JSON.stringify(cmd)} → ${reason}`, () => {
    const d = decide(cmd, { ...base, ...(over || {}) });
    assert.equal(d.action, 'skip');
    assert.equal(d.reason, reason);
  });
}

/** @type {Array<[string, string, boolean, Partial<import('../scripts/filter/decide.mjs').DecideContext>?]>} */
const REWRITE_CASES = [
  ['git status', 'rtk git status', true],
  ['mvn test', 'rtk mvn test', true],
  ['FOO=1 npm test', 'rtk FOO=1 npm run test', true],
  ['cd src && npm test', 'rtk cd src && npm run test', true],
  // a big trailing head/tail is removed to ask rtk (it declines pipes) and put back: the cap the model asked for stays
  ['npm test | tail -50', 'rtk npm run test | tail -50', true],
  ['mvn test 2>&1 | tail -n 150', 'rtk mvn test 2>&1 | tail -n 150', true],
  ['git log | grep x | tail -n 21', 'rtk git log | grep x | tail -n 21', true],
  ['cd "C:\\x\\api" && export JAVA_HOME="C:\\j" && ./mvnw.cmd -B test 2>&1 | tail -n 150', 'rtk cd "C:\\x\\api" && export JAVA_HOME="C:\\j" && mvnw.cmd -B test 2>&1 | tail -n 150', true],
  ['git add . && git status', 'rtk git add . && git status', true],
  ['echo "a > b"', 'rtk echo "a > b"', true],
  // discarding stderr/stdout is not a redirect; a script segment does not veto the chain
  ['ls -la 2>/dev/null', 'rtk ls -la 2>/dev/null', true],
  ['where java 2>/dev/null; ls -d /c/Program\\ Files/Java/*/ 2>/dev/null', 'rtk where java 2>/dev/null; ls -d /c/Program\\ Files/Java/*/ 2>/dev/null', true],
  ['node -e "const a=1&&2; console.log(a|3)" && grep -n foo README.md', 'rtk node -e "const a=1&&2; console.log(a|3)" && grep -n foo README.md', true],
  ['cd api && python3 gen.py && git status', 'rtk cd api && python3 gen.py && git status', true],
  ['git log --oneline', 'rtk git log --oneline', false, { rewrite: fakeRewrite({ 'git log --oneline': 3 }) }],
  ['mvn test', 'rtk mvn test', true, { onlyCommands: ['mvn'] }],
  ['docker ps', 'rtk docker ps', true],
  ['git diff', 'rtk git diff', true, { excludeCommands: ['curl'] }],
  // spellings normalized before asking rtk
  ['./mvnw.cmd test', 'rtk mvnw.cmd test', true],
  ['.\\mvnw.cmd test', 'rtk mvnw.cmd test', true],
  ['cd api && JAVA_HOME="/c/x" ./mvnw.cmd test', 'rtk cd api && JAVA_HOME="/c/x" mvnw.cmd test', true],
  ['./gradlew.bat build', 'rtk gradlew.bat build', true],
  ['npm test', 'rtk npm run test', true],
  ['pnpm test -- --run', 'rtk pnpm run test -- --run', true],
  ['pnpm build', 'rtk pnpm run build', true],
  ['npm run build', 'rtk npm run build', true],
  ['cd "C:\\x\\api" && export JAVA_HOME="/c/j" && ./mvnw test', 'rtk cd "C:\\x\\api" && export JAVA_HOME="/c/j" && mvnw test', true],
  ['source ~/.bashrc && mvn test', 'rtk source ~/.bashrc && mvn test', true],
];

/** PowerShell tool inputs: state prefixes + one command. */
const PS = { tool: 'PowerShell' };
const PS_CASES = [
  ['Set-Location "C:/x/api"; .\\mvnw.cmd test', 'Set-Location "C:/x/api"; rtk mvnw.cmd test'],
  ['Set-Location "C:/x/api"\n$env:JAVA_HOME = "C:/j"\n.\\mvnw.cmd test', 'Set-Location "C:/x/api"; $env:JAVA_HOME = "C:/j"; rtk mvnw.cmd test'],
  ['git status', 'rtk git status'],
  ['cd api; npm test', 'cd api; rtk npm run test'],
];
for (const [cmd, expected] of PS_CASES) {
  test(`powershell rewrite: ${JSON.stringify(cmd)}`, () => {
    const d = decide(cmd, { ...base, ...PS });
    assert.equal(d.action, 'rewrite', d.reason);
    assert.equal(d.command, expected);
  });
}
const PS_SKIP = [
  ['$candidates = @(); Get-ChildItem "C:/x" | ForEach-Object { $_ }', 'powershell-script'],
  ['Set-Location "C:/x"', 'interactive-or-stateful'],
  ['$env:JAVA_HOME = "C:/j"', 'interactive-or-stateful'],
  ['Get-Content x | Select-Object -First 5', 'powershell-script'],
  ['Set-Location "C:/x"; rtk mvn test', 'already-wrapped'],
  ['$env:NXY_RAW = "1"; mvn test', 'escape-env'],
  ['Set-Location "C:/x"; $env:NXY_RAW = "1"; .\\mvnw.cmd test', 'escape-env'],
  ['mvn test > out.txt', 'redirect'],
];
for (const [cmd, reason] of PS_SKIP) {
  test(`powershell skip: ${JSON.stringify(cmd)} → ${reason}`, () => {
    const d = decide(cmd, { ...base, ...PS });
    assert.equal(d.action, 'skip');
    assert.equal(d.reason, reason);
  });
}

for (const [cmd, expected, allow, over] of REWRITE_CASES) {
  test(`rewrite: ${JSON.stringify(cmd)} → ${expected} (allow=${allow})`, () => {
    const d = decide(cmd, { ...base, ...(over || {}) });
    assert.equal(d.action, 'rewrite', d.reason);
    assert.equal(d.command, expected);
    assert.equal(d.allow, allow);
  });
}
