// @ts-check
// Tabla de precios pura (sin módulos de Node): el Mod la importa para calcular el costo en frío.

/**
 * @typedef {object} ModelPrice
 * @property {number} input
 * @property {number} cache_write_5m
 * @property {number} cache_write_1h
 * @property {number} cache_read
 * @property {number} output
 * @property {{input: number, output: number}} [fast]
 */

/**
 * Reduce un id de modelo a la clave de la tabla de precios. Cubre las tres plataformas:
 *   - API directa: `claude-opus-5-20260401[1m]` → `claude-opus-5`, `-latest` fuera.
 *   - Bedrock: `us.anthropic.claude-sonnet-4-5-20250929-v1:0` → `claude-sonnet-4-5`
 *     (prefijos de región/alcance `us.` `eu.` `apac.` `global.`, luego `anthropic.`, sufijo `-vN:M`).
 *   - Vertex: `claude-sonnet-4-5@20250929` → `claude-sonnet-4-5`.
 * Lo que no matchea queda como está (`claude-3-5-haiku-latest` → `claude-3-5-haiku`, desconocido).
 * @param {string} id
 */
export function normalizeModel(id) {
  return String(id || '')
    .toLowerCase()
    .replace(/^(?:us|eu|apac|global)\./, '')
    .replace(/^anthropic\./, '')
    .replace(/-v\d+:\d+$/, '')
    .replace(/@\d{8}$/, '')
    .replace(/\[[^\]]*\]$/, '')
    .replace(/-\d{8}$/, '')
    .replace(/-latest$/, '');
}

/**
 * Familia de un id de modelo: opus|sonnet|haiku|fable|mythos (también el orden viejo
 * `claude-3-5-haiku`), o null si no es ninguna.
 * @param {string} id
 */
export function familyOf(id) {
  const m = /(opus|sonnet|haiku|fable|mythos)/.exec(normalizeModel(id));
  return m ? m[1] : null;
}

/**
 * Grupos de dígitos de una clave, sin la fecha: `claude-sonnet-5-5` → [5, 5]; `claude-3-5-haiku` → [3, 5].
 * @param {string} key
 * @returns {number[]}
 */
export function versionOf(key) {
  return (String(key).match(/\d+/g) || []).map(Number);
}

function cmpVersion(a, b) {
  for (let i = 0; i < Math.max(a.length, b.length); i++) {
    const d = (a[i] ?? 0) - (b[i] ?? 0);
    if (d) return d;
  }
  return 0;
}

/**
 * Precio de un modelo en `models`: clave exacta, o (estimado) la mayor versión conocida de la
 * misma familia que sea <= la pedida (si no hay, la menor por encima). Familia desconocida → null.
 * @param {Record<string, ModelPrice>} models
 * @param {string} model
 * @returns {{price: ModelPrice, key: string, estimated: boolean}|null}
 */
export function priceIn(models, model) {
  const id = normalizeModel(model);
  if (models[id]) return { price: models[id], key: id, estimated: false };
  const fam = familyOf(id);
  if (!fam) return null;
  const want = versionOf(id);
  const same = Object.keys(models)
    .filter((k) => familyOf(k) === fam)
    .map((k) => ({ k, v: versionOf(k) }))
    .sort((a, b) => cmpVersion(a.v, b.v));
  if (!same.length) return null;
  const below = same.filter((e) => cmpVersion(e.v, want) <= 0);
  const pick = below.length ? below[below.length - 1] : same[0];
  return { price: models[pick.k], key: pick.k, estimated: true };
}
