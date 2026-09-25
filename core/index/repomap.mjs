// @ts-check
/**
 * Repo map: a grep-able list of every symbol in the repo, built by a script and never by a model.
 *
 * The rule behind it (roadmap tema 3): search without a model, hand the model only the result.
 * Exact text is rg's job at zero tokens; structure is this index, queried without a model so that
 * only matching lines enter a context; meaning is the scout's job, over those lines. The map is the
 * floor that always works - no daemon, no dependencies, no language support to wait for.
 *
 * Format, one symbol per line, tab-separated:  path<TAB>line<TAB>kind<TAB>name<TAB>signature
 * Line-oriented on purpose: rg over this file is the query engine.
 */
import { existsSync, readFileSync, readdirSync, statSync, writeFileSync } from 'node:fs';
import { extname, join, relative, sep } from 'node:path';
import { ensureDir, nxyRuntimeDir } from '../paths.mjs';
import { extractSymbols, rulesFor } from './languages.mjs';

/** Directories that never hold source worth indexing, whatever the repo says. */
const ALWAYS_SKIP = new Set([
  '.git', 'node_modules', '.nxy', 'dist', 'build', 'target', 'out', 'bin', 'obj', 'vendor',
  '.venv', 'venv', '__pycache__', '.next', '.nuxt', '.svelte-kit', 'coverage', '.gradle',
  '.idea', '.vscode', '.cache', 'tmp', '.terraform', 'site-packages', '.tox', '.mypy_cache',
]);

const MAX_FILE_BYTES = 1_000_000; // bigger than this is generated or minified, not source to map

/** Where the index and its metadata live for a given repo. */
export function indexPaths(cwd) {
  const dir = join(nxyRuntimeDir(cwd), 'index');
  return { dir, map: join(dir, 'repomap.tsv'), meta: join(dir, 'repomap.meta.json') };
}

/**
 * Minimal .gitignore support: bare names, `dir/`, `/rooted` and `*.ext`. Anything fancier is
 * skipped rather than guessed at - over-indexing is cheap, wrongly dropping real source is not.
 * @param {string} root
 */
function gitignoreMatcher(root) {
  const p = join(root, '.gitignore');
  if (!existsSync(p)) return () => false;
  /** @type {{re: RegExp, dirOnly: boolean, rooted: boolean}[]} */
  const pats = [];
  for (let line of readFileSync(p, 'utf8').split('\n')) {
    line = line.trim();
    if (!line || line.startsWith('#') || line.startsWith('!')) continue;
    const dirOnly = line.endsWith('/');
    if (dirOnly) line = line.slice(0, -1);
    const rooted = line.startsWith('/');
    if (rooted) line = line.slice(1);
    if (!line || line.includes('**')) continue;
    const body = line.split('*').map((s) => s.replace(/[.+^${}()|[\]\\?]/g, '\\$&')).join('[^/]*');
    try {
      pats.push({ re: new RegExp(`^${body}$`), dirOnly, rooted });
    } catch {
      // a pattern we cannot compile is a pattern we do not enforce
    }
  }
  /** @param {string} rel @param {string} name @param {boolean} isDir */
  return (rel, name, isDir) => {
    for (const { re, dirOnly, rooted } of pats) {
      if (dirOnly && !isDir) continue;
      if (re.test(rooted ? rel : name)) return true;
    }
    return false;
  };
}

/**
 * Walks the repo and returns the files worth indexing.
 * @param {string} root
 * @returns {{rel: string, abs: string, mtimeMs: number, size: number}[]}
 */
export function listSourceFiles(root) {
  const ignored = gitignoreMatcher(root);
  /** @type {{rel: string, abs: string, mtimeMs: number, size: number}[]} */
  const out = [];
  /** @param {string} dir */
  const walk = (dir) => {
    let entries;
    try {
      entries = readdirSync(dir, { withFileTypes: true });
    } catch {
      return; // unreadable directory: skip it rather than fail the whole index
    }
    for (const e of entries) {
      const abs = join(dir, e.name);
      const rel = relative(root, abs).split(sep).join('/');
      if (e.isDirectory()) {
        if (ALWAYS_SKIP.has(e.name) || e.name.startsWith('.') || ignored(rel, e.name, true)) continue;
        walk(abs);
      } else if (e.isFile()) {
        if (!rulesFor(extname(e.name)) || ignored(rel, e.name, false)) continue;
        let st;
        try {
          st = statSync(abs);
        } catch {
          continue;
        }
        if (st.size > MAX_FILE_BYTES) continue;
        out.push({ rel, abs, mtimeMs: st.mtimeMs, size: st.size });
      }
    }
  };
  walk(root);
  return out;
}

/**
 * Builds (or refreshes) the repo map. Unchanged files are reused from the previous run, so a
 * rebuild after touching a handful of files costs one stat per file plus a read of those few.
 * @param {string} cwd
 * @param {{force?: boolean}} [opts]
 */
export function buildRepoMap(cwd, opts = {}) {
  const started = Date.now();
  const paths = indexPaths(cwd);
  const files = listSourceFiles(cwd);
  /** @type {Record<string, {mtimeMs: number, size: number, lines: string[]}>} */
  let prev = {};
  if (!opts.force && existsSync(paths.meta)) {
    try {
      prev = JSON.parse(readFileSync(paths.meta, 'utf8')).files || {};
    } catch {
      prev = {};
    }
  }
  /** @type {Record<string, {mtimeMs: number, size: number, lines: string[]}>} */
  const next = {};
  let reused = 0;
  let parsed = 0;
  for (const f of files) {
    const before = prev[f.rel];
    if (before && before.mtimeMs === f.mtimeMs && before.size === f.size && Array.isArray(before.lines)) {
      next[f.rel] = before;
      reused++;
      continue;
    }
    const rules = rulesFor(extname(f.rel));
    if (!rules) continue;
    let text;
    try {
      text = readFileSync(f.abs, 'utf8');
    } catch {
      continue;
    }
    const lines = extractSymbols(text, rules).map((s) => `${f.rel}\t${s.line}\t${s.kind}\t${s.name}\t${s.sig}`);
    next[f.rel] = { mtimeMs: f.mtimeMs, size: f.size, lines };
    parsed++;
  }
  /** @type {string[]} */
  const all = [];
  for (const rel of Object.keys(next).sort()) all.push(...next[rel].lines);
  ensureDir(paths.dir);
  writeFileSync(paths.map, all.length ? all.join('\n') + '\n' : '', 'utf8');
  writeFileSync(paths.meta, JSON.stringify({ builtAt: Date.now(), root: cwd, files: next }), 'utf8');
  return { files: files.length, symbols: all.length, parsed, reused, ms: Date.now() - started, map: paths.map };
}

/** Index stats without rebuilding, or null when there is no index yet. */
export function repoMapInfo(cwd) {
  const paths = indexPaths(cwd);
  if (!existsSync(paths.map) || !existsSync(paths.meta)) return null;
  try {
    const meta = JSON.parse(readFileSync(paths.meta, 'utf8'));
    const files = Object.keys(meta.files || {}).length;
    let symbols = 0;
    for (const v of Object.values(meta.files || {})) symbols += (v && Array.isArray(v.lines) ? v.lines.length : 0);
    return { builtAt: meta.builtAt || 0, files, symbols, map: paths.map, ageMs: Date.now() - (meta.builtAt || 0) };
  } catch {
    return null;
  }
}

/**
 * Queries the map without a model: substring, or /regex/, over name, path and signature. The cap
 * is the point - an uncapped query is the file dump this exists to prevent.
 * @param {string} cwd
 * @param {string} query
 * @param {{limit?: number, kind?: string, path?: string}} [opts]
 */
export function queryRepoMap(cwd, query, opts = {}) {
  const paths = indexPaths(cwd);
  if (!existsSync(paths.map)) return { ok: false, reason: 'no-index', rows: [], truncated: false, scanned: 0 };
  const limit = opts.limit ?? 40;
  const m = /^\/(.*)\/([gimsuy]*)$/.exec(query.trim());
  /** @type {RegExp|null} */
  let re = null;
  if (m) {
    try {
      re = new RegExp(m[1], m[2].replace(/g/g, '') + 'i');
    } catch {
      re = null; // a broken regex falls back to substring rather than throwing at the caller
    }
  }
  const needle = query.toLowerCase();
  /** @type {{path: string, line: number, kind: string, name: string, sig: string}[]} */
  const rows = [];
  let scanned = 0;
  for (const line of readFileSync(paths.map, 'utf8').split('\n')) {
    if (!line) continue;
    scanned++;
    const [path, ln, kind, name, sig] = line.split('\t');
    if (opts.kind && kind !== opts.kind) continue;
    if (opts.path && !path.toLowerCase().includes(opts.path.toLowerCase())) continue;
    const hit = re
      ? re.test(name || '') || re.test(path || '') || re.test(sig || '')
      : (name || '').toLowerCase().includes(needle) || (path || '').toLowerCase().includes(needle);
    if (!hit) continue;
    rows.push({ path, line: Number(ln), kind, name, sig: sig || '' });
    if (rows.length >= limit) break;
  }
  return { ok: true, reason: 'ok', rows, scanned, truncated: rows.length >= limit };
}
