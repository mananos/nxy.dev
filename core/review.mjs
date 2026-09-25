// @ts-check
/**
 * The review of a finished plan (0.4.2).
 *
 * Once per plan, after every batch passed its acceptance command and the full suite ran: one
 * reviewer (Sonnet high) reads a packet nxy assembles — the plan's diff against the copies kept
 * before its first edits, the plan, the lenses that apply and the repo's conventions — and returns
 * findings. It does not fix anything.
 *
 * What is decided by code, not by the model (gentle-ai's lesson, and its cost: 21 `fix(review)`
 * commits in a week were all transaction machinery — nxy keeps this to one packet, one reviewer,
 * one correction round):
 *   - **whether** there is a review: only docs or only tests changed → none;
 *   - **which lenses**: by what changed (paths and content), never by how much;
 *   - **whose finding it is**: a `path:line` inside a changed hunk is the change's; anywhere else it
 *     was there before, and stays informational.
 * The user picks which findings get fixed (checkpoint 2); one implementer round, no second review.
 *
 * Pure: text and lists in, text and lists out.
 */
import { createHash } from 'node:crypto';

export const SEVERITIES = ['high', 'medium', 'low'];

/** How close to a changed hunk a finding may point and still be the change's. */
const NEAR_LINES = 3;

/**
 * @typedef {{name: string, title: string, paths: RegExp[], content: string[], always: boolean, enabled: boolean, body: string}} Lens
 * @typedef {{path: string, content: string|null}} ChangedFile  path relative, forward slashes; content = file now (null if deleted)
 * @typedef {{lens: string, severity: string, file: string, line: number, title: string, evidence: string, fix: string}} Finding
 * @typedef {Finding & {id: string, cause: 'change' | 'preexisting'}} Classified
 */

/**
 * A glob (`**\/entity/**`, `*Controller.java`) as a path matcher. Without a slash it matches the file
 * name anywhere; with one, a path suffix. Case-insensitive: Windows and macOS paths are.
 * @param {string} glob
 */
export function globToRegExp(glob) {
  const g = String(glob).trim().replace(/\\/g, '/').replace(/^\.?\//, '');
  let re = '';
  for (let i = 0; i < g.length; i++) {
    const c = g[i];
    if (c === '*' && g[i + 1] === '*') {
      if (g[i + 2] === '/') {
        re += '(?:.*/)?';
        i += 2;
      } else {
        re += '.*';
        i += 1;
      }
    } else if (c === '*') re += '[^/]*';
    else if (c === '?') re += '[^/]';
    else re += c.replace(/[.+^${}()|[\]\\]/g, '\\$&');
  }
  return new RegExp(`(?:^|/)${re}$`, 'i');
}

const list = (v) => String(v || '').split(',').map((s) => s.trim()).filter(Boolean);

/**
 * A lens file: frontmatter (`name`, `title`, `paths` and `content` as comma-separated lists,
 * `always: true`, `enabled: false`) and the checklist the reviewer applies.
 * @param {string} text
 * @param {string} fallbackName
 * @returns {Lens|null}
 */
export function parseLens(text, fallbackName) {
  const m = /^---\r?\n([\s\S]*?)\r?\n---\r?\n?([\s\S]*)$/.exec(String(text || ''));
  if (!m) return null;
  /** @type {Record<string, string>} */
  const meta = {};
  for (const line of m[1].split(/\r?\n/)) {
    const kv = /^([a-zA-Z_]+):\s*(.*)$/.exec(line.trim());
    if (kv) meta[kv[1]] = kv[2].trim();
  }
  const name = meta.name || fallbackName;
  return {
    name,
    title: meta.title || name,
    paths: list(meta.paths).map(globToRegExp),
    content: list(meta.content),
    always: meta.always === 'true',
    enabled: meta.enabled !== 'false',
    body: m[2].trim(),
  };
}

/**
 * The plugin's lenses with the repo's on top: same name replaces, `enabled: false` removes.
 * @param {Lens[]} builtIn
 * @param {Lens[]} repo
 */
export function mergeLenses(builtIn, repo) {
  const byName = new Map(builtIn.map((l) => [l.name, l]));
  for (const l of repo) byName.set(l.name, l);
  return [...byName.values()].filter((l) => l.enabled);
}

const DOC_RE = /\.(md|mdx|markdown|txt|adoc|asciidoc|rst|png|jpe?g|gif|webp|ico)$/i;
const TEST_RE = /(^|\/)(tests?|__tests__|spec|specs|test-?data)\/|\.(test|spec)\.[cm]?[jt]sx?$|(Test|Tests|Spec|IT)\.(java|kt|scala|groovy)$|_test\.(go|py)$|(^|\/)test_[^/]*\.py$/;

export const isDocPath = (p) => DOC_RE.test(p);
export const isTestPath = (p) => TEST_RE.test(p);

/**
 * Whether the plan's change deserves a review at all. Size never decides it — only what kind of
 * files changed. Only docs, or only tests: none (gentle-ai: "a README asks nothing").
 * @param {string[]} paths
 */
export function reviewNeeded(paths) {
  if (!paths.length) return { needed: false, reason: 'no file changed' };
  if (paths.every(isDocPath)) return { needed: false, reason: 'only documentation changed' };
  if (paths.every((p) => isDocPath(p) || isTestPath(p))) return { needed: false, reason: 'only tests and documentation changed' };
  return { needed: true, reason: '' };
}

/**
 * The lenses that apply to what changed, each with the files that triggered it.
 * @param {Lens[]} lenses
 * @param {ChangedFile[]} files
 */
export function selectLenses(lenses, files) {
  const code = files.filter((f) => !isDocPath(f.path));
  return lenses
    .map((lens) => ({
      lens,
      files: lens.always ? [] : code.filter((f) => lens.paths.some((re) => re.test(f.path))
        || (f.content != null && lens.content.some((c) => f.content?.includes(c)))).map((f) => f.path),
    }))
    .filter(({ lens, files: hit }) => lens.always || hit.length);
}

/**
 * Findings as the reviewer returned them: a JSON array, or `{findings: [...]}`, possibly inside a
 * fenced block. Malformed entries are reported, not guessed.
 * @param {string} text
 * @returns {{findings: Finding[], errors: string[]}}
 */
export function parseFindings(text) {
  const raw = String(text || '').trim().replace(/^```(?:json)?\s*\n?|\n?```\s*$/g, '');
  let data;
  try {
    data = JSON.parse(raw);
  } catch {
    return { findings: [], errors: ['not JSON: expected an array of findings (or [] when there are none)'] };
  }
  const items = Array.isArray(data) ? data : Array.isArray(data?.findings) ? data.findings : null;
  if (!items) return { findings: [], errors: ['expected an array of findings'] };
  /** @type {Finding[]} */
  const findings = [];
  const errors = [];
  items.forEach((f, i) => {
    const line = Number(f?.line);
    const missing = ['file', 'title'].filter((k) => typeof f?.[k] !== 'string' || !f[k].trim());
    if (missing.length || !Number.isInteger(line) || line < 1) {
      errors.push(`finding ${i + 1}: needs ${[...missing, ...(!Number.isInteger(line) || line < 1 ? ['line (a number)'] : [])].join(', ')}`);
      return;
    }
    findings.push({
      lens: typeof f.lens === 'string' && f.lens.trim() ? f.lens.trim() : 'base',
      severity: SEVERITIES.includes(f.severity) ? f.severity : 'medium',
      file: f.file.trim().replace(/\\/g, '/').replace(/^\.\//, '').replace(/:\d+$/, ''),
      line,
      title: f.title.trim(),
      evidence: typeof f.evidence === 'string' ? f.evidence.trim() : '',
      fix: typeof f.fix === 'string' ? f.fix.trim() : '',
    });
  });
  return { findings, errors };
}

/**
 * Whose each finding is: inside (or within a few lines of) a changed hunk → the change's; anywhere
 * else → preexisting. Numbered by severity, the change's first.
 * @param {Finding[]} findings
 * @param {Map<string, [number, number][]>} changed new-file line ranges per changed path
 * @returns {Classified[]}
 */
export function classifyFindings(findings, changed) {
  const lower = new Map([...changed].map(([k, v]) => [k.toLowerCase(), v]));
  const cause = (f) => {
    const ranges = lower.get(f.file.toLowerCase()) || [...lower].find(([k]) => k.endsWith(`/${f.file.toLowerCase()}`))?.[1];
    if (!ranges) return 'preexisting';
    if (!ranges.length) return 'change';
    return ranges.some(([a, b]) => f.line >= a - NEAR_LINES && f.line <= b + NEAR_LINES) ? 'change' : 'preexisting';
  };
  const rank = (f) => (f.cause === 'change' ? 0 : 10) + SEVERITIES.indexOf(f.severity);
  return findings
    .map((f) => ({ ...f, cause: /** @type {'change'|'preexisting'} */ (cause(f)) }))
    .sort((a, b) => rank(a) - rank(b))
    .map((f, i) => ({ ...f, id: `R${i + 1}` }));
}

/** A short id for one review of one plan. */
export function reviewId(planHash, now = Date.now()) {
  return createHash('sha1').update(`${planHash}:${now}`).digest('hex').slice(0, 6);
}

/** The exact question of checkpoint 2. */
export function fixQuestion(id, part = 0, parts = 1) {
  return `Fix which findings of nxy review ${id}${parts > 1 ? ` (${part + 1}/${parts})` : ''}?`;
}

/**
 * The batch a fix belongs to: the one whose section names the file, else the last. Its `Accept:`
 * is what proves the fix did not break the batch.
 * @param {{n: number, text: string}[]} batches
 * @param {string} file
 */
export function batchForFile(batches, file) {
  const base = file.split('/').pop() || file;
  const hit = batches.find((b) => b.text.includes(file)) || batches.find((b) => b.text.includes(base));
  return (hit || batches[batches.length - 1])?.n ?? 1;
}

const where = (f) => `${f.file}:${f.line}`;

/**
 * Checkpoint 2: what the main thread shows and asks. Only the change's findings are offered; the
 * preexisting ones are listed as information.
 * `extra` is one more question for the same call (the documenter's, 0.4.3), with what to do after.
 * @param {{id: string, planHash: string, findings: Classified[], summary: {files: number, added: number, removed: number}, batchOf: (f: Finding) => number,
 *   extra?: {question: object, then: string}|null}} o
 */
export function checkpoint2(o) {
  const mine = o.findings.filter((f) => f.cause === 'change');
  const old = o.findings.filter((f) => f.cause === 'preexisting');
  const count = (sev) => mine.filter((f) => f.severity === sev).length;
  const lines = [
    `nxy review ${o.id} of plan ${o.planHash} · ${o.summary.files} file${o.summary.files === 1 ? '' : 's'}, +${o.summary.added} −${o.summary.removed}`,
    `${mine.length} finding${mine.length === 1 ? '' : 's'} from the change${mine.length ? ` (${SEVERITIES.map((s) => `${count(s)} ${s}`).join(', ')})` : ''}, ${old.length} preexisting.`,
    '',
    ...mine.map((f) => `${f.id} ${f.severity} [${f.lens}] ${where(f)} — ${f.title}${f.fix ? `\n    fix: ${f.fix}` : ''}`),
    ...(old.length ? ['', 'Preexisting (not caused by this change; informational):', ...old.map((f) => `${f.id} ${f.severity} [${f.lens}] ${where(f)} — ${f.title}`)] : []),
    '',
  ];
  if (!mine.length) {
    if (!o.extra) {
      lines.push('Checkpoint 2: show the user this summary. Nothing from the change to fix.');
      return lines.join('\n');
    }
    lines.push(
      'Checkpoint 2: show the user this summary (nothing from the change to fix) and ask with AskUserQuestion, with exactly this input:',
      `  ${JSON.stringify({ questions: [o.extra.question] })}`,
      o.extra.then,
    );
    return lines.join('\n');
  }
  const groups = [];
  const maxGroups = o.extra ? 3 : 4;
  for (let i = 0; i < mine.length && groups.length < maxGroups; i += 4) groups.push(mine.slice(i, i + 4));
  const offered = groups.flat();
  const input = {
    questions: [...groups.map((g, i) => ({
      question: fixQuestion(o.id, i, groups.length),
      header: 'Review',
      multiSelect: true,
      options: [
        ...g.map((f) => ({ label: `${f.id} ${f.title}`.slice(0, 60), description: `${f.severity} · ${f.lens} · ${where(f)}` })),
        ...(g.length === 1 ? [{ label: 'None', description: 'leave it as is' }] : []),
      ],
    })), ...(o.extra ? [o.extra.question] : [])],
  };
  lines.push(
    'Checkpoint 2: show the user the findings above and ask with AskUserQuestion, with exactly this input:',
    `  ${JSON.stringify(input)}`,
    ...(mine.length > offered.length ? [`(only the first ${offered.length} are offered; the rest stay in \`review show ${o.id}\`)`] : []),
    'For each finding the user picks, dispatch nxy:implementer with its line below (its batch\'s Accept is verified again):',
    ...offered.map((f) => `  ${f.id}: "Batch ${o.batchOf(f)} — fix ${f.id} of review ${o.id}: ${f.title} at ${where(f)}.${f.fix ? ` ${f.fix}` : ''}"`),
    'One round only: no second review. What is left after it is the user\'s call.',
    ...(o.extra ? [o.extra.then] : []),
  );
  return lines.join('\n');
}

/**
 * The packet the reviewer reads: plan, lenses, conventions, and the diff. The diff is cut past
 * `maxDiffLines`; the reviewer reads the rest of those files itself.
 * @param {{planHash: string, plan: string, conventions: {title: string, body: string}[], lenses: {lens: Lens, files: string[]}[],
 *   files: {path: string, status: string, added: number, removed: number, diff: string}[], recordCmd: string, maxDiffLines?: number}} o
 */
export function reviewPacket(o) {
  const max = o.maxDiffLines ?? 3000;
  const out = [
    `# nxy review packet · plan ${o.planHash}`,
    '',
    '## The plan the user approved',
    o.plan.trim(),
    '',
    '## Lenses (what to look for)',
    ...o.lenses.flatMap(({ lens, files }) => [`### ${lens.title} (lens: ${lens.name})${files.length ? ` — ${files.join(', ')}` : ''}`, lens.body, '']),
    '## Repo conventions (the user\'s decisions: a violation is a finding)',
    ...(o.conventions.length ? o.conventions.map((c) => `- ${c.title}${c.body && c.body !== c.title ? `: ${c.body.split('\n')[0]}` : ''}`) : ['- none recorded']),
    '',
    '## Changed files',
    ...o.files.map((f) => `- ${f.path} (${f.status}, +${f.added} −${f.removed})`),
    '',
    '## Diff (against each file as it was before the plan\'s first edit)',
  ];
  let used = 0;
  const cut = [];
  for (const f of o.files) {
    if (!f.diff) continue;
    const lines = f.diff.split('\n');
    if (used + lines.length > max) {
      cut.push(f.path);
      continue;
    }
    used += lines.length;
    out.push(f.diff, '');
  }
  if (cut.length) out.push(`(diff cut at ${max} lines; read these files yourself around their changes: ${cut.join(', ')})`, '');
  out.push(
    '## Your answer',
    'A JSON array, one object per finding, [] when there is none:',
    '  {"lens": "<lens name>", "severity": "high|medium|low", "file": "<path as listed above>", "line": <line in the file as it is now>, "title": "<a few words>", "evidence": "<what you saw>", "fix": "<the change, one line>"}',
    `Record it — nxy decides which findings are the change's — with:\n${o.recordCmd} <<'EOF'\n[ ... ]\nEOF`,
  );
  return out.join('\n');
}
