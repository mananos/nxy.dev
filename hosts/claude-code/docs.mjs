// @ts-check
/**
 * Finding the docs a change concerns (core/docs.mjs), on disk.
 *
 * With rg: one search over `*.md`/`*.mdx`, respecting .gitignore — milliseconds. Without it: a walk
 * over the repo's `.md` files that skips dependency and build folders, reading each once — a few
 * hundred ms on a big monorepo, and it runs once per review, never per edit.
 *
 * `docs.paths` in `.nxy/config.json` narrows the search to those folders and replaces the wiki
 * detection.
 */
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { findRg, rgSearch } from '../../core/rg.mjs';
import { docTerms, termsPattern, wikiDirFromCi } from '../../core/docs.mjs';

const SKIP = new Set(['node_modules', '.git', '.nxy', 'target', 'build', 'dist', 'out', '.next', '.angular', 'vendor', '.venv', 'venv', '__pycache__', 'coverage', '.gradle', '.idea', '.vscode']);
const MAX_FILES = 5000;
const MAX_BYTES = 1024 * 1024;

const isDirAt = (root) => (rel) => {
  try {
    return statSync(join(root, rel)).isDirectory();
  } catch {
    return false;
  }
};

/** CI pipeline files that may publish docs to a wiki. */
function pipelineTexts(root) {
  const files = [];
  const wf = join(root, '.github', 'workflows');
  if (existsSync(wf)) for (const n of readdirSync(wf)) if (/\.ya?ml$/i.test(n)) files.push(join(wf, n));
  for (const n of readdirSync(root)) if (/^(azure-pipelines.*|\.gitlab-ci|bitbucket-pipelines)\.ya?ml$/i.test(n)) files.push(join(root, n));
  const az = join(root, '.azure-pipelines');
  if (existsSync(az)) for (const n of readdirSync(az)) if (/\.ya?ml$/i.test(n)) files.push(join(az, n));
  return files.map((f) => {
    try {
      return readFileSync(f, 'utf8');
    } catch {
      return '';
    }
  });
}

/** Every .md under `dir`, skipping dependency and build folders. */
function walkMd(root, dir, out = []) {
  let entries;
  try {
    entries = readdirSync(dir, { withFileTypes: true });
  } catch {
    return out;
  }
  for (const e of entries) {
    if (out.length >= MAX_FILES) break;
    if (e.isDirectory()) {
      if (!SKIP.has(e.name)) walkMd(root, join(dir, e.name), out);
    } else if (/\.mdx?$/i.test(e.name)) out.push(join(dir, e.name));
  }
  return out;
}

/**
 * @param {string} root project root
 * @param {string[]} changed repo-relative paths the plan changed
 * @param {{paths?: string[]}} [docsCfg] `docs` from the config
 * @param {string|null} [rgBin] the rg to use; null = walk the files (what happens without rg)
 * @returns {{docs: {path: string, lines: {line: number, text: string}[]}[], wikiDir: string|null, terms: string[]}}
 */
export function findDocs(root, changed, docsCfg = {}, rgBin = findRg()) {
  const terms = docTerms(changed);
  const roots = Array.isArray(docsCfg.paths) && docsCfg.paths.length ? docsCfg.paths.filter(isDirAt(root)) : ['.'];
  const wikiDir = Array.isArray(docsCfg.paths) && docsCfg.paths.length ? null : wikiDirFromCi(pipelineTexts(root), isDirAt(root));
  if (!terms.length) return { docs: [], wikiDir, terms };
  const alreadyChanged = new Set(changed);
  /** @type {Map<string, {line: number, text: string}[]>} */
  const hits = new Map();
  const add = (path, line, text) => {
    if (alreadyChanged.has(path)) return;
    const list = hits.get(path) || [];
    if (list.length < 3) list.push({ line, text: text.trim().slice(0, 160) });
    hits.set(path, list);
  };
  const re = new RegExp(termsPattern(terms));
  if (rgBin) {
    for (const r of roots) {
      // The same folders the walk skips: .gitignore does not always list them (or there is no repo).
      const globs = ['*.md', '*.mdx', ...[...SKIP].map((d) => `!${d}`)];
      const res = rgSearch(join(root, r), termsPattern(terms), { bin: rgBin, fixed: false, globs, maxRows: 300 });
      for (const row of res.rows) add((r === '.' ? row.path : `${r.replace(/\/+$/, '')}/${row.path}`).replace(/^\.\//, ''), row.line, row.text);
    }
  } else {
    for (const r of roots) {
      for (const file of walkMd(root, join(root, r))) {
        try {
          if (statSync(file).size > MAX_BYTES) continue;
          readFileSync(file, 'utf8').split(/\r?\n/).forEach((text, i) => {
            if (re.test(text)) add(relative(root, file).replace(/\\/g, '/'), i + 1, text);
          });
        } catch {
          /* unreadable: skip */
        }
      }
    }
  }
  return { docs: [...hits].map(([path, lines]) => ({ path, lines })).sort((a, b) => a.path.localeCompare(b.path)), wikiDir, terms };
}
