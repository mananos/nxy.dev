// @ts-check
/**
 * End-to-end: `trend.mjs` and `stats.mjs` run against the fixture project. The fixture has one
 * unknown-priced call (claude-future-9) and one `<synthetic>` zero-usage line, so this pins the
 * "partial total, never a bare ?" behaviour on the real CLI output (table and --json).
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { FIXTURE, writeFixture } from './fixtures/make-transcript.mjs';
import { fmtUsdPartial } from '../scripts/lib/format.mjs';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const KNOWN = 0.159125 + 0.006 + 0.00506; // opus + sonnet + haiku calls of the fixture
const close = (a, b, msg) => assert.ok(Math.abs(a - b) < 1e-9, `${msg}: ${a} vs ${b}`);

/** A project dir whose `.nxy/config.json` points the metrics at the fixture transcripts. */
function setup() {
  const projectsDir = mkdtempSync(join(tmpdir(), 'nxy-cli-tx-'));
  writeFixture(projectsDir);
  const cwd = mkdtempSync(join(tmpdir(), 'nxy-cli-cwd-'));
  mkdirSync(join(cwd, '.nxy'));
  writeFileSync(join(cwd, '.nxy', 'config.json'), JSON.stringify({ metrics: { projectsDir } }));
  return cwd;
}

function run(script, args) {
  const r = spawnSync(process.execPath, [join(ROOT, 'scripts', 'metrics', script), ...args], { encoding: 'utf8', env: { ...process.env, CLAUDE_CODE_SESSION_ID: '' } });
  assert.equal(r.status, 0, `${script} ${args.join(' ')} failed:\n${r.stderr}`);
  return r.stdout;
}

test('fmtUsdPartial: complete, partial and fully unknown sums', () => {
  assert.equal(fmtUsdPartial(164.2, false), '$164.20');
  assert.equal(fmtUsdPartial(164.2, true), '$164.20+?');
  assert.equal(fmtUsdPartial(0.004, true), '$0.004+?');
  assert.equal(fmtUsdPartial(0, true), '?', 'nothing known → plain ?');
  assert.equal(fmtUsdPartial(null, true), '?');
  assert.equal(fmtUsdPartial(null, false), '?');
  assert.equal(fmtUsdPartial(0, false), '$0.000');
});

test('trend --by model: <synthetic> hidden, unknown model flagged partial, footer keeps the known sum', () => {
  const cwd = setup();
  const json = JSON.parse(run('trend.mjs', ['--by', 'model', '--since', '30d', '--cwd', cwd, '--json']));
  const names = json.models.map((m) => m.model);
  assert.ok(!names.includes('<synthetic>'), `synthetic must not be a model row: ${names}`);
  assert.ok(names.includes('claude-future-9'));
  const future = json.models.find((m) => m.model === 'claude-future-9');
  assert.equal(future.usd, 0);
  assert.equal(future.usdPartial, true);
  assert.deepEqual(future.unknownModels, ['claude-future-9']);
  const opus = json.models.find((m) => m.model === 'claude-opus-5');
  close(opus.usd, 0.159125, 'opus row');
  assert.equal(opus.usdPartial, false);
  close(json.total.usd, KNOWN, 'total = known part');
  assert.equal(json.total.usdPartial, true);
  assert.deepEqual(json.total.unknownModels, ['claude-future-9']);

  const text = run('trend.mjs', ['--by', 'model', '--since', '30d', '--cwd', cwd]);
  assert.ok(!text.includes('<synthetic>'));
  assert.match(text, /claude-future-9.*\?$/m, 'unknown model row shows ?');
  assert.match(text, /^total: .* \$0\.170\+\?$/m, 'footer shows known sum marked partial');
  assert.match(text, /no pricing for: claude-future-9/);
});

for (const by of ['day', 'week', 'session']) {
  test(`trend --by ${by}: period total is $X+? instead of ?`, () => {
    const cwd = setup();
    const json = JSON.parse(run('trend.mjs', ['--by', by, '--since', '30d', '--cwd', cwd, '--json']));
    assert.equal(json.periods.length, 1);
    close(json.periods[0].usd, KNOWN, 'period known sum');
    assert.equal(json.periods[0].usdPartial, true);
    assert.deepEqual(json.periods[0].unknownModels, ['claude-future-9']);
    assert.equal(json.sessions[0].sessionId, FIXTURE.sessionId);
    assert.equal(json.sessions[0].calls, 7, 'synthetic line not counted as a call');
    close(json.total.usd, KNOWN, 'footer known sum');
    assert.equal(json.total.usdPartial, true);

    const text = run('trend.mjs', ['--by', by, '--since', '30d', '--cwd', cwd]);
    assert.match(text, /\$0\.170\+\?/, 'row shows partial sum');
    assert.match(text, /^total: .* \$0\.170\+\? · /m, 'footer shows partial sum');
    // the USD cell sits right before the `nxy` cell; `rtk saved` may legitimately be `?` without rtk's ledger
    assert.doesNotMatch(text, /\s\?\s+(yes|no|\d+\/\d+)\s*$/m, 'no bare ? for USD');
  });
}

test('stats: session totals, by-model and note show partial sums; <synthetic> absent', () => {
  const cwd = setup();
  const json = JSON.parse(run('stats.mjs', ['--session', 'last', '--all', '--cwd', cwd, '--json']));
  close(json.usage.usd, KNOWN, 'session known sum');
  assert.equal(json.usage.usdPartial, true);
  assert.deepEqual(json.unknownModels, ['claude-future-9']);
  assert.ok(!('<synthetic>' in json.byModel));
  assert.equal(json.byModel['claude-future-9'].usdPartial, true);
  assert.equal(json.byAgentType.Explore.usdPartial, false);
  assert.equal(json.agents[0].usdPartial, false);

  const text = run('stats.mjs', ['--session', 'last', '--all', '--cwd', cwd]);
  assert.ok(!text.includes('<synthetic>'));
  // label is `USD` or `USD-equiv` depending on the user's `metrics.subscription`
  assert.match(text, /^USD\S* +\$0\.170\+\? +\$0\.165\+\? +\$0\.005$/m, 'total / main partial, subagents complete');
  assert.match(text, /no pricing for: claude-future-9 — USD\S* shown as \$…\+\? where affected/);
  assert.match(text, /^claude-future-9 .* \?$/m, 'unknown model row: nothing known → ?');
  assert.match(text, /^claude-opus-5 .* \$0\.159$/m);
});

test('stats: shape line — calls per turn, average main context, subagent share', () => {
  const cwd = setup();
  const json = JSON.parse(run('stats.mjs', ['--session', 'last', '--all', '--cwd', cwd, '--json']));
  // fixture: 2 prompts, 7 calls (5 main + 2 subagent), main input 65 510 tokens over 5 calls
  assert.equal(json.shape.callsPerTurn, 3.5);
  assert.equal(json.shape.mainCallsPerTurn, 2.5);
  assert.equal(json.shape.mainCtxAvg, 13102);
  close(json.shape.subagentSharePct, ((3000 + 3100 + 350) / (json.usage.totalInput + json.usage.usage.output)) * 100, 'subagent share');
  assert.equal(json.rtkSavings, null, 'no rtk rows for the fixture → ledger not consulted');

  const text = run('stats.mjs', ['--session', 'last', '--all', '--cwd', cwd]);
  assert.match(text, /^shape: 3\.5 calls\/turn \(main 2\.5\) · main context avg 13k\/call · peak 22k · subagents 9% of tokens$/m);
});

test('trend: calls/turn and ctx/call columns', () => {
  const cwd = setup();
  const json = JSON.parse(run('trend.mjs', ['--by', 'session', '--since', '30d', '--cwd', cwd, '--json']));
  assert.equal(json.periods[0].callsPerTurn, 3.5);
  assert.equal(json.periods[0].ctxPerCall, 13102);
  assert.equal(json.sessions[0].mainCalls, 5);
  const text = run('trend.mjs', ['--by', 'session', '--since', '30d', '--cwd', cwd]);
  assert.match(text, /calls\/turn\s+ctx\/call\s+main edits\s+bash lines\s+rtk\s+rtk saved/);
  assert.match(text, /\s3\.5\s+13k\s+2\s/, '3.5 calls/turn · 13k ctx/call · 2 files edited by main');
  assert.equal(json.periods[0].mainEdits, 2);
});
