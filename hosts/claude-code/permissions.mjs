// @ts-check
/**
 * Mirrors Claude Code's `permissions.allow` / `permissions.deny` rules for Bash so a
 * rewritten command can inherit the approval the original would have received.
 *
 * Supported rule forms:
 *   Bash                → every command
 *   Bash(git status)    → exact
 *   Bash(git status:*)  → prefix (`git status` or `git status <anything>`)
 *   Bash(npm * test)    → `*` wildcard anywhere
 */
import { existsSync, readFileSync } from 'node:fs';
import { hasSubstitution, splitTopLevel } from '../../core/shell.mjs';

const RULE = /^Bash(?:\((.*)\))?$/;

function escapeRegex(s) {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

/** @returns {RegExp|null} */
export function ruleToRegex(rule) {
  const m = RULE.exec(rule.trim());
  if (!m) return null;
  const body = m[1];
  if (body === undefined || body === '' || body === '*') return /^[\s\S]*$/;
  if (body.endsWith(':*')) {
    const prefix = body.slice(0, -2);
    return new RegExp(`^${wildcardToRegex(prefix)}(\\s[\\s\\S]*)?$`);
  }
  return new RegExp(`^${wildcardToRegex(body)}$`);
}

function wildcardToRegex(s) {
  return s.split('*').map(escapeRegex).join('[\\s\\S]*');
}

/**
 * @param {string} cmd
 * @param {string[]} rules
 */
export function matchesAnyRule(cmd, rules) {
  const c = cmd.trim();
  for (const r of rules || []) {
    const re = ruleToRegex(String(r));
    if (re && re.test(c)) return true;
  }
  return false;
}

/**
 * Reads `permissions.{allow,deny}` from the given settings files (missing/broken files ignored).
 * @param {string[]} files
 * @returns {{allow: string[], deny: string[]}}
 */
export function loadPermissionRules(files) {
  /** @type {{allow: string[], deny: string[]}} */
  const out = { allow: [], deny: [] };
  for (const f of files) {
    try {
      if (!existsSync(f)) continue;
      const p = JSON.parse(readFileSync(f, 'utf8'))?.permissions;
      if (Array.isArray(p?.allow)) out.allow.push(...p.allow);
      if (Array.isArray(p?.deny)) out.deny.push(...p.deny);
    } catch {
      /* ignore */
    }
  }
  return out;
}

/**
 * The commands a line runs: split at `&&`, `||`, `;`, `|`, newlines and a lone `&` (not `2>&1`,
 * not `&>`). Conservative on purpose: a split inside quotes only yields more parts to approve.
 * @param {string} cmd
 * @returns {string[]}
 */
export function commandSegments(cmd) {
  return String(cmd || '')
    .split(/\r?\n/)
    .flatMap((line) => splitTopLevel(line))
    .flatMap((s) => s.split(/(?<![&>|])&(?![&>])/))
    .map((s) => s.trim())
    .filter(Boolean);
}

/**
 * The verdict Claude Code's rules would give the original command, so the rewrite can inherit it.
 *
 * A chain is only as allowed as its least allowed part — the way Claude Code itself reads it:
 * `Bash(git status:*)` does not approve `git status && rm -rf build`. Matching the whole string
 * would (the prefix rule swallows the rest), and the rewrite would then skip a prompt the user
 * was owed. A substitution (`$(…)`, backticks) hides what runs, so it is never inherited as allowed.
 * @param {string} cmd original (pre-rewrite) command
 * @param {{allow: string[], deny: string[]}} rules
 * @returns {'deny'|'allow'|'none'}
 */
export function originalVerdict(cmd, rules) {
  const segments = commandSegments(cmd);
  if (matchesAnyRule(cmd, rules.deny) || segments.some((s) => matchesAnyRule(s, rules.deny))) return 'deny';
  if (!segments.length || hasSubstitution(cmd)) return 'none';
  return segments.every((s) => matchesAnyRule(s, rules.allow)) ? 'allow' : 'none';
}
