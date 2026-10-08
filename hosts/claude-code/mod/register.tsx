import type { Register, Timer } from 'claude-code'

import { COMPOSE_SECTION, readModConfig } from '../../../core/orchestrator.mjs'
import { durText, layoutOf, lifecycleLayout, panelText } from '../../../core/panel.mjs'
import { PALETTE as C, gaugeCells, progressCells } from '../../../core/raster.mjs'
import { createDriver } from './driver.mjs'

const PANE = 'nxy'
const SPIN = '⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏'
// A view tone as a palette colour; `info` is the plain text colour.
const TONE: Record<string, string> = { ok: C.green, running: C.blue, error: C.red, dim: C.muted, info: C.text }
const toneColor = (tone?: string) => TONE[tone ?? 'info'] ?? C.text

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
let frame = 0 // the animation frame (spinners, the progress shimmer)
let tickTimer: Timer | null = null // the one animation timer (Timer.cancel is part of the clock API)

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

// The animation clock. It redraws the pane only while an agent runs (`panel().animated`) and stops
// itself when the pane closes. Started from session.start, command.run and after each open: never from a render.
function stopTick() {
  const t = tickTimer
  tickTimer = null
  try { t?.cancel() } catch { /* already stopped */ }
}
function startTick($: any) {
  stopTick() // never two ticks: a restart replaces the one running
  const timer: Timer = $.clock.every(250, async () => {
    try {
      const up = (await $.ui.panes()).some((p: any) => p.id === PANE)
      if (!up) { if (tickTimer === timer) stopTick(); else timer.cancel(); return }
      if (!driver?.panel()?.animated) return
      frame++
      await $.ui.invalidate('ui.render')
    } catch { /* optional */ }
  })
  tickTimer = timer
}
async function openPane($: any) {
  const opened = await $.ui.open({ id: PANE, title: 'nxy' })
  try { startTick($) } catch { stopTick() }
  return opened
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
    if (!isUp) await openPane($)
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
        startTick($)
      }
      // A reload runs session.start again with the pane maybe still open: restart the clock then.
      if (!headless && (await $.ui.panes()).some((p: any) => p.id === PANE)) startTick($)
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
    const opened = await openPane($)
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
    try {
      return draw($, e, driver?.panel() ?? null)
    } catch (err) {
      const { Text } = $.ui.resolve(e) as any
      return <Text color={C.red}>{`nxy render error: ${String((err as any)?.message ?? err)}`}</Text>
    }
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

// The pane: a centred column of at most 78 cells. Labels wrap, they are never cut.
function draw($: any, e: any, v: any) {
  const { Box, Text, Button, Raster } = $.ui.resolve(e) as any
  if (!v) return <Text color={C.muted}>nxy: nothing running.</Text>
  const L = layoutOf(Number.isFinite(e.props?.bodyColumns) ? e.props.bodyColumns : 48)
  const { W, CW, narrow, stacked, tileW } = L
  const spin = (k = 0) => SPIN[(frame + k) % SPIN.length] ?? '●'
  const live = frame % 12 < 6
  const press = (id: string) => () => onPress($, id)

  const Pill = (p: { text: string; bg: string }) => <Text backgroundColor={p.bg} color={C.ink} bold>{` ${p.text} `}</Text>
  // A button on a keycap.
  const Keycap = (p: { id: string; label: string; hotkey?: string; bg?: string }) => (
    <Box key={`k:${p.id}`} backgroundColor={p.bg ?? C.keycap} paddingX={1}>
      <Button plain hotkey={p.hotkey} label={p.label} onPress={press(p.id)} />
    </Box>
  )
  // TITLE ──────────── right
  const Heading = (p: { title: string; color: string; right?: string }) => {
    const right = p.right ?? ''
    const rule = Math.max(2, CW - p.title.length - right.length - 3)
    return (
      <Box flexDirection="row" marginTop={1}>
        <Text bold color={p.color}>{p.title}</Text>
        <Text color={C.faint}>{` ${'─'.repeat(rule)} `}</Text>
        <Text color={C.muted}>{right}</Text>
      </Box>
    )
  }
  // A card: label, big value, a Raster gauge, hints.
  const Tile = (p: { tile: any }) => {
    const t = p.tile
    const gw = Math.max(4, tileW - 4)
    const stops = t.id === 'cache' ? [C.cyan, C.pink] : t.id === 'context' ? [C.green, C.amber, C.red] : [C.cyan, C.pink]
    return (
      <Box flexDirection="column" borderStyle="round" borderColor={C.card} paddingX={1} width={tileW}>
        <Text color={C.muted}>{t.label}</Text>
        <Text bold color={t.tone === 'dim' ? C.muted : toneColor(t.tone === 'info' ? undefined : t.tone)}>{t.value}</Text>
        {t.bar ? <Raster key={`g-${t.id}`} columns={gw} rows={1} cells={gaugeCells(gw, t.bar.limit > 0 ? t.bar.used / t.bar.limit : 0, stops)} /> : null}
        {(t.hints ?? []).map((hint: string, i: number) => <Text key={`${t.id}${i}`} color={C.muted} wrap="wrap">{hint}</Text>)}
      </Box>
    )
  }
  // A read-only setting: label and value, stacked when narrow.
  const Setting = (p: { label: string; value: string; tone?: string }) => (
    <Box flexDirection={stacked ? 'column' : 'row'} columnGap={1}>
      <Box width={stacked ? undefined : 20}><Text color={C.muted}>{p.label}</Text></Box>
      <Text color={toneColor(p.tone)} wrap="wrap">{p.value}</Text>
    </Box>
  )
  const AgentLine = (p: { a: any; i: number }) => (
    <Text key={`ag${p.i}`} wrap="wrap">
      <Text color={C.blue}>{`${spin(p.i * 5)} `}</Text>
      <Text color={C.text}>{p.a.role}</Text>
      <Text color={C.muted}>{`${p.a.batch != null ? ` · lote ${p.a.batch}` : ''}${p.a.sinceMs != null ? ` · ${durText(p.a.sinceMs)}` : ''}`}</Text>
    </Text>
  )

  // ---- header: pill, repo · branch, state; below it the progress strip ----
  const hd = v.header
  const where = [hd.repo, hd.branch].filter(Boolean).join('  ·  ')
  const header = (
    <Box flexDirection="column" width={CW}>
      <Box flexDirection="row" justifyContent="space-between" backgroundColor={C.band} width={CW}>
        <Box flexDirection="row" flexShrink={1}>
          <Pill text="◆ nxy" bg={C.brand} />
          <Text backgroundColor={C.band} color={C.muted} wrap="wrap">{narrow ? ` ${hd.branch || hd.repo}` : `  ${where}`}</Text>
        </Box>
        <Text backgroundColor={C.band} color={hd.pill?.tone === 'running' ? (live ? C.blue : C.cyan) : toneColor(hd.pill?.tone)} bold>
          {`${`${hd.pill?.glyph ?? ''} ${hd.pill?.text ?? ''}`.trim()} `}
        </Text>
      </Box>
      {hd.progress?.length ? <Raster key="progress" columns={CW} rows={1} cells={progressCells(CW, hd.progress, frame)} /> : null}
    </Box>
  )

  // ---- tabs: a segmented control ----
  const tabs = (
    <Box flexDirection="row" flexWrap="wrap" marginTop={1} columnGap={1}>
      {v.tabs.map((t: any) => (t.active
        ? <Pill key={`tab:${t.id}`} text={`${t.hotkey} ${t.label}`} bg={C.violet} />
        : <Box key={`tab:${t.id}`} paddingX={1}><Button plain hotkey={t.hotkey} label={t.label} dimColor onPress={press(`tab:${t.id}`)} /></Box>))}
    </Box>
  )

  // ---- the blocks of the active tab ----
  const nodeColor = (s: string) => (s === 'done' ? C.green : s === 'red' ? C.red : s === 'current' ? (live ? C.blue : C.cyan) : C.faint)
  const nodeGlyph = (s: string) => (s === 'done' ? '●' : s === 'current' ? '◉' : s === 'red' ? '✘' : '○')
  const lifecycle = (steps: any[]) => {
    const lay = lifecycleLayout(steps, CW)
    if (lay.mode === 'track') {
      return (
        <Box flexDirection="column" marginTop={1}>
          <Box flexDirection="row">
            {steps.map((s: any, i: number) => (
              <Text key={`n${i}`}>
                <Text color={nodeColor(s.state)} bold>{nodeGlyph(s.state)}</Text>
                <Text color={s.state === 'done' ? C.green : C.faint}>{i < steps.length - 1 ? (s.state === 'done' ? '━' : '─').repeat(lay.span - 1) : ''}</Text>
              </Text>
            ))}
          </Box>
          <Box flexDirection="row">
            {steps.map((s: any, i: number) => (
              <Box key={`l${i}`} width={lay.span}>
                <Text color={s.state === 'pending' ? C.muted : nodeColor(s.state)} bold={s.state === 'current'} wrap="wrap">{s.label}</Text>
              </Box>
            ))}
          </Box>
        </Box>
      )
    }
    return (
      <Box flexDirection="column" marginTop={1}>
        {lay.lines.map((line: any[], li: number) => (
          <Box key={`w${li}`} flexDirection="row">
            {line.map((s: any, si: number) => (
              <Text key={`s${si}`} color={s.state === 'pending' ? C.muted : nodeColor(s.state)} bold={s.state === 'current'}>
                {`${si ? ' › ' : ''}${nodeGlyph(s.state)} ${s.label}`}
              </Text>
            ))}
          </Box>
        ))}
      </Box>
    )
  }
  const stateGlyph = (s: string) => (s === 'done' ? { t: '✔', c: C.green } : s === 'running' ? { t: spin(), c: C.blue } : s === 'red' ? { t: '✘', c: C.red } : { t: '○', c: C.faint })
  const sevColor = (sev?: string) => (/alta|high|crit|blocker/i.test(sev ?? '') ? C.red : /media|medium/i.test(sev ?? '') ? C.amber : C.muted)

  const block = (b: any, bi: number) => {
    switch (b.type) {
      case 'hero': {
        if (b.status !== 'plan') {
          return (
            <Box key={`b${bi}`} flexDirection="column" marginTop={1}>
              <Text bold color={C.text} wrap="wrap">{b.headline}</Text>
              {b.handoff ? <Text color={C.muted}>{`Handoff: ${b.handoff.value}`}</Text> : null}
            </Box>
          )
        }
        const running = b.batch?.state === 'running'
        return (
          <Box key={`b${bi}`} flexDirection="column" marginTop={1}>
            <Box flexDirection="row" justifyContent="space-between">
              <Box flexShrink={1}>
                <Text wrap="wrap">
                  <Text color={C.blue} bold>{running ? `${spin()} ` : ''}</Text>
                  <Text bold color={C.text}>{b.headline}</Text>
                </Text>
              </Box>
              {b.elapsedMs != null ? <Text color={C.muted}>{` ${durText(b.elapsedMs)}`}</Text> : null}
            </Box>
            {b.steps.length ? lifecycle(b.steps) : null}
            {b.meta ? <Text color={C.muted} wrap="wrap">{b.meta}</Text> : null}
          </Box>
        )
      }
      case 'tiles':
        return (
          <Box key={`b${bi}`} flexDirection="row" flexWrap="wrap" columnGap={1} marginTop={1}>
            {b.tiles.map((t: any) => <Tile key={t.id} tile={t} />)}
          </Box>
        )
      case 'now':
        return (
          <Box key={`b${bi}`} flexDirection="column">
            <Heading title="Ahora" color={C.blue} />
            {b.agents.length ? b.agents.map((a: any, i: number) => <AgentLine key={`a${i}`} a={a} i={i} />) : <Text color={C.muted}>nada corriendo</Text>}
            {b.next ? <Text color={C.muted} wrap="wrap">{`→ ${b.next}`}</Text> : null}
          </Box>
        )
      case 'actions':
        return (
          <Box key={`b${bi}`} flexDirection="column">
            <Heading title="Acciones" color={C.cyan} />
            <Box flexDirection="row" flexWrap="wrap" columnGap={1}>{b.actions.map((a: any) => <Keycap key={a.id} id={a.id} label={a.label} hotkey={a.hotkey} />)}</Box>
          </Box>
        )
      case 'goal':
        return (
          <Box key={`b${bi}`} flexDirection="column" marginTop={1}>
            <Text color={C.muted}>Objetivo</Text>
            <Text color={C.text} wrap="wrap">{b.text}</Text>
          </Box>
        )
      case 'batches': {
        const done = b.items.filter((x: any) => x.state === 'done').length
        return (
          <Box key={`b${bi}`} flexDirection="column">
            <Heading title="Lotes" color={C.violet} right={`${done} de ${b.items.length} listo${done === 1 ? '' : 's'}`} />
            {b.items.map((x: any) => {
              const ic = stateGlyph(x.state)
              return (
                <Box key={`pb${x.n}`} flexDirection="column" backgroundColor={x.open ? C.band : undefined} width={CW}>
                  <Box flexDirection="row">
                    <Box width={4}><Text color={ic.c} bold>{`${ic.t} ${x.n}`}</Text></Box>
                    <Box width={CW - 4 - 9}><Button plain label={x.title} key={`batch:${x.n}`} onPress={press(`batch:${x.n}`)} /></Box>
                    <Box width={9} justifyContent="flex-end"><Text color={C.muted}>{x.durationMs != null ? durText(x.durationMs) : ''}</Text></Box>
                  </Box>
                  {x.note ? <Box paddingLeft={4}><Text color={C.red} wrap="wrap">{x.note}</Text></Box> : null}
                  {x.open ? (
                    <Box flexDirection="column" paddingLeft={4} paddingBottom={1}>
                      {x.files.map((f: string) => <Text key={f} color={C.cyan} wrap="wrap">{`· ${f}`}</Text>)}
                      <Text wrap="wrap"><Text color={C.muted}>acepta  </Text><Text color={C.text}>{x.accept}</Text></Text>
                      <Text color={toneColor(x.verdict.tone)} wrap="wrap">{`${x.state === 'running' ? `${spin()} ` : ''}${x.verdict.text}`}</Text>
                    </Box>
                  ) : null}
                </Box>
              )
            })}
          </Box>
        )
      }
      case 'suite':
        return (
          <Box key={`b${bi}`} flexDirection="row" flexWrap="wrap" marginTop={1} columnGap={3}>
            {b.rows.map((r: any) => (
              <Text key={r.label}><Text color={C.muted}>{`${r.label} `}</Text><Text color={toneColor(r.tone)}>{r.value}</Text></Text>
            ))}
          </Box>
        )
      case 'checkpoint':
        return (
          <Box key={`b${bi}`} flexDirection="column">
            <Heading title={`Checkpoint 2 · review ${b.id ?? ''}`.trim()} color={C.amber} right={`${b.picked} elegidos`} />
            {b.findings.map((f: any) => {
              const locW = Math.min(f.loc.length, 30)
              const inline = !!f.loc && !narrow && CW - 5 - 5 - locW >= 24
              return (
                <Box key={f.id} flexDirection="column" marginBottom={1}>
                  <Box flexDirection="row">
                    <Box width={5}><Button plain label={f.picked ? '◼' : '◻'} key={`pick:${f.id}`} onPress={press(`pick:${f.id}`)} /></Box>
                    <Box width={5}><Text bold color={sevColor(f.severity)}>{f.id}</Text></Box>
                    <Box flexGrow={1} flexShrink={1}><Text color={C.text} wrap="wrap">{f.title ?? ''}</Text></Box>
                    {inline ? <Box width={locW + 1} justifyContent="flex-end"><Text color={C.faint}>{f.loc}</Text></Box> : null}
                  </Box>
                  {!inline && f.loc ? <Box paddingLeft={10}><Text color={C.faint} wrap="wrap">{f.loc}</Text></Box> : null}
                  {f.cause ? <Box paddingLeft={10}><Text color={C.muted} wrap="wrap">{f.cause}</Text></Box> : null}
                </Box>
              )
            })}
            {b.fix ? <Box flexDirection="row" columnGap={1}><Keycap id={b.fix.id} label={b.fix.label} hotkey={b.fix.hotkey} /></Box> : null}
          </Box>
        )
      case 'agents':
        return (
          <Box key={`b${bi}`} flexDirection="column">
            <Heading title="Agentes" color={C.green} right={`${b.agents.length} corriendo`} />
            {b.agents.length ? b.agents.map((a: any, i: number) => <AgentLine key={`a${i}`} a={a} i={i} />) : <Text color={C.muted}>ninguno corriendo</Text>}
          </Box>
        )
      case 'kv':
        return (
          <Box key={`b${bi}`} flexDirection="column" marginTop={1}>
            {b.rows.map((r: any) => <Setting key={r.label} label={r.label} value={String(r.value ?? '')} tone={r.tone} />)}
          </Box>
        )
      case 'note':
        return <Box key={`b${bi}`} marginTop={1}><Text color={toneColor(b.tone)} wrap="wrap">{b.text}</Text></Box>
      default:
        return null
    }
  }

  return (
    <Box flexDirection="column" width={W} alignItems="center" paddingTop={1}>
      <Box flexDirection="column" width={CW}>
        {header}
        {tabs}
        {v.ask ? (
          <Box flexDirection="column" borderStyle="round" borderColor={v.ask.tone === 'error' ? C.red : C.amber} paddingX={1} marginTop={1}>
            <Text color={C.text} wrap="wrap">{v.ask.question}</Text>
            <Box flexDirection="row" flexWrap="wrap" columnGap={1}>{v.ask.buttons.map((b: any) => <Keycap key={b.id} id={b.id} label={b.label} hotkey={b.hotkey} />)}</Box>
          </Box>
        ) : null}
        {v.blocks.map(block)}
        {v.output ? (
          <Box flexDirection="column" borderStyle="round" borderColor={C.card} paddingX={1} marginTop={1}>
            <Box flexDirection="row" justifyContent="space-between">
              <Text bold color={C.text}>{v.output.title}</Text>
              <Button key="dismiss" plain label="x" hotkey={v.output.dismiss?.hotkey ?? 'd'} onPress={press('dismiss')} />
            </Box>
            {v.output.lines.map((line: string, li: number) => (
              <Text key={`o${li}`} color={v.output.running ? C.muted : C.text} wrap="wrap">{line}</Text>
            ))}
          </Box>
        ) : null}
        <Box flexDirection="row" flexWrap="wrap" columnGap={1} marginTop={1}>
          {(v.keys ?? []).map((k: any) => <Keycap key={k.id} id={k.id} label="refrescar" hotkey={k.hotkey} />)}
        </Box>
      </Box>
    </Box>
  )
}
