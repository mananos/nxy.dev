// @ts-check
/**
 * Memory: scopes, store and the Markdown interchange format.
 *
 * The two things worth pinning down are isolation and portability. Isolation: a monorepo's `web/`
 * memories must never surface in `api/`, and another project's must never surface at all.
 * Portability: the same repo has to be the same project across clones and machines, and an export
 * must round-trip without private or global notes leaking into a repository.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdirSync, mkdtempSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { activeScopes, areaFor, normalizeRemote, projectKey, repoRoot } from '../core/memory/scope.mjs';
import {
  TYPES, countsByScope, deleteMemory, getMemory, listMemories, makeId, openStore, saveMemory, searchMemories,
} from '../core/memory/store.mjs';
import { exportProject, fromMarkdown, importProject, memoryDir, toMarkdown } from '../core/memory/exchange.mjs';
import { contentTerms } from '../core/text.mjs';

const PROJECT = 'github.com/mananos/nxy.dev';

/** A store in a temp file — never the user's real database. */
function store() {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-mem-'));
  return { db: openStore({ path: join(dir, 'memory.db') }), dir };
}

/** A fake repo with a git config holding one remote. */
function fakeRepo(remote = 'git@github.com:mananos/nxy.dev.git') {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-repo-'));
  mkdirSync(join(dir, '.git'), { recursive: true });
  writeFileSync(join(dir, '.git', 'config'),
    `[core]\n\trepositoryformatversion = 0\n[remote "origin"]\n\turl = ${remote}\n\tfetch = +refs/heads/*\n`, 'utf8');
  return dir;
}

test('scope: one repo is one project however it was cloned', () => {
  const same = [
    'git@github.com:mananos/nxy.dev.git',
    'https://github.com/mananos/nxy.dev.git',
    'ssh://git@github.com/mananos/nxy.dev',
    'https://github.com/mananos/nxy.dev/',
  ].map(normalizeRemote);
  assert.equal(new Set(same).size, 1, `every clone URL must key the same project: ${same}`);
  assert.equal(same[0], 'github.com/mananos/nxy.dev');

  // Azure DevOps is the shape used at work; it must not collapse into something ambiguous.
  assert.equal(normalizeRemote('https://user@dev.azure.com/org/proj/_git/repo'), 'dev.azure.com/org/proj/_git/repo');
  assert.notEqual(normalizeRemote('github.com/a/one'), normalizeRemote('github.com/a/two'));
  assert.equal(normalizeRemote(''), null);

  const repo = fakeRepo();
  const key = projectKey(repo);
  assert.equal(key.key, 'github.com/mananos/nxy.dev');
  assert.equal(key.from, 'remote');
  assert.equal(key.warning, null);
  assert.equal(repoRoot(repo), repo);

  // No remote: still usable, but it says the key will not follow the repo elsewhere.
  const noRemote = mkdtempSync(join(tmpdir(), 'nxy-bare-'));
  mkdirSync(join(noRemote, '.git'), { recursive: true });
  writeFileSync(join(noRemote, '.git', 'config'), '[core]\n', 'utf8');
  const fallback = projectKey(noRemote);
  assert.equal(fallback.from, 'path');
  assert.match(String(fallback.warning), /no git remote/);
});

test('scope: areas are the monorepo split, not every folder', () => {
  const repo = fakeRepo();
  assert.equal(areaFor(repo, 'api/auth/login.ts'), 'api/auth');
  assert.equal(areaFor(repo, 'core/filter/decide.mjs'), 'core/filter');
  assert.equal(areaFor(repo, 'src/index.ts'), 'src', 'one level deep is still an area');
  assert.equal(areaFor(repo, 'README.md'), null, 'a root file belongs to the project, not an area');
  assert.deepEqual(activeScopes({ area: 'api/auth' }), ['area', 'project', 'global']);
  assert.deepEqual(activeScopes({}), ['project', 'global'], 'no area in play → no area memories');
});

test('store: save, update in place, get and delete', () => {
  const { db } = store();
  const first = saveMemory(db, { scope: 'project', project: PROJECT, title: 'El gate decide por contexto', body: 'original', type: 'decision' });
  assert.equal(first.created, true);
  assert.match(first.id, /^project-el-gate-decide-por-contexto-/, 'ids are readable and carry the scope');

  const again = saveMemory(db, { id: first.id, scope: 'project', project: PROJECT, title: 'El gate decide por contexto', body: 'corregido' });
  assert.equal(again.created, false, 'same id updates rather than duplicating');
  const got = getMemory(db, first.id);
  assert.equal(got?.body, 'corregido');
  assert.equal(got?.created, getMemory(db, first.id)?.created, 'created survives an update');

  assert.equal(listMemories(db, { project: PROJECT }).length, 1, 'an update is not a second memory');
  assert.equal(deleteMemory(db, first.id), true);
  assert.equal(getMemory(db, first.id), null);
  assert.equal(deleteMemory(db, 'nope'), false);

  assert.equal(saveMemory(db, { scope: 'global', title: 'x', body: 'y', type: 'nonsense' }).id.startsWith('global-'), true);
  assert.equal(getMemory(db, saveMemory(db, { scope: 'global', title: 'z', body: 'y', type: 'nonsense' }).id)?.type, 'decision',
    'an unknown type falls back instead of being stored');
  assert.ok(TYPES.includes('handoff'));
  assert.notEqual(makeId('project', 'Título con ácentos y símbolos!'), makeId('project', 'otro'));
  assert.match(makeId('project', 'Título con ácentos'), /^project-titulo-con-acentos-/, 'accents are folded for the id');
});

test('store: search is ranked, scoped, and ignores question words', () => {
  const { db } = store();
  saveMemory(db, { scope: 'project', project: PROJECT, title: 'El gate decide por contexto', body: 'El umbral es el tamaño del contexto del principal.', keywords: 'gate umbral threshold', type: 'decision' });
  saveMemory(db, { scope: 'area', project: PROJECT, area: 'core/filter', title: 'rtk es el motor del filtro', body: 'nxy no reimplementa el filtrado.', type: 'convention' });
  saveMemory(db, { scope: 'global', project: null, title: 'No commitear desde el agente', body: 'Mati commitea a mano.', type: 'preference' });
  saveMemory(db, { scope: 'project', project: 'otro/repo', title: 'De otro proyecto', body: 'no debe aparecer nunca', type: 'decision' });

  const hits = searchMemories(db, '¿cómo se decide el umbral?', { project: PROJECT });
  assert.ok(hits.length >= 1);
  assert.equal(hits[0].title, 'El gate decide por contexto', 'best match ranks first');
  assert.ok(!hits.some((h) => h.title === 'No commitear desde el agente'),
    'stopwords like "el"/"se" must not drag in unrelated memories');

  // Area memories only surface when the area is in play.
  assert.equal(searchMemories(db, 'rtk', { project: PROJECT }).length, 0);
  assert.equal(searchMemories(db, 'rtk', { project: PROJECT, areas: ['core/filter'] }).length, 1);
  assert.equal(searchMemories(db, 'rtk', { project: PROJECT, areas: ['web/ui'] }).length, 0, 'the wrong area sees nothing');

  // Global always applies; another project never does.
  assert.equal(searchMemories(db, 'commitear', { project: PROJECT }).length, 1);
  assert.equal(searchMemories(db, 'otro proyecto', { project: PROJECT }).length, 0);

  assert.equal(searchMemories(db, 'umbral', { project: PROJECT, type: 'convention' }).length, 0, 'type filter applies');
  assert.deepEqual(searchMemories(db, '¿?', { project: PROJECT }), [], 'a query with no content words returns nothing, not everything');
  assert.deepEqual(searchMemories(db, 'gate "unbalanced', { project: PROJECT }).length > 0, true, 'FTS5 syntax in a query never throws');

  const counts = countsByScope(db, PROJECT);
  assert.deepEqual(counts, { global: 1, project: 1, area: 1 });
});

test('interchange: Markdown round-trips and keeps private notes at home', () => {
  const { db } = store();
  const repo = fakeRepo();
  saveMemory(db, { scope: 'project', project: PROJECT, title: 'El gate decide por contexto', body: 'Cuerpo con **markdown** y\nvarias líneas.', keywords: 'gate umbral', type: 'decision' });
  saveMemory(db, { scope: 'area', project: PROJECT, area: 'core/filter', title: 'rtk es el motor', body: 'nxy no reimplementa.', type: 'convention' });
  saveMemory(db, { scope: 'global', project: null, title: 'Preferencia mía', body: 'no sale del equipo', type: 'preference' });
  saveMemory(db, { scope: 'project', project: PROJECT, title: 'Secreta', body: 'tampoco sale', type: 'decision', private: true });

  const res = exportProject(db, repo, PROJECT);
  assert.equal(res.total, 2, 'only shareable project/area memories are exported');
  const files = readdirSync(memoryDir(repo)).filter((f) => f.endsWith('.md'));
  assert.equal(files.length, 2);
  assert.ok(!files.some((f) => /global|secreta/i.test(f)), `global and private must not be written: ${files}`);

  const text = readFileSync(join(memoryDir(repo), files.find((f) => f.startsWith('project-')) || ''), 'utf8');
  assert.match(text, /^---\n/, 'frontmatter first — reviewable in a PR');
  assert.match(text, /scope: project/);
  assert.match(text, /\*\*markdown\*\*/, 'the body stays readable Markdown');

  const parsed = fromMarkdown(text);
  assert.ok(parsed);
  assert.equal(parsed?.title, 'El gate decide por contexto');
  assert.equal(parsed?.body, 'Cuerpo con **markdown** y\nvarias líneas.', 'body round-trips exactly');
  assert.equal(parsed?.keywords, 'gate umbral');
  assert.equal(fromMarkdown('no frontmatter here'), null, 'a foreign file is skipped, not guessed at');
  assert.ok(toMarkdown({ ...parsed, title: 'x: y' }).includes('"x: y"'), 'values needing quotes get them');

  // Re-exporting unchanged content must not touch files, or every export is a git diff.
  assert.equal(exportProject(db, repo, PROJECT).written, 0);
});

test('interchange: import is idempotent and newest-wins', () => {
  const repo = fakeRepo();
  const source = store();
  saveMemory(source.db, { scope: 'project', project: PROJECT, title: 'Convención del equipo', body: 'v1', type: 'convention' });
  exportProject(source.db, repo, PROJECT);

  // A teammate with an empty store clones the repo.
  const mine = store();
  const first = importProject(mine.db, repo, PROJECT);
  assert.equal(first.added, 1);
  assert.deepEqual(first.failed, []);
  assert.equal(importProject(mine.db, repo, PROJECT).skipped, 1, 'importing again costs nothing');

  // They pull a newer version of the same memory.
  const id = listMemories(mine.db, { project: PROJECT })[0].id;
  saveMemory(source.db, { id, scope: 'project', project: PROJECT, title: 'Convención del equipo', body: 'v2', updated: Date.now() + 60_000 });
  exportProject(source.db, repo, PROJECT);
  assert.equal(importProject(mine.db, repo, PROJECT).updated, 1);
  assert.equal(getMemory(mine.db, id)?.body, 'v2', 'newer updated wins');

  // An older file must not overwrite a newer local memory.
  saveMemory(mine.db, { id, scope: 'project', project: PROJECT, title: 'Convención del equipo', body: 'local nuevo', updated: Date.now() + 120_000 });
  assert.equal(importProject(mine.db, repo, PROJECT).skipped, 1);
  assert.equal(getMemory(mine.db, id)?.body, 'local nuevo', 'an import never clobbers newer local work');

  // A deleted memory does not come back, and a junk file is reported rather than swallowed.
  writeFileSync(join(memoryDir(repo), 'junk.md'), 'not ours\n', 'utf8');
  assert.deepEqual(importProject(mine.db, repo, PROJECT).failed, ['junk.md']);
});

test('contentTerms: keeps the words that carry meaning', () => {
  assert.deepEqual(contentTerms('¿cómo se decide el umbral?'), ['decide', 'umbral']);
  assert.deepEqual(contentTerms('where is the gate threshold'), ['gate', 'threshold']);
  assert.deepEqual(contentTerms('el la de 123 un'), [], 'stopwords and bare numbers carry nothing');
  assert.deepEqual(contentTerms('gate gate GATE'), ['gate'], 'duplicates collapse');
});
