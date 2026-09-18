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

export function fmtPct(v, digits = 0) {
  if (v === null || v === undefined || !Number.isFinite(v)) return '?';
  return v.toFixed(digits) + '%';
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
