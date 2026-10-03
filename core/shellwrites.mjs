// @ts-check
/**
 * Best-effort list of the files a console command is about to write (redirections, `sed -i`,
 * `tee`, `cp`/`mv`, ...), so the baseline can copy them BEFORE the command runs. It is a parser,
 * not a guarantee: scripts, `xargs`, `python -c`, variables and globs are not recognised (the
 * git list at review time covers those). Pure string work on one command; the only I/O is a
 * `statSync` on a `cp`/`mv` destination to tell a directory from a file. Never throws.
 */
import { statSync } from 'node:fs';
import { isAbsolute, join, resolve } from 'node:path';
import { splitTopLevel } from './shell.mjs';
import { toNativePath } from './paths.mjs';

/** @typedef {{text: string, op?: boolean, dup?: boolean}} Tok */

/** Removes heredoc bodies: from the line after `<<WORD` to the line equal to WORD. */
function cutHeredocs(command) {
  const out = [];
  const lines = command.split('\n');
  for (let i = 0; i < lines.length; i++) {
    out.push(lines[i]);
    const m = /(?<!<)<<-?\s*(['"]?)([A-Za-z_][A-Za-z0-9_]*)\1/.exec(lines[i]);
    if (!m) continue;
    while (i + 1 < lines.length) {
      i++;
      if (lines[i].trim() === m[2]) break;
    }
  }
  return out;
}

/** Splits on newlines that are outside quotes (a quoted multi-line argument stays in one piece). */
function splitLines(text) {
  const out = [];
  let start = 0;
  let q = '';
  for (let i = 0; i < text.length; i++) {
    const ch = text[i];
    if (q === "'") {
      if (ch === "'") q = '';
    } else if (ch === '\\' && q !== "'") i++;
    else if (q === '"') {
      if (ch === '"') q = '';
    } else if (ch === "'" || ch === '"') q = ch;
    else if (ch === '\n') {
      out.push(text.slice(start, i));
      start = i + 1;
    }
  }
  out.push(text.slice(start));
  return out;
}

/**
 * Splits a segment into words and redirection operators, honouring quotes. Operators are
 * `>`-family (with an optional fd or `&` before) and `<`-family; the word after one is its target.
 * @param {string} seg
 * @param {boolean} posix backslash escapes (false for PowerShell)
 * @returns {Tok[]}
 */
function tokenize(seg, posix) {
  /** @type {Tok[]} */
  const toks = [];
  let cur = '';
  let has = false;
  const flush = () => {
    if (has) toks.push({ text: cur });
    cur = '';
    has = false;
  };
  for (let i = 0; i < seg.length; i++) {
    const ch = seg[i];
    if (ch === "'") {
      has = true;
      const j = seg.indexOf("'", i + 1);
      const end = j < 0 ? seg.length : j;
      cur += seg.slice(i + 1, end);
      i = end;
    } else if (ch === '"') {
      has = true;
      i++;
      for (; i < seg.length && seg[i] !== '"'; i++) {
        if (posix && seg[i] === '\\' && i + 1 < seg.length) i++;
        cur += seg[i];
      }
    } else if (posix && ch === '\\' && i + 1 < seg.length) {
      has = true;
      cur += seg[++i];
    } else if (ch === '>' || ch === '<') {
      // `2>`, `&>`: the fd or `&` belongs to the operator, not to a word.
      if (has && (/^\d+$/.test(cur) || cur === '&')) {
        cur = '';
        has = false;
      } else flush();
      let op = ch;
      while (seg[i + 1] === ch || (ch === '>' && seg[i + 1] === '|')) op += seg[++i];
      if (ch === '>' && seg[i + 1] === '&') {
        // `2>&1`, `>&2`: a duplicate of a descriptor, not a file.
        i++;
        while (i + 1 < seg.length && !/\s/.test(seg[i + 1])) i++;
        toks.push({ text: op, op: true, dup: true });
      } else toks.push({ text: op, op: true, dup: ch === '<' });
    } else if (/\s/.test(ch)) flush();
    else {
      has = true;
      cur += ch;
    }
  }
  flush();
  return toks;
}

// `~` expands only at the start of a word; inside one it is literal (8.3 names: `C:\Users\RUNNER~1`).
const UNRESOLVABLE = /[$`*?{]|^~/;
const base = (p) => p.split(/[\\/]/).filter(Boolean).pop() || p;

/**
 * @param {string} command
 * @param {string} cwd directory the command starts in
 * @param {string} [tool] 'Bash' (default) or 'PowerShell'
 * @param {{content?: boolean}} [opts] `content`: only writes that rewrite a file's content
 *   (redirections, `sed -i`, `tee`...), not `cp`/`mv`
 * @returns {string[]} absolute paths, de-duplicated
 */
export function writeTargets(command, cwd, tool = 'Bash', opts = {}) {
  try {
    const ps = tool === 'PowerShell';
    /** @type {Set<string>} */
    const out = new Set();
    let dir = resolve(toNativePath(cwd));
    let known = true;

    /** @param {string} t @param {string} at @param {boolean} atKnown */
    const add = (t, at, atKnown) => {
      if (!t || UNRESOLVABLE.test(t) || /^\/dev\//.test(t) || t === 'NUL' || t === 'nul' || t.startsWith('&')) return;
      const native = toNativePath(t);
      if (!isAbsolute(native) && !atKnown) return;
      out.add(resolve(at, native));
    };

    const lines = splitLines(cutHeredocs(command).join('\n')).flatMap((l) => splitTopLevel(l));
    for (const seg of lines) {
      const toks = tokenize(seg, !ps);
      // Redirections apply to any command.
      /** @type {string[]} */
      const words = [];
      for (let i = 0; i < toks.length; i++) {
        const t = toks[i];
        if (t.op) {
          const target = toks[i + 1];
          if (target && !target.op) i++;
          if (!t.dup && t.text.startsWith('>') && target && !target.op) add(target.text, dir, known);
        } else words.push(t.text);
      }
      let k = 0;
      while (k < words.length) {
        const w = words[k];
        if (/^[A-Za-z_][A-Za-z0-9_]*=/.test(w) || w === 'sudo' || w === 'env' || w === 'rtk') k++;
        else break;
      }
      const name = (words[k] || '').replace(/\.exe$/i, '');
      const args = words.slice(k + 1);

      if (name === 'cd' || name === 'Set-Location') {
        const a = args[0];
        if (!a || a === '-' || UNRESOLVABLE.test(a)) known = false;
        else dir = resolve(dir, toNativePath(a));
        continue;
      }
      const lower = name.toLowerCase();
      if (ps) {
        if (lower === 'set-content' || lower === 'add-content' || lower === 'out-file') {
          let p;
          for (let i = 0; i < args.length; i++) {
            if (/^-(path|filepath|literalpath)$/i.test(args[i])) {
              p = args[i + 1];
              break;
            }
            if (!args[i].startsWith('-')) {
              p = args[i];
              break;
            }
          }
          if (p) add(p, dir, known);
        }
        continue;
      }

      const operands = (/** @type {string[]} */ a) => a.filter((x) => !x.startsWith('-') || x === '-');
      if (name === 'sed') {
        let inplace = false;
        let script = false;
        /** @type {string[]} */
        const ops = [];
        for (let i = 0; i < args.length; i++) {
          const a = args[i];
          if (a === '-e' || a === '-f') {
            script = true;
            i++;
          } else if (/^--(expression|file)=/.test(a)) script = true;
          else if (a === '--in-place' || a.startsWith('--in-place=')) inplace = true;
          else if (a.startsWith('--')) continue;
          else if (/^-[A-Za-z]*i/.test(a)) inplace = true;
          else if (a.startsWith('-') && a !== '-') continue;
          else ops.push(a);
        }
        if (inplace) for (const f of script ? ops : ops.slice(1)) add(f, dir, known);
      } else if (name === 'perl') {
        let inplace = false;
        let script = false;
        /** @type {string[]} */
        const ops = [];
        for (let i = 0; i < args.length; i++) {
          const a = args[i];
          if (/^-[A-Za-z]+/.test(a) && !a.startsWith('--')) {
            for (const c of a.slice(1)) {
              if (c === 'i') {
                inplace = true;
                break;
              }
              if (c === 'e' || c === 'E') {
                script = true;
                i++;
                break;
              }
              // an option that takes the rest of the cluster as its argument: stop scanning
              if ('MmIFxdDC'.includes(c)) break;
            }
          } else ops.push(a);
        }
        if (inplace) for (const f of script ? ops : ops.slice(1)) add(f, dir, known);
      } else if (name === 'tee') {
        for (const f of operands(args)) add(f, dir, known);
      } else if (name === 'truncate') {
        /** @type {string[]} */
        const ops = [];
        for (let i = 0; i < args.length; i++) {
          if (args[i] === '-s' || args[i] === '-r') i++;
          else if (!args[i].startsWith('-')) ops.push(args[i]);
        }
        for (const f of ops) add(f, dir, known);
      } else if (name === 'dd') {
        for (const a of args) if (a.startsWith('of=')) add(a.slice(3), dir, known);
      } else if ((name === 'cp' || name === 'mv') && !opts.content) {
        /** @type {string[]} */
        const ops = [];
        let tdir = null;
        for (let i = 0; i < args.length; i++) {
          if (args[i] === '-t') tdir = args[++i] ?? null;
          else if (!args[i].startsWith('-')) ops.push(args[i]);
        }
        const srcs = tdir === null ? ops.slice(0, -1) : ops;
        const dest = tdir === null ? ops[ops.length - 1] : tdir;
        if (dest && srcs.length) {
          let isDir = tdir !== null || /[\\/]$/.test(dest);
          if (!isDir && known && !UNRESOLVABLE.test(dest)) {
            try {
              isDir = statSync(resolve(dir, toNativePath(dest))).isDirectory();
            } catch {
              /* does not exist: a file */
            }
          }
          if (isDir || srcs.length > 1) {
            for (const s of srcs) if (!UNRESOLVABLE.test(s)) add(join(dest, base(s)), dir, known);
          } else add(dest, dir, known);
          if (name === 'mv') for (const s of srcs) add(s, dir, known);
        }
      }
    }
    return [...out];
  } catch {
    return [];
  }
}
