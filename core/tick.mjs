// @ts-check
/**
 * Pure redraw policy of the Mod pane's tick: what counts as "the view changed" and what each beat
 * should do. No node imports (the Mod bundle imports this file).
 */

/**
 * Signature of a `driver.panel()` view. Numbers under a key ending in `Ms` are cut to whole seconds
 * (what durText shows), so two views that draw the same give the same string.
 * @param {unknown} view
 * @returns {string}
 */
export function viewSig(view) {
  const s = JSON.stringify(view ?? null, (key, v) =>
    typeof v === 'number' && key.endsWith('Ms') ? Math.floor(v / 1000) : v);
  return s === undefined ? 'null' : s;
}

/**
 * The Raster key of a spinner. `site` + `id` are unique per spinner in the tree (a repeated key blanks the pane).
 * @param {string} site @param {string | number} id
 */
export const spinKey = (site, id) => `sp:${site}:${id}`;

/**
 * The spinners the active tab draws (one 1x1 Raster each) and whether the header strip shimmers.
 * Mirrors the `spinKey` sites of register.tsx's draw().
 * @param {any} view a `driver.panel()` view
 * @returns {{ spinners: Array<{key: string, k: number, band?: boolean}>, progress: boolean }}
 */
export function spinnersOf(view) {
  /** @type {Array<{key: string, k: number, band?: boolean}>} */
  const out = [];
  const add = (/** @type {string} */ site, /** @type {string | number} */ id, k = 0, band = false) =>
    out.push({ key: spinKey(site, id), k, ...(band ? { band: true } : {}) });
  for (const b of Array.isArray(view?.blocks) ? view.blocks : []) {
    switch (b?.type) {
      case 'hero': if (b.status === 'plan' && b.batch?.state === 'running') add('hero', 0); break;
      case 'now': (b.agents ?? []).forEach((/** @type {any} */ a, /** @type {number} */ i) => add('now', i, i * 5)); break;
      case 'batches':
        for (const x of b.items ?? []) {
          if (x.state !== 'running') continue;
          add('bt', x.n, 0, !!x.open);
          if (x.open) add('bv', x.n, 0, true);
        }
        break;
      case 'agents': (b.agents ?? []).forEach((/** @type {any} */ a, /** @type {number} */ i) => { if (a.live) add('ag', a.id, i * 5); }); break;
      case 'watching': if (b.card?.live) add('watch', 0); break;
      case 'agent': if (b.card?.live) add('detail', 0); break;
      case 'launch': if (b.card?.live) add('launch', 0); break;
      case 'memsearch': if (b.state === 'loading') add('ms', 0); break;
      case 'memdetail': if (b.state === 'loading') add('md', 0); break;
      case 'handoff': if (b.state === 'loading') add('ho', 0); break;
      case 'locate': if (b.state === 'loading') add('lo', 0); break;
      default: break;
    }
  }
  const progress = Array.isArray(view?.header?.progress) && view.header.progress.includes('running');
  return { spinners: out, progress };
}

/**
 * What one 250 ms beat does.
 * @param {{beat: number, shown: boolean, wasShown: boolean, sigChanged: boolean, agentsLive: boolean}} a
 * @returns {{checkPanes: boolean, poll: boolean, redraw: boolean}}
 */
export function tickPlan({ beat, shown, wasShown, sigChanged, agentsLive }) {
  return {
    checkPanes: beat % 4 === 1,
    poll: shown && agentsLive && beat % 8 === 0,
    redraw: shown && (sigChanged || !wasShown),
  };
}
