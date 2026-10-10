// @ts-check
/**
 * The project declares one version everywhere. Fails if one file is bumped and another forgotten.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const EXPECTED = '1.1.6';
const json = (/** @type {string[]} */ ...parts) => JSON.parse(readFileSync(join(ROOT, ...parts), 'utf8'));

test('every manifest carries the same version', () => {
  const lock = json('package-lock.json');
  const market = json('.claude-plugin', 'marketplace.json');
  const found = {
    'package.json': json('package.json').version,
    'package-lock.json': lock.version,
    'package-lock.json packages[""]': lock.packages[''].version,
    'plugin.json': json('.claude-plugin', 'plugin.json').version,
    'marketplace.json': market.plugins[0].version,
  };
  for (const [file, version] of Object.entries(found)) assert.equal(version, EXPECTED, file);
});

test('README "Versión actual" row shows the version', () => {
  const readme = readFileSync(join(ROOT, 'README.md'), 'utf8');
  const row = readme.split(/\r?\n/).find((l) => l.includes('Versión actual'));
  assert.ok(row, 'row present');
  assert.ok(row.includes(`\`${EXPECTED}\``), row);
});
