#!/usr/bin/env node
// @ts-check
/**
 * PreToolUse(Grep|Glob) hook: the planner's one-time locate nudge and the main thread's read gate
 * (hosts/claude-code/search-observe.mjs). Prints only a deny JSON. In the main thread a narrow
 * search exits after one string check (core/search-scope.mjs only); a broad one goes through the
 * read gate. Fail-open: any error, silent exit 0.
 */
import { readFileSync } from 'node:fs';

try {
  const input = JSON.parse(readFileSync(0, 'utf8'));
  const inSubagent = (typeof input?.agent_id === 'string' && input.agent_id)
    || (typeof input?.agent_type === 'string' && input.agent_type)
    || /[\\/]subagents[\\/]/.test(String(input?.transcript_path ?? ''));
  if (!inSubagent) {
    // Main thread: string work only; a narrow search exits here with nothing else read.
    const { isBroadMainSearch } = await import('../../../core/search-scope.mjs');
    if (!isBroadMainSearch(input?.tool_name, input?.tool_input)) process.exit(0);
  }
  if (input?.tool_name === 'Grep' || input?.tool_name === 'Glob') {
    const { toNativePath } = await import('../../../core/paths.mjs');
    const { onSearchTool } = await import('../search-observe.mjs');
    const cwd = toNativePath(process.env.CLAUDE_PROJECT_DIR || (typeof input.cwd === 'string' ? input.cwd : process.cwd()));
    const { deny } = await onSearchTool(cwd, input);
    if (deny) {
      process.stdout.write(JSON.stringify({
        hookSpecificOutput: { hookEventName: 'PreToolUse', permissionDecision: 'deny', permissionDecisionReason: deny },
      }));
    }
  }
} catch (err) {
  if (process.env.NXY_DEBUG) process.stderr.write(`[nxy] ${String((err instanceof Error && err.stack) || err)}\n`);
}
process.exit(0);
