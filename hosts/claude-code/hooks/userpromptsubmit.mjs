#!/usr/bin/env node
// @ts-check
/**
 * UserPromptSubmit hook: recall by prompt (core/memory/recall.mjs).
 *
 * Before each message reaches the model, looks for memories that deserve a pointer — by the files
 * the prompt names or the session has been editing, by keywords, by shared words — and puts up to
 * three one-line pointers in front of it. The body is never injected in `assisted` mode: the model
 * loads it with `mem get` if it is worth it.
 *
 * Deciding costs no tokens (SQLite, in this process). Writing nothing costs nothing, and that is
 * what happens when nothing is relevant, when the pointer was already shown this session, or in
 * `manual` mode.
 *
 * Fail-open: any error and the prompt goes through untouched.
 */
import { readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { loadConfig } from '../../../core/config.mjs';
import { gitBranch, nxyRuntimeDir, toNativePath } from '../../../core/paths.mjs';
import { areaFor, projectKey } from '../../../core/memory/scope.mjs';
import { memCommand, reviewCommand } from '../handoff.mjs';
import { notePointers, readSession } from '../recall-state.mjs';

/** @type {string[]} */
const context = [];

try {
  const input = JSON.parse(readFileSync(0, 'utf8'));
  const prompt = typeof input?.prompt === 'string' ? input.prompt : '';
  const cwd = toNativePath(process.env.CLAUDE_PROJECT_DIR || (typeof input?.cwd === 'string' ? input.cwd : process.cwd()));
  const cfg = loadConfig(cwd);
  const mode = cfg.memory?.mode || 'assisted';
  const session = typeof input.session_id === 'string' ? input.session_id : 'unknown';
  // On a branch whose plan was already reviewed, a correction now is what the review missed (0.4.3):
  // one line per session so the main thread offers to keep it. Nothing on any other branch.
  if (prompt.trim() && !prompt.trim().startsWith('/')) {
    const reminder = escapeReminder(cwd, session);
    if (reminder) context.push(reminder);
  }
  // A slash command is an instruction to nxy or Claude Code, not a question to remember things for.
  if (mode !== 'manual' && prompt.trim() && !prompt.trim().startsWith('/')) {
    const state = readSession(cwd, session);
    const { openStore } = await import('../../../core/memory/store.mjs');
    const { recall, formatPointers } = await import('../../../core/memory/recall.mjs');
    const db = openStore();
    try {
      const pointers = recall(db, prompt, {
        project: projectKey(cwd).key,
        areas: [...new Set(state.files.map((f) => areaFor(cwd, f)).filter((a) => typeof a === 'string'))],
        files: state.files,
        exclude: state.pointed,
      });
      const text = formatPointers(pointers, memCommand('get'), mode);
      if (text) {
        notePointers(cwd, session, pointers.map((p) => p.mem.id), text.length);
        context.push(text);
      }
    } finally {
      db.close();
    }
  }
} catch (err) {
  if (process.env.NXY_DEBUG) process.stderr.write(`[nxy] recall: ${String((err instanceof Error && err.stack) || err)}\n`);
}
if (context.length) {
  process.stdout.write(JSON.stringify({ hookSpecificOutput: { hookEventName: 'UserPromptSubmit', additionalContext: context.join('\n\n') } }));
}
process.exit(0);

/**
 * The escape reminder, once per session, on a branch reviewed in the last 30 days.
 * @param {string} cwd @param {string} session
 */
function escapeReminder(cwd, session) {
  try {
    const review = JSON.parse(readFileSync(join(nxyRuntimeDir(cwd), 'review.json'), 'utf8'));
    if (!review || review.branch !== gitBranch(cwd) || Date.now() - Number(review.ts) > 30 * 86_400_000) return null;
    const markPath = join(nxyRuntimeDir(cwd), 'escape-reminded.json');
    /** @type {string[]} */
    let seen = [];
    try {
      seen = JSON.parse(readFileSync(markPath, 'utf8'));
    } catch {
      /* first time */
    }
    if (seen.includes(session)) return null;
    writeFileSync(markPath, JSON.stringify([...seen, session].slice(-50)), 'utf8');
    return `nxy: this branch's plan was already reviewed (review ${review.id}). If the user is now correcting something that review missed, offer once to keep it — as a repo rule or as something future reviews check: ${reviewCommand('escape "<the rule>" --as convention|lens --lens <lens>')}`;
  } catch {
    return null;
  }
}
