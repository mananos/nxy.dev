// Where a subagent run's time goes: model / Bash / other tools / waiting, with
// usage-limit gaps excluded and counted apart. Pure: works on parsed transcript lines.
import { LOCATE_NUDGE_MARK, isLocateCommand } from '../search-scope.mjs';
import { table, fmtDuration, fmtPct, clip } from '../format.mjs';

const LIMIT_TEXT = /(usage|session|rate)\s+limit|limit reached|limit will reset/i;
const SHELL = new Set(['Bash', 'PowerShell']);

function blocks(ev) {
  const c = ev?.message?.content;
  return Array.isArray(c) ? c : [];
}

function resultText(b) {
  const c = b?.content;
  if (typeof c === 'string') return c;
  if (Array.isArray(c)) return c.map((x) => (typeof x?.text === 'string' ? x.text : '')).join('\n');
  return '';
}

function isLimitEvent(ev) {
  if (ev.apiError === 'usage_limit_reached' || ev.error === 'rate_limit') return true;
  if (ev.type === 'assistant' && (ev.message?.model === '<synthetic>')) {
    const text = blocks(ev).map((b) => (typeof b?.text === 'string' ? b.text : '')).join('\n');
    return LIMIT_TEXT.test(text);
  }
  return false;
}

function commandText(name, input) {
  if (SHELL.has(name)) return String(input?.command ?? '');
  const arg = input?.file_path ?? input?.pattern ?? input?.path ?? '';
  return arg ? `${name} ${arg}` : name;
}

/**
 * @param {object[]} events one transcript's parsed lines (file order)
 */
export function agentTiming(events) {
  const evs = [];
  for (const ev of events || []) {
    const ts = Date.parse(ev?.timestamp);
    if (Number.isFinite(ts)) evs.push({ ev, ts });
  }
  const empty = { wallMs: 0, modelMs: 0, bashMs: 0, toolMs: 0, waitMs: 0, limitStops: 0, limitMs: 0, slowest: [], locateCalls: 0, nudges: 0 };
  if (!evs.length) return empty;

  let first = Infinity;
  let last = -Infinity;
  for (const e of evs) {
    if (e.ts < first) first = e.ts;
    if (e.ts > last) last = e.ts;
  }

  // usage-limit stops: from the limit event to the next user/assistant event
  let limits = [];
  for (let i = 0; i < evs.length; i++) {
    if (!isLimitEvent(evs[i].ev)) continue;
    let end = evs[i].ts;
    for (let j = i + 1; j < evs.length; j++) {
      const t = evs[j].ev.type;
      if (t === 'user' || t === 'assistant') { end = evs[j].ts; break; }
    }
    limits.push([evs[i].ts, Math.max(end, evs[i].ts)]);
  }
  limits.sort((a, b) => a[0] - b[0]);
  const merged = [];
  for (const iv of limits) {
    const prev = merged[merged.length - 1];
    if (prev && iv[0] <= prev[1]) prev[1] = Math.max(prev[1], iv[1]);
    else merged.push([iv[0], iv[1]]);
  }
  limits = merged;

  // model intervals per request
  const model = [];
  const reqs = new Map();
  let lastOther = null;
  for (const { ev, ts } of evs) {
    if (ev.type === 'assistant' && ev.message?.model !== '<synthetic>') {
      const key = ev.requestId || ev.message?.id;
      if (!key) continue;
      const r = reqs.get(key);
      if (r) r.end = Math.max(r.end, ts);
      else reqs.set(key, { start: lastOther ?? ts, end: ts });
    } else if (ev.type !== 'assistant') {
      lastOther = ts;
    }
  }
  for (const r of reqs.values()) model.push([Math.min(r.start, r.end), r.end]);

  // command intervals
  const uses = new Map();
  const cmds = [];
  let locateCalls = 0;
  let nudges = 0;
  for (const { ev, ts } of evs) {
    for (const b of blocks(ev)) {
      if (b?.type === 'tool_use' && b.id) {
        uses.set(b.id, { ts, name: b.name, input: b.input });
        if (SHELL.has(b.name) && isLocateCommand(String(b.input?.command ?? ''))) locateCalls++;
      } else if (b?.type === 'tool_result') {
        if (resultText(b).includes(LOCATE_NUDGE_MARK)) nudges++;
        const u = uses.get(b.tool_use_id);
        if (u) {
          uses.delete(b.tool_use_id);
          cmds.push({ tool: u.name, text: commandText(u.name, u.input), start: u.ts, end: Math.max(ts, u.ts) });
        }
      }
    }
  }

  // sweep: priority limit > bash > tools > model > waiting
  const marks = [];
  const add = (iv, kind) => {
    const s = Math.max(iv[0], first);
    const e = Math.min(iv[1], last);
    if (e > s) { marks.push([s, kind, 1], [e, kind, -1]); }
  };
  for (const iv of limits) add(iv, 'limit');
  for (const c of cmds) add([c.start, c.end], SHELL.has(c.tool) ? 'bash' : 'tools');
  for (const iv of model) add(iv, 'model');
  marks.sort((a, b) => a[0] - b[0]);
  const count = { limit: 0, bash: 0, tools: 0, model: 0 };
  const sum = { limit: 0, bash: 0, tools: 0, model: 0, wait: 0 };
  let cursor = first;
  for (let i = 0; i < marks.length; ) {
    const t = marks[i][0];
    if (t > cursor) {
      const k = count.limit > 0 ? 'limit' : count.bash > 0 ? 'bash' : count.tools > 0 ? 'tools' : count.model > 0 ? 'model' : 'wait';
      sum[k] += t - cursor;
      cursor = t;
    }
    while (i < marks.length && marks[i][0] === t) { count[marks[i][1]] += marks[i][2]; i++; }
  }
  if (last > cursor) sum.wait += last - cursor; // nothing open past the last mark

  const slowest = cmds
    .map((c) => ({ tool: c.tool, text: c.text, ms: c.end - c.start }))
    .sort((a, b) => b.ms - a.ms)
    .slice(0, 5);

  return {
    wallMs: last - first - sum.limit,
    modelMs: sum.model,
    bashMs: sum.bash,
    toolMs: sum.tools,
    waitMs: sum.wait,
    limitStops: limits.length,
    limitMs: sum.limit,
    slowest,
    locateCalls,
    nudges,
  };
}

/** Nearest rank on an ascending array (same rule as entries/gate.mjs). */
export function quantile(sorted, p) {
  if (!sorted.length) return 0;
  return sorted[Math.min(sorted.length - 1, Math.floor(sorted.length * p))];
}

/**
 * @param {{agentType: string, timing: ReturnType<typeof agentTiming>}[]} runs
 */
export function summarizeTiming(runs) {
  const groups = new Map();
  const all = [];
  let limitStops = 0;
  for (const { agentType, timing } of runs || []) {
    if (!timing) continue;
    const type = agentType || 'unknown';
    if (!groups.has(type)) groups.set(type, []);
    groups.get(type).push(timing);
    limitStops += timing.limitStops || 0;
    for (const s of timing.slowest || []) all.push({ agentType: type, ...s });
  }
  const types = [];
  for (const [agentType, ts] of groups) {
    const walls = ts.map((t) => t.wallMs).sort((a, b) => a - b);
    const total = (k) => ts.reduce((n, t) => n + (t[k] || 0), 0);
    types.push({
      agentType,
      runs: ts.length,
      medianMs: quantile(walls, 0.5),
      p90Ms: quantile(walls, 0.9),
      maxMs: walls[walls.length - 1],
      modelMs: total('modelMs'),
      bashMs: total('bashMs'),
      toolMs: total('toolMs'),
      waitMs: total('waitMs'),
      limitStops: total('limitStops'),
      withLocate: ts.filter((t) => t.locateCalls > 0).length,
      nudges: total('nudges'),
    });
  }
  types.sort((a, b) => b.runs - a.runs || a.agentType.localeCompare(b.agentType));
  all.sort((a, b) => b.ms - a.ms);
  return { types, slowest: all.slice(0, 5), limitStops };
}

export function formatAgentTiming(summary) {
  const types = summary?.types || [];
  if (!types.length) return ['no subagent runs'];
  const rows = types.map((t) => {
    const tot = t.modelMs + t.bashMs + t.toolMs + t.waitMs;
    const pct = (v) => (tot > 0 ? (v / tot) * 100 : null);
    return { ...t, mp: pct(t.modelMs), bp: pct(t.bashMs), tp: pct(t.toolMs), wp: pct(t.waitMs), locate: `${t.withLocate}/${t.runs}` };
  });
  const out = table(rows, [
    { key: 'agentType', label: 'agent' },
    { key: 'runs', label: 'runs', align: 'right' },
    { key: 'medianMs', label: 'median', align: 'right', fmt: fmtDuration },
    { key: 'p90Ms', label: 'p90', align: 'right', fmt: fmtDuration },
    { key: 'maxMs', label: 'max', align: 'right', fmt: fmtDuration },
    { key: 'mp', label: 'model', align: 'right', fmt: (v) => fmtPct(v) },
    { key: 'bp', label: 'bash', align: 'right', fmt: (v) => fmtPct(v) },
    { key: 'tp', label: 'tools', align: 'right', fmt: (v) => fmtPct(v) },
    { key: 'wp', label: 'waiting', align: 'right', fmt: (v) => fmtPct(v) },
    { key: 'locate', label: 'locate', align: 'right' },
  ]).split('\n');
  if (summary.slowest?.length) {
    out.push('', 'slowest commands');
    for (const s of summary.slowest) out.push(`  ${fmtDuration(s.ms).padStart(6)}  ${s.agentType}  ${clip(s.text.replace(/\s+/g, ' '), 70)}`);
  }
  if (summary.limitStops > 0) out.push('', `usage-limit stops excluded: ${summary.limitStops}`);
  return out;
}
