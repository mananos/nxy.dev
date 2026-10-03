// @ts-check
/**
 * Agent prompts: the "edit with Edit/Write, never through the shell" rule cannot be dropped silently,
 * and every agent file carries the frontmatter the plugin needs.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const DIR = join(dirname(fileURLToPath(import.meta.url)), '..', 'agents');
const read = (/** @type {string} */ file) => readFileSync(join(DIR, file), 'utf8');

for (const file of ['implementer.md', 'documenter.md']) {
  test(`${file} edits only with Edit and Write, never through the shell`, () => {
    const rule = read(file)
      .split(/\r?\n/)
      .find((l) => /^\d+\.\s/.test(l) && /Edit and Write/.test(l));
    assert.ok(rule, 'rule line present');
    for (const banned of ['sed', 'awk', 'perl', 'script', 'heredoc']) {
      assert.ok(rule.includes(banned), `rule forbids ${banned}`);
    }
    assert.match(rule, /never/i);
    assert.match(rule, /backslashes/);
  });
}

test('reviewer.md explains (inventory) files and reads ## Coverage', () => {
  const text = read('reviewer.md');
  assert.match(text, /\(inventory\)/);
  assert.match(text, /## Coverage/);
});

test('every agent file has name, description and tools in its frontmatter', () => {
  const files = readdirSync(DIR).filter((f) => f.endsWith('.md'));
  assert.ok(files.length > 0);
  for (const file of files) {
    const match = read(file).match(/^---\r?\n([\s\S]*?)\r?\n---/);
    assert.ok(match, `${file}: frontmatter present`);
    for (const key of ['name', 'description', 'tools']) {
      assert.match(match[1], new RegExp(`^${key}:\\s*\\S`, 'm'), `${file}: ${key}`);
    }
    assert.match(match[1], new RegExp(`^name:\\s*${file.replace(/\.md$/, '')}\\s*$`, 'm'), `${file}: name matches file`);
  }
});
