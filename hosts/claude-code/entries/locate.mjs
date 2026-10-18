#!/usr/bin/env node
// @ts-check
/**
 * The search the `scout` agent runs before it reads anything.
 *
 * This prints the result of every model-free engine (repo map, rg, codegraph) for a question.
 * It is meant to be run *inside the scout*, never in the main thread: the output is dense on
 * purpose, and its whole value is that it dies with the subagent while ~500 tokens of conclusion
 * come back.
 *
 * Usage:
 *   locate.mjs "<pregunta>"      run every engine and print the candidates
 *   locate.mjs --callers|--callees|--impact <symbol> [--json]   relations (codegraph, rg fallback)
 *   locate.mjs --status          which engines this repo actually has
 *   locate.mjs --rebuild "<q>"   force a full repo-map rebuild first
 */
import { appendJsonl } from '../../../core/jsonl.mjs';
import { loadConfig } from '../../../core/config.mjs';
import { parseArgs } from '../../../core/format.mjs';
import { nxyRuntimeDir } from '../../../core/paths.mjs';
import { buildRepoMap } from '../../../core/index/repomap.mjs';
import { describeEngines, relationsWithoutModel, searchWithoutModel } from '../../../core/scout/search.mjs';
import { join } from 'node:path';

const { opts, positional } = parseArgs(process.argv.slice(2));
const cwd = typeof opts.cwd === 'string' ? opts.cwd : process.cwd();
const cfg = loadConfig(cwd);
// `--json "q"` would be read as a flag with a value: give the word back to the question.
const question = [typeof opts.json === 'string' && opts.json !== 'true' ? opts.json : '', ...positional].join(' ').trim();

/** @type {{kind: 'callers'|'callees'|'impact', symbol: string}|null} */
let relation = null;
for (const kind of /** @type {const} */ (['callers', 'callees', 'impact'])) {
  const v = opts[kind];
  if (typeof v === 'string' && v && v !== 'true') {
    relation = { kind, symbol: v.trim() };
    break;
  }
}

if (opts.status) {
  console.log(describeEngines(cwd));
  process.exit(0);
}

if (opts.index) {
  const r = buildRepoMap(cwd, { force: true });
  console.log(`repo map: ${r.symbols} symbols from ${r.files} files in ${r.ms} ms -> ${r.map}`);
  process.exit(0);
}

if (!question && !relation) {
  if (opts.json) {
    console.log(JSON.stringify({ ok: false, reason: 'no question' }));
    process.exit(0);
  }
  console.log('Usage: locate.mjs "<question>"  |  --callers|--callees|--impact <symbol>  |  --status  |  --index');
  process.exit(0);
}

const scout = cfg.scout || {};
const res = relation
  ? relationsWithoutModel(cwd, relation.kind, relation.symbol, { codegraph: scout.codegraph !== false, env: process.env })
  : searchWithoutModel(cwd, question, {
      codegraph: scout.codegraph !== false,
      rebuild: Boolean(opts.rebuild),
      env: process.env,
    });

if (opts.json) {
  console.log(JSON.stringify({
    ok: true, mode: relation ? relation.kind : 'search', question, hits: res.hits, terms: res.terms, degraded: res.degraded, ms: res.ms, codegraph: res.codegraph,
  }));
} else {
  console.log(res.text);
}

// One row per locate: terms, what degraded, how long. This is what tells us later whether
// codegraph is earning its place and whether the scout is missing — without anyone running an
// experiment on purpose.
try {
  appendJsonl(join(nxyRuntimeDir(cwd), 'metrics', 'locate.jsonl'), {
    ts: Date.now(),
    mode: relation ? relation.kind : 'search',
    q: (relation ? relation.symbol : question).slice(0, 200),
    terms: res.terms,
    degraded: res.degraded,
    ms: res.ms,
    chars: res.text.length,
  });
} catch {
  // the ledger is telemetry for us, never a reason to fail the search
}
