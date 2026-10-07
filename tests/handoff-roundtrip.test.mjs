// @ts-check
/**
 * `handoff show` prints derived lines (a header and nxy's recorded progress). Saving that output back
 * must not store them: the plan hash stays and the progress keeps coming from nxy's records.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { CONTEXT_CLOSE, CONTEXT_OPEN, stripDerivedLines } from '../core/memory/handoff.mjs';
import { planHash } from '../core/plan.mjs';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const HOOKS = join(ROOT, 'hosts', 'claude-code', 'hooks');
const MEM = join(ROOT, 'hosts', 'claude-code', 'entries', 'mem.mjs');
const PROGRESS = 'Progress (recorded by nxy, not by the model): batch 1 ✔, 2 pending';

test('stripDerivedLines: the header and the progress line go, a line the user wrote stays', () => {
  const shown = `handoff · feature/x · updated 2026-10-07 12:30 · plan abc123\n\nroute: implementer\n## Next\n- the rest\n\n${PROGRESS}\n`;
  assert.equal(stripDerivedLines(shown), 'route: implementer\n## Next\n- the rest');
  assert.equal(stripDerivedLines('route: x\nProgress: batch 1 done\nhandoff · notes'), 'route: x\nProgress: batch 1 done\nhandoff · notes');
  assert.equal(stripDerivedLines('a\r\n\r\n' + PROGRESS), 'a');
  assert.equal(stripDerivedLines(''), '');
});

test('handoff show then save: plan hash unchanged, progress still passed, no Progress line stored', () => {
  const dir = mkdtempSync(join(tmpdir(), 'nxy-rt-'));
  const repo = join(dir, 'repo');
  mkdirSync(join(repo, '.git'), { recursive: true });
  writeFileSync(join(repo, '.git', 'HEAD'), 'ref: refs/heads/feature/x\n');
  writeFileSync(join(repo, '.git', 'config'), '[remote "origin"]\n\turl = git@github.com:x/y.git\n');
  const env = { ...process.env, NXY_HOME: join(dir, 'home'), CLAUDE_PROJECT_DIR: repo };
  const node = (args, input) => spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', ...args, '--cwd', repo], { input, encoding: 'utf8', env, cwd: repo });

  const PLAN = ['## Plan', 'Goal: x', '### Batch 1 — a', '- `src/a.ts` — x', 'Accept: `npx vitest run src/a.test.ts`', '### Batch 2 — b', '- `src/b.ts` — y', 'Accept: `npx vitest run src/b.test.ts`'].join('\n');
  const hash = planHash(PLAN);
  node([MEM, 'handoff', 'plan'], PLAN);

  const t = join(dir, 'agent.jsonl');
  writeFileSync(t, [
    { type: 'user', message: { content: `${CONTEXT_OPEN}\n${PLAN}\n${CONTEXT_CLOSE}\n\nBatch 1 — a` } },
    { type: 'assistant', message: { content: [{ type: 'tool_use', id: 'e', name: 'Edit', input: { file_path: 'x' } }] } },
    { type: 'assistant', message: { content: [{ type: 'tool_use', id: 'r', name: 'Bash', input: { command: 'npx vitest run src/a.test.ts' } }] } },
    { type: 'user', message: { content: [{ type: 'tool_result', tool_use_id: 'r', content: 'ok' }] } },
  ].map((l) => JSON.stringify(l)).join('\n') + '\n');
  spawnSync(process.execPath, ['--disable-warning=ExperimentalWarning', join(HOOKS, 'subagentstop.mjs')], {
    input: JSON.stringify({ cwd: repo, agent_id: 'impl-1', agent_type: 'nxy:implementer', agent_transcript_path: t }), encoding: 'utf8', env,
  });

  const shown = node([MEM, 'handoff', 'show']).stdout;
  assert.match(shown, /^handoff · /, 'show prints the header');
  assert.match(shown, /batch 1 ✔, 2 pending$/m);
  assert.ok(shown.includes(`plan ${hash}`));

  const saved = node([MEM, 'handoff', 'save'], shown);
  assert.match(saved.stdout, new RegExp(`plan ${hash} kept`), saved.stdout + saved.stderr);

  const again = node([MEM, 'handoff', 'show']).stdout;
  assert.ok(again.includes(`plan ${hash}`), 'plan hash unchanged');
  assert.match(again, /batch 1 ✔, 2 pending$/m, 'progress still from the records');
  assert.equal((again.match(/Progress \(recorded by nxy/g) || []).length, 1, 'only the derived line, none stored');
  assert.equal((again.match(/^handoff · /gm) || []).length, 1, 'only the derived header');
});
