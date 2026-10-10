// @ts-check
/**
 * The Features tab's git side: lists the repo's worktrees with what the panel shows about each one,
 * creates and removes them, and marks a pause. The names, argv and texts come from core/featuresview.mjs
 * (pure); here git runs and files are read.
 *
 * Nothing here commits or pushes. A new worktree never gets a base from the session's HEAD: the base is
 * always explicit. The progress of each worktree is read the way orch.mjs reads it, from the files nxy
 * keeps in that worktree's `.nxy/local` and from its live handoff.
 */
import { existsSync, mkdirSync, readFileSync, realpathSync, renameSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { gitBranch, gitRoot, nxyRuntimeDir } from '../../core/paths.mjs';
import { isDone } from '../../core/batch-status.mjs';
import { extractPlan, parseBatches, planHash } from '../../core/plan.mjs';
import {
  addArgv, baseChoices, branchChoices, commandText, defaultBranchOf, featureNames, parseIssue, parseWorktrees,
  removeArgv, shellOf,
} from '../../core/featuresview.mjs';
import { gitResult, gitRun } from './gitstate.mjs';
import { lookupHandoff } from './handoff.mjs';
import { readActive } from './orch-state.mjs';
import { readReview, readSuite, readVerify } from './verify-state.mjs';

const MAX_DIRTY = 5000;
const MAX_BRANCHES = 200;
const MAX_MTIME_FILES = 200;

/** @param {string} p */
const real = (p) => {
  let r = p;
  try {
    r = realpathSync.native(p);
  } catch {
    /* gone or never there: compare as given */
  }
  r = r.replace(/\\/g, '/').replace(/\/+$/, '');
  return process.platform === 'win32' ? r.toLowerCase() : r;
};

/** @param {string} path */
const pausedFile = (path) => join(nxyRuntimeDir(path), 'features', 'paused.json');

/**
 * Files with changes in one worktree, as git sees them (nothing excluded), capped.
 * @param {string} path
 * @returns {string[] | null} null when git could not read the status
 */
function dirtyList(path) {
  const out = gitRun(path, ['status', '--porcelain', '-z', '-uall']);
  if (out == null) return null;
  const parts = out.split('\0');
  /** @type {string[]} */
  const files = [];
  for (let i = 0; i < parts.length && files.length < MAX_DIRTY; i++) {
    const e = parts[i];
    if (e.length < 4) continue;
    if (/[RC]/.test(e.slice(0, 2))) i++;
    files.push(e.slice(3));
  }
  return files;
}

/** @param {string} path @returns {{at: number, branch: string | null} | null} */
function readPaused(path) {
  try {
    const d = JSON.parse(readFileSync(pausedFile(path), 'utf8'));
    return d && typeof d.at === 'number' ? { at: d.at, branch: d.branch ?? null } : null;
  } catch {
    return null;
  }
}

/** @param {string} root @returns {string[]} */
export function listBranches(root) {
  const r = gitResult(root, ['for-each-ref', 'refs/heads', '--sort=-committerdate', '--format=%(refname:short)']);
  if (r.status !== 0) return [];
  return r.stdout.split(/\r?\n/).map((l) => l.trim()).filter(Boolean).slice(0, MAX_BRANCHES);
}

/** @param {string} cwd */
function repoRootOf(cwd) {
  return gitRoot(cwd);
}

/**
 * @param {string} root
 * @returns {import('../../core/featuresview.mjs').Worktree[] | null}
 */
function gitWorktrees(root) {
  const r = gitResult(root, ['worktree', 'list', '--porcelain']);
  if (r.status !== 0) return null;
  const list = parseWorktrees(r.stdout);
  return list.length ? list : null;
}

/**
 * The progress, handoff time and running marker of one worktree.
 * @param {import('../../core/featuresview.mjs').Worktree} wt
 */
async function progressOf(wt) {
  const path = wt.path;
  const branch = gitBranch(path);
  let handoffAt = null;
  let batchDone = 0;
  let batchTotal = 0;
  let reviewDone = false;
  /** @type {string | null} */
  let suite = null;
  try {
    const known = await lookupHandoff(path);
    if (known.handoff) {
      handoffAt = known.handoff.updated;
      const plan = extractPlan(known.handoff.body);
      if (plan) {
        const hash = planHash(plan);
        const verdicts = readVerify(path, branch, hash).batches;
        const batches = parseBatches(plan);
        batchTotal = batches.length;
        batchDone = batches.filter((b) => isDone(verdicts[String(b.n)]?.status)).length;
        reviewDone = !!readReview(path, hash);
        suite = readSuite(path, hash)?.status ?? null;
      }
    }
  } catch {
    /* a worktree whose handoff cannot be read still lists */
  }
  return { handoffAt, batchDone, batchTotal, reviewDone, suite };
}

/**
 * Worktrees of the repo that holds `cwd`, plus the branch lists the "new" form needs.
 * @param {string} cwd
 */
export async function listWorktrees(cwd) {
  const root = repoRootOf(cwd);
  if (!root) return { ok: false, reason: 'no es un repo git' };
  const list = gitWorktrees(root);
  if (!list) return { ok: false, reason: 'git no pudo listar los worktrees' };
  const here = real(root);
  const rows = [];
  // Without remotes every commit would count as unpushed: one check per list, not per worktree.
  const hasRemotes = !!gitRun(root, ['for-each-ref', '--count=1', 'refs/remotes'])?.trim();
  for (const wt of list) {
    const dirty = dirtyList(wt.path);
    const ahead = hasRemotes
      ? Number(gitRun(wt.path, ['rev-list', '--count', 'HEAD', '--not', '--remotes'])?.trim()) || 0
      : 0;
    const prog = await progressOf(wt);
    const paused = readPaused(wt.path);
    rows.push({
      path: wt.path,
      branch: wt.branch,
      main: wt.main,
      locked: wt.locked,
      current: real(wt.path) === here,
      dirty: dirty ? dirty.length : null,
      ahead,
      ...prog,
      pausedAt: paused ? paused.at : null,
      running: !!readActive(wt.path),
      missingDeps: existsSync(join(wt.path, 'package.json')) && !existsSync(join(wt.path, 'node_modules')),
    });
  }
  const main = list[0];
  const branches = listBranches(main.path);
  if (main.branch && !branches.includes(main.branch)) branches.unshift(main.branch);
  const originHead = gitRun(main.path, ['symbolic-ref', '--short', '-q', 'refs/remotes/origin/HEAD'])?.trim() || null;
  const defaultBranch = defaultBranchOf({ originHead, mainBranch: main.branch, branches });
  return {
    ok: true,
    main: main.path,
    repoName: main.path.slice(main.path.lastIndexOf('/') + 1),
    worktrees: rows,
    branches: branchChoices(branches, list),
    defaultBranch,
    bases: baseChoices(branches, defaultBranch),
    shell: shellOf({ platform: process.platform, env: process.env }),
    platform: process.platform,
  };
}

/** @param {string} root @param {string} branch */
const hasBranch = (root, branch) => gitResult(root, ['show-ref', '--verify', '--quiet', `refs/heads/${branch}`]).status === 0;

/**
 * Creates a worktree: `{issue, name, base}` is a new branch from `base`, `{branch}` an existing one.
 * Every check runs before `git worktree add`.
 * @param {string} cwd
 * @param {{issue?: string, name?: string, base?: string, branch?: string}} input
 * @param {import('../../core/featuresview.mjs').Shell} [shell]
 * @returns {{ok: true, command: string, path: string, branch: string} | {ok: false, reason: string}}
 */
export function addWorktree(cwd, input, shell = shellOf({ platform: process.platform, env: process.env })) {
  const root = repoRootOf(cwd);
  const list = root ? gitWorktrees(root) : null;
  if (!root || !list) return { ok: false, reason: 'no es un repo git' };
  const main = list[0].path;
  if (input.issue) {
    const iss = parseIssue(input.issue);
    if (!iss.ok) return { ok: false, reason: iss.error };
  }
  const names = featureNames(input, main);
  if (!names.ok) return { ok: false, reason: names.error };
  if (existsSync(names.path)) return { ok: false, reason: `la carpeta ${names.path} ya existe` };
  if (names.create) {
    if (hasBranch(main, names.branch)) {
      return { ok: false, reason: `la rama ${names.branch} ya existe: usá «rama existente» si querés abrirla` };
    }
    if (!hasBranch(main, /** @type {string} */ (names.base))) {
      return { ok: false, reason: `la base ${names.base} no existe como rama local` };
    }
  } else {
    if (!hasBranch(main, names.branch)) return { ok: false, reason: `la rama ${names.branch} no existe como rama local` };
    const held = list.find((w) => w.branch === names.branch);
    if (held) return { ok: false, reason: `la rama ${names.branch} ya está abierta en ${held.path}` };
  }
  const argv = addArgv(names.branch, names.path, names.create, names.base);
  const r = gitResult(main, argv.slice(1));
  if (r.status !== 0) return { ok: false, reason: (r.stderr || 'git worktree add falló').trim() };
  return { ok: true, command: commandText(argv, shell), path: names.path, branch: names.branch };
}

/**
 * Whether `path` is one of the repo's worktrees (compared by real path), and which.
 * @param {string} cwd @param {string} path
 */
export function findWorktree(cwd, path) {
  const root = repoRootOf(cwd);
  const list = root ? gitWorktrees(root) : null;
  if (!list) return null;
  const want = real(path);
  const wt = list.find((w) => real(w.path) === want);
  return wt ? { wt, list, root: /** @type {string} */ (root) } : null;
}

/**
 * Removes a worktree. Never `--force` unless asked; the branch stays.
 * @param {string} cwd @param {string} path @param {boolean} [force]
 * @param {import('../../core/featuresview.mjs').Shell} [shell]
 * @returns {{ok: true, command: string} | {ok: false, reason: string}}
 */
export function removeWorktree(cwd, path, force = false, shell = shellOf({ platform: process.platform, env: process.env })) {
  const found = findWorktree(cwd, path);
  if (!found) return { ok: false, reason: 'ese worktree no está en la lista' };
  const { wt, list, root } = found;
  if (wt.main) return { ok: false, reason: 'el worktree principal no se cierra' };
  if (real(wt.path) === real(root)) return { ok: false, reason: 'no se cierra el worktree de esta sesión' };
  if (readActive(wt.path)) return { ok: false, reason: 'tiene un plan corriendo en otra sesión' };
  const argv = removeArgv(wt.path, force);
  const r = gitResult(list[0].path, argv.slice(1));
  if (r.status !== 0) return { ok: false, reason: (r.stderr || 'git worktree remove falló').trim() };
  return { ok: true, command: commandText(argv, shell) };
}

/**
 * Writes or deletes the pause mark of a worktree (~100 bytes) and tells whether its handoff exists and
 * whether it is older than the last change. The handoff itself is the model's: nothing is generated here.
 * @param {string} path @param {boolean} on
 */
export async function setPaused(path, on) {
  const branch = gitBranch(path);
  const file = pausedFile(path);
  try {
    if (on) {
      mkdirSync(join(file, '..'), { recursive: true });
      const tmp = `${file}.${process.pid}.tmp`;
      writeFileSync(tmp, JSON.stringify({ at: Date.now(), branch }), 'utf8');
      renameSync(tmp, file);
    } else {
      rmSync(file, { force: true });
    }
  } catch (e) {
    return { ok: false, reason: String(/** @type {any} */ (e)?.message || e) };
  }
  let exists = false;
  let stale = false;
  let updated = null;
  let body = '';
  try {
    const known = await lookupHandoff(path);
    if (known.handoff) {
      exists = true;
      updated = known.handoff.updated;
      const plan = extractPlan(known.handoff.body);
      body = String(known.handoff.body || '').replace(/\r\n/g, '\n');
      if (plan) body = body.replace(plan, '');
      body = body.trim();
      let latest = 0;
      for (const f of (dirtyList(path) ?? []).filter((x) => x !== '.nxy' && !x.startsWith('.nxy/')).slice(0, MAX_MTIME_FILES)) {
        try {
          latest = Math.max(latest, statSync(join(path, f)).mtimeMs);
        } catch {
          /* deleted file */
        }
      }
      stale = latest > updated;
    }
  } catch {
    /* no handoff readable */
  }
  return { ok: true, paused: on, exists, stale, updated, body, branch };
}
