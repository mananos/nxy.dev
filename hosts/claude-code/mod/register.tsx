import type { Register } from 'claude-code'

import { COMPOSE_SECTION } from '../../../core/orchestrator.mjs'
import { createDriver } from './driver.mjs'

const PANE = 'nxy'

/**
 * nxy's Mod: the orchestrator's wiring. The logic is in driver.mjs (testable without Claude Code);
 * this file only maps engine events to it. Every hook is fail-open: an error releases the marker
 * and the main thread goes on as without the Mod.
 */
// The engine's checker follows `$` only inside this file, so the driver gets this adapter, not `$`.
const api = ($: any) => ({
  plugin: { root: $.plugin.root },
  fs: {
    read: (path: string) => $.fs.read(path),
    write: (path: string, data: string) => $.fs.write(path, data),
  },
  process: { run: (argv: string[]) => $.process.run(argv) },
  agent: { spawn: (args: any) => $.agent.spawn(args) },
  session: { append: (args: any) => $.session.append(args) },
  prompt: { submit: (args: any) => $.prompt.submit(args) },
})

let driver: any = null
let headless = false

async function fail($: any, where: string, err: unknown) {
  try { void $.ui.log({ text: `nxy orchestrator (${where}): ${String((err as any)?.message ?? err)}` }) } catch { /* nothing to do */ }
  try { await driver?.release() } catch { /* best-effort */ }
}
async function sync($: any) {
  if (!driver) return
  // Open once when the pane is not up; close it on hand-back (no view), never reopen per sync.
  const isUp = (await $.ui.panes()).some((p: any) => p.id === PANE)
  if (driver.view()) {
    if (!isUp) await $.ui.open({ id: PANE, title: 'nxy' })
  } else if (isUp) {
    await $.ui.close({ id: PANE })
    return
  }
  try { await $.ui.invalidate('ui.render') } catch { /* optional */ }
}
async function ensure($: any) {
  if (!driver) driver = createDriver(api($), { cwd: await $.session.cwd(), sessionId: await $.session.id(), headless })
  return driver
}

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    try {
      headless = !e.isInteractive
      driver = createDriver(api($), { cwd: e.cwd, sessionId: await $.session.id(), headless })
      await driver.release()
    } catch (err) { await fail($, 'session.start', err) }
    return next(e)
  })

  // The registration's own `.catch`: if this hook throws, the call goes on as without the Mod.
  on('tool.call', { tool: 'AskUserQuestion' }, async ($, e, next) => {
    const ran = await next(e)
    await ensure($)
      .then(d => d.step())
      .then(() => sync($))
      .catch(err => fail($, 'tool.call', err))
    return ran
  }).catch(($, e, next) => next(e))

  on('turn.complete', async ($, e, next) => {
    const ran = await next(e)
    try {
      const d = await ensure($)
      if (e.agentId) await d.onAgentDone(e.agentId, e.answer)
      else await d.step()
      await sync($)
    } catch (err) { await fail($, 'turn.complete', err) }
    return ran
  })

  on('ui.press', { requestId: PANE }, async ($, e, next) => {
    try {
      if (driver) {
        await driver.press(e.element)
        await sync($)
      }
    } catch (err) { await fail($, 'ui.press', err) }
    return next(e)
  })

  on('ui.render', { component: 'Pane', requestId: PANE }, async ($, e) => {
    const { Box, Text, Button } = $.ui.resolve(e)
    const v = driver?.view() ?? null
    if (!v) return <Text dimColor>nxy: nothing running.</Text>
    const color = (tone: string) => (tone === 'ok' ? 'green' : tone === 'error' ? 'red' : undefined)
    return (
      <Box flexDirection="column">
        {v.rows.map((r: any) => (
          <Text color={color(r.tone)} dimColor={r.tone === 'dim'}>{r.text}</Text>
        ))}
        {v.buttons.map((b: string, i: number) => (
          <Button key={b} label={b} hotkey={String(i + 1)} />
        ))}
      </Box>
    )
  })

  // The section appears once the driver has taken a plan over (identical text on every request after
  // that, so the cache is rewritten once, not per call); never when the orchestrator is off or idle.
  on('prompt.compose', async ($, e, next) => {
    const out = await next(e)
    try {
      if (!driver?.view()) return out
      return { ...out, sections: [...out.sections, { id: 'nxy-orchestrator', text: COMPOSE_SECTION, scope: 'session' }] }
    } catch (err) {
      await fail($, 'prompt.compose', err)
      return out
    }
  })
}
