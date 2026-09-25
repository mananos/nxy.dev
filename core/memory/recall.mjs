// @ts-check
/**
 * Recall by prompt: which memories deserve a pointer in front of this message.
 *
 * Code only, zero tokens to decide. What reaches the context is a pointer (`id — title`, ~30
 * tokens), not a body: in `assisted` mode a wrong call costs a pointer, so the rule can afford to
 * be simple. The body arrives only if the model asks for it (`mem get`).
 *
 * Relevance is **coverage, not score**. Raw BM25 depends on the corpus — the same -2.0 means one
 * thing with 10 notes and another with 500 — so any fixed floor on it is a number somebody would
 * have to tune, and nxy does not ask users to tune numbers. A memory earns a pointer when:
 *
 *  - it is attached to a file the prompt names or the session has been editing (the strongest
 *    signal: same code, whatever the words), or
 *  - the prompt contains one of its keywords (an alias someone wrote on purpose), or
 *  - it shares at least two distinct content words with the prompt, and at least one of them is in
 *    its title or keywords — its *subject*. For a long paste (a log, a stack trace) both have to be
 *    subject words: 200 words share two with almost any note in passing.
 *
 * BM25 only orders what passed. Whether the rule is right is measured, not assumed: every pointer
 * and every `mem get` goes to a ledger, and `mem status` shows how many pointers were followed.
 */
import { memoriesForFiles, neighbors, supersededIds } from './graph.mjs';
import { getMemory, searchMemories } from './store.mjs';
import { contentTerms } from '../text.mjs';

/** Pointers per prompt. Three lines is ~100 tokens at worst. */
export const MAX_POINTERS = 3;

/** Related memories shown next to each pointer. */
export const MAX_RELATED = 2;

/**
 * Past this many content words the prompt is a paste (log, stack trace, diff), not a question, and
 * matching on body words in passing turns into noise. Not a user setting: it separates two kinds
 * of input, it does not tune relevance.
 */
export const LONG_PROMPT_TERMS = 30;

/** Lower-case, accents folded: "decisión" and "decision" are the same word for recall. */
export const fold = (s) => String(s || '').toLowerCase().normalize('NFD').replace(/[̀-ͯ]/g, '');

/** @param {string} text */
const termSet = (text) => new Set(contentTerms(fold(text)));

/**
 * Why a memory is relevant to a prompt, or null when it is not.
 * @param {Set<string>} promptTerms  folded content terms of the prompt
 * @param {{title: string, body: string, keywords: string}} mem
 * @returns {string|null}
 */
export function coverage(promptTerms, mem) {
  const subject = termSet(`${mem.title} ${mem.keywords}`);
  for (const k of termSet(mem.keywords)) if (promptTerms.has(k)) return `keyword "${k}"`;
  if (promptTerms.size > LONG_PROMPT_TERMS) {
    // A pasted log or stack trace shares a few words with almost anything; only the subject counts.
    const onSubject = [...promptTerms].filter((t) => subject.has(t));
    return onSubject.length >= 2 ? `words: ${onSubject.slice(0, 4).join(', ')}` : null;
  }
  const all = new Set([...subject, ...termSet(mem.body)]);
  const shared = [...promptTerms].filter((t) => all.has(t));
  if (shared.length >= 2 && shared.some((t) => subject.has(t))) return `words: ${shared.slice(0, 4).join(', ')}`;
  return null;
}

/**
 * @typedef {object} Pointer
 * @property {import('./store.mjs').Memory} mem
 * @property {string} why
 * @property {{id: string, title: string, kind: string}[]} related
 */

/**
 * @param {import('node:sqlite').DatabaseSync} db
 * @param {string} prompt
 * @param {{project: string|null, areas?: string[], files?: string[], exclude?: Iterable<string>, limit?: number}} o
 * @returns {Pointer[]}
 */
export function recall(db, prompt, o) {
  const exclude = new Set(o.exclude || []);
  const superseded = supersededIds(db);
  const skip = (m) => !m || exclude.has(m.id) || superseded.has(m.id) || (m.type === 'handoff');
  const visible = (m) => m.scope === 'global' || m.project === o.project;
  const limit = o.limit ?? MAX_POINTERS;
  /** @type {Pointer[]} */
  const out = [];
  const taken = new Set();

  // 1. Files: named in the prompt, or edited in this session.
  const named = (o.files || []).concat(fileMentions(prompt));
  for (const [id, file] of memoriesForFiles(db, [...new Set(named)])) {
    const m = getMemory(db, id);
    if (!m || skip(m) || !visible(m) || taken.has(id)) continue;
    out.push({ mem: m, why: `file ${file}`, related: [] });
    taken.add(id);
  }

  // 2. Words, ordered by BM25, admitted by coverage.
  const terms = termSet(prompt);
  if (terms.size) {
    for (const m of searchMemories(db, prompt, { project: o.project, areas: o.areas || [], limit: 20 })) {
      if (out.length >= limit * 2) break;
      if (skip(m) || taken.has(m.id)) continue;
      const why = coverage(terms, m);
      if (!why) continue;
      out.push({ mem: m, why, related: [] });
      taken.add(m.id);
    }
  }

  const top = out.slice(0, limit);
  // 3. One hop of the graph for each pointer, never repeating a pointer or an excluded id.
  for (const p of top) {
    for (const n of neighbors(db, p.mem.id, { limit: 6 })) {
      if (p.related.length >= MAX_RELATED) break;
      if (taken.has(n.id) || exclude.has(n.id) || superseded.has(n.id)) continue;
      const m = getMemory(db, n.id);
      if (!m || !visible(m) || m.type === 'handoff') continue;
      p.related.push({ id: m.id, title: m.title, kind: n.kind });
    }
  }
  return top;
}

/** Things in a prompt that look like repo paths (`core/gate.mjs`, `src/a.ts:12`). */
export function fileMentions(prompt) {
  const re = /(?:^|[\s`'"(\[])((?:\.\/)?[\w@.-]+(?:\/[\w@.-]+)+\.[A-Za-z0-9]{1,8})(?::\d+)?/g;
  return [...new Set([...String(prompt || '').matchAll(re)].map((m) => m[1].replace(/^\.\//, '')))];
}

/**
 * The text that goes in front of the prompt.
 * @param {Pointer[]} pointers
 * @param {string} getCmd  host command that prints a memory by id
 * @param {'assisted'|'proactive'|string} mode
 */
export function formatPointers(pointers, getCmd, mode = 'assisted') {
  if (!pointers.length) return '';
  const lines = [`nxy memory — possibly relevant to this message (load one with: ${getCmd} <id>):`];
  for (const p of pointers) {
    const rel = p.related.length
      ? ` · ${p.related.map((r) => (r.kind === 'conflicts_with' ? `CONFLICTS with ${r.id}` : r.id)).join(', ')}`
      : '';
    lines.push(`- ${p.mem.id} — ${p.mem.title} (${p.why})${rel}`);
    if (mode === 'proactive') lines.push(...p.mem.body.split('\n').slice(0, 30).map((l) => `    ${l}`));
  }
  return lines.join('\n');
}
