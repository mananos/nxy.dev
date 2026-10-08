import type { Register } from 'claude-code'

import { COMPOSE_SECTION, readModConfig } from '../../../core/orchestrator.mjs'
import { barText, fitSteps, kFmt, panelText } from '../../../core/panel.mjs'
import { createDriver } from './driver.mjs'

const PANE = 'nxy'
// A view tone as a theme key; `info` keeps the default text color.
const TONE: Record<string, string | undefined> = { ok: 'success', running: 'warning', error: 'error', dim: 'subtle', info: undefined }

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
// The text has no leading `nxy`: the engine prefixes the plugin's name itself. Seen live on
// 2026-10-07, when `nxy ● ready` was drawn as `⚠ nxy: nxy ● ready`.
async function showStatus($: any) {
  try {
    if (!driver) return
    const text = driver.status()
    if (text === lastStatus) return
    await $.ui.status(text)
    lastStatus = text // only once it was sent: a throw above keeps the old value, so the next sync retries
  } catch { /* optional */ }
}
async function usageOf($: any) {
  try {
    const u = await $.session.usage()
    const { tokens, percent, window } = u.context ?? {}
    // tokens and percent are absent before the first response of the session
    if (!Number.isFinite(tokens) || !Number.isFinite(percent)) return null
    let costUsd: number | null = null
    try { costUsd = Number.isFinite(u.cost?.usd) ? u.cost.usd : null } catch { /* optional */ }
    let model: string | null = null
    try { model = (await $.session.model()) || null } catch { /* optional */ }
    return { tokens, percent, window: Number.isFinite(window) ? window : null, costUsd, model }
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
    driver.setUsage(await usageOf($))
    // A launcher runs on its own lane: draw its running box first, then wait for the output.
    const pressed = driver.press(element)
    await sync($)
    await pressed
    await sync($)
  } catch (err) { await fail($, 'ui.press', err) }
}

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    try {
      headless = !e.isInteractive
      driver = createDriver(api($), { cwd: e.cwd, sessionId: await $.session.id(), headless })
      await driver.start()
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
    if (headless) return { text: panelText(d.panel()).join('\n') }
    userPanel = true
    const opened = await $.ui.open({ id: PANE, title: 'nxy' })
    await showStatus($)
    try { await $.ui.invalidate('ui.render') } catch { /* optional */ }
    if (opened?.isPlaced) return { text: 'nxy panel opened' }
    return { text: panelText(d.panel()).join('\n') }
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
    const v = driver?.panel() ?? null
    if (!v) return <Text dimColor>nxy: nothing running.</Text>
    const cols: number = Number.isFinite(e.props?.bodyColumns) ? e.props.bodyColumns : 60
    const compact = cols < 40
    const color = (tone?: string) => TONE[tone ?? 'info']
    const glyphOf = (state: string) => (state === 'done' ? '✔' : state === 'running' || state === 'current' ? '●' : state === 'red' ? '✘' : '○')
    const stepGlyph = (state: string) => (state === 'done' ? '✔' : state === 'current' ? '◉' : state === 'red' ? '✘' : '○')
    const stepColor = (state: string) => (state === 'done' ? 'success' : state === 'red' ? 'error' : state === 'pending' ? 'subtle' : undefined)
    const btn = (b: any) => <Button key={b.id} plain label={b.label} hotkey={b.hotkey} onPress={() => onPress($, b.id)} />

    const row = (r: any, i: number) => {
      const value = r.bar ? (
        <Box flexDirection="column">
          <Box flexDirection="row" gap={1}>
            <Text color={color(r.bar.tone)}>{barText(r.bar.used, r.bar.limit, compact ? 6 : 10)}</Text>
            <Text>{`${kFmt(r.bar.used)} / ${kFmt(r.bar.limit)}`}</Text>
          </Box>
          {r.note ? <Text dimColor>{r.note}</Text> : null}
        </Box>
      ) : r.steps ? (
        <Box flexDirection="column">
          {fitSteps(r.steps, Math.max(10, cols - (compact ? 0 : 12))).map((line: any[], li: number) => (
            <Box key={`l${li}`} flexDirection="row">
              {line.map((s: any, si: number) => (
                <Text key={`s${si}`} color={stepColor(s.state)} bold={s.state === 'current'} dimColor={s.state === 'pending'}>
                  {`${si ? ' › ' : ''}${stepGlyph(s.state)} ${s.label}`}
                </Text>
              ))}
            </Box>
          ))}
        </Box>
      ) : (
        <Box flexDirection="column">
          <Text color={color(r.tone)}>{String(r.value ?? '')}</Text>
          {r.note ? <Text dimColor>{r.note}</Text> : null}
        </Box>
      )
      return compact ? (
        <Box key={`r${i}`} flexDirection="column">
          <Text dimColor>{r.label}</Text>
          {value}
        </Box>
      ) : (
        <Box key={`r${i}`} flexDirection="row" gap={1}>
          <Box width={11}><Text dimColor>{r.label}</Text></Box>
          {value}
        </Box>
      )
    }

    return (
      <Box flexDirection="column">
        <Box flexDirection="row" gap={2}>
          {v.tabs.map((t: any) => (
            <Button key={`tab:${t.id}`} plain label={t.label} hotkey={t.hotkey} dimColor={!t.active} onPress={() => onPress($, `tab:${t.id}`)} />
          ))}
        </Box>
        <Box flexDirection="row" justifyContent="space-between">
          <Text bold>{v.header.title}</Text>
          <Text color={color(v.header.pill?.tone)}>{`${v.header.pill?.glyph ?? ''} ${v.header.pill?.text ?? ''}`.trim()}</Text>
        </Box>
        <Text dimColor>{'─'.repeat(Math.max(1, cols))}</Text>
        {v.ask ? (
          <Box flexDirection="column" borderStyle="round" borderColor={v.ask.tone === 'error' ? 'error' : 'warning'} paddingX={1}>
            <Text>{v.ask.question}</Text>
            <Box flexDirection="row" flexWrap="wrap" gap={2}>{v.ask.buttons.map(btn)}</Box>
          </Box>
        ) : null}
        {v.sections.map((s: any, si: number) => (
          <Box key={`sec${si}`} flexDirection="column" marginTop={si ? 1 : 0}>
            {s.title ? <Text bold>{s.title}</Text> : null}
            {(s.rows ?? []).map(row)}
            {(s.batches ?? []).map((b: any) => (
              <Box key={`b${b.n}`} flexDirection="row" gap={1}>
                <Text color={b.state === 'done' ? 'success' : b.state === 'red' ? 'error' : b.state === 'running' ? 'warning' : 'subtle'}>{glyphOf(b.state)}</Text>
                <Text dimColor={b.state === 'pending'}>{`${b.n}. ${b.title}`}</Text>
                {b.note ? <Text dimColor>{b.note}</Text> : null}
              </Box>
            ))}
            {s.actions?.length ? <Box flexDirection="row" flexWrap="wrap" gap={2} marginTop={1}>{s.actions.map(btn)}</Box> : null}
          </Box>
        ))}
        {v.output ? (
          <Box flexDirection="column" borderStyle="round" paddingX={1} marginTop={1}>
            <Box flexDirection="row" justifyContent="space-between">
              <Text bold>{v.output.title}</Text>
              <Button key="dismiss" plain label="x" hotkey={v.output.dismiss?.hotkey ?? 'd'} onPress={() => onPress($, 'dismiss')} />
            </Box>
            {v.output.lines.map((line: string, li: number) => (
              <Text key={`o${li}`} dimColor={!!v.output.running}>{line}</Text>
            ))}
          </Box>
        ) : null}
        <Box flexDirection="row" marginTop={1}>
          <Button key="refresh" plain dimColor label="refrescar" hotkey="r" onPress={() => onPress($, 'refresh')} />
        </Box>
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
