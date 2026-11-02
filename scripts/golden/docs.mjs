// @ts-check
/**
 * Golden of the 1.x docs rules (`core/docs.mjs`) that `internal/docs` ports. Node is the oracle.
 *   terms:   {paths}              -> {terms: docTerms}
 *   userdoc: {path, wikiDir}      -> {isUserDoc}
 *   globs:   {wikiDir}            -> {globs: userDocGlobs}
 *   pattern: {terms}              -> {pattern: termsPattern}
 *   wiki:    {texts, dirs}        -> {wikiDir: wikiDirFromCi (isDir = the dir is in `dirs`)}
 *   packet:  {o}                  -> {text: docsPacket}
 *   shown:   {docs}               -> {shown: the short list of docsQuestion}
 * Sets: terms, userdoc, globs, pattern, wiki, packet, shown.
 *
 *   node scripts/golden/docs.mjs          write testdata/golden/docs/*.json
 *   node scripts/golden/docs.mjs --check  fail when they differ
 */
import { fileURLToPath } from 'node:url';
import { docTerms, docsPacket, docsQuestion, isUserDoc, termsPattern, userDocGlobs, wikiDirFromCi } from '../../core/docs.mjs';
import { diffArea, loadCorpus, staleSets, writeArea } from './lib.mjs';

export const AREA = 'docs';

const idx = (/** @type {number} */ i) => String(i).padStart(3, '0');

/** @returns {Record<string, any[]>} */
export function generate() {
  /** @type {any} */
  const c = loadCorpus('docs');

  const terms = c.terms.map((/** @type {string[]} */ paths, /** @type {number} */ i) => ({
    name: `terms:${idx(i)}`,
    input: { paths },
    output: { terms: docTerms(paths) },
  }));

  const userdoc = c.userdoc.map((/** @type {[string, string|null]} */ [path, wikiDir], /** @type {number} */ i) => ({
    name: `userdoc:${idx(i)}`,
    input: { path, wikiDir },
    output: { isUserDoc: isUserDoc(path, wikiDir) },
  }));

  const globs = c.globs.map((/** @type {string|null} */ wikiDir, /** @type {number} */ i) => ({
    name: `globs:${idx(i)}`,
    input: { wikiDir },
    output: { globs: userDocGlobs(wikiDir) },
  }));

  const pattern = c.pattern.map((/** @type {string[]} */ t, /** @type {number} */ i) => ({
    name: `pattern:${idx(i)}`,
    input: { terms: t },
    output: { pattern: termsPattern(t) },
  }));

  const wiki = c.wiki.map((/** @type {{texts: string[], dirs: string[]}} */ w, /** @type {number} */ i) => ({
    name: `wiki:${idx(i)}`,
    input: { texts: w.texts, dirs: w.dirs },
    output: { wikiDir: wikiDirFromCi(w.texts, (/** @type {string} */ rel) => w.dirs.includes(rel)) },
  }));

  const packet = c.packet.map((/** @type {any} */ o, /** @type {number} */ i) => ({
    name: `packet:${idx(i)}`,
    input: { o },
    output: { text: docsPacket(o) },
  }));

  const shown = c.shown.map((/** @type {string[]} */ docs, /** @type {number} */ i) => ({
    name: `shown:${idx(i)}`,
    input: { docs },
    output: { shown: docsQuestion('abc123', docs).question.options[0].description },
  }));

  return { terms, userdoc, globs, pattern, wiki, packet, shown };
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
