// @ts-check
// Task identity: pure, immutable transforms. A task is born when a plan is saved; it follows the checkout
// (`unpinned`) until its first edit pins it to the branch it was edited on (`branches[branch]`). I/O lives in
// hosts/claude-code/tasks.mjs.
import { createHash } from 'node:crypto';

/**
 * @typedef {{ id: string, goal: string, created: number, pinned?: number, offered?: boolean }} Entry
 * @typedef {{ unpinned: Entry|null, branches: Record<string, Entry> }} TasksState
 * @typedef {Entry & { branch: string|null }} FoundTask
 * @typedef {{ id: string, label: string, goal: string, branch: string|null, pinned: boolean, source: 'pinned'|'unpinned'|'branch' }} Resolved
 */

export const NO_BRANCH = '(no branch)';
const GOAL_MAX = 120;
const SLUG_MAX = 40;

/** @returns {TasksState} */
export const emptyTasks = () => ({ unpinned: null, branches: {} });

const capGoal = (goal) => String(goal || '').replace(/\s+/g, ' ').trim().slice(0, GOAL_MAX);

/**
 * Task id from the plan's Goal: readable slug + 6 hex of sha1(project, goal, now).
 * @param {string} goal
 * @param {string} project
 * @param {number} now
 */
export function taskIdFromGoal(goal, project, now) {
  const slug = String(goal || '').toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '').slice(0, SLUG_MAX).replace(/-$/, '') || 'task';
  const hash = createHash('sha1').update(`${project}\n${goal}\n${now}`).digest('hex').slice(0, 6);
  return `${slug}-${hash}`;
}

/** @returns {Entry|null} */
function normalizeEntry(e) {
  if (!e || typeof e !== 'object' || typeof e.id !== 'string' || !e.id) return null;
  /** @type {Entry} */
  const out = { id: e.id, goal: capGoal(typeof e.goal === 'string' ? e.goal : ''), created: Number.isFinite(e.created) ? e.created : 0 };
  if (Number.isFinite(e.pinned)) out.pinned = e.pinned;
  if (e.offered === true) out.offered = true;
  return out;
}

/**
 * Keeps what is well formed, drops the rest.
 * @param {any} raw
 * @returns {TasksState}
 */
export function normalizeTasks(raw) {
  const state = emptyTasks();
  if (!raw || typeof raw !== 'object') return state;
  state.unpinned = normalizeEntry(raw.unpinned);
  if (raw.branches && typeof raw.branches === 'object' && !Array.isArray(raw.branches)) {
    for (const [branch, e] of Object.entries(raw.branches)) {
      const entry = normalizeEntry(e);
      if (entry) state.branches[branch] = entry;
    }
  }
  return state;
}

/** @param {TasksState} state */
export const isEmptyTasks = (state) => !state.unpinned && Object.keys(state.branches).length === 0;

const branchKey = (branch) => branch || NO_BRANCH;

/**
 * The task active on a branch. Precedence: pinned for this branch > unpinned > the branch itself (legacy: a
 * handoff is the task whose id is the branch). An unpinned task is never active on a branch with a pinned one.
 * @param {TasksState} state
 * @param {string|null} branch
 * @returns {Resolved}
 */
export function resolveTask(state, branch) {
  const key = branchKey(branch);
  const pinned = state.branches[key];
  if (pinned) return { id: pinned.id, label: pinned.goal || pinned.id, goal: pinned.goal, branch: key, pinned: true, source: 'pinned' };
  if (state.unpinned) {
    const u = state.unpinned;
    return { id: u.id, label: u.goal || u.id, goal: u.goal, branch: branch || null, pinned: false, source: 'unpinned' };
  }
  return { id: key, label: key, goal: '', branch: branch || null, pinned: false, source: 'branch' };
}

/** Same Goal ignoring case and spacing. */
export const sameGoal = (a, b) => {
  const norm = (s) => String(s || '').replace(/\s+/g, ' ').trim().toLowerCase();
  return norm(a) === norm(b);
};

/**
 * @param {TasksState} state
 * @param {string} id
 * @returns {FoundTask|null}
 */
export function findTask(state, id) {
  if (state.unpinned && state.unpinned.id === id) return { ...state.unpinned, branch: null };
  for (const [branch, e] of Object.entries(state.branches)) if (e.id === id) return { ...e, branch };
  return null;
}

/**
 * What saving a plan means here.
 * @param {TasksState} state
 * @param {string|null} branch
 * @param {string} goal
 * @param {string} [explicitId]
 * @returns {{action: 'revise', id: string} | {action: 'refuse', task: FoundTask|null} | {action: 'birth'}}
 */
export function planDecision(state, branch, goal, explicitId) {
  if (explicitId) {
    const t = findTask(state, explicitId);
    return t ? { action: 'revise', id: t.id } : { action: 'refuse', task: null };
  }
  const pinned = state.branches[branchKey(branch)];
  if (pinned) {
    return !goal || sameGoal(pinned.goal, goal)
      ? { action: 'revise', id: pinned.id }
      : { action: 'refuse', task: { ...pinned, branch: branchKey(branch) } };
  }
  if (state.unpinned) {
    return sameGoal(state.unpinned.goal, goal)
      ? { action: 'revise', id: state.unpinned.id }
      : { action: 'refuse', task: { ...state.unpinned, branch: null } };
  }
  return { action: 'birth' };
}

const day = (ms) => (ms ? new Date(ms).toISOString().slice(0, 10) : '?');

/**
 * Refusal text: names the task in progress and the ways out.
 * @param {FoundTask|null} task
 * @param {string} cmd  mem command prefix, e.g. `node .../mem.mjs`
 */
export function refuseMessage(task, cmd) {
  if (!task) return 'nxy: no task with that id. See the live ones with `handoff show`.';
  return [
    `nxy: there is a task in progress: "${task.goal || task.id}" (id ${task.id}, created ${day(task.created)}). This plan was not saved, so it does not replace it.`,
    'Options:',
    `- Close it: \`${cmd} handoff done --task ${task.id}\` (archives it) or \`${cmd} handoff delete --task ${task.id}\` (discards it).`,
    '- Work in parallel: create a worktree from the panel (Features → Nuevo), which carries this task.',
    `- If this plan is a revision of that task: \`${cmd} handoff plan --task ${task.id}\`.`,
  ].join('\n');
}

/**
 * @param {TasksState} state
 * @param {{id: string, goal: string, now: number}} t
 * @returns {TasksState}
 */
export function withUnpinned(state, t) {
  return { ...state, unpinned: { id: t.id, goal: capGoal(t.goal), created: t.now } };
}

/**
 * Pins the unpinned task to a branch, unless that branch already has a task.
 * @param {TasksState} state
 * @param {string|null} branch
 * @param {number} now
 * @returns {TasksState}
 */
export function withPinned(state, branch, now) {
  const key = branchKey(branch);
  if (!state.unpinned || state.branches[key]) return state;
  return { unpinned: null, branches: { ...state.branches, [key]: { ...state.unpinned, pinned: now } } };
}

/** @returns {TasksState} */
export function withGoal(state, id, goal) {
  const g = capGoal(goal);
  const unpinned = state.unpinned && state.unpinned.id === id ? { ...state.unpinned, goal: g } : state.unpinned;
  const branches = Object.fromEntries(Object.entries(state.branches).map(([b, e]) => [b, e.id === id ? { ...e, goal: g } : e]));
  return { unpinned, branches };
}

/** @returns {TasksState} */
export function withoutTask(state, id) {
  const unpinned = state.unpinned && state.unpinned.id === id ? null : state.unpinned;
  const branches = Object.fromEntries(Object.entries(state.branches).filter(([, e]) => e.id !== id));
  return { unpinned, branches };
}

/**
 * Pinned tasks not yet offered whose branch is gone (no real branch to check for `(no branch)` or a detached sha).
 * @param {TasksState} state
 * @param {(branch: string) => boolean} exists
 * @returns {FoundTask[]}
 */
export function staleTasks(state, exists) {
  return Object.entries(state.branches)
    .filter(([b, e]) => !e.offered && b !== NO_BRANCH && !/^[0-9a-f]{7}$/.test(b) && !exists(b))
    .map(([branch, e]) => ({ ...e, branch }));
}

/** @returns {TasksState} */
export function withOffered(state, ids) {
  const set = new Set(ids);
  return { ...state, branches: Object.fromEntries(Object.entries(state.branches).map(([b, e]) => [b, set.has(e.id) ? { ...e, offered: true } : e])) };
}

/**
 * Context for the main thread: ask the user whether to close or delete each task whose branch is gone.
 * @param {FoundTask[]} entries
 * @param {string} cmd
 */
export function staleTaskHint(entries, cmd) {
  const lines = entries.map((e) => `- "${e.goal || e.id}" (branch \`${e.branch}\`): \`${cmd} handoff done --task ${e.id}\` to archive it, or \`${cmd} handoff delete --task ${e.id}\` to discard it`);
  return [
    `nxy: ${entries.length === 1 ? 'a task is' : 'tasks are'} still open but the branch it was edited on no longer exists. Ask the user, once, which to do:`,
    ...lines,
  ].join('\n');
}
