// @ts-check
/**
 * Search tools seen from the PreToolUse hooks (Grep, Glob, and Bash/PowerShell searches): the
 * planner is nudged once per run from a repo-wide grep to locate.mjs. Nothing else happens here;
 * the main thread's broad-search observation lives in search-history.mjs (from the transcripts).
 */
import { readFileSync, statSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { PLUGIN_ROOT, ensureDir, nxyRuntimeDir, toNativePath } from '../../core/paths.mjs';
import { LOCATE_NUDGE_MARK, classifySearch, isLocateCommand } from '../../core/search-scope.mjs';
import { agentTypeOf, isSubagentCall } from './context.mjs';

const NUDGE_TTL_MS = 24 * 60 * 60 * 1000;
const LOCATE = join(PLUGIN_ROOT, 'hosts', 'claude-code', 'entries', 'locate.mjs').replace(/\\/g, '/');

const nudgePath = (cwd) => join(nxyRuntimeDir(cwd), 'planner-nudge.json');

function readNudges(cwd) {
  try {
    const o = JSON.parse(readFileSync(nudgePath(cwd), 'utf8'));
    return o && typeof o === 'object' ? o : {};
  } catch {
    return {};
  }
}

function markDone(cwd, id, state) {
  const now = Date.now();
  const next = {};
  for (const [k, v] of Object.entries(state)) if (v && now - v.ts < NUDGE_TTL_MS) next[k] = v;
  next[id] = { ts: now, done: true };
  ensureDir(nxyRuntimeDir(cwd));
  writeFileSync(nudgePath(cwd), JSON.stringify(next), 'utf8');
}

/** The pattern as plain words, safe to put inside double quotes. */
function plainWords(pattern) {
  const w = String(pattern ?? '').replace(/[^\w.\s-]+/g, ' ').replace(/\s+/g, ' ').trim();
  return w || 'term';
}

function nudgeMessage(pattern) {
  return `${LOCATE_NUDGE_MARK} a repo-wide grep reads far more than it needs. Locate the files first with: node "${LOCATE}" "${plainWords(pattern)}" — then read only what it points at. Retry the search after that if you still need it.`;
}

/**
 * @param {string} cwd project root
 * @param {any} input hook payload
 * @returns {Promise<{deny: string|null}>}
 */
export async function onSearchTool(cwd, input) {
  const tool = input?.tool_name;
  const transcript = typeof input?.transcript_path === 'string' ? toNativePath(input.transcript_path) : null;
  const base = typeof input?.cwd === 'string' ? toNativePath(input.cwd) : cwd;
  const isFile = (p) => statSync(resolve(base, toNativePath(p))).isFile();

  if (isSubagentCall(input, transcript)) {
    const type = agentTypeOf(input, transcript);
    if (!type || !/(^|:)planner$/.test(type)) return { deny: null };
    const id = (typeof input.agent_id === 'string' && input.agent_id)
      || transcript?.match(/agent-([^\\/]+)\.jsonl$/)?.[1] || 'planner';
    const state = readNudges(cwd);
    if (state[id]?.done) return { deny: null };
    if (isLocateCommand(input?.tool_input?.command)) {
      markDone(cwd, id, state);
      return { deny: null };
    }
    const cls = classifySearch(tool, input?.tool_input, { isFile });
    if (!cls.kind) return { deny: null };
    markDone(cwd, id, state);
    return { deny: cls.kind === 'grep' && cls.broad ? nudgeMessage(cls.pattern) : null };
  }

  return { deny: null };
}
