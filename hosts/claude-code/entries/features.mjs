#!/usr/bin/env node
// @ts-check
/**
 * The Features tab's entry: the repo's worktrees and the actions on them.
 *
 * Usage (all take --cwd <dir> and accept --json; the output is always one JSON line):
 *   features.mjs list
 *   features.mjs new --name <n> [--issue <n>] --base <branch>     (new branch from a local base)
 *   features.mjs new --branch <branch>                             (an existing local branch)
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
        emit(addWorktree(cwd, { name, base, ...(issue !== null ? { issue } : {}) }, shell));
        return;
      }
      if (base !== null || opts.issue !== undefined) return fail('--base y --issue sólo van con --name');
      emit(addWorktree(cwd, { branch: /** @type {string} */ (branch) }, shell));
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
