// @ts-check
/**
 * Builds a synthetic Claude Code project folder in a temp dir:
 *   <root>/<slug>/<session>.jsonl                       main transcript
 *   <root>/<slug>/<session>/subagents/agent-<id>.jsonl  one Explore subagent (+ .meta.json)
 * Numbers are chosen so tests can assert exact totals.
 */
import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

export const FIXTURE = {
  cwd: '/home/dev/code/my.app',
  slug: '-home-dev-code-my-app',
  sessionId: 'sess-0001',
  agentId: 'a1b2c3d4e5f60718',
};

const T0 = Date.parse('2026-09-01T10:00:00.000Z');
const iso = (offsetMs) => new Date(T0 + offsetMs).toISOString();

/**
 * @param {{uuid: string, requestId: string, ts: number, model: string, usage: any, content: any[], effort?: string|null, sidechain?: boolean, agentId?: string}} p
 */
function assistant({ uuid, requestId, ts, model, usage, content, effort = 'high', sidechain = false, agentId }) {
  return {
    parentUuid: null,
    isSidechain: sidechain,
    ...(agentId ? { agentId } : {}),
    type: 'assistant',
    uuid,
    requestId,
    timestamp: iso(ts),
    effort,
    cwd: FIXTURE.cwd,
    version: '2.1.274',
    gitBranch: 'main',
    message: { id: `msg_${requestId}`, model, role: 'assistant', usage, content },
  };
}

/**
 * @param {{uuid: string, ts: number, content: any, toolUseResult?: any, sidechain?: boolean}} p
 */
function user({ uuid, ts, content, toolUseResult, sidechain = false }) {
  return {
    parentUuid: null,
    isSidechain: sidechain,
    type: 'user',
    uuid,
    timestamp: iso(ts),
    cwd: FIXTURE.cwd,
    version: '2.1.274',
    gitBranch: 'main',
    message: { role: 'user', content },
    ...(toolUseResult ? { toolUseResult } : {}),
  };
}

const U = (input, w5, w1, read, output, thinking = 0) => ({
  input_tokens: input,
  cache_creation_input_tokens: w5 + w1,
  cache_read_input_tokens: read,
  output_tokens: output,
  output_tokens_details: { thinking_tokens: thinking },
  cache_creation: { ephemeral_5m_input_tokens: w5, ephemeral_1h_input_tokens: w1 },
  service_tier: 'standard',
  speed: 'standard',
});

/** @returns {{root: string, mainPath: string}} */
export function writeFixture(root) {
  const projDir = join(root, FIXTURE.slug);
  const agentsDir = join(projDir, FIXTURE.sessionId, 'subagents');
  mkdirSync(agentsDir, { recursive: true });

  const main = [
    user({ uuid: 'u1', ts: 0, content: 'Add a health endpoint' }),
    // streamed twice: same requestId, output grows 10 → 40. Only the 40 must count.
    assistant({ uuid: 'a1', requestId: 'req_1', ts: 1000, model: 'claude-opus-5', usage: U(1000, 20000, 0, 0, 10), content: [{ type: 'text', text: 'ok' }] }),
    assistant({ uuid: 'a1b', requestId: 'req_1', ts: 1500, model: 'claude-opus-5', usage: U(1000, 20000, 0, 0, 40, 15), content: [{ type: 'tool_use', id: 'tu_read', name: 'Read', input: { file_path: '/home/dev/code/my.app/src/app.js' } }] }),
    user({ uuid: 'u2', ts: 2000, content: [{ type: 'tool_result', tool_use_id: 'tu_read', content: 'x' }], toolUseResult: { type: 'text', file: { filePath: '/home/dev/code/my.app/src/app.js', content: 'x', numLines: 1 } } }),
    assistant({ uuid: 'a2', requestId: 'req_2', ts: 3000, model: 'claude-opus-5', usage: U(0, 0, 0, 21000, 100), content: [{ type: 'tool_use', id: 'tu_bash', name: 'Bash', input: { command: 'npm test' } }] }),
    user({ uuid: 'u3', ts: 4000, content: [{ type: 'tool_result', tool_use_id: 'tu_bash', content: 'x' }], toolUseResult: { stdout: 'line1\nline2\nline3', stderr: '', interrupted: false } }),
    assistant({ uuid: 'a3', requestId: 'req_3', ts: 5000, model: 'claude-opus-5', usage: U(0, 500, 0, 21000, 60), content: [{ type: 'tool_use', id: 'tu_agent', name: 'Agent', input: { subagent_type: 'Explore', description: 'Find tests', prompt: '...' } }] }),
    user({ uuid: 'u4', ts: 6000, content: [{ type: 'tool_result', tool_use_id: 'tu_agent', content: 'done' }], toolUseResult: { agentId: FIXTURE.agentId, description: 'Find tests', resolvedModel: 'claude-haiku-4-5', status: 'completed' } }),
    // second human turn, via a slash command → skill attribution
    user({ uuid: 'u5', ts: 10000, content: '<command-name>/nxy:stats</command-name>\n<command-message>stats</command-message>' }),
    assistant({ uuid: 'a4', requestId: 'req_4', ts: 11000, model: 'claude-sonnet-5', usage: U(2000, 0, 0, 0, 200), content: [{ type: 'text', text: 'stats' }] }),
    // unknown model → its cost is unknown; the session total becomes partial ($X+?), never a bare ?
    assistant({ uuid: 'a5', requestId: 'req_5', ts: 12000, model: 'claude-future-9', usage: U(10, 0, 0, 0, 10), content: [{ type: 'text', text: '?' }] }),
    // Claude Code internal message: `<synthetic>` with zero usage → not an API call, must not poison anything
    assistant({ uuid: 'a6', requestId: 'req_6', ts: 12000, model: '<synthetic>', usage: U(0, 0, 0, 0, 0), content: [{ type: 'text', text: 'internal' }], effort: null }),
  ];
  const mainPath = join(projDir, `${FIXTURE.sessionId}.jsonl`);
  writeFileSync(mainPath, main.map((l) => JSON.stringify(l)).join('\n') + '\n');

  const sub = [
    user({ uuid: 's-u1', ts: 5100, content: 'Find tests', sidechain: true }),
    assistant({ uuid: 's-a1', requestId: 'req_s1', ts: 5200, model: 'claude-haiku-4-5', usage: U(3000, 0, 0, 0, 300), content: [{ type: 'tool_use', id: 's-tu', name: 'Bash', input: { command: 'ls' } }], sidechain: true, agentId: FIXTURE.agentId, effort: null }),
    user({ uuid: 's-u2', ts: 5300, content: [{ type: 'tool_result', tool_use_id: 's-tu', content: 'x' }], toolUseResult: { stdout: 'a\nb', stderr: '', interrupted: false }, sidechain: true }),
    assistant({ uuid: 's-a2', requestId: 'req_s2', ts: 5400, model: 'claude-haiku-4-5', usage: U(0, 0, 0, 3100, 50), content: [{ type: 'text', text: 'found' }], sidechain: true, agentId: FIXTURE.agentId, effort: null }),
  ];
  writeFileSync(join(agentsDir, `agent-${FIXTURE.agentId}.jsonl`), sub.map((l) => JSON.stringify(l)).join('\n') + '\n');
  writeFileSync(join(agentsDir, `agent-${FIXTURE.agentId}.meta.json`), JSON.stringify({ agentType: 'Explore', description: 'Find tests', toolUseId: 'tu_agent', spawnDepth: 1 }));
  return { root, mainPath };
}
