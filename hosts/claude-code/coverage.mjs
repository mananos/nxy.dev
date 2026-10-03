// @ts-check
/**
 * The `## Coverage` lines of a review packet: for each repo without git, what covered it (copies,
 * inventory) and what did not. A repo with git adds nothing.
 */
import { INVENTORY_CAP } from './inventory.mjs';

const norm = (/** @type {string} */ p) => p.replace(/\\/g, '/').toLowerCase();

/**
 * @param {{root: string, name: string, files: number, now: number, skipped: string|null}[]|undefined} coverage
 * @param {{more?: number}} [rv] the review result (`more`: inventory entries past the listing)
 * @param {string[]} [copies] absolute paths of the baseline copies
 * @returns {string[]}
 */
export function coverageLines(coverage, rv, copies = []) {
  if (!coverage || !coverage.length) return [];
  const out = [];
  for (const c of coverage) {
    const prefix = norm(c.root).replace(/\/+$/, '') + '/';
    const n = copies.filter((p) => norm(p).startsWith(prefix)).length;
    const covered = [`copies: ${n} file(s) the plan named or edited`];
    covered.push(c.skipped
      ? `inventory skipped: ${c.skipped} (more than ${INVENTORY_CAP} files), only copies cover this repo`
      : `inventory: ${c.files} file(s) at approval, ${c.now} now, with content hashes for the small ones`);
    const not = ['files in skipped folders (node_modules, target, dist, build, .git, .nxy and similar)'];
    if (!c.skipped) not.push('a change to a file over the copy limit that kept its size and date');
    if (rv?.more) not.push(`edits past the 200-file listing (${rv.more} more)`);
    out.push(`- ${c.name} (${c.root.replace(/\\/g, '/')}, no git): covered by ${covered.join('; ')}. Not covered: ${not.join('; ')}.`);
  }
  return out;
}

/** The `inventory:` line of an empty packet's explanation (null when no repo had one). */
export function inventoryWhy(coverage) {
  if (!coverage || !coverage.length) return null;
  return `inventory: ${coverage.map((c) => `${c.name} ${c.skipped ? c.skipped : `${c.files} at approval, ${c.now} now`}`).join(' · ')}`;
}
