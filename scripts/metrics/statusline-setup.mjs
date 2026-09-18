#!/usr/bin/env node
// @ts-check
/**
 * `/nxy:statusline` — prints the `statusLine` snippet for ~/.claude/settings.json.
 * With `--apply` it merges the snippet into the user settings (backup first). Claude Code
 * only reads the statusline setting from user/project settings, never from a plugin, so
 * this one step is manual by design.
 */
import { copyFileSync, existsSync, readFileSync, writeFileSync } from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';
import { parseArgs } from '../lib/format.mjs';
import { PLUGIN_ROOT } from '../lib/paths.mjs';

const { opts } = parseArgs(process.argv.slice(2));
const script = join(PLUGIN_ROOT, 'scripts', 'metrics', 'statusline.mjs').replace(/\\/g, '/');
const snippet = { statusLine: { type: 'command', command: `node "${script}"`, padding: 0 } };
const settingsPath = join(homedir(), '.claude', 'settings.json');

if (!opts.apply) {
  console.log('Add this to ~/.claude/settings.json (or run `/nxy:statusline --apply` to merge it for you):');
  console.log(JSON.stringify(snippet, null, 2));
  if (existsSync(settingsPath)) {
    try {
      const cur = JSON.parse(readFileSync(settingsPath, 'utf8'));
      if (cur.statusLine) console.log(`\nCurrent statusLine: ${JSON.stringify(cur.statusLine)} (will be replaced)`);
    } catch {
      /* ignore */
    }
  }
  console.log('\nRestart Claude Code after the change.');
} else {
  let cur = {};
  if (existsSync(settingsPath)) {
    cur = JSON.parse(readFileSync(settingsPath, 'utf8')); // throws on broken JSON: better than overwriting it
    const backup = `${settingsPath}.bak-${Date.now()}`;
    copyFileSync(settingsPath, backup);
    console.log(`backup: ${backup}`);
  }
  writeFileSync(settingsPath, JSON.stringify({ ...cur, ...snippet }, null, 2) + '\n');
  console.log(`statusLine written to ${settingsPath}. Restart Claude Code to see it.`);
}
