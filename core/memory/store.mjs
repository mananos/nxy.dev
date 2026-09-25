// @ts-check
/**
 * Memory store: one SQLite database per user, FTS5 for search, no dependencies.
 *
 * `node:sqlite` ships with Node 22 and carries FTS5 and bm25, which is the whole reason this can
 * be a real index instead of a folder of files grepped at startup. Storage was never the
 * bottleneck (SQLite answers in microseconds); the thing worth optimising is how many tokens
 * reach a context, and that is what the scope filter and the budget are for.
 *
 * One database holds every scope, with `scope` as a column — a single index is what lets one
 * search answer across global, project and area at once.
 */
import { createRequire } from 'node:module';
import { join } from 'node:path';
import { ensureDir, nxyUserDir } from '../paths.mjs';
import { contentTerms } from '../text.mjs';

const require = createRequire(import.meta.url);

/** @typedef {import('./scope.mjs').Scope} Scope */

/**
 * @typedef {object} Memory
 * @property {string} id
 * @property {Scope} scope
 * @property {string|null} project
 * @property {string|null} area
 * @property {string} type     decision | bug | convention | preference | handoff
 * @property {string} title
 * @property {string} body
 * @property {string} keywords  space-separated aliases: the lexical escape hatch for "same idea,
 *                              other words", which is the failure mode of any keyword search
 * @property {boolean} private  never leaves the machine on export
 * @property {number} created
 * @property {number} updated
 */

export const TYPES = ['decision', 'bug', 'convention', 'preference', 'handoff'];

let DatabaseSync = null;

/**
 * Loads `node:sqlite`.
 *
 * Importing it prints "SQLite is an experimental feature" to stderr, and that line cannot be
 * suppressed from inside the process: Node emits it from the runtime, so a `warning` listener
 * observes it but does not stop it from being printed. The only thing that works is the flag, so
 * every nxy entry point that touches memory runs with `--disable-warning=ExperimentalWarning`
 * (see hooks.json and the `mem` command). Anything else Node warns about still prints normally.
 */
function sqlite() {
  if (!DatabaseSync) ({ DatabaseSync } = require('node:sqlite'));
  return DatabaseSync;
}

/** Path of the user's memory database (`NXY_HOME` redirects it; tests use a temp dir). */
export function memoryDbPath() {
  return join(nxyUserDir(), 'memory', 'memory.db');
}

/**
 * Opens (and migrates) the store. Callers should `close()` when done; a CLI that exits can skip it.
 * @param {{path?: string}} [opts]
 */
export function openStore(opts = {}) {
  const path = opts.path || memoryDbPath();
  ensureDir(join(path, '..'));
  const Db = sqlite();
  const db = new Db(path);
  db.exec(`
    PRAGMA journal_mode = WAL;
    CREATE TABLE IF NOT EXISTS memories (
      rowid    INTEGER PRIMARY KEY,
      id       TEXT NOT NULL UNIQUE,
      scope    TEXT NOT NULL,
      project  TEXT,
      area     TEXT,
      type     TEXT NOT NULL,
      title    TEXT NOT NULL,
      body     TEXT NOT NULL,
      keywords TEXT NOT NULL DEFAULT '',
      private  INTEGER NOT NULL DEFAULT 0,
      created  INTEGER NOT NULL,
      updated  INTEGER NOT NULL
    );
    CREATE INDEX IF NOT EXISTS memories_scope ON memories(scope, project, area);
    CREATE INDEX IF NOT EXISTS memories_updated ON memories(updated DESC);
    CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
      title, body, keywords, content='memories', content_rowid='rowid'
    );
    CREATE TRIGGER IF NOT EXISTS memories_ai AFTER INSERT ON memories BEGIN
      INSERT INTO memories_fts(rowid, title, body, keywords) VALUES (new.rowid, new.title, new.body, new.keywords);
    END;
    CREATE TRIGGER IF NOT EXISTS memories_ad AFTER DELETE ON memories BEGIN
      INSERT INTO memories_fts(memories_fts, rowid, title, body, keywords) VALUES ('delete', old.rowid, old.title, old.body, old.keywords);
    END;
    CREATE TABLE IF NOT EXISTS edges (
      src     TEXT NOT NULL,
      dst     TEXT NOT NULL,
      kind    TEXT NOT NULL,
      weight  REAL NOT NULL DEFAULT 1,
      by      TEXT NOT NULL DEFAULT 'code',
      created INTEGER NOT NULL,
      PRIMARY KEY (src, dst, kind)
    );
    CREATE INDEX IF NOT EXISTS edges_dst ON edges(dst, kind);
    CREATE TRIGGER IF NOT EXISTS memories_ad_edges AFTER DELETE ON memories BEGIN
      DELETE FROM edges WHERE src = old.id OR dst = old.id;
    END;
    CREATE TRIGGER IF NOT EXISTS memories_au AFTER UPDATE ON memories BEGIN
      INSERT INTO memories_fts(memories_fts, rowid, title, body, keywords) VALUES ('delete', old.rowid, old.title, old.body, old.keywords);
      INSERT INTO memories_fts(rowid, title, body, keywords) VALUES (new.rowid, new.title, new.body, new.keywords);
    END;
  `);
  return db;
}

/** A stable, readable id: scope, a slug of the title, and enough of the clock to avoid collisions. */
export function makeId(scope, title, now = Date.now()) {
  const slug = (title || 'untitled')
    .toLowerCase()
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-|-$/g, '')
    .slice(0, 48) || 'untitled';
  return `${scope}-${slug}-${now.toString(36).slice(-5)}`;
}

/**
 * Inserts or updates a memory. Passing an existing `id` updates it in place, which is what makes
 * import idempotent and lets a memory be corrected instead of duplicated.
 * @param {import('node:sqlite').DatabaseSync} db
 * @param {Partial<Memory> & {title: string, body: string, scope: Scope}} mem
 */
export function saveMemory(db, mem) {
  const now = Date.now();
  const type = mem.type && TYPES.includes(mem.type) ? mem.type : 'decision';
  const id = mem.id || makeId(mem.scope, mem.title, now);
  const existing = db.prepare('SELECT id, created FROM memories WHERE id = ?').get(id);
  const created = existing ? Number(existing.created) : (mem.created || now);
  db.prepare(`
    INSERT INTO memories (id, scope, project, area, type, title, body, keywords, private, created, updated)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    ON CONFLICT(id) DO UPDATE SET
      scope=excluded.scope, project=excluded.project, area=excluded.area, type=excluded.type,
      title=excluded.title, body=excluded.body, keywords=excluded.keywords,
      private=excluded.private, updated=excluded.updated
  `).run(
    id, mem.scope, mem.project ?? null, mem.area ?? null, type,
    mem.title, mem.body, mem.keywords ?? '', mem.private ? 1 : 0,
    created, mem.updated || now,
  );
  return { id, created: !existing };
}

/** @param {import('node:sqlite').DatabaseSync} db */
export function getMemory(db, id) {
  const row = db.prepare('SELECT * FROM memories WHERE id = ?').get(id);
  return row ? rowToMemory(row) : null;
}

/** @param {import('node:sqlite').DatabaseSync} db */
export function deleteMemory(db, id) {
  return db.prepare('DELETE FROM memories WHERE id = ?').run(id).changes > 0;
}

function rowToMemory(row) {
  return {
    id: String(row.id),
    scope: /** @type {Scope} */ (String(row.scope)),
    project: row.project === null ? null : String(row.project),
    area: row.area === null ? null : String(row.area),
    type: String(row.type),
    title: String(row.title),
    body: String(row.body),
    keywords: String(row.keywords || ''),
    private: Number(row.private) === 1,
    created: Number(row.created),
    updated: Number(row.updated),
  };
}

/**
 * Builds the `WHERE` fragment that keeps a query inside the scopes in play.
 * Global memories always apply; project ones only for this project; area ones only for areas the
 * caller named. This is the filter that stops a monorepo's `web/` history from reaching `api/`.
 */
function scopeClause(opts) {
  const parts = ["scope = 'global'"];
  const params = [];
  if (opts.project) {
    parts.push('(scope = ? AND project = ?)');
    params.push('project', opts.project);
  }
  if (opts.project && opts.allAreas) {
    parts.push("(scope = 'area' AND project = ?)");
    params.push(opts.project);
  } else if (opts.project && opts.areas && opts.areas.length) {
    parts.push(`(scope = 'area' AND project = ? AND area IN (${opts.areas.map(() => '?').join(',')}))`);
    params.push(opts.project, ...opts.areas);
  }
  return { sql: `(${parts.join(' OR ')})`, params };
}

/**
 * Full-text search, ranked by bm25 and filtered by scope.
 *
 * The query is sanitised into a plain FTS5 OR-of-terms: users type questions, not FTS5 syntax, and
 * an unescaped quote or `*` would otherwise throw where a search should simply return nothing.
 * @param {import('node:sqlite').DatabaseSync} db
 * @param {string} query
 * @param {{project?: string|null, areas?: string[], limit?: number, type?: string|null}} [opts]
 */
export function searchMemories(db, query, opts = {}) {
  const terms = contentTerms(query).map((t) => `"${t.replace(/"/g, '')}"`);
  if (!terms.length) return [];
  const scope = scopeClause(opts);
  const typeSql = opts.type ? ' AND m.type = ?' : '';
  const rows = db.prepare(`
    SELECT m.*, bm25(memories_fts) AS score
    FROM memories_fts
    JOIN memories m ON m.rowid = memories_fts.rowid
    WHERE memories_fts MATCH ? AND ${scope.sql}${typeSql}
    ORDER BY score
    LIMIT ?
  `).all(terms.join(' OR '), ...scope.params, ...(opts.type ? [opts.type] : []), opts.limit ?? 10);
  return rows.map((r) => ({ ...rowToMemory(r), score: Number(r.score) }));
}

/**
 * The index: one line per memory, newest first, no bodies.
 * This is what a session can afford to carry; bodies are fetched on demand.
 * @param {import('node:sqlite').DatabaseSync} db
 * @param {{project?: string|null, areas?: string[], allAreas?: boolean, limit?: number, scope?: Scope|null, type?: string|null}} [opts]
 */
export function listMemories(db, opts = {}) {
  const scope = scopeClause(opts);
  const only = opts.scope ? ' AND scope = ?' : '';
  const ofType = opts.type ? ' AND type = ?' : '';
  const rows = db.prepare(`
    SELECT * FROM memories
    WHERE ${scope.sql}${only}${ofType}
    ORDER BY updated DESC
    LIMIT ?
  `).all(...scope.params, ...(opts.scope ? [opts.scope] : []), ...(opts.type ? [opts.type] : []), opts.limit ?? 200);
  return rows.map(rowToMemory);
}

/** Every memory of a project, for export. Private ones never leave. */
export function exportableMemories(db, project) {
  const rows = db.prepare(`
    SELECT * FROM memories
    WHERE project = ? AND scope IN ('project','area') AND private = 0
    ORDER BY scope, area, updated DESC
  `).all(project);
  return rows.map(rowToMemory);
}

/** Counts by scope, for `/nxy:mem status`. */
export function countsByScope(db, project) {
  const rows = db.prepare(`
    SELECT scope, COUNT(*) AS n FROM memories
    WHERE scope = 'global' OR project = ?
    GROUP BY scope
  `).all(project ?? null);
  /** @type {Record<string, number>} */
  const out = { global: 0, project: 0, area: 0 };
  for (const r of rows) out[String(r.scope)] = Number(r.n);
  return out;
}
