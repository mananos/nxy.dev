// @ts-check
/**
 * Golden of the 1.x project key (`core/memory/scope.mjs`) that `platform.LegacyProjectKey` ports.
 * Node is the oracle. Each case builds a small tree under a temp root and asks for the key from
 * `cwd` (relative to the root); the root shows up as `@ROOT@` in file texts and in the answer.
 *   remote: {text} -> {normalized: normalizeRemote(text)}
 *   key:    {files: {relpath: text}, cwd} -> {key: projectKey(cwd).key}
 *
 *   node scripts/golden/legacykey.mjs          write testdata/golden/legacykey/*.json
 *   node scripts/golden/legacykey.mjs --check  fail when they differ
 */
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { normalizeRemote, projectKey } from '../../core/memory/scope.mjs';
import { diffArea, staleSets, writeArea } from './lib.mjs';

export const AREA = 'legacykey';

const idx = (/** @type {number} */ i) => String(i).padStart(3, '0');

const REMOTES = [
  'https://github.com/mananos/nxy.dev.git',
  'https://github.com/mananos/nxy.dev',
  'https://GitHub.com/Mananos/NXY.dev.GIT',
  'git@github.com:mananos/nxy.dev.git',
  'git@github.com:mananos/nxy.dev',
  'ssh://git@github.com/mananos/nxy.dev',
  'ssh://git@host.example:2222/team/app.git',
  'https://user:token@gitlab.com/group/sub/app.git',
  'https://github.com/mananos/nxy.dev/',
  'host.example:team/app',
  '  https://github.com/a/b.git  ',
  '',
];

const cfg = (/** @type {string[]} */ ...lines) => `${lines.join('\n')}\n`;
const remote = (/** @type {string} */ name, /** @type {string} */ url) => `[remote "${name}"]\n\turl = ${url}\n`;

const KEYS = [
  { files: { '.git/config': cfg('[core]', '\tbare = false') + remote('origin', 'https://github.com/Mananos/NXY.dev.git') }, cwd: '' },
  { files: { 'sub/dir/x.txt': 'x', '.git/config': remote('origin', 'git@github.com:mananos/nxy.dev.git') }, cwd: 'sub/dir' },
  { files: { '.git/config': remote('up', 'https://example.com/z/up.git') + remote('origin', 'https://example.com/z/origin.git') }, cwd: '' },
  { files: { '.git/config': remote('up', 'https://example.com/z/up.git') }, cwd: '' },
  { files: { '.git/config': remote('a', 'https://example.com/z/a.git') + remote('b', 'https://example.com/z/b.git') }, cwd: '' },
  { files: { '.git/config': remote('origin', 'ssh://git@host.example:2222/team/app.git') }, cwd: '' },
  { files: { '.git/config': remote('origin', 'https://user:tok@gitlab.com/g/s/app.git') }, cwd: '' },
  { files: { '.git/config': cfg('[core]', '\tbare = false') }, cwd: '' },
  { files: { 'Work/Mi Repo/.git/HEAD': 'ref: refs/heads/main\n', 'Work/Mi Repo/src/a.txt': 'a' }, cwd: 'Work/Mi Repo/src' },
  { files: { 'Plain/Dir/a.txt': 'a' }, cwd: 'Plain/Dir' },
  {
    files: {
      'main/.git/config': remote('origin', 'https://github.com/mananos/nxy.dev.git'),
      'main/.git/worktrees/wt/commondir': '../..\n',
      'main/.git/worktrees/wt/HEAD': 'ref: refs/heads/feat\n',
      'wt/.git': 'gitdir: @ROOT@/main/.git/worktrees/wt\n',
      'wt/src/a.txt': 'a',
    },
    cwd: 'wt/src',
  },
  {
    files: {
      'main/.git/config': remote('origin', 'https://github.com/mananos/nxy.dev.git'),
      'main/.git/worktrees/wt/commondir': '@ROOT@/main/.git\n',
      'wt/.git': 'gitdir: ../main/.git/worktrees/wt\n',
    },
    cwd: 'wt',
  },
  { files: { 'wt/.git': 'gitdir: @ROOT@/nowhere\n' }, cwd: 'wt' },
];

/** @param {string} root */
function slashLower(root) {
  return root.replace(/\\/g, '/').toLowerCase();
}

/** @returns {Record<string, any[]>} */
export function generate() {
  const remote = REMOTES.map((text, i) => ({
    name: `remote:${idx(i)}`,
    input: { text },
    output: { normalized: normalizeRemote(text) },
  }));

  const key = KEYS.map((k, i) => {
    const root = mkdtempSync(join(tmpdir(), 'nxy-legacykey-'));
    try {
      const rootText = root.replace(/\\/g, '/');
      for (const [rel, text] of Object.entries(k.files)) {
        const p = join(root, rel);
        mkdirSync(dirname(p), { recursive: true });
        writeFileSync(p, text.replaceAll('@ROOT@', rootText));
      }
      const cwd = k.cwd ? resolve(root, k.cwd) : root;
      const r = projectKey(cwd);
      const out = r.key.replaceAll(slashLower(root), '@ROOT@');
      return { name: `key:${idx(i)}`, input: k, output: { key: out } };
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  return { remote, key };
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
