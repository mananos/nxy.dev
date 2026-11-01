// @ts-check
/**
 * End-to-end golden of the 1.x `/nxy:stats`, `/nxy:trend` and statusline commands: each case
 * materializes a small world under a temp root, runs the real 1.x entry in a child node process
 * (clock and time zone injected) and records what it printed. The Go ports (`nxy stats`,
 * `nxy trend`, `nxy statusline`) build the same world from the same case and must print the same.
 *
 *   node scripts/golden/usage-cli.mjs          write testdata/golden/usage-cli/*.json
 *   node scripts/golden/usage-cli.mjs --check  fail when they differ
 *
 * Sets (testdata/golden/usage-cli/<set>.json, one case per line):
 *   texts       {name, output: {text}}: every file text a case references by name (`ref`), cwd already
 *               replaced by `@ROOT@/proj`. Fixture texts are the 1.x test fixtures; `gen/<n>` ones are
 *               built here from corpus.gen.
 *   stats       {name, input: <case>, output: {stdout, stderr, code}}
 *   trend       same
 *   statusline  {name, input: <case>, output: {steps: [{stdout, stderr, code}], state, input}}
 *
 * THE CASE (input) — how a consumer materializes it. `root` is a new temp dir (realpath'd; when
 * `rootSpace` is true its name has a space in it, so URIs and quoting are exercised):
 *   tz          IANA zone the command runs in (default UTC).
 *   now         ISO instant the command sees as Date.now() (default 2026-09-10T12:00:00Z).
 *   session     value of CLAUDE_CODE_SESSION_ID (absent: unset).
 *   args        stats/trend only: argv after the command. `@ROOT@` here is the plain root path.
 *   files       relative path (under root) -> spec. A key may contain `@SLUG@` = the Claude Code project
 *               slug of <root>/proj (every non-alphanumeric char of that path as `-`). A spec is a text
 *               name (string, a `texts` entry) or an object `{ref | text | json, lines?: [from, to], mtime?}`:
 *               ref = a `texts` entry (lines = keep those lines, each with its "\n"), text = literal,
 *               json = a value written with JSON.stringify; mtime = ISO file time (default: now).
 *               `@ROOT@` inside a text = the root as embedded in a JSON string (backslashes doubled on
 *               Windows); `@ROOTRAW@` = the plain path (for non-JSON texts such as a `.git` pointer).
 *   userConfig  object -> <root>/nxyhome/config.json (NXY_HOME is that dir).
 *   projectConfig object -> <root>/proj/.nxy/config.json (always carries metrics.projectsDir = <root>/projects).
 *               In objects (configs, rows, stdin) `@ROOT@` is replaced inside the JSON text, so the
 *               value ends up as the plain path.
 *   git         <root>/proj/.git/HEAD text (default "ref: refs/heads/main\n"); false: .git without HEAD.
 *   filterRows  lines of <root>/proj/.nxy/local/metrics/filter.jsonl: objects (JSON line) or strings (raw line).
 *   ledger      rtk history db at RTK_DB_PATH = <root>/rtk/history.db:
 *                 {rows: [{timestamp, project_path, input_tokens, output_tokens, saved_tokens}]}
 *                   table `commands(id INTEGER PRIMARY KEY AUTOINCREMENT, timestamp TEXT NOT NULL,
 *                   project_path TEXT, input_tokens INTEGER, output_tokens INTEGER, saved_tokens INTEGER)`;
 *                 {garbage: true}  a file that is not a database;  {noTable: true}  a db without `commands`.
 *               Absent: no file.
 *   rtk         a stand-in rtk (scripts/golden/rtkstub): the engine cache (<NXY_HOME>/cache/engine.json in
 *               the 1.x) points at it and `rtk gain ... --format json` answers with
 *               {gain: <value>} (that JSON, in <root>/gain.json = NXY_GOLDEN_RTK_GAIN), {gainText: "..."}
 *               (that text), or {gain: null} (NXY_GOLDEN_RTK_GAIN unset: exit 1). Absent: no rtk anywhere
 *               (PATH is an empty dir; the 1.x engine cache says engine off).
 *   steps       statusline only: [{stdin: string | object, write?: {rel: spec}, append?: {rel: spec}}] run in
 *               order, each after its write (create/replace) and append (add to the end) of files.
 * Environment of the command: HOME/USERPROFILE/APPDATA/LOCALAPPDATA/XDG_* inside root, PATH = an empty dir,
 * NXY_HOME, RTK_DB_PATH, TZ, no NXY_* switches; cwd = <root>/proj.
 *
 * OUTPUT. stdout/stderr go through `normRootText` (fixtures.mjs; Go: goldentest.NormRootText): the root,
 * raw or JSON-escaped, becomes `<root>` + the rest of the path with slashes, and the root's slug becomes
 * `<rootslug>`. Statusline: `state` = {<session id as the 1.x sanitizes it>: text of the session cache
 * file} after the last step (every `"offset":N` masked as `"offset":0`: it counts bytes of transcripts
 * that embed the root path, whose length differs per machine), `input` = text of the last-payload file (null when absent), both normalized.
 *
 * The generator refuses a stats case whose main transcripts have broad searches
 * (broadSearchesInTranscript): the Go port returns none until F3.3.
 */
import { spawnSync } from 'node:child_process';
import { appendFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, realpathSync, rmSync, utimesSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { broadSearchesInTranscript } from '../../hosts/claude-code/search-history.mjs';
import { diffArea, loadCorpus, ROOT, staleSets, writeArea } from './lib.mjs';
import { fixtureTexts, normRootText } from './fixtures.mjs';
import { haveGo } from './hook.mjs';

export const AREA = 'usage-cli';
const CLOCK = join(ROOT, 'scripts', 'golden', 'clock.mjs');
const ENTRIES = {
  stats: join(ROOT, 'hosts', 'claude-code', 'entries', 'stats.mjs'),
  trend: join(ROOT, 'hosts', 'claude-code', 'entries', 'trend.mjs'),
  statusline: join(ROOT, 'hosts', 'claude-code', 'statusline.mjs'),
};
const DEFAULT_NOW = '2026-09-10T12:00:00Z';
const FIXTURE_CWD = '/srv/dev/code/my.app';

// ---------------------------------------------------------------------------
// Texts
// ---------------------------------------------------------------------------

const jsonl = (/** @type {any[]} */ lines) => lines.map((l) => JSON.stringify(l)).join('\n') + '\n';

/**
 * A synthetic main transcript from the corpus shorthand:
 * prompts [[atMs, text]], calls [[atMs, model, input, w5, w1, read, output, [[tool, input]]?]].
 * @param {string} name
 * @param {any} g
 * @param {boolean} [sidechain]
 */
function genText(name, g, sidechain = false) {
  const t0 = Date.parse(g.start);
  const base = { isSidechain: sidechain, cwd: '@ROOT@/proj', version: '2.1.274', gitBranch: 'main' };
  /** @type {{at: number, line: any}[]} */
  const items = [];
  g.prompts.forEach((/** @type {[number, string]} */ [at, text], /** @type {number} */ i) => {
    items.push({ at, line: { type: 'user', uuid: `${name}-p${i}`, timestamp: new Date(t0 + at).toISOString(), ...base, message: { role: 'user', content: text } } });
  });
  g.calls.forEach((/** @type {any[]} */ c, /** @type {number} */ i) => {
    const [at, model, input, w5, w1, read, output, tools] = c;
    const content = (tools || []).map((/** @type {[string, any]} */ [tool, tin], /** @type {number} */ j) => ({ type: 'tool_use', id: `${name}-t${i}-${j}`, name: tool, input: tin }));
    items.push({
      at,
      line: {
        type: 'assistant', uuid: `${name}-a${i}`, requestId: `req_${name}_${i}`, timestamp: new Date(t0 + at).toISOString(), effort: 'high', ...base,
        message: {
          id: `msg_${name}_${i}`, model, role: 'assistant', content,
          usage: { input_tokens: input, cache_creation_input_tokens: w5 + w1, cache_read_input_tokens: read, output_tokens: output, cache_creation: { ephemeral_5m_input_tokens: w5, ephemeral_1h_input_tokens: w1 } },
        },
      },
    });
  });
  items.sort((a, b) => a.at - b.at); // stable: a prompt before a call at the same instant
  return jsonl(items.map((x) => x.line));
}

/**
 * Every text a case can reference, by name.
 * @param {any} corpus
 * @returns {Record<string, string>}
 */
export function buildTexts(corpus) {
  /** @type {Record<string, string>} */
  const out = {};
  for (const [rel, text] of Object.entries(fixtureTexts())) {
    out[`fixture/${rel.slice(rel.indexOf('/') + 1)}`] = text.split(FIXTURE_CWD).join('@ROOT@/proj');
  }
  for (const [name, g] of Object.entries(corpus.gen)) out[`gen/${name}`] = genText(name, g);
  for (const [name, text] of Object.entries(corpus.texts || {})) out[`text/${name}`] = /** @type {string} */ (text);
  return Object.fromEntries(Object.entries(out).sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0)));
}

// ---------------------------------------------------------------------------
// Case expansion (sets -> files, projectsDir) — what the golden records as input
// ---------------------------------------------------------------------------

/** @param {any} a @param {any} b */
function deepMerge(a, b) {
  if (!a || typeof a !== 'object' || Array.isArray(a) || !b || typeof b !== 'object' || Array.isArray(b)) return b;
  const out = { ...a };
  for (const [k, v] of Object.entries(b)) out[k] = k in a ? deepMerge(a[k], v) : v;
  return out;
}

/**
 * @param {any} c corpus case
 * @param {any} corpus
 * @returns {any}
 */
function expand(c, corpus) {
  const { sets, ...rest } = c;
  /** @type {Record<string, any>} */
  const files = {};
  for (const s of sets || []) Object.assign(files, corpus.fileSets[s] ?? (() => { throw new Error(`${c.name}: unknown file set ${s}`); })());
  Object.assign(files, c.files || {});
  for (const k of Object.keys(files)) if (files[k] === null) delete files[k];
  const out = { ...rest, files, projectConfig: deepMerge({ metrics: { projectsDir: '@ROOT@/projects' } }, c.projectConfig || {}) };
  if (!Object.keys(files).length) delete out.files;
  return out;
}

// ---------------------------------------------------------------------------
// Materialization
// ---------------------------------------------------------------------------

/** @type {{dir: string, exe: string} | null} */
let stub = null;

/** Builds scripts/golden/rtkstub once per process. */
function stubExe() {
  if (stub) return stub.exe;
  if (!haveGo()) throw new Error('usage-cli golden: go is needed to build scripts/golden/rtkstub');
  const dir = realpathSync(mkdtempSync(join(tmpdir(), 'nxy-golden-stub-')));
  const exe = join(dir, process.platform === 'win32' ? 'rtk.exe' : 'rtk');
  const b = spawnSync('go', ['build', '-o', exe, './scripts/golden/rtkstub'], { cwd: ROOT, encoding: 'utf8', windowsHide: true });
  if (b.status !== 0) {
    rmSync(dir, { recursive: true, force: true });
    throw new Error(`go build rtkstub failed: ${b.stderr || b.stdout}`);
  }
  stub = { dir, exe };
  return exe;
}

function cleanupStub() {
  if (stub) rmSync(stub.dir, { recursive: true, force: true });
  stub = null;
}

/** @returns {any} */
function loadSqlite() {
  const orig = process.emitWarning;
  process.emitWarning = /** @type {any} */ ((w, ...rest) => { if (!/sqlite/i.test(String(w))) orig.call(process, w, ...rest); });
  try {
    return createRequire(import.meta.url)('node:sqlite');
  } finally {
    process.emitWarning = orig;
  }
}

/** @param {string} path @param {string} text */
function put(path, text) {
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, text);
}

/**
 * @typedef {{root: string, proj: string, fill: (t: string) => string, texts: Record<string, string>, now: number}} World
 */

/**
 * Text of a file spec.
 * @param {any} spec
 * @param {World} w
 * @returns {{text: string, mtime: number}}
 */
function specText(spec, w) {
  const o = typeof spec === 'string' ? { ref: spec } : spec;
  let text;
  if (o.ref !== undefined) {
    text = w.texts[o.ref];
    if (text === undefined) throw new Error(`unknown text ${o.ref}`);
    if (o.lines) text = text.split('\n').slice(o.lines[0], o.lines[1]).map((/** @type {string} */ l) => `${l}\n`).join('');
  } else if (o.text !== undefined) text = o.text;
  else text = JSON.stringify(o.json);
  return { text: w.fill(text), mtime: o.mtime ? Date.parse(o.mtime) / 1000 : w.now / 1000 };
}

/**
 * @param {Record<string, any>} files
 * @param {World} w
 * @param {boolean} append
 */
function writeFiles(files, w, append) {
  const slug = w.proj.replace(/[^A-Za-z0-9]/g, '-');
  for (const [key, spec] of Object.entries(files)) {
    const path = join(w.root, ...key.split('@SLUG@').join(slug).split('/'));
    const { text, mtime } = specText(spec, w);
    mkdirSync(dirname(path), { recursive: true });
    if (append) appendFileSync(path, text);
    else writeFileSync(path, text);
    utimesSync(path, mtime, mtime);
  }
}

/**
 * Builds the world of a case under a new root.
 * @param {any} c expanded case
 * @param {Record<string, string>} texts
 * @returns {{world: World, env: Record<string, string>, cleanup: () => void}}
 */
function materialize(c, texts) {
  const root = realpathSync(mkdtempSync(join(tmpdir(), c.rootSpace ? 'nxy golden ' : 'nxy-golden-')));
  const esc = JSON.stringify(root).slice(1, -1);
  const fill = (/** @type {string} */ t) => t.split('@ROOT@').join(esc).split('@ROOTRAW@').join(root);
  const world = { root, proj: join(root, 'proj'), fill, texts, now: Date.parse(c.now || DEFAULT_NOW) };
  const nxyHome = join(root, 'nxyhome');
  const dbPath = join(root, 'rtk', 'history.db');
  mkdirSync(join(world.proj, '.git'), { recursive: true });
  if (c.git !== false) put(join(world.proj, '.git', 'HEAD'), c.git ?? 'ref: refs/heads/main\n');
  writeFiles(c.files || {}, world, false);
  if (c.userConfig) put(join(nxyHome, 'config.json'), fill(JSON.stringify(c.userConfig)));
  put(join(world.proj, '.nxy', 'config.json'), fill(JSON.stringify(c.projectConfig)));
  if (c.filterRows) {
    put(join(world.proj, '.nxy', 'local', 'metrics', 'filter.jsonl'), c.filterRows.map((/** @type {any} */ r) => (typeof r === 'string' ? r : JSON.stringify(r))).join('\n') + '\n');
  }
  if (c.ledger) {
    if (c.ledger.garbage) put(dbPath, 'this is not a sqlite database\n'.repeat(40));
    else {
      mkdirSync(dirname(dbPath), { recursive: true });
      const { DatabaseSync } = loadSqlite();
      const db = new DatabaseSync(dbPath);
      if (c.ledger.noTable) db.exec('CREATE TABLE other (id INTEGER PRIMARY KEY, note TEXT)');
      else {
        db.exec('CREATE TABLE commands (id INTEGER PRIMARY KEY AUTOINCREMENT, timestamp TEXT NOT NULL, project_path TEXT, input_tokens INTEGER, output_tokens INTEGER, saved_tokens INTEGER)');
        const ins = db.prepare('INSERT INTO commands (timestamp, project_path, input_tokens, output_tokens, saved_tokens) VALUES (?, ?, ?, ?, ?)');
        for (const r of JSON.parse(fill(JSON.stringify(c.ledger.rows)))) ins.run(r.timestamp, r.project_path ?? null, r.input_tokens, r.output_tokens, r.saved_tokens);
      }
      db.close();
    }
  }
  const emptyBin = join(root, 'emptybin');
  mkdirSync(emptyBin, { recursive: true });
  /** @type {Record<string, string>} */
  const env = {};
  // Windows env names are case-insensitive: keep exactly one spelling of each
  for (const k of ['SystemRoot', 'PATHEXT', 'ComSpec', 'TEMP', 'TMP', 'TMPDIR', 'windir']) {
    if (process.env[k] !== undefined) env[k] = /** @type {string} */ (process.env[k]);
  }
  Object.assign(env, {
    PATH: emptyBin,
    HOME: join(root, 'home'),
    USERPROFILE: join(root, 'home'),
    APPDATA: join(root, 'appdata'),
    LOCALAPPDATA: join(root, 'localappdata'),
    XDG_DATA_HOME: join(root, 'xdgdata'),
    XDG_CONFIG_HOME: join(root, 'xdgconfig'),
    NXY_HOME: nxyHome,
    RTK_DB_PATH: dbPath,
    TZ: c.tz || 'UTC',
    NXY_GOLDEN_NOW: String(world.now),
  });
  if (c.session) env.CLAUDE_CODE_SESSION_ID = c.session;
  // the 1.x engine cache: fresh at `now`, so the command never probes for rtk
  const rtkPath = c.rtk ? stubExe().replace(/\\/g, '/') : null;
  put(join(nxyHome, 'cache', 'engine.json'), JSON.stringify({
    engine: rtkPath ? 'rtk' : 'off', rtkPath, rtkVersion: rtkPath ? '0.0.0-golden' : null, rtkHookDetected: false,
    rgAvailable: true, rgInstalledAt: null, reason: rtkPath ? 'rtk-found' : 'config', resolvedAt: world.now,
  }));
  if (c.rtk && (c.rtk.gain !== undefined && c.rtk.gain !== null || c.rtk.gainText !== undefined)) {
    const gainPath = join(root, 'gain.json');
    put(gainPath, c.rtk.gainText ?? JSON.stringify(c.rtk.gain));
    env.NXY_GOLDEN_RTK_GAIN = gainPath;
  }
  return { world, env, cleanup: () => rmSync(root, { recursive: true, force: true }) };
}

/**
 * @param {string} entry
 * @param {string[]} args
 * @param {string} stdin
 * @param {World} w
 * @param {Record<string, string>} env
 * @returns {{stdout: string, stderr: string, code: number}}
 */
function run(entry, args, stdin, w, env) {
  const r = spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', '--import', pathToFileURL(CLOCK).href, entry, ...args], {
    cwd: w.proj, env, input: stdin, encoding: 'utf8', timeout: 60_000, windowsHide: true,
  });
  if (r.error) throw r.error;
  return { stdout: normRootText(r.stdout || '', w.root), stderr: normRootText(r.stderr || '', w.root), code: r.status ?? -1 };
}

/** Throws when a stats case would show broad searches (they wait for F3.3). */
function refuseBroadSearches(/** @type {any} */ c, /** @type {World} */ w) {
  const threshold = c.projectConfig?.gate?.contextTokens ?? c.userConfig?.gate?.contextTokens ?? 100000;
  const dir = join(w.root, 'projects');
  if (!existsSync(dir)) return;
  for (const p of readdirSync(dir)) {
    for (const f of readdirSync(join(dir, p), { withFileTypes: true })) {
      if (!f.isFile() || !f.name.endsWith('.jsonl')) continue;
      if (broadSearchesInTranscript(join(dir, p, f.name), { threshold }).length) {
        throw new Error(`usage-cli ${c.name}: ${p}/${f.name} has broad searches (F3.3); change the case`);
      }
    }
  }
}

/** @param {any} c expanded case @param {'stats'|'trend'} cmd @param {Record<string, string>} texts */
function runPlain(c, cmd, texts) {
  const { world, env, cleanup } = materialize(c, texts);
  try {
    if (cmd === 'stats') refuseBroadSearches(c, world);
    const args = (c.args || []).map((/** @type {string} */ a) => a.split('@ROOT@').join(world.root));
    return run(ENTRIES[cmd], args, '', world, env);
  } finally {
    cleanup();
  }
}

/** @param {any} c expanded case @param {Record<string, string>} texts */
function runStatusline(c, texts) {
  const { world, env, cleanup } = materialize(c, texts);
  try {
    const steps = [];
    for (const s of c.steps) {
      if (s.write) writeFiles(s.write, world, false);
      if (s.append) writeFiles(s.append, world, true);
      const stdin = world.fill(typeof s.stdin === 'string' ? s.stdin : JSON.stringify(s.stdin));
      steps.push(run(ENTRIES.statusline, [], stdin, world, env));
    }
    const cache = join(env.NXY_HOME, 'cache');
    /** @type {Record<string, string>} */
    const state = {};
    if (existsSync(cache)) {
      for (const n of readdirSync(cache).sort()) {
        const m = /^statusline-(.+)\.json$/.exec(n);
        // byte offsets include the transcripts' embedded root path, whose length differs per machine
        if (m && n !== 'statusline-input.json') state[m[1]] = normRootText(readFileSync(join(cache, n), 'utf8'), world.root).replace(/"offset":\d+/g, '"offset":0');
      }
    }
    const inputPath = join(cache, 'statusline-input.json');
    const input = existsSync(inputPath) ? normRootText(readFileSync(inputPath, 'utf8'), world.root) : null;
    return { steps, state, input };
  } finally {
    cleanup();
  }
}

// ---------------------------------------------------------------------------
// Sets
// ---------------------------------------------------------------------------

/** @param {any} input @param {Set<string>} refs */
function collectRefs(input, refs) {
  const visit = (/** @type {any} */ spec) => {
    if (typeof spec === 'string') refs.add(spec);
    else if (spec && typeof spec === 'object' && spec.ref !== undefined) refs.add(spec.ref);
  };
  for (const spec of Object.values(input.files || {})) visit(spec);
  for (const s of input.steps || []) {
    for (const spec of Object.values(s.write || {})) visit(spec);
    for (const spec of Object.values(s.append || {})) visit(spec);
  }
}

/** Cases left out of the last generate() because go is not available. */
const skipped = new Set(/** @type {string[]} */ ([]));

/** @returns {Record<string, any[]>} */
export function generate() {
  skipped.clear();
  /** @type {any} */
  const corpus = loadCorpus('usage-cli');
  const texts = buildTexts(corpus);
  const refs = new Set(/** @type {string[]} */ ([]));
  try {
    /** @type {Record<'stats'|'trend'|'statusline', any[]>} */
    const sets = { stats: [], trend: [], statusline: [] };
    for (const cmd of /** @type {const} */ (['stats', 'trend', 'statusline'])) {
      for (const raw of corpus[cmd]) {
        const input = expand(raw, corpus);
        collectRefs(input, refs);
        if (input.rtk && !haveGo()) {
          skipped.add(`${cmd}:${raw.name}`); // needs the rtkstub; TestGoldenTrend/Stats check these in Go
          continue;
        }
        const output = cmd === 'statusline' ? runStatusline(input, texts) : runPlain(input, cmd, texts);
        sets[cmd].push({ name: `${cmd}:${raw.name}`, input: (({ name, ...i }) => i)(input), output });
      }
    }
    const textCases = [...refs].sort().map((name) => {
      if (texts[name] === undefined) throw new Error(`unknown text ${name}`);
      return { name, input: {}, output: { text: texts[name] } };
    });
    return { texts: textCases, ...sets };
  } finally {
    cleanupStub();
  }
}

/** One case per line. */
const COMPACT = true;

/** @returns {string[]} differences, empty when up to date */
export function check() {
  const sets = generate();
  const diffs = [];
  for (const [set, cases] of Object.entries(sets)) diffs.push(...diffArea(AREA, set, cases, COMPACT));
  diffs.push(...staleSets(AREA, Object.keys(sets)));
  if (!skipped.size) return diffs;
  // without go the rtk cases were not generated: they are not differences
  return diffs.filter((d) => !d.endsWith(' formatting differs') && ![...skipped].some((n) => d.endsWith(` stale case ${n}`)));
}

export function write() {
  if (!haveGo()) throw new Error('usage-cli golden: go is needed to write the golden (rtk cases would be dropped)');
  return Object.entries(generate()).map(([set, cases]) => writeArea(AREA, set, cases, COMPACT));
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
