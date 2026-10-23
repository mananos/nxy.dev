// @ts-check
/**
 * The role table, applied (0.4.4): `roles.<role>.model` in the config becomes the `model` of each
 * dispatch of that nxy agent, via the Agent hook's `updatedInput`. Until then the table only said
 * whether a role existed — the model was whatever the agent's frontmatter fixed.
 *
 * `roles.<role>.effort` works the same way (1.1.0): it becomes the `effort` of the dispatch.
 *
 * Since 1.3.1 both are always sent, the frontmatter's when the table says nothing: Claude Code 2.1.300
 * launches subagents "with defaults" and runs them on the main thread's model, ignoring the agent's
 * `model:` (an explicit `model` on the dispatch still wins). Only nxy's own agents, and only Claude
 * Code's model aliases and the efforts nxy offers; anything else is left as the agent says.
 */
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { EFFORTS, MODELS } from '../../core/configedit.mjs';
import { parseAgentMeta } from '../../core/agentview.mjs';
import { PLUGIN_ROOT } from '../../core/paths.mjs';

/** What the Agent tool accepts as `model`. */
const ALIASES = new Set(MODELS);

/** @param {string} name */
function frontmatterModel(name) {
  try {
    const text = readFileSync(join(PLUGIN_ROOT, 'agents', `${name}.md`), 'utf8');
    return /^model:\s*(\S+)/m.exec(text)?.[1] ?? null;
  } catch {
    return null;
  }
}

/**
 * The model this dispatch should run on: the role table's, else the agent's own; null leaves the
 * dispatch as it is (not an nxy agent, no valid alias, or the dispatch already names one).
 * @param {string} agentType `nxy:planner`, or `planner` for a plugin agent addressed bare
 * @param {{roles?: Record<string, {model?: string}|null>}} cfg
 * @param {string|undefined} [requested] a model the dispatch already names: the main thread's choice wins
 */
export function roleModel(agentType, cfg, requested) {
  if (requested) return null;
  const name = String(agentType || '').replace(/^nxy:/, '');
  if (!/^[a-z-]+$/.test(name) || !existsSync(join(PLUGIN_ROOT, 'agents', `${name}.md`))) return null;
  const want = cfg.roles?.[name]?.model;
  if (typeof want === 'string' && ALIASES.has(want)) return want;
  const own = frontmatterModel(name);
  return own && ALIASES.has(own) ? own : null;
}

/** @param {string} name */
export function frontmatterEffort(name) {
  try {
    return parseAgentMeta(readFileSync(join(PLUGIN_ROOT, 'agents', `${name}.md`), 'utf8')).effort || null;
  } catch {
    return null;
  }
}

/**
 * The effort this dispatch should run with: the role table's, else the agent's own; null leaves the
 * dispatch as it is.
 * @param {string} agentType
 * @param {{roles?: Record<string, {effort?: string}|null>}} cfg
 * @param {string|undefined} [requested] an effort the dispatch already names: it wins
 */
export function roleEffort(agentType, cfg, requested) {
  if (requested) return null;
  const name = String(agentType || '').replace(/^nxy:/, '');
  if (!/^[a-z-]+$/.test(name) || !existsSync(join(PLUGIN_ROOT, 'agents', `${name}.md`))) return null;
  const want = cfg.roles?.[name]?.effort;
  if (typeof want === 'string' && EFFORTS.includes(want)) return want;
  const own = frontmatterEffort(name);
  return own && EFFORTS.includes(own) ? own : null;
}
