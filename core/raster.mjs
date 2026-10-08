// @ts-check
/**
 * Pure helpers for the panel's Raster drawings (gauges, progress strip).
 *
 * A Raster is a grid of cells, each a [codePoint, fg, bg] u32 triplet, shipped as base64. No
 * Node built-in imports here: the Mod bundle imports this file, and it has to stay testable without a terminal.
 */

/** nxy's "vivid" statusline palette, as hex. */
export const PALETTE = {
  brand: '#ff87ff', green: '#87d787', violet: '#af87ff', blue: '#5fafff', cyan: '#5fd7d7',
  pink: '#d787d7', amber: '#ffaf00', red: '#ff5f5f', ink: '#0b0b12', band: '#1d1630',
  card: '#3b3355', text: '#e6e3f0', muted: '#8a84a3', faint: '#4d4863', keycap: '#2a2540',
};

/** Marker meaning "the terminal's default colour". */
export const DEFAULT = 0x01000000;

const B64 = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';

/** @param {Uint8Array} bytes */
export function b64(bytes) {
  let out = '';
  for (let i = 0; i < bytes.length; i += 3) {
    const a = bytes[i] ?? 0, b = bytes[i + 1] ?? 0, c = bytes[i + 2] ?? 0;
    const n = (a << 16) | (b << 8) | c;
    out += B64[(n >> 18) & 63] + B64[(n >> 12) & 63];
    out += i + 1 < bytes.length ? B64[(n >> 6) & 63] : '=';
    out += i + 2 < bytes.length ? B64[n & 63] : '=';
  }
  return out;
}

/** @param {string} h "#rrggbb" */
export const hex = (h) => parseInt(h.slice(1), 16);

/**
 * @param {number} columns
 * @param {number} rows
 * @param {(x: number, y: number) => [string, number, number]} at
 */
export function cells(columns, rows, at) {
  const words = new Uint32Array(columns * rows * 3);
  for (let y = 0; y < rows; y++) for (let x = 0; x < columns; x++) {
    const [ch, fg, bg] = at(x, y);
    const i = (y * columns + x) * 3;
    words[i] = ch.codePointAt(0) ?? 32;
    words[i + 1] = fg;
    words[i + 2] = bg;
  }
  return b64(new Uint8Array(words.buffer));
}

/**
 * Colour at `t` (0..1) along a gradient of hex stops.
 * @param {string[]} stops
 * @param {number} t
 */
export function mix(stops, t) {
  const s = stops.map(hex);
  const p = Math.min(0.9999, Math.max(0, t)) * (s.length - 1);
  const i = Math.floor(p), f = p - i;
  const a = s[i], b = s[i + 1] ?? s[i];
  const ch = (/** @type {number} */ sh) => Math.round(((a >> sh) & 255) * (1 - f) + ((b >> sh) & 255) * f);
  return (ch(16) << 16) | (ch(8) << 8) | ch(0);
}

/**
 * @param {number} rgb
 * @param {number} t 0..1 towards white
 */
export const lighten = (rgb, t) => {
  const ch = (/** @type {number} */ sh) => Math.round(((rgb >> sh) & 255) + (255 - ((rgb >> sh) & 255)) * t);
  return (ch(16) << 16) | (ch(8) << 8) | ch(0);
};

/**
 * Gauge: gradient fill up to `ratio`, a faint track after.
 * @param {number} width
 * @param {number} ratio
 * @param {string[]} stops
 */
export function gaugeCells(width, ratio, stops) {
  const fill = Math.round(Math.min(1, Math.max(0, ratio)) * width);
  return cells(width, 1, (x) => (x < fill
    ? ['━', mix(stops, x / Math.max(1, width - 1)), DEFAULT]
    : ['─', hex(PALETTE.faint), DEFAULT]));
}

/**
 * Plan progress strip: one segment per batch; the running one shimmers with `frame`.
 * @param {number} width
 * @param {Array<'done'|'running'|'pending'|'red'>} states
 * @param {number} frame
 */
export function progressCells(width, states, frame) {
  const n = states.length, gap = 1;
  const seg = Math.max(2, Math.floor((width - gap * Math.max(0, n - 1)) / Math.max(1, n)));
  return cells(width, 1, (x) => {
    const i = Math.floor(x / (seg + gap)), off = x % (seg + gap);
    const st = states[i];
    if (!st || off >= seg) return [' ', DEFAULT, DEFAULT];
    if (st === 'done') return ['━', hex(PALETTE.green), DEFAULT];
    if (st === 'red') return ['━', hex(PALETTE.red), DEFAULT];
    if (st === 'running') {
      const head = frame % (seg + 6) - 3;
      const glow = Math.max(0, 1 - Math.abs(off - head) / 3);
      return ['━', lighten(hex(PALETTE.blue), glow * 0.75), DEFAULT];
    }
    return ['─', hex(PALETTE.faint), DEFAULT];
  });
}
