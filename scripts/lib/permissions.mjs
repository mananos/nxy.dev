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
 * @param {string} cmd original (pre-rewrite) command
 * @param {{allow: string[], deny: string[]}} rules
 * @returns {'deny'|'allow'|'none'}
 */
export function originalVerdict(cmd, rules) {
  if (matchesAnyRule(cmd, rules.deny)) return 'deny';
  if (matchesAnyRule(cmd, rules.allow)) return 'allow';
  return 'none';
}
