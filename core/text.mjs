// @ts-check
/**
 * Words that carry no signal, in the two languages this tool gets used in.
 *
 * Shared by the scout's search and by memory search: both take a question typed by a human and
 * have to turn it into terms. Keeping one list means a fix to either one helps both — and means
 * "¿cómo se decide el umbral?" does not match a note just because both contain "el".
 */
export const STOPWORDS = new Set([
  // es
  'donde', 'dónde', 'como', 'cómo', 'cual', 'cuál', 'que', 'qué', 'quien', 'quién', 'para', 'por',
  'con', 'sin', 'del', 'las', 'los', 'una', 'uno', 'unos', 'unas', 'esta', 'este', 'esto', 'esa',
  'ese', 'eso', 'estan', 'están', 'hace', 'hacen', 'tiene', 'tienen', 'puede', 'pueden', 'cuando',
  'cuándo', 'archivo', 'archivos', 'codigo', 'código', 'funcion', 'función', 'metodo', 'método',
  'clase', 'sobre', 'entre', 'desde', 'hasta', 'todo', 'toda', 'todos', 'todas', 'mas', 'más',
  'pero', 'porque', 'si', 'no', 'ser', 'son', 'era', 'fue', 'hay', 'muy', 'ya', 'lo', 'la', 'el',
  'en', 'de', 'un', 'se', 'al', 'su', 'sus', 'nos', 'les', 'aca', 'acá', 'ahi', 'ahí',
  // en
  'where', 'what', 'which', 'when', 'how', 'why', 'who', 'does', 'did', 'the', 'and', 'for',
  'with', 'without', 'from', 'into', 'this', 'that', 'these', 'those', 'are', 'was', 'were',
  'file', 'files', 'code', 'function', 'method', 'class', 'about', 'there', 'here', 'happen',
  'happens', 'handled', 'handle', 'find', 'look', 'show', 'has', 'have', 'its', 'not', 'but',
  'you', 'your', 'can', 'will', 'should', 'would', 'been', 'being', 'they', 'them',
]);

/**
 * The words of a question worth searching on: at least three characters, not a stopword, not a
 * bare number. Order is preserved and duplicates are dropped.
 * @param {string} text
 * @returns {string[]}
 */
export function contentTerms(text) {
  const seen = new Set();
  /** @type {string[]} */
  const out = [];
  for (const raw of String(text || '').split(/[^\p{L}\p{N}_]+/u)) {
    const t = raw.toLowerCase();
    if (t.length < 3 || STOPWORDS.has(t) || /^\d+$/.test(t) || seen.has(t)) continue;
    seen.add(t);
    out.push(t);
  }
  return out;
}
