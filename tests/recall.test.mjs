// @ts-check
/**
 * Recall by prompt, the memory graph and the librarian's plumbing.
 *
 * What has to hold: relevance is decided by files and keywords, never by loose words or a raw score
 * (so it does not drift with corpus size); a pointer is shown once per session; superseded memories stop being
 * pointed at; edges that are judgements travel with an export and edges that are telemetry do
 * not; and nothing private leaks through an edge.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { deleteMemory, getMemory, openStore, saveMemory } from '../core/memory/store.mjs';
import {
  addEdge, bumpCoUse, deriveEdges, edgesOf, extractFiles, extractLinks, memoriesForFiles, neighbors, sharedEdges,
  supersededIds,
} from '../core/memory/graph.mjs';
import { fileMentions, formatPointers, keywordHit, recall } from '../core/memory/recall.mjs';
import { librarianRoute, linkCandidates, linkRequest } from '../core/memory/librarian.mjs';
import { exportProject, fromMarkdown, importProject } from '../core/memory/exchange.mjs';
import { contentTerms } from '../core/text.mjs';
import { conventionSummary, noteChatOffers, noteConventionOffers, noteConventionSave, noteFetch, noteFile, notePointers, readSession, recallSummary } from '../hosts/claude-code/recall-state.mjs';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const HOOK = join(ROOT, 'hosts', 'claude-code', 'hooks', 'userpromptsubmit.mjs');
const MEM = join(ROOT, 'hosts', 'claude-code', 'entries', 'mem.mjs');
const PROJECT = 'github.com/mananos/nxy.dev';

/** A repo with a couple of real files, and a store in a temp dir. */
function world() {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-recall-'));
  const repo = join(dir, 'repo');
  for (const d of ['.git', 'core', 'api/auth']) mkdirSync(join(repo, d), { recursive: true });
  writeFileSync(join(repo, '.git', 'HEAD'), 'ref: refs/heads/main\n');
  writeFileSync(join(repo, '.git', 'config'), '[remote "origin"]\n\turl = git@github.com:mananos/nxy.dev.git\n');
  writeFileSync(join(repo, 'core', 'gate.mjs'), '');
  writeFileSync(join(repo, 'api', 'auth', 'login.ts'), '');
  const home = join(dir, 'home');
  const db = openStore({ path: join(home, 'memory', 'memory.db') });
  /** @param {Partial<import('../core/memory/store.mjs').Memory> & {title: string, body: string}} m */
  const save = (m) => {
    const { id } = saveMemory(db, { scope: 'project', project: PROJECT, type: 'decision', ...m });
    const mem = getMemory(db, id);
    if (mem) deriveEdges(db, mem, repo);
    return id;
  };
  return { dir, repo, home, db, save };
}

test('graph: files that exist, [[links]], and nothing that only looks like a path', () => {
  const { repo } = world();
  const text = 'Ver `core/gate.mjs:42` y api/auth/login.ts. No existe: core/nope.mjs. Prosa: y/o. Fuera: ../x.js';
  assert.deepEqual(extractFiles(text, repo).sort(), ['api/auth/login.ts', 'core/gate.mjs']);
  assert.deepEqual(extractFiles(text, null), [], 'without a repo nothing can be verified, so nothing is linked');
  assert.deepEqual(extractLinks('ver [[project-a-1]] y [[project-b-2]] y [[project-a-1]]'), ['project-a-1', 'project-b-2']);
  assert.deepEqual(fileMentions('mirá src/a.ts:12 y `core/gate.mjs`, no gate.mjs'), ['src/a.ts', 'core/gate.mjs'],
    'a bare name is not a path: too ambiguous to match on');
});

test('graph: neighbours by strength, supersedes hides, delete cascades, co-use needs two', () => {
  const { db, save } = world();
  const a = save({ title: 'Gate por contexto', body: 'core/gate.mjs decide por contexto' });
  const b = save({ title: 'Otra nota del gate', body: 'También toca core/gate.mjs' });
  const c = save({ title: 'Nota vieja', body: 'umbral 50k' });
  const d = save({ title: 'Conflicto', body: 'el gate mide el cambio' });
  const e = save({ title: 'Usada junto', body: 'x' });

  assert.equal(memoriesForFiles(db, ['core/gate.mjs']).size, 2);
  addEdge(db, a, c, 'supersedes', { by: 'user' });
  addEdge(db, a, d, 'conflicts_with', { by: 'librarian' });
  bumpCoUse(db, a, e);
  assert.deepEqual(neighbors(db, a).map((n) => `${n.id === d ? 'd' : n.id === c ? 'c' : n.id === b ? 'b' : 'e'}:${n.kind}`),
    ['d:conflicts_with', 'c:supersedes', 'b:file'], 'conflicts first; a single co-use is coincidence, not an edge');
  bumpCoUse(db, e, a);
  assert.ok(neighbors(db, a).some((n) => n.id === e && n.kind === 'co_use'), 'seen twice, it counts');

  assert.ok(supersededIds(db).has(c));
  assert.equal(addEdge(db, a, a, 'related'), false, 'no self-edges');
  assert.equal(addEdge(db, a, b, 'nonsense'), false, 'no unknown kinds');
  deleteMemory(db, a);
  assert.equal(edgesOf(db, c).length, 0, 'deleting a memory takes its edges with it');
});

test('keywordHit: whole word of the keywords field only; accents folded; title and body never count', () => {
  const mem = { title: 'El gate decide por contexto', keywords: 'umbral, Decisión threshold', body: 'Mide el contexto del principal.' };
  assert.match(keywordHit('¿cuál es el threshold?', mem) || '', /keyword "threshold"/);
  assert.match(keywordHit('la decision de ayer', mem) || '', /keyword "decision"/, 'accents folded');
  assert.equal(keywordHit('por qué el gate mide contexto del principal', mem), null, 'title and body words give nothing');
  assert.equal(keywordHit('los umbrales', mem), null, 'a partial word is not a hit');
  assert.equal(keywordHit('umbral', { keywords: '' }), null);
});

test('recall: files first, then words; handoffs, superseded and already-pointed are skipped', () => {
  const { db, save } = world();
  const login = save({ title: 'Login usa JWT', body: 'Tokens firmados en api/auth/login.ts' });
  const gate = save({ title: 'Gate por contexto', body: 'mide contexto', keywords: 'umbral' });
  const old = save({ title: 'Umbral viejo del gate', body: 'umbral 50k', keywords: 'umbral' });
  addEdge(db, gate, old, 'supersedes');
  save({ title: 'handoff: main', body: 'route: inline\n## Next\n- umbral', type: 'handoff' });
  const related = save({ title: 'Refresh tokens', body: 'rotación' });
  addEdge(db, login, related, 'related');

  const byFile = recall(db, 'agregá un test', { project: PROJECT, files: ['api/auth/login.ts'] });
  assert.deepEqual(byFile.map((p) => p.mem.id), [login], 'the session edits it: pointed at with no shared word');
  assert.equal(byFile[0].why, 'file api/auth/login.ts');
  assert.deepEqual(byFile[0].related.map((r) => r.id), [related], 'and its judged neighbour rides along');

  const byWords = recall(db, 'cuál es el umbral', { project: PROJECT });
  assert.deepEqual(byWords.map((p) => p.mem.id), [gate], 'the superseded note and the handoff never get a pointer');
  assert.deepEqual(recall(db, 'cuál es el umbral', { project: PROJECT, exclude: [gate] }), [], 'shown once per session');
  assert.deepEqual(recall(db, 'other project', { project: 'github.com/x/y' }), []);
  save({ title: 'Panel antes de aprobar', body: 'nxy panel before approval' });
  assert.deepEqual(recall(db, 'Gate por contexto mide contexto', { project: PROJECT }), [], 'two shared words but no keyword: nothing');

  const text = formatPointers(byFile, 'mem get', 'assisted');
  assert.equal(text.split('\n').length, 2, 'assisted: a header and one line per pointer, no body');
  assert.match(text, /mem get <id>/);
  assert.match(formatPointers(byFile, 'mem get', 'proactive'), /Tokens firmados/, 'proactive carries the body');
});

test('recall: an old keyworded memory still gets its pointer among more than 500 newer ones', () => {
  const { db, save } = world();
  const old = save({ title: 'Regla vieja', body: 'x', keywords: 'zafiro' });
  db.prepare('UPDATE memories SET updated = ? WHERE id = ?').run('2000-01-01T00:00:00.000Z', old);
  for (let i = 0; i < 520; i++) save({ title: `Nota ${i}`, body: `cuerpo ${i}` });
  assert.deepEqual(recall(db, 'qué pasa con zafiro', { project: PROJECT }).map((p) => p.mem.id), [old]);
});

test('librarian: code preselects candidates, the role table picks who judges', () => {
  const { db, save } = world();
  const a = save({ title: 'Gate por contexto', body: 'el umbral mide contexto', keywords: 'umbral' });
  const b = save({ title: 'Umbral en 100k', body: 'default del umbral', keywords: 'umbral' });
  save({ title: 'CSS del header', body: 'colores' });
  const mem = getMemory(db, b);
  if (!mem) throw new Error('missing');
  assert.deepEqual(linkCandidates(db, mem, { project: PROJECT }).map((m) => m.id), [a], 'only what shares something');
  addEdge(db, b, a, 'related', { by: 'librarian' });
  assert.deepEqual(linkCandidates(db, mem, { project: PROJECT }), [], 'already judged: not asked again');

  assert.deepEqual(librarianRoute(/** @type {any} */ ({ roles: { librarian: { model: 'haiku', provider: 'claude' } } })), { kind: 'subagent', model: 'haiku' });
  assert.deepEqual(librarianRoute(/** @type {any} */ ({ roles: { librarian: { model: 'x', provider: 'deepseek' } } })), { kind: 'unsupported', provider: 'deepseek' },
    'the seam for an external model: declared, not implemented');
  assert.deepEqual(librarianRoute(/** @type {any} */ ({ roles: {} })), { kind: 'off' });
  assert.equal(linkRequest('m1', ['a', 'b']), 'Relate memory m1 to these candidates: a b');
});

test('exchange: judged edges travel when both ends do; files re-derive; co-use and private never leave', () => {
  const src = world();
  const a = src.save({ title: 'Gate por contexto', body: 'ver core/gate.mjs' });
  const b = src.save({ title: 'Umbral 100k', body: 'default' });
  const secret = src.save({ title: 'Mi atajo privado', body: 'x', private: true });
  addEdge(src.db, b, a, 'supersedes', { by: 'user' });
  addEdge(src.db, a, secret, 'related', { by: 'librarian' });
  bumpCoUse(src.db, a, b);
  bumpCoUse(src.db, a, b);
  exportProject(src.db, src.repo, PROJECT);
  const md = readFileSync(join(src.repo, '.nxy', 'memory', `${b}.md`), 'utf8');
  assert.match(md, new RegExp(`^supersedes: ${a}$`, 'm'));
  const mdA = readFileSync(join(src.repo, '.nxy', 'memory', `${a}.md`), 'utf8');
  assert.ok(!mdA.includes(secret), 'an edge to a private memory would leak its id and title slug');
  assert.ok(!/co_use|file:/.test(mdA), 'telemetry and derivable edges stay out of the file');

  const dst = world();
  writeFileSync(join(dst.repo, 'core', 'gate.mjs'), '');
  mkdirSync(join(dst.repo, '.nxy', 'memory'), { recursive: true });
  for (const id of [a, b]) writeFileSync(join(dst.repo, '.nxy', 'memory', `${id}.md`), readFileSync(join(src.repo, '.nxy', 'memory', `${id}.md`)));
  importProject(dst.db, dst.repo, PROJECT);
  assert.ok(supersededIds(dst.db).has(a), 'the verdict arrived: the teammate does not pay for it again');
  assert.ok(edgesOf(dst.db, a).some((e) => e.kind === 'file' && e.dst === 'file:core/gate.mjs'), 'files re-derived in the importer checkout');
  assert.ok(!edgesOf(dst.db, a).some((e) => e.kind === 'co_use'));
  importProject(dst.db, dst.repo, PROJECT);
  assert.equal(edgesOf(dst.db, b).length, 1, 're-import is idempotent for edges too');
  assert.equal(sharedEdges(dst.db, new Set([a])).size, 0, 'an edge needs both ends in the set');

  // Regression: the list was once split on the letter "s", truncating ids that end in it.
  const parsed = fromMarkdown('---\nid: x\ntitle: t\nrelated: "project-ideas project-sss"\n---\nbody');
  assert.deepEqual(parsed?.edges.related, ['project-ideas', 'project-sss']);
});

test('recall state: files per session, pointers once, fetches make co-use and a precision number', () => {
  const { repo } = world();
  noteFile(repo, 's1', join(repo, 'api', 'auth', 'login.ts'));
  noteFile(repo, 's1', 'core/gate.mjs');
  noteFile(repo, 's1', join(repo, '..', 'outside.ts'));
  assert.deepEqual(readSession(repo, 's1').files, ['api/auth/login.ts', 'core/gate.mjs'], 'repo-relative, outside ignored');
  assert.deepEqual(readSession(repo, 'other').files, []);

  const t0 = Date.now();
  assert.deepEqual(noteFetch(repo, 'a', t0), []);
  assert.deepEqual(noteFetch(repo, 'b', t0 + 60_000), ['a'], 'fetched close together');
  assert.deepEqual(noteFetch(repo, 'c', t0 + 5 * 3_600_000), [], 'hours apart is not "together"');
  assert.deepEqual(noteFetch(repo, 'd', t0 + 5 * 3_600_000 + 1000, 'link'), [], 'the librarian relating is not co-use');
  assert.deepEqual(noteFetch(repo, 'e', t0 + 5 * 3_600_000 + 2000), ['c'], 'and its reads do not pair with later ones either');
});

test('recall summary: followed pointers, misses, and librarian bookkeeping kept out', () => {
  const { repo } = world();
  const now = Date.now();
  notePointers(repo, 's1', ['a', 'b'], 120);
  noteFetch(repo, 'a', now + 1000);                // pointed, then loaded: followed
  noteFetch(repo, 'x', now + 2000);                // loaded, never pointed: miss
  noteFetch(repo, 'y', now + 3000, 'search');      // found by the librarian: miss the hook did not see
  noteFetch(repo, 'z', now + 4000, 'link');        // librarian relating a new memory: neither
  assert.deepEqual(recallSummary(repo, undefined, now + 5000), { pointers: 2, followed: 1, tokens: 30, misses: 2, missesBySearch: 1, related: 0, relatedTasks: 0 });
});

test('conventionSummary: distinct offers by source, repeated checkpoints do not double count, saves counted', () => {
  const { repo } = world();
  noteConventionOffers(repo, ['Validation lives in services', 'Use records'], 'plan');
  noteConventionOffers(repo, ['validation lives in services', 'Use records'], 'plan');
  noteChatOffers(repo, ['Never mock the db']);
  noteConventionSave(repo, 'Use records');
  assert.deepEqual(conventionSummary(repo), { offered: 3, fromPlans: 2, fromChat: 1, saved: 1 });
  assert.deepEqual(conventionSummary(repo, 1000, Date.now() + 5000), { offered: 0, fromPlans: 0, fromChat: 0, saved: 0 });
});

test('userpromptsubmit hook end to end: pointers once, silence otherwise', () => {
  const { dir, repo, home, save, db } = world();
  save({ title: 'Gate por contexto', body: 'el gate mide el contexto', keywords: 'umbral' });
  save({ title: 'nxy panel before approval', body: 'the panel shows the plan before approval' });
  db.close();
  const run = (prompt, session = 's1') => execFileSync(process.execPath, ['--disable-warning=ExperimentalWarning', HOOK], {
    input: JSON.stringify({ session_id: session, cwd: repo, prompt }), encoding: 'utf8',
    env: { ...process.env, NXY_HOME: home, CLAUDE_PROJECT_DIR: repo },
  });
  const out = JSON.parse(run('cuál es el umbral')).hookSpecificOutput;
  assert.equal(out.hookEventName, 'UserPromptSubmit');
  assert.match(out.additionalContext, /Gate por contexto \(keyword "umbral"\)/);
  assert.equal(run('cuál es el umbral').trim(), '', 'already pointed at in this session');
  assert.notEqual(run('cuál es el umbral', 's2').trim(), '', 'a new session gets it again');
  assert.equal(run('arreglemos el css').trim(), '', 'nothing relevant → zero tokens');
  assert.equal(run('nxy panel before approval', 's5').trim(), '', 'title/body words alone stay silent');
  assert.equal(run('/nxy:mem search umbral', 's3').trim(), '', 'slash commands are instructions, not questions');
  assert.equal(recallSummary(repo).pointers, 2);

  mkdirSync(join(repo, '.nxy'), { recursive: true });
  writeFileSync(join(repo, '.nxy', 'config.json'), JSON.stringify({ memory: { mode: 'manual' } }));
  assert.equal(run('cuál es el umbral', 's4').trim(), '', 'manual: memory only when asked');
  assert.equal(execFileSync(process.execPath, [HOOK], { input: 'garbage', encoding: 'utf8', env: { ...process.env, NXY_HOME: join(dir, 'h2') } }).trim(), '');
});

test('mem CLI: save derives files and dictates the librarian; link, index, get', () => {
  const { repo, home } = world();
  const mem = (...args) => execFileSync(process.execPath, ['--disable-warning=ExperimentalWarning', MEM, ...args, '--cwd', repo], {
    encoding: 'utf8', env: { ...process.env, NXY_HOME: home },
  });
  const first = mem('save', 'Gate por contexto', '--body', 'ver core/gate.mjs', '--keywords', 'umbral');
  assert.match(first, /files: core\/gate\.mjs/);
  assert.ok(!/librarian/.test(first), 'nothing to relate yet: no dispatch');
  const idA = /saved: (\S+)/.exec(first)?.[1] || '';

  const second = mem('save', 'Umbral en 100k', '--body', 'default del umbral', '--keywords', 'umbral', '--supersedes', idA);
  const idB = /saved: (\S+)/.exec(second)?.[1] || '';
  assert.match(second, new RegExp(`supersedes: ${idA}`));
  assert.ok(!second.includes('nxy:librarian'), 'the superseded one is already judged');

  const third = mem('save', 'Umbral por repo', '--body', 'cada repo puede cambiar el umbral', '--keywords', 'umbral');
  assert.match(third, new RegExp(`next: dispatch the nxy:librarian subagent with: "Relate memory \\S+ to these candidates: .*${idB}`));

  assert.match(mem('link', idB, idA, '--kind', 'nope'), /Usage/);
  assert.match(mem('link', idB, 'ghost', '--kind', 'related'), /no memory with id ghost/);
  const idC = /saved: (\S+)/.exec(third)?.[1] || '';
  assert.match(mem('link', idC, idB, '--kind', 'related', '--by', 'librarian'), /linked/);
  assert.equal(mem('index').trim().split('\n').length, 3, 'one line per memory');
  const got = mem('get', idA);
  assert.match(got, /file: core\/gate\.mjs/);
  assert.match(got, new RegExp(`superseded by: ${idB}`));
  assert.match(mem('unlink', idC, idB, '--kind', 'related'), /removed/);
  assert.ok(!/conventions 30d/.test(mem('status')), 'nothing offered or saved yet');
  mem('save', 'Records over classes', '--body', 'prefer records', '--type', 'convention');
  assert.match(mem('status'), /conventions 30d: 0 offered \(0 from plans, 0 from the chat\), 1 saved/);
});
