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
import { ASYNC_TTL_MS, clearAsync } from '../agent-notes.mjs';
import { isSubagentCall, lastContextTokens } from '../context.mjs';
import { recordGate } from '../gate-ledger.mjs';
import { handoffCheck, lookupHandoff, memCommand } from '../handoff.mjs';
import {
  checkpointDenyMessage, decideCheckpoint, extractPlan, parseBatches, parseQuestions, planHash, questionsDenyMessage,
} from '../../../core/plan.mjs';
import {
  CONTINUE, batchOfPrompt, blockedDispatchMessage, blockingBatch, continueQuestion, suiteFixOfPrompt, unnamedBatchMessage,
} from '../../../core/verify.mjs';
import { gitBranch, nxyRuntimeDir } from '../../../core/paths.mjs';
import { appendJsonl } from '../../../core/jsonl.mjs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { consumeTicket, readActive, readOrchState } from '../orch-state.mjs';
import { denyMessage, ticketOf } from '../../../core/orchestrator.mjs';
import { findAnswer, isApproved } from '../plan-approval.mjs';
import {
  progressFor, readSuite, readVerify, recordBatch, recordReviewLaunch, recordSuite, recordSuiteFix,
} from '../verify-state.mjs';
import { roleEffort, roleModel } from '../roles.mjs';

/** Plugin agents arrive namespaced (`nxy:implementer`); a user-level copy would not be. */
const IMPLEMENTER = /(^|:)implementer$/;
const TESTER = /(^|:)tester$/;
const REVIEWER = /(^|:)reviewer$/;

/** @param {Record<string, unknown>} out */
const emit = (out) => process.stdout.write(JSON.stringify({ hookSpecificOutput: { hookEventName: 'PreToolUse', ...out } }));

const RESET_HINT = 'If the tester never reported, dispatching nxy:tester again resets it (a suite marked running for over 6 hours is ignored).';

/** Whether the full suite is in flight: marked running, and not so long ago that the tester surely never ended. */
function suiteRunning(cwd, hash) {
  const s = readSuite(cwd, hash);
  return s?.status === 'running' && Date.now() - (Number(s.ts) || 0) <= ASYNC_TTL_MS;
}

/**
 * Why this dispatch cannot carry out a batch yet, or null.
 * @param {string} plan @param {string} hash @param {string} prompt @param {string} cwd
 * @param {string|null|undefined} transcript
 */
function batchGate(plan, hash, prompt, cwd, transcript, wavedThrough = /** @type {number[]} */ ([])) {
  const all = parseBatches(plan);
  const batches = all.map((b) => b.n);
  const n = batchOfPrompt(prompt);
  const batch = all.find((b) => b.n === n);
  const state = readVerify(cwd, gitBranch(cwd), hash);
  const allDone = all.length > 0 && all.every((b) => ['pass', 'manual', 'none'].includes(state.batches[String(b.n)]?.status));
  if (suiteFixOfPrompt(prompt)) {
    if (!allDone) return { reason: 'suitefix-early', message: `nxy: a "Suite fix" starts after every batch of plan ${hash} is verified; some are not (see the progress line). Carry out and verify the batches first.` };
    if (suiteRunning(cwd, hash)) return { reason: 'suitefix-running', message: `nxy: the full suite of plan ${hash} has not finished: wait for its completion notice, then dispatch the "Suite fix" if it failed. ${RESET_HINT}` };
    return null;
  }
  if (n == null || !batch) return { reason: 'batch-unnamed', message: unnamedBatchMessage(batches, allDone) };
  const hit = blockingBatch(state.batches, batch.depends, (red) => wavedThrough.includes(red) || findAnswer(transcript, continueQuestion(hash, red)) === CONTINUE);
  return hit == null ? null : { reason: hit.why === 'red' ? 'batch-red' : 'batch-pending', message: blockedDispatchMessage(hash, hit.k, n, hit.why) };
}

try {
  const input = JSON.parse(readFileSync(0, 'utf8'));
  const toolInput = input?.tool_input;
  const agent = toolInput?.subagent_type;
  const isAgentTool = input?.tool_name === 'Agent' || input?.tool_name === 'Task';
  const cwd = toNativePath(process.env.CLAUDE_PROJECT_DIR || (typeof input?.cwd === 'string' ? input.cwd : process.cwd()));
  // The role table's model (0.4.4), for any nxy agent; the implementer carries it in its own output below.
  const roleCfg = isAgentTool && typeof agent === 'string' ? loadConfig(cwd) : null;
  const model = roleCfg ? roleModel(agent, roleCfg, toolInput.model) : null;
  const effort = roleCfg ? roleEffort(agent, roleCfg, toolInput.effort) : null;
  const roleName = typeof agent === 'string' ? agent.replace(/^nxy:/, '') : '';
  const roleWhy = `nxy: roles.${roleName}.${[model && 'model', effort && 'effort'].filter(Boolean).join('/')}`;
  const roleInput = { ...(model ? { model } : {}), ...(effort ? { effort } : {}) };
  // The tester starts the full suite; the reviewer must not start while it runs (its verdict arrives
  // when it ends, possibly in the background). Recorded here, from the dispatch itself.
  let denied = false;
  // While the orchestrator runs a plan, only its own (ticketed) dispatches of the three roles pass.
  let ticketed = false;
  if (isAgentTool && typeof agent === 'string' && (IMPLEMENTER.test(agent) || TESTER.test(agent) || REVIEWER.test(agent))) {
    const marker = readActive(cwd);
    const transcript = typeof input.transcript_path === 'string' ? toNativePath(input.transcript_path) : input.transcript_path;
    if (marker && !isSubagentCall(input, transcript)) {
      const ticket = ticketOf(toolInput.description);
      ticketed = ticket != null && consumeTicket(cwd, ticket.nonce);
      if (!ticketed) {
        let hash = typeof marker.hash === 'string' ? marker.hash : '';
        if (!hash) {
          const known = await lookupHandoff(cwd);
          const plan = known.handoff ? extractPlan(known.handoff.body) : null;
          hash = plan ? planHash(plan) : '';
        }
        const release = `node ${join(dirname(fileURLToPath(import.meta.url)), '..', 'entries', 'orch.mjs')} release --cwd ${cwd}`;
        emit({ permissionDecision: 'deny', permissionDecisionReason: denyMessage(hash, release) });
        process.exit(0);
      }
    }
  }
  if (isAgentTool && typeof agent === 'string' && (TESTER.test(agent) || REVIEWER.test(agent))) {
    const transcript = typeof input.transcript_path === 'string' ? toNativePath(input.transcript_path) : input.transcript_path;
    if (!isSubagentCall(input, transcript)) {
      const known = await lookupHandoff(cwd);
      const plan = known.handoff ? extractPlan(known.handoff.body) : null;
      const hash = plan ? planHash(plan) : null;
      if (hash && TESTER.test(agent)) {
        recordSuite(cwd, hash, { status: 'running', ts: Date.now() });
        clearAsync(cwd, 'tester');
      } else if (hash && suiteRunning(cwd, hash)) {
        denied = true;
        emit({ permissionDecision: 'deny', permissionDecisionReason: `nxy: the full suite of plan ${hash} has not finished: wait for its completion notice, then dispatch the reviewer. ${RESET_HINT}` });
      } else if (hash) {
        recordReviewLaunch(cwd, hash);
        clearAsync(cwd, 'reviewer');
      }
    }
  }
  if (!denied && isAgentTool && typeof agent === 'string' && !IMPLEMENTER.test(agent) && (model || effort)) {
    emit({ permissionDecision: 'allow', permissionDecisionReason: roleWhy, updatedInput: { ...toolInput, ...roleInput } });
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
      ? batchGate(plan, hash, typeof toolInput.prompt === 'string' ? toolInput.prompt : '', cwd, transcript, ticketed ? readOrchState(cwd).continued : [])
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
        const { conventionsOf, touchedRepos } = await import('../repos.mjs');
        conventions = await conventionsOf(db, touchedRepos(cwd, gitBranch(cwd)));
        if (!conventions.length) conventions = listMemories(db, { project: known.project, allAreas: true, type: 'convention', limit: 500 });
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
      // An allowed dispatch of a batch (or the suite fix) is in flight from now on: a previous ✔ or ✘ is
      // never read for it, and its dependents wait.
      if (plan && hash && !isSubagent) {
        if (suiteFixOfPrompt(prompt)) {
          recordSuiteFix(cwd, hash, { status: 'running', ts: Date.now() });
          clearAsync(cwd, 'implementer:suite-fix');
        } else if (n != null && parseBatches(plan).some((b) => b.n === n)) {
          recordBatch(cwd, gitBranch(cwd), hash, n, { status: 'running', ts: Date.now() });
          clearAsync(cwd, `implementer:batch-${n}`);
        }
      }
      recordGate(cwd, { ts: Date.now(), tool: input.tool_name, allow: true, reason: 'handoff-attached', contextTokens, threshold });
      emit({
        permissionDecision: 'allow',
        permissionDecisionReason: 'nxy: task handoff attached to the implementer',
        updatedInput: { ...toolInput, prompt: `${block}\n\n${prompt}`, ...(model ? { model } : {}), ...(effort ? { effort } : {}) },
      });
    } else if (model || effort) {
      emit({ permissionDecision: 'allow', permissionDecisionReason: roleWhy, updatedInput: { ...toolInput, ...roleInput } });
    }
  }
} catch (err) {
  if (process.env.NXY_DEBUG) process.stderr.write(`[nxy] ${String((err instanceof Error && err.stack) || err)}\n`);
}
process.exit(0);
