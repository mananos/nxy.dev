// @ts-nocheck
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { parseHits, parseMemIds, memResultRows, memDetailLines, hitRows, clipOutput } from '../core/memview.mjs';

test('parseHits: backticks optional, drive letters, symbol of the same line, de-duplicated, max 10', () => {
  const text = [
    '- `src/a.mjs:10` — function foo',
    '- core/b.mjs:7 bar',
    '- C:/x/y.mjs:12 (class Z)',
    '- C:\\x\\w.mjs:3: baz',
    'repeat src/a.mjs:10 again',
    'see https://example.com:80 and no hit here',
  ].join('\n');
  const hits = parseHits(text);
  assert.deepEqual(hits.map((h) => `${h.path}:${h.line}`), ['src/a.mjs:10', 'core/b.mjs:7', 'C:/x/y.mjs:12', 'C:\\x\\w.mjs:3']);
  assert.equal(hits[0].note, 'function foo');
  assert.equal(hits[1].note, 'bar');
  assert.equal(hits[2].note, 'class Z');
  assert.equal(hits[3].note, 'baz');
  const many = Array.from({ length: 15 }, (_, i) => `f${i}.mjs:${i + 1}`).join('\n');
  assert.equal(parseHits(many).length, 10);
  assert.deepEqual(parseHits(''), []);
  assert.deepEqual(parseHits(null), []);
});

test('parseMemIds: the "- id — why" lines, none gives nothing', () => {
  const text = '- decision-gate-20260101 — aplica al gate\n- other-note - same topic\n- bare-id';
  assert.deepEqual(parseMemIds(text), [
    { id: 'decision-gate-20260101', why: 'aplica al gate' },
    { id: 'other-note', why: 'same topic' },
    { id: 'bare-id', why: '' },
  ]);
  assert.deepEqual(parseMemIds('none — looked for X'), []);
});

test('memResultRows, memDetailLines and hitRows', () => {
  assert.deepEqual(memResultRows([{ id: 'a', title: 'T', type: 'decision', area: 'core', scope: 'repo' }, null, {}]), [
    { id: 'a', press: 'mem:a', text: 'T', sub: 'decision · core · repo' },
  ]);
  assert.deepEqual(memResultRows(undefined), []);
  const lines = memDetailLines(
    { id: 'a', title: 'T', type: 'decision', scope: 'repo', private: true, keywords: ['k1', 'k2'], body: 'uno\ndos' },
    [{ kind: 'related', dir: 'out', other: 'b' }, { kind: 'supersedes', dir: 'in', other: 'c' }],
  );
  assert.deepEqual(lines, ['T', 'decision · repo · privada', 'palabras: k1, k2', '→ related b', '← supersedes c', '', 'uno', 'dos']);
  // title, blank, then the body clipped to 30 lines plus the «more lines» marker
  assert.equal(memDetailLines({ id: 'a', body: Array.from({ length: 50 }, (_, i) => `l${i}`).join('\n') }, []).length, 2 + 31);
  assert.deepEqual(memDetailLines(null, []), []);
  const long = 'C:/very/long/path/that/must/never/be/cut/because/it/is/the/point/of/the/row/file.mjs';
  assert.deepEqual(hitRows([{ path: long, line: 9, kind: 'function', name: 'foo' }, { path: 'a.mjs', line: 1 }, { path: 'b.mjs', line: 2, note: 'why' }]), [
    { loc: `${long}:9`, rest: 'function foo', text: `${long}:9  function foo` },
    { loc: 'a.mjs:1', rest: '', text: 'a.mjs:1' },
    { loc: 'b.mjs:2', rest: 'why', text: 'b.mjs:2  why' },
  ]);
  assert.equal(clipOutput(Array.from({ length: 40 }, (_, i) => `l${i}`).join('\n'), 5).length, 6);
});

test('memview.mjs is pure: no node: imports, no imports at all', () => {
  const src = readFileSync(fileURLToPath(new URL('../core/memview.mjs', import.meta.url)), 'utf8');
  assert.ok(!/from\s+['"]node:/.test(src));
  assert.ok(!/^import\s/m.test(src));
});
