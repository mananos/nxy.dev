// @ts-check
/**
 * Classifies a search-like tool call (Grep, Glob, Read, or a console `rg`/`grep -r`/`find`/...) as
 * broad (it can pull a lot into context) or targeted. Pure string work: the only outside knowledge,
 * "is this path a file?", comes from the injected `isFile` callback. Never throws.
 */
import { splitTopLevel } from './shell.mjs';
import { splitLines, tokenize } from './shellwrites.mjs';

/** Fixed first words of the planner nudge, so metrics can find it in transcripts. */
export const LOCATE_NUDGE_MARK = 'nxy locate nudge:';

/** True when the command runs the locate entry. */
export function isLocateCommand(cmd) {
  return /locate\.mjs/.test(String(cmd ?? ''));
}

/** @typedef {{kind: 'grep'|'glob'|'find'|'read'|null, broad: boolean, target: string, pattern: string|null, hasPath?: boolean}} SearchClass */

const NONE = () => /** @type {SearchClass} */ ({ kind: null, broad: false, target: '', pattern: null });

const WILD = /[*?[]/;

/** @param {(p: string) => boolean} isFile */
function safeIsFile(isFile, p) {
  try {
    return !WILD.test(p) && !!isFile(p);
  } catch {
    return false;
  }
}

/** Broad unless every path is a file. No paths = the whole working tree = broad. */
function pathsBroad(paths, isFile) {
  if (!paths.length) return true;
  return !paths.every((p) => safeIsFile(isFile, p));
}

/** Short and long options that take a separate value, per tool. */
const RG_VALUE = new Set(['-g', '--glob', '-t', '--type', '-T', '--type-not', '-A', '-B', '-C', '-m', '--max-count', '-j', '--threads', '-M', '--max-columns', '--iglob', '-r', '--replace', '-d', '--max-depth', '--sort', '--sortr', '-f', '--file', '--type-add', '--context']);
const GREP_VALUE = new Set(['-m', '--max-count', '-A', '-B', '-C', '--include', '--exclude', '--exclude-dir', '--include-dir', '-f', '--file', '--context', '--label', '-d', '--directories', '-D', '--devices']);
const FD_VALUE = new Set(['-e', '--extension', '-t', '--type', '-d', '--max-depth', '-E', '--exclude', '-x', '--exec', '-X', '--exec-batch', '-j', '--threads', '-c', '--color', '--min-depth', '-S', '--size', '--changed-within', '--changed-before', '-o', '--owner', '-g', '--glob']);

/**
 * Splits args into flags seen and positionals. `-e`/`--regexp` consume the pattern.
 * @param {string[]} args
 * @param {Set<string>} valueFlags
 */
function parseArgs(args, valueFlags) {
  /** @type {string[]} */ const pos = [];
  /** @type {string[]} */ const flags = [];
  let pattern = /** @type {string|null} */ (null);
  for (let i = 0; i < args.length; i++) {
    const a = args[i];
    if (a === '--') {
      pos.push(...args.slice(i + 1));
      break;
    }
    if (a === '-e' || a === '--regexp') {
      if (pattern === null) pattern = args[i + 1] ?? '';
      i++;
      flags.push('-e');
    } else if (a.startsWith('--regexp=')) {
      if (pattern === null) pattern = a.slice('--regexp='.length);
      flags.push('-e');
    } else if (a.startsWith('--')) {
      flags.push(a.split('=')[0]);
      if (!a.includes('=') && valueFlags.has(a)) i++;
    } else if (a.length > 1 && a[0] === '-') {
      flags.push(a);
      // A value flag may sit at the end of a cluster (`-nA 2`), or be attached (`-A2`: no skip).
      const last = '-' + a[a.length - 1];
      if (a.length === 2 ? valueFlags.has(a) : valueFlags.has(last) && /^-[A-Za-z]+$/.test(a)) i++;
    } else pos.push(a);
  }
  return { pos, flags, pattern };
}

/**
 * @param {string} name lowercased command name
 * @param {string[]} args
 * @param {string} tool
 * @param {(p: string) => boolean} isFile
 * @returns {SearchClass}
 */
function classifyCommand(name, args, tool, isFile) {
  const ps = tool === 'PowerShell';
  if (name === 'rg' || name === 'ripgrep') {
    const { pos, pattern } = parseArgs(args, RG_VALUE);
    const pat = pattern ?? pos.shift() ?? null;
    return { kind: 'grep', broad: pathsBroad(pos, isFile), target: pos[0] ?? '.', pattern: pat, hasPath: pos.length > 0 };
  }
  if (name === 'git') {
    const rest = args.filter((a, i) => !(a === '-C' || args[i - 1] === '-C'));
    const sub = rest.find((a) => !a.startsWith('-'));
    if (sub !== 'grep') return NONE();
    const after = args.slice(args.indexOf('grep') + 1);
    const { pos, pattern } = parseArgs(after, GREP_VALUE);
    const pat = pattern ?? pos.shift() ?? null;
    const dd = pos.indexOf('--');
    const paths = dd >= 0 ? pos.slice(dd + 1) : pos;
    return { kind: 'grep', broad: pathsBroad(paths, isFile), target: paths[0] ?? '.', pattern: pat, hasPath: paths.length > 0 };
  }
  if (name === 'grep' || name === 'egrep' || name === 'fgrep') {
    const { pos, flags, pattern } = parseArgs(args, GREP_VALUE);
    const recursive = flags.some((f) => f === '--recursive' || f === '--include' || f === '--dereference-recursive' || (/^-[A-Za-z]+$/.test(f) && /[rR]/.test(f)));
    if (!recursive) return NONE();
    const pat = pattern ?? pos.shift() ?? null;
    return { kind: 'grep', broad: pathsBroad(pos, isFile), target: pos[0] ?? '.', pattern: pat, hasPath: pos.length > 0 };
  }
  if (name === 'find') {
    const start = [];
    for (const a of args) {
      if (a.startsWith('-') || a === '(' || a === '!') break;
      start.push(a);
    }
    const ix = args.findIndex((a) => /^-(i?name|i?path|i?regex)$/.test(a));
    return { kind: 'find', broad: pathsBroad(start, isFile), target: start[0] ?? '.', pattern: ix >= 0 ? (args[ix + 1] ?? null) : null };
  }
  if (name === 'fd' || name === 'fdfind') {
    const { pos } = parseArgs(args, FD_VALUE);
    const pat = pos.shift() ?? null;
    return { kind: 'find', broad: pathsBroad(pos, isFile), target: pos[0] ?? '.', pattern: pat };
  }
  if (name === 'get-childitem' || name === 'gci' || (ps && (name === 'ls' || name === 'dir'))) {
    if (!args.some((a) => /^-r(ec\w*)?$/i.test(a))) return NONE();
    const pi = args.findIndex((a) => /^-(literal)?path$/i.test(a));
    const positional = args.filter((a, i) => !a.startsWith('-') && !/^-(filter|include|exclude|depth|path|literalpath)$/i.test(args[i - 1] ?? ''));
    const target = pi >= 0 ? (args[pi + 1] ?? '.') : (positional[0] ?? '.');
    return { kind: 'find', broad: pathsBroad([target], isFile), target, pattern: null };
  }
  if (name === 'select-string' || name === 'sls') {
    const pi = args.findIndex((a) => /^-(literal)?path$/i.test(a));
    const pati = args.findIndex((a) => /^-pattern$/i.test(a));
    const positional = args.filter((a, i) => !a.startsWith('-') && !/^-\w+$/.test(args[i - 1] ?? ''));
    const pattern = pati >= 0 ? (args[pati + 1] ?? null) : (positional.shift() ?? null);
    const target = pi >= 0 ? (args[pi + 1] ?? '') : (positional[0] ?? '');
    // No path means it reads the pipeline: not a tree search.
    const broad = !!target && pathsBroad([target], isFile);
    return { kind: 'grep', broad, target: target || '.', pattern, hasPath: !!target };
  }
  return NONE();
}

const SKIP_PREFIX = new Set(['rtk', 'sudo', 'command', 'time']);

/**
 * @param {string} command
 * @param {string} tool 'Bash' | 'PowerShell'
 * @param {(p: string) => boolean} isFile
 * @returns {SearchClass}
 */
function classifyShell(command, tool, isFile) {
  /** @type {SearchClass|null} */ let firstTargeted = null;
  for (const line of splitLines(command)) {
    let from = 0;
    for (const seg of splitTopLevel(line)) {
      const at = line.indexOf(seg, from);
      from = at < 0 ? from : at + seg.length;
      // A segment right after a single `|` reads the pipe, not the tree.
      const before = at > 0 ? line.slice(0, at).trimEnd() : '';
      const piped = before.endsWith('|') && !before.endsWith('||');
      const posix = tool !== 'PowerShell' && !/[A-Za-z]:\\/.test(seg);
      const toks = tokenize(seg, posix).filter((t) => !t.op).map((t) => t.text);
      let i = 0;
      while (i < toks.length && (/^[A-Za-z_]\w*=/.test(toks[i]) || SKIP_PREFIX.has(toks[i]))) i++;
      if (i >= toks.length) continue;
      const name = toks[i].replace(/^.*[\\/]/, '').replace(/\.exe$/i, '').toLowerCase();
      const res = classifyCommand(name, toks.slice(i + 1), tool, isFile);
      if (!res.kind) continue;
      // `x | rg foo` reads stdin unless a path was given.
      if (piped && res.kind === 'grep' && !res.hasPath && name !== 'select-string' && name !== 'sls') continue;
      if (res.broad) return res;
      firstTargeted ??= res;
    }
  }
  return firstTargeted ?? NONE();
}

/**
 * @param {string} tool Grep | Glob | Read | Bash | PowerShell
 * @param {any} toolInput
 * @param {{isFile?: (p: string) => boolean}} [opts]
 * @returns {SearchClass}
 */
export function classifySearch(tool, toolInput, opts = {}) {
  try {
    const isFile = opts.isFile ?? (() => false);
    const inp = toolInput && typeof toolInput === 'object' ? toolInput : {};
    if (tool === 'Grep') {
      const path = typeof inp.path === 'string' && inp.path ? inp.path : '';
      return { kind: 'grep', broad: !path || !safeIsFile(isFile, path), target: path || '.', pattern: inp.pattern ?? null };
    }
    if (tool === 'Glob') {
      return { kind: 'glob', broad: true, target: inp.path || '.', pattern: inp.pattern ?? null };
    }
    if (tool === 'Read') {
      const targeted = inp.offset != null || inp.limit != null || inp.pages != null;
      return { kind: 'read', broad: !targeted, target: inp.file_path ?? '', pattern: null };
    }
    if ((tool === 'Bash' || tool === 'PowerShell') && typeof inp.command === 'string') {
      return classifyShell(inp.command, tool, isFile);
    }
  } catch {
    /* fall through */
  }
  return NONE();
}
