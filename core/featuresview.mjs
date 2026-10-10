// @ts-check
/**
 * The Features tab's pure logic: parsing `git worktree list --porcelain`, branch / folder names, the exact
 * `git worktree add` / `remove` argv and how it is shown, the "open" command per shell, which branches are
 * free or busy, and the texts of a row and of the two confirmations.
 *
 * Pure: no `node:*`, no `process`, no git. The entry (worktrees.mjs) runs git and feeds this module.
 */

/**
 * @typedef {{path: string, branch: string | null, head: string, detached: boolean, locked: boolean, main: boolean}} Worktree
 * @typedef {{ok: boolean, create: boolean, slug: string, branch: string, base: string | null, path: string, error: string}} FeatureNames
 * @typedef {{
 *   path: string, branch: string | null, main?: boolean, current?: boolean, dirty?: number | null, ahead?: number,
 *   batchDone?: number, batchTotal?: number, reviewDone?: boolean, handoffAt?: number | null,
 *   pausedAt?: number | null, running?: boolean, missingDeps?: boolean,
 * }} RowInput
 */

const SLUG_MAX = 50;
const ISSUE_MAX = 7;

/** @param {string} p */
const slashes = (p) => p.replace(/\\/g, '/');

/**
 * Parses `git worktree list --porcelain`. The first block is the main worktree.
 * @param {string} porcelain
 * @returns {Worktree[]}
 */
export function parseWorktrees(porcelain) {
  /** @type {Worktree[]} */
  const out = [];
  for (const block of String(porcelain || '').split(/\r?\n\s*\r?\n/)) {
    /** @type {Worktree | null} */
    let wt = null;
    for (const raw of block.split(/\r?\n/)) {
      const line = raw.trimEnd();
      if (line.startsWith('worktree ')) {
        wt = { path: slashes(line.slice(9)), branch: null, head: '', detached: false, locked: false, main: out.length === 0 };
      } else if (!wt) {
        continue;
      } else if (line.startsWith('HEAD ')) wt.head = line.slice(5);
      else if (line.startsWith('branch ')) wt.branch = line.slice(7).replace(/^refs\/heads\//, '');
      else if (line === 'detached') wt.detached = true;
      else if (line === 'locked' || line.startsWith('locked ')) wt.locked = true;
    }
    if (wt) out.push(wt);
  }
  return out;
}

/**
 * @param {string} text
 * @returns {string}
 */
export function slugOf(text) {
  const s = String(text || '')
    .normalize('NFD').replace(/[̀-ͯ]/g, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '_')
    .replace(/^_+|_+$/g, '')
    .slice(0, SLUG_MAX);
  return s.replace(/_+$/, '');
}

/**
 * @param {string} text
 * @returns {{ok: boolean, issue: string, error: string}}
 */
export function parseIssue(text) {
  const t = String(text ?? '').trim().replace(/^#/, '').trim();
  if (t === '') {
    // A lone "#" is not an issue number.
    if (String(text ?? '').trim() === '#') return { ok: false, issue: '', error: 'el n.º de issue son solo dígitos (14 o #14)' };
    return { ok: true, issue: '', error: '' };
  }
  if (!/^\d+$/.test(t) || t.length > ISSUE_MAX) {
    return { ok: false, issue: '', error: `el n.º de issue son solo dígitos, hasta ${ISSUE_MAX} (14 o #14)` };
  }
  return { ok: true, issue: t, error: '' };
}

/**
 * @param {string} s
 * @returns {string}
 */
function dirOf(s) {
  const i = s.lastIndexOf('/');
  return i <= 0 ? (i === 0 ? '/' : '.') : s.slice(0, i);
}

/**
 * @param {{issue?: string, name?: string, base?: string, branch?: string}} input
 * @param {string} mainPath
 * @returns {FeatureNames}
 */
export function featureNames(input, mainPath) {
  const main = slashes(String(mainPath || '')).replace(/\/+$/, '');
  const dir = dirOf(main);
  const repo = main.slice(main.lastIndexOf('/') + 1);
  const root = `${dir === '/' ? '' : dir}/${repo}.worktrees`;
  /** @param {string} error */
  const fail = (error) => ({ ok: false, create: false, slug: '', branch: '', base: null, path: '', error });
  const existing = input.branch !== undefined && input.name === undefined;
  if (existing) {
    const branch = String(input.branch || '').trim();
    const slug = slugOf(branch.replace(/^feature\//, ''));
    if (!branch || !slug) return fail('elegí una rama');
    return { ok: true, create: false, slug, branch, base: null, path: `${root}/${slug}`, error: '' };
  }
  const slug = slugOf(input.name ?? '');
  if (!slug) return fail('poné un nombre para el feature (letras o números)');
  const issue = String(input.issue ?? '');
  const base = String(input.base ?? '').trim();
  if (!base) return fail('elegí la rama base');
  const branch = issue ? `feature/#${issue}_${slug}` : `feature/${slug}`;
  // la carpeta sale de la rama sin `feature/`: reabrirla como «existente» cae en la misma carpeta
  const folder = slugOf(branch.replace(/^feature\//, ''));
  return { ok: true, create: true, slug: folder, branch, base, path: `${root}/${folder}`, error: '' };
}

/**
 * The repo's main branch: origin/HEAD (if that branch exists locally), else the main worktree's branch,
 * else main / master, else null.
 * @param {{originHead?: string | null, mainBranch?: string | null, branches: string[]}} p
 * @returns {string | null}
 */
export function defaultBranchOf({ originHead, mainBranch, branches }) {
  const has = (/** @type {string} */ b) => branches.includes(b);
  const o = String(originHead || '').trim();
  if (o) {
    const b = o.includes('/') ? o.slice(o.indexOf('/') + 1) : o;
    if (b && has(b)) return b;
  }
  if (mainBranch) return mainBranch;
  if (has('main')) return 'main';
  if (has('master')) return 'master';
  return null;
}

/**
 * The bases to choose from: the main branch first, then the other local branches (busy ones included).
 * @param {string[]} branches
 * @param {string | null} defaultBranch
 * @returns {string[]}
 */
export function baseChoices(branches, defaultBranch) {
  const out = defaultBranch ? [defaultBranch] : [];
  for (const b of branches) if (!out.includes(b)) out.push(b);
  return out;
}

/**
 * @param {string} branch
 * @param {string} path
 * @param {boolean} create
 * @param {string | null} [base]
 * @returns {string[]}
 */
export function addArgv(branch, path, create, base) {
  if (create) {
    if (!base) throw new Error('addArgv: create needs an explicit base');
    return ['git', 'worktree', 'add', '-b', branch, path, base];
  }
  return ['git', 'worktree', 'add', path, branch];
}

/**
 * @param {string} path
 * @param {boolean} [force]
 * @returns {string[]}
 */
export function removeArgv(path, force) {
  return force ? ['git', 'worktree', 'remove', '--force', path] : ['git', 'worktree', 'remove', path];
}

/** @typedef {'powershell' | 'posix'} Shell */

/**
 * @param {string} s
 * @param {Shell} shell
 */
function quote(s, shell) {
  return `'${s.replace(/'/g, shell === 'powershell' ? "''" : "'\\''")}'`;
}

/**
 * The command as shown: single quotes only around an argument that needs them. `shell` only matters for
 * an argument that itself holds a single quote (default posix).
 * @param {string[]} argv
 * @param {Shell} [shell]
 * @returns {string}
 */
export function commandText(argv, shell = 'posix') {
  return argv.map((a) => (/^[A-Za-z0-9_\-./:=@%+,]+$/.test(a) ? a : quote(a, shell))).join(' ');
}

/**
 * @param {{platform?: string, env?: Record<string, string | undefined>}} p
 * @returns {Shell}
 */
export function shellOf({ platform, env }) {
  return platform === 'win32' && !(env && env.SHELL) ? 'powershell' : 'posix';
}

/**
 * @param {string} path
 * @param {Shell} shell
 * @returns {string}
 */
export function openCommand(path, shell) {
  return shell === 'powershell' ? `cd ${quote(path, shell)}; claude` : `cd ${quote(path, shell)} && claude`;
}

/**
 * Local branches no worktree has open (free) and those that one has (busy, with its path).
 * @param {string[]} branches
 * @param {{path: string, branch: string | null}[]} worktrees
 * @returns {{free: string[], busy: {name: string, path: string}[]}}
 */
export function branchChoices(branches, worktrees) {
  /** @type {Map<string, string>} */
  const held = new Map();
  for (const w of worktrees) if (w.branch && !held.has(w.branch)) held.set(w.branch, w.path);
  /** @type {string[]} */
  const free = [];
  /** @type {{name: string, path: string}[]} */
  const busy = [];
  for (const b of branches) {
    const p = held.get(b);
    if (p === undefined) free.push(b);
    else busy.push({ name: b, path: p });
  }
  return { free, busy };
}

/**
 * @param {number} ms
 * @returns {string}
 */
function ago(ms) {
  const m = Math.max(0, Math.round(ms / 60000));
  if (m < 1) return 'menos de 1 min';
  if (m < 60) return `${m} min`;
  const h = Math.round(m / 60);
  if (h < 48) return `${h} h`;
  return `${Math.round(h / 24)} d`;
}

/**
 * The texts of one worktree row.
 * @param {RowInput} wt
 * @param {{now: number}} o
 * @returns {{name: string, branch: string, tags: string[], lines: string[]}}
 */
export function rowView(wt, { now }) {
  const path = slashes(wt.path);
  const name = path.slice(path.lastIndexOf('/') + 1);
  /** @type {string[]} */
  const tags = [];
  if (wt.main) tags.push('principal');
  if (wt.current) tags.push('esta sesión');
  if (wt.pausedAt) tags.push(`pausado hace ${ago(now - wt.pausedAt)}`);
  if (wt.running) tags.push('plan corriendo');
  /** @type {string[]} */
  const lines = [];
  const dirty = wt.dirty ?? 0;
  lines.push(wt.dirty === null
    ? 'no se pudo leer el estado'
    : dirty > 0 ? `${dirty} ${dirty === 1 ? 'archivo' : 'archivos'} sin commitear` : 'limpio');
  if (wt.batchTotal) {
    lines.push(`lote ${wt.batchDone ?? 0}/${wt.batchTotal}${wt.reviewDone ? ' · review hecha' : ''}`);
  }
  lines.push(wt.handoffAt ? `handoff hace ${ago(now - wt.handoffAt)}` : 'sin handoff');
  const ahead = wt.ahead ?? 0;
  if (ahead > 0) lines.push(`${ahead} ${ahead === 1 ? 'commit' : 'commits'} sin pushear`);
  if (wt.missingDeps) lines.push('falta node_modules: corré npm install en esa carpeta');
  return { name, branch: wt.branch ?? '(detached)', tags, lines };
}

const CLOSE_TERMINAL = 'Si hay una terminal con Claude abierta ahí, cerrala antes.';

/**
 * @param {FeatureNames} names
 * @param {string | null} [baseBranch]
 * @param {Shell} [shell]
 * @returns {string}
 */
export function newQuestion(names, baseBranch, shell = 'posix') {
  const base = baseBranch ?? names.base;
  const cmd = commandText(addArgv(names.branch, names.path, names.create, base), shell);
  const what = names.create
    ? `Crea la rama ${names.branch}, que sale de la rama local ${base}, tal como está en este equipo: no se baja nada del remoto.`
    : `Usa la rama ${names.branch} que ya existe; no crea ninguna rama.`;
  return `${cmd}\n${what}\n${CLOSE_TERMINAL}`;
}

/**
 * @param {RowInput} wt
 * @param {Shell} [shell]
 * @returns {string}
 */
export function closeQuestion(wt, shell = 'posix') {
  const unknown = wt.dirty === null;
  const cmd = commandText(removeArgv(wt.path, unknown || (wt.dirty ?? 0) > 0), shell);
  const dirty = wt.dirty ?? 0;
  const lost = unknown
    ? 'No se pudo leer el estado: puede tener cambios sin commitear, que SE PIERDEN, igual que lo ignorado (node_modules, .nxy/local). La rama no se borra.'
    : dirty > 0
    ? `Tiene ${dirty} ${dirty === 1 ? 'archivo' : 'archivos'} sin commitear: SE PIERDEN, igual que lo ignorado (node_modules, .nxy/local). La rama no se borra.`
    : 'Está limpio. La rama no se borra.';
  return `${cmd}\n${lost}\n${CLOSE_TERMINAL}`;
}
