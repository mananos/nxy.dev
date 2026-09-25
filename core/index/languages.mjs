// @ts-check
/**
 * Symbol extraction by regex, one ruleset per language. No parser, no dependencies, no model:
 * this is the "search without a model" floor — a script builds the map, and only the lines that
 * match a query ever reach a model.
 *
 * The rules are deliberately shallow. A repo map answers "where is X defined, roughly" so that
 * something else (rg for exact text, codegraph for structure, a scout for meaning) can take over.
 * A missed symbol costs a fallback to `rg`; a wrong one costs a bad `path:line`, so the patterns
 * favour precision over recall.
 */

/** @typedef {{kind: string, re: RegExp}} Rule */

const JS_RULES = [
  { kind: 'class', re: /^\s*(?:export\s+(?:default\s+)?)?(?:abstract\s+)?class\s+([A-Za-z_$][\w$]*)/ },
  { kind: 'function', re: /^\s*(?:export\s+(?:default\s+)?)?(?:async\s+)?function\s*\*?\s*([A-Za-z_$][\w$]*)/ },
  { kind: 'const', re: /^\s*(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*(?:async\s*)?(?:\([^)]*\)|[A-Za-z_$][\w$]*)\s*=>/ },
  { kind: 'interface', re: /^\s*(?:export\s+)?interface\s+([A-Za-z_$][\w$]*)/ },
  { kind: 'type', re: /^\s*(?:export\s+)?type\s+([A-Za-z_$][\w$]*)\s*[=<]/ },
  { kind: 'enum', re: /^\s*(?:export\s+)?(?:const\s+)?enum\s+([A-Za-z_$][\w$]*)/ },
  // class members: two-space-ish indent, not a control keyword, followed by a call signature
  { kind: 'method', re: /^\s{2,}(?:(?:public|private|protected|static|readonly|async|get|set|override)\s+)*([A-Za-z_$][\w$]*)\s*\([^)]*\)\s*(?::[^{;]+)?\s*\{/ },
];

const JAVA_RULES = [
  { kind: 'class', re: /^\s*(?:(?:public|private|protected|static|final|abstract|sealed)\s+)*class\s+([A-Za-z_][\w$]*)/ },
  { kind: 'interface', re: /^\s*(?:(?:public|private|protected|static|sealed)\s+)*interface\s+([A-Za-z_][\w$]*)/ },
  { kind: 'enum', re: /^\s*(?:(?:public|private|protected|static)\s+)*enum\s+([A-Za-z_][\w$]*)/ },
  { kind: 'record', re: /^\s*(?:(?:public|private|protected|static|final)\s+)*record\s+([A-Za-z_][\w$]*)/ },
  { kind: 'method', re: /^\s+(?:(?:public|private|protected|static|final|abstract|synchronized|native|default)\s+)+(?:<[^>]+>\s*)?[\w$<>\[\],.?\s]+\s+([A-Za-z_][\w$]*)\s*\([^;]*$/ },
  { kind: 'endpoint', re: /^\s*@(?:Get|Post|Put|Delete|Patch|Request)Mapping\s*\(\s*(?:value\s*=\s*)?"([^"]+)"/ },
];

const SCALA_RULES = [
  { kind: 'object', re: /^\s*(?:(?:private|protected|implicit|case)\s+)*object\s+([A-Za-z_][\w$]*)/ },
  { kind: 'class', re: /^\s*(?:(?:private|protected|abstract|final|sealed|implicit|case)\s+)*class\s+([A-Za-z_][\w$]*)/ },
  { kind: 'trait', re: /^\s*(?:(?:private|protected|sealed)\s+)*trait\s+([A-Za-z_][\w$]*)/ },
  { kind: 'def', re: /^\s*(?:(?:private|protected|override|implicit|final)\s+)*def\s+([A-Za-z_][\w$]*)/ },
];

const PY_RULES = [
  { kind: 'class', re: /^\s*class\s+([A-Za-z_]\w*)/ },
  { kind: 'def', re: /^\s*(?:async\s+)?def\s+([A-Za-z_]\w*)/ },
  // Route decorators carry the name people actually search for ("where is /users/{id} handled").
  // The decorator sits above the def, so both lines get indexed and either one finds the handler.
  { kind: 'endpoint', re: /^\s*@(?:[\w.]+)\.(?:route|get|post|put|delete|patch|websocket)\s*\(\s*['"]([^'"]+)['"]/ },
  // Module-level constants: config and registry values live here and are searched by name.
  { kind: 'const', re: /^([A-Z][A-Z0-9_]{2,})\s*(?::[^=]+)?=/ },
];

const GO_RULES = [
  { kind: 'func', re: /^func\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)/ },
  { kind: 'type', re: /^type\s+([A-Za-z_]\w*)/ },
];

const CS_RULES = [
  { kind: 'class', re: /^\s*(?:(?:public|private|protected|internal|static|sealed|abstract|partial)\s+)*class\s+([A-Za-z_]\w*)/ },
  { kind: 'interface', re: /^\s*(?:(?:public|private|protected|internal)\s+)*interface\s+([A-Za-z_]\w*)/ },
  { kind: 'method', re: /^\s+(?:(?:public|private|protected|internal|static|virtual|override|async|sealed)\s+)+[\w<>\[\],.?\s]+\s+([A-Za-z_]\w*)\s*\([^;]*$/ },
];

/** Extension → rules. Anything not listed is indexed by path only. */
const BY_EXT = {
  '.js': JS_RULES, '.mjs': JS_RULES, '.cjs': JS_RULES, '.jsx': JS_RULES,
  '.ts': JS_RULES, '.tsx': JS_RULES, '.mts': JS_RULES, '.cts': JS_RULES,
  '.java': JAVA_RULES,
  '.scala': SCALA_RULES, '.sc': SCALA_RULES,
  '.py': PY_RULES, '.pyi': PY_RULES,
  '.go': GO_RULES,
  '.cs': CS_RULES,
};

/** @param {string} ext */
export function rulesFor(ext) {
  return BY_EXT[ext.toLowerCase()] || null;
}

/** Every extension the index knows how to read symbols from. */
export function indexedExtensions() {
  return Object.keys(BY_EXT);
}

/**
 * Extracts symbols from one file's text.
 * @param {string} text
 * @param {Rule[]} rules
 * @returns {{line: number, kind: string, name: string, sig: string}[]}
 */
export function extractSymbols(text, rules) {
  const out = [];
  const lines = text.split('\n');
  for (let i = 0; i < lines.length; i++) {
    const raw = lines[i];
    // cheap skips before running any regex: blank, comment-only, or absurdly long (minified)
    if (!raw || raw.length > 400) continue;
    const t = raw.trimStart();
    if (!t || t.startsWith('//') || t.startsWith('*') || t.startsWith('#') || t.startsWith('/*')) continue;
    for (const { kind, re } of rules) {
      const m = re.exec(raw);
      if (!m) continue;
      const name = m[1];
      if (!name || RESERVED.has(name)) break;
      out.push({ line: i + 1, kind, name, sig: raw.trim().slice(0, 160) });
      break; // one symbol per line: the first rule that matches wins
    }
  }
  return out;
}

/** Keywords that the shallow patterns would otherwise pick up as method names. */
const RESERVED = new Set([
  'if', 'for', 'while', 'switch', 'catch', 'return', 'function', 'class', 'new', 'do', 'else',
  'try', 'finally', 'await', 'typeof', 'delete', 'throw', 'case', 'with', 'yield', 'super', 'this',
]);
