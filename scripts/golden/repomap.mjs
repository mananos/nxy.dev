// @ts-check
/**
 * Golden of the 1.x repo map (`core/index/languages.mjs`, `core/index/repomap.mjs`) that
 * `internal/repomap` ports. Node is the oracle. Sets: ext (path.extname + rulesFor, and
 * indexedExtensions), symbols (extractSymbols over snippets of the six rule sets; the corpus writes
 * long lines as {pre, fill, n, post} and the golden input holds the expanded text), ignore (the
 * .gitignore matcher, fed one probe at a time), query (queryRepoMap over a literal map text written
 * to the index path), noindex (query with no map), build (buildRepoMap + listSourceFiles over a tree
 * materialized under a real temp dir, Date.now fixed; meta root shown as <root>), info (repoMapInfo).
 *
 *   node scripts/golden/repomap.mjs          write testdata/golden/repomap/*.json
 *   node scripts/golden/repomap.mjs --check  fail when they differ
 */
import { mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, utimesSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, extname, join, relative, sep } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { extractSymbols, indexedExtensions, rulesFor } from '../../core/index/languages.mjs';
import { buildRepoMap, indexPaths, listSourceFiles, queryRepoMap, repoMapInfo } from '../../core/index/repomap.mjs';
import { diffArea, loadCorpus, staleSets, writeArea } from './lib.mjs';

export const AREA = 'repomap';

/**
 * The 1.x keeps gitignoreMatcher private. The golden loads the same source text with one export
 * added (relative imports made absolute, since a data: module cannot resolve them), so the oracle is
 * the real matcher and core/ stays untouched.
 * @returns {Promise<(root: string) => (rel: string, name: string, isDir: boolean) => boolean>}
 */
async function privateGitignoreMatcher() {
  const src = fileURLToPath(new URL('../../core/index/repomap.mjs', import.meta.url));
  const dirUrl = pathToFileURL(dirname(src) + sep).href;
  const text = readFileSync(src, 'utf8').replace(/from '(\.\.?\/[^']+)'/g, (_, p) => `from '${new URL(p, dirUrl).href}'`);
  const mod = await import(`data:text/javascript;base64,${Buffer.from(`${text}\nexport { gitignoreMatcher as goldenGitignoreMatcher };\n`).toString('base64')}`);
  return mod.goldenGitignoreMatcher;
}

const gitignoreMatcher = await privateGitignoreMatcher();

const idx = (/** @type {number} */ i) => String(i).padStart(3, '0');
const kindsOf = (/** @type {string} */ ext) => rulesFor(ext)?.map((r) => r.kind) ?? null;

/** @param {string | {pre?: string, fill: string, n: number, post?: string}} l */
const expand = (l) => (typeof l === 'string' ? l : `${l.pre ?? ''}${l.fill.repeat(l.n)}${l.post ?? ''}`);

/** @returns {Record<string, any[]>} */
export function generate() {
  /** @type {any} */
  const c = loadCorpus('repomap');

  const ext = [
    ...c.names.map((/** @type {string} */ name, /** @type {number} */ i) => {
      const e = extname(name);
      return { name: `ext:${idx(i)}:${JSON.stringify(name)}`, input: { name }, output: { ext: e, kinds: kindsOf(e) } };
    }),
    { name: 'indexed', input: {}, output: { extensions: indexedExtensions() } },
  ];

  const symbols = c.symbols.map((/** @type {any} */ x, /** @type {number} */ i) => {
    const text = x.lines.map(expand).join(x.crlf ? '\r\n' : '\n');
    const rules = rulesFor(x.ext);
    if (!rules) throw new Error(`symbols:${x.name}: no rules for ${x.ext}`);
    return { name: `symbols:${idx(i)}:${x.name}`, input: { ext: x.ext, text }, output: { symbols: extractSymbols(text, rules) } };
  });

  const root = realpathSync(mkdtempSync(`${tmpdir()}/nxy-repomap-golden-`));
  /** @type {any[]} */
  let query = [];
  /** @type {any[]} */
  let ignore = [];
  try {
    // the matcher reads <root>/.gitignore, so each case writes its text there
    ignore = c.ignore.map((/** @type {any} */ x, /** @type {number} */ i) => {
      const probes = x.probes.map((/** @type {[string, boolean]} */ [rel, isDir]) => ({ rel, name: rel.split('/').pop(), isDir }));
      writeFileSync(`${root}/.gitignore`, x.text, 'utf8');
      const m = gitignoreMatcher(root);
      return {
        name: `ignore:${idx(i)}:${x.name}`,
        input: { text: x.text, probes },
        output: { ignored: probes.map((/** @type {any} */ p) => m(p.rel, p.name, p.isDir)) },
      };
    });

    const paths = indexPaths(root);
    mkdirSync(paths.dir, { recursive: true });
    const mapText = c.query.rows.map((/** @type {string[]} */ r) => r.join('\t')).join('\n') + '\n';
    writeFileSync(paths.map, mapText, 'utf8');
    query = c.query.queries.map((/** @type {any} */ q, /** @type {number} */ i) => ({
      name: `query:${idx(i)}:${JSON.stringify(q.query)}`,
      input: { map: mapText, query: q.query, opts: q.opts ?? {} },
      output: queryRepoMap(root, q.query, q.opts ?? {}),
    }));
  } finally {
    rmSync(root, { recursive: true, force: true });
  }

  const noindex = [oneInRoot((r) => ({ name: 'noindex', input: { query: 'app' }, output: queryRepoMap(r, 'app') }))];
  const build = c.build.map((/** @type {any} */ x) => oneInRoot((r) => buildCase(r, x)));
  const info = c.info.map((/** @type {any} */ x) => oneInRoot((r) => infoCase(r, x)));

  return { ext, symbols, ignore, query, noindex, build, info };
}

/**
 * Runs fn on a fresh real temp dir (mkdtemp + realpath), removed after.
 * @param {(root: string) => any} fn
 */
function oneInRoot(fn) {
  const root = realpathSync(mkdtempSync(`${tmpdir()}/nxy-repomap-golden-`));
  try {
    return fn(root);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
}

/** Runs fn with Date.now fixed, so builtAt is known and ms is 0. */
function atNow(/** @type {number} */ now, /** @type {() => any} */ fn) {
  const real = Date.now;
  Date.now = () => now;
  try {
    return fn();
  } finally {
    Date.now = real;
  }
}

/** A spec is {text, mtime} or {size, mtime} (size: that many "a"). */
function writeSpec(/** @type {string} */ root, /** @type {string} */ rel, /** @type {any} */ spec) {
  const abs = join(root, rel);
  mkdirSync(dirname(abs), { recursive: true });
  writeFileSync(abs, spec.text ?? 'a'.repeat(spec.size));
  const d = new Date(spec.mtime);
  utimesSync(abs, d, d);
}

const relTo = (/** @type {string} */ root, /** @type {string} */ p) => relative(root, p).split(sep).join('/');
const byUnits = (/** @type {string} */ a, /** @type {string} */ b) => (a < b ? -1 : a > b ? 1 : 0);
const shown = (/** @type {string} */ root, /** @type {any} */ r) => ({ ...r, map: relTo(root, r.map) });

/** @param {string} root @param {any} x */
function buildCase(root, x) {
  if (x.gitignore !== undefined) writeFileSync(join(root, '.gitignore'), x.gitignore, 'utf8');
  for (const [rel, spec] of Object.entries(x.tree)) writeSpec(root, rel, spec);
  const paths = indexPaths(root);
  const readMap = () => readFileSync(paths.map, 'utf8');
  const first = atNow(x.now, () => buildRepoMap(root));
  const meta = JSON.parse(readFileSync(paths.meta, 'utf8'));
  meta.root = '<root>';
  const listed = listSourceFiles(root)
    .map((f) => ({ rel: f.rel, size: f.size, mtimeMs: f.mtimeMs }))
    .sort((a, b) => byUnits(a.rel, b.rel));
  const steps = [];
  for (const s of x.steps ?? []) {
    for (const [rel, spec] of Object.entries(s.write ?? {})) writeSpec(root, rel, spec);
    for (const rel of s.remove ?? []) rmSync(join(root, rel));
    if (s.meta !== undefined) writeFileSync(paths.meta, s.meta, 'utf8');
    const r = atNow(x.now, () => buildRepoMap(root, { force: !!s.force }));
    steps.push({ name: s.name, result: shown(root, r), map: readMap() });
  }
  return {
    name: `build:${x.name}`,
    input: { now: x.now, gitignore: x.gitignore ?? null, tree: x.tree, steps: x.steps ?? [] },
    output: { first: shown(root, first), map: readMap(), meta, listed, steps },
  };
}

/** @param {string} root @param {any} x */
function infoCase(root, x) {
  const paths = indexPaths(root);
  mkdirSync(paths.dir, { recursive: true });
  if (x.map !== undefined) writeFileSync(paths.map, x.map, 'utf8');
  if (x.meta !== undefined) writeFileSync(paths.meta, JSON.stringify(x.meta), 'utf8');
  if (x.metaRaw !== undefined) writeFileSync(paths.meta, x.metaRaw, 'utf8');
  const got = atNow(x.now, () => repoMapInfo(root));
  return {
    name: `info:${x.name}`,
    input: { now: x.now, map: x.map ?? null, meta: x.meta ?? null, metaRaw: x.metaRaw ?? null },
    output: { info: got ? shown(root, got) : null },
  };
}

/** @returns {string[]} differences, empty when up to date */
export function check() {
  const sets = generate();
  const diffs = [];
  for (const [set, cases] of Object.entries(sets)) diffs.push(...diffArea(AREA, set, cases, true));
  diffs.push(...staleSets(AREA, Object.keys(sets)));
  return diffs;
}

export function write() {
  return Object.entries(generate()).map(([set, cases]) => writeArea(AREA, set, cases, true));
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  if (process.argv.includes('--check')) {
    const diffs = check();
    for (const d of diffs) console.error(d);
    if (diffs.length) process.exit(1);
    console.log('golden files up to date');
  } else {
    for (const p of write()) console.log(`wrote ${p}`);
  }
}
