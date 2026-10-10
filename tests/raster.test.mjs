import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { PALETTE, DEFAULT, hex, mix, cells, gaugeCells, progressCells } from '../core/raster.mjs'

/** Decode the base64 into rows of [cp, fg, bg]. */
function decode(s) {
  const buf = Buffer.from(s, 'base64')
  const w = new Uint32Array(buf.buffer, buf.byteOffset, buf.length / 4)
  const out = []
  for (let i = 0; i < w.length; i += 3) out.push([w[i], w[i + 1], w[i + 2]])
  return out
}

const filled = (s) => decode(s).filter(([cp]) => cp === 0x2501).length

test('cells encodes columns*rows triplets of u32', () => {
  const s = cells(5, 2, () => ['x', 1, 2])
  assert.equal(Buffer.from(s, 'base64').length, 5 * 2 * 3 * 4)
  assert.deepEqual(decode(s)[0], [120, 1, 2])
})

test('gaugeCells fills up to ratio and clamps out-of-range values', () => {
  const stops = [PALETTE.green, PALETTE.red]
  assert.equal(filled(gaugeCells(10, 0.5, stops)), 5)
  assert.equal(filled(gaugeCells(10, -3, stops)), 0)
  assert.equal(filled(gaugeCells(10, 7, stops)), 10)
})

test('mix returns the extremes at 0 and 1', () => {
  const stops = [PALETTE.green, PALETTE.violet, PALETTE.red]
  assert.equal(mix(stops, 0), hex(PALETTE.green))
  assert.equal(mix(stops, 1), hex(PALETTE.red))
})

test('progressCells: running segment changes with frame, others do not', () => {
  /** @type {Array<'done'|'running'|'pending'|'red'>} */
  const states = ['done', 'running', 'pending']
  const a = decode(progressCells(30, states, 0))
  const b = decode(progressCells(30, states, 4))
  assert.notDeepEqual(a, b)
  assert.deepEqual(a[0], [0x2501, hex(PALETTE.green), DEFAULT])
  assert.deepEqual(a[28], [0x2500, hex(PALETTE.faint), DEFAULT])
  assert.deepEqual(a.slice(0, 9), b.slice(0, 9))
  assert.equal(decode(progressCells(30, ['red'], 0))[0][1], hex(PALETTE.red))
})

test('raster.mjs stays pure (no node: imports)', () => {
  const src = readFileSync(new URL('../core/raster.mjs', import.meta.url), 'utf8')
  assert.doesNotMatch(src, /node:/)
})
