#!/usr/bin/env node
// @ts-check
/**
 * Stop hook: the main thread does not end its turn with the task's handoff stale (0.4.5,
 * core/memory/handoff.mjs `decideStop`).
 *
 * Frozen by design (roadmap 8.5): zero tokens on the happy path, milliseconds, never a loop, never
 * in a small task. Here that is: no live handoff → exit; `stop_hook_active` → exit; nothing edited
 * since the last save → exit; already reminded about this state → exit. Two small JSON reads, no
 * SQLite. When it does block (exit 2), stderr tells the model exactly what to run, and the state is
 * recorded *after* the reminder is written, so a crash in between loses nothing.
 *
 * Fail-open: any error and the turn ends as it would have.
 */
import { readFileSync } from 'node:fs';
import { gitBranch, toNativePath } from '../../../core/paths.mjs';
import { decideStop, staleHandoffMessage } from '../../../core/memory/handoff.mjs';
import { memCommand } from '../handoff.mjs';
import { handoffSavedTs, readStopState, writeStopState } from '../stop-state.mjs';

let code = 0;
try {
  const input = JSON.parse(readFileSync(0, 'utf8'));
  const session = typeof input?.session_id === 'string' ? input.session_id : null;
  if (session && !input.agent_id) {
    const cwd = toNativePath(process.env.CLAUDE_PROJECT_DIR || (typeof input.cwd === 'string' ? input.cwd : process.cwd()));
    const branch = gitBranch(cwd);
    const state = readStopState(cwd, session);
    const verdict = decideStop({
      stopHookActive: input.stop_hook_active === true,
      savedTs: handoffSavedTs(cwd, branch),
      lastEditTs: state.lastEditTs,
      remindedTs: state.remindedTs,
    });
    if (verdict.block && state.lastEditTs != null) {
      process.stderr.write(staleHandoffMessage(branch, memCommand('handoff show'), memCommand('handoff save')));
      code = 2;
      writeStopState(cwd, session, { remindedTs: state.lastEditTs });
    }
  }
} catch (err) {
  if (process.env.NXY_DEBUG) process.stderr.write(`[nxy] ${String((err instanceof Error && err.stack) || err)}\n`);
  code = 0;
}
process.exit(code);
