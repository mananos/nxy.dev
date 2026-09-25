// @ts-check
/**
 * The memory graph: edges between memories, and from memories to the files they are about.
 *
 * Plain SQLite (`edges` table in store.mjs), no graph database and no server: what the recall hook
 * needs is "memories touching these files" and "neighbours of these memories", two indexed
 * lookups. Walking it never costs a token — only what ends up in a context does.
 *
 * Where edges come from, and whether they travel with an export:
 *
 * | kind            | made by                                   | exported                      |
 * | --------------- | ----------------------------------------- | ----------------------------- |
 * | file            | code: paths in the body that exist         | no: re-derived from the body  |
 * | link            | code: `[[id]]` in the body                 | no: re-derived from the body  |
 * | supersedes      | the user (`save --supersedes`) or librarian | yes                           |
 * | related         | librarian, when a memory is saved          | yes                           |
 * | conflicts_with  | librarian, when a memory is saved          | yes                           |
 * | co_use          | code: two memories loaded close together   | no: telemetry of *your* sessions |
 *
 * The rule behind the split: the model decides once, at write time, and the decision is stored as
 * an edge — so reading stays code, and a teammate who imports does not pay for the judgement again.
 */
import { existsSync, statSync } from 'node:fs';
import { join } from 'node:path';

export const EDGE_KINDS = ['file', 'link', 'supersedes', 'related', 'conflicts_with', 'co_use'];

/** Kinds that are a judgement about the memory (not derivable from its body) and so travel. */
export const SHARED_KINDS = ['supersedes', 'related', 'conflicts_with'];

/** What the librarian (or the user) may assert between two memories. */
export const VERDICTS = ['related', 'supersedes', 'conflicts_with'];

const FILE_PREFIX = 'file:';

/**
 * Repo-relative paths mentioned in a text that exist in this checkout. Existence is the filter:
 * "a/b" in prose is not a file, `core/gate.mjs` is. Checked against *this* checkout, so an import
 * re-derives them for whoever imports.
 * @param {string} text
 * @param {string|null} root  repo root; without one nothing can be verified, so nothing is linked
 * @returns {string[]}
 */
export function extractFiles(text, root) {
  if (!root) return [];
  const out = new Set();
  const re = /(?:^|[\s`'"(\[])((?:\.{0,2}\/)?[\w@.-]+(?:\/[\w@.-]+)*\.[A-Za-z0-9]{1,8})(?::\d+)?(?=$|[\s`'"),\].:;])/gm;
  for (const m of String(text || '').matchAll(re)) {
    const rel = m[1].replace(/^\.\//, '').replace(/\\/g, '/');
    if (rel.startsWith('../') || rel.startsWith('/')) continue;
    try {
      const p = join(root, rel);
      if (existsSync(p) && statSync(p).isFile()) out.add(rel);
    } catch {
      /* not a path */
    }
  }
  return [...out];
}

/** `[[id]]` references in a text. */
export function extractLinks(text) {
  return [...new Set([...String(text || '').matchAll(/\[\[([\w.-]+)\]\]/g)].map((m) => m[1]))];
}

/** Normalises a file reference the way edges store it (repo-relative, forward slashes). */
export function fileNode(rel) {
  return FILE_PREFIX + String(rel).replace(/\\/g, '/').replace(/^\.\//, '');
}

/**
 * @param {import('node:sqlite').DatabaseSync} db
 * @param {string} src
 * @param {string} dst
 * @param {string} kind
 * @param {{by?: string, weight?: number}} [o]
 */
export function addEdge(db, src, dst, kind, o = {}) {
  if (!EDGE_KINDS.includes(kind) || !src || !dst || src === dst) return false;
  db.prepare(`
    INSERT INTO edges (src, dst, kind, weight, by, created) VALUES (?, ?, ?, ?, ?, ?)
    ON CONFLICT(src, dst, kind) DO UPDATE SET weight = excluded.weight, by = excluded.by
  `).run(src, dst, kind, o.weight ?? 1, o.by || 'code', Date.now());
  return true;
}

/** @param {import('node:sqlite').DatabaseSync} db */
export function removeEdge(db, src, dst, kind) {
  return db.prepare('DELETE FROM edges WHERE src = ? AND dst = ? AND kind = ?').run(src, dst, kind).changes > 0;
}

/**
 * Re-derives a memory's code edges (files, links) from its body. Called on every save and import,
 * so editing the body is the only thing needed to change them.
 * @param {import('node:sqlite').DatabaseSync} db
 * @param {{id: string, body: string}} mem
 * @param {string|null} root
 */
export function deriveEdges(db, mem, root) {
  db.prepare("DELETE FROM edges WHERE src = ? AND kind IN ('file','link') AND by = 'code'").run(mem.id);
  const files = extractFiles(mem.body, root);
  for (const f of files) addEdge(db, mem.id, fileNode(f), 'file');
  const links = extractLinks(mem.body);
  for (const l of links) addEdge(db, mem.id, l, 'link');
  return { files, links };
}

/**
 * Strengthens the co-use edge between two memories (stored once, lower id first).
 * @param {import('node:sqlite').DatabaseSync} db
 */
export function bumpCoUse(db, a, b) {
  if (!a || !b || a === b) return;
  const [src, dst] = a < b ? [a, b] : [b, a];
  db.prepare(`
    INSERT INTO edges (src, dst, kind, weight, by, created) VALUES (?, ?, 'co_use', 1, 'code', ?)
    ON CONFLICT(src, dst, kind) DO UPDATE SET weight = weight + 1
  `).run(src, dst, Date.now());
}

/**
 * Every edge touching a memory, both directions.
 * @param {import('node:sqlite').DatabaseSync} db
 * @param {string} id
 * @returns {{src: string, dst: string, kind: string, weight: number, by: string}[]}
 */
export function edgesOf(db, id) {
  return db.prepare('SELECT src, dst, kind, weight, by FROM edges WHERE src = ? OR dst = ? ORDER BY kind, dst').all(id, id)
    .map((r) => ({ src: String(r.src), dst: String(r.dst), kind: String(r.kind), weight: Number(r.weight), by: String(r.by) }));
}

/**
 * Ids that another memory supersedes: they stay searchable but are never pointed at.
 * @param {import('node:sqlite').DatabaseSync} db
 * @returns {Set<string>}
 */
export function supersededIds(db) {
  return new Set(db.prepare("SELECT dst FROM edges WHERE kind = 'supersedes'").all().map((r) => String(r.dst)));
}

/**
 * Memories attached to any of these files.
 * @param {import('node:sqlite').DatabaseSync} db
 * @param {string[]} files  repo-relative
 * @returns {Map<string, string>}  memory id → the file that matched
 */
export function memoriesForFiles(db, files) {
  /** @type {Map<string, string>} */
  const out = new Map();
  if (!files.length) return out;
  const nodes = files.map(fileNode);
  const rows = db.prepare(`SELECT src, dst FROM edges WHERE kind = 'file' AND dst IN (${nodes.map(() => '?').join(',')})`).all(...nodes);
  for (const r of rows) if (!out.has(String(r.src))) out.set(String(r.src), String(r.dst).slice(FILE_PREFIX.length));
  return out;
}

/**
 * One hop of neighbours of a memory, strongest first: explicit and judged edges, then two
 * memories sharing a file, then co-use seen at least twice (once is coincidence).
 * @param {import('node:sqlite').DatabaseSync} db
 * @param {string} id
 * @param {{limit?: number}} [o]
 * @returns {{id: string, kind: string}[]}
 */
export function neighbors(db, id, o = {}) {
  const rows = db.prepare(`
    WITH n(other, kind, rank) AS (
      SELECT CASE WHEN src = ?1 THEN dst ELSE src END, kind,
             CASE kind WHEN 'conflicts_with' THEN 0 WHEN 'supersedes' THEN 1 WHEN 'link' THEN 2 WHEN 'related' THEN 2 ELSE 4 END
      FROM edges
      WHERE (src = ?1 OR dst = ?1) AND kind IN ('link','related','supersedes','conflicts_with')
      UNION ALL
      SELECT b.src, 'file', 3
      FROM edges a JOIN edges b ON a.dst = b.dst AND b.kind = 'file' AND b.src <> ?1
      WHERE a.src = ?1 AND a.kind = 'file'
      UNION ALL
      SELECT CASE WHEN src = ?1 THEN dst ELSE src END, 'co_use', 4
      FROM edges WHERE (src = ?1 OR dst = ?1) AND kind = 'co_use' AND weight >= 2
    )
    SELECT n.other AS id, n.kind AS kind, MIN(n.rank) AS rank
    FROM n JOIN memories m ON m.id = n.other
    GROUP BY n.other
    ORDER BY rank, n.other
    LIMIT ?2
  `).all(id, o.limit ?? 5);
  return rows.map((r) => ({ id: String(r.id), kind: String(r.kind) }));
}

/**
 * Judged edges between memories of a set, for export: only edges whose *both* ends are exported,
 * so an edge never leaks the id (and therefore the title slug) of a private or global memory.
 * @param {import('node:sqlite').DatabaseSync} db
 * @param {Set<string>} ids
 * @returns {Map<string, Record<string, string[]>>}  src → kind → dst[]
 */
export function sharedEdges(db, ids) {
  /** @type {Map<string, Record<string, string[]>>} */
  const out = new Map();
  const rows = db.prepare(`SELECT src, dst, kind FROM edges WHERE kind IN (${SHARED_KINDS.map(() => '?').join(',')}) ORDER BY src, kind, dst`).all(...SHARED_KINDS);
  for (const r of rows) {
    const src = String(r.src);
    const dst = String(r.dst);
    if (!ids.has(src) || !ids.has(dst)) continue;
    const byKind = out.get(src) || {};
    (byKind[String(r.kind)] ||= []).push(dst);
    out.set(src, byKind);
  }
  return out;
}
