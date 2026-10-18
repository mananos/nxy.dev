// @ts-check
/**
 * Runtime state of recall, per checkout (`.nxy/local/`, never committed):
 *
 *  - per session: which memories were already pointed at (a pointer is shown once per session)
 *    and which files the session has been editing (the file signal of recall);
 *  - a ledger of pointers and fetches, which is how the relevance rule gets judged by use instead
 *    of by a user tuning a number: a pointer that is followed was worth its ~30 tokens.
 *
 * Everything here is best-effort. Losing a state file costs, at worst, one repeated pointer.
 */
import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import { isAbsolute, join, relative, resolve, sep } from 'node:path';
import { appendJsonl } from '../../core/jsonl.mjs';
import { ensureDir, nxyRuntimeDir } from '../../core/paths.mjs';
import { repoRoot } from '../../core/memory/scope.mjs';

/** Two fetches this close together count as "used together" when no session id is at hand. */
export const CO_USE_WINDOW_MS = 2 * 3_600_000;

const MAX_FILES = 50;

const safeId = (s) => String(s || 'unknown').replace(/[^\w.-]/g, '_').slice(0, 100);
const statePath = (cwd, session) => join(nxyRuntimeDir(cwd), 'recall', `${safeId(session)}.json`);
export const recallLedgerPath = (cwd) => join(nxyRuntimeDir(cwd), 'metrics', 'recall.jsonl');

/**
 * @param {string} cwd
 * @param {string} session
 * @returns {{pointed: string[], files: string[]}}
 */
export function readSession(cwd, session) {
  try {
    const p = statePath(cwd, session);
    if (!existsSync(p)) return { pointed: [], files: [] };
    const s = JSON.parse(readFileSync(p, 'utf8'));
    return { pointed: Array.isArray(s.pointed) ? s.pointed : [], files: Array.isArray(s.files) ? s.files : [] };
  } catch {
    return { pointed: [], files: [] };
  }
}

/** @param {string} cwd @param {string} session @param {{pointed: string[], files: string[]}} s */
function writeSession(cwd, session, s) {
  try {
    ensureDir(join(nxyRuntimeDir(cwd), 'recall'));
    writeFileSync(statePath(cwd, session), JSON.stringify(s), 'utf8');
  } catch {
    /* best-effort */
  }
}

/**
 * Remembers a file the session edits, repo-relative with forward slashes (how file edges are
 * stored). Newest last, bounded. Files outside the repo are ignored.
 * @param {string} cwd
 * @param {string} session
 * @param {string} filePath  absolute or relative to cwd
 */
export function noteFile(cwd, session, filePath) {
  const root = repoRoot(cwd);
  if (!session || !filePath || !root) return;
  const rel = relative(root, resolve(cwd, filePath)).split(sep).join('/');
  if (!rel || rel.startsWith('..') || isAbsolute(rel)) return;
  const s = readSession(cwd, session);
  s.files = s.files.filter((f) => f !== rel).concat(rel).slice(-MAX_FILES);
  writeSession(cwd, session, s);
}

/** Records pointers shown to a session, in its state and in the ledger. */
export function notePointers(cwd, session, ids, chars) {
  if (!ids.length) return;
  const s = readSession(cwd, session);
  s.pointed = [...new Set([...s.pointed, ...ids])];
  writeSession(cwd, session, s);
  try {
    appendJsonl(recallLedgerPath(cwd), { ts: Date.now(), ev: 'pointer', session, ids, tokens: Math.ceil(chars / 4) });
  } catch {
    /* telemetry */
  }
}

/**
 * Why a memory was fetched. `link` is the librarian reading candidates to relate a new memory:
 * bookkeeping, not use — it neither counts as a miss nor makes co-use edges.
 * @typedef {'link'|'search'|null} FetchPurpose
 */

/**
 * Records a fetch and returns the other ids fetched within the co-use window, so the caller can
 * strengthen those edges.
 * @param {string} cwd
 * @param {string} id
 * @param {number} [now]
 * @param {FetchPurpose} [purpose]
 * @returns {string[]}
 */
export function noteFetch(cwd, id, now = Date.now(), purpose = null) {
  const recent = new Set();
  if (purpose !== 'link') {
    for (const r of readLedger(cwd)) {
      if (r.ev === 'get' && r.for !== 'link' && r.id !== id && now - r.ts <= CO_USE_WINDOW_MS) recent.add(r.id);
    }
  }
  try {
    appendJsonl(recallLedgerPath(cwd), { ts: now, ev: 'get', id, ...(purpose ? { for: purpose } : {}) });
  } catch {
    /* telemetry */
  }
  return [...recent];
}

/** Records a `mem related` lookup (the planner asking for closed tasks) and the task ids it delivered. */
export function noteRelated(cwd, ids) {
  try {
    appendJsonl(recallLedgerPath(cwd), { ts: Date.now(), ev: 'related', ids });
  } catch {
    /* telemetry */
  }
}

const MAX_ASKED = 200;
const chatRulesPath = (cwd) => join(nxyRuntimeDir(cwd), 'chat-rules-asked.json');

/**
 * True only the first time for a task: the chat-rules question is asked once per task. Task ids
 * are kept per checkout (last 200, a few KB, never committed).
 * @param {string} cwd
 * @param {string} taskId
 */
export function claimChatRulesAsk(cwd, taskId) {
  try {
    let ids = [];
    try {
      const s = JSON.parse(readFileSync(chatRulesPath(cwd), 'utf8'));
      if (Array.isArray(s)) ids = s.map(String);
    } catch {
      /* none yet */
    }
    if (ids.includes(taskId)) return false;
    ensureDir(nxyRuntimeDir(cwd));
    writeFileSync(chatRulesPath(cwd), JSON.stringify([...ids, taskId].slice(-MAX_ASKED)), 'utf8');
    return true;
  } catch {
    return false;
  }
}

/** Records the rules offered from the chat (the labels of the question's options). */
export function noteChatOffers(cwd, rules) {
  if (!rules.length) return;
  try {
    appendJsonl(recallLedgerPath(cwd), { ts: Date.now(), ev: 'convention-offer', source: 'chat', rules });
  } catch {
    /* telemetry */
  }
}

/** Records the plan decisions offered as conventions at a checkpoint. */
export function noteConventionOffers(cwd, rules, source = 'plan') {
  if (!rules.length) return;
  try {
    appendJsonl(recallLedgerPath(cwd), { ts: Date.now(), ev: 'convention-offer', source, rules });
  } catch {
    /* telemetry */
  }
}

/** Records a convention saved to memory. */
export function noteConventionSave(cwd, title) {
  try {
    appendJsonl(recallLedgerPath(cwd), { ts: Date.now(), ev: 'convention-save', title });
  } catch {
    /* telemetry */
  }
}

const fold = (s) => String(s).normalize('NFD').replace(/[̀-ͯ]/g, '').trim().toLowerCase();

/**
 * Conventions offered vs saved. Offers count distinct folded rule texts, so a checkpoint printed
 * again does not count twice.
 * @param {string} cwd
 * @param {number} [sinceMs]
 * @param {number} [now]
 */
export function conventionSummary(cwd, sinceMs = 30 * 86_400_000, now = Date.now()) {
  const rows = readLedger(cwd).filter((r) => now - r.ts <= sinceMs);
  const plans = new Set();
  const chat = new Set();
  for (const r of rows) {
    if (r.ev !== 'convention-offer' || !Array.isArray(r.rules)) continue;
    for (const rule of r.rules) (r.source === 'chat' ? chat : plans).add(fold(rule));
  }
  const saved = new Set(rows.filter((r) => r.ev === 'convention-save').map((r) => fold(r.title)));
  return { offered: plans.size + chat.size, fromPlans: plans.size, fromChat: chat.size, saved: saved.size };
}

/** @returns {any[]} */
function readLedger(cwd) {
  try {
    const p = recallLedgerPath(cwd);
    if (!existsSync(p)) return [];
    return readFileSync(p, 'utf8').split('\n').filter(Boolean).map((l) => {
      try {
        return JSON.parse(l);
      } catch {
        return null;
      }
    }).filter(Boolean);
  } catch {
    return [];
  }
}

/**
 * How recall is doing, in the three numbers that decide whether its rule needs to change:
 *  - `followed`: pointers the model then loaded — the pointer was worth its tokens (precision);
 *  - `misses`: memories loaded that no pointer had announced in the window before — the model (or
 *    the librarian, or the user) needed them and the hook did not see it coming (recall's gap).
 *    `missesBySearch` is the subset the librarian found by meaning: the ones words and files missed;
 *  - `tokens`: what the pointers cost.
 * @param {string} cwd
 * @param {number} [sinceMs]
 */
export function recallSummary(cwd, sinceMs = 30 * 86_400_000, now = Date.now()) {
  const rows = readLedger(cwd).filter((r) => now - r.ts <= sinceMs);
  const gets = rows.filter((r) => r.ev === 'get' && r.for !== 'link');
  const pointerRows = rows.filter((r) => r.ev === 'pointer');
  let pointers = 0;
  let followed = 0;
  let tokens = 0;
  for (const r of pointerRows) {
    tokens += Number(r.tokens) || 0;
    for (const id of r.ids || []) {
      pointers++;
      if (gets.some((g) => g.id === id && g.ts >= r.ts && g.ts - r.ts <= CO_USE_WINDOW_MS)) followed++;
    }
  }
  const announced = (g) => pointerRows.some((r) => (r.ids || []).includes(g.id) && r.ts <= g.ts && g.ts - r.ts <= CO_USE_WINDOW_MS);
  const missed = gets.filter((g) => !announced(g));
  const relatedRows = rows.filter((r) => r.ev === 'related');
  const relatedTasks = relatedRows.reduce((n, r) => n + (Array.isArray(r.ids) ? r.ids.length : 0), 0);
  return {
    pointers, followed, tokens, misses: missed.length, missesBySearch: missed.filter((g) => g.for === 'search').length,
    related: relatedRows.length, relatedTasks,
  };
}
