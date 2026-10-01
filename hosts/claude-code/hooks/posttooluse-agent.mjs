#!/usr/bin/env node
// @ts-check
/**
 * PostToolUse(Agent) hook: what the main thread learns when one of nxy's agents returns.
 *
 * - An implementer from a plan batch: one line with the batch's verdict as nxy recorded it (0.4.1) —
 *   ✔, ✘ with what to do, or manual — and, once every batch is green, the instruction to run the
 *   full suite once.
 * - The tester: the instruction to review (0.4.2), or why there is nothing to review.
 * - At any of those pauses (and when the reviewer returns): if the main thread's context is past the
 *   statusline's warning, a suggestion to cut the session — once per session (0.4.4). The handoff
 *   carries the plan and its progress; nxy never clears on its own.
 *
 * The verdict comes from `.nxy/local/verify/<plan>/`, written by the SubagentStop hook from the
 * implementer's transcript just before this runs; the implementer's own report is not consulted.
 *
 * Silent for anything else, and on any error.
 */
import { readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { loadConfig } from '../../../core/config.mjs';
import { ensureDir, nxyRuntimeDir, toNativePath } from '../../../core/paths.mjs';
import { batchOfPrompt, cutSuggestion, launchNote, suiteFixOfPrompt } from '../../../core/verify.mjs';
import { completionLines } from '../agent-done.mjs';
import { isAsyncLaunch, markAsync, takeNotes } from '../agent-notes.mjs';
import { lastContextTokens } from '../context.mjs';
import { memCommand } from '../handoff.mjs';

const IMPLEMENTER = /(^|:)implementer$/;
const TESTER = /(^|:)tester$/;
const REVIEWER = /(^|:)reviewer$/;

/** @type {string[]} */
const out = [];

try {
  const input = JSON.parse(readFileSync(0, 'utf8'));
  const toolInput = input?.tool_input;
  const agent = toolInput?.subagent_type;
  const mainThread = (input?.tool_name === 'Agent' || input?.tool_name === 'Task') && !input.agent_id && typeof agent === 'string';
  const cwd = toNativePath(process.env.CLAUDE_PROJECT_DIR || (typeof input?.cwd === 'string' ? input.cwd : process.cwd()));
  let pause = false;
  const prompt = typeof toolInput?.prompt === 'string' ? toolInput.prompt : '';
  const role = !mainThread ? null : IMPLEMENTER.test(agent) ? 'implementer' : TESTER.test(agent) ? 'tester' : REVIEWER.test(agent) ? 'reviewer' : null;

  if (role && isAsyncLaunch(input.tool_response)) {
    // A background launch: the verdict comes when the agent ends (SubagentStop leaves it as a note).
    const n = batchOfPrompt(prompt);
    if (role === 'implementer' && suiteFixOfPrompt(prompt)) {
      markAsync(cwd, 'implementer:suite-fix');
      out.push(launchNote('suite-fix'));
    } else if (role === 'implementer') {
      if (n != null) {
        markAsync(cwd, `implementer:batch-${n}`);
        out.push(launchNote('batch', n));
      }
    } else {
      markAsync(cwd, role);
      out.push(launchNote(role));
    }
  } else if (role) {
    const done = await completionLines({ cwd, role, prompt });
    out.push(...done.lines);
    pause = done.pause;
  }
  if (mainThread) out.push(...takeNotes(cwd));

  // The session cut (0.4.4): at a pause, past the statusline's own warning, once per session.
  if (pause) {
    const transcript = typeof input.transcript_path === 'string' ? toNativePath(input.transcript_path) : null;
    const tokens = lastContextTokens(transcript);
    const warn = Number(loadConfig(cwd).metrics?.statusline?.ctxWarnTokens) || 100_000;
    const session = typeof input.session_id === 'string' ? input.session_id : null;
    if (typeof tokens === 'number' && tokens >= warn && session && firstCutSuggestion(cwd, session)) {
      out.push(cutSuggestion(tokens, warn, memCommand('handoff save'), memCommand('handoff show')));
    }
  }
} catch (err) {
  if (process.env.NXY_DEBUG) process.stderr.write(`[nxy] ${String((err instanceof Error && err.stack) || err)}\n`);
}
if (out.length) process.stdout.write(JSON.stringify({ hookSpecificOutput: { hookEventName: 'PostToolUse', additionalContext: out.join('\n\n') } }));
process.exit(0);

/** True the first time for this session (and records it). */
function firstCutSuggestion(cwd, session) {
  const path = join(nxyRuntimeDir(cwd), 'cut-suggested.json');
  /** @type {string[]} */
  let seen = [];
  try {
    seen = JSON.parse(readFileSync(path, 'utf8'));
  } catch {
    /* first time */
  }
  if (seen.includes(session)) return false;
  try {
    ensureDir(nxyRuntimeDir(cwd));
    writeFileSync(path, JSON.stringify([...seen, session].slice(-50)), 'utf8');
  } catch {
    /* best-effort */
  }
  return true;
}
