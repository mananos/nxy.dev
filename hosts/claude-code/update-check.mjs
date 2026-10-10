// Decide si `claude` falta tras un spawnSync. Puro: sin node:*.
// Falta sólo por ENOENT, status 127 o (win32) el mensaje del shell con status 1 y stdout vacío.
// Cualquier otro «not found» es salida del propio claude y se muestra.

/**
 * @param {{error?: any, status?: number|null, stdout?: string|null, stderr?: string|null}} r
 * @param {string} platform
 */
export function claudeMissing(r, platform) {
  if (r.error && r.error.code === 'ENOENT') return true;
  if (r.status === 127) return true;
  if (platform === 'win32' && r.status === 1 && !String(r.stdout ?? '').trim()) {
    return /not recognized|no se reconoce/i.test(String(r.stderr ?? ''));
  }
  return false;
}
