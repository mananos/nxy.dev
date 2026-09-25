#!/usr/bin/env node
// @ts-check
/**
 * PreToolUse(Agent) hook: what an implementer dispatch must carry.
 *
 * 1. No handoff, session past the gate threshold → deny until one is saved (0.3.1). Past the
 *    threshold the main thread no longer edits — the implementer does — so the dispatch is where
 *    write work actually starts; on the edit hook alone, a model that delegates straight away
 *    would never meet the rule.
 * 2. A handoff exists → attach it in front of the prompt via `updatedInput` (0.3.2). The subagent
 *    starts with the task's decisions and files whether or not the main thread remembered to copy
 *    them. Any block already in the prompt (a copied re-dispatch, an imitation) is replaced by the
 *    runtime's fresh one.
 *
 * 3. An approved plan → the dispatch names its batch ("Batch N — …"), and a batch that ended red
 *    stops the ones after it until it passes or the user picks "Continue anyway" (0.4.1).
 *
 * The planner is never held back by its own plan: re-dispatching it with the user's answers is how
 * a draft's questions get closed (0.4.0b).
 *
 * Other agents (scout, Explore, anything else) pass untouched: reading is not the work a handoff
 * protects, and a locator should search without being told where to look.
 *
 * Fail-open: any error or unknown, and the dispatch goes through as the model wrote it.
 */
import { readFileSync } from 'node:fs';
import { loadConfig } from '../../../core/config.mjs';
import { toNativePath } from '../../../core/paths.mjs';
import { isSubagentCall, lastContextTokens } from '../context.mjs';
import { recordGate } from '../gate-ledger.mjs';
import { handoffCheck, lookupHandoff, memCommand } from '../handoff.mjs';
import {
  checkpointDenyMessage, decideCheckpoint, extractPlan, parseBatches, parseQuestions, planHash, questionsDenyMessage,
} from '../../../core/plan.mjs';
import {
  CONTINUE, batchOfPrompt, blockedDispatchMessage, blockingBatch, continueQuestion, unnamedBatchMessage,
} from '../../../core/verify.mjs';
import { gitBranch, nxyRuntimeDir } from '../../../core/paths.mjs';
import { appendJsonl } from '../../../core/jsonl.mjs';
import { join } from 'node:path';
import { findAnswer, isApproved } from '../plan-approval.mjs';
import { progressFor, readVerify } from '../verify-state.mjs';
import { roleModel } from '../roles.mjs';

/** Plugin agents arrive namespaced (`nxy:implementer`); a user-level copy would not be. */
const IMPLEMENTER = /(^|:)implementer$/;

/** @param {Record<string, unknown>} out */
const emit = (out) => process.stdout.write(JSON.stringify({ hookSpecificOutput: { hookEventName: 'PreToolUse', ...out } }));

/**
 * Why this dispatch cannot carry out a batch yet, or null.
 * @param {string} plan @param {string} hash @param {string} prompt @param {string} cwd
 * @param {string|null|undefined} transcript
 */
function batchGate(plan, hash, prompt, cwd, transcript) {
  const all = parseBatches(plan);
  const batches = all.map((b) => b.n);
  const n = batchOfPrompt(prompt);
  const batch = all.find((b) => b.n === n);
  if (n == null || !batch) return { reason: 'batch-unnamed', message: unnamedBatchMessage(batches) };
  const state = readVerify(cwd, gitBranch(cwd), hash);
  const hit = blockingBatch(state.batches, batch.depends, (red) => findAnswer(transcript, continueQuestion(hash, red)) === CONTINUE);
  return hit == null ? null : { reason: hit.why === 'red' ? 'batch-red' : 'batch-pending', message: blockedDispatchMessage(hash, hit.k, n, hit.why) };
}

try {
  const input = JSON.parse(readFileSync(0, 'utf8'));
  const toolInput = input?.tool_input;
  const agent = toolInput?.subagent_type;
  const isAgentTool = input?.tool_name === 'Agent' || input?.tool_name === 'Task';
  const cwd = toNativePath(process.env.CLAUDE_PROJECT_DIR || (typeof input?.cwd === 'string' ? input.cwd : process.cwd()));
  // The role table's model (0.4.4), for any nxy agent; the implementer carries it in its own output below.
  const model = isAgentTool && typeof agent === 'string' ? roleModel(agent, loadConfig(cwd), toolInput.model) : null;
  if (isAgentTool && typeof agent === 'string' && !IMPLEMENTER.test(agent) && model) {
    emit({ permissionDecision: 'allow', permissionDecisionReason: `nxy: roles.${agent.replace(/^nxy:/, '')}.model`, updatedInput: { ...toolInput, model } });
  }
  if (isAgentTool && typeof agent === 'string' && IMPLEMENTER.test(agent)) {
    const cfg = loadConfig(cwd);
    const transcript = typeof input.transcript_path === 'string' ? toNativePath(input.transcript_path) : input.transcript_path;
    const isSubagent = isSubagentCall(input, transcript);
    const contextTokens = isSubagent ? null : lastContextTokens(transcript);
    const threshold = Number(cfg.gate?.contextTokens) || 0;
    const known = await lookupHandoff(cwd);

    // The plan checkpoint (0.4.0) comes first: an implementer dispatched before the user approved
    // the plan is exactly the "400 lines I have to undo" this exists to prevent.
    const plan = known.handoff ? extractPlan(known.handoff.body) : null;
    const hash = plan ? planHash(plan) : null;
    const questions = plan ? parseQuestions(plan).length : 0;
    const checkpoint = decideCheckpoint({ isSubagent, planHash: hash, questions, approved: hash && !questions ? isApproved(cwd, transcript, hash) : false });
    const verdict = checkpoint.block
      ? { block: false, reason: 'skipped' }
      : await handoffCheck({ cwd, cfg, isSubagent, contextTokens, known, then: 'Then dispatch the implementer again, unchanged.' });
    // With the plan approved, each dispatch is one of its batches (0.4.1): named, so the SubagentStop
    // hook can verify it, and never on top of a batch that ended red unless the user said so.
    const batchBlock = !checkpoint.block && !verdict.block && plan && hash && !isSubagent
      ? batchGate(plan, hash, typeof toolInput.prompt === 'string' ? toolInput.prompt : '', cwd, transcript)
      : null;

    if (checkpoint.block && hash) {
      recordGate(cwd, { ts: Date.now(), tool: input.tool_name, allow: false, reason: checkpoint.reason, contextTokens, threshold });
      emit({
        permissionDecision: 'deny',
        permissionDecisionReason: questions ? questionsDenyMessage(hash, questions, memCommand('handoff next')) : checkpointDenyMessage(hash),
      });
    } else if (batchBlock) {
      recordGate(cwd, { ts: Date.now(), tool: input.tool_name, allow: false, reason: batchBlock.reason, contextTokens, threshold });
      emit({ permissionDecision: 'deny', permissionDecisionReason: batchBlock.message });
    } else if (verdict.block) {
      recordGate(cwd, { ts: Date.now(), tool: input.tool_name, allow: false, reason: verdict.reason, contextTokens, threshold });
      emit({ permissionDecision: 'deny', permissionDecisionReason: verdict.message || 'nxy: save a handoff first' });
    } else if (known.handoff) {
      const { stripContextBlock, subagentContextBlock } = await import('../../../core/memory/handoff.mjs');
      // The conventions sheet (0.4.3): the batch's area first, capped; the rest by reference.
      const { conventionSheet, pathsIn } = await import('../../../core/memory/conventions.mjs');
      const { listMemories, openStore } = await import('../../../core/memory/store.mjs');
      const db = openStore();
      /** @type {{title: string, area: string|null}[]} */
      let conventions = [];
      try {
        conventions = listMemories(db, { project: known.project, allAreas: true, type: 'convention', limit: 500 });
      } finally {
        db.close();
      }
      const n = plan ? batchOfPrompt(typeof toolInput.prompt === 'string' ? toolInput.prompt : '') : null;
      const scope = plan ? (parseBatches(plan).find((b) => b.n === n)?.text || plan) : '';
      const sheet = conventionSheet(conventions, pathsIn(scope), memCommand('list --type convention --all-areas'));
      // The plan's progress as nxy recorded it (0.4.5): which batches passed, which are red.
      const progress = plan ? await progressFor(cwd, gitBranch(cwd), plan) : '';
      const block = subagentContextBlock(known.handoff, memCommand('handoff show'), Date.now(), [progress, sheet].filter(Boolean).join('\n\n'));
      const prompt = stripContextBlock(typeof toolInput.prompt === 'string' ? toolInput.prompt : '');
      // A fix dispatched from checkpoint 2 is the user's choice: counted for `review status`.
      const fix = /\bfix (R\d+) of review ([0-9a-f]{6})\b/.exec(prompt);
      if (fix) appendJsonl(join(nxyRuntimeDir(cwd), 'review.jsonl'), { ts: Date.now(), kind: 'chosen', id: fix[2], finding: fix[1] });
      recordGate(cwd, { ts: Date.now(), tool: input.tool_name, allow: true, reason: 'handoff-attached', contextTokens, threshold });
      emit({
        permissionDecision: 'allow',
        permissionDecisionReason: 'nxy: task handoff attached to the implementer',
        updatedInput: { ...toolInput, prompt: `${block}\n\n${prompt}`, ...(model ? { model } : {}) },
      });
    } else if (model) {
      emit({ permissionDecision: 'allow', permissionDecisionReason: 'nxy: roles.implementer.model', updatedInput: { ...toolInput, model } });
    }
  }
} catch (err) {
  if (process.env.NXY_DEBUG) process.stderr.write(`[nxy] ${String((err instanceof Error && err.stack) || err)}\n`);
}
process.exit(0);
