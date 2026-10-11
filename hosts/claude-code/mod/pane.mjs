// @ts-check
import { layoutOf } from '../../../core/panel.mjs';
import { PALETTE, spinCells, progressCells } from '../../../core/raster.mjs';
import { viewSig, tickPlan, spinnersOf } from '../../../core/tick.mjs';

const TICK_MS = 250;
const REDRAW_MIN_MS = 1000;

/**
 * The pane's redraw policy, apart from the engine: every engine call goes through `io`, built in
 * register.tsx (the engine's checker follows `$` only there). No node imports.
 * @typedef {object} PaneIo
 * @property {() => Promise<any> | any} invalidate  redraw the pane
 * @property {() => Promise<any> | any} panes       the pane list
 * @property {() => Promise<any> | any} usage       the session usage the driver shows
 * @property {(args: any) => Promise<any> | any} blit
 * @property {(text: string) => any} log
 * @property {() => number} now
 */

/**
 * @param {PaneIo} io
 * @param {any} driver
 * @param {{ id: string }} opts
 */
export function createPane(io, driver, opts) {
  const { id } = opts;
  /** @type {string | null} the signature of what the render handler drew last */
  let drawnSig = null;
  let frameNo = 0;
  let beat = 0;
  let shown = true;
  let wasShown = true;
  let wasLive = false;
  /** @type {number | null} */
  let lastAt = null;
  let lastRedrawAt = -Infinity;
  // Counters of this pane's cost: drift = the engine backlog (gap between ticks minus 250 ms).
  const perf = { redraws: 0, polls: 0, driftMs: 0, pressMs: 0, blits: 0, denies: 0 };

  /**
   * Move the spinners and the progress strip by blit (no redraw). A deny (not mounted, resized) or a
   * throw falls back to ONE redraw.
   * @param {any} view
   */
  async function animate(view) {
    const { spinners, progress } = spinnersOf(view);
    if (!spinners.length && !progress) return;
    const f = ++frameNo;
    const calls = spinners.map((s) => ({ requestId: id, key: s.key, cells: spinCells(f, s.k, s.band ? PALETTE.band : undefined) }));
    if (progress) {
      const cw = layoutOf(driver.getColumns?.() ?? 78).CW;
      calls.push({ requestId: id, key: 'progress', cells: progressCells(cw, view.header.progress, f) });
    }
    for (const args of calls) {
      let denied = false;
      try {
        const r = await io.blit(args);
        denied = !!(r && r.deny);
      } catch { denied = true; }
      perf.blits++;
      if (denied) {
        perf.denies++;
        perf.redraws++;
        try { await io.invalidate(); } catch { /* optional */ }
        return;
      }
    }
  }

  return {
    /** The render handler drew this view. */
    drawn(/** @type {any} */ view) {
      drawnSig = view ? viewSig(view) : null;
    },
    /** Invalidate only when forced or the view differs from the one drawn. */
    async redraw(force = false) {
      if (!force) {
        const view = driver.panel();
        if (view && drawnSig !== null && viewSig(view) === drawnSig) return;
      }
      try { await io.invalidate(); } catch { /* optional */ }
    },
    /** A button press: the first engine call is the redraw; the work follows. */
    async press(/** @type {string} */ element) {
      const t0 = io.now();
      const pressed = driver.press(element);
      await this.redraw(true);
      perf.pressMs = Math.max(perf.pressMs, io.now() - t0);
      await pressed;
      driver.setUsage(await io.usage());
      await this.redraw();
    },
    /**
     * One 250 ms beat. Returns `{ stop: true }` when the pane is gone (the caller cancels its timer).
     * @returns {Promise<{ stop?: boolean } | undefined>}
     */
    async tick() {
      beat++;
      const t = io.now();
      if (lastAt !== null) perf.driftMs = Math.max(perf.driftMs, t - lastAt - TICK_MS);
      lastAt = t;
      const view = driver.panel();
      const live = !!driver.agentsLive();
      const sigChanged = !view || drawnSig === null || viewSig(view) !== drawnSig;
      const args = { beat, shown, wasShown, agentsLive: live, sigChanged };
      let now = tickPlan(args);
      if (now.checkPanes) {
        // The pane list is an engine round trip: once a second.
        const list = await io.panes();
        // A non-array answer keeps the previous state; only a list without this pane stops the tick.
        if (Array.isArray(list)) {
          const mine = list.find((p) => p?.id === id);
          if (!mine) return { stop: true };
          const nowShown = !!(mine.isShown && mine.isPlaced);
          if (nowShown !== shown) {
            shown = nowShown;
            // Planned again with the pane state of this beat.
            now = tickPlan({ ...args, shown });
          }
          driver.setShown(shown);
        }
      }
      if (now.poll) {
        // Detached: a slow `session.messages` must not stall the next redraw (syncAgents has its own guard).
        perf.polls++;
        try { Promise.resolve(driver.syncAgents()).catch(() => {}); } catch { /* optional */ }
      }
      if (wasLive && !live) {
        try {
          io.log(`nxy perf: redraws=${perf.redraws} polls=${perf.polls} driftMs=${Math.round(perf.driftMs)} pressMs=${Math.round(perf.pressMs)}`);
        } catch { /* optional */ }
      }
      wasLive = live;
      const comingBack = !wasShown;
      wasShown = shown;
      // Sig-driven redraws are at least 1 s apart (a withheld change is still different from what was
      // drawn, so a later beat draws it); coming back from hidden is forced.
      if (now.redraw && (comingBack || t - lastRedrawAt >= REDRAW_MIN_MS)) {
        lastRedrawAt = t;
        perf.redraws++;
        try { await io.invalidate(); } catch { /* optional */ }
      } else if (shown && view?.animated && beat % 2 === 0) {
        await animate(view);
      }
      return undefined;
    },
    perf,
    frame: () => frameNo,
  };
}
