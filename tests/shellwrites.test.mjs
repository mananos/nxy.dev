// @ts-check
/** Copy-before-console-writes: the parser (core/shellwrites.mjs) and the Bash hook that uses it. */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { writeTargets } from '../core/shellwrites.mjs';
import { readBaseline } from '../hosts/claude-code/baseline.mjs';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const MEM = join(ROOT, 'hosts', 'claude-code', 'entries', 'mem.mjs');
const HOOK = join(ROOT, 'hosts', 'claude-code', 'hooks', 'pretooluse-bash.mjs');
const NODE = ['--disable-warning=ExperimentalWarning'];

const cwd = mkdtempSync(join(tmpdir(), 'nxy-sw-'));
const abs = (...p) => resolve(cwd, ...p);
const targets = (cmd, tool = 'Bash') => writeTargets(cmd, cwd, tool);

test('recognised writes', () => {
  assert.deepEqual(targets("sed -i 's/a/b/' f"), [abs('f')]);
  assert.deepEqual(targets('sed -i.bak -e s/a/b/ f g'), [abs('f'), abs('g')]);
  assert.deepEqual(targets("perl -pi -e 's/a/b/' f"), [abs('f')]);
  assert.deepEqual(targets('echo x > a.txt'), [abs('a.txt')]);
  assert.deepEqual(targets('echo x >> a.txt'), [abs('a.txt')]);
  assert.deepEqual(targets('cmd 2> err.log'), [abs('err.log')]);
  assert.deepEqual(targets('echo x | tee -a out.txt'), [abs('out.txt')]);
  assert.deepEqual(targets('cp a b'), [abs('b')]);
  assert.deepEqual(targets('mv a b').sort(), [abs('a'), abs('b')].sort());
  assert.deepEqual(targets('truncate -s 0 f'), [abs('f')]);
  assert.deepEqual(targets('dd if=a of=b'), [abs('b')]);
  assert.deepEqual(targets('echo x > "my file.txt"'), [abs('my file.txt')]);
  assert.deepEqual(targets('cd sub && echo x > f'), [abs('sub', 'f')]);
  assert.deepEqual(targets('FOO=1 sudo rtk sed -i s/a/b/ f'), [abs('f')]);
});

test('cp into an existing directory', () => {
  mkdirSync(join(cwd, 'dir'), { recursive: true });
  assert.deepEqual(targets('cp a dir'), [abs('dir', 'a')]);
  assert.deepEqual(targets('cp a b dir/'), [abs('dir', 'a'), abs('dir', 'b')]);
});

test('PowerShell', () => {
  assert.deepEqual(targets('Set-Content -Path out.txt -Value x', 'PowerShell'), [abs('out.txt')]);
  assert.deepEqual(targets("'x' | Out-File o.txt", 'PowerShell'), [abs('o.txt')]);
  assert.deepEqual(targets('echo x > a.txt', 'PowerShell'), [abs('a.txt')]);
  assert.deepEqual(targets('sed -i s/a/b/ f', 'PowerShell'), []);
});

test('ignored', () => {
  assert.deepEqual(targets('cmd 2>&1'), []);
  assert.deepEqual(targets('cmd >/dev/null 2>&1'), []);
  assert.deepEqual(targets('echo "a > b"'), []);
  assert.deepEqual(targets('cat <<EOF\nfoo > x\nEOF'), []);
  assert.deepEqual(targets('cat > out.txt <<EOF\nfoo > x\nEOF'), [abs('out.txt')]);
  assert.deepEqual(targets('echo x > "$OUT"'), []);
  assert.deepEqual(targets('ls *.txt'), []);
  assert.deepEqual(targets('cd - && echo x > rel'), []);
  assert.deepEqual(targets('echo "line one\na > x" '), []);
  assert.deepEqual(targets("echo 'line one\na > x'"), []);
  assert.deepEqual(targets("echo a\necho b > f"), [abs('f')]);
  assert.deepEqual(targets("perl -Mstrict -e 'print 1' f"), []);
  assert.deepEqual(targets("perl -pi -e 's/a/b/' f"), [abs('f')]);
  assert.deepEqual(targets(/** @type {any} */ (null)), []);
});

test('parsing is cheap', () => {
  const cmd = Array.from({ length: 20 }, (_, i) => `echo ${i} > f${i}.txt`).join(' && ');
  const t0 = Date.now();
  for (let i = 0; i < 1000; i++) writeTargets(cmd, cwd);
  assert.ok(Date.now() - t0 < 1000);
});

function setup() {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-swh-'));
  const repo = join(dir, 'repo');
  mkdirSync(join(repo, '.git'), { recursive: true });
  writeFileSync(join(repo, '.git', 'HEAD'), 'ref: refs/heads/feature/y\n');
  writeFileSync(join(repo, '.git', 'config'), '[remote "origin"]\n\turl = git@github.com:x/z.git\n');
  const env = { ...process.env, NXY_HOME: join(dir, 'home'), CLAUDE_PROJECT_DIR: repo };
  writeFileSync(join(repo, 'f.txt'), 'before\n');
  writeFileSync(join(repo, 'g.txt'), 'before-g\n');
  writeFileSync(join(repo, 'h.txt'), 'before-h\n');
  const plan = ['## Plan', 'Goal: x', '### Batch 1 — api: svc', '- `f.txt:1` — change', 'Accept: `./mvnw -q test`'].join('\n');
  const hook = (input) => spawnSync(process.execPath, [...NODE, HOOK], { input, encoding: 'utf8', env, cwd: repo });
  const event = (command) => JSON.stringify({ cwd: repo, tool_name: 'Bash', tool_input: { command } });
  return { repo, env, plan, hook, event };
}

test('hook copies before sed -i, a redirect and cp, only with an active plan', () => {
  const { repo, env, plan, hook, event } = setup();
  const branch = 'feature/y';
  for (const c of ['sed -i s/a/b/ f.txt', 'echo x > g.txt', 'cp a h.txt']) assert.equal(hook(event(c)).status, 0);
  assert.deepEqual(Object.keys(readBaseline(repo, branch)), [], 'no plan: nothing copied');
  // the baseline lives in <repo>/.nxy/local (not under NXY_HOME), so readBaseline needs no env
  const r = spawnSync(process.execPath, [...NODE, MEM, 'handoff', 'plan'], { input: plan, encoding: 'utf8', env, cwd: repo });
  assert.equal(r.status, 0, r.stderr);
  for (const c of ['sed -i s/a/b/ f.txt', 'echo x > g.txt', 'cp a h.txt']) {
    const out = hook(event(c));
    assert.equal(out.status, 0);
  }
  const base = readBaseline(repo, branch);
  assert.deepEqual(Object.keys(base).sort(), [join(repo, 'f.txt'), join(repo, 'g.txt'), join(repo, 'h.txt')].sort());
  const copy = base[join(repo, 'g.txt')];
  assert.ok(copy.copy);
  const dir = join(repo, '.nxy', 'local', 'baseline', 'feature_y');
  assert.equal(readFileSync(join(dir, copy.copy), 'utf8'), 'before-g\n');
});

test('hook: targets outside the project, under .nxy or under .git are not copied', () => {
  const { repo, env, plan, hook, event } = setup();
  spawnSync(process.execPath, [...NODE, MEM, 'handoff', 'plan'], { input: plan, encoding: 'utf8', env, cwd: repo });
  const outside = join(repo, '..', 'outside.txt');
  writeFileSync(outside, 'o\n');
  const outsideCmd = resolve(outside).replace(/\\/g, '/'); // backslashes are escapes in the Bash command
  writeFileSync(join(repo, '.nxy', 'note.txt'), 'n\n');
  writeFileSync(join(repo, '.git', 'note.txt'), 'g\n');
  for (const c of [`echo x > "${outsideCmd}"`, 'echo x > .nxy/note.txt', 'echo x > .git/note.txt']) assert.equal(hook(event(c)).status, 0);
  assert.deepEqual(Object.keys(readBaseline(repo, 'feature/y')), []);
});

test('hook: a subagent shell write into the project is denied; main thread and other cases pass', () => {
  const { repo, hook } = setup();
  const ev = (command, sub) => JSON.stringify({ cwd: repo, tool_name: 'Bash', tool_input: { command }, ...(sub ? { agent_id: 'a1', agent_type: 'nxy:implementer' } : {}) });
  const outside = resolve(repo, '..', 'outside.txt').replace(/\\/g, '/');
  for (const c of ['sed -i s/a/b/ f.txt', 'echo x > g.txt', 'cat >> f.txt <<EOF\nline\nEOF']) {
    const r = hook(ev(c, true));
    assert.equal(r.status, 0);
    const out = JSON.parse(r.stdout).hookSpecificOutput;
    assert.equal(out.permissionDecision, 'deny', c);
    assert.match(out.permissionDecisionReason, /Edit\/Write/);
    assert.ok(!JSON.stringify(hook(ev(c, false)).stdout).includes('deny'), `main thread: ${c}`);
  }
  for (const c of ['cp a b', `node --test x > "${outside}"`, 'cmd > /dev/null 2>&1', 'echo x > .nxy/n.txt']) {
    const r = hook(ev(c, true));
    assert.equal(r.status, 0);
    assert.ok(!r.stdout.includes('deny'), c);
  }
  const bad = hook('not json');
  assert.equal(bad.status, 0);
  assert.equal(bad.stdout, '');
});

test('hook: nothing for 2>&1, >/dev/null; malformed events exit 0 silently', () => {
  const { repo, env, plan, hook, event } = setup();
  spawnSync(process.execPath, [...NODE, MEM, 'handoff', 'plan'], { input: plan, encoding: 'utf8', env, cwd: repo });
  for (const c of ['ls 2>&1', 'ls >/dev/null 2>&1']) hook(event(c));
  assert.deepEqual(Object.keys(readBaseline(repo, 'feature/y')), []);
  for (const bad of ['not json', JSON.stringify({ tool_name: 'Bash', tool_input: {} }), '']) {
    const r = hook(bad);
    assert.equal(r.status, 0);
    assert.equal(r.stdout, '');
  }
});
