// @ts-check
/**
 * Shared by the transcript goldens: the synthetic Claude Code project the 1.x tests use
 * (`tests/fixtures/make-transcript.mjs`) as plain texts, and helpers to write them back to disk
 * and to make outputs machine independent.
 */
import { mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, relative, sep } from 'node:path';
import { writeFixture, writeTimingFixture } from '../../tests/fixtures/make-transcript.mjs';

// The personal-path test in tests/golden-filter.test.mjs rejects /home/<x>/.
const anonymize = (/** @type {string} */ text) => text.split('/home/dev/').join('/srv/dev/');

/** @returns {string[]} every file under dir, as paths */
function walk(dir) {
  /** @type {string[]} */
  const out = [];
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    const p = join(dir, e.name);
    if (e.isDirectory()) out.push(...walk(p));
    else out.push(p);
  }
  return out;
}

/** @returns {Record<string, string>} relative path (forward slashes) -> file text, sorted by path */
export function fixtureTexts() {
  const root = mkdtempSync(join(tmpdir(), 'nxy-golden-'));
  try {
    writeFixture(root);
    writeTimingFixture(root);
    /** @type {Record<string, string>} */
    const texts = {};
    for (const p of walk(root).sort()) {
      texts[relative(root, p).split(sep).join('/')] = anonymize(readFileSync(p, 'utf8'));
    }
    return texts;
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
}

/**
 * Writes texts under a new temp dir.
 * @param {Record<string, string>} texts relative path -> text
 * @returns {string} the root (the caller removes it)
 */
export function materialize(texts) {
  const root = mkdtempSync(join(tmpdir(), 'nxy-golden-'));
  for (const [rel, text] of Object.entries(texts)) {
    const p = join(root, ...rel.split('/'));
    mkdirSync(dirname(p), { recursive: true });
    writeFileSync(p, text);
  }
  return root;
}

/**
 * Every string starting with root becomes `<root>` + the rest with backslashes as slashes
 * (same rule as goldentest.NormRoot).
 * @param {any} value
 * @param {string} root
 * @returns {any}
 */
export function normRoot(value, root) {
  if (typeof value === 'string') return value.startsWith(root) ? `<root>${value.slice(root.length).replace(/\\/g, '/')}` : value;
  if (Array.isArray(value)) return value.map((v) => normRoot(v, root));
  if (value && typeof value === 'object') return Object.fromEntries(Object.entries(value).map(([k, v]) => [k, normRoot(v, root)]));
  return value;
}

/**
 * Map -> object, Set -> array, recursively; the shape JSON.stringify would not keep.
 * @param {any} value
 * @returns {any}
 */
export function plain(value) {
  if (value instanceof Map) return Object.fromEntries([...value].map(([k, v]) => [k, plain(v)]));
  if (value instanceof Set) return [...value].map(plain);
  if (Array.isArray(value)) return value.map(plain);
  if (value && typeof value === 'object') return Object.fromEntries(Object.entries(value).map(([k, v]) => [k, plain(v)]));
  return value;
}
