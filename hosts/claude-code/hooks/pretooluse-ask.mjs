#!/usr/bin/env node
// @ts-check
/**
 * PreToolUse hook on AskUserQuestion: counts the rules offered as repo conventions from the chat
 * (the question nxy asks when a task closes), with no model call. The saves are counted where they
 * happen (`mem save --type convention`).
 *
 * Never blocks, never prints, exits 0 on any error.
 */
import { readFileSync } from 'node:fs';

try {
  // Dynamic imports inside the try: a load error must still exit 0.
  const { toNativePath } = await import('../../../core/paths.mjs');
  const { CHAT_RULES_QUESTION } = await import('../../../core/review.mjs');
  const { noteChatOffers } = await import('../recall-state.mjs');
  const input = JSON.parse(readFileSync(0, 'utf8'));
  const questions = Array.isArray(input?.tool_input?.questions) ? input.tool_input.questions : [];
  const ask = questions.find((q) => q && q.question === CHAT_RULES_QUESTION);
  if (ask) {
    const cwd = toNativePath(process.env.CLAUDE_PROJECT_DIR || (typeof input.cwd === 'string' ? input.cwd : process.cwd()));
    const rules = (Array.isArray(ask.options) ? ask.options : []).map((o) => String(o?.label ?? '')).filter(Boolean);
    noteChatOffers(cwd, rules);
  }
} catch (err) {
  if (process.env.NXY_DEBUG) process.stderr.write(`[nxy] ${String((err instanceof Error && err.stack) || err)}\n`);
}
process.exit(0);
