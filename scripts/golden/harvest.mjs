// @ts-check
/**
 * Pulls real Bash/PowerShell commands out of Claude Code transcripts into the golden corpus.
 *
 *   node scripts/golden/harvest.mjs [--max N] <transcript.jsonl>...
 *
 * Commands are anonymised (home dir -> `~`, repo root -> `.`) and dropped when they are long or look
 * private (emails, tokens, credentials in URLs, paths outside this repo / the home dir). New ones are
 * appended to corpus/harvested.json as `h-NNN`; record their rtk answers afterwards with
 * `node scripts/golden/filter.mjs --record`. Read the file before committing it.
 */
import { readFileSync } from 'node:fs';
import { homedir, userInfo } from 'node:os';
import { basename, dirname, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { loadCorpus, ROOT, saveCorpus } from './lib.mjs';

const MAX_LEN = 500;

/** @param {string} s */
const escapeRe = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

/** Every spelling of a directory: `C:\a\b`, `C:/a/b`, `/c/a/b`, with either slash style in the tail. */
function spellings(dir) {
  const fwd = dir.replace(/\\/g, '/').replace(/\/+$/, '');
  const out = new Set([fwd]);
  const drive = /^([A-Za-z]):(\/.*)$/.exec(fwd);
  if (drive) {
    out.add(`/${drive[1].toLowerCase()}${drive[2]}`);
    out.add(`/${drive[1].toUpperCase()}${drive[2]}`);
  }
  const res = [];
  for (const s of out) {
    const body = s.split('/').map(escapeRe).join('[\\\\/]+');
    res.push(new RegExp(`${body}(?=[\\\\/\\s"']|$)`, 'gi'));
  }
  return res;
}

/** @param {string} cmd */
export function anonymise(cmd) {
  let out = cmd;
  for (const re of spellings(ROOT)) out = out.replace(re, '.');
  for (const re of spellings(homedir())) out = out.replace(re, '~');
  // the repo root already written relative to the home dir (`~/Documents/x/repo`)
  const rel = relative(homedir(), ROOT).replace(/\\/g, '/');
  if (rel && !rel.startsWith('..')) out = out.replace(new RegExp(`~/${escapeRe(rel)}(?=[\\\\/\\s"']|$)`, 'g'), '.');
  // any other user's home, in every spelling
  out = out
    .replace(/[A-Za-z]:[\\/]+Users[\\/]+[^\\/\s"']+/gi, '~')
    .replace(/(?<![\w.])\/[A-Za-z]\/Users\/[^/\s"']+/g, '~')
    .replace(/(?<![\w.])\/(?:home|Users)\/[^/\s"'~]+/g, '~');
  return out;
}

/** @param {string} cmd already anonymised @returns {string | null} reason to drop, null to keep */
export function dropReason(cmd) {
  if (cmd.length > MAX_LEN) return 'long';
  // the user name survives in Claude Code project dir names (`C--Users-<name>-…`) and the like
  for (const n of new Set([basename(homedir()), userInfo().username])) {
    if (n.length >= 3 && new RegExp(`(?<![A-Za-z])${escapeRe(n)}(?![A-Za-z])`, 'i').test(cmd)) return 'username';
  }
  // a sibling checkout next to this repo is somebody's other project
  const parent = relative(homedir(), dirname(ROOT)).replace(/\\/g, '/');
  if (parent && !parent.startsWith('..') && cmd.includes(`~/${parent}`)) return 'sibling-repo';
  if (/[\w.+-]+@[\w-]+\.[\w.-]+/.test(cmd)) return 'email';
  if (/\b(?:sk-[A-Za-z0-9]|ghp_|gho_|ghs_|github_pat_|xox[abp]-|AKIA[0-9A-Z]|eyJ[A-Za-z0-9])/.test(cmd)) return 'token';
  if (/(?:token|secret|passw(?:or)?d|api[_-]?key|authorization|bearer|credential)/i.test(cmd)) return 'secret-word';
  if (/:\/\/[^/\s@]*:[^/\s@]*@/.test(cmd) || /:\/\/[^/\s@]+@/.test(cmd)) return 'url-credentials';
  if (/(?<![\w.])[A-Za-z]:[\\/]/.test(cmd) || /(?<![\w.])\/(?:[A-Za-z]|mnt|home|Users|root)\//.test(cmd)) return 'outside-path';
  return null;
}

/** @param {string} file @returns {Array<{tool: string, command: string}>} */
function commandsOf(file) {
  const out = [];
  for (const line of readFileSync(file, 'utf8').split('\n')) {
    if (!line.includes('tool_use')) continue;
    let o;
    try {
      o = JSON.parse(line);
    } catch {
      continue;
    }
    if (o?.type !== 'assistant' || !Array.isArray(o.message?.content)) continue;
    for (const part of o.message.content) {
      if (part?.type !== 'tool_use' || (part.name !== 'Bash' && part.name !== 'PowerShell')) continue;
      if (typeof part.input?.command === 'string') out.push({ tool: part.name, command: part.input.command });
    }
  }
  return out;
}

function main() {
  const args = process.argv.slice(2);
  let max = 150;
  const files = [];
  for (let i = 0; i < args.length; i++) {
    if (args[i] === '--max') max = Number(args[++i]);
    else files.push(args[i]);
  }
  if (!files.length) {
    console.error('usage: node scripts/golden/harvest.mjs [--max N] <transcript.jsonl>...');
    process.exit(2);
  }
  const existing = loadCorpus('harvested');
  const seen = new Set([...loadCorpus('commands'), ...existing].map((c) => c.command));
  let next = existing.reduce((n, c) => Math.max(n, Number(/^h-(\d+)$/.exec(c.name)?.[1] ?? 0)), 0) + 1;
  let harvested = 0;
  const dropped = /** @type {Record<string, number>} */ ({});
  let duplicates = 0;
  for (const file of files) {
    for (const { tool, command } of commandsOf(file)) {
      if (harvested >= max) break;
      const cmd = anonymise(command);
      if (!cmd.trim()) continue;
      const why = dropReason(cmd);
      if (why) {
        dropped[why] = (dropped[why] || 0) + 1;
        continue;
      }
      if (seen.has(cmd)) {
        duplicates++;
        continue;
      }
      seen.add(cmd);
      existing.push({ name: `h-${String(next++).padStart(3, '0')}`, command: cmd, tool });
      harvested++;
    }
  }
  saveCorpus('harvested', existing);
  console.log(`harvested ${harvested}, duplicates ${duplicates}, dropped ${JSON.stringify(dropped)}`);
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) main();
