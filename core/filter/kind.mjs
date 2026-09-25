// @ts-check
import { firstToken, splitTopLevel } from '../shell.mjs';

const TOOL_ALIASES = { mvn: 'maven', mvnw: 'maven', mvnd: 'maven', gradlew: 'gradle', python3: 'python', py: 'python', pnpx: 'npx' };

/** Tools whose first non-option argument is a meaningful subcommand (`git status`, `npm test`). */
const WITH_SUBCOMMAND = new Set(['git', 'npm', 'pnpm', 'yarn', 'bun', 'docker', 'cargo', 'go', 'maven', 'gradle', 'gh', 'kubectl', 'dotnet', 'pip', 'uv', 'poetry', 'make', 'npx', 'deno']);

/** Segments that only set shell state (bash or PowerShell); a chain is classified by the first segment after them. */
const STATE_SEGMENTS = new Set(['cd', 'export', 'source', 'set', 'unset', 'pushd', 'set-location', 'push-location', 'chdir']);

/** Removes a leading `rtk` / `rtk proxy` (bare, .exe or absolute path), keeping any VAR=value prefixes. */
function stripRtk(seg) {
  return seg.replace(/^\s*((?:[A-Za-z_][A-Za-z0-9_]*=(?:"[^"]*"|'[^']*'|\S+)\s+)*)"?(?:[^"\s]*[\\/])?rtk(?:\.exe)?"?\s+(?:proxy\s+)?/i, '$1');
}

function isStateSegment(seg) {
  return STATE_SEGMENTS.has(firstToken(seg).toLowerCase()) || /^\$env:/i.test(seg);
}

/**
 * Normalized command kind for metrics grouping. Rewritten and raw invocations of the same
 * tool land in the same bucket, and a `cd … && export … && mvn test` chain (or its
 * PowerShell form `Set-Location …; $env:X = …; mvn test`) counts as maven.
 * `rtk mvn test` → 'maven-test', `git status -s` → 'git-status', `ls -la` → 'ls'.
 * @param {string} cmd
 */
export function commandKind(cmd) {
  const segments = (cmd || '').trim().split(/\r?\n/).flatMap((line) => splitTopLevel(line)).map(stripRtk);
  const head = segments.find((seg) => !isStateSegment(seg)) || segments[0] || '';
  const tool = TOOL_ALIASES[firstToken(head)] || firstToken(head);
  if (!tool) return 'unknown';
  if (tool.startsWith('$')) return 'powershell-script';
  if (!WITH_SUBCOMMAND.has(tool)) return tool;
  const words = head.split(/\s+/);
  const start = words.findIndex((w) => (TOOL_ALIASES[firstToken(w)] || firstToken(w)) === tool);
  for (let i = start + 1; i < words.length; i++) {
    const w = words[i];
    if (w === '-C' || w === '-c') { i++; continue; }
    if (w.startsWith('-') || /^[A-Za-z_][A-Za-z0-9_]*=/.test(w)) continue;
    return `${tool}-${w.replace(/[^A-Za-z0-9_.:-]/g, '')}`;
  }
  return tool;
}

/** First 80 chars of a command with obvious secrets masked. */
export function commandHead(cmd, max = 80) {
  const masked = (cmd || '')
    .replace(/(--?(?:password|passwd|token|secret|api[-_]?key|authorization)[=\s]+)[^\s"']+/gi, '$1***')
    .replace(/\b(Bearer\s+)[^\s"']+/gi, '$1***')
    .replace(/([A-Za-z_]*(?:TOKEN|SECRET|PASSWORD|KEY)[A-Za-z_]*=)[^\s"']+/g, '$1***');
  return masked.length > max ? masked.slice(0, max - 1) + '…' : masked;
}
