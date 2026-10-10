// Install plans for the external tools nxy leans on (rtk, rg, codegraph).
// Pure: planning never touches the machine. Only runPlan spawns, and only with the
// spawn it is given. Commands and sources are the same ones the README documents.
import { spawnSync } from 'node:child_process';
import { detectRtkHook } from './filter/engine.mjs';

export const TOOLS = ['rtk', 'rg', 'codegraph'];

const RTK_SH = 'https://raw.githubusercontent.com/rtk-ai/rtk/refs/heads/master/install.sh';
const CG_SH = 'https://raw.githubusercontent.com/colbymchenry/codegraph/main/install.sh';
const CG_PS1 = 'https://raw.githubusercontent.com/colbymchenry/codegraph/main/install.ps1';

/** True when rtk's own hook is wired in any of the given settings objects. */
export function detectRtkConflict(settingsObjects) {
  return detectRtkHook(settingsObjects);
}

/**
 * True when one parsed config object declares an mcpServers.codegraph entry.
 * @param {any} obj parsed settings / ~/.claude.json / .mcp.json (or a projects[cwd] entry)
 */
export function detectCodegraphMcp(obj) {
  return !!obj?.mcpServers && typeof obj.mcpServers === 'object' && 'codegraph' in obj.mcpServers;
}

/**
 * True when `codegraph install` left its wiring: an mcpServers.codegraph entry or a hook
 * whose command names codegraph.
 * @param {Array<any>} settingsObjects parsed settings.json files
 * @param {Array<any>} mcpObjects parsed ~/.claude.json / .mcp.json files
 */
export function detectCodegraphWiring(settingsObjects, mcpObjects) {
  for (const o of [...(settingsObjects || []), ...(mcpObjects || [])]) {
    if (detectCodegraphMcp(o)) return true;
  }
  for (const s of settingsObjects || []) {
    const hooks = s?.hooks;
    if (!hooks || typeof hooks !== 'object') continue;
    for (const groups of Object.values(hooks)) {
      if (!Array.isArray(groups)) continue;
      for (const g of groups) {
        for (const h of g?.hooks || []) {
          if (typeof h?.command === 'string' && /codegraph/i.test(h.command)) return true;
        }
      }
    }
  }
  return false;
}

/**
 * Package manager for the platform, or null.
 * @param {string} platform process.platform value
 * @param {(bin: string) => boolean} has whether a binary is on PATH
 */
export function packageManager(platform, has) {
  if (platform === 'win32') return has('winget') ? 'winget' : null;
  if (platform === 'darwin') return has('brew') ? 'brew' : null;
  for (const pm of ['apt-get', 'dnf', 'pacman', 'brew']) if (has(pm)) return pm;
  return null;
}

// The script is downloaded to a temp file first so a failed curl fails the step (a plain
// `curl | sh` exits with sh's status). The displayed command is the executed one.
const shStep = (url, source) => {
  const script = `t=$(mktemp) && curl -fsSL ${url} -o "$t" && sh "$t"; r=$?; rm -f "$t"; exit $r`;
  return { display: script, file: 'sh', args: ['-c', script], source, needsSudo: false };
};

const quoteArg = (a) => (/\s/.test(a) ? `"${a}"` : a);
const cmdStep = (file, args, source) => ({ display: [file, ...args].map(quoteArg).join(' '), file, args, source, needsSudo: false });

function sudoStep(file, args, source) {
  return {
    display: ['sudo', '-n', file, ...args].join(' '),
    file: 'sudo',
    args: ['-n', file, ...args],
    source,
    needsSudo: true,
  };
}

function rgSteps(os, pm, found) {
  const src = 'README.md';
  if (os === 'win32') {
    return [cmdStep('winget', [found ? 'upgrade' : 'install', 'BurntSushi.ripgrep.MSVC'], src)];
  }
  if (pm === 'brew') return [cmdStep('brew', [found ? 'upgrade' : 'install', 'ripgrep'], src)];
  if (pm === 'apt-get') return [sudoStep('apt-get', ['install', '-y', 'ripgrep'], src)];
  if (pm === 'dnf') return [sudoStep('dnf', ['install', '-y', 'ripgrep'], src)];
  if (pm === 'pacman') return [sudoStep('pacman', ['-S', '--noconfirm', 'ripgrep'], src)];
  return [];
}

function stepsFor(tool, os, pm, found) {
  if (tool === 'rtk') {
    if (os === 'win32') return [cmdStep('winget', [found ? 'upgrade' : 'install', 'rtk-ai.rtk'], 'README.md')];
    if (os === 'darwin') return [cmdStep('brew', [found ? 'upgrade' : 'install', 'rtk'], 'README.md')];
    return [shStep(RTK_SH, 'github.com/rtk-ai/rtk install.sh')];
  }
  if (tool === 'rg') return rgSteps(os, pm, found);
  if (tool === 'codegraph') {
    if (os === 'win32') {
      return [
        {
          // Stop makes a failed download a terminating error, hence a non-zero exit.
          display: `powershell -NoProfile -Command "$ErrorActionPreference='Stop'; irm ${CG_PS1} | iex"`,
          file: 'powershell',
          args: ['-NoProfile', '-Command', `$ErrorActionPreference='Stop'; irm ${CG_PS1} | iex`],
          source: 'github.com/colbymchenry/codegraph install.ps1',
          needsSudo: false,
        },
      ];
    }
    return [shStep(CG_SH, 'github.com/colbymchenry/codegraph install.sh')];
  }
  return [];
}

/**
 * Build the plan for one tool. `conflict` is true/false for rtk (its own hook) and for
 * codegraph may be 'mcp' (registered MCP server) or 'hooks' / true (loose settings hooks).
 * @param {string} tool one of TOOLS
 * `rtkPath` is the resolved rtk binary; `hookScopes` says where rtk's hook lives ('user' and/or
 * 'project'); `mcpScope` is where the codegraph MCP was found ('user' | 'project' | 'local').
 * @param {{platform: string, pm?: string|null, found?: boolean, conflict?: boolean|string, rtkPath?: string|null, hookScopes?: string[], mcpScope?: string|null}} opts
 */
export function installPlan(tool, opts) {
  const { platform: os, pm = null, found = false, conflict = false, hookScopes = ['user'], mcpScope = null } = opts;
  const rtkPath = opts.rtkPath === undefined ? (found ? 'rtk' : null) : opts.rtkPath;
  const steps = stepsFor(tool, os, pm, found);
  /** @type {Array<{display: string, file: string, args: string[], source: string, needsSudo: boolean}>} */
  const removes = [];
  /** @type {string[]} */
  const manual = [];
  if (conflict) {
    if (tool === 'rtk') {
      // `rtk init --help` lists --uninstall ("Remove RTK artifacts"); -g targets the global settings only.
      if (hookScopes.includes('user')) {
        if (rtkPath) removes.push(cmdStep(rtkPath, ['init', '-g', '--uninstall'], 'rtk init --help'));
        else manual.push('rtk no se encontró, así que no se puede quitar su hook: una vez instalado, corré `rtk init -g --uninstall` en una terminal.');
      }
      if (hookScopes.includes('project')) {
        manual.push('El hook de rtk está en los settings del proyecto (.claude/settings*.json): quitalo a mano (-g no lo toca; nxy no edita settings ajenos).');
      }
    } else if (tool === 'codegraph') {
      if (conflict === 'mcp') {
        if (mcpScope) removes.push(cmdStep('claude', ['mcp', 'remove', 'codegraph', '-s', mcpScope], 'claude mcp'));
        else manual.push('Quitá a mano el servidor MCP codegraph (`claude mcp remove codegraph`, o de tus settings): no se pudo saber en qué alcance está.');
      } else {
        manual.push('Quitá a mano de tus settings el hook cuyo comando nombra codegraph (nxy no edita settings ajenos).');
      }
    }
  }
  for (const s of steps) {
    if (s.needsSudo) manual.push(['sudo', s.file === 'sudo' ? s.args.slice(1).join(' ') : s.display].join(' '));
  }
  return {
    tool,
    action: found ? 'reinstall' : 'install',
    os,
    steps,
    removes,
    manual,
  };
}

/**
 * Readable lines: exact command and source per step; sudo steps are flagged. The "a mano"
 * notes are only included with `{manual: true}` (they are not part of what will run).
 * @param {ReturnType<typeof installPlan>} plan
 * @param {{manual?: boolean}} [opts]
 */
export function describePlan(plan, opts = {}) {
  const lines = [];
  for (const r of plan.removes) lines.push(`quita:   ${r.display}   (${r.source})`);
  if (!plan.steps.length) lines.push(`${plan.action}: sin gestor de paquetes conocido para ${plan.tool} en ${plan.os}`);
  for (const s of plan.steps) {
    lines.push(`${plan.action}: ${s.display}   (fuente: ${s.source})${s.needsSudo ? '   [sudo -n: sin contraseña en caché falla]' : ''}`);
  }
  if (opts.manual) for (const m of plan.manual) lines.push(`a mano:  ${m}`);
  return lines;
}

// sudo -n output when it would need a password or a terminal (English and Spanish locales).
const SUDO_NO_TTY = /password is required|terminal is required|no tty present|se requiere una (contrase|terminal)|requiere.{0,20}(contrase|terminal)/i;

const clip =(s, n = 1500) => (s.length > n ? '…' + s.slice(-n) : s);

/**
 * Run removes then steps in order, stop at the first failure.
 * @param {ReturnType<typeof installPlan>} plan
 * @param {{spawn?: Function, timeoutMs?: number}} [opts]
 */
export function runPlan(plan, opts = {}) {
  const spawn = opts.spawn || spawnSync;
  const timeout = opts.timeoutMs ?? 300000;
  /** @type {string[]} */
  const ran = [];
  let tail = '';
  for (const s of [...plan.removes, ...plan.steps]) {
    ran.push(s.display);
    const r = spawn(s.file, s.args, { encoding: 'utf8', timeout, windowsHide: true });
    tail = clip(`${r?.stdout || ''}${r?.stderr || ''}${r?.error ? String(r.error.message || r.error) : ''}`.trim());
    if (r?.error || r?.status !== 0) {
      if (s.needsSudo && SUDO_NO_TTY.test(tail)) {
        tail = clip(`${tail}\nsudo no pudo pedir contraseña sin terminal. Pegá en una terminal: ${plan.manual.join(' ; ')}`.trim());
      }
      return { ok: false, ran, failedAt: s.display, tail, manual: plan.manual };
    }
  }
  return { ok: true, ran, failedAt: null, tail, manual: [] };
}
