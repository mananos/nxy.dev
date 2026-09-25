#!/usr/bin/env node
// @ts-check
/**
 * PostToolUse(Bash) hook: appends one metrics row per Bash call to
 * `<project>/.nxy/local/metrics/filter.jsonl`. Kept synchronous: the row is guaranteed to be on
 * disk before the next tool call and `/nxy:stats` never races it. Cost: ~100 ms of Node
 * startup per Bash call, no context. Prints nothing.
 */
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { loadConfig } from '../../../core/config.mjs';
import { toNativePath } from '../../../core/paths.mjs';
import { appendJsonl } from '../../../core/jsonl.mjs';
import { nxyRuntimeDir } from '../../../core/paths.mjs';
import { decide } from '../../../core/filter/decide.mjs';
import { engineInfo } from '../../../core/filter/engine.mjs';
import { claudeSettingsFiles } from '../settings.mjs';
import { commandHead, commandKind } from '../../../core/filter/kind.mjs';
import { extractRecallHash, usesRtk } from '../../../core/filter/rtk.mjs';

/** Bash tool_response is `{stdout, stderr, interrupted}` in current versions; older ones passed a string. */
function responseText(resp) {
  if (typeof resp === 'string') return resp;
  if (resp && typeof resp === 'object') {
    const parts = [resp.stdout, resp.stderr].filter((s) => typeof s === 'string' && s.length);
    return parts.join('\n');
  }
  return '';
}

try {
  const input = JSON.parse(readFileSync(0, 'utf8'));
  const command = input?.tool_input?.command;
  if ((input?.tool_name === 'Bash' || input?.tool_name === 'PowerShell') && typeof command === 'string') {
    // The project root, not the shell's current dir: after `cd sub && …` Claude Code reports
    // `cwd` = sub, and config/metrics must not move around mid-session.
    const cwd = toNativePath(process.env.CLAUDE_PROJECT_DIR || (typeof input.cwd === 'string' ? input.cwd : process.cwd()));
    const cfg = loadConfig(cwd);
    if (cfg.modules.metrics) {
      const text = responseText(input.tool_response ?? input.tool_result);
      const wrapped = usesRtk(command);
      const info = engineInfo(cfg, claudeSettingsFiles(cwd));
      let engine = 'off';
      let reason = 'filter-disabled';
      if (cfg.modules.filter) {
        if (wrapped) {
          engine = 'rtk';
          reason = 'rtk';
        } else {
          // Replay the decision without the engine to learn why this command was not rewritten.
          const d = decide(command, {
            tool: input.tool_name,
            engine: info.engine,
            rtkHookDetected: info.rtkHookDetected,
            excludeCommands: cfg.filter.excludeCommands,
            onlyCommands: cfg.filter.onlyCommands,
            rewrite: () => ({ code: 1, stdout: '' }),
          });
          engine = 'skip';
          reason = d.reason;
        }
      }
      const lines = text ? text.split('\n').length : 0;
      appendJsonl(join(nxyRuntimeDir(cwd), 'metrics', 'filter.jsonl'), {
        ts: new Date().toISOString(),
        session_id: input.session_id ?? null,
        tool: input.tool_name,
        engine,
        reason,
        command_kind: commandKind(command),
        command_head: commandHead(command),
        filtered_lines: lines,
        filtered_chars: text.length,
        est_tokens: Math.ceil(text.length / 4),
        rtk_recall_hash: wrapped ? extractRecallHash(text) : null,
        interrupted: Boolean(input?.tool_response?.interrupted),
      });
    }
  }
} catch (err) {
  // fail-open: never block the tool call; NXY_DEBUG=1 surfaces the error on stderr
  if (process.env.NXY_DEBUG) process.stderr.write(`[nxy] ${String((err instanceof Error && err.stack) || err)}\n`);
}
process.exit(0);
