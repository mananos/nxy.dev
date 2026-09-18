#!/usr/bin/env node
// @ts-check
/**
 * PreToolUse(Bash) hook: rewrites the command through the filter engine (RTK) so the
 * model receives a compact result instead of the full log. Decision rules live in
 * `filter/decide.mjs`; this file only wires stdin → decision → hook JSON.
 * Fail-open: any error means "run the original command untouched".
 */
import { readFileSync } from 'node:fs';
import { loadConfig } from '../lib/config.mjs';
import { toNativePath } from '../lib/paths.mjs';
import { loadPermissionRules, originalVerdict } from '../lib/permissions.mjs';
import { decide } from '../filter/decide.mjs';
import { claudeSettingsFiles, engineInfo } from '../filter/engine.mjs';
import { pinRtkPath, rtkRewrite } from '../filter/rtk.mjs';

try {
  const input = JSON.parse(readFileSync(0, 'utf8'));
  const command = input?.tool_input?.command;
  if ((input?.tool_name === 'Bash' || input?.tool_name === 'PowerShell') && typeof command === 'string') {
    const cwd = toNativePath(typeof input.cwd === 'string' ? input.cwd : process.cwd());
    const cfg = loadConfig(cwd);
    if (cfg.modules.filter) {
      const info = engineInfo(cfg, cwd);
      const rtkPath = info.rtkPath;
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
          // rtk exits 3 whenever it finds no rule, which is the common case.
          if (cfg.filter.autoAllowWhenOriginalAllowed && (decision.allow || verdict === 'allow')) {
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
