// @ts-check
/**
 * Hook-level golden of the 1.x PreToolUse Bash hook: stdin JSON -> stdout, with a stub rtk
 * (scripts/golden/rtkstub) answering from a per-case table. Needs `go` to build the stub.
 */
import { spawnSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, realpathSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, delimiter, join } from 'node:path';
import { ROOT, loadCorpus } from './lib.mjs';

export const HOOK_AREA = 'hook-pretooluse-bash';
const HOOK = join(ROOT, 'hosts', 'claude-code', 'hooks', 'pretooluse-bash.mjs');

export function haveGo() {
  try {
    return spawnSync('go', ['version'], { encoding: 'utf8' }).status === 0;
  } catch {
    return false;
  }
}

/** @param {string} p @param {any} data */
function writeJson(p, data) {
  mkdirSync(dirname(p), { recursive: true });
  writeFileSync(p, JSON.stringify(data), 'utf8');
}

/**
 * @returns {{cases: any[], error?: string}} golden cases (`{name, input, output: {stdout}}`) or why they could not be built
 */
export function generateHookCases() {
  if (!haveGo()) return { cases: [], error: 'go not available' };
  const root = realpathSync(mkdtempSync(join(tmpdir(), 'nxy-golden-hook-')));
  try {
    const binDir = join(root, 'bin');
    const exe = join(binDir, process.platform === 'win32' ? 'rtk.exe' : 'rtk');
    const b = spawnSync('go', ['build', '-o', exe, './scripts/golden/rtkstub'], { cwd: ROOT, encoding: 'utf8' });
    if (b.status !== 0) return { cases: [], error: `go build rtkstub failed: ${b.stderr || b.stdout}` };
    const stubFwd = exe.replace(/\\/g, '/');

    /** @type {any[]} */
    const out = [];
    /** @type {any[]} */
    const corpus = loadCorpus('hook-pretooluse-bash');
    corpus.forEach((c, i) => {
      const dir = join(root, `c${i}`);
      const home = join(dir, 'home');
      const nxyhome = join(dir, 'nxyhome');
      const project = join(dir, 'project');
      mkdirSync(home, { recursive: true });
      mkdirSync(project, { recursive: true });
      if (c.settings?.user) writeJson(join(home, '.claude', 'settings.json'), c.settings.user);
      if (c.settings?.project) writeJson(join(project, '.claude', 'settings.json'), c.settings.project);
      if (c.settings?.local) writeJson(join(project, '.claude', 'settings.local.json'), c.settings.local);
      if (c.userConfig) writeJson(join(nxyhome, 'config.json'), c.userConfig);
      if (c.projectConfig) writeJson(join(project, '.nxy', 'config.json'), c.projectConfig);
      const eng = c.engine || { engine: 'rtk', rtkPath: 'rtk', rtkHookDetected: false };
      const isRtk = eng.engine === 'rtk';
      writeJson(join(nxyhome, 'cache', 'engine.json'), {
        engine: eng.engine,
        rtkPath: isRtk ? (eng.rtkPath === 'abs' ? stubFwd : eng.rtkPath) : null,
        rtkVersion: isRtk ? '0.0.0-golden' : null,
        rtkHookDetected: !!eng.rtkHookDetected,
        rgAvailable: true,
        rgInstalledAt: null,
        reason: isRtk ? 'rtk-found' : 'config',
        resolvedAt: Date.now(),
      });
      const tablePath = join(dir, 'rtk-table.json');
      writeJson(tablePath, c.rtk || {});

      /** @type {Record<string, string>} */
      const env = {};
      // Windows env names are case-insensitive: keep exactly one spelling of each, or the stub dir may lose to a second PATH
      for (const k of ['SystemRoot', 'PATHEXT', 'ComSpec', 'TEMP', 'TMP', 'TMPDIR', 'windir']) {
        if (process.env[k] !== undefined) env[k] = /** @type {string} */ (process.env[k]);
      }
      env.PATH = `${binDir}${delimiter}${process.env.PATH || ''}`;
      Object.assign(env, {
        HOME: home,
        USERPROFILE: home,
        NXY_HOME: nxyhome,
        LOCALAPPDATA: join(dir, 'localappdata'),
        XDG_DATA_HOME: join(dir, 'xdgdata'),
        CLAUDE_PROJECT_DIR: project,
        NXY_GOLDEN_RTK_TABLE: tablePath,
        ...(c.env || {}),
      });
      const r = spawnSync(process.execPath, [HOOK], {
        input: JSON.stringify(c.payload), encoding: 'utf8', env, cwd: project, timeout: 20000,
      });
      if (r.error || r.status !== 0) throw new Error(`hook failed for ${c.name}: ${r.error || r.stderr}`);
      const stdout = (r.stdout || '').split(stubFwd).join('/golden/rtk');
      out.push({ name: c.name, input: c, output: { stdout } });
    });
    return { cases: out };
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
}
