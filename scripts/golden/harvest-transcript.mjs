// @ts-check
/**
 * Turns one real Claude Code session of THIS repo into a corpus case that holds no free text.
 *
 *   node scripts/golden/harvest-transcript.mjs <session.jsonl> [--agent <id>] --name <case>
 *
 * Privacy first. The output is rebuilt from a whitelist of fields, not filtered from the original:
 * what is not named here is gone. Rules:
 *  - a session whose `cwd` is not this repo (ROOT) is refused, so no work repo can get in;
 *  - ROOT becomes /srv/nxy.dev, the home dir /srv/home (native, forward-slash and Git Bash /c/... forms);
 *  - free text (user/assistant text, thinking, tool results, stdout/stderr, Bash commands, descriptions,
 *    patterns, paths outside ROOT) keeps its shape but every letter/digit (and any non-ASCII char) is `x`;
 *  - what the parsers look for survives: <command-name>/<command-message> contents that look like a
 *    command name, the humanText prefixes, `locate.mjs`, `nxy locate nudge:` and the limit texts;
 *  - ids, models, timestamps, usage numbers, tool names and the skill / subagent_type names stay;
 *  - the main file keeps its first lines up to ~120 KB, the subagent (at most one) up to ~60 KB.
 * The result is scripts/golden/corpus/transcripts-real/<case>.json. Read it by eye before committing.
 */
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { homedir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { CORPUS_DIR, ROOT, toJson } from './lib.mjs';

const MAIN_BUDGET = 120_000;
const SUB_BUDGET = 60_000;
const FAKE_ROOT = '/srv/nxy.dev';
const FAKE_HOME = '/srv/home';

/** @param {string} p */
const fwd = (p) => p.replace(/\\/g, '/');
/** @param {string} p  C:\x\y -> /c/x/y */
const gitBash = (p) => fwd(p).replace(/^([A-Za-z]):/, (_, d) => `/${d.toLowerCase()}`);

/** @param {string} dir  the forms a directory can show up in */
function forms(dir) {
  return [...new Set([dir, fwd(dir), gitBash(dir)])].sort((a, b) => b.length - a.length);
}

/**
 * Case-insensitive prefix rewrite of a path.
 * @param {string} s
 */
export function rewritePaths(s) {
  let out = s;
  for (const [dir, fake] of /** @type {[string, string][]} */ ([[ROOT, FAKE_ROOT], [homedir(), FAKE_HOME]])) {
    for (const f of forms(dir)) {
      const re = new RegExp(f.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'gi');
      out = out.replace(re, fake);
    }
  }
  return out;
}

/** Windows paths keep backslashes after the fake prefix: make them slashes. */
/** @param {string} s */
function tidy(s) {
  return s.replace(/(\/srv\/(?:nxy\.dev|home))([\\/][^\s"'<>|*?]*)/g, (_, a, rest) => a + rest.replace(/\\/g, '/'));
}

/** @param {string} s @returns {string} */
const mask = (s) => s.replace(/[\p{L}\p{N}]/gu, 'x').replace(/[^\x00-\x7f]/gu, 'x');

// What the parsers read out of free text.
const KEEP = /<command-(name|message)>([\s\S]*?)<\/command-\1>|^<(?:task-notification|scheduled-wakeup|background-task|system-reminder)|^\[Request interrupted|locate\.mjs|nxy locate nudge:|(?:usage|session|rate)\s+limit|limit reached|limit will reset/gi;

/** @param {string} s */
export function maskText(s) {
  if (typeof s !== 'string') return s;
  let out = '';
  let last = 0;
  for (const m of s.matchAll(KEEP)) {
    const idx = /** @type {number} */ (m.index);
    out += mask(s.slice(last, idx));
    if (m[1]) {
      // command tag: keep the content only when it is a command name
      out += /^\/?[\w:.-]+$/.test(m[2]) ? m[0] : `<command-${m[1]}>${mask(m[2])}</command-${m[1]}>`;
    } else {
      out += m[0];
    }
    last = idx + m[0].length;
  }
  return out + mask(s.slice(last));
}

/** A path inside this repo stays (rewritten); anything else is masked keeping separators. */
/** @param {any} p */
function safePath(p) {
  if (typeof p !== 'string') return p;
  const r = tidy(rewritePaths(p));
  return r.startsWith(`${FAKE_ROOT}/`) || r === FAKE_ROOT ? r : mask(r);
}

/** A name that must survive (skill, subagent type, command): kept only when it looks like a name. */
/** @param {any} s */
const safeName = (s) => (typeof s === 'string' ? (/^[\w:.-]+$/.test(s) ? s : mask(s)) : s);

/** @param {any} v @returns {any} numbers, booleans and a few enum strings of a usage block */
function safeUsage(v) {
  if (Array.isArray(v)) return v.map(safeUsage);
  if (v && typeof v === 'object') {
    /** @type {any} */
    const out = {};
    for (const [k, x] of Object.entries(v)) {
      if (typeof x === 'string') {
        if (['speed', 'service_tier', 'inference_geo', 'type'].includes(k) && /^[\w-]+$/.test(x)) out[k] = x;
      } else if (x && typeof x === 'object') out[k] = safeUsage(x);
      else if (typeof x === 'number' || typeof x === 'boolean' || x === null) out[k] = x;
    }
    return out;
  }
  return v;
}

/** @param {any} input */
function safeInput(input) {
  if (!input || typeof input !== 'object') return {};
  /** @type {any} */
  const o = {};
  for (const k of ['file_path', 'notebook_path', 'path']) if (typeof input[k] === 'string') o[k] = safePath(input[k]);
  if (input.skill !== undefined) o.skill = safeName(String(input.skill));
  if (input.subagent_type !== undefined) o.subagent_type = safeName(String(input.subagent_type));
  for (const k of ['description', 'prompt', 'command', 'pattern']) if (typeof input[k] === 'string') o[k] = maskText(input[k]);
  return o;
}

/** @param {any} c tool_result content */
function maskContent(c) {
  if (typeof c === 'string') return maskText(c);
  if (Array.isArray(c)) return c.map((b) => (b && typeof b === 'object' && typeof b.text === 'string' ? { type: String(b.type || 'text'), text: maskText(b.text) } : { type: 'text', text: '' }));
  return '';
}

/** @param {any} content message.content */
function safeContent(content) {
  if (typeof content === 'string') return maskText(content);
  if (!Array.isArray(content)) return [];
  return content.map((b) => {
    if (!b || typeof b !== 'object') return { type: 'text', text: '' };
    switch (b.type) {
      case 'text':
        return { type: 'text', text: maskText(b.text || '') };
      case 'thinking':
        return { type: 'thinking', thinking: '' };
      case 'tool_use':
        return { type: 'tool_use', id: b.id, name: safeName(b.name), input: safeInput(b.input) };
      case 'tool_result':
        return { type: 'tool_result', tool_use_id: b.tool_use_id, content: maskContent(b.content) };
      default:
        return { type: safeName(String(b.type)) };
    }
  });
}

/** @param {any} tur toolUseResult */
function safeToolUseResult(tur) {
  if (typeof tur === 'string') return maskText(tur);
  if (!tur || typeof tur !== 'object') return tur;
  /** @type {any} */
  const o = {};
  if (Array.isArray(tur)) return o;
  if (typeof tur.agentId === 'string') o.agentId = tur.agentId;
  if (typeof tur.resolvedModel === 'string') o.resolvedModel = safeName(tur.resolvedModel);
  if (typeof tur.description === 'string') o.description = maskText(tur.description);
  if (typeof tur.stdout === 'string') o.stdout = maskText(tur.stdout);
  if (typeof tur.stderr === 'string') o.stderr = maskText(tur.stderr);
  if (tur.interrupted !== undefined) o.interrupted = !!tur.interrupted;
  if (tur.file && typeof tur.file === 'object' && typeof tur.file.filePath === 'string') o.file = { filePath: safePath(tur.file.filePath) };
  return o;
}

const TOP_COPY = ['type', 'uuid', 'parentUuid', 'timestamp', 'sessionId', 'isSidechain', 'version', 'requestId', 'agentId', 'subtype', 'isMeta', 'isCompactSummary'];

/**
 * Rebuilds one line from the whitelist; null drops it.
 * @param {any} e
 */
function safeLine(e) {
  if (!e || typeof e !== 'object' || typeof e.type !== 'string') return null;
  /** @type {any} */
  const o = {};
  if (e.type === 'user' || e.type === 'assistant' || e.type === 'system') {
    for (const k of TOP_COPY) if (e[k] !== undefined) o[k] = k === 'subtype' ? safeName(e[k]) : e[k];
    if (typeof e.cwd === 'string') o.cwd = safePath(e.cwd);
    if (typeof e.gitBranch === 'string') o.gitBranch = safeName(e.gitBranch);
    if (typeof e.effort === 'string') o.effort = safeName(e.effort);
    else if (e.effort === null) o.effort = null;
    if (typeof e.attributionSkill === 'string') o.attributionSkill = safeName(e.attributionSkill);
    if (e.message && typeof e.message === 'object') {
      const m = e.message;
      /** @type {any} */
      const msg = {};
      if (m.id !== undefined) msg.id = m.id;
      if (m.role !== undefined) msg.role = safeName(m.role);
      if (typeof m.model === 'string') msg.model = safeName(m.model);
      if (m.content !== undefined) msg.content = safeContent(m.content);
      if (m.usage && typeof m.usage === 'object') msg.usage = safeUsage(m.usage);
      o.message = msg;
    }
    if (e.toolUseResult !== undefined) o.toolUseResult = safeToolUseResult(e.toolUseResult);
    return o;
  }
  // any other line type: only what moves the clock
  if (typeof e.timestamp !== 'string') return null;
  o.type = e.type;
  if (e.uuid !== undefined) o.uuid = e.uuid;
  o.timestamp = e.timestamp;
  return o;
}

/** @param {string} text @returns {any[]} */
function parseLines(text) {
  const out = [];
  for (const l of text.split('\n')) {
    if (!l) continue;
    try {
      out.push(JSON.parse(l));
    } catch {
      /* skip */
    }
  }
  return out;
}

/** Every cwd must be this repo. @param {any[]} lines */
function assertThisRepo(lines, what) {
  const root = fwd(ROOT).toLowerCase();
  let seen = 0;
  for (const e of lines) {
    if (typeof e.cwd !== 'string') continue;
    seen++;
    if (fwd(e.cwd).toLowerCase() !== root) throw new Error(`${what}: cwd ${JSON.stringify(e.cwd)} is not this repo; refusing`);
  }
  if (!seen) throw new Error(`${what}: no cwd in the session; cannot tell which repo it is; refusing`);
}

/** @param {any[]} lines @param {number} budget */
function harvest(lines, budget) {
  /** @type {any[]} */
  const out = [];
  let size = 0;
  for (const e of lines) {
    const s = safeLine(e);
    if (!s) continue;
    const n = JSON.stringify(s).length + 1;
    if (size + n > budget) break;
    size += n;
    out.push(s);
  }
  return out;
}

/** @param {any} meta */
function safeMeta(meta) {
  if (!meta || typeof meta !== 'object') return null;
  /** @type {any} */
  const o = {};
  for (const [k, v] of Object.entries(meta)) {
    if (k === 'description') o[k] = maskText(String(v));
    else if (typeof v === 'string') {
      if (['agentType', 'toolUseId', 'requestShape'].includes(k)) o[k] = safeName(v);
    } else if (typeof v === 'number' || typeof v === 'boolean') o[k] = v;
  }
  return o;
}

/** Hard stop on anything that smells personal in the final text. @param {string} text */
function assertClean(text) {
  const checks = [
    [/[A-Za-z]:[\\/]+Users[\\/]/i, 'a Windows user dir'],
    [/\/home\/[^/"\\]+\//, 'a /home/<x>/ path'],
    [/\/Users\/[^/"\\]+\//, 'a /Users/<x>/ path'],
    [/[\w.+-]+@[\w-]+\.[\w.-]+/, 'an e-mail'],
    [new RegExp(homedir().split(/[\\/]/).pop() || '@@@', 'i'), 'the user name'],
    [/Documents/i, 'a home folder name'],
  ];
  for (const [re, what] of /** @type {[RegExp, string][]} */ (checks)) {
    const m = re.exec(text);
    if (m) throw new Error(`personal data left in the output (${what}): ${JSON.stringify(m[0])}`);
  }
}

function main() {
  const args = process.argv.slice(2);
  const take = (/** @type {string} */ flag) => {
    const i = args.indexOf(flag);
    if (i === -1) return null;
    const [, v] = args.splice(i, 2);
    return v ?? null;
  };
  const agent = take('--agent');
  const name = take('--name');
  const file = args[0];
  if (!file || !name || !/^[\w-]+$/.test(name)) {
    console.error('usage: node scripts/golden/harvest-transcript.mjs <session.jsonl> [--agent <id>] --name <case>');
    process.exit(2);
  }
  const path = resolve(file);
  const mainLines = parseLines(readFileSync(path, 'utf8'));
  assertThisRepo(mainLines, 'session');
  const sessionId = /([^\\/]+)\.jsonl$/.exec(path)?.[1] ?? 'session';

  /** @type {any} */
  let subagent = null;
  if (agent) {
    if (!/^[\w-]+$/.test(agent)) throw new Error('bad agent id');
    const sub = join(dirname(path), sessionId, 'subagents', `agent-${agent}.jsonl`);
    const subLines = parseLines(readFileSync(sub, 'utf8'));
    assertThisRepo(subLines, 'subagent');
    const metaPath = sub.replace(/\.jsonl$/, '.meta.json');
    let meta = null;
    if (existsSync(metaPath)) {
      try {
        meta = safeMeta(JSON.parse(readFileSync(metaPath, 'utf8')));
      } catch {
        meta = null;
      }
    }
    subagent = { agentId: agent, meta, lines: harvest(subLines, SUB_BUDGET) };
  }
  const result = { case: name, sessionId, project: '-srv-nxy-dev', main: harvest(mainLines, MAIN_BUDGET), subagent };
  const text = toJson(result);
  assertClean(text);
  const dir = join(CORPUS_DIR, 'transcripts-real');
  mkdirSync(dir, { recursive: true });
  const out = join(dir, `${name}.json`);
  writeFileSync(out, text, 'utf8');
  console.log(`wrote ${out} (${result.main.length} main lines${subagent ? `, ${subagent.lines.length} subagent lines` : ''}, ${text.length} bytes)`);
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) main();
