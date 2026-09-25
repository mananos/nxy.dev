#!/usr/bin/env node
// @ts-check
/**
 * SessionStart hook (startup, resume, clear, compact):
 *  - resolves the filter engine once per session and caches it so the per-command PreToolUse
 *    hook never has to spawn `rtk --version`;
 *  - points at the branch's live handoff, per memory mode (core/memory/handoff.mjs). In the
 *    default `assisted` mode that is one line (~40 tokens), only when a handoff exists;
 *    otherwise stdout stays empty (zero context cost).
 * Diagnostics go to stderr (visible with `claude --debug`).
 */
import { readFileSync } from 'node:fs';
import { loadConfig } from '../../../core/config.mjs';
import { toNativePath } from '../../../core/paths.mjs';
import { resolveEngine, writeEngineCache } from '../../../core/filter/engine.mjs';
import { claudeSettingsFiles } from '../settings.mjs';
import { lookupHandoff, memCommand } from '../handoff.mjs';

try {
  let cwd = process.cwd();
  try {
    const input = JSON.parse(readFileSync(0, 'utf8'));
    cwd = toNativePath(process.env.CLAUDE_PROJECT_DIR || (typeof input?.cwd === 'string' ? input.cwd : cwd));
  } catch {
    /* no/invalid stdin: fall back to process.cwd() */
  }
  const cfg = loadConfig(cwd);
  const info = resolveEngine(cfg, claudeSettingsFiles(cwd));
  writeEngineCache(info);
  if (cfg.modules.filter && info.engine === 'off' && info.reason !== 'config') {
    process.stderr.write(`[nxy] filter engine off (${info.reason}). Run /nxy:filter status for install hints.\n`);
  }
  if (info.rtkHookDetected) {
    process.stderr.write('[nxy] RTK\'s own hook is installed; nxy will not rewrite commands (metrics only).\n');
  }

  // Separate from the filter on purpose: a memory failure must not cost the session its filter.
  const mode = cfg.memory?.mode || 'assisted';
  if (mode !== 'manual') {
    try {
      const { sessionStartContext } = await import('../../../core/memory/handoff.mjs');
      const { handoff } = await lookupHandoff(cwd);
      const context = sessionStartContext(mode, handoff, memCommand('handoff show'));
      if (context) {
        process.stdout.write(JSON.stringify({ hookSpecificOutput: { hookEventName: 'SessionStart', additionalContext: context } }));
      }
    } catch (err) {
      if (process.env.NXY_DEBUG) process.stderr.write(`[nxy] handoff: ${String((err instanceof Error && err.stack) || err)}\n`);
    }
  }
} catch (err) {
  // fail-open: never block the tool call; NXY_DEBUG=1 surfaces the error on stderr
  if (process.env.NXY_DEBUG) process.stderr.write(`[nxy] ${String((err instanceof Error && err.stack) || err)}\n`);
}
process.exit(0);
