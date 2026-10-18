// @ts-check
import assert from 'node:assert/strict';
import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';
import { hasMap, isPlanRevision, plannerMapMessage } from '../core/plan.mjs';
import { sandbox } from './sandbox.mjs';

const PLAN = '## Plan\nGoal: g\n### Batch 1 — a: x\n- `a.txt:1` — change\nAccept: `node -e 0`\n';
const PLANNER = 'nxy:planner';

test('hasMap / isPlanRevision', () => {
  assert.equal(hasMap('task\n\n## Map\n- a.mjs:3'), true);
  assert.equal(hasMap('no map here'), false);
  assert.equal(isPlanRevision('Finish nxy plan abc'), true);
  assert.equal(isPlanRevision('Plan a thing'), false);
  assert.match(plannerMapMessage(), /## Map/);
});

test('the first planner dispatch without a map is denied once per task', () => {
  const s = sandbox(PLAN);
  const first = s.dispatch('Plan the thing', s.main(), PLANNER);
  assert.equal(first?.permissionDecision, 'deny');
  assert.match(first?.permissionDecisionReason, /nxy:scout/);
  const second = s.dispatch('Plan the thing', s.main(), PLANNER);
  assert.notEqual(second?.permissionDecision, 'deny');
});

test('a planner dispatch with a map, a finish/change prompt passes at once', () => {
  const s = sandbox(PLAN);
  assert.notEqual(s.dispatch('Plan it\n\n## Map\n- a.mjs:3 — x', s.main(), PLANNER)?.permissionDecision, 'deny');
  assert.notEqual(s.dispatch('Finish nxy plan abc123', s.main(), PLANNER)?.permissionDecision, 'deny');
});

test('without roles.scout the planner passes', () => {
  const s = sandbox(PLAN);
  mkdirSync(join(s.repo, '.nxy'), { recursive: true });
  writeFileSync(join(s.repo, '.nxy', 'config.json'), JSON.stringify({ roles: { scout: null } }));
  assert.notEqual(s.dispatch('Plan the thing', s.main(), PLANNER)?.permissionDecision, 'deny');
});

test('a subagent dispatch passes', () => {
  const s = sandbox(PLAN);
  const out = s.hook('pretooluse-agent.mjs', {
    tool_name: 'Agent', agent_id: 'a1', agent_type: 'nxy:implementer', transcript_path: s.main(),
    tool_input: { subagent_type: PLANNER, prompt: 'Plan the thing' },
  });
  assert.doesNotMatch(out, /"deny"/);
});
