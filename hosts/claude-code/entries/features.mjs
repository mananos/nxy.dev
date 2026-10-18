#!/usr/bin/env node
// @ts-check
/**
 * The Features tab's entry: the repo's worktrees and the actions on them.
 *
 * Usage (all take --cwd <dir> and accept --json; the output is always one JSON line):
 *   features.mjs list
 *   features.mjs new --name <n> [--issue <n>] --base <branch>     (new branch from a local base)
 *   features.mjs new --branch <branch>                             (an existing local branch)
 *   (either form takes --carry <task id>: moves this checkout's unpinned task into the new worktree)
 *   features.mjs pause --path <worktree>     (marks it and shows the handoff that already exists)
 *   features.mjs resume --path <worktree>
 *   features.mjs close --path <worktree> [--force]
 *
 * Fail-open: any error is `{"ok":false,"reason":...}`. Nothing here commits or pushes, and nothing is
 * asked of the model: pausing only marks the worktree.
 */
import { parseArgs } from '../../../core/format.mjs';
import { toNativePath } from '../../../core/paths.mjs';
import { openCommand, shellOf } from '../../../core/featuresview.mjs';
import { extractPlan } from '../../../core/plan.mjs';
import { armPlan } from '../plan-arm.mjs';
import { carryTask } from '../tasks.mjs';
import { addWorktree, findWorktree, listWorktrees, removeWorktree, setPaused } from '../worktrees.mjs';

const { opts, positional } = parseArgs(process.argv.slice(2));
const cwd = toNativePath(typeof opts.cwd === 'string' ? opts.cwd : process.env.CLAUDE_PROJECT_DIR || process.cwd());
const action = (positional[0] || 'list').toLowerCase();

/** @param {unknown} obj */
const emit = (obj) => console.log(JSON.stringify(obj));
/** @param {string} reason */
const fail = (reason) => emit({ ok: false, reason });
/** @param {string} key */
const str = (key) => (typeof opts[key] === 'string' ? /** @type {string} */ (opts[key]) : null);

/**
 * With `--carry <id>`: moves the unpinned task into the fresh worktree and arms its plan there. The worktree
 * exists either way; a failed carry leaves the task where it was and says why in `carryError`.
 * @param {any} res result of addWorktree
 */
async function withCarry(res) {
  const id = str('carry');
  if (!res || !res.ok || id === null) return res;
  try {
    const { projectKey } = await import('../../../core/memory/scope.mjs');
    const { openStore } = await import('../../../core/memory/store.mjs');
    const { liveHandoff } = await import('../../../core/memory/handoff.mjs');
    const project = projectKey(cwd).key;
    if (projectKey(res.path).key !== project) {
      return { ...res, carryError: 'este repo no tiene un proyecto de memoria compartido (por ejemplo, no tiene remoto git): la tarea queda en este checkout' };
    }
    const db = openStore();
    let body = '';
    try {
      body = liveHandoff(db, project, id)?.body || '';
    } finally {
      db.close();
    }
    const plan = extractPlan(body);
    if (!plan) return { ...res, carryError: 'la tarea no tiene un plan guardado para llevar' };
    const moved = carryTask(cwd, res.path, id);
    if (!moved.ok) return { ...res, carryError: moved.reason };
    const carried = { id: moved.id, goal: moved.goal };
    try {
      if (process.env.NXY_TEST_ARM_FAIL === '1') throw new Error('armPlan forced to fail');
      armPlan(res.path, id, plan);
    } catch (e) {
      // The task already moved: it is carried, but its approval marker is not armed in the worktree.
      return { ...res, carried, carryWarning: `la tarea se llevó, pero no se pudo armar su plan en el worktree (${String(/** @type {any} */ (e)?.message || e)}): guardá el plan de nuevo allí con mem handoff plan --task ${id}` };
    }
    return { ...res, carried };
  } catch (e) {
    return { ...res, carryError: String(/** @type {any} */ (e)?.message || e) };
  }
}

async function main() {
  const shell = shellOf({ platform: process.platform, env: process.env });
  switch (action) {
    case 'list':
      emit(await listWorktrees(cwd));
      return;
    case 'new': {
      const name = str('name');
      const branch = str('branch');
      const base = str('base');
      if (name !== null && branch !== null) return fail('usá --name con --base, o --branch, no los dos');
      if (name === null && branch === null) return fail('falta --name con --base, o --branch');
      if (name !== null) {
        if (base === null) return fail('falta --base: elegí la rama de la que sale la rama nueva');
        const issue = str('issue');
        emit(await withCarry(addWorktree(cwd, { name, base, ...(issue !== null ? { issue } : {}) }, shell)));
        return;
      }
      if (base !== null || opts.issue !== undefined) return fail('--base y --issue sólo van con --name');
      emit(await withCarry(addWorktree(cwd, { branch: /** @type {string} */ (branch) }, shell)));
      return;
    }
    case 'pause':
    case 'resume': {
      const path = str('path');
      if (!path) return fail('falta --path');
      if (!findWorktree(cwd, path)) return fail('ese worktree no está en la lista');
      const on = action === 'pause';
      const res = await setPaused(path, on);
      if (!res.ok || !on) return emit(res);
      const lines = (res.body || '').split('\n');
      const shown = lines.slice(0, 30).join('\n');
      const advice = !res.exists
        ? 'No hay handoff de este worktree. En su terminal corré `/nxy:mem handoff save` para dejarlo.'
        : res.stale
          ? 'El handoff es anterior a los últimos cambios. En la terminal de ese worktree corré `/nxy:mem handoff save` para actualizarlo.'
          : null;
      return emit({ ...res, handoff: shown, truncated: lines.length > 30, advice, open: openCommand(path, shell) });
    }
    case 'close': {
      const path = str('path');
      if (!path) return fail('falta --path');
      emit(removeWorktree(cwd, path, Boolean(opts.force), shell));
      return;
    }
    default:
      return fail(`acción desconocida «${action}». Usá: list | new | pause | resume | close`);
  }
}

try {
  await main();
} catch (e) {
  fail(String(/** @type {any} */ (e)?.message || e));
}
