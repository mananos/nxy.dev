// @ts-check
/**
 * Golden of the pure side of the 1.x multi-repo naming (`hosts/claude-code/repos.mjs`) that
 * `internal/repos` ports. Node is the oracle. Cases use absolute POSIX-looking paths with no
 * case-colliding names, so the result is the same on every OS.
 *   names: {roots} -> {names: repoNames}
 *   label: {roots, root, primary, abs} -> {label: repoLabel}
 *   of:    {roots, file} -> {index: position in roots of the repo the file belongs to, or null}
 *
 *   node scripts/golden/repos.mjs          write testdata/golden/repos/*.json
 *   node scripts/golden/repos.mjs --check  fail when they differ
 */
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { repoLabel, repoNames, repoOf } from '../../hosts/claude-code/repos.mjs';
import { diffArea, staleSets, writeArea } from './lib.mjs';

export const AREA = 'repos';

const idx = (/** @type {number} */ i) => String(i).padStart(3, '0');

const NAMES = [
  ['/work/api'],
  ['/work/api', '/work/web'],
  ['/work/api', '/work/api'],
  ['/clientX/api', '/clientY/api', '/work/web'],
  ['/a/b/api', '/c/d/api'],
  ['/a/x/api', '/b/y/api', '/c/z/api', '/c/z/web'],
  ['/work/Mi App', '/work/web-2'],
  ['/srv/dev/proyectos/api', '/srv/dev/otros/api'],
];

const LABELS = [
  { roots: ['/work/api', '/work/web'], root: '/work/api', primary: true, abs: '/work/api/src/Main.java' },
  { roots: ['/work/api', '/work/web'], root: '/work/web', primary: false, abs: '/work/web/src/app.ts' },
  { roots: ['/work/api', '/work/web'], root: '/work/web', primary: false, abs: '/work/web' },
  { roots: ['/work/api', '/work/web'], root: '/work/api', primary: true, abs: '/work/api' },
  { roots: ['/clientX/api', '/clientY/api'], root: '/clientY/api', primary: false, abs: '/clientY/api/a/b.go' },
  { roots: ['/clientX/api', '/clientY/api'], root: '/clientX/api', primary: true, abs: '/clientX/api/a/b.go' },
];

const OF = [
  { roots: ['/work/api', '/work/web'], file: '/work/api/src/Main.java' },
  { roots: ['/work/api', '/work/web'], file: '/work/web/src/app.ts' },
  { roots: ['/work/api', '/work/web'], file: '/work/api' },
  { roots: ['/work/api', '/work/web'], file: '/work/apix/a.js' },
  { roots: ['/work/api', '/work/web'], file: '/other/zzz/a.js' },
  { roots: ['/work', '/work/api'], file: '/work/api/a.go' },
  { roots: ['/work', '/work/api'], file: '/work/web/a.go' },
  { roots: ['/work/api', '/work/api/vendor/lib'], file: '/work/api/vendor/lib/x.go' },
];

/** @returns {Record<string, any[]>} */
export function generate() {
  const names = NAMES.map((roots, i) => {
    const m = repoNames(roots);
    return { name: `names:${idx(i)}`, input: { roots }, output: { names: Object.fromEntries(m) } };
  });

  const label = LABELS.map((l, i) => {
    const m = repoNames(l.roots);
    return {
      name: `label:${idx(i)}`,
      input: l,
      output: { label: repoLabel({ root: l.root, primary: l.primary }, l.abs, m) },
    };
  });

  const of = OF.map((o, i) => {
    const r = repoOf(o.roots[0], o.file, o.roots);
    const at = r ? o.roots.findIndex((x) => resolve(x) === resolve(r.root)) : -1;
    return { name: `of:${idx(i)}`, input: o, output: { index: at < 0 ? null : at } };
  });

  return { names, label, of };
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
