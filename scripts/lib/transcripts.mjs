// @ts-check
/**
 * Reads Claude Code transcripts (`~/.claude/projects/<slug>/<session>.jsonl` and their
 * `<session>/subagents/agent-<id>.jsonl`) and turns them into per-call usage records.
 *
 * Derived from Anthropic's session-report plugin (Apache-2.0) — see NOTICE. Rewritten with
 * per-call state (no module globals) so it can be used both for full reports and for the
 * incremental statusline reader.
 */
import { closeSync, existsSync, openSync, readFileSync, readSync, readdirSync, statSync } from 'node:fs';
import { basename, join } from 'node:path';
import { claudeProjectsDir, projectSlug } from './paths.mjs';
import { addUsage, cacheHitPct, costFor, emptyUsage, isSyntheticModel, isZeroUsage, toUsage, totalInput } from './pricing.mjs';

const IDLE_GAP_MS = 5 * 60 * 1000; // gaps longer than this do not count as "active" time

/**
 * @typedef {object} SessionRef
 * @property {string} sessionId
 * @property {string} path      main transcript
 * @property {string} project   project slug
 * @property {string} agentsDir `<session>/subagents`
 * @property {number} mtimeMs
 * @property {number} size
 */

/**
 * @typedef {object} Call
 * @property {string} key       dedupe key (requestId / message id)
 * @property {number} ts        epoch ms
 * @property {string} model
 * @property {import('./pricing.mjs').Usage} usage
 * @property {string|null} effort
 * @property {string|null} skill
 * @property {string|null} promptKey
 * @property {string|null} agentId  null for the main transcript
 */

// ---------------------------------------------------------------------------
// Session discovery
// ---------------------------------------------------------------------------

/**
 * @param {{cwd?: string, all?: boolean, projectsDir?: string|null, since?: number|null}} opts
 * @returns {SessionRef[]} newest first
 */
export function listSessions(opts = {}) {
  const root = claudeProjectsDir(opts.projectsDir || undefined);
  if (!existsSync(root)) return [];
  const projects = opts.all || !opts.cwd ? readdirSync(root) : [projectSlug(opts.cwd)];
  /** @type {SessionRef[]} */
  const out = [];
  for (const project of projects) {
    const dir = join(root, project);
    let entries;
    try {
      entries = readdirSync(dir, { withFileTypes: true });
    } catch {
      continue;
    }
    for (const e of entries) {
      if (!e.isFile() || !e.name.endsWith('.jsonl')) continue;
      const path = join(dir, e.name);
      const st = statSync(path);
      if (opts.since && st.mtimeMs < opts.since) continue;
      const sessionId = basename(e.name, '.jsonl');
      out.push({ sessionId, path, project, agentsDir: join(dir, sessionId, 'subagents'), mtimeMs: st.mtimeMs, size: st.size });
    }
  }
  return out.sort((a, b) => b.mtimeMs - a.mtimeMs);
}

/**
 * `'last'` → most recent session of the project; otherwise an id or unique id prefix.
 * @param {string} selector
 * @param {{cwd?: string, all?: boolean, projectsDir?: string|null}} opts
 * @returns {SessionRef|null}
 */
export function resolveSession(selector, opts = {}) {
  const sessions = listSessions({ ...opts, all: opts.all || selector !== 'last' });
  if (selector === 'last') return sessions[0] || null;
  return sessions.find((s) => s.sessionId === selector) || sessions.find((s) => s.sessionId.startsWith(selector)) || null;
}

/** Subagent transcripts of a session with their `.meta.json` (if present). */
export function subagentFiles(ref) {
  if (!existsSync(ref.agentsDir)) return [];
  return readdirSync(ref.agentsDir)
    .filter((n) => n.startsWith('agent-') && n.endsWith('.jsonl'))
    .map((n) => {
      const path = join(ref.agentsDir, n);
      const agentId = n.slice('agent-'.length, -'.jsonl'.length);
      let meta = null;
      try {
        meta = JSON.parse(readFileSync(path.replace(/\.jsonl$/, '.meta.json'), 'utf8'));
      } catch {
        /* no meta */
      }
      return { path, agentId, meta };
    });
}

// ---------------------------------------------------------------------------
// Line-level parsing
// ---------------------------------------------------------------------------

/** Text of a human prompt line, or null for tool results / meta / continuations. */
function humanText(e) {
  if (e.isMeta || e.isCompactSummary) return null;
  const c = e.message?.content;
  let text = null;
  if (typeof c === 'string') text = c;
  else if (Array.isArray(c)) {
    const first = c[0];
    if (!first || first.type === 'tool_result') return null;
    if (first.type === 'text') text = first.text || '';
  }
  if (text === null) return null;
  if (/^<(task-notification|scheduled-wakeup|background-task|system-reminder)/.test(text)) return null;
  if (text.startsWith('[Request interrupted')) return null;
  return text;
}

/** Tools that write a file the model names in the call (Bash edits via sed/heredoc are invisible here). */
const EDIT_TOOLS = new Set(['Edit', 'Write', 'MultiEdit', 'NotebookEdit']);

function slashCommand(text) {
  const m = /<command-(?:name|message)>\/?([^<]+)<\/command-/.exec(text);
  return m ? m[1].trim() : null;
}

/**
 * Parses one transcript file. Streaming duplicates (same requestId, growing output) are
 * collapsed keeping the largest output count.
 * @param {string} path
 * @param {{agentId?: string|null, since?: number|null, seenUuids?: Set<string>}} [opts]
 */
export function parseFile(path, opts = {}) {
  const agentId = opts.agentId ?? null;
  const seenUuids = opts.seenUuids || new Set();
  /** @type {Map<string, Call>} */
  const calls = new Map();
  /** @type {Map<string, {subagentType: string, description: string}>} */
  const toolUseToAgent = new Map();
  /** @type {Map<string, {agentType: string, description: string, model: string|null, toolUseId: string}>} */
  const agentLinks = new Map();
  const tools = { calls: /** @type {Record<string, number>} */ ({}), filesRead: new Set(), filesEdited: new Set(), bash: { calls: 0, resultLines: 0, resultChars: 0, interrupted: 0 } };
  const prompts = [];
  let firstTs = null;
  let lastTs = null;
  let prevTs = null;
  let activeMs = 0;
  let currentSkill = null;
  let currentPrompt = null;
  let meta = { cwd: null, version: null, gitBranch: null, model: null };

  let text;
  try {
    text = readFileSync(path, 'utf8');
  } catch {
    return { calls: [], agentLinks, tools, prompts, firstTs, lastTs, activeMs, turns: 0, meta };
  }
  for (const line of text.split('\n')) {
    if (!line) continue;
    let e;
    try {
      e = JSON.parse(line);
    } catch {
      continue;
    }
    if (e.uuid) {
      if (seenUuids.has(e.uuid)) continue;
      seenUuids.add(e.uuid);
    }
    if (e.timestamp) {
      const ts = Date.parse(e.timestamp);
      if (!Number.isNaN(ts)) {
        if (opts.since && ts < opts.since) continue;
        if (firstTs === null) firstTs = ts;
        if (prevTs !== null) {
          const gap = ts - prevTs;
          if (gap > 0 && gap < IDLE_GAP_MS) activeMs += gap;
        }
        prevTs = ts;
        lastTs = ts;
      }
    }
    if (!meta.cwd && e.cwd) meta = { ...meta, cwd: e.cwd, version: e.version || null, gitBranch: e.gitBranch || null };

    if (e.type === 'user') {
      const tur = e.toolUseResult;
      const c = e.message?.content;
      const tuid = Array.isArray(c) && c[0]?.tool_use_id;
      if (tur && typeof tur === 'object') {
        if (tur.agentId && tuid) {
          const link = toolUseToAgent.get(tuid);
          agentLinks.set(tur.agentId, {
            agentType: link?.subagentType || 'unknown',
            description: tur.description || link?.description || '',
            model: tur.resolvedModel || null,
            toolUseId: tuid,
          });
        } else if (typeof tur.stdout === 'string' || typeof tur.stderr === 'string') {
          const out = `${tur.stdout || ''}${tur.stderr ? '\n' + tur.stderr : ''}`;
          tools.bash.calls++;
          tools.bash.resultChars += out.length;
          tools.bash.resultLines += out ? out.split('\n').length : 0;
          if (tur.interrupted) tools.bash.interrupted++;
        } else if (tur.file?.filePath) {
          tools.filesRead.add(tur.file.filePath);
        }
        continue;
      }
      const ht = humanText(e);
      if (ht === null) continue;
      const cmd = slashCommand(ht);
      currentSkill = cmd; // a plain human message resets skill attribution
      if (!e.isSidechain && agentId === null) {
        currentPrompt = e.uuid || `${path}:${prompts.length}`;
        prompts.push({ key: currentPrompt, ts: lastTs, text: cmd ? `/${cmd}` : ht.slice(0, 200), command: cmd });
      }
      continue;
    }

    if (e.type !== 'assistant') continue;
    const msg = e.message || {};
    if (Array.isArray(msg.content)) {
      for (const b of msg.content) {
        if (b?.type !== 'tool_use') continue;
        tools.calls[b.name] = (tools.calls[b.name] || 0) + 1;
        if (EDIT_TOOLS.has(b.name)) {
          const p = b.input?.file_path || b.input?.notebook_path;
          if (typeof p === 'string' && p) tools.filesEdited.add(p);
        }
        if (b.name === 'Skill' && b.input?.skill) currentSkill = String(b.input.skill);
        if ((b.name === 'Agent' || b.name === 'Task') && b.input) {
          toolUseToAgent.set(b.id, { subagentType: String(b.input.subagent_type || 'general-purpose'), description: String(b.input.description || '') });
        }
      }
    }
    if (typeof e.attributionSkill === 'string' && e.attributionSkill) currentSkill = e.attributionSkill;
    if (!msg.usage) continue;
    const usage = toUsage(msg.usage);
    const synthetic = isSyntheticModel(msg.model);
    // `<synthetic>` con usage en cero es un mensaje interno de Claude Code, no una llamada a la API.
    if (synthetic && isZeroUsage(usage)) continue;
    if (msg.model && !synthetic && !meta.model) meta.model = msg.model;
    const key = e.requestId || (typeof msg.id === 'string' && msg.id.startsWith('msg_') ? msg.id : `${path}:${e.uuid || calls.size}`);
    const prev = calls.get(key);
    if (!prev || usage.output >= prev.usage.output) {
      calls.set(key, {
        key,
        ts: lastTs ?? 0,
        model: msg.model || 'unknown',
        usage,
        effort: typeof e.effort === 'string' ? e.effort : null,
        skill: currentSkill,
        promptKey: currentPrompt,
        agentId,
      });
    }
  }
  return { calls: [...calls.values()], agentLinks, tools, prompts, firstTs, lastTs, activeMs, turns: prompts.length, meta };
}

// ---------------------------------------------------------------------------
// Session-level aggregation
// ---------------------------------------------------------------------------

/**
 * Accumulator for a group of calls. `usd` es la suma de lo que sí tiene tarifa; `usdUnknownCalls`
 * cuenta las llamadas que quedaron fuera (modelo sin precio) y `unknownModels` cuáles fueron.
 */
export function newBucket() {
  return { calls: 0, usage: emptyUsage(), usd: 0, usdUnknownCalls: 0, unknownModels: /** @type {Set<string>} */ (new Set()), contextPeak: 0 };
}

/** Adds one call into a bucket (mutates). Cost is recomputed from the pricing table each time. */
export function addCall(bucket, call) {
  bucket.calls++;
  addUsage(bucket.usage, call.usage);
  const usd = costFor(call.model, call.usage);
  if (usd === null) {
    bucket.usdUnknownCalls++;
    bucket.unknownModels.add(call.model);
  } else bucket.usd += usd;
  bucket.contextPeak = Math.max(bucket.contextPeak, totalInput(call.usage));
  return bucket;
}

/**
 * Cierra un bucket para salida: `usd` queda como la parte conocida (nunca null) y `usdPartial`
 * avisa que faltan llamadas por tarifar — quien muestre el número decide cómo marcarlo.
 * @template {ReturnType<typeof newBucket>} B
 * @param {B} bucket
 */
function finish(bucket) {
  return {
    ...bucket,
    usdPartial: bucket.usdUnknownCalls > 0,
    unknownModels: [...bucket.unknownModels],
    cacheHitPct: cacheHitPct(bucket.usage),
    totalInput: totalInput(bucket.usage),
  };
}

/**
 * @param {SessionRef} ref
 * @param {{includeSubagents?: boolean, since?: number|null, cacheBreakThreshold?: number}} [opts]
 */
export function parseSession(ref, opts = {}) {
  const includeSubagents = opts.includeSubagents !== false;
  const threshold = opts.cacheBreakThreshold ?? 100000;
  const seenUuids = new Set();
  const main = parseFile(ref.path, { since: opts.since, seenUuids });

  const total = newBucket();
  const mainBucket = newBucket();
  const subBucket = newBucket();
  /** @type {Record<string, ReturnType<typeof newBucket>>} */
  const byModel = {};
  /** @type {Record<string, ReturnType<typeof newBucket> & {agents: number}>} */
  const byAgentType = {};
  /** @type {Record<string, ReturnType<typeof newBucket>>} */
  const bySkill = {};
  const cacheBreaks = [];
  const agents = [];
  const tools = {
    calls: { ...main.tools.calls },
    filesRead: new Set(main.tools.filesRead),
    filesEdited: new Set(main.tools.filesEdited),
    // What the main agent wrote itself — the number 0.2.x wants to push down (orchestrate, don't edit).
    filesEditedMain: [...main.tools.filesEdited],
    bash: { ...main.tools.bash },
  };

  const ingest = (call, agentType) => {
    addCall(total, call);
    addCall(agentType ? subBucket : mainBucket, call);
    addCall((byModel[call.model] ||= newBucket()), call);
    if (call.skill) addCall((bySkill[call.skill] ||= newBucket()), call);
    const uncached = call.usage.input + call.usage.cacheWrite5m + call.usage.cacheWrite1h;
    if (uncached > threshold) cacheBreaks.push({ ts: call.ts, uncached, total: totalInput(call.usage), agentType: agentType || null });
  };

  for (const c of main.calls) ingest(c, null);

  let firstTs = main.firstTs;
  let lastTs = main.lastTs;
  if (includeSubagents) {
    for (const f of subagentFiles(ref)) {
      const link = main.agentLinks.get(f.agentId);
      const agentType = f.meta?.agentType || link?.agentType || 'unknown';
      const parsed = parseFile(f.path, { agentId: f.agentId, since: opts.since, seenUuids });
      if (!parsed.calls.length && parsed.firstTs === null) continue;
      const b = newBucket();
      for (const c of parsed.calls) {
        ingest(c, agentType);
        addCall(b, c);
      }
      const at = (byAgentType[agentType] ||= { ...newBucket(), agents: 0 });
      at.agents++;
      for (const c of parsed.calls) addCall(at, c);
      agents.push({
        agentId: f.agentId,
        agentType,
        description: f.meta?.description || link?.description || '',
        model: parsed.meta.model || link?.model || null,
        ...finish(b),
        durationMs: parsed.firstTs !== null && parsed.lastTs !== null ? parsed.lastTs - parsed.firstTs : null,
      });
      for (const [k, v] of Object.entries(parsed.tools.calls)) tools.calls[k] = (tools.calls[k] || 0) + v;
      for (const p of parsed.tools.filesRead) tools.filesRead.add(p);
      for (const p of parsed.tools.filesEdited) tools.filesEdited.add(p);
      tools.bash.calls += parsed.tools.bash.calls;
      tools.bash.resultLines += parsed.tools.bash.resultLines;
      tools.bash.resultChars += parsed.tools.bash.resultChars;
      tools.bash.interrupted += parsed.tools.bash.interrupted;
      if (parsed.firstTs !== null && (firstTs === null || parsed.firstTs < firstTs)) firstTs = parsed.firstTs;
      if (parsed.lastTs !== null && (lastTs === null || parsed.lastTs > lastTs)) lastTs = parsed.lastTs;
    }
  }

  const mapFinish = (obj) => Object.fromEntries(Object.entries(obj).map(([k, v]) => [k, finish(v)]));
  const usage = finish(total);
  return {
    sessionId: ref.sessionId,
    project: ref.project,
    cwd: main.meta.cwd,
    version: main.meta.version,
    gitBranch: main.meta.gitBranch,
    firstTs,
    lastTs,
    wallMs: firstTs !== null && lastTs !== null ? lastTs - firstTs : 0,
    activeMs: main.activeMs,
    turns: main.turns,
    prompts: main.prompts,
    usage,
    main: finish(mainBucket),
    subagents: finish(subBucket),
    byModel: mapFinish(byModel),
    byAgentType: mapFinish(byAgentType),
    bySkill: mapFinish(bySkill),
    agents: agents.sort((a, b) => b.totalInput + b.usage.output - (a.totalInput + a.usage.output)),
    tools: { ...tools, filesRead: [...tools.filesRead], filesEdited: [...tools.filesEdited] },
    cacheBreaks,
    unknownModels: usage.unknownModels,
  };
}

// ---------------------------------------------------------------------------
// Incremental reading (statusline)
// ---------------------------------------------------------------------------

/**
 * @typedef {object} IncrementalState
 * @property {number} offset          bytes already folded
 * @property {string} partial         trailing incomplete line
 * @property {Record<string, number>} seen  requestId → largest output seen (bounded)
 * @property {ReturnType<typeof emptyUsage>} usage
 * @property {number} usd             parte con tarifa
 * @property {number} usdUnknown      llamadas sin tarifa (→ `$X+?`)
 * @property {string|null} model
 * @property {number} calls
 * @property {number} turns           prompts humanos vistos (sólo transcript principal)
 * @property {number} turnUsd         USD del turno actual (desde el último prompt humano)
 * @property {number} turnUsdUnknown  llamadas sin tarifa en el turno actual
 * @property {number|null} lastTs     timestamp (ms) de la última llamada API vista
 * @property {number|null} turnStartTs timestamp (ms) del prompt que abrió el turno actual
 */

/**
 * Reads only the bytes appended since `state.offset`, folds complete assistant lines into
 * `state.usage`, and keeps a bounded `seen` map so a streamed message that grows is counted
 * once (by output delta), not twice. Human prompts (main transcript only) start a new turn.
 *
 * With `opts.turnSinceTs` (subagent transcripts) the turn is the parent's: `turnUsd` restarts
 * whenever the parent's turn changes and only calls at or after that timestamp count, so a
 * rebuilt cache never dumps an agent's whole history into the current turn.
 * @param {string} path
 * @param {IncrementalState} state
 * @param {{turnSinceTs?: number|null}} [opts]
 */
export function readIncremental(path, state, opts = {}) {
  const since = opts.turnSinceTs ?? null;
  if (since !== null && state.turnStartTs !== since) {
    state.turnStartTs = since;
    state.turnUsd = 0;
    state.turnUsdUnknown = 0;
  }
  let fd;
  try {
    fd = openSync(path, 'r');
  } catch {
    return state;
  }
  try {
    const size = statSync(path).size;
    if (size < state.offset) {
      // file was rewritten (new session id reuse is unlikely, but be safe)
      state.offset = 0;
      state.partial = '';
    }
    const len = size - state.offset;
    if (len <= 0) return state;
    const buf = Buffer.alloc(len);
    readSync(fd, buf, 0, len, state.offset);
    state.offset = size;
    const text = state.partial + buf.toString('utf8');
    const lines = text.split('\n');
    state.partial = lines.pop() || '';
    for (const line of lines) {
      const isUser = line.includes('"type":"user"');
      if (!isUser && !line.includes('"usage"')) continue;
      let e;
      try {
        e = JSON.parse(line);
      } catch {
        continue;
      }
      if (e.type === 'user') {
        // a plain human prompt on the main transcript starts a new turn; tool results and
        // subagent sidechains never do
        if (e.isSidechain || e.toolUseResult || humanText(e) === null) continue;
        state.turns = (state.turns || 0) + 1;
        state.turnUsd = 0;
        state.turnUsdUnknown = 0;
        const pts = typeof e.timestamp === 'string' ? Date.parse(e.timestamp) : NaN;
        state.turnStartTs = Number.isNaN(pts) ? null : pts;
        continue;
      }
      if (e.type !== 'assistant' || !e.message?.usage) continue;
      const key = e.requestId || e.message.id || e.uuid;
      const usage = toUsage(e.message.usage);
      const model = e.message.model || 'unknown';
      if (isSyntheticModel(model) && isZeroUsage(usage)) continue; // mensaje interno, no una llamada
      const ts = typeof e.timestamp === 'string' ? Date.parse(e.timestamp) : NaN;
      // a call belongs to the current turn unless it predates the parent's prompt
      const inTurn = since === null || Number.isNaN(ts) || ts >= since;
      const prevOut = state.seen[key];
      let usd = null;
      if (prevOut === undefined) {
        addUsage(state.usage, usage);
        usd = costFor(model, usage);
        if (usd === null) {
          state.usdUnknown = (state.usdUnknown || 0) + 1; // `|| 0`: caches previos a este campo
          if (inTurn) state.turnUsdUnknown = (state.turnUsdUnknown || 0) + 1;
        }
        state.calls++;
      } else if (usage.output > prevOut) {
        const delta = { ...emptyUsage(), output: usage.output - prevOut };
        addUsage(state.usage, delta);
        usd = costFor(model, { ...delta, thinking: 0 });
      }
      if (usd !== null) {
        state.usd += usd;
        if (inTurn) state.turnUsd = (state.turnUsd || 0) + usd;
      }
      state.seen[key] = Math.max(prevOut ?? 0, usage.output);
      if (!isSyntheticModel(model)) state.model = model;
      if (!Number.isNaN(ts) && (state.lastTs === null || state.lastTs === undefined || ts > state.lastTs)) state.lastTs = ts;
    }
    const keys = Object.keys(state.seen);
    if (keys.length > 256) for (const k of keys.slice(0, keys.length - 256)) delete state.seen[k];
  } finally {
    closeSync(fd);
  }
  return state;
}

/** Fresh state for {@link readIncremental}. */
/** @returns {IncrementalState} */
export function newIncrementalState() {
  return { offset: 0, partial: '', seen: {}, usage: emptyUsage(), usd: 0, usdUnknown: 0, model: null, calls: 0, turns: 0, turnUsd: 0, turnUsdUnknown: 0, lastTs: null, turnStartTs: null };
}
