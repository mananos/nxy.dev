#!/usr/bin/env node
// @ts-check
/**
 * PreToolUse(Edit|Write|NotebookEdit) hook: the write gate, and the handoff rule in front of it.
 *
 * Reads how much context the main thread is carrying and, above the configured threshold:
 *  1. if the branch has no live handoff, denies the edit until one is saved (core/memory/handoff.mjs);
 *  2. otherwise denies it with an instruction to delegate to the implementer (core/gate.mjs).
 * This file only wires stdin → context → verdict → hook JSON.
 *
 * Costs on the happy path: one tail read of the transcript and one config read. No tokens — a
 * hook that allows writes nothing at all, so the model never sees it. The memory store is only
 * opened when an edit would already be refused.
 *
 * Fail-open, like every other nxy hook: any error, any unknown, and the edit goes through.
 */
import { readFileSync } from 'node:fs';
import { loadConfig } from '../../../core/config.mjs';
import { gitBranch, toNativePath } from '../../../core/paths.mjs';
import { decideGate } from '../../../core/gate.mjs';
import { isSubagentCall, lastContextTokens } from '../context.mjs';
import { consumeEscape } from '../gate-state.mjs';
import { recordGate } from '../gate-ledger.mjs';
import { handoffCheck, lookupHandoff, memCommand } from '../handoff.mjs';
import { noteFile, readSession } from '../recall-state.mjs';
import {
  checkpointDenyMessage, decideCheckpoint, extractPlan, parseBatches, planActiveDenyMessage, planFirstDenyMessage, questionsDenyMessage,
} from '../../../core/plan.mjs';
import { isApproved, readMarker } from '../plan-approval.mjs';
import { snapshot } from '../baseline.mjs';
import { writeStopState } from '../stop-state.mjs';

const GATED_TOOLS = new Set(['Edit', 'Write', 'NotebookEdit', 'MultiEdit']);

let denied = false;

/** @param {string} reason */
function deny(reason) {
  denied = true;
  process.stdout.write(JSON.stringify({
    hookSpecificOutput: {
      hookEventName: 'PreToolUse',
      permissionDecision: 'deny',
      permissionDecisionReason: reason,
    },
  }));
}

try {
  const input = JSON.parse(readFileSync(0, 'utf8'));
  if (GATED_TOOLS.has(input?.tool_name)) {
    const cwd = toNativePath(process.env.CLAUDE_PROJECT_DIR || (typeof input.cwd === 'string' ? input.cwd : process.cwd()));
    const cfg = loadConfig(cwd);
    const gateCfg = cfg.gate || { enabled: false, contextTokens: 0, escapeMinutes: 5 };
    // Same normalisation as cwd: a host that reports an MSYS-style path (/c/Users/...) would
    // otherwise read as C:\c\Users and the gate would silently fail open on every edit.
    const transcript = typeof input.transcript_path === 'string' ? toNativePath(input.transcript_path) : input.transcript_path;
    const isSubagent = isSubagentCall(input, transcript);
    const contextTokens = isSubagent ? null : lastContextTokens(transcript);
    const hasImplementer = Boolean(cfg.roles && cfg.roles.implementer);
    const file = typeof input?.tool_input?.file_path === 'string' ? input.tool_input.file_path : undefined;
    // Whether this session already edited another file (for `flow.plan: "always"`, 0.4.4).
    const before = file && typeof input.session_id === 'string' ? readSession(cwd, input.session_id).files : [];
    // What the session is working on is recall's strongest signal (memories attached to it).
    if (file && typeof input.session_id === 'string') noteFile(cwd, input.session_id, toNativePath(file));
    const current = file && typeof input.session_id === 'string' ? readSession(cwd, input.session_id).files.at(-1) : null;
    const touchedOther = Boolean(current) && before.some((f) => f !== current);
    // The plan's "before" (0.4.2): the first time its work touches a file, keep a copy for the review.
    const branch = gitBranch(cwd);
    const onBranch = file ? readMarker(cwd, branch) : null;
    if (file && onBranch && !onBranch.questions) snapshot(cwd, branch, toNativePath(file));

    // A plan waiting for the user's approval stops everything else: nothing is written before the
    // checkpoint (0.4.0). Then the handoff, which never consumes the gate's escape: `/nxy:gate
    // once` means "edit here", not "skip writing down where we are".
    const pending = isSubagent ? null : readMarker(cwd, branch);
    const pendingHash = pending ? pending.hash : null;
    const checkpoint = decideCheckpoint({
      isSubagent, planHash: pendingHash, questions: pending?.questions,
      approved: pending && !pending.questions ? isApproved(cwd, transcript, pending.hash) : false,
    });
    const gateWouldBlock = gateCfg.enabled !== false && hasImplementer;
    const handoff = checkpoint.block ? { block: false, reason: 'skipped' } : await handoffCheck({
      cwd, cfg, isSubagent, contextTokens,
      then: gateWouldBlock
        ? 'Then dispatch the `implementer` subagent with the exact `path:line`, the change and an acceptance criterion — past this threshold the main thread does not edit.'
        : 'Then retry the edit.',
    });

    // With an approved plan the main thread does not write: each batch goes to an implementer, which
    // is what gets verified (0.4.4). And a repo with `flow.plan: "always"` plans any change that
    // touches a second file. Both yield to the user's `/nxy:gate once`.
    const planRule = !checkpoint.block && !isSubagent && hasImplementer
      ? (checkpoint.reason === 'approved' ? 'plan-active'
        : checkpoint.reason === 'no-plan' && cfg.flow?.plan === 'always' && touchedOther ? 'plan-first' : null)
      : null;
    const planRuleBlocks = planRule ? !consumeEscape(cwd, gateCfg.escapeMinutes) : false;

    if (checkpoint.block && pendingHash) {
      recordGate(cwd, { ts: Date.now(), tool: input.tool_name, allow: false, reason: checkpoint.reason, contextTokens, threshold: gateCfg.contextTokens });
      deny(checkpoint.reason === 'plan-questions'
        ? questionsDenyMessage(pendingHash, pending?.questions || 0, memCommand('handoff next'))
        : checkpointDenyMessage(pendingHash));
    } else if (planRule && planRuleBlocks) {
      recordGate(cwd, { ts: Date.now(), tool: input.tool_name, allow: false, reason: planRule, contextTokens, threshold: gateCfg.contextTokens });
      if (planRule === 'plan-active' && pendingHash) {
        const known = await lookupHandoff(cwd);
        const plan = known.handoff ? extractPlan(known.handoff.body) : null;
        deny(planActiveDenyMessage(pendingHash, plan ? parseBatches(plan).map((b) => b.n) : [], '/nxy:gate once'));
      } else deny(planFirstDenyMessage('/nxy:gate once'));
    } else if (planRule) {
      recordGate(cwd, { ts: Date.now(), tool: input.tool_name, allow: true, reason: 'escape', contextTokens, threshold: gateCfg.contextTokens });
    } else if (handoff.block) {
      recordGate(cwd, { ts: Date.now(), tool: input.tool_name, allow: false, reason: handoff.reason, contextTokens, threshold: gateCfg.contextTokens });
      deny(handoff.message || 'nxy: save a handoff first');
    } else if (gateCfg.enabled !== false) {
      const verdict = decideGate(
        {
          contextTokens,
          isSubagent,
          hasImplementer,
          // Only consult (and burn) the escape when everything else points at a block.
          escapeActive: !isSubagent && typeof contextTokens === 'number' && contextTokens >= gateCfg.contextTokens
            ? consumeEscape(cwd, gateCfg.escapeMinutes)
            : false,
          tool: input.tool_name,
          file,
        },
        gateCfg,
      );

      recordGate(cwd, {
        ts: Date.now(),
        tool: input.tool_name,
        allow: verdict.allow,
        reason: verdict.reason,
        contextTokens,
        threshold: gateCfg.contextTokens,
      });

      if (!verdict.allow) deny(verdict.message || 'nxy: delegate this edit to the implementer');
    }

    // For the Stop hook (0.4.5): this session wrote something the handoff may not say yet. Plan
    // batch work (an implementer under a plan) is not counted: its progress is derived by nxy.
    if (file && !denied && typeof input.session_id === 'string' && !(isSubagent && onBranch)) {
      writeStopState(cwd, input.session_id, { lastEditTs: Date.now() });
    }
  }
} catch (err) {
  // fail-open: never block an edit because of our own bug; NXY_DEBUG=1 shows why
  if (process.env.NXY_DEBUG) process.stderr.write(`[nxy] ${String((err instanceof Error && err.stack) || err)}\n`);
}
process.exit(0);
