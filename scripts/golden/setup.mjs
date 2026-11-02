// @ts-check
/**
 * Golden of the 1.x install planner (`core/setup.mjs`) that `internal/setup` ports. Node is the oracle.
 *   plan:   {tool, opts} -> {plan: installPlan, describe: describePlan, describeManual (only when it differs)}
 *   pm:     {platform, has: [bins]} -> {pm: packageManager | null}
 *   detect: {settings, mcp, obj} -> {rtk: detectRtkConflict, mcp: detectCodegraphMcp, wiring: detectCodegraphWiring}
 *   sudo:   {text} -> {sudo: whether runPlan treats it as "sudo -n needed a password or terminal"}
 *   run:    {tool, opts, results} -> runPlan with a scripted spawn (result i answers call i; no entry = exit 0).
 *           A result is {stdout, stderr, status, error: {message}} or stdoutRepeat: [text, times].
 * Sets: plan, pm, detect, sudo, run. `sudo` goes through runPlan on a sudo step (the regex is not exported).
 *
 *   node scripts/golden/setup.mjs          write testdata/golden/setup/*.json
 *   node scripts/golden/setup.mjs --check  fail when they differ
 */
import { fileURLToPath } from 'node:url';
import { describePlan, detectCodegraphMcp, detectCodegraphWiring, detectRtkConflict, installPlan, packageManager, runPlan } from '../../core/setup.mjs';
import { diffArea, loadCorpus, staleSets, writeArea } from './lib.mjs';

export const AREA = 'setup';

const idx = (/** @type {number} */ i) => String(i).padStart(3, '0');

/** @param {any} r a scripted result of the corpus */
function scripted(r) {
  const stdout = r.stdoutRepeat ? r.stdoutRepeat[0].repeat(r.stdoutRepeat[1]) : r.stdout;
  return { stdout, stderr: r.stderr, status: r.status === undefined ? 0 : r.status, error: r.error };
}

/** @param {any} results @param {any} plan */
function runWith(plan, results) {
  let i = 0;
  const r = runPlan(plan, { spawn: () => scripted(results[i++] || {}) });
  return { ok: r.ok, ran: r.ran, failedAt: r.failedAt, tail: r.tail, manual: r.manual };
}

/** @returns {Record<string, any[]>} */
export function generate() {
  /** @type {any} */
  const c = loadCorpus('setup');

  /** @type {Array<[string, any]>} */
  const planInputs = [];
  for (const tool of c.matrix.tools) {
    for (const platform of c.matrix.oses) for (const found of [false, true]) planInputs.push([tool, { platform, found }]);
  }
  for (const [platform, pms, founds] of c.matrix.rg) {
    for (const pm of pms) for (const found of founds) planInputs.push(['rg', pm === null ? { platform, found } : { platform, pm, found }]);
  }
  planInputs.push(...c.plans);

  const plan = planInputs.map(([tool, opts], i) => {
    const p = installPlan(tool, opts);
    const describe = describePlan(p);
    const describeManual = describePlan(p, { manual: true });
    const output = { plan: p, describe, ...(describeManual.length === describe.length ? {} : { describeManual }) };
    return { name: `plan:${idx(i)}:${tool}:${opts.platform}`, input: { tool, opts }, output };
  });

  const pm = [];
  for (const platform of c.pm.platforms) {
    for (const has of c.pm.has) {
      pm.push({
        name: `pm:${platform}:${has.join('+') || 'none'}`,
        input: { platform, has },
        output: { pm: packageManager(platform, (/** @type {string} */ b) => has.includes(b)) },
      });
    }
  }

  const detect = c.detect.map((/** @type {any} */ d, /** @type {number} */ i) => ({
    name: `detect:${idx(i)}`,
    input: { settings: d.settings, mcp: d.mcp, obj: d.obj },
    output: { rtk: detectRtkConflict(d.settings), mcp: detectCodegraphMcp(d.obj), wiring: detectCodegraphWiring(d.settings, d.mcp) },
  }));

  const sudoPlan = installPlan('rg', { platform: 'linux', pm: 'apt-get' });
  const sudo = c.sudo.map((/** @type {string} */ text, /** @type {number} */ i) => {
    // The step fails with `text`; the plan's manual note shows up in the tail only when the regex matched.
    const r = runPlan(sudoPlan, { spawn: () => ({ stdout: '', stderr: text, status: 1 }) });
    return { name: `sudo:${idx(i)}`, input: { text }, output: { sudo: r.tail.includes('sudo no pudo pedir contraseña') } };
  });

  const run = c.run.map(([tool, opts, results], /** @type {number} */ i) => ({
    name: `run:${idx(i)}:${tool}`,
    input: { tool, opts, results },
    output: runWith(installPlan(tool, opts), results),
  }));

  return { plan, pm, detect, sudo, run };
}

/** @returns {string[]} differences, empty when up to date */
export function check() {
  const sets = generate();
  const diffs = [];
  for (const [set, cases] of Object.entries(sets)) diffs.push(...diffArea(AREA, set, cases, true));
  diffs.push(...staleSets(AREA, Object.keys(sets)));
  return diffs;
}

export function write() {
  return Object.entries(generate()).map(([set, cases]) => writeArea(AREA, set, cases, true));
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  if (process.argv.includes('--check')) {
    const diffs = check();
    for (const d of diffs) console.error(d);
    if (diffs.length) process.exit(1);
    console.log('golden files up to date');
  } else {
    for (const p of write()) console.log(`wrote ${p}`);
  }
}
