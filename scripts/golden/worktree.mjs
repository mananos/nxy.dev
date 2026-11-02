// @ts-check
/**
 * Golden of the pure side of the 1.x worktrees (`core/featuresview.mjs`) that `internal/worktree` ports.
 * Node is the oracle.
 *   parse:   {porcelain} -> {worktrees}
 *   slug:    {text} -> {slug}
 *   issue:   {text} -> {ok, issue, error}
 *   names:   {input, mainPath} -> featureNames
 *   default: {originHead, mainBranch, branches} -> {branch: defaultBranchOf}
 *   bases:   {branches, default, worktrees} -> {bases: baseChoices, choices: branchChoices}
 *   add:     {branch, path, create, base} -> {argv} or {error}
 *   remove:  {path, force} -> {argv}
 *   command: {argv, shell} -> {text: commandText}
 *   shell:   {platform, env} -> {shell: shellOf}
 *   open:    {path, shell} -> {command: openCommand}
 *   row:     {now, wt} -> rowView
 *
 *   node scripts/golden/worktree.mjs          write testdata/golden/worktree/*.json
 *   node scripts/golden/worktree.mjs --check  fail when they differ
 */
import { fileURLToPath } from 'node:url';
import {
  addArgv, baseChoices, branchChoices, commandText, defaultBranchOf, featureNames, openCommand, parseIssue,
  parseWorktrees, removeArgv, rowView, shellOf, slugOf,
} from '../../core/featuresview.mjs';
import { diffArea, loadCorpus, staleSets, writeArea } from './lib.mjs';

export const AREA = 'worktree';

const idx = (/** @type {number} */ i) => String(i).padStart(3, '0');

/** @returns {Record<string, any[]>} */
export function generate() {
  /** @type {any} */
  const c = loadCorpus('worktree');

  const parse = c.parse.map((/** @type {string} */ porcelain, /** @type {number} */ i) => ({
    name: `parse:${idx(i)}`,
    input: { porcelain },
    output: { worktrees: parseWorktrees(porcelain) },
  }));

  const slug = c.slug.map((/** @type {string} */ text, /** @type {number} */ i) => ({
    name: `slug:${idx(i)}`,
    input: { text },
    output: { slug: slugOf(text) },
  }));

  const issue = c.issue.map((/** @type {string} */ text, /** @type {number} */ i) => ({
    name: `issue:${idx(i)}`,
    input: { text },
    output: parseIssue(text),
  }));

  const names = c.names.map((/** @type {any} */ n, /** @type {number} */ i) => ({
    name: `names:${idx(i)}`,
    input: n,
    output: featureNames(n.input, n.mainPath),
  }));

  const def = c.default.map((/** @type {any} */ d, /** @type {number} */ i) => ({
    name: `default:${idx(i)}`,
    input: d,
    output: { branch: defaultBranchOf(d) },
  }));

  const bases = c.bases.map((/** @type {any} */ b, /** @type {number} */ i) => ({
    name: `bases:${idx(i)}`,
    input: b,
    output: { bases: baseChoices(b.branches, b.default), choices: branchChoices(b.branches, b.worktrees) },
  }));

  const add = c.add.map((/** @type {any} */ a, /** @type {number} */ i) => {
    let output;
    try {
      output = { argv: addArgv(a.branch, a.path, a.create, a.base) };
    } catch (e) {
      output = { error: String(/** @type {any} */ (e)?.message || e) };
    }
    return { name: `add:${idx(i)}`, input: a, output };
  });

  const remove = c.remove.map((/** @type {any} */ r, /** @type {number} */ i) => ({
    name: `remove:${idx(i)}`,
    input: r,
    output: { argv: removeArgv(r.path, r.force) },
  }));

  const command = c.command.map((/** @type {any} */ m, /** @type {number} */ i) => ({
    name: `command:${idx(i)}`,
    input: m,
    output: { text: commandText(m.argv, m.shell) },
  }));

  const shell = c.shell.map((/** @type {any} */ s, /** @type {number} */ i) => ({
    name: `shell:${idx(i)}`,
    input: s,
    output: { shell: shellOf(s) },
  }));

  const open = c.open.map((/** @type {any} */ o, /** @type {number} */ i) => ({
    name: `open:${idx(i)}`,
    input: o,
    output: { command: openCommand(o.path, o.shell) },
  }));

  const row = c.row.map((/** @type {any} */ r, /** @type {number} */ i) => ({
    name: `row:${idx(i)}`,
    input: r,
    output: rowView(r.wt, { now: r.now }),
  }));

  return { parse, slug, issue, names, default: def, bases, add, remove, command, shell, open, row };
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
