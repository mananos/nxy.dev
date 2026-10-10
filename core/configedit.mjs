// @ts-check
// Schema editable de la pestaña Config (sin módulos de Node): qué claves se pueden cambiar desde el panel,
// con qué valores, y los helpers puros para ciclarlas, escribirlas en un objeto y saber de dónde viene cada una.

export const MODELS = ['haiku', 'sonnet', 'opus', 'fable'];
export const EFFORTS = ['low', 'medium', 'high'];
export const GATE_THRESHOLDS = [50000, 75000, 100000, 150000, 200000, 300000];
export const ROLES = ['planner', 'implementer', 'tester', 'reviewer', 'documenter', 'scout', 'librarian'];

/**
 * @typedef {object} Field
 * @property {string[]} path
 * @property {'cycle'|'toggle'} kind
 * @property {Array<string|number|boolean|null>} options  null = «predeterminado» (quita la clave)
 */

/** @type {Record<string, Field>} */
export const FIELDS = {};
for (const r of ROLES) {
  FIELDS[`roles.${r}.model`] = { path: ['roles', r, 'model'], kind: 'cycle', options: [null, ...MODELS] };
  FIELDS[`roles.${r}.effort`] = { path: ['roles', r, 'effort'], kind: 'cycle', options: [null, ...EFFORTS] };
}
FIELDS['flow.orchestrator'] = { path: ['flow', 'orchestrator'], kind: 'toggle', options: ['auto', 'off'] };
FIELDS['flow.pauseAfterBatch'] = { path: ['flow', 'pauseAfterBatch'], kind: 'toggle', options: [false, true] };
FIELDS['gate.enabled'] = { path: ['gate', 'enabled'], kind: 'toggle', options: [true, false] };
FIELDS['gate.contextTokens'] = { path: ['gate', 'contextTokens'], kind: 'cycle', options: GATE_THRESHOLDS };
FIELDS['modules.filter'] = { path: ['modules', 'filter'], kind: 'toggle', options: [true, false] };
FIELDS['ui.panel'] = { path: ['ui', 'panel'], kind: 'toggle', options: ['auto', 'off'] };

/** @param {unknown} id */
export function isField(id) {
  return typeof id === 'string' && Object.prototype.hasOwnProperty.call(FIELDS, id);
}

/**
 * Próximo valor de un campo. `dir`: 'next' | 'prev' | 'toggle' (toggle = next).
 * @param {string} fieldId
 * @param {unknown} current
 * @param {'next'|'prev'|'toggle'} [dir]
 * @param {unknown} [fallback] lo que se ve cuando la clave no está guardada (null = predeterminado)
 */
export function nextValue(fieldId, current, dir = 'next', fallback = null) {
  if (!isField(fieldId)) return null;
  const { kind, options } = FIELDS[fieldId];
  if (kind === 'toggle') {
    const first = options[0];
    // current sin definir cuenta como el primer valor (el default real de cada toggle)
    const cur = current === undefined || current === null ? first : current;
    return cur === first ? options[1] : first;
  }
  const step = dir === 'prev' ? -1 : 1;
  if (typeof options[0] === 'number') {
    const nums = /** @type {number[]} */ (options);
    const cur = Number(current);
    const i = nums.indexOf(cur);
    if (i >= 0) return nums[(i + step + nums.length) % nums.length];
    if (!Number.isFinite(cur)) return nums[0];
    if (step > 0) return nums.find((n) => n > cur) ?? nums[nums.length - 1];
    return [...nums].reverse().find((n) => n < cur) ?? nums[0];
  }
  const i = options.indexOf(current === undefined ? null : /** @type {any} */ (current));
  if (i < 0) return options[0];
  // «predeterminado» (null) se ve como `fallback` (p. ej. el effort del frontmatter): un paso que no cambia lo que se ve se salta
  const shown = (/** @type {any} */ v) => (v === null ? fallback ?? null : v);
  for (let k = 1; k <= options.length; k++) {
    const cand = options[(i + step * k + options.length * k) % options.length];
    if (shown(cand) !== shown(options[i])) return cand;
  }
  return options[(i + step + options.length) % options.length];
}

/**
 * Copia de `obj` con `value` en `path`. `value === null` borra la clave y poda los objetos que quedan vacíos.
 * @param {Record<string, any>} obj
 * @param {string[]} path
 * @param {unknown} value
 */
export function setPath(obj, path, value) {
  const root = { ...(obj && typeof obj === 'object' ? obj : {}) };
  if (!path.length) return root;
  const [head, ...rest] = path;
  if (!rest.length) {
    if (value === null) delete root[head];
    else root[head] = value;
    return root;
  }
  const child = root[head] && typeof root[head] === 'object' && !Array.isArray(root[head]) ? root[head] : {};
  const next = setPath(child, rest, value);
  if (value === null && Object.keys(next).length === 0) delete root[head];
  else root[head] = next;
  return root;
}

/**
 * @param {string} fieldId
 * @param {unknown} value
 * @returns {{ok: boolean, reason?: string}}
 */
export function validate(fieldId, value) {
  if (!isField(fieldId)) return { ok: false, reason: `campo desconocido: ${fieldId}` };
  const { options } = FIELDS[fieldId];
  // null borra la clave (vuelve al predeterminado): todo campo conocido lo acepta
  if (value === null) return { ok: true };
  if (!options.includes(/** @type {any} */ (value))) return { ok: false, reason: `valor inválido para ${fieldId}: ${String(value)}` };
  return { ok: true };
}

/**
 * De dónde viene el valor de un campo: el repo pisa al usuario (misma precedencia que loadConfig).
 * @param {string} fieldId
 * @param {{user?: any, repo?: any}} files
 * @param {Record<string, string|undefined>} [env] pass process.env from the host (this module stays pure)
 * @returns {'default'|'user'|'repo'|'env'}
 */
export function sourcesOf(fieldId, files, env = {}) {
  if (!isField(fieldId)) return 'default';
  // loadConfig applies NXY_FILTER=0|1 last, over every file
  if (fieldId === 'modules.filter' && env && (env.NXY_FILTER === '0' || env.NXY_FILTER === '1')) return 'env';
  const has = (/** @type {any} */ o) => {
    let cur = o;
    for (const k of FIELDS[fieldId].path) {
      if (!cur || typeof cur !== 'object' || !Object.prototype.hasOwnProperty.call(cur, k)) return false;
      cur = cur[k];
    }
    return cur !== undefined;
  };
  if (has(files && files.repo)) return 'repo';
  if (has(files && files.user)) return 'user';
  return 'default';
}
