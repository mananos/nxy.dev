// @ts-check
/**
 * Export/import as Markdown with frontmatter.
 *
 * SQLite is the storage; Markdown is the *interchange* format. The difference matters: engram's
 * git sync writes gzipped chunks, which sync fine but cannot be read or reviewed in a pull
 * request. A team convention that nobody can review in a PR is not a shared convention.
 *
 * So: one file per memory, under `<repo>/.nxy/memory/`, with a stable id in the filename. Diffable,
 * editable by hand, and mergeable by id. Import is idempotent — same id and same `updated` is a
 * no-op, so re-importing after every pull costs nothing.
 *
 * `global` never leaves the machine, and neither does anything marked private: onboarding should
 * hand somebody the team's decisions, not your preferences.
 *
 * Graph edges: judged ones (`supersedes`, `related`, `conflicts_with`) travel as frontmatter
 * lists, and only when both ends are exported. File and `[[id]]` edges do not need to: they are
 * re-derived from the body on import, checked against the importer's own checkout. Co-use never
 * travels (see graph.mjs).
 */
import { existsSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { ensureDir, nxyProjectDir } from '../paths.mjs';
import { TYPES, exportableMemories, getMemory, saveMemory } from './store.mjs';
import { SHARED_KINDS, addEdge, deriveEdges, sharedEdges } from './graph.mjs';
import { repoRoot } from './scope.mjs';

/** Where a repo keeps its shareable memories. Committed on purpose. */
export function memoryDir(cwd) {
  return join(nxyProjectDir(cwd), 'memory');
}

const FIELDS = ['id', 'scope', 'type', 'area', 'title', 'keywords', 'created', 'updated'];

/**
 * Serialises one memory to Markdown. Values are quoted only when they need to be.
 * @param {any} mem
 * @param {Record<string, string[]>} [edges]  judged edges from this memory, by kind
 */
export function toMarkdown(mem, edges = {}) {
  const esc = (v) => {
    const s = String(v ?? '');
    return /^[\w./@:-]*$/.test(s) && s !== '' ? s : JSON.stringify(s);
  };
  const lines = ['---'];
  for (const f of FIELDS) {
    const v = mem[f];
    if (v === null || v === undefined || v === '') continue;
    lines.push(`${f}: ${esc(v)}`);
  }
  for (const k of SHARED_KINDS) if (edges[k]?.length) lines.push(`${k}: ${esc(edges[k].join(' '))}`);
  lines.push('---', '', `# ${mem.title}`, '', String(mem.body).trim(), '');
  return lines.join('\n');
}

/** Parses a Markdown file back into a memory, or null when it is not one of ours. */
export function fromMarkdown(text) {
  const m = /^---\r?\n([\s\S]*?)\r?\n---\r?\n?([\s\S]*)$/.exec(text || '');
  if (!m) return null;
  /** @type {Record<string, string>} */
  const meta = {};
  for (const line of m[1].split('\n')) {
    const kv = /^([a-zA-Z_]+):\s*(.*)$/.exec(line.trim());
    if (!kv) continue;
    let v = kv[2].trim();
    if (v.startsWith('"')) {
      try {
        v = JSON.parse(v);
      } catch {
        v = v.replace(/^"|"$/g, '');
      }
    }
    meta[kv[1]] = v;
  }
  if (!meta.id || !meta.title) return null;
  // Drop the leading `# Title` the exporter adds; the title lives in the frontmatter.
  const body = m[2].replace(/^\s*#\s+.*\r?\n+/, '').trim();
  return {
    id: meta.id,
    scope: /** @type {import('./scope.mjs').Scope} */ (meta.scope === 'area' ? 'area' : 'project'), // a global memory is never in a repo
    type: TYPES.includes(meta.type) ? meta.type : 'decision',
    area: meta.area || null,
    title: meta.title,
    keywords: meta.keywords || '',
    body,
    created: Number(meta.created) || Date.now(),
    updated: Number(meta.updated) || Date.now(),
    private: false,
    edges: Object.fromEntries(SHARED_KINDS.filter((k) => meta[k]).map((k) => [k, meta[k].split(/\s+/).filter(Boolean)])),
  };
}

const fileName = (mem) => `${mem.id}.md`;

/**
 * Writes this project's shareable memories to `<repo>/.nxy/memory/`.
 *
 * Files whose memory no longer exists are removed, so the directory is a faithful mirror rather
 * than an append-only pile — otherwise a deleted memory would come back on the next import.
 * @param {import('node:sqlite').DatabaseSync} db
 * @param {string} cwd
 * @param {string} project
 */
export function exportProject(db, cwd, project) {
  const dir = memoryDir(cwd);
  const mems = exportableMemories(db, project);
  const edges = sharedEdges(db, new Set(mems.map((m) => m.id)));
  ensureDir(dir);
  const keep = new Set(mems.map(fileName));
  let written = 0;
  for (const mem of mems) {
    const p = join(dir, fileName(mem));
    const next = toMarkdown(mem, edges.get(mem.id));
    let current = null;
    try {
      current = existsSync(p) ? readFileSync(p, 'utf8') : null;
    } catch {
      current = null;
    }
    if (current === next) continue; // do not touch mtimes for unchanged files: keeps git quiet
    writeFileSync(p, next, 'utf8');
    written++;
  }
  let removed = 0;
  for (const f of safeReaddir(dir)) {
    if (!f.endsWith('.md') || keep.has(f)) continue;
    try {
      rmSync(join(dir, f));
      removed++;
    } catch {
      /* leave it; the next export will try again */
    }
  }
  return { dir, total: mems.length, written, removed };
}

function safeReaddir(dir) {
  try {
    return readdirSync(dir);
  } catch {
    return [];
  }
}

/**
 * Reads `<repo>/.nxy/memory/` into the store.
 *
 * Merge rule: newest `updated` wins, per id. Equal timestamps are a no-op, which is what makes
 * running this on every pull free. Nothing is ever deleted locally by an import — a teammate
 * dropping a memory from the repo should not silently erase yours.
 * @param {import('node:sqlite').DatabaseSync} db
 * @param {string} cwd
 * @param {string} project
 */
export function importProject(db, cwd, project) {
  const dir = memoryDir(cwd);
  let added = 0;
  let updated = 0;
  let skipped = 0;
  const failed = [];
  const root = repoRoot(cwd);
  for (const f of safeReaddir(dir)) {
    if (!f.endsWith('.md')) continue;
    let mem;
    try {
      mem = fromMarkdown(readFileSync(join(dir, f), 'utf8'));
    } catch {
      mem = null;
    }
    if (!mem) {
      failed.push(f);
      continue;
    }
    // Edges are a union: a verdict a teammate made adds to yours and never removes one.
    for (const [kind, dsts] of Object.entries(mem.edges)) for (const d of dsts) addEdge(db, mem.id, d, kind, { by: 'import' });
    const existing = getMemory(db, mem.id);
    if (existing && existing.updated >= mem.updated) {
      skipped++;
      continue;
    }
    const { edges: _e, ...fields } = mem;
    saveMemory(db, { ...fields, project });
    deriveEdges(db, fields, root);
    if (existing) updated++;
    else added++;
  }
  return { dir, added, updated, skipped, failed };
}
