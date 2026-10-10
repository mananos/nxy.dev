import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createDriver } from '../hosts/claude-code/mod/driver.mjs';
import { panelText as lines } from '../core/panel.mjs';

const panelText = (/** @type {any} */ p) => /** @type {any[]} */ ([]).concat(lines(p)).join('\n');

const MAIN = '/w/repo';
const SESSION = '/w/repo.worktrees/sesion';
const DIRTY = '/w/repo.worktrees/sucio';
const CLEAN = '/w/repo.worktrees/limpio';

/** @param {any} [over] */
function listOut(over = {}) {
  return {
    ok: true, main: MAIN, repoName: 'repo', shell: 'powershell', platform: 'win32',
    worktrees: [
      { path: MAIN, branch: 'main', main: true, current: false, dirty: 0 },
      { path: SESSION, branch: 'feature/sesion', main: false, current: true, dirty: 0 },
      { path: DIRTY, branch: 'feature/sucio', main: false, current: false, dirty: 3 },
      { path: CLEAN, branch: 'feature/limpio', main: false, current: false, dirty: 0 },
    ],
    branches: { free: ['libre-1', 'libre-2'], busy: [{ name: 'main', path: MAIN }] },
    defaultBranch: 'main', bases: ['main', 'libre-1', 'feature/sucio'],
    ...over,
  };
}

/** A fake `$`: process.run scripted by entry name, ui.copy and fs recorded. No real git. */
function world() {
  /** @type {{ runs: any[], copies: any[], copyResult: any, submits: any[], outs: Record<string, any>, $?: any, driver?: any }} */
  const w = { runs: [], copies: [], copyResult: { isCopied: true }, submits: [], outs: { 'features:list': listOut() } };
  const snap = { ok: true, runtimeDir: '/rt/nxy', projectDir: '/proj', branch: 'main', config: { orchestrator: 'auto' } };
  w.$ = {
    plugin: { root: '/plugin' },
    fs: { read: async () => { throw new Error('ENOENT'); }, write: async () => {} },
    prompt: { submit: async (/** @type {any} */ x) => { w.submits.push(x); } },
    ui: { copy: async (/** @type {any} */ x) => { w.copies.push(x); return w.copyResult; } },
    process: {
      run: async (/** @type {string[]} */ argv) => {
        w.runs.push(argv);
        if (argv.includes('--snapshot') || argv.includes('snapshot')) return { exitCode: 0, stdout: JSON.stringify(snap), stderr: '' };
        const i = argv.findIndex((a) => /\/entries\/\w+\.mjs$/.test(a));
        const name = String(argv[i].split('/').pop()).replace('.mjs', '');
        const out = w.outs[`${name}:${argv[i + 1]}`] ?? w.outs[name] ?? '{"ok":true}';
        return { exitCode: 0, stdout: typeof out === 'string' ? out : JSON.stringify(out), stderr: '' };
      },
    },
  };
  w.driver = async () => { const d = createDriver(w.$, { cwd: '/proj', sessionId: 's1' }); await d.start(); return d; };
  return w;
}
const feats = (/** @type {any} */ w) => w.runs.filter((/** @type {string[]} */ r) => r.some((a) => /entries\/features\.mjs$/.test(a)));
const sub = (/** @type {string[]} */ r) => r[r.findIndex((a) => /entries\/features\.mjs$/.test(a)) + 1];

async function ready() {
  const w = world();
  const d = await w.driver();
  await d.press('tab:features');
  return { w, d };
}
async function toBaseStep(/** @type {any} */ d, issue = '14', name = 'algo') {
  await d.press('feat:mode:new');
  await d.submitInput('feature-issue', issue);
  await d.submitInput('feature-name', name);
}

test('opening the tab runs one features list; a second visit and other tabs do not', async () => {
  const w = world();
  const d = await w.driver();
  await d.press('tab:stats');
  await d.press('tab:memory');
  assert.equal(feats(w).length, 0);
  await d.press('tab:features');
  assert.equal(feats(w).length, 1);
  assert.equal(sub(feats(w)[0]), 'list');
  await d.press('tab:features');
  assert.equal(feats(w).length, 1);
  assert.match(panelText(d.panel()), /Worktrees/);
});

test('r reloads the list on the Features tab', async () => {
  const { w, d } = await ready();
  await d.press('refresh');
  assert.equal(feats(w).length, 2);
});

test('Rama nueva asks for the issue first: empty goes on without issue, #14 is 14, abc is an error', async () => {
  const { d } = await ready();
  await d.press('feat:mode:new');
  await d.submitInput('feature-issue', 'abc');
  assert.match(panelText(d.panel()), /issue/i);
  assert.match(panelText(d.panel()), /n\.º de issue/);
  await d.submitInput('feature-issue', '#14');
  assert.match(panelText(d.panel()), /nombre del feature/);
  await d.press('feat:mode:cancel');
  await d.press('feat:mode:new');
  await d.submitInput('feature-issue', '');
  assert.match(panelText(d.panel()), /nombre del feature/);
});

test('a valid name goes to the base step without a confirmation or a run; a clashing path does not advance', async () => {
  const { w, d } = await ready();
  const n = w.runs.length;
  await toBaseStep(d);
  assert.equal(w.runs.length, n);
  assert.match(panelText(d.panel()), /feature\/#14_algo/);
  assert.doesNotMatch(panelText(d.panel()), /git worktree add/);
  const { d: d2 } = await ready();
  await d2.press('feat:mode:new');
  await d2.submitInput('feature-issue', '');
  await d2.submitInput('feature-name', 'sucio');
  assert.match(panelText(d2.panel()), /nombre del feature/, 'still on the name step');
  assert.match(panelText(d2.panel()), /ya hay un worktree/);
});

test('pickBase shows the exact command and runs nothing until yes; no does not run either', async () => {
  const { w, d } = await ready();
  await toBaseStep(d);
  const n = w.runs.length;
  d.pickBase('feature/sucio');
  const t = panelText(d.panel());
  assert.match(t, /git worktree add -b 'feature\/#14_algo' \/w\/repo\.worktrees\/14_algo feature\/sucio/);
  assert.match(t, /feature\/sucio/);
  assert.equal(w.runs.length, n);
  await d.press('confirm-no');
  assert.equal(w.runs.length, n);
  d.pickBase('main');
  await d.press('confirm-yes');
  const run = feats(w).find((r) => sub(r) === 'new');
  assert.ok(run);
  assert.deepEqual(run.slice(run.indexOf('new') + 1, run.indexOf('new') + 7), ['--name', 'algo', '--issue', '14', '--base', 'main']);
  assert.equal(feats(w).filter((r) => sub(r) === 'list').length, 2, 'reloaded after');
  assert.doesNotMatch(panelText(d.panel()), /falta elegir de qué rama sale/);
});

test('pickBase ignores empty, unknown branches and calls outside the base step', async () => {
  const { d } = await ready();
  d.pickBase('main');
  await d.press('feat:mode:new');
  d.pickBase('main');
  await d.submitInput('feature-issue', '');
  d.pickBase('main');
  await d.submitInput('feature-name', 'algo');
  d.pickBase('');
  d.pickBase('no-existe');
  assert.doesNotMatch(panelText(d.panel()), /git worktree add/);
  assert.match(panelText(d.panel()), /falta elegir/);
});

test('a name without issue runs new without --issue', async () => {
  const { w, d } = await ready();
  await toBaseStep(d, '', 'algo');
  d.pickBase('libre-1');
  assert.match(panelText(d.panel()), /-b feature\/algo \/w\/repo\.worktrees\/algo libre-1/);
  await d.press('confirm-yes');
  const run = feats(w).find((r) => sub(r) === 'new');
  assert.ok(run && !run.includes('--issue') && run.includes('libre-1'));
});

test('Rama existente: a free branch shows the command without -b and runs new --branch', async () => {
  const { w, d } = await ready();
  await d.press('feat:mode:existing');
  d.pickBranch('libre-1');
  const t = panelText(d.panel());
  assert.match(t, /git worktree add \/w\/repo\.worktrees\/libre_1 libre-1/);
  assert.doesNotMatch(t, / -b /);
  await d.press('confirm-yes');
  const run = feats(w).find((r) => sub(r) === 'new');
  assert.ok(run);
  assert.deepEqual(run.slice(run.indexOf('new') + 1, run.indexOf('new') + 3), ['--branch', 'libre-1']);
});

test('Rama existente: a busy branch, an unknown one and the empty value open nothing', async () => {
  const { w, d } = await ready();
  await d.press('feat:mode:existing');
  d.pickBranch('main');
  d.pickBranch('');
  d.pickBranch('zzz');
  assert.doesNotMatch(panelText(d.panel()), /git worktree add/);
  await d.press('confirm-yes');
  assert.equal(feats(w).filter((r) => sub(r) === 'new').length, 0);
});

test('Cancelar at every step goes back without running anything', async () => {
  const { w, d } = await ready();
  const n = w.runs.length;
  await d.press('feat:mode:new');
  await d.press('feat:mode:cancel');
  await d.press('feat:mode:new');
  await d.submitInput('feature-issue', '');
  await d.press('feat:mode:cancel');
  await toBaseStep(d);
  await d.press('feat:mode:cancel');
  await toBaseStep(d);
  d.pickBase('main');
  await d.press('feat:mode:cancel');
  assert.doesNotMatch(panelText(d.panel()), /git worktree add/);
  await d.press('confirm-yes');
  await d.press('feat:mode:existing');
  d.pickBranch('libre-1');
  await d.press('feat:mode:cancel');
  await d.press('confirm-yes');
  assert.equal(w.runs.length, n);
  assert.match(panelText(d.panel()), /Rama nueva/);
});

test('Cerrar a dirty worktree asks with «se pierden» and passes --force only after yes; a clean one does not', async () => {
  const { w, d } = await ready();
  await d.press('feat:close:2');
  assert.match(panelText(d.panel()), /SE PIERDEN/);
  assert.equal(feats(w).filter((r) => sub(r) === 'close').length, 0);
  await d.press('confirm-yes');
  const run = feats(w).find((r) => sub(r) === 'close');
  assert.ok(run && run.includes('--force') && run.includes(DIRTY));
  const x = await ready();
  await x.d.press('feat:close:3');
  assert.doesNotMatch(panelText(x.d.panel()), /SE PIERDEN/);
  await x.d.press('confirm-yes');
  const run2 = feats(x.w).find((r) => sub(r) === 'close');
  assert.ok(run2 && !run2.includes('--force') && run2.includes(CLEAN));
});

test('Cerrar with unknown changes (dirty null) asks «se pierden» and passes --force only after yes', async () => {
  const w = world();
  w.outs['features:list'] = listOut({ worktrees: listOut().worktrees.map((x, i) => (i === 3 ? { ...x, dirty: null } : x)) });
  const d = await w.driver();
  await d.press('tab:features');
  await d.press('feat:close:3');
  assert.match(panelText(d.panel()), /SE PIERDEN/);
  assert.equal(feats(w).filter((r) => sub(r) === 'close').length, 0);
  await d.press('confirm-yes');
  const run = feats(w).find((r) => sub(r) === 'close');
  assert.ok(run && run.includes('--force') && run.includes(CLEAN));
});

test('Pausar and Cerrar leave a half-filled Nuevo form intact', async () => {
  const { w, d } = await ready();
  await toBaseStep(d);
  await d.press('feat:pause:3');
  assert.match(panelText(d.panel()), /falta elegir de qué rama sale/);
  await d.press('feat:close:3');
  await d.press('confirm-yes');
  assert.ok(feats(w).find((r) => sub(r) === 'close'));
  assert.match(panelText(d.panel()), /falta elegir de qué rama sale/);
  assert.match(panelText(d.panel()), /feature\/#14_algo/);
});

test('Cerrar reloads the list before asking', async () => {
  const { w, d } = await ready();
  w.outs['features:list'] = listOut({ worktrees: listOut().worktrees.map((x, i) => (i === 3 ? { ...x, dirty: 2 } : x)) });
  await d.press('feat:close:3');
  assert.match(panelText(d.panel()), /SE PIERDEN/);
  await d.press('confirm-yes');
  assert.ok(feats(w).find((r) => sub(r) === 'close')?.includes('--force'));
});

test('the main worktree and this session cannot be closed', async () => {
  const { w, d } = await ready();
  const n = w.runs.length;
  await d.press('feat:close:0');
  await d.press('feat:close:1');
  await d.press('confirm-yes');
  assert.equal(w.runs.length, n);
});

test('a worktree with a running plan cannot be closed: no confirmation, nothing runs', async () => {
  const { w, d } = await ready();
  w.outs['features:list'] = listOut({ worktrees: listOut().worktrees.map((x, i) => (i === 3 ? { ...x, running: true } : x)) });
  await d.press('r');
  await d.press('feat:close:3');
  assert.doesNotMatch(panelText(d.panel()), /¿Cerrar|SE PIERDEN/);
  await d.press('confirm-yes');
  assert.equal(feats(w).filter((r) => sub(r) === 'close').length, 0);
});

test('Abrir copies the PowerShell or the bash command by shell, and shows the other one', async () => {
  const { w, d } = await ready();
  await d.press('feat:open:3');
  assert.equal(w.copies[0].text, `cd '${CLEAN}'; claude`);
  assert.match(panelText(d.panel()), /&& claude/);
  const w2 = world();
  w2.outs['features:list'] = listOut({ shell: 'posix' });
  const d2 = await w2.driver();
  await d2.press('tab:features');
  await d2.press('feat:open:3');
  assert.equal(w2.copies[0].text, `cd '${CLEAN}' && claude`);
  w2.copyResult = { isCopied: false, reason: 'sin portapapeles' };
  await d2.press('feat:open:3');
  assert.match(panelText(d2.panel()), /sin portapapeles/);
  assert.match(panelText(d2.panel()), /claude/);
});

test('Abrir ignores this session', async () => {
  const { w, d } = await ready();
  await d.press('feat:open:1');
  assert.equal(w.copies.length, 0);
});

test('Pausar runs pause (no prompt.submit) and shows the handoff and the advice; a paused one resumes', async () => {
  const { w, d } = await ready();
  w.outs['features:pause'] = { ok: true, paused: true, exists: true, stale: true, branch: 'feature/limpio', handoff: 'mi handoff', truncated: false, advice: 'El handoff es anterior', open: 'x' };
  await d.press('feat:pause:3');
  const run = feats(w).find((r) => sub(r) === 'pause');
  assert.ok(run && run.includes('--path') && run.includes(CLEAN));
  assert.equal(w.submits.length, 0);
  assert.match(panelText(d.panel()), /mi handoff/);
  assert.match(panelText(d.panel()), /anterior/);
  w.outs['features:list'] = listOut({ worktrees: listOut().worktrees.map((x, i) => (i === 3 ? { ...x, pausedAt: 5 } : x)) });
  await d.press('refresh');
  await d.press('feat:pause:3');
  assert.ok(feats(w).find((r) => sub(r) === 'resume'));
});
