// @ts-check
import { appendFileSync, existsSync, readFileSync } from 'node:fs';
import { dirname } from 'node:path';
import { ensureDir } from './paths.mjs';

/** Appends one JSON object as a single line. A single `appendFileSync` of one line is atomic enough for our row sizes. */
export function appendJsonl(path, obj) {
  ensureDir(dirname(path));
  appendFileSync(path, JSON.stringify(obj) + '\n', 'utf8');
}

/**
 * Reads every parseable line. Malformed lines are skipped, never thrown.
 * @template T
 * @param {string} path
 * @param {(row: T) => boolean} [filter]
 * @returns {T[]}
 */
export function readJsonl(path, filter) {
  if (!existsSync(path)) return [];
  const out = [];
  for (const line of readFileSync(path, 'utf8').split('\n')) {
    if (!line.trim()) continue;
    try {
      const row = JSON.parse(line);
      if (!filter || filter(row)) out.push(row);
    } catch {
      /* skip */
    }
  }
  return out;
}
