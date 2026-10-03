#!/usr/bin/env node
// @ts-check
/**
 * PreToolUse(Bash) hook: rewrites the command through the filter engine (RTK) so the
 * model receives a compact result instead of the full log. Decision rules live in
 * `filter/decide.mjs`; this file only wires stdin → decision → hook JSON.
 * While a plan is active it also copies the files the command is about to write (baseline).
 * Fail-open: any error means "run the original command untouched".
 */
import { readFileSync } from 'node:fs';
import { relative, resolve } from 'node:path';
import { loadConfig } from '../../../core/config.mjs';
import { gitBranch, toNativePath } from '../../../core/paths.mjs';
import { noteUndeclaredRepo, primaryRoot, repoNames, repoOf } from '../repos.mjs';
import { pushNote } from '../agent-notes.mjs';
import { readMarker } from '../plan-approval.mjs';
import { snapshot } from '../baseline.mjs';
import { writeTargets } from '../../../core/shellwrites.mjs';
import { commandSegments, loadPermissionRules, originalVerdict } from '../permissions.mjs';
import { hasSubstitution } from '../../../core/shell.mjs';
import { decide } from '../../../core/filter/decide.mjs';
import { engineInfo } from '../../../core/filter/engine.mjs';
import { claudeSettingsFiles } from '../settings.mjs';
import { pinRtkPath, rtkRewrite, silenceRtkHookWarning } from '../../../core/filter/rtk.mjs';

const sameRoot = (a, b) => {
  const n = (p) => resolve(toNativePath(p)).replace(/[\\/]+$/, '');
  return process.platform === 'win32' ? n(a).toLowerCase() === n(b).toLowerCase() : n(a) === n(b);
};

/**
 * The repo a write target belongs to (the project's, a declared one, or a git repo found by walking
 * up from the target), or null when it is outside every repo or inside nxy state / git internals.
 * @param {string} cwd @param {string} target @param {string[]} repos
 */
function projectTarget(cwd, target, repos) {
  const repo = repoOf(cwd, target, repos);
  if (!repo) return null;
  const rel = relative(repo.root, resolve(cwd, target));
  if (!rel || rel.startsWith('..')) return null;
  return rel.split(/[\\/]/).some((s) => s === '.nxy' || s === '.git') ? null : repo;
}

try {
  const input = JSON.parse(readFileSync(0, 'utf8'));
  const command = input?.tool_input?.command;
  if ((input?.tool_name === 'Bash' || input?.tool_name === 'PowerShell') && typeof command === 'string') {
    // The project root, not the shell's current dir: after `cd sub && …` Claude Code reports
    // `cwd` = sub, and config/metrics must not move around mid-session.
    const cwd = toNativePath(process.env.CLAUDE_PROJECT_DIR || (typeof input.cwd === 'string' ? input.cwd : process.cwd()));
    // Subagents edit with Edit/Write: a shell write of content into a project file is denied
    // (the payload carries `agent_id` only inside a subagent). Its own try: on error, allow.
    try {
      if (typeof input.agent_id === 'string' && input.agent_id) {
        const targets = writeTargets(command, typeof input.cwd === 'string' ? toNativePath(input.cwd) : cwd, input.tool_name, { content: true });
        const repos = targets.length ? (readMarker(cwd, gitBranch(cwd))?.repos || []) : [];
        const inside = targets.some((t) => projectTarget(cwd, t, repos) != null);
        if (inside) {
          process.stdout.write(
            JSON.stringify({
              hookSpecificOutput: {
                hookEventName: 'PreToolUse',
                permissionDecision: 'deny',
                permissionDecisionReason:
                  'nxy: subagents edit files with Edit/Write, not through the shell — shell quoting eats backslashes and $. Use Edit (or Write for a whole file). Bash is for running commands.',
              },
            }),
          );
          process.exit(0);
        }
      }
    } catch {
      /* best-effort: allow */
    }
    // Copy-before-write for console writes (sed -i, `>`, cp...) while a plan is active, so the
    // review sees them like Edit changes. Its own try: it never blocks or delays the command.
    try {
      const branch = gitBranch(cwd);
      const plan = readMarker(cwd, branch);
      if (plan && !plan.questions) {
        // Only files inside a repo (the project's, a declared one or any git repo), never nxy state
        // (.nxy) or git internals (.git).
        const known = plan.repos || [];
        for (const t of writeTargets(command, typeof input.cwd === 'string' ? toNativePath(input.cwd) : cwd, input.tool_name)) {
          const repo = projectTarget(cwd, t, known);
          if (!repo) continue;
          snapshot(cwd, branch, t);
          if (!repo.primary && !known.some((r) => sameRoot(r, repo.root)) && noteUndeclaredRepo(cwd, repo.root)) {
            pushNote(cwd, `${repoNames([primaryRoot(cwd), ...known, repo.root]).get(repo.root)} is not in the plan; its prior uncommitted changes cannot be told apart`);
          }
        }
      }
    } catch {
      /* best-effort */
    }
    const cfg = loadConfig(cwd);
    if (cfg.modules.filter) {
      const info = engineInfo(cfg, claudeSettingsFiles(cwd));
      const rtkPath = info.rtkPath;
      // Keep rtk's "No hook installed" line out of every result (see silenceRtkHookWarning).
      if (info.engine === 'rtk') silenceRtkHookWarning();
      const decision = decide(command, {
        tool: input.tool_name,
        engine: info.engine,
        rtkHookDetected: info.rtkHookDetected,
        excludeCommands: cfg.filter.excludeCommands,
        onlyCommands: cfg.filter.onlyCommands,
        rewrite: rtkPath ? (c) => rtkRewrite(rtkPath, c) : undefined,
      });

      if (decision.action === 'rewrite' && decision.command) {
        // A denied original must stay denied: never rewrite it into something the deny rule no longer matches.
        const verdict = originalVerdict(command, loadPermissionRules(claudeSettingsFiles(cwd)));
        if (verdict !== 'deny') {
          /** @type {Record<string, unknown>} */
          const hookSpecificOutput = {
            hookEventName: 'PreToolUse',
            updatedInput: { ...input.tool_input, command: pinRtkPath(decision.command, rtkPath || '') },
          };
          // Inherit the approval the ORIGINAL command would have received, so the rewrite
          // never adds a permission prompt the user did not have before. `decision.allow`
          // is rtk's own verdict (exit 0); `verdict` is ours over user+project settings —
          // rtk exits 3 whenever it finds no rule, which is the common case. rtk's verdict is only
          // taken for a single command: for a chain, ours checks every part (see originalVerdict).
          const rtkAllows = decision.allow === true && commandSegments(command).length === 1 && !hasSubstitution(command);
          if (cfg.filter.autoAllowWhenOriginalAllowed && (rtkAllows || verdict === 'allow')) {
            hookSpecificOutput.permissionDecision = 'allow';
            hookSpecificOutput.permissionDecisionReason = 'nxy: original command matched an allow rule';
          }
          process.stdout.write(JSON.stringify({ hookSpecificOutput, suppressOutput: true }));
        }
      }
    }
  }
} catch (err) {
  // fail-open: never block the tool call; NXY_DEBUG=1 surfaces the error on stderr
  if (process.env.NXY_DEBUG) process.stderr.write(`[nxy] ${String((err instanceof Error && err.stack) || err)}\n`);
}
process.exit(0);
