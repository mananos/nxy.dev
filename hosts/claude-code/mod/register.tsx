import type { Register } from 'claude-code'

import { COMPOSE_SECTION, PANEL_CLOSE, readModConfig } from '../../../core/orchestrator.mjs'
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
let userPanel = false // the user opened the panel with /nxy-panel
let autoPanel = false // the Mod opened it at session start
let lastStatus = ''

async function fail($: any, where: string, err: unknown) {
  try { void $.ui.log({ text: `nxy orchestrator (${where}): ${String((err as any)?.message ?? err)}` }) } catch { /* nothing to do */ }
  try { await driver?.release() } catch { /* best-effort */ }
}
// The status entry under the prompt; only sent when the text changed. Fail-open.
async function showStatus($: any) {
  try {
    if (!driver) return
    const text = driver.status()
    if (text === lastStatus) return
    lastStatus = text
    await $.ui.status(text)
  } catch { /* optional */ }
}
async function usageOf($: any) {
  try {
    const u = await $.session.usage()
    const { tokens, percent } = u.context ?? {}
    // both are absent before the first response of the session
    if (!Number.isFinite(tokens) || !Number.isFinite(percent)) return null
    return { tokens, percent }
  } catch { return null }
}
async function readConfig($: any) {
  const read = async (path: string | null) => {
    if (!path) return null
    try { return await $.fs.read(path) } catch { return null }
  }
  let nxyHome: string | undefined, homeDir: string | undefined, profile: string | undefined
  try { nxyHome = await $.env.get('NXY_HOME') } catch { /* unset */ }
  try { homeDir = await $.env.get('HOME') } catch { /* unset */ }
  try { profile = await $.env.get('USERPROFILE') } catch { /* unset */ }
  const home = nxyHome || homeDir || profile
  const cwd = await $.session.cwd()
  return readModConfig([
    await read(`${$.plugin.root}/nxy.config.json`),
    await read(home ? `${home}/.nxy/config.json` : null),
    await read(`${cwd}/.nxy/config.json`),
  ])
}
async function sync($: any) {
  if (!driver) return
  // The pane stays up for the session unless the panel is off; on hand-back it redraws as the panel view.
  const keep = userPanel || (driver.panelSetting() === 'auto' && !headless)
  const isUp = (await $.ui.panes()).some((p: any) => p.id === PANE)
  if (autoPanel && !userPanel && driver.panelSetting() === 'off') {
    // The snapshot says the panel is off: close only a pane the Mod opened unasked.
    autoPanel = false
    if (isUp && !driver.view()) {
      await $.ui.close({ id: PANE })
      await showStatus($)
      return
    }
  }
  if (driver.view()) {
    if (!isUp) await $.ui.open({ id: PANE, title: 'nxy' })
  } else if (isUp && !keep) {
    await $.ui.close({ id: PANE })
    await showStatus($)
    return
  }
  try { await $.ui.invalidate('ui.render') } catch { /* optional */ }
  await showStatus($)
}
async function ensure($: any) {
  if (!driver) driver = createDriver(api($), { cwd: await $.session.cwd(), sessionId: await $.session.id(), headless })
  return driver
}

// A button's press (the bottom of the `ui.press` chain; runs in the plugin's environment).
async function onPress($: any, element: string): Promise<void> {
  try {
    if (!driver) return
    if (element === PANEL_CLOSE) {
      userPanel = false
      autoPanel = false
      await $.ui.close({ id: PANE })
    } else {
      driver.setUsage(await usageOf($))
      await driver.press(element)
      await sync($)
    }
  } catch (err) { await fail($, 'ui.press', err) }
}

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    try {
      headless = !e.isInteractive
      driver = createDriver(api($), { cwd: e.cwd, sessionId: await $.session.id(), headless })
      await driver.release()
      driver.setConfig(await readConfig($))
      driver.setUsage(await usageOf($))
      await showStatus($)
      if (!headless && driver.panelSetting() === 'auto') {
        await $.ui.open({ id: PANE, title: 'nxy' })
        autoPanel = true
      }
    } catch (err) { await fail($, 'session.start', err) }
    try {
      await $.command.register({ name: 'nxy-panel', description: 'nxy panel: plan, handoff, gate and quick actions' })
    } catch { /* optional */ }
    return next(e)
  })

  on('command.run', { command: 'nxy-panel' }, async ($, e) => {
    const d = await ensure($)
    d.setUsage(await usageOf($))
    await d.openPanel()
    // Not interactive (-p): the engine places every pane, so print the rows as text instead.
    if (headless) return { text: d.panel().rows.map((r: any) => r.text).join('\n') }
    userPanel = true
    const opened = await $.ui.open({ id: PANE, title: 'nxy' })
    await showStatus($)
    try { await $.ui.invalidate('ui.render') } catch { /* optional */ }
    if (opened?.isPlaced) return { text: 'nxy panel opened' }
    return { text: d.panel().rows.map((r: any) => r.text).join('\n') }
  }).catch(($, e, next) => next(e))

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
      else await d.step('turn')
      await sync($)
    } catch (err) { await fail($, 'turn.complete', err) }
    return ran
  })

  on('ui.render', { component: 'Pane', requestId: PANE }, async ($, e) => {
    const { Box, Text, Button } = $.ui.resolve(e)
    const v = driver?.view() ?? driver?.panel() ?? null
    if (!v) return <Text dimColor>nxy: nothing running.</Text>
    const color = (tone: string) => (tone === 'ok' ? 'green' : tone === 'error' ? 'red' : undefined)
    return (
      <Box flexDirection="column">
        {v.rows.map((r: any) => (
          <Text color={color(r.tone)} dimColor={r.tone === 'dim'}>{r.text}</Text>
        ))}
        {v.buttons.map((b: string, i: number) => (
          <Button key={b} label={b} hotkey={String(i + 1)} onPress={() => onPress($, b)} />
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
