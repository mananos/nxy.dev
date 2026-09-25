// @ts-check
/**
 * How much context the main thread is carrying right now, read from the transcript Claude Code
 * is already writing.
 *
 * The gate needs this on every Edit, so it has to be free: a tail read of the last few KB of the
 * JSONL, no state, no daemon, no extra call. Same number the statusline shows.
 */
import { closeSync, existsSync, openSync, readSync, statSync } from 'node:fs';

const TAIL_BYTES = 256 * 1024; // enough to hold several assistant messages, cheap to read

/**
 * A subagent's transcript lives under `<session>/subagents/agent-<id>.jsonl`. That is how the
 * metrics side already tells main from subagent, so the gate uses the same signal rather than
 * inventing a second one.
 * @param {string|undefined|null} transcriptPath
 */
export function isSubagentTranscript(transcriptPath) {
  if (!transcriptPath) return false;
  return /[\\/]subagents[\\/]/.test(transcriptPath);
}

/**
 * Whether a hook call comes from inside a subagent. Claude Code puts `agent_id` / `agent_type` in
 * the payload of hooks fired inside one (gentle-ai's preflight relies on the same fields); the
 * transcript path is kept as a second signal, so a payload without them still resolves.
 * @param {{agent_id?: unknown, agent_type?: unknown}} input
 * @param {string|undefined|null} transcriptPath  already normalised with toNativePath
 */
export function isSubagentCall(input, transcriptPath) {
  if (typeof input?.agent_id === 'string' && input.agent_id) return true;
  if (typeof input?.agent_type === 'string' && input.agent_type) return true;
  return isSubagentTranscript(transcriptPath);
}

/**
 * Last known context size of the transcript's thread: `input + cache_read + cache_creation` of
 * the most recent assistant message that reported usage.
 *
 * Returns null when it cannot be determined — the caller must fail open, never guess.
 * @param {string|undefined|null} transcriptPath
 * @returns {number|null}
 */
export function lastContextTokens(transcriptPath) {
  if (!transcriptPath || !existsSync(transcriptPath)) return null;
  let fd;
  try {
    const size = statSync(transcriptPath).size;
    if (!size) return null;
    const start = Math.max(0, size - TAIL_BYTES);
    const len = size - start;
    const buf = Buffer.alloc(len);
    fd = openSync(transcriptPath, 'r');
    readSync(fd, buf, 0, len, start);
    const text = buf.toString('utf8');
    const lines = text.split('\n');
    // Walk backwards: the newest usage wins, and we stop at the first one we can read.
    for (let i = lines.length - 1; i >= 0; i--) {
      const line = lines[i];
      if (!line || line[0] !== '{') continue; // a partial first line from the tail cut
      let o;
      try {
        o = JSON.parse(line);
      } catch {
        continue;
      }
      if (o.type !== 'assistant') continue;
      const u = o.message?.usage;
      if (!u) continue;
      const total = (u.input_tokens || 0) + (u.cache_read_input_tokens || 0) + (u.cache_creation_input_tokens || 0);
      if (total > 0) return total;
    }
    return null;
  } catch {
    return null;
  } finally {
    if (fd !== undefined) {
      try {
        closeSync(fd);
      } catch {
        /* already gone */
      }
    }
  }
}
