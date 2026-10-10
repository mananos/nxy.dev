// @ts-check
/**
 * Pure helpers of the panel's Memoria tab and the scout/librarian launch block: no I/O, no `node:*`, no imports.
 * Parse what the agents answer (`path:line` hits, `- <id> — why` lines) and shape rows/lines for the blocks.
 */

/**
 * Clip a text for the pane: at most `maxLines` lines, long lines cut at 160 columns (panel.mjs re-exports it).
 * @param {string} text @param {number} [maxLines]
 */
export function clipOutput(text, maxLines = 30) {
  const lines = String(text ?? '').replace(/\s+$/, '').split(/\r?\n/).map((l) => (l.length > 160 ? `${l.slice(0, 159)}…` : l));
  if (lines.length <= maxLines) return lines;
  return [...lines.slice(0, maxLines), `… (${lines.length - maxLines} more lines)`];
}

// A path (optionally with a drive letter) with an extension, then `:line` (or `:line-line`).
const HIT_RE = /(?:^|[\s`(\[*])((?:[A-Za-z]:[\\/])?[^\s`:()[\]'"*]+\.[A-Za-z0-9]+):(\d+)(?:-\d+)?(?=$|[\s`)\]:,;.*])/g;

/**
 * Every `path:line` in a scout answer, with the rest of its line as the note. Max 10, de-duplicated.
 * @param {string} text @returns {{path: string, line: number, note: string}[]}
 */
export function parseHits(text) {
  /** @type {{path: string, line: number, note: string}[]} */
  const out = [];
  const seen = new Set();
  for (const raw of String(text ?? '').split(/\r?\n/)) {
    for (const m of raw.matchAll(HIT_RE)) {
      const key = `${m[1]}:${m[2]}`;
      if (seen.has(key)) continue;
      seen.add(key);
      const after = raw.slice((m.index ?? 0) + m[0].length).replace(/^[\s`*:,;—–\-()]+/, '').replace(/[\s`*)]+$/, '');
      out.push({ path: m[1], line: Number(m[2]), note: after.length > 120 ? `${after.slice(0, 119)}…` : after });
      if (out.length >= 10) return out;
    }
  }
  return out;
}

/**
 * The `- <id> — why` lines of a librarian answer (`none — …` gives nothing). Max 10.
 * @param {string} text @returns {{id: string, why: string}[]}
 */
export function parseMemIds(text) {
  /** @type {{id: string, why: string}[]} */
  const out = [];
  for (const raw of String(text ?? '').split(/\r?\n/)) {
    const m = /^\s*[-*]\s+`?([^\s`]+)`?\s+(?:[—–]|--|-)\s+(.*)$/.exec(raw) ?? /^\s*[-*]\s+`?([^\s`]+)`?\s*$/.exec(raw);
    if (!m) continue;
    out.push({ id: m[1], why: (m[2] ?? '').trim() });
    if (out.length >= 10) break;
  }
  return out;
}

/**
 * Rows of a memory search/list result; each is a button (`press`).
 * @param {any[] | null | undefined} results
 * @returns {{id: string, press: string, text: string, sub: string}[]}
 */
export function memResultRows(results) {
  return (Array.isArray(results) ? results : []).filter((r) => r && typeof r.id === 'string').map((r) => ({
    id: r.id, press: `mem:${r.id}`, text: String(r.title || r.id),
    sub: [r.type, r.area, r.scope].filter((x) => typeof x === 'string' && x).join(' · '),
  }));
}

/**
 * The lines of an opened memory: title, meta, keywords, edges, then the body clipped.
 * @param {any} memory @param {{kind: string, dir: string, other: string}[] | null | undefined} edges
 * @param {number} [maxBody]
 * @returns {string[]}
 */
export function memDetailLines(memory, edges, maxBody = 30) {
  if (!memory) return [];
  const lines = [String(memory.title || memory.id)];
  const meta = [memory.type, memory.area, memory.scope, memory.private ? 'privada' : '', memory.updated ? String(memory.updated) : '']
    .filter((x) => typeof x === 'string' && x);
  if (meta.length) lines.push(meta.join(' · '));
  if (Array.isArray(memory.keywords) && memory.keywords.length) lines.push(`palabras: ${memory.keywords.join(', ')}`);
  for (const e of Array.isArray(edges) ? edges : []) lines.push(`${e.dir === 'in' ? '←' : '→'} ${e.kind} ${e.other}`);
  lines.push('');
  lines.push(...clipOutput(memory.body ?? '', maxBody));
  return lines;
}

/**
 * Rows of locate/scout hits: `path:line  kind name`, the path never truncated.
 * @param {{path: string, line: number, kind?: string, name?: string, note?: string}[] | null | undefined} hits
 * @returns {{loc: string, rest: string, text: string}[]}
 */
export function hitRows(hits) {
  return (Array.isArray(hits) ? hits : []).filter((h) => h && typeof h.path === 'string').map((h) => {
    const loc = `${h.path}:${h.line}`;
    const rest = [h.kind, h.name || h.note].filter(Boolean).join(' ');
    return { loc, rest, text: rest ? `${loc}  ${rest}` : loc };
  });
}
