// @ts-check
/**
 * Closed tasks that touched the same files: what the planner reads before drafting, so a task in
 * the same code starts from what the previous one decided. Pure: the db comes as an argument.
 *
 * Ranking is by how many of the asked files the task touched, then by recency. Fixed constants,
 * not config: nobody should have to tune how much history a plan reads.
 */
import { getMemory } from './store.mjs';
import { fileHitsByMemory } from './graph.mjs';
import { extractPlan, parseDecisions } from '../plan.mjs';

export const MAX_RELATED_TASKS = 3;
export const MAX_RELATED_LINES = 40;

/**
 * @param {import('node:sqlite').DatabaseSync} db
 * @param {{project: string, files: string[], limit?: number}} o  files repo-relative, forward slashes
 * @returns {{id: string, title: string, updated: number, overlap: number, matched: string[], decisions: {rule: string, why: string, scope: 'repo'|'task'}[]}[]}
 */
export function relatedTasks(db, { project, files, limit = MAX_RELATED_TASKS }) {
  const hits = fileHitsByMemory(db, files);
  const out = [];
  for (const [id, matched] of hits) {
    if (!id.includes('-done-')) continue;
    const mem = getMemory(db, id);
    if (!mem || mem.type !== 'handoff' || mem.project !== project) continue;
    const plan = extractPlan(mem.body);
    out.push({
      id, title: mem.title, updated: mem.updated, overlap: matched.length, matched,
      decisions: plan ? parseDecisions(plan) : [],
    });
  }
  out.sort((a, b) => b.overlap - a.overlap || b.updated - a.updated);
  return out.slice(0, limit);
}

/**
 * Text for the planner, at most MAX_RELATED_LINES lines: per task its title, id, matching files
 * and decisions; decisions beyond a task's share are cut with a pointer to the full memory.
 * @param {ReturnType<typeof relatedTasks>} list
 * @param {string} getCmd  command prefix that fetches a memory (`mem get`)
 */
export function formatRelated(list, getCmd) {
  if (!list.length) return '';
  const share = Math.max(2, Math.floor((MAX_RELATED_LINES - 3 * list.length) / list.length));
  const lines = [];
  for (const t of list) {
    lines.push(t.title, `id: ${t.id}`, `files: ${t.overlap} (${t.matched.join(', ')})`);
    const show = t.decisions.length > share ? share - 1 : t.decisions.length;
    for (const d of t.decisions.slice(0, show)) lines.push(`- [${d.scope}] ${d.rule}${d.why ? ` — ${d.why}` : ''}`);
    if (show < t.decisions.length) lines.push(`… ${t.decisions.length - show} more: ${getCmd} ${t.id}`);
  }
  return lines.slice(0, MAX_RELATED_LINES).join('\n');
}
