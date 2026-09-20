// @ts-check
/**
 * Minimal, dependency-free analysis of a bash command string. It is deliberately
 * conservative: whenever a construct is ambiguous we report it as "unsafe" so the
 * caller leaves the command untouched.
 */

/**
 * Walks the string once, tracking quote state, and calls `visit(ch, i, ctx)` for
 * every character outside single quotes. `ctx.inDouble` tells if inside "...".
 * Returning `false` from `visit` stops the scan.
 */
function scan(cmd, visit) {
  let inSingle = false;
  let inDouble = false;
  for (let i = 0; i < cmd.length; i++) {
    const ch = cmd[i];
    if (ch === '\\' && !inSingle) {
      i++; // skip escaped char
      continue;
    }
    if (ch === "'" && !inDouble) {
      inSingle = !inSingle;
      continue;
    }
    if (ch === '"' && !inSingle) {
      inDouble = !inDouble;
      continue;
    }
    if (inSingle) continue;
    if (visit(ch, i, { inDouble }) === false) return;
  }
}

/**
 * Splits at top-level `&&`, `||`, `;` and `|` (outside quotes). Returns trimmed
 * segments; operators are dropped. `echo a && ls` → ['echo a', 'ls'].
 * @param {string} cmd
 * @returns {string[]}
 */
export function splitTopLevel(cmd) {
  const segments = [];
  let start = 0;
  scan(cmd, (ch, i, { inDouble }) => {
    if (inDouble) return;
    let opLen = 0;
    if (cmd.startsWith('&&', i) || cmd.startsWith('||', i)) opLen = 2;
    else if (ch === ';' || ch === '|') opLen = 1;
    if (opLen) {
      segments.push(cmd.slice(start, i));
      start = i + opLen;
    }
  });
  segments.push(cmd.slice(start));
  return segments.map((s) => s.trim()).filter(Boolean);
}

/** True for `<<` / `<<-` / `<<<` outside quotes. */
export function hasHeredoc(cmd) {
  let found = false;
  scan(cmd, (ch, i, { inDouble }) => {
    if (!inDouble && ch === '<' && cmd[i + 1] === '<') {
      found = true;
      return false;
    }
  });
  return found;
}

/** True for `$(`, backticks or `${` outside single quotes (they expand inside double quotes too). */
export function hasSubstitution(cmd) {
  let found = false;
  scan(cmd, (ch, i) => {
    if (ch === '`' || (ch === '$' && (cmd[i + 1] === '(' || cmd[i + 1] === '{'))) {
      found = true;
      return false;
    }
  });
  return found;
}

/**
 * True for top-level `>`, `>>`, `2>file`, `&>`, `<` (input from file) outside quotes.
 * `2>&1` (merge stderr into stdout) is NOT a redirect for our purposes: nothing leaves the
 * tool's output, and rtk keeps it as is. Neither is any redirect INTO `/dev/null`
 * (`2>/dev/null`, `&>/dev/null`, `> /dev/null`): it discards, it does not write a file, and
 * rtk keeps it verbatim on the segment it wraps.
 */
export function hasRedirect(cmd) {
  let found = false;
  scan(cmd, (ch, i, { inDouble }) => {
    if (inDouble) return;
    if (ch === '>') {
      if (cmd[i - 1] === '2' && cmd.startsWith('>&1', i)) return; // 2>&1
      if (/^>?\s*\/dev\/null(?=\s|$|[;&|)])/.test(cmd.slice(i + 1))) return; // >/dev/null, >>/dev/null, 2> /dev/null
      found = true;
      return false;
    }
    if (ch === '<' && cmd[i + 1] !== '<' && cmd[i - 1] !== '<') {
      found = true;
      return false;
    }
  });
  return found;
}

/**
 * Removes a trailing `| tail -n N` / `| head -n N` (with or without `-n`), the kind of suffix
 * the model adds to cap output. Returns the command unchanged when there is no such suffix.
 * @param {string} cmd
 */
export function stripTrailingLimit(cmd) {
  return cmd.replace(/\s*\|\s*(?:tail|head)(?:\s+-n?\s*\d+|\s+-\d+|\s+--lines[= ]\d+)?\s*$/, '');
}

/** True when the command ends with a background `&` (not `&&`). */
export function isBackgrounded(cmd) {
  const t = cmd.trimEnd();
  return t.endsWith('&') && !t.endsWith('&&');
}

/**
 * First token of a segment, skipping `VAR=value` prefixes and a leading `time`/`nice`.
 * `FOO=1 npm test` → 'npm'. Empty string if none.
 * @param {string} segment
 */
export function firstToken(segment) {
  const tokens = segment.trim().split(/\s+/);
  for (const t of tokens) {
    if (/^[A-Za-z_][A-Za-z0-9_]*=/.test(t)) continue;
    if (t === 'time' || t === 'nice' || t === 'command' || t === 'exec') continue;
    return t.replace(/^\.[\\/]/, '').replace(/\.(exe|cmd|bat)$/i, '');
  }
  return '';
}

/**
 * Leading `VAR=value` assignments of a command (used for the `NXY_RAW=1` escape hatch).
 * @param {string} cmd
 * @returns {Record<string,string>}
 */
export function leadingAssignments(cmd) {
  /** @type {Record<string,string>} */
  const out = {};
  for (const t of cmd.trim().split(/\s+/)) {
    const m = /^([A-Za-z_][A-Za-z0-9_]*)=(.*)$/.exec(t);
    if (!m) break;
    out[m[1]] = m[2];
  }
  return out;
}
