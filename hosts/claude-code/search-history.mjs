// @ts-check
/**
 * Broad searches of the main thread (whole-file Read, Grep, Glob, Bash/PowerShell `rg`/`grep -r`/`find`)
 * made while the context was at or above the gate threshold, counted on demand from the session
 * transcripts. No per-call hook: it works over past sessions and costs nothing while working.
 * Kept apart from search-observe.mjs so the Grep/Glob hook never loads this or transcripts.mjs.
 */
import { readFileSync, statSync } from 'node:fs';
import { basename, resolve } from 'node:path';
import { toNativePath } from '../../core/paths.mjs';
import { classifySearch } from '../../core/search-scope.mjs';
import { listSessions } from './transcripts.mjs';

const TOOLS = new Set(['Grep', 'Glob', 'Read', 'Bash', 'PowerShell']);
const EXT = /\.[A-Za-z0-9]{1,8}$/;

/**
 * @param {string} cwd
 * @returns {(p: string) => boolean}
 */
function fileCheck(cwd) {
  return (p) => {
    try {
      return statSync(resolve(toNativePath(cwd), toNativePath(p))).isFile();
    } catch {
      return EXT.test(basename(String(p).replace(/[\\/]+$/, '')));
    }
  };
}

/**
 * @param {string} path main transcript (jsonl)
 * @param {{threshold: number, sinceMs?: number}} opts
 * @returns {{ts: number, tool: string, kind: string|null, contextTokens: number}[]}
 */
export function broadSearchesInTranscript(path, { threshold, sinceMs = 0 }) {
  if (!(threshold > 0)) return [];
  let text;
  try {
    text = readFileSync(path, 'utf8');
  } catch {
    return [];
  }
  const rows = [];
  const seen = new Set();
  /** @type {Map<string, number>} */ const lastCtx = new Map();
  for (const line of text.split('\n')) {
    if (!line.includes('"tool_use"')) continue;
    let e;
    try {
      e = JSON.parse(line);
    } catch {
      continue;
    }
    if (!e || e.type !== 'assistant' || e.isSidechain) continue;
    const ts = Date.parse(e.timestamp);
    if (Number.isFinite(ts) && ts < sinceMs) continue;
    const u = e.message?.usage;
    let ctx = u ? (u.input_tokens || 0) + (u.cache_read_input_tokens || 0) + (u.cache_creation_input_tokens || 0) : 0;
    const rid = typeof e.requestId === 'string' ? e.requestId : null;
    if (ctx > 0 && rid) lastCtx.set(rid, ctx);
    else if (rid && lastCtx.has(rid)) ctx = lastCtx.get(rid) ?? 0;
    if (!(ctx >= threshold)) continue;
    const content = e.message?.content;
    if (!Array.isArray(content)) continue;
    const isFile = fileCheck(typeof e.cwd === 'string' ? e.cwd : '.');
    for (const b of content) {
      if (!b || b.type !== 'tool_use' || !TOOLS.has(b.name)) continue;
      if (b.id) {
        if (seen.has(b.id)) continue;
        seen.add(b.id);
      }
      const cls = classifySearch(b.name, b.input, { isFile });
      if (cls.broad) rows.push({ ts: Number.isFinite(ts) ? ts : 0, tool: b.name, kind: cls.kind, contextTokens: ctx });
    }
  }
  return rows;
}

/**
 * @param {{tool: string, contextTokens: number}[]} rows
 * @param {number} threshold
 */
export function summarizeBroadSearches(rows, threshold) {
  if (!(threshold > 0)) return { count: 0, byKind: {}, contextTokens: 0, threshold: null };
  /** @type {Record<string, number>} */ const byKind = {};
  let contextTokens = 0;
  for (const r of rows) {
    const k = r.tool === 'PowerShell' ? 'Bash' : r.tool;
    byKind[k] = (byKind[k] || 0) + 1;
    contextTokens += r.contextTokens || 0;
  }
  return { count: rows.length, byKind, contextTokens, threshold };
}

/**
 * Broad searches of every main session of the project since `sinceMs`.
 * @param {string} cwd
 * @param {{sinceMs?: number, projectsDir?: string|null, threshold?: number}} [opts]
 */
export function projectBroadSearches(cwd, { sinceMs = 0, projectsDir, threshold = 0 } = {}) {
  if (!(threshold > 0)) return summarizeBroadSearches([], 0);
  const rows = [];
  for (const s of listSessions({ cwd, projectsDir, since: sinceMs })) rows.push(...broadSearchesInTranscript(s.path, { threshold, sinceMs }));
  return summarizeBroadSearches(rows, threshold);
}
