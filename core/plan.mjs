// @ts-check
/**
 * The plan and its checkpoint (0.4.0).
 *
 * The problem it exists for: correcting a flow halfway ("I don't like how it's doing this") after
 * 400 lines were written, when 20 lines of plan would have been enough to redirect it. So a
 * non-trivial task gets a plan at class/method level, and **nothing is written until the user
 * approves it**.
 *
 * Where it lives: a `## Plan` section inside the branch's handoff. One artifact for the task — the
 * implementer already receives the handoff (0.3.2), and a plan that survives `/clear` is the point.
 *
 * Who approves: the user, and nxy checks it — not the model. Each plan has a hash; the main thread
 * asks with AskUserQuestion "Approve nxy plan <hash>?" and the hook finds the user's answer in the
 * transcript (gentle-ai's lesson: authority comes from what the runtime recorded, never from what
 * the model says). A changed plan has a new hash and needs a new approval.
 *
 * When: only when a plan exists. No plan, no checkpoint — a one-line fix stays a one-line fix.
 *
 * Questions before the plan (0.4.0b): the checkpoint corrects a plan *after* it is drafted; a
 * choice only the user can make (new endpoint or extend? Lombok or record?) would otherwise be
 * made silently and caught — maybe — by reading. So a draft may carry `### Questions`, and while it
 * does it cannot be approved: the user answers, the planner turns the answers into `Decisions:`,
 * and only that final plan reaches Approve/Change. Each decision is then offered as a repo
 * convention, so the next plan does not ask it again.
 *
 * Pure: text in, text out.
 */
import { createHash } from 'node:crypto';

export const APPROVE = 'Approve';
export const CHANGE = 'Change';

export const PLAN_TEMPLATE = [
  '## Plan',
  'Goal: <one line: what is true when this is done>',
  '### Batch 1 — <repo or area>: <what this batch does>',
  '- `path/to/File.java:120` — <the change, at class/method level>',
  '- `path/to/new-file.ts` (new) — <what it holds>',
  'Accept: `<the smallest command that proves this batch: one test class or file>`   (or: manual — <what to look at>)',
  '### Batch 2 — ...',
  'Accept: `...`',
  'Risks: <optional: what could go wrong, what to watch in review>',
].join('\n');

/** The optional section of a draft: decisions only the user can make, recommended option first. */
export const QUESTIONS_TEMPLATE = [
  '### Questions',
  '- Q: <a choice that changes the plan and reading the code cannot settle>',
  '  - <option, a few words> (recommended) — <what it means for the plan>',
  '  - <option> — <what it means>',
].join('\n');

const PLAN_RE = /^##\s*Plan\s*$[\s\S]*?(?=^##\s|(?![\s\S]))/im;
const QUESTIONS_RE = /^###\s*Questions\s*$([\s\S]*?)(?=^#{2,3}\s|(?![\s\S]))/im;
/** `label — description`: an em/en dash, or a hyphen with spaces around it. */
const DASH_RE = /\s+[—–-]\s+/;

const norm = (text) => String(text || '').replace(/\r\n/g, '\n');

/**
 * @typedef {{label: string, description: string, recommended: boolean}} Option
 * @typedef {{question: string, options: Option[]}} Question
 */

/**
 * The open questions of a plan: `- Q: <question>` followed by its options as bullets. An option
 * marked `(recommended)` has the mark stripped from its label.
 * @param {string} plan
 * @returns {Question[]}
 */
export function parseQuestions(plan) {
  const m = QUESTIONS_RE.exec(norm(plan));
  if (!m) return [];
  /** @type {Question[]} */
  const out = [];
  for (const raw of m[1].split('\n')) {
    const q = /^\s*[-*]\s+Q:\s*(.+?)\s*$/i.exec(raw);
    if (q) {
      out.push({ question: q[1], options: [] });
      continue;
    }
    const o = /^\s*[-*]\s+(.+?)\s*$/.exec(raw);
    if (!o || !out.length) continue;
    const recommended = /\(recommended\)/i.test(o[1]);
    const [label, ...rest] = o[1].replace(/\s*\(recommended\)\s*/gi, ' ').trim().split(DASH_RE);
    out[out.length - 1].options.push({ label: label.trim(), description: rest.join(' — ').trim(), recommended });
  }
  return out;
}

/**
 * @typedef {{kind: 'command', command: string} | {kind: 'manual', note: string} | {kind: 'none'}} Accept
 */

/**
 * A batch's acceptance criterion (0.4.1): the command in backticks — what the implementer runs and
 * nxy checks — or an explicit `manual — <what to look at>` for what no command can prove.
 * @param {string} text what follows `Accept:`
 * @returns {Accept}
 */
export function parseAccept(text) {
  const t = String(text || '').trim();
  if (/^manual\b/i.test(t)) return { kind: 'manual', note: t.replace(/^manual\s*[—–:-]?\s*/i, '') };
  const c = /`([^`]+)`/.exec(t);
  return c && c[1].trim() ? { kind: 'command', command: c[1].trim() } : { kind: 'none' };
}

/** The `### ...` sections that are batches (everything but `### Questions`). */
const batchSections = (text) => norm(text).split(/^###\s+/m).slice(1).filter((s) => !/^Questions\s*$/i.test(s.split('\n')[0]));

/**
 * The plan's batches, numbered as written ("### Batch 2 — ...") or by position.
 *
 * `depends` (0.4.4): the batches this one builds on. `Depends: 1` (or `1, 3`, or `none`) says so;
 * without the line, a batch builds on the one before it — the plan's order is the planner's intent.
 * Batches that depend only on a contract batch can run in parallel, one implementer per repo.
 * @param {string} plan
 * @returns {{n: number, title: string, accept: Accept, text: string, depends: number[]}[]}
 */
export function parseBatches(plan) {
  const sections = batchSections(plan).map((s, i) => {
    const title = s.split('\n')[0].trim();
    const num = /^Batch\s+(\d+)/i.exec(title);
    return { s, title, n: num ? Number(num[1]) : i + 1 };
  });
  return sections.map(({ s, title, n }, i) => {
    const acc = /^Accept:(.*)$/im.exec(s);
    const dep = /^Depends:(.*)$/im.exec(s);
    const depends = dep
      ? (/^\s*(none|-)?\s*$/i.test(dep[1]) ? [] : [...dep[1].matchAll(/\d+/g)].map((m) => Number(m[0])))
      : (i > 0 ? [sections[i - 1].n] : []);
    return { n, title, accept: parseAccept(acc ? acc[1] : ''), text: s, depends };
  });
}

/**
 * The plan's `Decisions:` list — the user's answers, written by the planner as rules.
 * @param {string} plan
 * @returns {{rule: string, why: string}[]}
 */
export function parseDecisions(plan) {
  const lines = norm(plan).split('\n');
  const start = lines.findIndex((l) => /^Decisions:\s*$/i.test(l.trim()));
  if (start < 0) return [];
  const out = [];
  for (const l of lines.slice(start + 1)) {
    const m = /^\s*[-*]\s+(.+?)\s*$/.exec(l);
    if (!m) break;
    const [rule, ...why] = m[1].split(DASH_RE);
    out.push({ rule: rule.trim(), why: why.join(' — ').trim() });
  }
  return out;
}

/** The `## Plan` section of a handoff body (heading included), or null. */
export function extractPlan(body) {
  const m = PLAN_RE.exec(String(body || '').replace(/\r\n/g, '\n'));
  return m ? m[0].trim() : null;
}

/**
 * Stable hash of a plan: whitespace-insensitive, so re-saving the same plan with different line
 * endings or indentation does not demand a new approval.
 * @param {string} plan
 */
export function planHash(plan) {
  const norm = String(plan || '').replace(/\r\n/g, '\n').split('\n').map((l) => l.trim()).filter(Boolean).join('\n');
  return createHash('sha1').update(norm).digest('hex').slice(0, 8);
}

/**
 * The shape a plan must have to be worth a checkpoint: at least one batch, every batch with an
 * acceptance criterion that is a command (or explicitly manual), and at least one concrete path. A plan without those is prose, and prose
 * is exactly what cannot be approved or verified.
 *
 * Questions are shaped for AskUserQuestion as they are: 2–4 options, exactly one recommended and
 * listed first (the user who agrees with the planner answers with one click).
 * @param {string} plan
 * @returns {{ok: boolean, errors: string[], batches: number, questions: number}}
 */
export function validatePlan(plan) {
  const text = norm(plan);
  const errors = [];
  if (!/^##\s*Plan\s*$/im.test(text)) errors.push('missing the "## Plan" heading');
  const batches = batchSections(text);
  if (!batches.length) errors.push('no batch: add at least one "### Batch 1 — ..." section');
  batches.forEach((b, i) => {
    const acc = /^Accept:(.*)$/im.exec(b);
    if (!acc || !acc[1].trim()) errors.push(`batch ${i + 1} has no "Accept:" line`);
    else if (parseAccept(acc[1]).kind === 'none') {
      errors.push(`batch ${i + 1}: "Accept:" needs the command that proves it, in backticks (\`mvn -q test -Dtest=FooTest\`), or "manual — <what to look at>"`);
    }
  });
  if (!/`?[\w@.-]+(?:\/[\w@.-]+)+\.[A-Za-z0-9]{1,8}/.test(text) && !/`[\w@.-]+\.[A-Za-z0-9]{1,8}(?::\d+)?`/.test(text)) {
    errors.push('no file path: a plan names the files it touches (`path/File.ext:line`)');
  }
  // A batch can only build on batches written before it: no cycles, no dangling numbers.
  const parsed = parseBatches(text);
  parsed.forEach((b, i) => {
    const earlier = new Set(parsed.slice(0, i).map((x) => x.n));
    const bad = b.depends.filter((d) => !earlier.has(d));
    if (bad.length) errors.push(`batch ${b.n}: "Depends:" can only name batches written before it (${bad.join(', ')} is not)`);
  });
  const questions = parseQuestions(text);
  questions.forEach((q, i) => {
    const n = q.options.length;
    if (n < 2 || n > 4) errors.push(`question ${i + 1} has ${n} option${n === 1 ? '' : 's'}: it needs 2 to 4`);
    const rec = q.options.filter((o) => o.recommended).length;
    if (rec !== 1 || (n && !q.options[0].recommended)) errors.push(`question ${i + 1}: mark exactly one option "(recommended)" and list it first`);
  });
  return { ok: errors.length === 0, errors, batches: batches.length, questions: questions.length };
}

/**
 * Puts a plan into a handoff body, replacing any previous one. The plan goes last: the handoff's
 * own sections (Done, Next, Files, Decisions) stay where a reader expects them.
 * @param {string} body
 * @param {string} plan
 */
export function withPlan(body, plan) {
  const text = String(body || '').replace(/\r\n/g, '\n');
  const without = text.replace(PLAN_RE, '').replace(/\n{3,}/g, '\n\n').trim();
  return `${without}\n\n${String(plan).trim()}`.trim();
}

/** The exact question the hook looks for. */
export function approvalQuestion(hash) {
  return `Approve nxy plan ${hash}?`;
}

/** AskUserQuestion takes at most 4 questions per call. */
const PER_CALL = 4;

/** @template T @param {T[]} list @param {number} size @returns {T[][]} */
const chunks = (list, size) => Array.from({ length: Math.ceil(list.length / size) }, (_, i) => list.slice(i * size, i * size + size));

/**
 * The AskUserQuestion inputs for a draft's questions, ready to copy: one object per call.
 * @param {Question[]} questions
 */
export function questionsPayload(questions) {
  return chunks(questions, PER_CALL).map((group, g) => ({
    questions: group.map((q, i) => ({
      question: q.question,
      header: `Plan Q${g * PER_CALL + i + 1}`,
      multiSelect: false,
      options: q.options.map((o) => ({ label: o.recommended ? `${o.label} (Recommended)` : o.label, description: o.description })),
    })),
  }));
}

/**
 * What the main thread does with a draft that still has questions: ask them, then send the
 * answers back to the planner. No approval question — a plan with open questions cannot be approved.
 * @param {string} hash
 * @param {Question[]} questions
 */
export function questionsInstruction(hash, questions) {
  const n = questions.length;
  return [
    `Questions first: plan ${hash} is a draft with ${n} decision${n === 1 ? '' : 's'} only the user can make. Do not show the draft or ask for approval yet: it changes with the answers, and a plan with open questions cannot be approved.`,
    `Ask them with AskUserQuestion, with exactly this input (one call per line):`,
    ...questionsPayload(questions).map((p) => `  ${JSON.stringify(p)}`),
    `Then dispatch the nxy:planner subagent again with: "Finish nxy plan ${hash} with the user's answers:" and one line per question, "<question> → <answer>". It starts from the draft saved in the handoff and does not explore again.`,
  ].join('\n');
}

/**
 * What the main thread has to do to pass the checkpoint, spelled out so it can comply in one call.
 * When the plan carries decisions the user made (its answered questions) that are not conventions
 * yet, the same call offers to keep them as repo conventions.
 * @param {string} hash
 * @param {{offers?: {rule: string, why: string}[], saveCmd?: string}} [o]
 */
export function checkpointInstruction(hash, o = {}) {
  const offers = (o.offers || []).slice(0, (PER_CALL - 1) * PER_CALL);
  const groups = chunks(offers, PER_CALL);
  const input = {
    questions: [
      {
        question: approvalQuestion(hash),
        header: 'Plan',
        multiSelect: false,
        options: [
          { label: APPROVE, description: 'write the code as planned' },
          { label: CHANGE, description: 'say what to change' },
        ],
      },
      ...groups.map((g, i) => ({
        question: `Save as repo conventions${groups.length > 1 ? ` (${i + 1}/${groups.length})` : ''}? The next plan will follow them instead of asking.`,
        header: 'Convention',
        multiSelect: true,
        options: g.map((d) => ({ label: d.rule, description: d.why || d.rule })),
      })),
    ],
  };
  return [
    `Checkpoint: show the user the plan (verbatim, it is short) and ask with AskUserQuestion, with exactly this input:`,
    `  ${JSON.stringify(input)}`,
    ...(offers.length ? [`For each convention the user picks: ${o.saveCmd || 'mem save'} "<rule>" --type convention --body "<rule>: <the question it settles>"`] : []),
    `Nothing is written — no Edit, no implementer — until the user picks "${APPROVE}". On "${CHANGE}", revise the plan (new hash) and ask again.`,
    `After "${APPROVE}": one nxy:implementer per batch, each prompt starting "Batch N — "; batches whose dependencies passed can go in parallel. nxy verifies each against its Accept command.`,
  ].join('\n');
}

/**
 * Whether write work must wait for the plan: its open questions first, then its approval. An
 * approval of a plan that still has questions does not count — whatever the transcript says.
 * @param {{isSubagent: boolean, planHash: string|null, questions?: number, approved: boolean}} input
 * @returns {{block: boolean, reason: string}}
 */
export function decideCheckpoint(input) {
  if (input.isSubagent) return { block: false, reason: 'subagent' };
  if (!input.planHash) return { block: false, reason: 'no-plan' };
  if (input.questions) return { block: true, reason: 'plan-questions' };
  if (input.approved) return { block: false, reason: 'approved' };
  return { block: true, reason: 'plan-pending' };
}

/**
 * With an approved plan, the main thread does not write: each batch goes to an implementer, which is
 * what gets verified (0.4.4). The escape is the gate's (`/nxy:gate once`).
 * @param {string} hash @param {number[]} batches @param {string} onceCmd
 */
export function planActiveDenyMessage(hash, batches, onceCmd) {
  return [
    `nxy: plan ${hash} is approved, so its work goes through implementers — one per batch, each verified against its Accept command.`,
    `Dispatch nxy:implementer with "Batch N — <the change>" (batches: ${batches.join(', ')}).`,
    `If this edit is not part of the plan and the user wants it done here, the user can run \`${onceCmd}\`; when the task is over, \`mem handoff done\` closes the plan.`,
  ].join('\n');
}

/** @param {string} onceCmd */
export function planFirstDenyMessage(onceCmd) {
  return [
    'nxy: this repo plans every change that touches more than one file (`flow.plan: "always"` in .nxy/config.json), and there is no plan yet.',
    'Dispatch the nxy:planner subagent with the task first; the user approves the plan, then implementers carry it out.',
    `For a one-off exception, the user can run \`${onceCmd}\`.`,
  ].join('\n');
}

/** @param {string} hash */
export function checkpointDenyMessage(hash) {
  return `nxy: the plan (${hash}) has not been approved by the user yet.\n\n${checkpointInstruction(hash)}`;
}

/**
 * @param {string} hash
 * @param {number} questions
 * @param {string} nextCmd the command that prints the questions, ready to ask
 */
export function questionsDenyMessage(hash, questions, nextCmd) {
  return `nxy: the plan (${hash}) still has ${questions} open question${questions === 1 ? '' : 's'} for the user; it cannot be approved or carried out yet.\n\nRun \`${nextCmd}\`: it prints them ready for AskUserQuestion, and what to do with the answers.`;
}
