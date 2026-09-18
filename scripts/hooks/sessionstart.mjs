#!/usr/bin/env node
// @ts-check
/**
 * SessionStart hook: resolves the filter engine once per session and caches it so the
 * per-command PreToolUse hook never has to spawn `rtk --version`. Prints nothing to
 * stdout (zero context cost); diagnostics go to stderr (visible with `claude --debug`).
 */
import { readFileSync } from 'node:fs';
import { loadConfig } from '../lib/config.mjs';
import { toNativePath } from '../lib/paths.mjs';
import { resolveEngine, writeEngineCache } from '../filter/engine.mjs';

try {
  let cwd = process.cwd();
  try {
    const input = JSON.parse(readFileSync(0, 'utf8'));
    if (typeof input?.cwd === 'string') cwd = toNativePath(input.cwd);
  } catch {
    /* no/invalid stdin: fall back to process.cwd() */
  }
  const cfg = loadConfig(cwd);
  const info = resolveEngine(cfg, cwd);
  writeEngineCache(info);
  if (cfg.modules.filter && info.engine === 'off' && info.reason !== 'config') {
    process.stderr.write(`[nxy] filter engine off (${info.reason}). Run /nxy:filter status for install hints.\n`);
  }
  if (info.rtkHookDetected) {
    process.stderr.write('[nxy] RTK\'s own hook is installed; nxy will not rewrite commands (metrics only).\n');
  }
} catch (err) {
  // fail-open: never block the tool call; NXY_DEBUG=1 surfaces the error on stderr
  if (process.env.NXY_DEBUG) process.stderr.write(`[nxy] ${String((err instanceof Error && err.stack) || err)}\n`);
}
process.exit(0);
