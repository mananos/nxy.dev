// @ts-check
/** Presentation helpers (ported in spirit from gentle-shell's shell-bar formatters). */

/** 842 · 4.2k · 412k · 1.3M · 12M */
export function fmtTokens(n) {
  if (n === null || n === undefined || !Number.isFinite(n)) return '?';
  if (n < 1000) return String(Math.round(n));
  if (n < 10_000) return (n / 1000).toFixed(1) + 'k';
  if (n < 1_000_000) return Math.round(n / 1000) + 'k';
  if (n < 10_000_000) return (n / 1_000_000).toFixed(1) + 'M';
  return Math.round(n / 1_000_000) + 'M';
}

/** $0.004 below $1, $1.23 above. `null` → '?' */
export function fmtUsd(v) {
  if (v === null || v === undefined || !Number.isFinite(v)) return '?';
  return '$' + (v >= 1 ? v.toFixed(2) : v.toFixed(3));
}

/**
 * Suma de USD que puede estar incompleta: `$164.20+?` cuando alguna llamada no tiene tarifa;
 * `?` cuando no se conoce nada; igual que {@link fmtUsd} cuando la suma es completa.
 * @param {number|null|undefined} v   parte conocida de la suma
 * @param {boolean} [partial]         true si quedaron llamadas sin tarifar
 */
export function fmtUsdPartial(v, partial = false) {
  if (!partial) return fmtUsd(v);
  if (v === null || v === undefined || !Number.isFinite(v) || v === 0) return '?';
  return fmtUsd(v) + '+?';
}

export function fmtPct(v, digits = 0) {
  if (v === null || v === undefined || !Number.isFinite(v)) return '?';
  return v.toFixed(digits) + '%';
}

/** Small ratio such as calls per turn: 0.8 · 4.2 · 15 · 120. `null` → '?' */
export function fmtRatio(v) {
  if (v === null || v === undefined || !Number.isFinite(v)) return '?';
  return v < 10 ? v.toFixed(1) : String(Math.round(v));
}

/** 45s · 12m · 2h 05m · 3d 4h */
export function fmtDuration(ms) {
  if (ms === null || ms === undefined || !Number.isFinite(ms) || ms < 0) return '?';
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ${String(m % 60).padStart(2, '0')}m`;
  return `${Math.floor(h / 24)}d ${h % 24}h`;
}

export function fmtDate(ts, { time = true } = {}) {
  if (!ts) return '?';
  const d = new Date(ts);
  const p = (n) => String(n).padStart(2, '0');
  const date = `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
  return time ? `${date} ${p(d.getHours())}:${p(d.getMinutes())}` : date;
}

/** `▰▰▰▰▱▱▱▱` — 8 cells by default; null → all dim. */
export function gauge(pct, cells = 8) {
  if (pct === null || pct === undefined || !Number.isFinite(pct)) return '▱'.repeat(cells);
  const filled = Math.max(0, Math.min(cells, Math.round((pct / 100) * cells)));
  return '▰'.repeat(filled) + '▱'.repeat(cells - filled);
}

/**
 * ANSI styles for the statusline (Claude Code renders them). A style is a space-separated list
 * of tokens: named colours (`red`, `cyan`, `muted`…), `bold`/`dim`, `#rrggbb` (truecolor) or a
 * bare 0–255 palette index. Unknown tokens are ignored; `text`/empty → unchanged.
 */
const NAMED = {
  bold: '1', dim: '2',
  black: '30', red: '31', green: '32', yellow: '33', blue: '34', magenta: '35', cyan: '36', white: '37',
  muted: '38;5;245', gray: '38;5;245', grey: '38;5;245',
};
function ansiCodes(style) {
  const codes = [];
  for (let tok of String(style || '').trim().split(/\s+/)) {
    if (!tok || tok === 'text') continue;
    // `bg:<colour>` paints the background instead (`bg:80`, `bg:#1e1e2e`, `bg:red`)
    const bg = tok.startsWith('bg:');
    if (bg) tok = tok.slice(3);
    if (Object.hasOwn(NAMED, tok) && typeof NAMED[tok] === 'string') {
      const code = NAMED[tok];
      if (bg && /^3\d$/.test(code)) codes.push(`4${code[1]}`);
      else if (bg && code.startsWith('38;')) codes.push(`48;${code.slice(3)}`);
      else if (!bg) codes.push(code);
    } else if (/^#[0-9a-f]{6}$/i.test(tok)) codes.push(`${bg ? 48 : 38};2;${parseInt(tok.slice(1, 3), 16)};${parseInt(tok.slice(3, 5), 16)};${parseInt(tok.slice(5, 7), 16)}`);
    else if (/^\d{1,3}$/.test(tok) && Number(tok) <= 255) codes.push(`${bg ? 48 : 38};5;${Number(tok)}`);
  }
  return [...new Set(codes)];
}
/**
 * @param {string} text
 * @param {string|null|undefined} style   see above
 * @param {boolean} [enabled]             false → plain text (config `color: false`)
 */
export function colorize(text, style, enabled = true) {
  if (!enabled || !style) return text;
  const codes = ansiCodes(style);
  return codes.length ? `\x1b[${codes.join(';')}m${text}\x1b[0m` : text;
}

/**
 * Severity of a value against two thresholds: `null` below warn, `'warn'` from warn, `'crit'`
 * from crit. Unknown values never get a severity.
 * @param {number|null|undefined} v
 * @returns {'warn'|'crit'|null}
 */
export function severity(v, warn, crit) {
  if (v === null || v === undefined || !Number.isFinite(v)) return null;
  if (v >= crit) return 'crit';
  if (v >= warn) return 'warn';
  return null;
}

/**
 * Plain-text table. `columns`: [{key, label, align?: 'left'|'right', fmt?}]
 * @param {Array<Record<string, any>>} rows
 * @param {Array<{key: string, label: string, align?: 'left'|'right', fmt?: (v: any, row: any) => string}>} columns
 */
export function table(rows, columns) {
  const cells = rows.map((r) => columns.map((c) => (c.fmt ? c.fmt(r[c.key], r) : String(r[c.key] ?? ''))));
  const widths = columns.map((c, i) => Math.max(c.label.length, ...cells.map((row) => row[i].length)));
  const line = (vals) => vals.map((v, i) => (columns[i].align === 'right' ? v.padStart(widths[i]) : v.padEnd(widths[i]))).join('  ').trimEnd();
  return [line(columns.map((c) => c.label)), line(widths.map((w) => '-'.repeat(w))), ...cells.map(line)].join('\n');
}

/** Truncates with an ellipsis. */
export function clip(s, max) {
  s = String(s ?? '');
  return s.length > max ? s.slice(0, Math.max(0, max - 1)) + '…' : s;
}

/**
 * `7d` · `24h` · `30m` · ISO date → epoch ms lower bound; null when unparseable.
 * @param {string|undefined} s
 */
export function parseSince(s, now = Date.now()) {
  if (!s) return null;
  const m = /^(\d+)([smhdw])$/.exec(s.trim());
  if (m) {
    const mult = { s: 1000, m: 60_000, h: 3_600_000, d: 86_400_000, w: 604_800_000 }[m[2]];
    return now - Number(m[1]) * mult;
  }
  const t = Date.parse(s);
  return Number.isNaN(t) ? null : t;
}

/** Tiny argv parser: `--key value`, `--flag`, positional. */
export function parseArgs(argv) {
  /** @type {Record<string, string|boolean>} */
  const opts = {};
  const positional = [];
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a.startsWith('--')) {
      const eq = a.indexOf('=');
      if (eq !== -1) opts[a.slice(2, eq)] = a.slice(eq + 1);
      else if (i + 1 < argv.length && !argv[i + 1].startsWith('--')) opts[a.slice(2)] = argv[++i];
      else opts[a.slice(2)] = true;
    } else positional.push(a);
  }
  return { opts, positional };
}
