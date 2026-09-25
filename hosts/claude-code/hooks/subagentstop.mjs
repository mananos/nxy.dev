#!/usr/bin/env node
// @ts-check
/**
 * SubagentStop hook: an implementer cannot finish a plan batch without its acceptance command
 * passing after its last edit (0.4.1).
 *
 * Reads the implementer's own transcript — which batch it was given, what it edited, what it ran
 * and how each run ended — and records the batch's verdict. Unproven (not run, or red) the first
 * time: exit 2, and the implementer keeps going with the exact command to run. The second time it
 * stops anyway and the batch is recorded red: the main thread decides with the user, never a loop.
 *
 * Only implementers with a batch of an approved plan are looked at. Anything else, any error: silent.
 */
import { readFileSync } from 'node:fs';
import { gitBranch, toNativePath } from '../../../core/paths.mjs';
import { extractPlan, parseBatches, parseQuestions, planHash } from '../../../core/plan.mjs';
import { batchOfPrompt, batchVerdict, isRed, stopMessage } from '../../../core/verify.mjs';
import { lookupHandoff } from '../handoff.mjs';
import { agentTranscriptPath, claimSendBack, readAgentRun, recordBatch } from '../verify-state.mjs';

/** Plugin agents arrive namespaced (`nxy:implementer`); a user-level copy would not be. */
const IMPLEMENTER = /(^|:)implementer$/;

let code = 0;
try {
  const input = JSON.parse(readFileSync(0, 'utf8'));
  if (typeof input?.agent_type === 'string' && IMPLEMENTER.test(input.agent_type)) {
    const cwd = toNativePath(process.env.CLAUDE_PROJECT_DIR || (typeof input.cwd === 'string' ? input.cwd : process.cwd()));
    const known = await lookupHandoff(cwd);
    const plan = known.handoff ? extractPlan(known.handoff.body) : null;
    const path = agentTranscriptPath(input);
    const run = plan && !parseQuestions(plan).length ? readAgentRun(path ? toNativePath(path) : null) : null;
    const n = run ? batchOfPrompt(run.prompt) : null;
    const batch = plan && n != null ? parseBatches(plan).find((b) => b.n === n) : null;
    if (plan && batch && run && n != null) {
      const hash = planHash(plan);
      const verdict = batchVerdict(run.events, batch.accept);
      // Only this batch's file: a parallel batch finishing at the same moment writes its own.
      recordBatch(cwd, gitBranch(cwd), hash, n, { ...verdict, ts: Date.now() });
      const key = `${input.agent_id || path}:${n}`;
      if (isRed(verdict.status) && batch.accept.kind === 'command' && claimSendBack(cwd, hash, key)) {
        process.stderr.write(stopMessage(n, batch.accept.command, verdict));
        code = 2;
      }
    }
  }
} catch (err) {
  if (process.env.NXY_DEBUG) process.stderr.write(`[nxy] ${String((err instanceof Error && err.stack) || err)}\n`);
  code = 0;
}
process.exit(code);
