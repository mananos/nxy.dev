// @ts-check
/**
 * The search a script can do without a model, so that a model never has to read files to find out
 * where something is.
 *
 * Three engines, cheapest first, each one covering what the previous cannot (roadmap tema 3):
 *   1. repo map  — structure. Built by a script, queried by a script. Always available.
 *   2. rg        — exact text. Zero tokens. Finds the string literals and wiring the index misses.
 *   3. codegraph — relationships. Optional, capped, and it declares its own fallback.
 *
 * The result of all three is what the scout reads. The scout is what the main thread reads. That
 * two-step is the whole point: the dense payload dies with the subagent, and roughly 500 tokens
 * of conclusion come back.
 */
import { buildRepoMap, queryRepoMap, repoMapInfo } from '../index/repomap.mjs';
import { findRg, rgSearch } from '../rg.mjs';
import { describeCodegraph, explore } from './codegraph.mjs';
import { STOPWORDS } from '../text.mjs';


/**
 * Pulls the terms worth searching out of a natural-language question.
 *
 * Identifiers win: `cancelBooking` or `user_id` in the question is almost always the answer, so
 * they are kept verbatim *and* split, since the index may hold either spelling.
 * @param {string} question
 */
export function queryTerms(question) {
  const raw = (question || '').split(/[^A-Za-z0-9_$./-]+/).filter(Boolean);
  /** @type {string[]} */
  const identifiers = [];
  /** @type {string[]} */
  const words = [];
  for (const t of raw) {
    const looksLikeId = /[A-Z]/.test(t.slice(1)) || t.includes('_') || t.includes('.') || t.includes('/');
    if (looksLikeId && t.length >= 3) {
      identifiers.push(t);
      for (const part of t.split(/[._/-]|(?=[A-Z])/)) {
        if (part.length >= 4 && !STOPWORDS.has(part.toLowerCase())) words.push(part.toLowerCase());
      }
    } else if (t.length >= 3 && !STOPWORDS.has(t.toLowerCase()) && !/^\d+$/.test(t)) {
      words.push(t.toLowerCase());
    }
  }
  const seen = new Set();
  const terms = [...identifiers, ...words].filter((t) => {
    const k = t.toLowerCase();
    if (seen.has(k)) return false;
    seen.add(k);
    return true;
  });
  return { identifiers, terms: terms.slice(0, 8) };
}

const MAX_OUTPUT_CHARS = 40_000; // ~10k tokens, inside a scout that dies. Never the main thread.

/**
 * Runs every available engine for a question and returns a compact report plus what degraded.
 *
 * Never throws and never returns nothing: if every engine fails, the report says so and the scout
 * falls back to reading a couple of files itself, which is still better than the main thread
 * reading forty.
 *
 * @param {string} cwd
 * @param {string} question
 * @param {{codegraph?: boolean, rebuild?: boolean, limit?: number, env?: NodeJS.ProcessEnv}} [opts]
 */
export function searchWithoutModel(cwd, question, opts = {}) {
  const env = opts.env || process.env;
  const started = Date.now();
  /** @type {string[]} */
  const degraded = [];
  const { terms, identifiers } = queryTerms(question);
  const out = [`# model-free search — "${question}"`, ''];

  if (!terms.length) {
    out.push('No searchable term could be pulled out of this question. Search it yourself with Grep/Glob.');
    return { text: out.join('\n'), degraded: ['no-terms'], terms, ms: Date.now() - started };
  }

  // 1. repo map ------------------------------------------------------------------------------
  let info = repoMapInfo(cwd);
  if (!info || opts.rebuild) {
    const built = buildRepoMap(cwd, { force: Boolean(opts.rebuild) });
    info = { builtAt: Date.now(), files: built.files, symbols: built.symbols, map: built.map, ageMs: 0 };
  }
  /** @type {Map<string, {path: string, line: number, kind: string, name: string, sig: string}>} */
  const symbols = new Map();
  for (const t of terms) {
    for (const row of queryRepoMap(cwd, t, { limit: opts.limit ?? 25 }).rows) {
      symbols.set(`${row.path}:${row.line}`, row);
    }
  }
  out.push(`## repo index (${info.files} files, ${info.symbols ?? '?'} symbols)`);
  if (symbols.size) {
    out.push('```');
    for (const r of [...symbols.values()].slice(0, 60)) out.push(`${r.path}:${r.line}\t${r.kind}\t${r.name}\t${r.sig}`);
    out.push('```');
  } else {
    out.push('_No symbols for these terms — the domain vocabulary probably differs from the code._');
    degraded.push('repomap:no-hits');
  }
  out.push('');

  // 2. rg ------------------------------------------------------------------------------------
  const rgBin = findRg(env);
  out.push('## exact text (rg)');
  if (!rgBin) {
    out.push('_ripgrep is not installed: this layer is skipped._');
    degraded.push('rg:missing');
  } else {
    let any = false;
    for (const t of [...identifiers, ...terms].slice(0, 5)) {
      const res = rgSearch(cwd, t, { bin: rgBin, maxRows: 12, env });
      if (!res.ok || !res.rows.length) continue;
      any = true;
      out.push(`\`${t}\`${res.truncated ? ' (capped)' : ''}:`);
      out.push('```');
      for (const r of res.rows) out.push(`${r.path}:${r.line}: ${r.text}`);
      out.push('```');
    }
    if (!any) {
      out.push('_None of the terms appear literally anywhere in the repo._');
      degraded.push('rg:no-hits');
    }
  }
  out.push('');

  // 3. codegraph -----------------------------------------------------------------------------
  out.push('## codegraph');
  if (opts.codegraph === false) {
    out.push('_Disabled by configuration._');
  } else {
    const res = explore(cwd, question, { env });
    if (res.ok) {
      out.push(`_${res.ms} ms${res.truncated ? ', payload capped' : ''}_`);
      out.push('```');
      out.push(res.text);
      out.push('```');
    } else {
      out.push(`_Unavailable (${res.reason}) — answered with the index and rg instead._`);
      degraded.push(`codegraph:${res.reason}`);
    }
  }

  let text = out.join('\n');
  if (text.length > MAX_OUTPUT_CHARS) {
    text = text.slice(0, MAX_OUTPUT_CHARS) + '\n\n_[output capped by nxy]_';
    degraded.push('output:truncated');
  }
  return { text, degraded, terms, ms: Date.now() - started };
}

/** Status block for `/nxy:locate --status`: which engines the scout would actually have. */
export function describeEngines(cwd, env = process.env) {
  const info = repoMapInfo(cwd);
  const age = info ? `${Math.round(info.ageMs / 1000)}s` : null;
  return [
    `repo map:     ${info ? `${info.files} files, ${info.symbols} symbols (built ${age} ago)` : 'not built — it builds itself on the first /nxy:locate'}`,
    `ripgrep (rg): ${findRg(env) ? 'available' : 'not found (the scout uses the index only)'}`,
    describeCodegraph(cwd, env).line,
  ].join('\n');
}
