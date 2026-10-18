// @ts-check
// Arms a saved plan for its task: approval marker, dirty-at-approval, the "before" copy and the inventory,
// all under one key (the task id, or the branch for a legacy handoff). Shared by `mem handoff plan|save`.
import { parseQuestions, parseRepos, planHash } from '../../core/plan.mjs';
import { writeMarker } from './plan-approval.mjs';
import { snapshotPlanFiles } from './baseline.mjs';
import { recordDirtyAtApproval } from './gitstate.mjs';
import { resolveRepos } from './repos.mjs';
import { takeInventory } from './inventory.mjs';

/**
 * @param {string} cwd
 * @param {string} key
 * @param {string} plan
 */
export function armPlan(cwd, key, plan) {
  const hash = planHash(plan);
  const questions = parseQuestions(plan).length;
  const repos = resolveRepos(cwd, parseRepos(plan));
  writeMarker(cwd, key, hash, questions, repos.map((r) => r.root));
  recordDirtyAtApproval(cwd, key, repos, hash);
  const pre = snapshotPlanFiles(cwd, key, plan, repos);
  takeInventory(cwd, key, repos);
  return { hash, questions, repos, copied: pre.taken };
}
