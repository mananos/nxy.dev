// @ts-check
/**
 * Shared by the hook-level flow tests (not a test file): a repo on a branch with an approved plan, and
 * helpers to run the hooks as Claude Code would.
 */
import { execFileSync, spawnSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { APPROVE, approvalQuestion, planHash } from '../core/plan.mjs';
import { CONTEXT_CLOSE, CONTEXT_OPEN } from '../core/memory/handoff.mjs';
import { readVerify } from '../hosts/claude-code/verify-state.mjs';

export const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
export const HOOKS = join(ROOT, 'hosts', 'claude-code', 'hooks');
export const MEM = join(ROOT, 'hosts', 'claude-code', 'entries', 'mem.mjs');

/**
 * @param {string} PLAN the approved plan
 */
export function sandbox(PLAN) {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-verify-'));
  const repo = join(dir, 'repo');
  mkdirSync(join(repo, '.git'), { recursive: true });
  writeFileSync(join(repo, '.git', 'HEAD'), 'ref: refs/heads/feature/x\n');
  writeFileSync(join(repo, '.git', 'config'), '[remote "origin"]\n\turl = git@github.com:x/y.git\n');
  const env = { ...process.env, NXY_HOME: join(dir, 'home'), CLAUDE_PROJECT_DIR: repo };
  const hash = planHash(PLAN);
  spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', MEM, 'handoff', 'plan', '--cwd', repo], { input: PLAN, encoding: 'utf8', env });

  /** The main transcript: cheap context, the plan approved, and any other answers. */
  const main = (answers = {}) => {
    /** @type {object[]} */
    const lines = [{ type: 'assistant', message: { usage: { cache_read_input_tokens: 20_000 } } }];
    Object.entries({ [approvalQuestion(hash)]: APPROVE, ...answers }).forEach(([q, a], i) => {
      lines.push({ type: 'assistant', message: { content: [{ type: 'tool_use', id: `q${i}`, name: 'AskUserQuestion', input: { questions: [{ question: q }] } }] } });
      lines.push({ type: 'user', message: { content: [{ type: 'tool_result', tool_use_id: `q${i}`, content: 'ok' }] }, toolUseResult: { answers: { [q]: a } } });
    });
    const p = join(dir, `main-${Math.random().toString(36).slice(2)}.jsonl`);
    writeFileSync(p, lines.map((l) => JSON.stringify(l)).join('\n') + '\n');
    return p;
  };

  /**
   * An agent's transcript: its prompt (a batch number, or the whole prompt text), then edits and runs
   * ({edit} | {run, ok, out}).
   */
  const agent = (n, steps) => {
    /** @type {object[]} */
    const lines = [{ type: 'user', isSidechain: true, message: { role: 'user', content: `${CONTEXT_OPEN}\n${PLAN}\n${CONTEXT_CLOSE}\n\n${typeof n === 'string' ? n : `Batch ${n} — do it`}` } }];
    steps.forEach((s, i) => {
      const tool = s.edit ? { name: 'Edit', input: { file_path: 'x' } } : { name: 'Bash', input: { command: s.run } };
      lines.push({ type: 'assistant', isSidechain: true, message: { content: [{ type: 'tool_use', id: `t${i}`, ...tool }] } });
      lines.push({ type: 'user', isSidechain: true, message: { content: [{ type: 'tool_result', tool_use_id: `t${i}`, is_error: s.ok === false, content: s.out || 'ok' }] } });
    });
    const p = join(dir, `agent-${Math.random().toString(36).slice(2)}.jsonl`);
    writeFileSync(p, lines.map((l) => JSON.stringify(l)).join('\n') + '\n');
    return p;
  };

  const payload = (o) => JSON.stringify({ cwd: repo, session_id: 's1', ...o });
  /** SubagentStop: exit status and what the agent is told. */
  const stop = (agentId, transcript, agentType = 'nxy:implementer') => {
    const r = spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', join(HOOKS, 'subagentstop.mjs')], {
      input: payload({ hook_event_name: 'SubagentStop', agent_id: agentId, agent_type: agentType, agent_transcript_path: transcript }), encoding: 'utf8', env,
    });
    return { status: r.status, stderr: r.stderr };
  };
  const hook = (file, o) => execFileSync(process.execPath, ['--disable-warning=ExperimentalWarning', join(HOOKS, file)], { input: payload(o), encoding: 'utf8', env });
  const dispatch = (prompt, transcript = main(), subagentType = 'nxy:implementer') => {
    const out = hook('pretooluse-agent.mjs', { tool_name: 'Agent', transcript_path: transcript, tool_input: { subagent_type: subagentType, prompt } });
    return out.trim() ? JSON.parse(out).hookSpecificOutput : null;
  };
  /** PostToolUse:Agent for an agent that returned (sync) or was launched (`response`). */
  const returnedAs = (prompt, subagentType = 'nxy:implementer', response = /** @type {any} */ (undefined)) => {
    const out = hook('posttooluse-agent.mjs', { tool_name: 'Agent', tool_input: { subagent_type: subagentType, prompt }, ...(response ? { tool_response: response } : {}) });
    return out.trim() ? JSON.parse(out).hookSpecificOutput.additionalContext : '';
  };
  const returned = (n) => returnedAs(`Batch ${n} — do it`);
  const recorded = () => readVerify(repo, 'feature/x', hash).batches;
  return { hash, dir, repo, env, main, agent, stop, hook, dispatch, returned, returnedAs, recorded };
}
