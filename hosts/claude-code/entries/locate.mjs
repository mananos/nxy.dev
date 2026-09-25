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
 *   locate.mjs --status          which engines this repo actually has
 *   locate.mjs --rebuild "<q>"   force a full repo-map rebuild first
 */
import { appendJsonl } from '../../../core/jsonl.mjs';
import { loadConfig } from '../../../core/config.mjs';
import { parseArgs } from '../../../core/format.mjs';
import { nxyRuntimeDir } from '../../../core/paths.mjs';
import { buildRepoMap } from '../../../core/index/repomap.mjs';
import { describeEngines, searchWithoutModel } from '../../../core/scout/search.mjs';
import { join } from 'node:path';

const { opts, positional } = parseArgs(process.argv.slice(2));
const cwd = typeof opts.cwd === 'string' ? opts.cwd : process.cwd();
const cfg = loadConfig(cwd);
const question = positional.join(' ').trim();

if (opts.status) {
  console.log(describeEngines(cwd));
  process.exit(0);
}

if (opts.index) {
  const r = buildRepoMap(cwd, { force: true });
  console.log(`repo map: ${r.symbols} symbols from ${r.files} files in ${r.ms} ms -> ${r.map}`);
  process.exit(0);
}

if (!question) {
  console.log('Usage: locate.mjs "<question>"  |  --status  |  --index');
  process.exit(0);
}

const scout = cfg.scout || {};
const res = searchWithoutModel(cwd, question, {
  codegraph: scout.codegraph !== false,
  rebuild: Boolean(opts.rebuild),
  env: process.env,
});

console.log(res.text);

// One row per locate: terms, what degraded, how long. This is what tells us later whether
// codegraph is earning its place and whether the scout is missing — without anyone running an
// experiment on purpose.
try {
  appendJsonl(join(nxyRuntimeDir(cwd), 'metrics', 'locate.jsonl'), {
    ts: Date.now(),
    q: question.slice(0, 200),
    terms: res.terms,
    degraded: res.degraded,
    ms: res.ms,
    chars: res.text.length,
  });
} catch {
  // the ledger is telemetry for us, never a reason to fail the search
}
