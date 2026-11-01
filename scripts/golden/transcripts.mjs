// @ts-check
/**
 * Golden of the 1.x Claude Code transcript reader (`hosts/claude-code/transcripts.mjs`).
 * Sets: texts (every transcript text once, by name: the 1.x test fixtures + the hand corpus +
 * the sess/ and disc/ trees; the other sets reference it by name; also the discovery mtimes),
 * files (parseFile per file and option variant), sessions (parseSession + subagentFiles),
 * discovery (listSessions / resolveSession), incremental (readIncremental steps), tail (lastCacheTtl).
 * The real case plugs into
 * `buildTexts()` / `generate()`. Timestamps all carry an offset or `Z`: no time zone dependence.
 *
 *   node scripts/golden/transcripts.mjs          write testdata/golden/transcripts/*.json
 *   node scripts/golden/transcripts.mjs --check  fail when they differ
 */
import { appendFileSync, existsSync, mkdtempSync, readdirSync, readFileSync, rmSync, utimesSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { lastCacheTtl, listSessions, newIncrementalState, parseFile, parseSession, readIncremental, resolveSession, subagentFiles } from '../../hosts/claude-code/transcripts.mjs';
import { CORPUS_DIR, diffArea, loadCorpus, staleSets, writeArea } from './lib.mjs';
import { fixtureTexts, materialize, normRoot, plain } from './fixtures.mjs';

export const AREA = 'transcripts';

/** @param {any} line */
function lineText(line) {
  if (typeof line === 'string') return line;
  if (line && typeof line === 'object' && '$crlf' in line) return `${JSON.stringify(line.$crlf)}\r`;
  return JSON.stringify(line);
}

/** @returns {Record<string, string>} corpus rel (`corpus/<name>.jsonl`) -> file text */
export function corpusTexts() {
  /** @type {any} */
  const corpus = loadCorpus('transcripts');
  /** @type {Record<string, string>} */
  const out = {};
  for (const [name, spec] of Object.entries(corpus.files)) {
    const lines = Array.isArray(spec) ? spec : spec.lines;
    const noEol = !Array.isArray(spec) && spec.noEol;
    out[`corpus/${name}.jsonl`] = lines.map(lineText).join('\n') + (noEol ? '' : '\n');
  }
  return out;
}

// ---------------------------------------------------------------------------
// Session trees (sess/) and discovery projects (disc/), built here because their lines are
// formulaic. Cwd is /srv/dev/code/app (slug -srv-dev-code-app).
// ---------------------------------------------------------------------------

const T0 = Date.parse('2026-09-01T10:00:00.000Z');
const iso = (/** @type {number} */ ms) => new Date(T0 + ms).toISOString();
const OPUS = 'claude-opus-5';
const U = (/** @type {number} */ input, /** @type {number} */ w5, /** @type {number} */ w1, /** @type {number} */ read, /** @type {number} */ output, /** @type {any} */ extra = {}) => ({
  input_tokens: input,
  cache_creation_input_tokens: w5 + w1,
  cache_read_input_tokens: read,
  output_tokens: output,
  cache_creation: { ephemeral_5m_input_tokens: w5, ephemeral_1h_input_tokens: w1 },
  ...extra,
});
const base = { cwd: '/srv/dev/code/app', version: '2.1.274', gitBranch: 'main' };
const asst = (/** @type {string} */ id, /** @type {number} */ ms, /** @type {string} */ model, /** @type {any} */ usage, /** @type {any[]} */ content = [], /** @type {any} */ extra = {}) => ({
  type: 'assistant', uuid: `${id}-u`, requestId: `req_${id}`, timestamp: iso(ms), ...base, effort: 'high',
  message: { id: `msg_${id}`, model, usage, content }, ...extra,
});
const usr = (/** @type {string} */ id, /** @type {number} */ ms, /** @type {any} */ content, /** @type {any} */ extra = {}) => ({
  type: 'user', uuid: `${id}-u`, timestamp: iso(ms), ...base, message: { role: 'user', content }, ...extra,
});
const toolUse = (/** @type {string} */ id, /** @type {string} */ name, /** @type {any} */ input) => ({ type: 'tool_use', id, name, input });
const toolResult = (/** @type {string} */ id, /** @type {number} */ ms, /** @type {string} */ toolId, /** @type {any} */ tur) => usr(id, ms, [{ type: 'tool_result', tool_use_id: toolId, content: 'x' }], tur ? { toolUseResult: tur } : {});
const jsonl = (/** @type {any[]} */ lines) => lines.map((l) => JSON.stringify(l)).join('\n') + '\n';
const H = 3_600_000;

/** @returns {Record<string, string>} */
function sessionTree() {
  const P = 'sess/-srv-dev-code-app';
  /** @type {Record<string, string>} */
  const t = {};
  // s0: main only, one break of each cause, a skill, an unknown model, fast mode, synthetic lines.
  t[`${P}/s0.jsonl`] = jsonl([
    usr('s0p1', 0, 'hello world'),
    asst('s0a1', 1000, OPUS, U(1000, 120000, 0, 0, 500)),
    asst('s0a2', 601_000, OPUS, U(200, 130000, 0, 120000, 400)),
    usr('s0p2', 700_000, '<command-name>/nxy:plan</command-name><command-message>nxy:plan</command-message>'),
    asst('s0a3', 710_000, OPUS, U(10, 0, 200000, 0, 300)),
    asst('s0a4', 720_000, 'mystery-9', U(150000, 0, 0, 0, 100)),
    asst('s0a5', 730_000, 'claude-haiku-5-5', U(50, 60000, 50000, 0, 10)),
    asst('s0a5b', 735_000, 'claude-haiku-5-5', U(250000, 0, 0, 0, 10)),
    { type: 'system', subtype: 'compact_boundary', uuid: 's0cb', timestamp: iso(790_000), ...base },
    asst('s0a6', 800_000, OPUS, U(100, 110000, 0, 0, 50)),
    asst('s0a7', 800_000 + H + 100_000, OPUS, U(5, 0, 150000, 0, 5)),
    asst('s0a8', 800_000 + H + 110_000, OPUS, U(1000, 120000, 0, 0, 100, { speed: 'fast' })),
    usr('s0p3', 800_000 + H + 120_000, 'plain message'),
    asst('s0a9', 800_000 + H + 130_000, '<synthetic>', U(0, 0, 0, 0, 0), [{ type: 'text', text: 'internal' }]),
    asst('s0a10', 800_000 + H + 140_000, '<synthetic>', U(7, 0, 0, 0, 3), [{ type: 'text', text: 'synthetic with usage' }]),
    asst('s0a11', 800_000 + H + 150_000, OPUS, U(1, 2, 0, 3, 4), [toolUse('tu-skill', 'Skill', { skill: 'nxy:review' }), toolUse('tu-edit', 'Edit', { file_path: '/srv/dev/code/app/a.go' })]),
    asst('s0a12', 800_000 + H + 160_000, OPUS, U(1, 2, 0, 3, 4), [], { attributionSkill: 'attributed' }),
  ]);

  // s1: one subagent without meta (type from the Agent link), a main call after it waited on it.
  t[`${P}/s1.jsonl`] = jsonl([
    usr('s1p1', 0, 'delegate'),
    asst('s1a1', 1000, OPUS, U(10, 1000, 0, 0, 10), [toolUse('toolu_s1', 'Agent', { subagent_type: 'Explore', description: 'look around' })]),
    toolResult('s1u2', 400_000, 'toolu_s1', { agentId: 'aaaa0001', description: 'look around (result)', resolvedModel: 'claude-haiku-5-5' }),
    asst('s1a2', 401_000, OPUS, U(10, 150000, 0, 0, 20)),
  ]);
  t[`${P}/s1/subagents/agent-aaaa0001.jsonl`] = jsonl([
    usr('s1s1', 2000, 'explore', { isSidechain: true }),
    asst('s1sa1', 3000, 'claude-haiku-5-5', U(150000, 0, 0, 0, 100), [toolUse('s1-bash', 'Bash', { command: 'sleep 30' })], { isSidechain: true, effort: null }),
    toolResult('s1s2', 63_000, 's1-bash', { stdout: 'ok', stderr: '' }),
    asst('s1sa2', 64_000, 'claude-haiku-5-5', U(100, 0, 0, 150000, 50), [], { isSidechain: true, effort: null }),
  ]);

  // s2: several subagents: meta full / broken / partial / missing, an empty one, a user-only one,
  // two of one type, a line shared with the main file, files that are not subagents.
  t[`${P}/s2.jsonl`] = jsonl([
    usr('s2p1', 0, 'fan out'),
    asst('s2a1', 1000, OPUS, U(10, 1000, 0, 0, 10), [
      toolUse('toolu_1', 'Agent', { subagent_type: 'Explore', description: 'one' }),
      toolUse('toolu_2', 'Agent', { subagent_type: 'nxy:planner', description: 'two' }),
      toolUse('toolu_3', 'Task', { prompt: 'no type' }),
    ]),
    toolResult('s2u2', 20_000, 'toolu_1', { agentId: 'bbbb0001', description: '' }),
    toolResult('s2u3', 21_000, 'toolu_2', { agentId: 'bbbb0002', resolvedModel: 'claude-opus-5' }),
    toolResult('s2u4', 22_000, 'toolu_3', { agentId: 'bbbb0003' }),
    asst('dup-main', 30_000, OPUS, U(10, 0, 0, 1000, 10)),
    asst('s2a2', 700_000, OPUS, U(10, 120000, 0, 0, 10)),
  ]);
  const sub = (/** @type {string} */ id, /** @type {any[]} */ lines) => { t[`${P}/s2/subagents/agent-${id}.jsonl`] = jsonl(lines); };
  const meta = (/** @type {string} */ id, /** @type {string} */ text) => { t[`${P}/s2/subagents/agent-${id}.meta.json`] = text; };
  sub('bbbb0001', [
    usr('b1u1', 5000, 'explore', { isSidechain: true }),
    asst('dup-main', 30_000, OPUS, U(10, 0, 0, 1000, 10), [], { isSidechain: true }), // same uuid as the main line: counted once
    asst('b1a1', 6000, 'claude-haiku-5-5', U(100, 70000, 40000, 0, 30), [toolUse('b1-bash', 'Bash', { command: './mvnw test' })], { isSidechain: true, effort: null }),
    toolResult('b1u2', 100_000, 'b1-bash', { stdout: 'a\nb', stderr: 'e', interrupted: true }),
    asst('b1a2', 101_000, 'claude-haiku-5-5', U(10, 0, 0, 110000, 40), [toolUse('b1-read', 'Read', { file_path: '/srv/dev/code/app/r.go' })], { isSidechain: true, effort: null }),
  ]);
  meta('bbbb0001', JSON.stringify({ agentType: 'Explore', description: 'from meta' }));
  sub('bbbb0002', [
    usr('b2u1', 5000, 'plan', { isSidechain: true }),
    asst('b2a1', 6000, OPUS, U(50, 0, 0, 0, 900), [toolUse('b2-edit', 'Write', { file_path: '/srv/dev/code/app/w.go' })], { isSidechain: true, effort: null }),
  ]);
  meta('bbbb0002', '{oops');
  sub('bbbb0003', [
    asst('b3a1', 7000, 'mystery-9', U(70, 0, 0, 0, 5), [], { isSidechain: true, effort: null }),
  ]);
  meta('bbbb0003', JSON.stringify({ description: 'only desc' }));
  sub('bbbb0004', []);
  sub('bbbb0005', [asst('b5a1', 8000, OPUS, U(70, 0, 0, 0, 5), [], { isSidechain: true, effort: null })]);
  sub('bbbb0006', [usr('b6u1', 9000, 'only a user line', { isSidechain: true })]);
  sub('bbbb0007', [asst('b7a1', 9500, 'claude-haiku-5-5', U(5, 0, 0, 0, 1), [], { isSidechain: true, effort: null })]);
  meta('bbbb0007', JSON.stringify({ agentType: 'Explore' }));
  sub('bbbb0008', [asst('b8a1', 9600, 'claude-haiku-5-5', U(5, 0, 0, 0, 1), [], { isSidechain: true, effort: null })]);
  meta('bbbb0008', 'null');
  t[`${P}/s2/subagents/notes.txt`] = 'not a subagent\n';
  t[`${P}/s2/subagents/other.jsonl`] = jsonl([asst('oth', 1, OPUS, U(1, 1, 1, 1, 1))]);
  t[`${P}/s2/subagents/agent-bbbb0009.meta.json`] = JSON.stringify({ agentType: 'orphan meta' }); // meta without transcript

  // disc/: projects for discovery
  const D = 'disc';
  const tiny = (/** @type {string} */ id, /** @type {number} */ n) => jsonl(Array.from({ length: n }, (_, i) => asst(`${id}${i}`, i * 1000, OPUS, U(1, 1, 0, 0, 1))));
  t[`${D}/-srv-dev-code-app/aaa111.jsonl`] = tiny('aaa', 1);
  t[`${D}/-srv-dev-code-app/bbb222.jsonl`] = tiny('bbb', 2);
  t[`${D}/-srv-dev-code-app/abc999.jsonl`] = tiny('abc', 3);
  t[`${D}/-srv-dev-code-app/abd000.jsonl`] = tiny('abd', 4);
  t[`${D}/-srv-dev-code-app/notes.txt`] = 'not a transcript\n';
  t[`${D}/-srv-dev-code-app/dir.jsonl/inner.txt`] = 'a directory named like a transcript\n';
  t[`${D}/-srv-dev-code-app/aaa111/subagents/agent-s1.jsonl`] = tiny('sub', 1);
  t[`${D}/-srv-dev-code-other/ccc333.jsonl`] = tiny('ccc', 5);
  t[`${D}/-srv-dev-code-other/aaa111.jsonl`] = tiny('oaa', 6);
  t[`${D}/-srv-dev-code-empty/.keep`] = '';
  t[`${D}/stray.txt`] = 'a file among the projects\n';
  return t;
}

// ---------------------------------------------------------------------------
// Real nxy sessions, harvested and masked by harvest-transcript.mjs (no free text in them).
// ---------------------------------------------------------------------------

/** @returns {any[]} the harvested cases, by name */
function realCases() {
  const dir = join(CORPUS_DIR, 'transcripts-real');
  if (!existsSync(dir)) return [];
  return readdirSync(dir)
    .filter((f) => f.endsWith('.json'))
    .sort()
    .map((f) => JSON.parse(readFileSync(join(dir, f), 'utf8')));
}

const linesText = (/** @type {any[]} */ lines) => lines.map((l) => JSON.stringify(l)).join('\n') + '\n';

/** @param {any} c @returns {{main: string, subagent: string|null}} */
function realFiles(c) {
  return { main: linesText(c.main), subagent: c.subagent ? linesText(c.subagent.lines) : null };
}

/** @returns {Record<string, string>} */
function realTexts() {
  /** @type {Record<string, string>} */
  const t = {};
  for (const c of realCases()) {
    const dir = `real/${c.case}/${c.project}`;
    const f = realFiles(c);
    t[`${dir}/${c.sessionId}.jsonl`] = f.main;
    if (c.subagent && f.subagent !== null) {
      t[`${dir}/${c.sessionId}/subagents/agent-${c.subagent.agentId}.jsonl`] = f.subagent;
      if (c.subagent.meta) t[`${dir}/${c.sessionId}/subagents/agent-${c.subagent.agentId}.meta.json`] = JSON.stringify(c.subagent.meta);
    }
  }
  return t;
}

/** @returns {any[]} session cases of the real transcripts */
function realSessionCases() {
  const lastTs = (/** @type {any} */ c) => c.main.filter((/** @type {any} */ l) => l.timestamp).at(-1).timestamp;
  return realCases().flatMap((c) => {
    const base = { projectsDir: `real/${c.case}`, project: c.project, id: c.sessionId };
    return [
      { name: `real:${c.case}:default`, ...base },
      { name: `real:${c.case}:no-subagents`, ...base, opts: { includeSubagents: false } },
      { name: `real:${c.case}:timing`, ...base, opts: { timing: true } },
      { name: `real:${c.case}:threshold-20000`, ...base, opts: { cacheBreakThreshold: 20000 } },
      { name: `real:${c.case}:since-mid`, ...base, opts: { since: c.main[Math.floor(c.main.length / 2)].timestamp } },
      { name: `real:${c.case}:since-after-end`, ...base, opts: { since: new Date(Date.parse(lastTs(c)) + 1000).toISOString() } },
    ];
  });
}

/** Cut points that never fall inside a multi-byte char: the text is ASCII (the harvester guarantees it). */
const cutAt = (/** @type {string} */ s, /** @type {number} */ frac) => Math.floor(s.length * frac);

/** @returns {{incremental: any[], tail: any[]}} specs run by incrementalCases / tailCases */
function realIncrementalSteps() {
  /** @type {{name: string, steps: any[]}[]} */
  const inc = [];
  /** @type {{name: string, file: string, tailBytes?: number}[]} */
  const tail = [];
  for (const c of realCases()) {
    const { main, subagent } = realFiles(c);
    inc.push({ name: `real:${c.case}:one-step`, steps: [{ append: main }, {}] });
    const a = cutAt(main, 0.33);
    const b = cutAt(main, 0.71);
    inc.push({ name: `real:${c.case}:three-steps`, steps: [{ append: main.slice(0, a) }, { append: main.slice(a, b) }, { append: main.slice(b) }] });
    inc.push({ name: `real:${c.case}:three-steps-reload`, steps: [{ append: main.slice(0, a) }, { reload: true, append: main.slice(a, b) }, { reload: true, append: main.slice(b) }] });
    if (subagent !== null) {
      // the subagent view: the parent's turn decides turnUsd
      const since = Date.parse(c.subagent.lines.filter((/** @type {any} */ l) => l.timestamp)[2].timestamp);
      inc.push({ name: `real:${c.case}:subagent-turn-since`, steps: [{ turnSinceTs: since, append: subagent }] });
    }
    tail.push({ name: `real:${c.case}:default`, file: main });
    tail.push({ name: `real:${c.case}:window-20000`, file: main, tailBytes: 20000 });
    tail.push({ name: `real:${c.case}:window-3000`, file: main, tailBytes: 3000 });
    if (subagent !== null) tail.push({ name: `real:${c.case}:subagent-file`, file: subagent });
  }
  return { incremental: inc, tail };
}

/** @returns {Record<string, string>} every transcript text by name, sorted */
export function buildTexts() {
  /** @type {Record<string, string>} */
  const all = { ...fixtureTexts(), ...corpusTexts(), ...sessionTree(), ...realTexts() };
  return Object.fromEntries(Object.entries(all).sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0)));
}

/**
 * Runs fn with the texts written under a temp root; always removes the root.
 * @template T
 * @param {Record<string, string>} texts
 * @param {(root: string, path: (rel: string) => string) => T} fn
 * @returns {T}
 */
export function withRoot(texts, fn) {
  const root = materialize(texts);
  try {
    return fn(root, (rel) => `${root}/${rel}`);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
}

// ---------------------------------------------------------------------------
// incremental (statusline) and tail (last cache TTL)
// ---------------------------------------------------------------------------

/**
 * One step: {write?|append?|remove?, reload?, turnSinceTs?}. Order: file change, reload (state
 * through JSON, as the statusline cache), readIncremental. Output per step: the state and the
 * order of its `seen` keys.
 * @param {any[]} steps
 */
function runIncremental(steps) {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-golden-'));
  const path = join(dir, 't.jsonl');
  try {
    let st = newIncrementalState();
    return steps.map((s) => {
      if (s.write !== undefined) writeFileSync(path, s.write);
      else if (s.append !== undefined) appendFileSync(path, s.append);
      else if (s.remove) rmSync(path, { force: true });
      if (s.reload) st = JSON.parse(JSON.stringify(st));
      readIncremental(path, st, { turnSinceTs: s.turnSinceTs ?? null });
      return { state: JSON.parse(JSON.stringify(st)), seenKeys: Object.keys(st.seen) };
    });
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

const tiny = (/** @type {string} */ id, /** @type {number} */ out) => JSON.stringify({ type: 'assistant', requestId: `req_${id}`, timestamp: iso(1000), message: { model: OPUS, usage: { input_tokens: 1, output_tokens: out } } }) + '\n';

/** @returns {any[]} */
function incrementalCases() {
  const HAIKU = 'claude-haiku-5-5';
  /** @type {{name: string, steps: any[]}[]} */
  const cases = [];

  // whole lines appended: prompts, streaming growth and shrink, haiku above-tier delta, synthetic, estimated models
  cases.push({
    name: 'append-whole-lines',
    steps: [
      { append: jsonl([usr('p1', 0, 'hello'), asst('a1', 1000, OPUS, U(100, 0, 0, 0, 50)), asst('a2', 2000, 'mystery-9', U(10, 0, 0, 0, 5))]) },
      {
        append: jsonl([
          toolResult('tr1', 3000, 'tu1', { stdout: 'x' }),
          asst('a1', 3500, OPUS, U(100, 0, 0, 0, 80)),
          asst('a1', 3600, OPUS, U(100, 0, 0, 0, 60)),
          usr('p2', 4000, 'next'),
          asst('hb', 5000, HAIKU, U(250000, 0, 0, 0, 10)),
          asst('hb', 5100, HAIKU, U(250000, 0, 0, 0, 500)),
          asst('syn0', 6000, '<synthetic>', U(0, 0, 0, 0, 0)),
          asst('syn1', 6100, '<synthetic>', U(7, 0, 0, 0, 3)),
          asst('est1', 7000, 'claude-opus-9', U(1000, 0, 0, 0, 100)),
          asst('est2', 7100, 'claude-sonnet-5-9', U(1000, 200, 0, 5000, 100)),
          usr('side', 7200, 'sidechain prompt', { isSidechain: true }),
          usr('tur', 7300, 'with tool use result', { toolUseResult: { stdout: 'x' } }),
          usr('intr', 7400, '[Request interrupted by user]'),
          usr('slash', 7500, '<command-name>/nxy:plan</command-name><command-message>nxy:plan</command-message>'),
          asst('a3', 8000, OPUS, U(5, 0, 150000, 0, 5)),
          asst('fast', 8100, OPUS, U(5, 100, 0, 0, 5, { speed: 'fast' })),
        ]),
      },
      {},
      { append: jsonl([asst('hb', 9000, HAIKU, U(250000, 0, 0, 0, 900)), asst('hb', 9100, HAIKU, U(250000, 0, 0, 0, 100))]) },
    ],
  });

  // a line split across two steps, and across a reload
  const split = jsonl([usr('s1', 0, 'split me'), asst('x1', 1000, OPUS, U(100, 5000, 0, 0, 50)), asst('x2', 2000, OPUS, U(10, 0, 0, 900, 20))]);
  const cut = split.indexOf('"x1"') + 10; // inside the second line
  const cut2 = split.indexOf('"x2"') + 3;
  cases.push({
    name: 'line-split-across-steps',
    steps: [{ append: split.slice(0, cut) }, { append: split.slice(cut, cut2) }, { append: split.slice(cut2) }],
  });
  cases.push({
    name: 'line-split-across-reload',
    steps: [{ append: split.slice(0, cut) }, { reload: true, append: split.slice(cut, cut2) }, { reload: true }, { reload: true, append: split.slice(cut2) }],
  });

  // the file is replaced by a shorter one
  cases.push({
    name: 'replaced-by-shorter-file',
    steps: [
      { write: jsonl([usr('r1', 0, 'one'), asst('r1a', 1000, OPUS, U(100, 0, 0, 0, 50)), asst('r1b', 2000, OPUS, U(100, 0, 0, 0, 50))]) },
      { write: jsonl([asst('r2a', 3000, OPUS, U(7, 0, 0, 0, 7))]) },
      { append: jsonl([asst('r2b', 4000, OPUS, U(9, 0, 0, 0, 9))]) },
    ],
  });

  // missing file: unchanged; read; removed again: unchanged
  cases.push({
    name: 'missing-file',
    steps: [{}, { turnSinceTs: T0 + 500 }, { write: jsonl([asst('m1', 1000, OPUS, U(1, 1, 0, 0, 1))]) }, { remove: true }],
  });

  // subagent view: the parent's turn decides what counts for turnUsd
  const sinceA = T0 + 10_000;
  cases.push({
    name: 'turn-since-ts',
    steps: [
      {
        turnSinceTs: sinceA,
        append: jsonl([
          asst('t0', 0, OPUS, U(100, 0, 0, 0, 10)),
          asst('t1', 5000, OPUS, U(100, 0, 0, 0, 10)),
          asst('t2', 20_000, OPUS, U(100, 0, 0, 0, 10)),
          asst('t3', 25_000, 'mystery-9', U(100, 0, 0, 0, 10)),
          asst('t3b', 5000, 'mystery-9', U(100, 0, 0, 0, 10)),
          { type: 'assistant', requestId: 'req_nots', message: { model: OPUS, usage: U(1, 0, 0, 0, 1) } },
        ]),
      },
      { turnSinceTs: sinceA, append: jsonl([asst('t4', 30_000, OPUS, U(100, 0, 0, 0, 10))]) },
      { turnSinceTs: T0 + 28_000, append: jsonl([asst('t5', 40_000, OPUS, U(100, 0, 0, 0, 10)), asst('t4', 41_000, OPUS, U(100, 0, 0, 0, 90))]) },
      { turnSinceTs: T0 + 28_000, reload: true },
      { append: jsonl([usr('tp', 50_000, 'a main-thread prompt'), asst('t6', 51_000, OPUS, U(100, 0, 0, 0, 10))]) },
    ],
  });

  // odd lines: key fallbacks, bad timestamps and fields, malformed, CRLF, spaced JSON, old usage
  const noReq = (/** @type {string} */ id) => JSON.stringify({ type: 'assistant', timestamp: iso(1000), message: { id, model: OPUS, usage: { input_tokens: 10, output_tokens: 5 } } });
  cases.push({
    name: 'odd-lines',
    steps: [
      {
        append:
          [
            noReq('msg_x1'),
            noReq('msg_x1'),
            JSON.stringify({ type: 'assistant', uuid: 'only-uuid', timestamp: iso(2000), message: { model: OPUS, usage: { input_tokens: 10, output_tokens: 5 } } }),
            JSON.stringify({ type: 'assistant', message: { model: OPUS, usage: { input_tokens: 10, output_tokens: 5 } } }),
            JSON.stringify({ type: 'assistant', message: { model: OPUS, usage: { input_tokens: 10, output_tokens: 9 } } }),
            JSON.stringify({ type: 'assistant', requestId: 'req_old', timestamp: 'garbage', message: { model: OPUS, usage: { input_tokens: 10, cache_creation_input_tokens: 4000, cache_read_input_tokens: '12', output_tokens: 'many' } } }),
            JSON.stringify({ type: 'assistant', requestId: 'req_nomodel', timestamp: iso(3000), message: { usage: { input_tokens: 10, output_tokens: 5 } } }),
            JSON.stringify({ type: 'assistant', requestId: 'req_nomodel', timestamp: iso(3100), message: { usage: { input_tokens: 10, output_tokens: 50 } } }),
            JSON.stringify({ type: 'assistant', requestId: 'req_empty', timestamp: iso(3200), message: { model: OPUS, usage: {} } }),
            '{ not json "usage"',
            JSON.stringify({ type: 'assistant', requestId: 'req_crlf', timestamp: iso(4000), message: { model: OPUS, usage: { input_tokens: 3, output_tokens: 3 } } }) + '\r',
            '{"type": "user", "timestamp": "' + iso(5000) + '", "message": {"role": "user", "content": "spaced json is not prefiltered in"}}',
            JSON.stringify({ type: 'user', timestamp: 'garbage', message: { role: 'user', content: 'bad ts prompt' } }),
            JSON.stringify({ type: 'system', usage: 1 }),
            JSON.stringify({ type: 'assistant', requestId: 'req_after', timestamp: iso(6000), message: { model: OPUS, usage: { input_tokens: 1, output_tokens: 1 } } }),
          ].join('\n') + '\n',
      },
    ],
  });

  // seen is bounded to 256 keys, the oldest dropped; a dropped key counts again, a kept one keeps its place
  const many = (/** @type {number} */ from, /** @type {number} */ to) => {
    let s = '';
    for (let i = from; i < to; i++) s += tiny(`k${String(i).padStart(3, '0')}`, (i % 7) + 1);
    return s;
  };
  cases.push({
    name: 'seen-bounded-to-256',
    steps: [
      { append: many(0, 200) },
      { append: many(200, 400) },
      { append: tiny('k150', 3) + tiny('k399', 50) + tiny('k300', 1) },
      { reload: true, append: many(400, 420) },
      { reload: true, append: tiny('k144', 2) + tiny('k420', 1) },
    ],
  });

  cases.push(...realIncrementalSteps().incremental);

  return cases.map((c) => ({ name: c.name, input: { steps: c.steps }, output: { steps: runIncremental(c.steps) } }));
}

/** @returns {any[]} */
function tailCases() {
  const HAIKU = 'claude-haiku-5-5';
  const line = (/** @type {string} */ id, /** @type {any} */ usage, /** @type {any} */ extra = {}) => asst(id, 1000, OPUS, usage, [], extra);
  const jl = (/** @type {any[]} */ ls) => ls.map((l) => JSON.stringify(l)).join('\n') + '\n';
  const w5 = U(10, 5000, 0, 0, 5);
  const w1 = U(10, 0, 5000, 0, 5);
  const noWrite = U(10, 0, 0, 900, 5);
  const bytes = (/** @type {string} */ s) => Buffer.byteLength(s);
  const lastLine = (/** @type {string} */ text) => text.slice(text.trimEnd().lastIndexOf('\n') + 1);

  const mixed = jl([line('o1', w1), line('o2', w5), line('o3', w1, { isSidechain: true })]);
  const cutText = jl([line('c1', w1), line('c2', noWrite)]);
  const longFirst = jl([line('f1', w5), line('f2', noWrite)]);
  /** @type {{name: string, file?: string|null, pathNull?: boolean, tailBytes?: number}[]} */
  const cases = [
    { name: 'null-path', pathNull: true, file: jl([line('n1', w1)]) },
    { name: 'missing-file', file: null },
    { name: 'empty-file', file: '' },
    { name: 'five-minute-write', file: jl([line('a', w5)]) },
    { name: 'one-hour-write', file: jl([line('a', w1)]) },
    { name: 'both-tiers-one-call', file: jl([line('a', U(10, 100, 5000, 0, 5))]) },
    { name: 'no-cache-write', file: jl([line('a', noWrite)]) },
    { name: 'sidechain-only', file: jl([line('a', w1, { isSidechain: true })]) },
    { name: 'last-write-decides', file: mixed },
    { name: 'skips-newer-calls-without-write', file: jl([line('a', w1), line('b', noWrite), line('c', noWrite)]) },
    { name: 'old-transcript-without-breakdown', file: jl([line('a', { input_tokens: 10, cache_creation_input_tokens: 4000, output_tokens: 5 })]) },
    { name: 'malformed-last-line', file: jl([line('a', w5)]) + '{"usage": broken\n' },
    { name: 'no-trailing-newline', file: jl([line('a', w1)]).trimEnd() },
    { name: 'user-and-other-types', file: jl([line('a', w5), { type: 'user', uuid: 'u', message: { role: 'user', content: 'usage' } }, { type: 'system', usage: 1 }]) },
    { name: 'synthetic-model-write', file: jl([{ ...line('a', w1), message: { id: 'm', model: '<synthetic>', usage: w1 } }]) },
    { name: 'haiku-model-write', file: jl([asst('h', 1000, HAIKU, w1)]) },
    // tailBytes: the window starts mid-line and that partial line is dropped
    { name: 'window-keeps-whole-file', file: cutText, tailBytes: bytes(cutText) },
    { name: 'window-bigger-than-file', file: cutText, tailBytes: bytes(cutText) + 1000 },
    { name: 'window-cuts-first-line', file: cutText, tailBytes: bytes(lastLine(cutText)) + 20 },
    { name: 'window-starts-at-line-boundary', file: cutText, tailBytes: bytes(lastLine(cutText)) + 1 },
    { name: 'window-inside-last-line', file: cutText, tailBytes: 30 },
    { name: 'window-cut-keeps-later-write', file: longFirst + jl([line('f3', w1)]), tailBytes: bytes(jl([line('f3', w1)])) + 25 },
    ...realIncrementalSteps().tail,
  ];
  return cases.map((c) => ({
    name: c.name,
    input: { file: c.file ?? null, pathNull: !!c.pathNull, tailBytes: c.tailBytes ?? 0 },
    output: { ttl: tailTtl(c) },
  }));
}

/** @param {{file?: string|null, pathNull?: boolean, tailBytes?: number}} c */
function tailTtl(c) {
  if (c.pathNull) return lastCacheTtl(null, c.tailBytes ? { tailBytes: c.tailBytes } : {});
  const dir = mkdtempSync(join(tmpdir(), 'nxy-golden-'));
  try {
    const path = join(dir, 't.jsonl');
    if (c.file !== null && c.file !== undefined) writeFileSync(path, c.file);
    return lastCacheTtl(path, c.tailBytes ? { tailBytes: c.tailBytes } : {});
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

/** @returns {Record<string, any[]>} */
export function generate() {
  /** @type {any} */
  const corpus = loadCorpus('transcripts');
  const texts = buildTexts();
  const jsonl = Object.keys(texts).filter((r) => r.endsWith('.jsonl'));

  /** @type {{name: string, files: string[], agentId?: string|null, since?: string|number|null, share?: boolean}[]} */
  const runs = [];
  for (const rel of jsonl) {
    runs.push({ name: `file:${rel}`, files: [rel] });
    const m = /agent-([^/]+)\.jsonl$/.exec(rel);
    runs.push({ name: `agent:${rel}`, files: [rel], agentId: m ? m[1] : 'ag1' });
  }
  for (const c of corpus.cases) runs.push(c);

  const files = withRoot(texts, (root, path) =>
    runs.map((r) => {
      const since = typeof r.since === 'string' ? Date.parse(r.since) : (r.since ?? null);
      const seenUuids = r.share ? new Set() : undefined;
      const results = r.files.map((rel) => normRoot(plain(parseFile(path(rel), { agentId: r.agentId ?? null, since, ...(seenUuids ? { seenUuids } : {}) })), root));
      // callsOrder pins the key order of tools.calls (JSON object comparison ignores it)
      const callsOrder = results.map((x) => Object.keys(x.tools.calls));
      return { name: r.name, input: { files: r.files, agentId: r.agentId ?? null, since, share: !!r.share }, output: { results, callsOrder } };
    }),
  );

  // sessions: parseSession (+ the subagent files it found) per case
  const sessions = withRoot(texts, (root) =>
    [...corpus.sessions, ...realSessionCases()].map((/** @type {any} */ c) => {
      const dir = join(root, c.projectsDir, c.project);
      const ref = { sessionId: c.id, path: join(dir, `${c.id}.jsonl`), project: c.project, agentsDir: join(dir, c.id, 'subagents'), mtimeMs: 0, size: 0 };
      const o = c.opts || {};
      const since = typeof o.since === 'string' ? Date.parse(o.since) : (o.since ?? null);
      const opts = { ...o, since };
      const session = normRoot(plain(parseSession(ref, opts)), root);
      const subagents = normRoot(plain(subagentFiles(ref)), root);
      const input = { projectsDir: c.projectsDir, project: c.project, id: c.id, opts: { ...o, ...(since === null ? {} : { since }) } };
      return { name: c.name, input, output: { session, subagents, callsOrder: Object.keys(session.tools.calls) } };
    }),
  );

  // discovery: listSessions / resolveSession over disc/ with the corpus mtimes
  const discovery = withRoot(texts, (root) => {
    for (const [rel, sec] of Object.entries(corpus.mtimes)) utimesSync(join(root, ...rel.split('/')), /** @type {number} */ (sec), /** @type {number} */ (sec));
    return corpus.discovery.map((/** @type {any} */ c) => {
      const opts = { projectsDir: join(root, c.projectsDir), cwd: c.cwd, all: c.all, since: c.since };
      const input = { projectsDir: c.projectsDir, cwd: c.cwd ?? '', all: !!c.all, since: c.since ?? 0, selector: c.selector ?? null };
      const output = c.selector === undefined ? { list: normRoot(listSessions(opts), root) } : { ref: normRoot(resolveSession(c.selector, opts), root) };
      return { name: c.name, input, output };
    });
  });

  return { texts: [{ name: 'texts', input: {}, output: { texts, mtimes: corpus.mtimes } }], files, sessions, discovery, incremental: incrementalCases(), tail: tailCases() };
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
