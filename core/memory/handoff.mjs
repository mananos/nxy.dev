// @ts-check
/**
 * The live handoff: the ~20 lines a fresh session needs to pick up a task without rebuilding it.
 *
 * One per project and branch. The branch is the task: it is the one identity that already exists,
 * survives `/clear`, and needs nobody to declare anything. Saving again replaces it (a handoff is
 * a snapshot, not a log); `done` archives it — still searchable, no longer pointed at.
 *
 * It is a memory of type `handoff` in the same store, private by default: a handoff is working
 * state, not a team convention. Sharing one (passing a task to a teammate) is a deliberate flag.
 *
 * Why it is *required* past the gate threshold, instead of only described in a prompt: gentle-ai
 * tried the prompt first ("mandatory delegation triggers") and the model read it and carried on.
 * What worked was a hook that refuses the action and names the exact command. Same here: past the
 * threshold, no write work starts — neither an edit nor an implementer dispatch — until a handoff
 * exists. Below it nothing is asked, because a cheap session is cheap to rebuild.
 *
 * Everything here is pure or takes the db as an argument; the host decides how to show it.
 */
import { createHash } from 'node:crypto';
import { getMemory, deleteMemory, saveMemory } from './store.mjs';
import { extractPlan } from '../plan.mjs';

/** What the model may declare. `inline` = the main thread edits; `implementer` = it delegates. */
export const ROUTES = ['inline', 'implementer'];

/** Past this, the handoff has stopped being a handoff and become a transcript. Warned, not refused. */
export const MAX_LINES = 40;

export const TEMPLATE = [
  'route: implementer        # inline | implementer — who writes the code for this task',
  '## Done',
  '- what is finished, with key details',
  '## Next',
  '- what remains, in the order to do it',
  '## Files',
  '- path/to/file — what it does or what changed',
  '## Decisions',
  '- what was decided and why, so the next session does not reopen it',
].join('\n');

/** Key used when the checkout has no branch (not a repo, or HEAD unreadable). */
const NO_BRANCH = '(no branch)';

/**
 * Deterministic id of the live handoff: readable slug of the branch plus a hash of project+branch,
 * so two repos with a `main` branch never collide.
 * @param {string} project
 * @param {string|null} branch
 */
export function handoffId(project, branch) {
  const b = branch || NO_BRANCH;
  const slug = b.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '').slice(0, 40) || 'branch';
  const hash = createHash('sha1').update(`${project}\n${b}`).digest('hex').slice(0, 8);
  return `handoff-${slug}-${hash}`;
}

/**
 * Checks the shape. Only two things are required, because they are the two a next session cannot
 * reconstruct: *who writes* (the route — makes non-delegation visible instead of silent) and *what
 * is next*. Everything else is encouraged by the template, not enforced.
 * @param {string} body
 * @returns {{ok: boolean, errors: string[], warnings: string[], route: string|null, lines: number}}
 */
export function validateHandoff(body) {
  const text = String(body || '').replace(/\r\n/g, '\n').trim();
  const lines = text ? text.split('\n').length : 0;
  const errors = [];
  const warnings = [];

  const m = /^route:\s*([A-Za-z-]+)/im.exec(text);
  const route = m ? m[1].toLowerCase() : null;
  if (!route) errors.push(`missing a "route:" line (${ROUTES.join(' | ')})`);
  else if (!ROUTES.includes(route)) errors.push(`route "${route}" is not one of: ${ROUTES.join(' | ')}`);

  const next = /^##\s*Next\s*$([\s\S]*?)(?=^##\s|(?![\s\S]))/im.exec(text);
  if (!next) errors.push('missing a "## Next" section');
  else if (!next[1].trim()) errors.push('"## Next" is empty — say what remains, even if it is "nothing, close the branch"');

  // The plan (0.4.0) has its own shape and its own checkpoint; the length warning is about the rest.
  const plan = extractPlan(text);
  const own = plan ? text.replace(plan, '').trim().split('\n').length : lines;
  if (own > MAX_LINES) warnings.push(`${own} lines (aim for ~20): a handoff is read at the start of every resumed session`);
  return { ok: errors.length === 0, errors, warnings, route, lines };
}

/**
 * The live handoff for this project and branch, or null.
 * @param {import('node:sqlite').DatabaseSync} db
 * @param {string} project
 * @param {string|null} branch
 */
export function liveHandoff(db, project, branch) {
  return getMemory(db, handoffId(project, branch));
}

/**
 * Saves (replaces) the live handoff. Callers validate first; this stores whatever it is given.
 * @param {import('node:sqlite').DatabaseSync} db
 * @param {{project: string, branch: string|null, body: string, share?: boolean}} h
 */
export function saveHandoff(db, h) {
  const branch = h.branch || NO_BRANCH;
  return saveMemory(db, {
    id: handoffId(h.project, h.branch),
    scope: 'project',
    project: h.project,
    type: 'handoff',
    title: `handoff: ${branch}`,
    body: String(h.body).replace(/\r\n/g, '\n').trim(),
    keywords: `handoff ${branch.replace(/[/_-]+/g, ' ')}`,
    private: !h.share,
  });
}

/**
 * Archives the live handoff: same content under a dated id, then the live one is gone. It stays
 * searchable (`mem search --type handoff`), which is how a PR comment weeks later finds its task.
 * @param {import('node:sqlite').DatabaseSync} db
 * @param {string} project
 * @param {string|null} branch
 * @param {number} [now]
 * @returns {string|null} the archived id, or null when there was no live handoff
 */
export function archiveHandoff(db, project, branch, now = Date.now()) {
  const live = liveHandoff(db, project, branch);
  if (!live) return null;
  const id = `${live.id}-done-${now.toString(36)}`;
  saveMemory(db, {
    ...live,
    id,
    title: `handoff (done): ${branch || NO_BRANCH} — ${new Date(now).toISOString().slice(0, 10)}`,
    created: live.created,
    updated: now,
  });
  deleteMemory(db, live.id);
  return id;
}

/** "3 min" / "2 h" / "4 d": precise enough to tell a fresh handoff from a stale one. */
export function age(ms) {
  const min = Math.max(0, Math.round(ms / 60_000));
  if (min < 60) return `${min} min`;
  const h = Math.round(min / 60);
  if (h < 48) return `${h} h`;
  return `${Math.round(h / 24)} d`;
}

/**
 * What a session sees at start, by memory mode:
 *  - `manual`: nothing. Memory is reached only when the user asks.
 *  - `assisted` (default): one line — that a handoff exists, how old, and the command to load it.
 *    ~40 tokens; the body costs only when someone actually resumes.
 *  - `proactive`: the whole handoff, every session on that branch, used or not.
 * @param {'manual'|'assisted'|'proactive'|string} mode
 * @param {import('./store.mjs').Memory|null} h
 * @param {string} showCmd   host-specific command that prints the handoff
 * @param {number} [now]
 * @returns {string|null}
 */
export function sessionStartContext(mode, h, showCmd, now = Date.now()) {
  if (!h || mode === 'manual') return null;
  const branch = h.title.replace(/^handoff:\s*/, '');
  const { route, lines } = validateHandoff(h.body);
  const head = `nxy: there is a handoff for branch \`${branch}\` from ${age(now - h.updated)} ago (${lines} lines, route: ${route || '?'}).`;
  if (mode === 'proactive') return `${head}\n\n${h.body}`;
  return `${head} If the user wants to resume that work, load it with: ${showCmd}`;
}

/**
 * @typedef {object} HandoffGateInput
 * @property {boolean} required        config switch
 * @property {boolean} isSubagent      subagents never write a handoff; the main thread owns the task
 * @property {number|null} contextTokens
 * @property {number} threshold        the gate's threshold: same line, same meaning ("this session is expensive")
 * @property {boolean} hasHandoff
 */

/**
 * Whether write work must wait for a handoff. Fails open on anything unknown, like the gate.
 * @param {HandoffGateInput} input
 * @returns {{block: boolean, reason: string}}
 */
export function decideHandoff(input) {
  if (!input.required) return { block: false, reason: 'disabled' };
  if (input.isSubagent) return { block: false, reason: 'subagent' };
  if (input.hasHandoff) return { block: false, reason: 'has-handoff' };
  if (typeof input.contextTokens !== 'number' || !Number.isFinite(input.contextTokens)) return { block: false, reason: 'context-unknown' };
  if (!Number.isFinite(input.threshold) || input.threshold <= 0) return { block: false, reason: 'no-threshold' };
  if (input.contextTokens < input.threshold) return { block: false, reason: 'under-threshold' };
  return { block: true, reason: 'no-handoff' };
}

/**
 * Mandatory context for a subagent (0.3.2): the task's handoff, put in front of the dispatch prompt
 * by the hook, so the subagent starts with the decisions and files without the main thread having
 * to remember to copy them — the failure mode is silent: nobody sees what was *not* passed.
 *
 * Inline, not by reference: a reference makes the subagent spend a call to fetch it, and that call
 * carries its whole context; ~20 lines paid once are cheaper.
 */
export const CONTEXT_OPEN = '<nxy-context source="nxy hook: attached by the runtime, not written by the main thread">';
export const CONTEXT_CLOSE = '</nxy-context>';

/**
 * A handoff longer than this is cut when injected: past it, it is a transcript, not a handoff.
 * The plan is never what gets cut — it is the one part the implementer is there to execute.
 */
export const MAX_INJECT_LINES = 60;

/**
 * @param {import('./store.mjs').Memory} h
 * @param {string} showCmd   command that prints the full handoff, for when it had to be cut
 * @param {number} [now]
 * @param {string} [sheet]   sections appended at the end: the plan's progress (0.4.5) and the conventions sheet (0.4.3), already capped
 */
export function subagentContextBlock(h, showCmd, now = Date.now(), sheet = '') {
  const branch = h.title.replace(/^handoff:\s*/, '');
  const plan = extractPlan(h.body);
  const rest = plan ? h.body.replace(plan, '').trim() : h.body;
  const lines = rest.split('\n');
  const cut = lines.length > MAX_INJECT_LINES
    ? [...lines.slice(0, MAX_INJECT_LINES), `… (${lines.length - MAX_INJECT_LINES} more lines: ${showCmd})`].join('\n')
    : rest;
  const body = plan ? `${cut}\n\n${plan}` : cut;
  return [
    CONTEXT_OPEN,
    `Task handoff for branch \`${branch}\` (updated ${age(now - h.updated)} ago). Its decisions are settled: do not reopen them.`,
    'Use its files as starting points; do not read them all. Your own request below is what you must do.',
    '',
    body,
    ...(sheet ? ['', sheet] : []),
    CONTEXT_CLOSE,
  ].join('\n');
}

/**
 * Whether the main thread may end its turn (0.4.5, the `Stop` hook): not while this session has
 * edited files after the task's handoff was last saved — `/clear` or closing would lose what the
 * handoff does not say. Only with a live handoff (a small task never has one), once per state, and
 * never when Claude Code says the stop hook already ran (`stop_hook_active`): the model may not be
 * able to comply, and a loop is worse than a stale handoff.
 *
 * The session's own record is the baseline: edits from earlier sessions are not this one's to save.
 * Plan progress (batches, review) is derived by nxy and never makes the handoff stale.
 * @param {{stopHookActive: boolean, savedTs: number|null, lastEditTs: number|null, remindedTs: number|null}} s
 * @returns {{block: boolean, reason: string}}
 */
export function decideStop(s) {
  if (s.stopHookActive) return { block: false, reason: 'already-reminded-this-stop' };
  if (s.savedTs == null) return { block: false, reason: 'no-live-handoff' };
  if (s.lastEditTs == null || s.lastEditTs <= s.savedTs) return { block: false, reason: 'handoff-current' };
  if (s.remindedTs === s.lastEditTs) return { block: false, reason: 'already-reminded-this-state' };
  return { block: true, reason: 'handoff-stale' };
}

/** @param {string|null} branch @param {string} showCmd @param {string} saveCmd */
export function staleHandoffMessage(branch, showCmd, saveCmd) {
  return [
    `nxy: this session edited files after the handoff of \`${branch || '(no branch)'}\` was last saved. Before ending the turn, bring it up to date so /clear or a new session picks up from here:`,
    `  1. \`${showCmd}\`  2. update Done / Next / Files / Decisions (the plan and its progress are kept by nxy: leave them)  3. save it with \`${saveCmd}\` (heredoc).`,
    'Then end the turn. This is asked once.',
  ].join('\n');
}

/**
 * Removes any block already in the prompt — a re-dispatch that copied the previous prompt, or a
 * model imitating the block. The runtime's copy is the only one that survives, and it is fresh.
 * @param {string} prompt
 */
export function stripContextBlock(prompt) {
  const open = CONTEXT_OPEN.slice(0, '<nxy-context'.length);
  let out = String(prompt || '');
  for (let i = out.indexOf(open); i !== -1; i = out.indexOf(open)) {
    const end = out.indexOf(CONTEXT_CLOSE, i);
    out = end === -1 ? out.slice(0, i) : out.slice(0, i) + out.slice(end + CONTEXT_CLOSE.length);
  }
  return out.trim();
}

/**
 * The refusal text. Like the gate's, it is the only token cost of the feature, so it carries the
 * exact command and the template: the model should be able to comply in one call, not three.
 * @param {{saveCmd: string, branch: string|null, then: string}} o
 */
export function handoffDenyMessage(o) {
  return [
    `nxy: this session is past the context threshold and branch \`${o.branch || NO_BRANCH}\` has no handoff yet. Save one before starting write work:`,
    '',
    `${o.saveCmd} <<'EOF'`,
    TEMPLATE,
    'EOF',
    '',
    '~20 lines. Keep it current: save again after each finished task (it replaces the previous one).',
    o.then,
  ].join('\n');
}
