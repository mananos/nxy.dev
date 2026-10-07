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
 * Exit 2 only reaches a foreground agent (no launch marker: PostToolUse marks every async launch and a
 * sync dispatch clears it). A background agent (`hasAsync`) is never sent back: its stop is final and
 * it never continues, so the verdict is recorded and the main thread hears it through a note.
 *
 * The same for a post-suite fix ("Suite fix — ..."): verified against the plan's `Suite:` commands, or
 * else the commands the tester saw fail. The tester's stop records how the full suite ended; a reviewer
 * that stops without having recorded its review is sent back once.
 *
 * A background agent's launch was marked by PostToolUse; when it stops (and is not sent back), what
 * the main thread must hear is left as a note for the next hook that runs on it.
 *
 * Anything else, any error: silent.
 */
import { readFileSync } from 'node:fs';
import { gitBranch, toNativePath } from '../../../core/paths.mjs';
import { extractPlan, parseBatches, parseQuestions, parseSuites, planHash } from '../../../core/plan.mjs';
import { batchOfPrompt, batchVerdict, canonCommand, inspectRun, isRed, stopMessage, suiteFixOfPrompt, suiteFixVerdict } from '../../../core/verify.mjs';
import { completionLines, reviewExpected } from '../agent-done.mjs';
import { hasAsync, pushNote, takeAsync } from '../agent-notes.mjs';
import { lookupHandoff } from '../handoff.mjs';
import {
  agentTranscriptPath, claimSendBack, readAgentRun, readSuite, recordBatch, recordSuite, recordSuiteFix, reviewRecorded,
} from '../verify-state.mjs';

/** Plugin agents arrive namespaced (`nxy:implementer`); a user-level copy would not be. */
const IMPLEMENTER = /(^|:)implementer$/;
const TESTER = /(^|:)tester$/;
const REVIEWER = /(^|:)reviewer$/;

let code = 0;
try {
  const input = JSON.parse(readFileSync(0, 'utf8'));
  const type = typeof input?.agent_type === 'string' ? input.agent_type : '';
  const role = IMPLEMENTER.test(type) ? 'implementer' : TESTER.test(type) ? 'tester' : REVIEWER.test(type) ? 'reviewer' : null;
  if (role) {
    const cwd = toNativePath(process.env.CLAUDE_PROJECT_DIR || (typeof input.cwd === 'string' ? input.cwd : process.cwd()));
    const known = await lookupHandoff(cwd);
    const plan = known.handoff ? extractPlan(known.handoff.body) : null;
    const path = agentTranscriptPath(input);
    const hash = plan && !parseQuestions(plan).length ? planHash(plan) : null;
    const run = path ? readAgentRun(path ? toNativePath(path) : null) : null;
    /** The identity of the launch marker, and the prompt for the completion lines. */
    let marker = /** @type {string|null} */ (null);
    let sentBack = false;

    if (plan && hash && run && role === 'implementer' && suiteFixOfPrompt(run.prompt)) {
      marker = 'implementer:suite-fix';
      const fromPlan = parseSuites(plan).map((s) => s.command);
      const commands = fromPlan.length ? fromPlan : (readSuite(cwd, hash)?.red || []).map((r) => r.command);
      const verdict = suiteFixVerdict(run.events, commands, cwd);
      const failed = verdict.failed || [];
      recordSuiteFix(cwd, hash, {
        status: 'done', ts: Date.now(), ran: commands,
        red: failed.map((command) => ({
          command, status: verdict.status,
          ...(verdict.error ? { error: verdict.error } : {}), ...(verdict.detail ? { detail: verdict.detail } : {}),
        })),
      });
      if (isRed(verdict.status) && !hasAsync(cwd, marker) && claimSendBack(cwd, hash, `${input.agent_id || path}:suite-fix`)) {
        process.stderr.write([
          `nxy verify: the suite fix has not passed — ${verdict.status === 'fail' ? `it failed after your last edit${verdict.error ? ` (${verdict.error})` : ''}` : `the suite commands were not run after your last edit${verdict.detail ? ` (${verdict.detail})` : ''}`}.`,
          `Run each exactly as written, not piped and not in the background: ${commands.map((c) => `\`${c}\``).join(', ')}`,
          'If it fails because of your change, fix it and run it again. If it was already failing before your change, say so in your Notes and stop.',
        ].join('\n'));
        code = 2;
        sentBack = true;
      }
    } else if (plan && hash && run && role === 'implementer') {
      const n = batchOfPrompt(run.prompt);
      const batch = n != null ? parseBatches(plan).find((b) => b.n === n) : null;
      if (batch && n != null) {
        marker = `implementer:batch-${n}`;
        const verdict = batchVerdict(run.events, batch.accept, cwd);
        // Only this batch's file: a parallel batch finishing at the same moment writes its own.
        recordBatch(cwd, gitBranch(cwd), hash, n, { ...verdict, ts: Date.now() });
        const key = `${input.agent_id || path}:${n}`;
        if (isRed(verdict.status) && batch.accept.kind === 'command' && !hasAsync(cwd, marker) && claimSendBack(cwd, hash, key)) {
          process.stderr.write(stopMessage(n, batch.accept.command, verdict));
          code = 2;
          sentBack = true;
        }
      }
    } else if (plan && hash && run && role === 'tester') {
      marker = 'tester';
      // What failed: the last run of each command, if it was non-zero; never nxy's own memory calls. When
      // the plan names its suite and Accept commands, only those count (a probe is not the suite).
      const runs = /** @type {any[]} */ (run.events.filter((e) => e.kind === 'run' && !/mem\.mjs/.test(/** @type {any} */ (e).command)));
      const known = [...new Set([...parseSuites(plan).map((s) => s.command), ...parseBatches(plan).flatMap((b) => (b.accept.kind === 'command' ? [b.accept.command] : []))])];
      // Keyed by the known command a run matched (root-aware: `cd "<root>" && npm run check` is `npm run check`),
      // so a later clean run supersedes an earlier failing one. A run whose exit code is hidden (a pipe) is
      // a near miss: when it is all there is for a known command, that command is not certified.
      const last = new Map();
      const nearMiss = new Map();
      for (const e of runs) {
        if (!known.length) { last.set(canonCommand(e.command), e); continue; }
        for (const k of known) {
          const r = inspectRun(e.command, k, cwd);
          if (r.match) { last.set(k, e); nearMiss.delete(k); } else if (r.why) nearMiss.set(k, r.why);
        }
      }
      /** @type {{command: string, error?: string, detail?: string}[]} */
      const red = [...last.values()].filter((e) => !e.ok).map((e) => ({ command: e.command, ...(e.error ? { error: e.error } : {}) }));
      for (const [k, why] of nearMiss) if (!last.has(k)) red.push({ command: k, detail: `not certified: ${why}` });
      recordSuite(cwd, hash, {
        status: 'done', ts: Date.now(),
        ran: runs.map((e) => e.command),
        red,
      });
    } else if (plan && hash && role === 'reviewer') {
      marker = 'reviewer';
      if (!reviewRecorded(cwd, hash) && await reviewExpected(cwd, hash) && !hasAsync(cwd, marker) && claimSendBack(cwd, hash, `${input.agent_id || path}:reviewer`)) {
        process.stderr.write(`nxy: you stopped without recording your review of plan ${hash}. Run the record command printed at the end of the packet output (\`review.mjs record\`, with your findings), then stop.`);
        code = 2;
        sentBack = true;
      }
    }

    // A background agent ended for good: leave what the main thread must hear for the next hook.
    // The launch note promised a report: when nothing is recorded for it, say so rather than stay silent.
    if (!marker && role !== 'implementer') marker = role;
    if (!marker && run) {
      const n = batchOfPrompt(run.prompt);
      marker = suiteFixOfPrompt(run.prompt) ? 'implementer:suite-fix' : n != null ? `implementer:batch-${n}` : null;
    }
    if (marker && !sentBack && takeAsync(cwd, marker)) {
      const done = plan && hash ? await completionLines({ cwd, role, prompt: run?.prompt || '' }) : { lines: [] };
      pushNote(cwd, done.lines.join('\n\n') || `nxy: the background ${marker.replace(/^implementer:/, '').replace('-', ' ')} ended and nxy recorded nothing for it (${plan && hash ? 'no named batch' : 'no approved plan'}): check its report.`);
    }
  }
} catch (err) {
  if (process.env.NXY_DEBUG) process.stderr.write(`[nxy] ${String((err instanceof Error && err.stack) || err)}\n`);
  code = 0;
}
process.exit(code);
