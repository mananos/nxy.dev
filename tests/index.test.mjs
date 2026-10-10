// @ts-check
/**
 * Repo map and model-free search: the floor the scout stands on. What these pin down is that a
 * script can answer "where is X" without a model, and that every engine declares it when it
 * cannot — a silent degradation would show up as a wrong `path:line`, which is worse than none.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdirSync, mkdtempSync, writeFileSync, utimesSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { extractSymbols, indexedExtensions, rulesFor } from '../core/index/languages.mjs';
import { buildRepoMap, listSourceFiles, queryRepoMap, repoMapInfo } from '../core/index/repomap.mjs';
import { queryTerms, searchWithoutModel } from '../core/scout/search.mjs';
import { explore, findCodegraph, knownCodegraphLocations, resolveLauncher } from '../core/scout/codegraph.mjs';

/** A throwaway repo. NXY_HOME is irrelevant here: the index lives under the repo's own .nxy/local. */
function repo() {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-idx-'));
  const put = (rel, text) => {
    const abs = join(dir, rel);
    mkdirSync(join(abs, '..'), { recursive: true });
    writeFileSync(abs, text, 'utf8');
    return abs;
  };
  return { dir, put };
}

test('languages: symbols per language, and control keywords are never symbols', () => {
  const ts = [
    'export function decide(command, ctx) {',
    '  if (ctx) {',
    '    return 1;',
    '  }',
    '}',
    'export class Engine {',
    '  resolve(a, b) {',
    '    for (const x of a) {',
    '      break;',
    '    }',
    '  }',
    '}',
    'export const run = async (x) => x;',
    'export interface Opts { a: string }',
    'export type Verdict = "allow" | "deny";',
  ].join('\n');
  const got = extractSymbols(ts, /** @type {any} */ (rulesFor('.ts'))).map((s) => `${s.kind}:${s.name}`);
  assert.deepEqual(got, ['function:decide', 'class:Engine', 'method:resolve', 'const:run', 'interface:Opts', 'type:Verdict']);
  assert.ok(!got.some((g) => /:(if|for|while|return)$/.test(g)), `control keywords leaked: ${got}`);

  const py = [
    'MAX_RETRIES = 3',
    'class UserService:',
    '    def get_user(self, uid):',
    '        if uid:',
    '            return None',
    '    async def save(self, u):',
    '        pass',
    '@app.route("/users/<id>")',
    'def handle_user(id):',
    '    pass',
    '@router.get("/health")',
    'async def health():',
    '    pass',
  ].join('\n');
  const pySyms = extractSymbols(py, /** @type {any} */ (rulesFor('.py'))).map((s) => `${s.kind}:${s.name}`);
  assert.deepEqual(pySyms, [
    'const:MAX_RETRIES', 'class:UserService', 'def:get_user', 'def:save',
    'endpoint:/users/<id>', 'def:handle_user', 'endpoint:/health', 'def:health',
  ], 'python: classes, defs, async defs, route decorators and module constants');

  const java = [
    'public class BookingService {',
    '    @GetMapping("/bookings/{id}")',
    '    public Booking find(Long id) {',
    '        return null;',
    '    }',
    '}',
  ].join('\n');
  const javaSyms = extractSymbols(java, /** @type {any} */ (rulesFor('.java'))).map((s) => `${s.kind}:${s.name}`);
  assert.ok(javaSyms.includes('class:BookingService'), javaSyms.join(','));
  assert.ok(javaSyms.includes('endpoint:/bookings/{id}'), 'spring route is searchable by its URL');

  const scala = ['object Routes {', '  def cancelBooking(id: Long) = {', '  }', '}'].join('\n');
  const scalaSyms = extractSymbols(scala, /** @type {any} */ (rulesFor('.scala'))).map((s) => `${s.kind}:${s.name}`);
  assert.deepEqual(scalaSyms, ['object:Routes', 'def:cancelBooking']);

  assert.equal(rulesFor('.unknown'), null, 'an unknown extension is simply not indexed');
  assert.ok(indexedExtensions().includes('.py') && indexedExtensions().includes('.tsx'));
});

test('repo map: builds, skips what it should, queries by name and by regex', () => {
  const { dir, put } = repo();
  put('src/booking.ts', 'export function cancelBooking(id) {\n  return id;\n}\n');
  put('src/user.py', 'class UserService:\n    def get_user(self, uid):\n        pass\n');
  put('node_modules/pkg/index.js', 'export function shouldNotBeIndexed() {}\n');
  put('dist/bundle.js', 'export function alsoNotIndexed() {}\n');
  put('README.md', '# not source\n');
  put('.gitignore', 'secret/\n*.generated.ts\n');
  put('secret/hidden.ts', 'export function hiddenFn() {}\n');
  put('src/thing.generated.ts', 'export function generatedFn() {}\n');

  const built = buildRepoMap(dir);
  assert.equal(built.files, 2, `only real source is indexed, got ${built.files}`);
  // cancelBooking + UserService + get_user: every symbol of the two indexed files, and nothing else
  assert.equal(built.symbols, 3, `expected exactly the symbols of the two source files, got ${built.symbols}`);

  const byName = queryRepoMap(dir, 'cancelBooking');
  assert.equal(byName.rows.length, 1);
  assert.equal(byName.rows[0].path, 'src/booking.ts');
  assert.equal(byName.rows[0].line, 1, 'line numbers are 1-based and real');

  assert.equal(queryRepoMap(dir, 'shouldNotBeIndexed').rows.length, 0, 'node_modules never enters the index');
  assert.equal(queryRepoMap(dir, 'alsoNotIndexed').rows.length, 0, 'build output never enters the index');
  assert.equal(queryRepoMap(dir, 'hiddenFn').rows.length, 0, '.gitignore dir patterns are honoured');
  assert.equal(queryRepoMap(dir, 'generatedFn').rows.length, 0, '.gitignore glob patterns are honoured');

  const byRegex = queryRepoMap(dir, '/^get_/');
  assert.equal(byRegex.rows.length, 1);
  assert.equal(byRegex.rows[0].name, 'get_user');

  assert.deepEqual(queryRepoMap(dir, 'cancelBooking', { kind: 'class' }).rows, [], 'kind filter applies');
  const capped = queryRepoMap(dir, 'e', { limit: 1 });
  assert.equal(capped.rows.length, 1);
  assert.equal(capped.truncated, true, 'a capped query says so instead of silently dropping rows');
});

test('repo map: a rebuild reuses unchanged files and re-reads only what moved', () => {
  const { dir, put } = repo();
  put('a.ts', 'export function alpha() {}\n');
  const b = put('b.ts', 'export function beta() {}\n');

  const first = buildRepoMap(dir);
  assert.equal(first.parsed, 2);
  assert.equal(first.reused, 0);

  const second = buildRepoMap(dir);
  assert.equal(second.parsed, 0, 'nothing changed → nothing re-read');
  assert.equal(second.reused, 2);

  writeFileSync(b, 'export function betaRenamed() {}\n', 'utf8');
  const future = new Date(Date.now() + 5000);
  utimesSync(b, future, future); // make the change visible even on coarse filesystem clocks
  const third = buildRepoMap(dir);
  assert.equal(third.parsed, 1, 'only the touched file is re-read');
  assert.equal(third.reused, 1);
  assert.equal(queryRepoMap(dir, 'betaRenamed').rows.length, 1);
  assert.equal(queryRepoMap(dir, 'beta').rows.length, 1, 'the stale symbol is gone, the new one is there');

  const info = repoMapInfo(dir);
  assert.ok(info && info.files === 2 && info.symbols === 2, JSON.stringify(info));
  assert.equal(repoMapInfo(mkdtempSync(join(tmpdir(), 'nxy-empty-'))), null, 'no index → null, not a throw');

  assert.ok(listSourceFiles(dir).every((f) => !f.rel.includes('node_modules')));
});

test('queryTerms: identifiers win, stopwords in both languages are dropped', () => {
  const es = queryTerms('donde se decide si un comando pasa por rtk?');
  assert.ok(!es.terms.includes('donde') && !es.terms.includes('que'), `stopwords leaked: ${es.terms}`);
  assert.ok(es.terms.includes('decide') && es.terms.includes('rtk'));

  const id = queryTerms('where is resolveEngine called from');
  assert.deepEqual(id.identifiers, ['resolveEngine'], 'a camelCase identifier is kept verbatim');
  assert.equal(id.terms[0], 'resolveEngine', 'and it is searched first');
  assert.ok(id.terms.includes('resolve') && id.terms.includes('engine'), 'split spellings are searched too');

  assert.ok(queryTerms('user_id lookup').identifiers.includes('user_id'), 'snake_case counts as an identifier');
  assert.deepEqual(queryTerms('donde esta?').terms, [], 'a question with no content words yields nothing');
  assert.ok(queryTerms('a b c').terms.length === 0, 'terms shorter than 3 chars are noise');
});

test('search: finds the real definition and declares every degradation', () => {
  const { dir, put } = repo();
  put('src/filter/decide.ts', [
    'export function decide(command, ctx) {',
    '  return shouldFilter(command);',
    '}',
    'function shouldFilter(cmd) {',
    '  return cmd.startsWith("git");',
    '}',
  ].join('\n'));
  put('src/other.ts', 'export function unrelated() {}\n');

  const res = searchWithoutModel(dir, 'donde se decide si un comando pasa por el filtro', { env: { ...process.env } });
  assert.ok(res.text.includes('src/filter/decide.ts:1'), `expected the definition with its line:\n${res.text}`);
  assert.ok(res.text.includes('decide'), 'the symbol itself is reported');
  assert.ok(res.degraded.includes('codegraph:not-installed') || res.degraded.some((d) => d.startsWith('codegraph:')),
    `codegraph must declare why it did not run: ${res.degraded}`);
  assert.ok(res.ms < 15_000, 'the model-free pass is fast by construction');

  const none = searchWithoutModel(dir, 'donde esta?', { env: { ...process.env } });
  assert.deepEqual(none.degraded, ['no-terms'], 'no usable terms is reported, not guessed around');
  assert.ok(none.text.includes('Grep'), 'and it tells the scout what to do instead');

  const noCg = searchWithoutModel(dir, 'decide', { codegraph: false, env: { ...process.env } });
  assert.ok(noCg.text.includes('Disabled by configuration'), 'scout.codegraph=false is honoured');
});

test('codegraph: absent or unindexed never throws, always names the reason', () => {
  const { dir } = repo();
  const res = explore(dir, 'anything', { bin: undefined, env: { ...process.env, PATH: '', Path: '', NXY_CODEGRAPH_PATH: '' } });
  assert.equal(res.ok, false);
  assert.ok(['not-installed', 'no-index'].includes(res.reason), `unexpected reason: ${res.reason}`);
  assert.equal(res.text, '');

  const missing = explore(dir, 'anything', { bin: 'definitely-not-a-binary-xyz' });
  assert.equal(missing.ok, false, 'a bad binary is a declared failure, not an exception');

  assert.equal(findCodegraph({ PATH: '', Path: '', HOME: dir, USERPROFILE: dir, LOCALAPPDATA: dir }), null);
});

/** Fake official Windows bundle: <root>/current/{bin/codegraph.cmd, lib/dist/bin/codegraph.js}. */
function fakeBundle(root) {
  const js = join(root, 'current', 'lib', 'dist', 'bin', 'codegraph.js');
  mkdirSync(join(root, 'current', 'lib', 'dist', 'bin'), { recursive: true });
  mkdirSync(join(root, 'current', 'bin'), { recursive: true });
  writeFileSync(js, "if (process.argv[2] === '--version') console.log('9.9.9'); else console.log(JSON.stringify(process.argv.slice(2)));\n");
  const shim = join(root, 'current', 'bin', 'codegraph.cmd');
  writeFileSync(shim, '@"%~dp0..\\node.exe" --liftoff-only --disable-warning=ExperimentalWarning "%~dp0..\\lib\\dist\\bin\\codegraph.js" %*\r\n');
  return { js, shim };
}

test('codegraph: resolveLauncher reads the official .cmd shim and avoids a shell', () => {
  const tmp = mkdtempSync(join(tmpdir(), 'nxy-cg-'));
  const { js, shim } = fakeBundle(join(tmp, 'codegraph'));
  const l = resolveLauncher(shim, { platform: 'win32' });
  assert.equal(l?.file, process.execPath, 'node.exe absent next to the shim: falls back to the running node');
  assert.equal(l?.prefix[0], js);
  assert.deepEqual(resolveLauncher('/x/codegraph', { platform: 'linux' }), { file: '/x/codegraph', prefix: [] });
  assert.equal(resolveLauncher(join(tmp, 'missing.cmd'), { platform: 'win32' }), null);
});

test('codegraph: findCodegraph finds the official Windows install without PATH', () => {
  const tmp = mkdtempSync(join(tmpdir(), 'nxy-cg-'));
  fakeBundle(join(tmp, 'codegraph'));
  const base = { PATH: '', Path: '', HOME: tmp, USERPROFILE: tmp, LOCALAPPDATA: tmp };
  assert.equal(findCodegraph(base, 'win32')?.version, '9.9.9');

  const other = mkdtempSync(join(tmpdir(), 'nxy-cg-'));
  const elsewhere = mkdtempSync(join(tmpdir(), 'nxy-cg-'));
  fakeBundle(join(elsewhere, 'cg'));
  const viaDir = findCodegraph({ ...base, LOCALAPPDATA: other, CODEGRAPH_INSTALL_DIR: join(elsewhere, 'cg') }, 'win32');
  assert.equal(viaDir?.version, '9.9.9');
});

test('codegraph: a .cmd on PATH is found and an npm-style shim resolves', () => {
  const tmp = mkdtempSync(join(tmpdir(), 'nxy-cg-'));
  const bin = join(tmp, 'npm');
  const js = join(bin, 'node_modules', 'codegraph', 'dist', 'codegraph.js');
  mkdirSync(join(bin, 'node_modules', 'codegraph', 'dist'), { recursive: true });
  writeFileSync(js, "console.log('9.9.9');\n");
  writeFileSync(join(bin, 'codegraph.cmd'), '@ECHO off\r\nSET dp0=%~dp0\r\n"%_prog%"  "%dp0%\\node_modules\\codegraph\\dist\\codegraph.js" %*\r\n');
  assert.equal(resolveLauncher(join(bin, 'codegraph.cmd'), { platform: 'win32' })?.prefix[0], js);
  const found = findCodegraph({ PATH: bin, HOME: tmp, USERPROFILE: tmp, LOCALAPPDATA: tmp }, 'win32');
  assert.equal(found?.path, 'codegraph', 'a PATH hit is still reported as bare "codegraph"');
  assert.equal(found?.version, '9.9.9');
});

test('codegraph: explore passes shell metacharacters verbatim as one argument', () => {
  const tmp = mkdtempSync(join(tmpdir(), 'nxy-cg-'));
  const { shim } = fakeBundle(join(tmp, 'codegraph'));
  const cwd = join(tmp, 'work');
  mkdirSync(join(cwd, '.codegraph'), { recursive: true });
  const q = 'a & echo pwned | %PATH% ^ <x>';
  const res = explore(cwd, q, { bin: shim, platform: 'win32', env: { ...process.env, CODEGRAPH_DIR: '' } });
  assert.equal(res.ok, true, res.reason);
  assert.deepEqual(JSON.parse(res.text), ['explore', q]);
});

test('codegraph: knownCodegraphLocations covers the official unix installer', () => {
  const env = { HOME: '/h', CODEGRAPH_INSTALL_DIR: '/opt/cg', CODEGRAPH_BIN_DIR: '/opt/bin' };
  const locs = knownCodegraphLocations(env, 'linux').map((p) => p.replace(/\\/g, '/'));
  assert.ok(locs.includes('/opt/cg/current/bin/codegraph'));
  assert.ok(locs.includes('/opt/bin/codegraph'));
  assert.ok(knownCodegraphLocations({ HOME: '/h' }, 'linux').map((p) => p.replace(/\\/g, '/')).includes('/h/.codegraph/current/bin/codegraph'));
});
