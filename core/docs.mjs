// @ts-check
/**
 * Which docs a plan's change concerns (0.4.3): the `.md` files that name what changed, found by
 * code — no tokens, and no documenter offered when there are none.
 *
 * "Name what changed" = a changed code file's name without extension (`ClienteService`,
 * `cliente.component`) as a whole word. Names too generic to point at anything (`index`, `utils`)
 * and very short ones are left out: they would match every doc in the repo.
 *
 * The folder a CI job syncs to a wiki (Wiki.js, Azure DevOps wiki) is where docs live even when none
 * names the change yet; it is passed along, not searched blindly.
 *
 * Pure.
 */
import { isDocPath, isTestPath } from './review.mjs';

const GENERIC = new Set(['index', 'main', 'app', 'utils', 'util', 'types', 'type', 'config', 'constants', 'routes', 'module', 'helpers', 'helper', 'common', 'shared', 'service', 'model', 'models', 'test', 'spec', 'readme', 'package', 'build', 'settings', 'setup']);

/**
 * The words to look for in docs: one per changed code file.
 * @param {string[]} paths repo-relative
 */
export function docTerms(paths) {
  const out = new Set();
  for (const p of paths) {
    if (isDocPath(p) || isTestPath(p)) continue;
    const base = (p.split('/').pop() || '').replace(/\.[A-Za-z0-9]+$/, '');
    if (base.length < 5 || GENERIC.has(base.toLowerCase())) continue;
    out.add(base);
  }
  return [...out];
}

/** A regex source matching any term as a whole word (dots inside a term are literal). */
export function termsPattern(terms) {
  return terms.map((t) => `\\b${t.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}\\b`).join('|');
}

/**
 * The folder a CI pipeline publishes to a wiki, from the pipeline files' text: a line that talks
 * about a wiki and names a path that exists as a directory in the repo.
 * @param {string[]} pipelineTexts
 * @param {(rel: string) => boolean} isDir
 * @returns {string|null}
 */
export function wikiDirFromCi(pipelineTexts, isDir) {
  for (const text of pipelineTexts) {
    const lines = String(text).split(/\r?\n/);
    for (let i = 0; i < lines.length; i++) {
      if (!/wiki/i.test(lines[i])) continue;
      // The path is usually on the same line or the step's next few lines (`path:`, `source:`, `-d docs`).
      const near = lines.slice(i, i + 6).join(' ');
      for (const m of near.matchAll(/(?:^|[\s"'=:])\.?\/?((?:[\w.-]+\/)*[\w.-]+)\/?(?=[\s"',]|$)/g)) {
        const rel = m[1];
        if (!rel || rel.includes('..') || /^(https?|wiki|true|false|main|master)$/i.test(rel) || !/[a-z]/i.test(rel)) continue;
        if (isDir(rel)) return rel;
      }
    }
  }
  return null;
}

/**
 * What the documenter reads: the plan, what changed, and the docs that name it with the lines
 * where they do.
 * @param {{id: string, plan: string, files: {path: string, status: string, added: number, removed: number, diff: string}[],
 *   docs: {path: string, lines: {line: number, text: string}[]}[], wikiDir: string|null, maxDiffLines?: number}} o
 */
export function docsPacket(o) {
  const max = o.maxDiffLines ?? 1500;
  const out = [
    `# nxy docs packet · review ${o.id}`,
    '',
    '## The plan',
    o.plan.trim(),
    '',
    '## Docs that name what changed (edit these)',
    ...o.docs.flatMap((d) => [`- ${d.path}`, ...d.lines.map((l) => `    ${d.path}:${l.line}: ${l.text}`)]),
    '',
    ...(o.wikiDir ? [`The repo's CI publishes \`${o.wikiDir}/\` to its wiki: that is where its docs live.`, ''] : []),
    '## What changed',
    ...o.files.map((f) => `- ${f.path} (${f.status}, +${f.added} −${f.removed})`),
    '',
  ];
  let used = 0;
  for (const f of o.files) {
    if (!f.diff) continue;
    const n = f.diff.split('\n').length;
    if (used + n > max) {
      out.push(`(diff of ${f.path} left out: read the file if a doc depends on it)`);
      continue;
    }
    used += n;
    out.push(f.diff, '');
  }
  return out.join('\n');
}

/**
 * The docs question for checkpoint 2, and what to do with the answer.
 * @param {string} id review id
 * @param {string[]} docs
 */
export function docsQuestion(id, docs) {
  const shown = docs.slice(0, 4).join(', ') + (docs.length > 4 ? `, +${docs.length - 4} more` : '');
  return {
    question: {
      question: `Update the docs that mention this change (review ${id})?`,
      header: 'Docs',
      multiSelect: false,
      options: [
        { label: 'Update docs', description: shown },
        { label: 'Leave docs', description: 'the docs stay as they are' },
      ],
    },
    then: `If the user picks "Update docs": dispatch the nxy:documenter subagent with "Docs for nxy review ${id}" — it edits only those docs, in this branch.`,
  };
}
