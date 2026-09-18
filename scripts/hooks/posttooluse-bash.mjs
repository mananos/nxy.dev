#!/usr/bin/env node
// @ts-check
/**
 * PostToolUse(Bash) hook: appends one metrics row per Bash call to
 * `<project>/.nxy/metrics/filter.jsonl`. Runs async (Claude Code does not wait for it)
 * and prints nothing else, so it costs zero context and ~no latency.
 */
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { loadConfig } from '../lib/config.mjs';
import { toNativePath } from '../lib/paths.mjs';
import { appendJsonl } from '../lib/jsonl.mjs';
import { nxyProjectDir } from '../lib/paths.mjs';
import { decide } from '../filter/decide.mjs';
import { engineInfo } from '../filter/engine.mjs';
import { commandHead, commandKind } from '../filter/kind.mjs';
import { extractRecallHash, usesRtk } from '../filter/rtk.mjs';

/** Bash tool_response is `{stdout, stderr, interrupted}` in current versions; older ones passed a string. */
function responseText(resp) {
  if (typeof resp === 'string') return resp;
  if (resp && typeof resp === 'object') {
    const parts = [resp.stdout, resp.stderr].filter((s) => typeof s === 'string' && s.length);
    return parts.join('\n');
  }
  return '';
}

process.stdout.write('{"async": true}\n');

try {
  const input = JSON.parse(readFileSync(0, 'utf8'));
  const command = input?.tool_input?.command;
  if ((input?.tool_name === 'Bash' || input?.tool_name === 'PowerShell') && typeof command === 'string') {
    const cwd = toNativePath(typeof input.cwd === 'string' ? input.cwd : process.cwd());
    const cfg = loadConfig(cwd);
    if (cfg.modules.metrics) {
      const text = responseText(input.tool_response ?? input.tool_result);
      const wrapped = usesRtk(command);
      const info = engineInfo(cfg, cwd);
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
      appendJsonl(join(nxyProjectDir(cwd), 'metrics', 'filter.jsonl'), {
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
