// @ts-check
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { render } from '../hosts/claude-code/statusline.mjs';
import { shimSource, versionsDirFor } from '../hosts/claude-code/entries/statusline-setup.mjs';
import { colorize, severity } from '../core/format.mjs';
import { gitBranch } from '../core/paths.mjs';
import { loadConfig } from '../core/config.mjs';
import { newIncrementalState } from '../hosts/claude-code/transcripts.mjs';

const RED = '\x1b[31m';
const RESET = '\x1b[0m';
const BOLD_RED = '\x1b[1;31m';
const BOLD_YELLOW = '\x1b[1;33m';

/** @param {Partial<ReturnType<typeof newIncrementalState>>} over */
const state = (over) => ({ ...newIncrementalState(), ...over });
const usage = (input, output) => ({ input, cacheWrite5m: 0, cacheWrite1h: 0, cacheRead: 0, output, thinking: 0 });

/** classic preset unless overridden — the repo default may be another preset */
function cfgWith(over = {}) {
  const cfg = loadConfig(mkdtempSync(join(tmpdir(), 'nxy-sl-cfg-')), {});
  cfg.metrics.statusline = { ...cfg.metrics.statusline, preset: 'classic', layout: 'line', ...over };
  return cfg;
}
const PLAIN = cfgWith({ color: false });
const NOW = Date.parse('2026-09-20T12:00:00.000Z');
const at = (input, main, cfg = cfgWith(), ctx = {}) => render(input, { main, agents: {} }, { agentCount: 0, branch: null, now: NOW, ...ctx }, cfg);

test('colorize: style tokens, backgrounds, severity, opt-out', () => {
  assert.equal(colorize('x', 'red'), `${RED}x${RESET}`);
  assert.equal(colorize('x', 'bold yellow'), `${BOLD_YELLOW}x${RESET}`, 'space-separated tokens combine');
  assert.equal(colorize('x', '80'), '\x1b[38;5;80mx\x1b[0m', 'palette index');
  assert.equal(colorize('x', '#ff8800'), '\x1b[38;2;255;136;0mx\x1b[0m', 'truecolor');
  assert.equal(colorize('x', 'bg:red'), '\x1b[41mx\x1b[0m');
  assert.equal(colorize('x', 'bg:muted'), '\x1b[48;5;245mx\x1b[0m');
  assert.equal(colorize('x', 'white bg:#102030'), '\x1b[37;48;2;16;32;48mx\x1b[0m');
  assert.equal(colorize('x', '80 80'), '\x1b[38;5;80mx\x1b[0m', 'duplicate codes collapse');
  assert.equal(colorize('x', 'text'), 'x');
  assert.equal(colorize('x', 'nonsense'), 'x', 'unknown tokens never break the line');
  assert.equal(colorize('x', 'constructor toString bg:constructor'), 'x', 'prototype keys are not style tokens');
  assert.equal(colorize('x', 'red', false), 'x', 'color:false leaves text untouched');
  assert.equal(severity(41, 80, 95), null);
  assert.equal(severity(80, 80, 95), 'warn');
  assert.equal(severity(95, 80, 95), 'crit');
  assert.equal(severity(null, 80, 95), null);
});

test('render: full line, plain — brand, where, model, ctx, turn, session, cache, windows', () => {
  const cache = {
    main: state({ calls: 3, usage: usage(3000, 300), usd: 1.5, model: 'claude-opus-5', turns: 2, turnUsd: 0.42, lastTs: NOW - 60_000 }),
    agents: { 'agent-a.jsonl': state({ calls: 2, usage: usage(1000, 100), usd: 0.05 }) },
  };
  const input = {
    cwd: '/home/dev/code/my.app',
    model: { display_name: 'Opus 5' },
    effort: { level: 'high' },
    context_window: { used_percentage: 41, total_input_tokens: 82_000, context_window_size: 200_000 },
    prompt_cache: { hit_ratio: 0.98 },
    rate_limits: { five_hour: { used_percentage: 62 }, seven_day: { used_percentage: 31 } },
  };
  const ctx = { agentCount: 1, branch: 'main', now: NOW };
  assert.equal(render(input, cache, ctx, PLAIN), '◆ nxy ⟡ my.app main ⟡ Opus 5 · high ⟡ ctx ▰▰▰▱▱▱▱▱ 41% · 82k ⟡ turno $0.420~ ⟡ sesión $1.55~ · 4.0k→400 · +1 agent ⟡ cache 98% ⟡ 5h 62% · 7d 31%');
  assert.ok(render(input, cache, ctx, cfgWith({ color: false, brand: '' })).startsWith('my.app'), 'brand: "" hides it');
});

test('render: classic roles are painted and the theme is overridable', () => {
  const main = state({ calls: 1, usage: usage(10, 1), usd: 0.01, turns: 1, turnUsd: 0.01, lastTs: NOW });
  const input = { cwd: '/x/app', model: { display_name: 'Opus 5' }, effort: { level: 'high' }, context_window: { used_percentage: 41, total_input_tokens: 82_000 } };
  const line = at(input, main, cfgWith(), { branch: 'main' });
  assert.ok(line.includes('\x1b[1;38;5;80m◆ nxy\x1b[0m'), 'brand in accent');
  assert.ok(line.includes('\x1b[38;5;245mapp\x1b[0m \x1b[1mmain\x1b[0m'), 'path muted, branch bold');
  assert.ok(line.includes('\x1b[38;5;80m▰▰▰\x1b[0m\x1b[2m▱▱▱▱▱\x1b[0m'), 'gauge: filled accent, empty dim');
  assert.ok(line.includes('\x1b[38;5;245mctx\x1b[0m') && line.includes('\x1b[38;5;110mhigh\x1b[0m'), 'labels muted, effort powder blue');
  const themed = at(input, main, cfgWith({ theme: { brand: 'magenta', gauge: '#00ff00' } }), { branch: 'main' });
  assert.ok(themed.includes('\x1b[35m◆ nxy\x1b[0m') && themed.includes('\x1b[38;2;0;255;0m▰▰▰\x1b[0m'), `theme override: ${themed}`);
});

test('render: presets and layouts', () => {
  const main = state({ calls: 1, usage: usage(10, 1), usd: 0.01, turns: 1, turnUsd: 0.5, lastTs: NOW });
  const input = { cwd: '/x/app', model: { display_name: 'Opus 5' }, effort: { level: 'high' }, context_window: { used_percentage: 41, total_input_tokens: 82_000 }, prompt_cache: { warm: true, hit_ratio: 0.9 }, rate_limits: { five_hour: { used_percentage: 12 } } };
  const vivid = at(input, main, cfgWith({ preset: 'vivid' }), { branch: 'main' });
  assert.ok(vivid.includes('\x1b[38;5;114mapp\x1b[0m') && vivid.includes('\x1b[38;5;252mturno\x1b[0m') && vivid.includes('\x1b[38;5;252;1m$0.500~\x1b[0m'), `vivid: segment hues, labels in hue, values hue+bold: ${vivid}`);
  assert.ok(!vivid.includes('38;5;245mctx'), 'vivid has no muted labels');
  const power = at(input, main, cfgWith({ preset: 'powerline' }), { branch: 'main' });
  assert.ok(power.includes('\x1b[1;38;5;16;48;5;80m◆ nxy\x1b[0m') && power.includes('48;5;24m') && !power.includes('⟡'), `powerline: backgrounds, padding, no glyph separator: ${power}`);
  const two = at(input, main, cfgWith({ color: false, layout: 'two-line' }), { branch: 'main' });
  const [l1, l2] = two.split('\n');
  assert.equal(l1, '◆ nxy ⟡ app main ⟡ Opus 5 · high ⟡ cache 90% ⟡ 5h 12%');
  assert.equal(l2, 'ctx ▰▰▰▰▰▰▰▱▱▱▱▱▱▱▱▱ 41% · 82k ⟡ turno $0.500~ ⟡ sesión $0.010~ · 10→1', 'second line: 16-cell gauge and the money');
  assert.ok(at(input, main, cfgWith({ color: false, separator: '|' })).includes(' | '), 'separator is configurable');
  assert.ok(at(input, main, cfgWith({ preset: 'nope', color: false })).startsWith('◆ nxy ⟡'), 'unknown preset → classic');
});

test('render: context severity follows absolute token thresholds, not the percentage', () => {
  const main = state({ calls: 1, usage: usage(10, 1), usd: 0.01, turns: 1, turnUsd: 0.01, lastTs: NOW });
  const ctx = (cw, cfg = cfgWith()) => at({ context_window: cw }, main, cfg);
  // defaults: warn 100k, crit 200k
  const small = ctx({ used_percentage: 41, total_input_tokens: 82_000, context_window_size: 200_000 });
  assert.ok(small.includes('\x1b[1m41%\x1b[0m') && !small.includes(BOLD_RED) && !small.includes(BOLD_YELLOW), `below warn: plain value: ${small}`);
  const big = ctx({ used_percentage: 41, total_input_tokens: 410_000, context_window_size: 1_000_000 });
  assert.ok(big.includes(`${BOLD_RED}▰▰▰${RESET}`) && big.includes(`${BOLD_RED}410k${RESET}`), `same 41% on a 1M window is red: ${big}`);
  assert.ok(ctx({ used_percentage: 60, total_input_tokens: 120_000 }).includes(`${BOLD_YELLOW}120k${RESET}`), 'warn → yellow');
  assert.ok(ctx({ used_percentage: 41, total_input_tokens: 410_000 }, cfgWith({ ctxCritTokens: 500_000 })).includes(`${BOLD_YELLOW}410k`), 'thresholds are configurable');
  assert.ok(!ctx({ used_percentage: 41, total_input_tokens: 410_000 }, PLAIN).includes('\x1b['), 'color:false disables ANSI');
  // older Claude Code without total_input_tokens: derive tokens from % × window size
  assert.ok(ctx({ used_percentage: 60, context_window_size: 1_000_000 }).includes(`${BOLD_RED}600k${RESET}`), 'derived tokens');
  const pctOnly = ctx({ used_percentage: 60 }, PLAIN);
  assert.ok(pctOnly.includes('ctx ▰▰▰▰▰▱▱▱ 60% ⟡') && !pctOnly.includes('60% ·'), 'percentage alone: no tokens');
});

test('render: the turn total is main + subagents, each with its own turn spend', () => {
  const main = state({ calls: 2, usage: usage(10, 1), usd: 1, turns: 2, turnUsd: 0.5, turnStartTs: NOW - 60_000, lastTs: NOW });
  const agents = {
    'agent-a.jsonl': state({ calls: 3, usage: usage(10, 1), usd: 5, turnUsd: 0.25, turnStartTs: NOW - 60_000 }),
    'agent-b.jsonl': state({ calls: 3, usage: usage(10, 1), usd: 9, turnUsd: 0, turnStartTs: NOW - 60_000 }), // ran in an earlier turn
  };
  const line = render({}, { main, agents }, { agentCount: 2, branch: null, now: NOW }, PLAIN);
  assert.ok(line.includes('turno $0.750~') && line.includes('sesión $15.00~'), `turn = 0.5 + 0.25, session = all: ${line}`);
});

test('render: turn cost severity (the hand-off signal) and partial sums', () => {
  const turn = (turnUsd, unknown = 0, cfg = cfgWith()) => at({}, state({ calls: 2, usage: usage(10, 1), usd: turnUsd, usdUnknown: unknown, turns: 1, turnUsd, turnUsdUnknown: unknown, lastTs: NOW }), cfg);
  assert.ok(turn(0.5).includes('\x1b[1m$0.500~\x1b[0m'), 'under $1: plain');
  assert.ok(turn(1.5).includes(`${BOLD_YELLOW}$1.50~${RESET}`), '$1 → warn');
  assert.ok(turn(3.89).includes(`${BOLD_RED}$3.89~${RESET}`), '$3 → crit');
  assert.ok(turn(3.89, 0, cfgWith({ turnCritUsd: 10 })).includes(`${BOLD_YELLOW}$3.89~`), 'configurable');
  assert.ok(turn(0.5, 1, PLAIN).includes('turno $0.500+?~ ⟡ sesión $0.500+?~'), 'partial sums keep +?');
  assert.ok(!turn(5, 1).includes(BOLD_RED), 'partial turn cost never claims a severity');
});

test('render: prompt_cache from Claude Code wins over the TTL heuristic; cold shows re-cache tokens and their price', () => {
  const main = state({ calls: 1, usage: usage(10, 1), usd: 0.01, turns: 1, lastTs: NOW - 60 * 60_000 }); // heuristic alone would say cold
  const model = { id: 'claude-opus-5', display_name: 'Opus 5' };
  const warm = at({ model, prompt_cache: { warm: true, caching_observed: true, ttl: '5m', expires_at: NOW / 1000 + 240, hit_ratio: 0.97, recache_tokens_if_cold: 830_000 } }, main, PLAIN);
  assert.ok(warm.includes('cache 97% · 4m'), `warm per Claude Code, countdown to expiry: ${warm}`);
  const cold = at({ model, prompt_cache: { warm: false, caching_observed: true, ttl: '5m', expires_at: NOW / 1000 - 120, hit_ratio: 0.97, recache_tokens_if_cold: 830_000 } }, main, PLAIN);
  assert.ok(cold.includes('❄830k $5.19~') && !cold.includes('cache 97%'), `cold: tokens to re-cache and their cache-write price: ${cold}`);
  const cold1h = at({ model, prompt_cache: { warm: false, caching_observed: true, ttl: '1h', expires_at: NOW / 1000 - 120, hit_ratio: 0.97, recache_tokens_if_cold: 830_000 } }, main, PLAIN);
  assert.ok(cold1h.includes('❄830k $8.30~'), `1h TTL uses the 1h cache-write price: ${cold1h}`);
  const noCaching = at({ model, prompt_cache: { warm: false, caching_observed: false, hit_ratio: null } }, main, PLAIN);
  assert.ok(!noCaching.includes('❄'), 'provider without caching: never cold');
  const unknownModel = at({ model: { id: 'claude-future-9' }, prompt_cache: { warm: false, caching_observed: true, ttl: '5m', recache_tokens_if_cold: 1000 } }, main, PLAIN);
  assert.ok(unknownModel.endsWith('❄1.0k'), `unknown price → tokens only, never a made-up number: ${unknownModel}`);
  const coldPainted = at({ model, prompt_cache: { warm: false, caching_observed: true, ttl: '5m', recache_tokens_if_cold: 830_000 } }, main);
  assert.ok(coldPainted.includes(`${BOLD_RED}❄830k${RESET}`), 'cold is painted crit');
});

test('render: without prompt_cache (older Claude Code) fall back to "last call older than the TTL"', () => {
  const input = { prompt_cache: { hit_ratio: 0.9 } };
  const warm = state({ calls: 1, usage: usage(10, 1), usd: 0.01, turns: 1, lastTs: NOW - 4 * 60_000 });
  assert.ok(at(input, warm, PLAIN).includes('cache 90%'), '4m < 5m TTL: still warm');
  const cold = state({ calls: 1, usage: usage(10, 1), usd: 0.01, turns: 1, lastTs: NOW - 12 * 60_000 });
  const line = at(input, cold, PLAIN);
  assert.ok(line.includes('❄12m') && !line.includes('cache'), `cold cache shows idle time: ${line}`);
  assert.ok(at(input, cold, cfgWith({ promptCacheTtlMin: 60, color: false })).includes('cache 90%'), 'TTL is configurable (1h cache)');
  assert.ok(at(input, state({ calls: 1, usage: usage(10, 1), usd: 0.01, turns: 1 }), PLAIN).includes('cache 90%'), 'unknown last call → never claims cold');
});

test('render: placeholders when nothing is known', () => {
  assert.equal(at({}, newIncrementalState(), PLAIN), '◆ nxy ⟡ ? ⟡ ctx ?% ⟡ no calls yet');
  const one = state({ calls: 2, usage: usage(10, 1), usd: 0.5, turns: 1, turnUsd: 0.5, lastTs: NOW });
  const line = at({ cwd: 'C:\\Users\\dev\\proj' }, one, PLAIN, { branch: 'feat/x' });
  assert.ok(line.includes('⟡ proj feat/x ⟡ '), `windows path basename: ${line}`);
});

test('gitBranch: reads HEAD from .git dir, worktree pointer, detached head; null outside a repo', () => {
  const root = mkdtempSync(join(tmpdir(), 'nxy-git-'));
  const repo = join(root, 'repo');
  mkdirSync(join(repo, '.git'), { recursive: true });
  mkdirSync(join(repo, 'src', 'deep'), { recursive: true });
  writeFileSync(join(repo, '.git', 'HEAD'), 'ref: refs/heads/feature/#3_stats\n');
  assert.equal(gitBranch(repo), 'feature/#3_stats');
  assert.equal(gitBranch(join(repo, 'src', 'deep')), 'feature/#3_stats', 'walks up to the repo root');

  const wtGit = join(repo, '.git', 'worktrees', 'wt');
  mkdirSync(wtGit, { recursive: true });
  writeFileSync(join(wtGit, 'HEAD'), 'ref: refs/heads/hotfix\n');
  const wt = join(root, 'wt');
  mkdirSync(wt);
  writeFileSync(join(wt, '.git'), `gitdir: ${wtGit}\n`);
  assert.equal(gitBranch(wt), 'hotfix');

  writeFileSync(join(repo, '.git', 'HEAD'), '2140138abcdef0123456789\n');
  assert.equal(gitBranch(repo), '2140138', 'detached → short sha');

  const bare = join(root, 'nogit');
  mkdirSync(bare);
  assert.equal(gitBranch(bare), null);
});

test('statusline shim: resolves the newest installed version, honours env, falls back to the checkout', () => {
  const root = mkdtempSync(join(tmpdir(), 'nxy-shim-'));
  const plugin = join(root, 'plugins', 'cache', 'nxy-dev', 'nxy');
  // Two layouts coexist in the wild: 0.1.x shipped `scripts/metrics/`, 0.2.0+ ships
  // `hosts/claude-code/`. The shim resolves versions it did not generate, so it must find both.
  const OLD = ['scripts', 'metrics'];
  const NEW = ['hosts', 'claude-code'];
  const fake = (dir, tag, layout = OLD) => {
    mkdirSync(join(dir, ...layout), { recursive: true });
    writeFileSync(join(dir, ...layout, 'statusline.mjs'), `export function main() { process.stdout.write(${JSON.stringify(tag)}); }\n`);
  };
  for (const v of ['0.1.0', '0.1.2', '0.1.10']) fake(join(plugin, v), `v${v}`);
  mkdirSync(join(plugin, '0.2.0')); // downloaded but empty: must be skipped
  const checkout = join(root, 'checkout');
  fake(checkout, 'dev', NEW);

  assert.equal(versionsDirFor(join(plugin, '0.1.2')), plugin);
  assert.equal(versionsDirFor(checkout), null, 'a git checkout has no versions dir');

  const runShim = (src, env = {}) => {
    const shim = join(root, `shim-${Math.random().toString(36).slice(2)}.mjs`);
    writeFileSync(shim, src);
    return execFileSync(process.execPath, [shim], { env: { ...process.env, NXY_STATUSLINE: '', ...env }, encoding: 'utf8' });
  };
  assert.equal(runShim(shimSource(join(plugin, '0.1.2'))), 'v0.1.10', 'no installed_plugins.json → newest: semver order, not lexical; empty 0.2.0 skipped');
  // Claude Code's registry names the active version: a rollback keeps hooks and statusline on the same code
  writeFileSync(join(root, 'plugins', 'installed_plugins.json'), JSON.stringify({ version: 2, plugins: { 'nxy@nxy-dev': [{ scope: 'user', installPath: join(plugin, '0.1.2'), version: '0.1.2' }] } }));
  assert.equal(runShim(shimSource(join(plugin, '0.1.2'))), 'v0.1.2', 'installed version wins over the newest cached one');
  writeFileSync(join(root, 'plugins', 'installed_plugins.json'), '{not json');
  assert.equal(runShim(shimSource(join(plugin, '0.1.2'))), 'v0.1.10', 'broken registry → newest');
  // a throwing main never leaves the statusline blank
  mkdirSync(join(plugin, '0.1.11', ...OLD), { recursive: true });
  writeFileSync(join(plugin, '0.1.11', ...OLD, 'statusline.mjs'), "export function main() { throw new Error('boom'); }\n");
  assert.equal(runShim(shimSource(join(plugin, '0.1.2'))), 'nxy ?', 'error inside main → placeholder, exit 0');
  assert.equal(runShim(shimSource(checkout)), 'dev', 'no cache layout → the checkout that ran --apply');
  assert.equal(runShim(shimSource(join(plugin, '0.1.2')), { NXY_STATUSLINE: join(checkout, ...NEW, 'statusline.mjs') }), 'dev', 'env override wins');

  // A post-reorder version installed in the cache resolves through the same shim.
  fake(join(plugin, '0.2.1'), 'v0.2.1', NEW);
  writeFileSync(join(root, 'plugins', 'installed_plugins.json'), JSON.stringify({ version: 2, plugins: { 'nxy@nxy-dev': [{ scope: 'user', installPath: join(plugin, '0.2.1'), version: '0.2.1' }] } }));
  assert.equal(runShim(shimSource(join(plugin, '0.2.1'))), 'v0.2.1', 'hosts/claude-code layout resolves too');
});
