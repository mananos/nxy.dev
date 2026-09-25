// @ts-check
/**
 * The conventions sheet (0.4.3): the repo's conventions as the implementer receives them, inside
 * the `<nxy-context>` block the agent hook already attaches.
 *
 * Conventions are the user's decisions (answered plan questions, escapes, `mem save --type
 * convention`) — the one thing a fresh subagent cannot infer from the code it reads. Titles only,
 * short or by reference (roadmap 8.5): those of the batch's area first, then the project's, then the
 * rest while they fit; past the budget, the command that lists them all.
 *
 * Pure.
 */

/** ~400 tokens: about 40 titles. Paid once per implementer dispatch. */
export const SHEET_MAX_CHARS = 1600;

/**
 * @param {{title: string, area: string|null}[]} conventions
 * @param {string[]} paths repo-relative paths the batch touches
 * @param {string} listCmd
 * @param {number} [maxChars]
 */
export function conventionSheet(conventions, paths, listCmd, maxChars = SHEET_MAX_CHARS) {
  if (!conventions.length) return '';
  const norm = paths.map((p) => p.replace(/\\/g, '/').toLowerCase());
  const inArea = (c) => {
    if (!c.area) return false;
    const a = c.area.replace(/\\/g, '/').replace(/\/+$/, '').toLowerCase();
    return norm.some((p) => p === a || p.startsWith(`${a}/`) || p.includes(`/${a}/`));
  };
  const ordered = [
    ...conventions.filter(inArea),
    ...conventions.filter((c) => !c.area),
    ...conventions.filter((c) => c.area && !inArea(c)),
  ];
  const lines = [];
  let used = 0;
  for (const c of ordered) {
    const l = `- ${c.title}${c.area ? ` (${c.area})` : ''}`;
    if (used + l.length > maxChars) break;
    lines.push(l);
    used += l.length + 1;
  }
  const rest = ordered.length - lines.length;
  return [
    'Repo conventions (the user\'s decisions: follow them; breaking one is a review finding):',
    ...lines,
    ...(rest ? [`… and ${rest} more: ${listCmd}`] : []),
  ].join('\n');
}

/** Repo-relative paths named in a plan section (`path/to/File.ext`, with or without backticks). */
export function pathsIn(text) {
  const out = new Set();
  for (const m of String(text || '').matchAll(/`?((?:[\w@.-]+\/)+[\w@.-]+\.[A-Za-z0-9]{1,8})(?::\d+)?`?/g)) out.add(m[1]);
  return [...out];
}
