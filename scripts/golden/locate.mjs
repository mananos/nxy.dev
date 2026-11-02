// @ts-check
/**
 * Golden of the 1.x model-free search (`core/scout/search.mjs`) that `internal/scout` and
 * `internal/app/locate.go` port. Node is the oracle. Sets:
 *   terms:   queryTerms(question)            -> {identifiers, terms}
 *   report:  searchWithoutModel / relationsWithoutModel over a small tree materialized under a temp
 *            dir, with `rg` and `codegraph` replaced by scripts/golden/locatestub (answers from a
 *            table in the case) so the golden does not depend on what the machine has installed.
 *            Output: {text, degraded, terms, hits, codegraph}; `ms` is dropped and "_<n> ms" in the
 *            text becomes "_<ms> ms".
 * Not covered here: the "rg is not installed" layer (on Linux a real /usr/bin/rg is always found, so
 * the 1.x answer would depend on the machine); the Go tests in internal/scout cover it.
 *
 *   node scripts/golden/locate.mjs          write testdata/golden/locate/*.json
 *   node scripts/golden/locate.mjs --check  fail when they differ
 */
import { spawnSync } from 'node:child_process';
import { copyFileSync, mkdirSync, mkdtempSync, realpathSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { queryTerms, relationsWithoutModel, searchWithoutModel } from '../../core/scout/search.mjs';
import { ROOT, diffArea, loadCorpus, staleSets, writeArea } from './lib.mjs';

export const AREA = 'locate';
const idx = (/** @type {number} */ i) => String(i).padStart(3, '0');

function haveGo() {
  return spawnSync('go', ['version'], { windowsHide: true }).status === 0;
}

/** Builds scripts/golden/locatestub and copies it as rg and codegraph; returns the dir. */
function buildStubs() {
  const dir = realpathSync(mkdtempSync(join(tmpdir(), 'nxy-golden-locate-')));
  const ext = process.platform === 'win32' ? '.exe' : '';
  const rg = join(dir, `rg${ext}`);
  const b = spawnSync('go', ['build', '-o', rg, './scripts/golden/locatestub'], { cwd: ROOT, encoding: 'utf8', windowsHide: true });
  if (b.status !== 0) {
    rmSync(dir, { recursive: true, force: true });
    throw new Error(`go build locatestub failed: ${b.stderr || b.stdout}`);
  }
  copyFileSync(rg, join(dir, `codegraph${ext}`));
  return dir;
}

/** rg `rows` become the stdout of rg; codegraph answers carry their stdout as is. */
function stubTable(/** @type {any} */ c) {
  /** @type {Record<string, any>} */
  const rg = {};
  for (const [k, v] of Object.entries(c.rg ?? {})) {
    const rows = /** @type {string[]} */ (v.rows);
    rg[k] = { code: v.code, stdout: rows.length ? `${rows.join('\n')}\n` : '' };
  }
  return { rg, codegraph: c.cg ?? {} };
}

/** @param {string} stubDir @param {string} root @param {any} c */
function runCase(stubDir, root, tree, c) {
  for (const [rel, text] of Object.entries(tree)) {
    mkdirSync(dirname(join(root, rel)), { recursive: true });
    writeFileSync(join(root, rel), text);
  }
  if (c.index) {
    mkdirSync(join(root, '.codegraph'), { recursive: true });
    writeFileSync(join(root, '.codegraph', 'codegraph.db'), 'x');
  }
  const table = join(stubDir, 'table.json');
  writeFileSync(table, JSON.stringify(stubTable(c)));
  /** @type {NodeJS.ProcessEnv} */
  const env = {};
  for (const [k, v] of Object.entries(process.env)) if (!/^path$/i.test(k) && k !== 'CODEGRAPH_DIR') env[k] = v;
  env.PATH = stubDir;
  env.NXY_GOLDEN_LOCATE_TABLE = table;
  env.NXY_CODEGRAPH_PATH = join(stubDir, `codegraph${process.platform === 'win32' ? '.exe' : ''}`);
  const codegraph = c.codegraph === true;
  const res = c.mode
    ? relationsWithoutModel(root, c.mode, c.symbol, { codegraph, env })
    : searchWithoutModel(root, c.question, { codegraph, rebuild: Boolean(c.rebuild), env });
  return {
    text: res.text.replace(/_\d+ ms/g, '_<ms> ms'),
    degraded: res.degraded,
    terms: res.terms,
    hits: res.hits,
    codegraph: res.codegraph,
  };
}

/** @returns {Record<string, any[]>} */
export function generate(withReports = true) {
  /** @type {any} */
  const c = loadCorpus('locate');
  const terms = c.terms.map((/** @type {string} */ q, /** @type {number} */ i) => ({
    name: `terms:${idx(i)}:${JSON.stringify(q).slice(0, 60)}`,
    input: { question: q },
    output: queryTerms(q),
  }));
  /** @type {any[]} */
  let report = [];
  if (withReports) {
    const stubDir = buildStubs();
    try {
      report = c.reports.map((/** @type {any} */ x, /** @type {number} */ i) => {
        const root = realpathSync(mkdtempSync(join(tmpdir(), 'nxy-golden-locate-root-')));
        try {
          const tree = x.tree ?? c.tree;
          const input = {
            mode: x.mode ?? 'search',
            question: x.question ?? null,
            symbol: x.symbol ?? null,
            codegraph: x.codegraph === true,
            index: x.index === true,
            rebuild: x.rebuild === true,
            tree,
            rg: x.rg ?? {},
            cg: x.cg ?? {},
          };
          return { name: `report:${idx(i)}:${x.name}`, input, output: runCase(stubDir, root, tree, x) };
        } finally {
          rmSync(root, { recursive: true, force: true });
        }
      });
    } finally {
      rmSync(stubDir, { recursive: true, force: true });
    }
  }
  return { terms, report };
}

/** @returns {string[]} differences, empty when up to date */
export function check() {
  const reports = haveGo(); // without Go the stubs cannot be built: the terms are still checked
  const sets = generate(reports);
  const diffs = [];
  for (const [set, cases] of Object.entries(sets)) {
    if (set === 'report' && !reports) continue;
    diffs.push(...diffArea(AREA, set, cases, true));
  }
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
