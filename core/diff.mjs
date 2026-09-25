// @ts-check
/**
 * Line diff for the review (0.4.2): what a plan changed, between the copy nxy kept before the plan's
 * first edit of a file and the file as it is now. No git: it works outside a repo, across repos, and
 * never mixes in changes the user already had uncommitted.
 *
 * Common prefix and suffix are trimmed first (an edit touches a few lines of a long file), and the
 * middle is an LCS. A middle too large for that is reported as replaced whole — correct, just less
 * precise, and only for rewrites of thousands of lines.
 *
 * Pure: text in, hunks out.
 */

/** Past this many cells the LCS table costs more than it is worth. */
const MAX_LCS_CELLS = 4_000_000;

/**
 * @typedef {{op: ' ' | '-' | '+', line: string}} Op
 * @typedef {{oldStart: number, oldLines: number, newStart: number, newLines: number, ops: Op[]}} Hunk
 */

const splitLines = (text) => {
  if (!text) return [];
  const lines = String(text).replace(/\r\n/g, '\n').split('\n');
  if (lines[lines.length - 1] === '') lines.pop();
  return lines;
};

/** @param {string[]} a @param {string[]} b @returns {Op[]} */
function lcs(a, b) {
  const n = a.length;
  const m = b.length;
  const w = m + 1;
  const t = new Uint32Array((n + 1) * w);
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      t[i * w + j] = a[i] === b[j] ? t[(i + 1) * w + j + 1] + 1 : Math.max(t[(i + 1) * w + j], t[i * w + j + 1]);
    }
  }
  /** @type {Op[]} */
  const out = [];
  let i = 0;
  let j = 0;
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      out.push({ op: ' ', line: a[i] });
      i++;
      j++;
    } else if (t[(i + 1) * w + j] >= t[i * w + j + 1]) out.push({ op: '-', line: a[i++] });
    else out.push({ op: '+', line: b[j++] });
  }
  while (i < n) out.push({ op: '-', line: a[i++] });
  while (j < m) out.push({ op: '+', line: b[j++] });
  return out;
}

/**
 * @param {string} before
 * @param {string} after
 * @returns {Op[]}
 */
export function diffLines(before, after) {
  const a = splitLines(before);
  const b = splitLines(after);
  let p = 0;
  while (p < a.length && p < b.length && a[p] === b[p]) p++;
  let s = 0;
  while (s < a.length - p && s < b.length - p && a[a.length - 1 - s] === b[b.length - 1 - s]) s++;
  const A = a.slice(p, a.length - s);
  const B = b.slice(p, b.length - s);
  const mid = A.length * B.length > MAX_LCS_CELLS
    ? [...A.map((line) => ({ op: /** @type {const} */ ('-'), line })), ...B.map((line) => ({ op: /** @type {const} */ ('+'), line }))]
    : lcs(A, B);
  const same = (lines) => lines.map((line) => ({ op: /** @type {const} */ (' '), line }));
  return [...same(a.slice(0, p)), ...mid, ...same(a.slice(a.length - s))];
}

/**
 * Groups a diff into hunks with `context` unchanged lines around each change (unified-diff style).
 * @param {Op[]} ops
 * @param {number} [context]
 * @returns {Hunk[]}
 */
export function hunks(ops, context = 3) {
  /** @type {Hunk[]} */
  const out = [];
  const changed = ops.map((o, i) => (o.op === ' ' ? -1 : i)).filter((i) => i >= 0);
  if (!changed.length) return out;
  // Ranges of op indexes to show, merged when their context overlaps.
  /** @type {[number, number][]} */
  const ranges = [];
  for (const i of changed) {
    const from = Math.max(0, i - context);
    const to = Math.min(ops.length - 1, i + context);
    const last = ranges[ranges.length - 1];
    if (last && from <= last[1] + 1) last[1] = Math.max(last[1], to);
    else ranges.push([from, to]);
  }
  // Line numbers (1-based) before each op.
  let oldNo = 1;
  let newNo = 1;
  const at = ops.map((o) => {
    const pos = { oldNo, newNo };
    if (o.op !== '+') oldNo++;
    if (o.op !== '-') newNo++;
    return pos;
  });
  for (const [from, to] of ranges) {
    const slice = ops.slice(from, to + 1);
    out.push({
      oldStart: at[from].oldNo,
      oldLines: slice.filter((o) => o.op !== '+').length,
      newStart: at[from].newNo,
      newLines: slice.filter((o) => o.op !== '-').length,
      ops: slice,
    });
  }
  return out;
}

/**
 * Unified diff text of one file, plus counts and the new-file line ranges each hunk covers (what a
 * finding's `path:line` is checked against).
 * @param {string} label the path shown in the header
 * @param {string|null} before null when the file is new
 * @param {string|null} after null when the file was deleted
 */
export function fileDiff(label, before, after) {
  const ops = diffLines(before ?? '', after ?? '');
  const hs = hunks(ops);
  const added = ops.filter((o) => o.op === '+').length;
  const removed = ops.filter((o) => o.op === '-').length;
  const header = [`--- ${before == null ? '/dev/null' : `a/${label}`}`, `+++ ${after == null ? '/dev/null' : `b/${label}`}`];
  const body = hs.flatMap((h) => [
    `@@ -${h.oldStart},${h.oldLines} +${h.newStart},${h.newLines} @@`,
    ...h.ops.map((o) => `${o.op}${o.line}`),
  ]);
  return {
    text: hs.length ? [...header, ...body].join('\n') : '',
    added,
    removed,
    ranges: hs.map((h) => /** @type {[number, number]} */ ([h.newStart, h.newStart + Math.max(h.newLines, 1) - 1])),
  };
}
