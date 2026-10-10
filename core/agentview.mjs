// @ts-check
// Modelo puro de la pestaña Agentes (sin módulos de Node): el «libro» en memoria de los agentes de la
// sesión y las vistas (filas, detalle) que dibuja el panel. El driver lo alimenta; acá no hay I/O.

import { priceIn, normalizeModel } from './price-table.mjs';

/**
 * @typedef {object} ToolNote
 * @property {string} label
 * @property {number} at
 */
/**
 * @typedef {object} SentNote
 * @property {string} text
 * @property {number} at
 * @property {'en cola'|'recibido'|'no entregado'} state
 * @property {string} [reason]
 */
/**
 * @typedef {object} AgentEntry
 * @property {string} id
 * @property {string} type
 * @property {string} name
 * @property {string} description
 * @property {string} prompt
 * @property {string} model        modelo resuelto en el spawn
 * @property {string} realModel    modelo real que reportó el último turno
 * @property {string} [spawnedBy]
 * @property {boolean} confirmed   visto en un spawn o en `agent.list`; los eventos sueltos no lo muestran
 * @property {number} sentCount    mensajes enviados en total (no se recorta)
 * @property {number|null} spawnedAt
 * @property {number|null} firstSeen
 * @property {string} status
 * @property {ToolNote[]} tools
 * @property {ToolNote|null} lastTool
 * @property {number} hookAt       último evento de herramienta que vio el gancho (0 = ninguno)
 * @property {number} turns
 * @property {number} inTokens
 * @property {number} outTokens
 * @property {number} usd
 * @property {boolean} estimated
 * @property {boolean} costUnknown
 * @property {string} answer
 * @property {string} result
 * @property {string} reason
 * @property {number|null} endedAt
 * @property {SentNote[]} sent
 */
/**
 * @typedef {object} Book
 * @property {Map<string, AgentEntry>} agents
 * @property {Record<string, {model: string, effort: string}>} roles
 * @property {number} cap
 */

const CAP = 40;
const TOOL_RING = 6;
const PROMPT_MAX = 1500;
const ANSWER_MAX = 1200;
const DATA_MAX = 120;
const SENT_MAX = 5;
const TRAIL_QUIET_MS = 6000;

/** @param {any} v @param {number} n */
const cut = (v, n) => {
  const s = typeof v === 'string' ? v : '';
  return s.length > n ? s.slice(0, n - 1) + '…' : s;
};
/** @param {any} v */
const num = (v) => (typeof v === 'number' && Number.isFinite(v) ? v : 0);

/**
 * model/effort del frontmatter de `agents/<rol>.md`.
 * @param {string} mdText
 * @returns {{model: string, effort: string}}
 */
export function parseAgentMeta(mdText) {
  const out = { model: '', effort: '' };
  const m = /^---\r?\n([\s\S]*?)\r?\n---/.exec(String(mdText ?? ''));
  if (!m) return out;
  for (const line of m[1].split(/\r?\n/)) {
    const kv = /^(model|effort)\s*:\s*(.*?)\s*$/.exec(line);
    if (kv) out[/** @type {'model'|'effort'} */ (kv[1])] = kv[2].replace(/^["']|["']$/g, '');
  }
  return out;
}

/**
 * @param {string} type
 * @returns {{role: string, nxy: boolean}}
 */
export function roleOf(type) {
  const t = String(type ?? '');
  const m = /^nxy:(.+)$/.exec(t);
  return m ? { role: m[1], nxy: true } : { role: t || 'agent', nxy: false };
}

/**
 * @param {string} status AgentStatus
 * @param {string} [lastReason] razón del último turno (`error`, `aborted`, …)
 * @returns {{text: string, tone: 'running'|'ok'|'error'|'dim', live: boolean}}
 */
export function statusView(status, lastReason) {
  if (status === 'running') return { text: 'corriendo', tone: 'running', live: true };
  if (status === 'waiting') return { text: 'esperando', tone: 'running', live: true };
  if (status === 'pending') return { text: 'pendiente', tone: 'running', live: true };
  if (status === 'failed' || lastReason === 'error') return { text: 'falló', tone: 'error', live: false };
  if (status === 'killed') return { text: 'detenido', tone: 'dim', live: false };
  if (lastReason === 'aborted') return { text: 'interrumpido', tone: 'dim', live: false };
  return { text: 'terminó', tone: 'ok', live: false };
}

/**
 * Etiqueta corta de una llamada a herramienta; el dato se topa en 120 caracteres.
 * @param {string} tool
 * @param {any} [input]
 */
export function toolLabel(tool, input) {
  const t = String(tool ?? '');
  const i = input && typeof input === 'object' ? input : {};
  const d = (/** @type {any} */ v) => cut(typeof v === 'string' ? v.split(/\r?\n/)[0].trim() : '', DATA_MAX);
  if (!t || t.startsWith('mcp__')) return t;
  if (t === 'Bash') return d(i.command) ? `Bash: ${d(i.command)}` : 'Bash';
  const pick = {
    Edit: i.file_path, Write: i.file_path, MultiEdit: i.file_path, Read: i.file_path,
    NotebookEdit: i.notebook_path, Grep: i.pattern, Glob: i.pattern,
    WebFetch: i.url, WebSearch: i.query, Agent: i.description, Task: i.description,
  }[/** @type {'Edit'} */ (t)];
  return d(pick) ? `${t} ${d(pick)}` : t;
}

/**
 * Costo de un turno: los 4 conteos × el precio del modelo (escritura de cache según el TTL).
 * @param {any} usage
 * @param {string} model
 * @param {Record<string, import('./price-table.mjs').ModelPrice>|null|undefined} prices
 * @param {'5m'|'1h'|null} [ttl]
 * @returns {{usd: number, estimated: boolean}|null}
 */
export function costOf(usage, model, prices, ttl) {
  if (!usage || !prices) return null;
  const hit = priceIn(prices, usage.model || model);
  if (!hit) return null;
  // Tarifa base a propósito (sin tierFor): `usage` es el turno entero del agente y puede sumar
  // varios requests; decidir el tramo con la suma cobraría de más. Es una cota inferior para un
  // Haiku 5.5 con algún request de prompt > 100K.
  const p = hit.price;
  const w = ttl === '1h' ? p.cache_write_1h : p.cache_write_5m;
  const usd = (num(usage.input_tokens) * p.input + num(usage.cache_read_input_tokens) * p.cache_read
    + num(usage.cache_creation_input_tokens) * w + num(usage.output_tokens) * p.output) / 1_000_000;
  return { usd, estimated: hit.estimated };
}

// ── libro ──────────────────────────────────────────────────────────────────

/** @returns {Book} */
export function createBook() {
  return { agents: new Map(), roles: {}, cap: CAP };
}

/** @param {AgentEntry} e */
const isLive = (e) => statusView(e.status, e.reason).live;
/** @param {AgentEntry} e */
const stamp = (e) => e.endedAt ?? e.spawnedAt ?? e.firstSeen ?? 0;

/** @param {Book} book @param {string} id @param {number} [now] */
function ensure(book, id, now) {
  let e = book.agents.get(id);
  if (!e) {
    e = {
      id, type: '', name: '', description: '', prompt: '', model: '', realModel: '', spawnedAt: null,
      firstSeen: now ?? null, status: 'running', tools: [], lastTool: null, hookAt: 0, turns: 0,
      inTokens: 0, outTokens: 0, usd: 0, estimated: false, costUnknown: false, answer: '', result: '',
      reason: '', endedAt: null, sent: [], sentCount: 0, confirmed: false,
    };
    book.agents.set(id, e);
  }
  return e;
}

/** @param {Book} book */
function enforceCap(book) {
  while (book.agents.size > book.cap) {
    const all = [...book.agents.values()];
    const done = all.filter((e) => !isLive(e));
    const pool = done.length ? done : all;
    const victim = pool.reduce((a, b) => (stamp(b) < stamp(a) ? b : a));
    book.agents.delete(victim.id);
  }
}

/** @param {AgentEntry} e @param {number} at */
function markReceived(e, at) {
  for (const s of e.sent) if (s.state === 'en cola' && s.at <= at) s.state = 'recibido';
}

/**
 * @param {Book} book
 * @param {{id: string, type?: string, description?: string, prompt?: string, model?: string,
 *   spawnedBy?: string, at?: number}} a
 */
export function bookSpawn(book, a) {
  if (!a?.id) return;
  const e = ensure(book, a.id);
  e.confirmed = true;
  // Lo que anotó el driver (nxy) manda: una nota posterior ajena sólo completa lo que falte.
  if (e.spawnedBy === 'nxy' && a.spawnedBy !== 'nxy') {
    e.description = e.description || a.description || '';
    e.prompt = e.prompt || cut(a.prompt, PROMPT_MAX);
    e.model = e.model || a.model || '';
    return;
  }
  e.type = a.type ?? e.type;
  e.description = a.description ?? e.description;
  e.prompt = cut(a.prompt, PROMPT_MAX) || e.prompt;
  e.model = a.model ?? e.model;
  e.spawnedBy = a.spawnedBy ?? e.spawnedBy;
  e.spawnedAt = a.at ?? Date.now();
  e.status = 'running';
  e.endedAt = null;
  enforceCap(book);
}

/**
 * @param {Book} book
 * @param {string} id
 * @param {{tool: string, input?: any, at?: number}} a
 */
export function bookTool(book, id, a) {
  if (!id || !a?.tool) return;
  const at = a.at ?? Date.now();
  const e = ensure(book, id, at);
  const note = { label: toolLabel(a.tool, a.input), at };
  e.tools.push(note);
  if (e.tools.length > TOOL_RING) e.tools.splice(0, e.tools.length - TOOL_RING);
  e.lastTool = note;
  e.hookAt = at;
  markReceived(e, at);
  enforceCap(book);
}

/**
 * @param {Book} book
 * @param {{agentId?: string, usage?: any, answer?: string, text?: string, reason?: string, at?: number}} e
 * @param {{prices?: any, ttl?: '5m'|'1h'|null}} [ctx]
 */
export function bookTurn(book, e, ctx = {}) {
  const id = e?.agentId;
  if (!id) return;
  const at = e.at ?? Date.now();
  const a = ensure(book, id, at);
  markReceived(a, at);
  const u = e.usage;
  if (u) {
    a.inTokens += num(u.input_tokens) + num(u.cache_read_input_tokens) + num(u.cache_creation_input_tokens);
    a.outTokens += num(u.output_tokens);
    if (u.model) a.realModel = u.model;
    const c = costOf(u, a.realModel || a.model, ctx.prices, ctx.ttl);
    if (c) {
      a.usd += c.usd;
      a.estimated = a.estimated || c.estimated;
    } else a.costUnknown = true;
  }
  a.turns += 1;
  a.answer = cut(e.answer ?? e.text, ANSWER_MAX) || a.answer;
  a.reason = e.reason ?? '';
  a.endedAt = at;
  if (a.status === 'running' || a.status === 'pending' || a.status === 'waiting') {
    a.status = a.reason === 'error' ? 'failed' : 'completed';
  }
  enforceCap(book);
}

/**
 * Mezcla `$.agent.list()` en el libro.
 * @param {Book} book
 * @param {any[]} infos
 * @param {number} [now]
 */
export function bookList(book, infos, now = Date.now()) {
  if (!Array.isArray(infos)) return;
  for (const i of infos) {
    if (!i?.id) continue;
    const e = ensure(book, i.id, now);
    e.confirmed = true;
    if (i.status) e.status = i.status;
    if (typeof i.teammateId === 'string') e.name = i.teammateId;
    if (i.name) e.name = i.name;
    if (i.description) e.description = e.description || i.description;
    if (i.type) e.type = e.type || i.type;
    if (isLive(e)) e.endedAt = null;
    else if (e.endedAt == null) e.endedAt = now;
  }
  enforceCap(book);
}

/**
 * Respaldo por `session.messages`: herramientas, prompt (primer mensaje) y resultado (último texto).
 * @param {Book} book
 * @param {string} id
 * @param {any} messages SessionMessage[] o `{deny}`
 * @param {number} [now]
 */
export function bookTrail(book, id, messages, now = Date.now()) {
  if (!Array.isArray(messages)) return;
  const e = book.agents.get(id);
  if (!e) return;
  const first = messages.find((m) => m?.role === 'user' && m.text);
  if (first && !e.prompt) e.prompt = cut(first.text, PROMPT_MAX);
  const uses = messages.flatMap((m) => (m?.role === 'assistant' && Array.isArray(m.toolUses) ? m.toolUses : []));
  if (uses.length) {
    const last = uses.slice(-TOOL_RING).map((u) => toolLabel(u.tool, u.input));
    const prev = new Map(e.tools.map((t) => [t.label, t.at]));
    e.tools = last.map((label) => ({ label, at: prev.get(label) ?? now }));
    const tail = e.tools[e.tools.length - 1];
    if (!e.lastTool || e.lastTool.label !== tail.label) e.lastTool = tail;
  }
  const lastText = [...messages].reverse().find((m) => m?.role === 'assistant' && m.text);
  if (lastText) e.result = cut(lastText.text, ANSWER_MAX);
}

/**
 * Registra un mensaje enviado a un agente.
 * @param {Book} book
 * @param {string} id
 * @param {{text: string, at?: number, delivered?: boolean, reason?: string}} a
 */
export function bookSent(book, id, a) {
  if (!id || !a) return;
  const at = a.at ?? Date.now();
  const e = ensure(book, id, at);
  /** @type {SentNote} */
  const note = { text: cut(a.text, DATA_MAX * 4), at, state: a.delivered === false ? 'no entregado' : 'en cola' };
  if (a.delivered === false) note.reason = a.reason ?? '';
  e.sent.push(note);
  e.sentCount += 1;
  if (e.sent.length > SENT_MAX) e.sent.splice(0, e.sent.length - SENT_MAX);
  if (a.delivered !== false) {
    // Reanuda a un agente terminado: queda corriendo hasta el próximo list.
    e.status = 'running';
    e.endedAt = null;
  }
  enforceCap(book);
}

/**
 * Model · effort declarados de los roles nxy.
 * @param {Book} book
 * @param {Record<string, {model?: string, effort?: string}>|null|undefined} roles
 */
export function bookRoles(book, roles) {
  if (!roles || typeof roles !== 'object') return;
  /** @type {Record<string, {model: string, effort: string}>} */
  const out = {};
  for (const [k, v] of Object.entries(roles)) out[k] = { model: String(v?.model ?? ''), effort: String(v?.effort ?? '') };
  book.roles = out;
}

// ── vistas ─────────────────────────────────────────────────────────────────

/** @param {number} n */
const kfmt = (n) => (n >= 1_000_000 ? `${(n / 1_000_000).toFixed(1)}M` : n >= 1000 ? `${(n / 1000).toFixed(1)}k` : String(n));
/** @param {number} usd */
const usdText = (usd) => (usd > 0 && usd < 0.01 ? '<$0.01' : `$${usd.toFixed(2)}`);

/** @param {any} inFlight @param {string} id */
function flightOf(inFlight, id) {
  if (!inFlight) return null;
  if (Array.isArray(inFlight)) return inFlight.find((a) => a && (a.agentId === id || a.id === id)) ?? null;
  return inFlight[id] ?? null;
}

/** @param {AgentEntry} e @param {number} now @param {any} inFlight @param {Book} book */
function rowOf(e, now, inFlight, book) {
  const { role, nxy } = roleOf(e.type);
  const st = statusView(e.status, e.reason);
  const declared = nxy ? book.roles[role] : null;
  const rawModel = e.realModel || e.model || declared?.model || '';
  const model = rawModel ? normalizeModel(rawModel) : '';
  const effort = nxy ? declared?.effort || '' : '';
  const start = e.spawnedAt ?? e.firstSeen ?? now;
  const elapsedMs = Math.max(0, (st.live ? now : e.endedAt ?? now) - start);
  const secs = (/** @type {number} */ ms) => Math.max(0, Math.round(ms / 1000));
  let activity;
  if (e.lastTool && st.live) activity = `${e.lastTool.label} · hace ${secs(now - e.lastTool.at)}s`;
  else if (st.live) activity = `trabajando… ${secs(elapsedMs)}s`;
  else activity = cut((e.answer || e.result).split(/\r?\n/).find((l) => l.trim()) ?? '', DATA_MAX) || st.text;
  const costText = e.turns === 0 ? '…' : e.costUnknown && !e.usd ? '?' : `${e.estimated ? '~' : ''}${usdText(e.usd)}`;
  const f = flightOf(inFlight, e.id);
  return {
    id: e.id, role, nxy,
    label: e.description || e.name || e.id,
    batch: f?.batch ?? null,
    state: st.text, tone: st.tone, live: st.live,
    model, effort, modelEffort: effort ? `${model} · ${effort}` : model,
    activity, elapsedMs, costText,
    tokensText: e.turns ? `${kfmt(e.inTokens)} in · ${kfmt(e.outTokens)} out` : '',
  };
}

/**
 * Filas de la lista: vivos primero, luego los más recientes.
 * @param {Book} book
 * @param {{now?: number, inFlight?: any, snap?: any}} [ctx]
 */
export function agentRows(book, ctx = {}) {
  const now = ctx.now ?? Date.now();
  return [...book.agents.values()]
    .filter((e) => e.confirmed)
    .map((e) => ({ e, live: isLive(e) }))
    .sort((a, b) => (a.live === b.live ? stamp(b.e) - stamp(a.e) : a.live ? -1 : 1))
    .map(({ e }) => rowOf(e, now, ctx.inFlight, book));
}

/**
 * ¿Hay algún agente visible y vivo? (los de eventos sueltos sin confirmar no cuentan)
 * @param {Book} book
 */
export function bookLive(book) {
  for (const e of book.agents.values()) if (e.confirmed && isLive(e)) return true;
  return false;
}

/**
 * Un implementer de un plan que corre el orquestador: hablarle puede romper el lote.
 * @param {{id?: string, type?: string, role?: string, nxy?: boolean}|null|undefined} entry
 * @param {{owned?: boolean, inFlight?: any}} [ctx]
 */
export function needsConfirm(entry, ctx = {}) {
  if (!entry || !ctx.owned || !entry.id) return false;
  const role = entry.type != null ? roleOf(entry.type) : { role: entry.role ?? '', nxy: !!entry.nxy };
  return role.nxy && role.role === 'implementer' && !!flightOf(ctx.inFlight, entry.id);
}

/**
 * Detalle de un agente.
 * @param {Book} book
 * @param {string} id
 * @param {{now?: number, inFlight?: any, snap?: any, owned?: boolean}} [ctx]
 */
export function agentDetail(book, id, ctx = {}) {
  const e = book.agents.get(id);
  if (!e) return null;
  const now = ctx.now ?? Date.now();
  const card = rowOf(e, now, ctx.inFlight, book);
  const failed = card.tone === 'error';
  const text = e.result || e.answer;
  return {
    card,
    prompt: e.prompt,
    tools: e.tools.map((t) => ({ label: t.label, ago: Math.max(0, Math.round((now - t.at) / 1000)) })),
    result: failed ? '' : text,
    error: failed ? text || e.reason || 'error' : '',
    batch: card.batch,
    sent: e.sent.map((s) => ({ text: s.text, state: s.state, reason: s.reason ?? '' })),
    sentCount: e.sentCount,
    canSend: e.status !== 'killed',
    confirm: needsConfirm(e, { owned: ctx.owned, inFlight: ctx.inFlight }),
  };
}

/**
 * ¿Hace falta leer `session.messages` para saber qué hace? Vivo y sin herramienta vista por el gancho en 6 s.
 * @param {Book} book
 * @param {string} id
 * @param {number} [now]
 */
export function needsTrail(book, id, now = Date.now()) {
  const e = book.agents.get(id);
  if (!e || !isLive(e)) return false;
  return !e.hookAt || now - e.hookAt > TRAIL_QUIET_MS;
}
