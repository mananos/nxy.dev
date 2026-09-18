// @ts-check
/**
 * Pure decision: should this Bash command be rewritten through the filter engine?
 * No I/O here — the engine call is injected so the rules are testable by table.
 */
import {
  firstToken, hasHeredoc, hasRedirect, hasSubstitution, isBackgrounded, leadingAssignments, splitTopLevel, stripTrailingLimit,
} from '../lib/shell.mjs';
import { normalizeForRtk, usesRtk } from './rtk.mjs';

/**
 * @typedef {object} RewriteResult
 * @property {number} code   0 = rewritten+allow, 1 = no equivalent, 2 = deny, 3 = rewritten but ask
 * @property {string} stdout rewritten command when code is 0 or 3
 */

/**
 * @typedef {object} DecideContext
 * @property {'rtk'|'off'} engine
 * @property {boolean} [rtkHookDetected]  RTK's own PreToolUse hook is installed → nxy must not double-rewrite
 * @property {string[]} [excludeCommands] first tokens never rewritten
 * @property {string[]} [onlyCommands]    when non-empty, only these first tokens are rewritten
 * @property {(cmd: string) => RewriteResult} [rewrite] engine call (rtk rewrite)
 */

/**
 * @typedef {object} Decision
 * @property {'skip'|'rewrite'} action
 * @property {string} reason
 * @property {string} [command] rewritten command (action === 'rewrite')
 * @property {boolean} [allow]  engine says the rewritten command needs no permission prompt
 */

/** Commands that are interactive, stateful or write-only: nothing to filter, real risk of breaking them. */
const NEVER_FIRST_TOKENS = new Set([
  'sudo', 'su', 'vim', 'vi', 'nano', 'less', 'more', 'top', 'htop', 'ssh', 'telnet', 'claude',
  'python', 'python3', 'node', 'irb', 'psql', 'mysql', 'sqlite3', 'watch', 'tmux', 'screen', 'alias',
]);

/** Shell state setters: harmless as a prefix of a chain (`export JAVA_HOME=… && mvn test`), pointless alone. */
const STATE_ONLY_TOKENS = new Set(['cd', 'export', 'source', 'unset', 'set']);

/** `git <sub>` subcommands that mutate state or prompt; their output is short anyway. */
const NEVER_GIT_SUBCOMMANDS = new Set(['commit', 'rebase', 'push', 'merge', 'cherry-pick', 'stash', 'tag', 'reset', 'checkout', 'switch']);

/** First non-option word after `git`, skipping the argument of `-C` / `-c`. */
function gitSubcommand(segment) {
  const words = segment.trim().split(/\s+/);
  const start = words.findIndex((w) => firstToken(w) === 'git');
  for (let i = start + 1; i < words.length; i++) {
    const w = words[i];
    if (w === '-C' || w === '-c') { i++; continue; }
    if (w.startsWith('-')) continue;
    return w;
  }
  return null;
}

const ESCAPE_COMMENT = /(^|\s)#\s*(nxy:)?raw\b/;

function segmentIsInteractive(segment) {
  const t = firstToken(segment);
  if (NEVER_FIRST_TOKENS.has(t)) return true;
  if (t === 'git') {
    const sub = gitSubcommand(segment);
    if (sub && NEVER_GIT_SUBCOMMANDS.has(sub)) return true;
    if (sub === 'add' && /\s(-p|--patch|-i|--interactive)\b/.test(segment)) return true;
  }
  if (/^docker\s+(exec|run)\b.*\s-\w*it?\b/.test(segment.trim())) return true;
  return false;
}

/** PowerShell statements that only set state before the real command. */
const PS_STATE = /^(?:Set-Location|cd|chdir|pushd|Push-Location)\s|^\$env:[A-Za-z_][A-Za-z0-9_]*\s*=/i;
/** Anything that makes a PowerShell input a script rather than "prefix + one command". */
const PS_SCRIPT = /[|`]|\$\(|@\(|\bforeach\b|\bif\s*\(|\btry\s*\{|\bfunction\b|\$[A-Za-z_]+\s*=(?!=)/i;

/**
 * PowerShell tool input. rtk speaks bash, so we only handle the shape Claude uses for
 * build/test runs: `Set-Location "x"; $env:JAVA_HOME = "y"; .\mvnw.cmd test` — state
 * prefixes followed by one command. The prefixes are kept verbatim; the command is asked
 * of rtk with a bash-style spelling and swapped for rtk's answer (valid PowerShell too).
 * @param {string} cmd
 * @param {DecideContext} ctx
 * @returns {Decision}
 */
function decidePowerShell(cmd, ctx) {
  const segments = cmd.split(/\r?\n|;/).map((s) => s.trim()).filter(Boolean);
  if (!segments.length) return { action: 'skip', reason: 'empty' };
  const last = segments[segments.length - 1];
  const prefixes = segments.slice(0, -1);
  if (!prefixes.every((p) => PS_STATE.test(p))) return { action: 'skip', reason: 'powershell-script' };
  if (PS_STATE.test(last)) return { action: 'skip', reason: 'interactive-or-stateful' };
  if (PS_SCRIPT.test(last) || usesRtk(last)) return { action: 'skip', reason: usesRtk(last) ? 'already-wrapped' : 'powershell-script' };
  if (hasRedirect(last)) return { action: 'skip', reason: 'redirect' };
  if (segmentIsInteractive(last)) return { action: 'skip', reason: 'interactive-or-stateful' };
  if (ctx.rtkHookDetected) return { action: 'skip', reason: 'rtk-hook-present' };
  if (!ctx.rewrite) return { action: 'skip', reason: 'no-engine' };
  const asked = normalizeForRtk(stripTrailingLimit(last));
  let res;
  try {
    res = ctx.rewrite(asked);
  } catch {
    return { action: 'skip', reason: 'rtk-error' };
  }
  const rewritten = (res.stdout || '').trim();
  if ((res.code === 0 || res.code === 3) && rewritten && rewritten !== asked) {
    return { action: 'rewrite', reason: res.code === 0 ? 'rtk' : 'rtk-ask', command: [...prefixes, rewritten].join('; '), allow: res.code === 0 };
  }
  if (res.code === 2) return { action: 'skip', reason: 'rtk-deny' };
  return { action: 'skip', reason: res.code === 1 ? 'rtk-none' : 'rtk-unchanged' };
}

/**
 * @param {string} command
 * @param {DecideContext & {tool?: string}} ctx
 * @returns {Decision}
 */
export function decide(command, ctx) {
  const cmd = (command || '').trim();
  if (!cmd) return { action: 'skip', reason: 'empty' };
  if (ctx.engine !== 'rtk') return { action: 'skip', reason: 'engine-off' };

  // 1. Escape hatches
  if (leadingAssignments(cmd).NXY_RAW === '1' || /^\$env:NXY_RAW\s*=\s*["']?1/.test(cmd)) return { action: 'skip', reason: 'escape-env' };
  if (ESCAPE_COMMENT.test(cmd)) return { action: 'skip', reason: 'escape-comment' };

  if (ctx.tool === 'PowerShell') return decidePowerShell(cmd, ctx);

  // 2. Already wrapped
  const segments = splitTopLevel(cmd);
  const tokens = segments.map(firstToken);
  if (usesRtk(cmd)) return { action: 'skip', reason: 'already-wrapped' };

  // 3. Allow/deny lists on first tokens
  const exclude = new Set(ctx.excludeCommands || []);
  if (tokens.some((t) => exclude.has(t))) return { action: 'skip', reason: 'excluded' };
  const only = ctx.onlyCommands || [];
  if (only.length && !tokens.some((t) => only.includes(t))) return { action: 'skip', reason: 'not-in-only' };

  // 4. Constructs we never touch
  if (hasHeredoc(cmd)) return { action: 'skip', reason: 'heredoc' };
  if (hasSubstitution(cmd)) return { action: 'skip', reason: 'substitution' };
  if (hasRedirect(cmd)) return { action: 'skip', reason: 'redirect' };
  if (isBackgrounded(cmd)) return { action: 'skip', reason: 'background' };
  if (tokens.every((t) => STATE_ONLY_TOKENS.has(t))) return { action: 'skip', reason: 'interactive-or-stateful' };
  if (segments.some(segmentIsInteractive)) return { action: 'skip', reason: 'interactive-or-stateful' };
  if (/(^|[\s'"/])\.nxy[\\/]/.test(cmd)) return { action: 'skip', reason: 'nxy-internal' };

  // 5. RTK's own hook is present: it already rewrites; a second updatedInput would be undefined behavior
  if (ctx.rtkHookDetected) return { action: 'skip', reason: 'rtk-hook-present' };

  // 6. Ask the engine
  if (!ctx.rewrite) return { action: 'skip', reason: 'no-engine' };
  // Ask about a spelling rtk understands (./mvnw.cmd → mvnw.cmd, npm test → npm run test); the
  // original string is what runs if rtk declines.
  // A trailing `| tail -n N` only caps output; rtk's filtered output is already short, so ask without it.
  const asked = normalizeForRtk(stripTrailingLimit(cmd));
  let res;
  try {
    res = ctx.rewrite(asked);
  } catch {
    return { action: 'skip', reason: 'rtk-error' };
  }
  const rewritten = (res.stdout || '').trim();
  if (res.code === 0 && rewritten && rewritten !== asked) return { action: 'rewrite', reason: 'rtk', command: rewritten, allow: true };
  if (res.code === 3 && rewritten && rewritten !== asked) return { action: 'rewrite', reason: 'rtk-ask', command: rewritten, allow: false };
  if (res.code === 1) return { action: 'skip', reason: 'rtk-none' };
  if (res.code === 2) return { action: 'skip', reason: 'rtk-deny' };
  return { action: 'skip', reason: 'rtk-unchanged' };
}
