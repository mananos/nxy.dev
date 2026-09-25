#!/usr/bin/env node
// @ts-check
/**
 * `/nxy:mem` — the memory that survives `/clear`.
 *
 * Deliberately a CLI and not an MCP server: an MCP surface adds its tool descriptions to every
 * turn whether or not memory is used that session. Here the cost is paid only when called.
 *
 * Usage:
 *   mem.mjs save "<title>" --body "<text>" [--scope global|project|area] [--type <t>]
 *                          [--area a/b] [--keywords "x y"] [--private] [--supersedes <id>]
 *   mem.mjs search "<query>" [--type <t>] [--limit N] [--all-areas]
 *   mem.mjs get <id> [--for link|search]  (also shows its edges; --for tags who fetched it)
 *   mem.mjs link <id> <other> --kind related|supersedes|conflicts_with [--by librarian]
 *   mem.mjs unlink <id> <other> --kind <k>
 *   mem.mjs index                     (one line per memory: what the librarian reads)
 *   mem.mjs list [--scope <s>] [--type <t>] [--all-areas] [--limit N]
 *   mem.mjs delete <id>
 *   mem.mjs handoff [show | save [--body "<text>" | --file <path> | <stdin>] [--share] | plan [<same>] | next | done]
 *   mem.mjs export | import
 *   mem.mjs status
 */
import { readFileSync } from 'node:fs';
import { parseArgs } from '../../../core/format.mjs';
import { gitBranch, toNativePath } from '../../../core/paths.mjs';
import {
  TYPES, countsByScope, deleteMemory, getMemory, listMemories, memoryDbPath, openStore,
  saveMemory, searchMemories,
} from '../../../core/memory/store.mjs';
import { areaFor, projectKey, repoRoot } from '../../../core/memory/scope.mjs';
import { VERDICTS, addEdge, bumpCoUse, deriveEdges, edgesOf, removeEdge } from '../../../core/memory/graph.mjs';
import { librarianRoute, linkCandidates, linkRequest } from '../../../core/memory/librarian.mjs';
import { loadConfig } from '../../../core/config.mjs';
import { noteFetch, recallSummary } from '../recall-state.mjs';
import {
  PLAN_TEMPLATE, QUESTIONS_TEMPLATE, checkpointInstruction, extractPlan, parseDecisions, parseQuestions, planHash,
  questionsInstruction, validatePlan, withPlan,
} from '../../../core/plan.mjs';
import { writeMarker } from '../plan-approval.mjs';
import { memCommand } from '../handoff.mjs';
import { clearBaseline } from '../baseline.mjs';
import { markHandoffSaved } from '../stop-state.mjs';
import { progressFor } from '../verify-state.mjs';
import { exportProject, importProject, memoryDir } from '../../../core/memory/exchange.mjs';
import {
  TEMPLATE, archiveHandoff, liveHandoff, saveHandoff, validateHandoff,
} from '../../../core/memory/handoff.mjs';

const { opts, positional } = parseArgs(process.argv.slice(2));
const cwd = toNativePath(typeof opts.cwd === 'string' ? opts.cwd : process.cwd());
const action = (positional[0] || 'status').toLowerCase();
const rest = positional.slice(1);

const { key: project, from, warning } = projectKey(cwd);
const db = openStore();

/** Areas a query should see: whatever the user named, else the whole project. */
function requestedAreas() {
  if (opts.allAreas || opts['all-areas']) return [];
  if (typeof opts.area === 'string') return [opts.area];
  return [];
}

/**
 * What the main thread does next with a saved plan: its open questions, or the checkpoint — which
 * offers the plan's decisions as conventions, minus those the project already has.
 * @param {string} plan
 */
function nextStep(plan) {
  const hash = planHash(plan);
  const questions = parseQuestions(plan);
  if (questions.length) return questionsInstruction(hash, questions);
  const known = new Set(listMemories(db, { project, allAreas: true, type: 'convention', limit: 1000 }).map((m) => m.title.trim().toLowerCase()));
  const offers = parseDecisions(plan).filter((d) => d.rule && !known.has(d.rule.toLowerCase()));
  return checkpointInstruction(hash, { offers, saveCmd: memCommand('save') });
}

const fmtDate = (ms) => new Date(ms).toISOString().slice(0, 10);
const line = (m) => `${m.id}\n  [${m.scope}${m.area ? `:${m.area}` : ''}] ${m.type} · ${fmtDate(m.updated)}\n  ${m.title}`;

switch (action) {
  case 'save': {
    const title = rest.join(' ').trim();
    const body = typeof opts.body === 'string' ? opts.body : '';
    if (!title || !body) {
      console.log('Usage: mem.mjs save "<title>" --body "<text>" [--scope global|project|area] [--area a/b] [--type decision|bug|convention|preference|handoff] [--keywords "x y"] [--private] [--supersedes <id>]');
      break;
    }
    let scope = typeof opts.scope === 'string' ? opts.scope : 'project';
    let area = typeof opts.area === 'string' ? opts.area : null;
    // `--area x` without `--scope` is unambiguous: the user meant an area memory.
    if (area && scope === 'project') scope = 'area';
    if (scope === 'area' && !area) {
      console.log('scope=area needs --area <path> (e.g. --area api/auth)');
      break;
    }
    const res = saveMemory(db, {
      scope: /** @type {any} */ (scope),
      project: scope === 'global' ? null : project,
      area: scope === 'area' ? area : null,
      type: typeof opts.type === 'string' ? opts.type : 'decision',
      title,
      body,
      keywords: typeof opts.keywords === 'string' ? opts.keywords : '',
      private: Boolean(opts.private),
    });
    console.log(`${res.created ? 'saved' : 'updated'}: ${res.id}`);
    if (scope !== 'global' && warning) console.log(`note: ${warning}`);
    const saved = getMemory(db, res.id);
    if (!saved) break;
    const derived = deriveEdges(db, saved, repoRoot(cwd));
    if (derived.files.length) console.log(`files: ${derived.files.join(', ')}`);
    if (typeof opts.supersedes === 'string') {
      console.log(getMemory(db, opts.supersedes) && addEdge(db, res.id, opts.supersedes, 'supersedes', { by: 'user' })
        ? `supersedes: ${opts.supersedes} (it stays searchable, it is no longer pointed at)`
        : `no memory with id ${opts.supersedes}; nothing superseded`);
    }
    // The CLI dictates the next step (roadmap 8.3): the model does not have to remember it.
    const route = librarianRoute(loadConfig(cwd));
    const candidates = route.kind === 'off' ? [] : linkCandidates(db, saved, { project });
    if (candidates.length && route.kind === 'subagent') {
      console.log(`\nnext: dispatch the nxy:librarian subagent with: "${linkRequest(res.id, candidates.map((c) => c.id))}"`);
    } else if (candidates.length && route.kind === 'unsupported') {
      console.log(`\nnote: librarian provider "${route.provider}" is not supported yet; ${candidates.length} candidates left unrelated`);
    }
    break;
  }

  case 'search': {
    const query = rest.join(' ').trim();
    if (!query) {
      console.log('Usage: mem.mjs search "<query>"');
      break;
    }
    const rows = searchMemories(db, query, {
      project,
      areas: requestedAreas(),
      type: typeof opts.type === 'string' ? opts.type : null,
      limit: Number(opts.limit) || 10,
    });
    if (!rows.length) {
      console.log(`no memory matches "${query}"`);
      console.log('(search is lexical: try the words the note itself would use, or check `mem list`)');
      break;
    }
    for (const m of rows) console.log(line(m) + '\n');
    break;
  }

  case 'get': {
    const mem = rest[0] ? getMemory(db, rest[0]) : null;
    if (!mem) {
      console.log(rest[0] ? `no memory with id ${rest[0]}` : 'Usage: mem.mjs get <id>');
      break;
    }
    console.log(`${mem.title}\n[${mem.scope}${mem.area ? `:${mem.area}` : ''}] ${mem.type} · updated ${fmtDate(mem.updated)}${mem.private ? ' · private' : ''}`);
    if (mem.keywords) console.log(`keywords: ${mem.keywords}`);
    for (const e of edgesOf(db, mem.id)) {
      if (e.kind === 'co_use') continue;
      if (e.kind === 'file') console.log(`file: ${e.dst.slice(5)}`);
      else if (e.src === mem.id) console.log(`${e.kind.replace('_', ' ')}: ${e.dst}`);
      else console.log(`${e.kind === 'supersedes' ? 'superseded by' : e.kind.replace('_', ' ')}: ${e.src}`);
    }
    console.log(`\n${mem.body}`);
    // A fetch is the signal recall is judged by, and two fetches close together make a co-use edge.
    // `--for link` (the librarian relating a new memory) is bookkeeping: no miss, no co-use.
    const purpose = opts.for === 'link' || opts.for === 'search' ? opts.for : null;
    for (const other of noteFetch(cwd, mem.id, Date.now(), purpose)) bumpCoUse(db, mem.id, other);
    break;
  }

  case 'list': {
    const rows = listMemories(db, {
      project,
      areas: requestedAreas(),
      allAreas: Boolean(opts.allAreas || opts['all-areas']),
      scope: typeof opts.scope === 'string' ? /** @type {any} */ (opts.scope) : null,
      type: typeof opts.type === 'string' ? opts.type : null,
      limit: Number(opts.limit) || 50,
    });
    if (!rows.length) {
      console.log(typeof opts.type === 'string' ? `no ${opts.type} memories yet for this project` : 'no memories yet for this project');
      break;
    }
    for (const m of rows) console.log(line(m) + '\n');
    break;
  }

  case 'link':
  case 'unlink': {
    const [a, b] = rest;
    const kind = typeof opts.kind === 'string' ? opts.kind : '';
    if (!a || !b || !VERDICTS.includes(kind)) {
      console.log(`Usage: mem.mjs ${action} <id> <other> --kind ${VERDICTS.join('|')}`);
      break;
    }
    if (action === 'unlink') {
      console.log(removeEdge(db, a, b, kind) ? `removed: ${a} ${kind} ${b}` : 'no such edge');
      break;
    }
    const missing = [a, b].find((id) => !getMemory(db, id));
    if (missing) {
      console.log(`no memory with id ${missing}`);
      break;
    }
    addEdge(db, a, b, kind, { by: typeof opts.by === 'string' ? opts.by : 'user' });
    console.log(`linked: ${a} ${kind} ${b}`);
    break;
  }

  case 'index': {
    const rows = listMemories(db, { project, allAreas: true, limit: Number(opts.limit) || 500 }).filter((m) => m.type !== 'handoff');
    if (!rows.length) {
      console.log('no memories yet for this project');
      break;
    }
    for (const m of rows) console.log(`${m.id} · ${m.type}${m.area ? ` · ${m.area}` : ''} · ${m.title}${m.keywords ? ` [${m.keywords}]` : ''}`);
    break;
  }

  case 'delete': {
    if (!rest[0]) {
      console.log('Usage: mem.mjs delete <id>');
      break;
    }
    console.log(deleteMemory(db, rest[0]) ? `deleted ${rest[0]}` : `no memory with id ${rest[0]}`);
    break;
  }

  case 'handoff': {
    const sub = (rest[0] || 'show').toLowerCase();
    const branch = gitBranch(cwd);
    const label = branch || '(no branch)';

    // Body from --body, --file, or stdin (a heredoc: the only multi-line form that survives
    // quoting in Git Bash, WSL and PowerShell alike).
    const readBody = () => {
      let body = typeof opts.body === 'string' ? opts.body : '';
      if (!body && typeof opts.file === 'string') body = readFileSync(opts.file, 'utf8');
      if (!body && !process.stdin.isTTY) body = readFileSync(0, 'utf8');
      return body;
    };

    if (sub === 'save') {
      let body = readBody();
      const check = validateHandoff(body);
      if (!check.ok) {
        console.log(`handoff not saved: ${check.errors.join('; ')}\n\nExpected shape (~20 lines):\n${TEMPLATE}`);
        process.exitCode = 1;
        break;
      }
      // Saving the handoff must not be a way around the checkpoint: a body without a plan keeps
      // the one already there; a body with a plan replaces it (and needs its own approval).
      const livePlan = extractPlan(liveHandoff(db, project, branch)?.body || '');
      if (!extractPlan(body) && livePlan) body = withPlan(body, livePlan);
      const res = saveHandoff(db, { project, branch, body, share: Boolean(opts.share) });
      markHandoffSaved(cwd, branch);
      const plan = extractPlan(body);
      writeMarker(cwd, branch, plan ? planHash(plan) : null, plan ? parseQuestions(plan).length : 0);
      console.log(`${res.created ? 'saved' : 'replaced'} handoff for \`${label}\` (${check.lines} lines, route: ${check.route})${plan ? ` · plan ${planHash(plan)} kept` : ''}${opts.share ? ' · shared: `mem export` will write it' : ''}`);
      for (const w of check.warnings) console.log(`warning: ${w}`);
      console.log('Save again after each finished task; `mem handoff done` archives it when the branch is finished.');
      break;
    }

    if (sub === 'plan') {
      const plan = readBody().replace(/\r\n/g, '\n').trim();
      const check = validatePlan(plan);
      if (!check.ok) {
        console.log(`plan not saved: ${check.errors.join('; ')}\n\nExpected shape:\n${PLAN_TEMPLATE}\n\nOptional, only for choices the user alone can make:\n${QUESTIONS_TEMPLATE}`);
        process.exitCode = 1;
        break;
      }
      const live = liveHandoff(db, project, branch);
      const base = live?.body
        || 'route: implementer\n## Done\n- nothing yet: plan drafted\n## Next\n- after the user approves the plan: run its batches in order, one implementer per batch';
      saveHandoff(db, { project, branch, body: withPlan(base, plan), share: Boolean(live && !live.private) });
      markHandoffSaved(cwd, branch);
      const hash = planHash(plan);
      writeMarker(cwd, branch, hash, check.questions);
      const open = check.questions ? `, ${check.questions} open question${check.questions === 1 ? '' : 's'}` : '';
      console.log(`plan ${hash} saved in the handoff of \`${label}\` (${check.batches} batch${check.batches === 1 ? '' : 'es'}${open})\n`);
      console.log(`Next (main thread): ${nextStep(plan)}`);
      break;
    }

    if (sub === 'next') {
      const plan = extractPlan(liveHandoff(db, project, branch)?.body || '');
      console.log(plan ? `Next (main thread): ${nextStep(plan)}` : `no plan in the handoff of \`${label}\``);
      break;
    }

    if (sub === 'done') {
      markHandoffSaved(cwd, branch, null);
      writeMarker(cwd, branch, null);
      clearBaseline(cwd, branch);
      const id = archiveHandoff(db, project, branch);
      console.log(id
        ? `archived handoff for \`${label}\` as ${id}\nStill searchable: mem search "<words>" --type handoff`
        : `no live handoff for \`${label}\``);
      break;
    }

    if (sub !== 'show') {
      console.log('Usage: mem.mjs handoff [show | save [--body "<text>" | --file <path> | <stdin>] [--share] | plan [<same inputs>] | next | done]');
      break;
    }
    const live = liveHandoff(db, project, branch);
    if (!live) {
      console.log(`no handoff for \`${label}\` yet. Save one with \`mem handoff save\`, shaped like:\n\n${TEMPLATE}`);
      break;
    }
    const shownPlan = extractPlan(live.body);
    // The plan's progress is derived from what nxy recorded (0.4.5): never stale, never typed by the model.
    const progress = shownPlan ? await progressFor(cwd, branch, shownPlan) : '';
    console.log(`handoff · ${label} · updated ${new Date(live.updated).toISOString().slice(0, 16).replace('T', ' ')}${live.private ? '' : ' · shared'}${shownPlan ? ` · plan ${planHash(shownPlan)}` : ''}\n\n${live.body}${progress ? `\n\n${progress}` : ''}`);
    break;
  }

  case 'export': {
    const res = exportProject(db, cwd, project);
    console.log(`exported ${res.total} memories to ${res.dir.replace(/\\/g, '/')}`);
    console.log(`  ${res.written} written, ${res.removed} removed, ${res.total - res.written} unchanged`);
    console.log('Commit that directory to share these with the team. Global and private memories never leave.');
    break;
  }

  case 'import': {
    const res = importProject(db, cwd, project);
    console.log(`imported from ${res.dir.replace(/\\/g, '/')}`);
    console.log(`  ${res.added} new, ${res.updated} updated, ${res.skipped} already current`);
    if (res.failed.length) console.log(`  could not parse: ${res.failed.join(', ')}`);
    break;
  }

  default: {
    if (action !== 'status') console.log(`unknown action "${action}". Use: save | search | get | list | index | link | unlink | delete | handoff | export | import | status\n`);
    const counts = countsByScope(db, project);
    console.log(`store:      ${memoryDbPath().replace(/\\/g, '/')}`);
    console.log(`project:    ${project}  (from ${from})`);
    if (warning) console.log(`            note: ${warning}`);
    console.log(`memories:   ${counts.global} global · ${counts.project} project · ${counts.area} area`);
    console.log(`shared dir: ${memoryDir(cwd).replace(/\\/g, '/')}  (commit it to share; \`mem export\` writes it)`);
    console.log(`types:      ${TYPES.join(' | ')}`);
    const rs = recallSummary(cwd);
    if (rs.pointers || rs.misses) {
      const pct = rs.pointers ? ` (${Math.round((100 * rs.followed) / rs.pointers)}%)` : '';
      const bySearch = rs.missesBySearch ? `, ${rs.missesBySearch} found by the librarian` : '';
      console.log(`recall 30d: ${rs.pointers} pointers (~${rs.tokens} tokens), ${rs.followed} loaded${pct} · ${rs.misses} loaded without a pointer (misses${bySearch})`);
    }
    const here = areaFor(cwd, 'x');
    if (here) console.log(`area here:  ${here}`);
  }
}
