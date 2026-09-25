// @ts-check
/**
 * The librarian: the one place where a model decides something about memory.
 *
 * Two jobs, both bounded:
 *  1. **On save** — relate the new memory to a handful of candidates the *code* preselects, and
 *     store the verdicts as edges (`related` / `supersedes` / `conflicts_with`). The model judges
 *     once, when writing; every later read is SQL and costs nothing. The verdict is exported with
 *     the memory, so a teammate who imports does not pay for it again.
 *  2. **On demand** — when the main thread starts a task and the code pointed at nothing, search
 *     the index by meaning ("other words, same concept": the one thing lexical search and the
 *     graph cannot do).
 *
 * It is never run per prompt: a model call on every message costs more than it saves.
 *
 * Provider: the role table (`roles.librarian`) decides who runs it. `claude` runs it as a Claude
 * Code subagent. Any other provider is the seam for an external model (an OpenAI-compatible API,
 * a local one) called by the CLI itself — the Claude session would then only see the resulting
 * ids. Not implemented until there is a real second provider (roadmap tema 6), but the route is
 * decided here so adding it touches this file and an adapter, nothing else.
 */
import { neighbors } from './graph.mjs';
import { getMemory, searchMemories } from './store.mjs';

/** How many candidates the librarian judges per save. Enough to catch a duplicate, cheap to read. */
export const MAX_CANDIDATES = 5;

/**
 * Candidates for relating a just-saved memory: lexical matches on its own title + keywords + body,
 * plus its graph neighbours (same files, explicit links), minus itself and anything already judged.
 * @param {import('node:sqlite').DatabaseSync} db
 * @param {import('./store.mjs').Memory} mem
 * @param {{project: string|null}} o
 * @returns {import('./store.mjs').Memory[]}
 */
export function linkCandidates(db, mem, o) {
  const judged = new Set(db.prepare(`
    SELECT CASE WHEN src = ?1 THEN dst ELSE src END AS other FROM edges
    WHERE (src = ?1 OR dst = ?1) AND kind IN ('related','supersedes','conflicts_with')
  `).all(mem.id).map((r) => String(r.other)));
  /** @type {Map<string, import('./store.mjs').Memory>} */
  const out = new Map();
  const query = `${mem.title} ${mem.keywords} ${mem.body}`;
  for (const m of searchMemories(db, query, { project: o.project, areas: mem.area ? [mem.area] : [], limit: MAX_CANDIDATES * 2 })) {
    if (m.id === mem.id || judged.has(m.id) || m.type === 'handoff') continue;
    out.set(m.id, m);
    if (out.size >= MAX_CANDIDATES) break;
  }
  if (out.size < MAX_CANDIDATES) {
    for (const n of neighbors(db, mem.id, { limit: MAX_CANDIDATES })) {
      if (out.size >= MAX_CANDIDATES) break;
      if (judged.has(n.id) || out.has(n.id)) continue;
      const m = getMemory(db, n.id);
      if (m && m.type !== 'handoff') out.set(n.id, m);
    }
  }
  return [...out.values()];
}

/**
 * Who runs the librarian, from the role table.
 * @param {import('../config.mjs').NxyConfig} cfg
 * @returns {{kind: 'subagent', model: string} | {kind: 'off'} | {kind: 'unsupported', provider: string}}
 */
export function librarianRoute(cfg) {
  const role = cfg.roles?.librarian;
  if (!role) return { kind: 'off' };
  const provider = role.provider || 'claude';
  if (provider === 'claude') return { kind: 'subagent', model: role.model };
  return { kind: 'unsupported', provider };
}

/**
 * The request the main thread hands the librarian after a save. Short on purpose: the librarian
 * fetches the bodies itself (it pays for them, the main thread does not).
 * @param {string} id
 * @param {string[]} candidateIds
 */
export function linkRequest(id, candidateIds) {
  return `Relate memory ${id} to these candidates: ${candidateIds.join(' ')}`;
}
