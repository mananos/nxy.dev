import type { Register, Timer } from 'claude-code'

import { COMPOSE_SECTION, readModConfig } from '../../../core/orchestrator.mjs'
import { durText, layoutOf, lifecycleLayout, panelText } from '../../../core/panel.mjs'
import { PALETTE as C, mix, gaugeCells, progressCells } from '../../../core/raster.mjs'
import { COLUMN_WIDTH, money } from '../../../core/statsview.mjs'
import { createDriver } from './driver.mjs'

const PANE = 'nxy'
const SPIN = '⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏'
// A view tone as a palette colour; `info` is the plain text colour.
const TONE: Record<string, string> = { ok: C.green, running: C.blue, error: C.red, bad: C.red, warn: C.amber, dim: C.muted, info: C.text }
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
  agent: { spawn: (args: any) => $.agent.spawn(args), list: () => $.agent.list() },
  session: {
    append: (args: any) => $.session.append(args),
    send: (args: any) => $.session.send(args),
    messages: (args: any) => $.session.messages(args),
  },
  prompt: { submit: (args: any) => $.prompt.submit(args) },
  settings: { read: () => $.settings.read() },
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
    let limits: { kind: string; percentUsed: number; resetsAt: string }[] = []
    try {
      limits = (u.rateLimits ?? []).map((l: any) => ({ kind: l.kind, percentUsed: l.percentUsed, resetsAt: l.resetsAt }))
    } catch { /* optional */ }
    return { tokens, percent, window: Number.isFinite(window) ? window : null, costUsd, model, limits }
  } catch { return null }
}
// The two cache timers (warn at TTL-60 s, expire at the TTL): replaced on every main-thread answer.
let cacheTimers: Timer[] = []
function cancelCacheTimers() {
  const ts = cacheTimers
  cacheTimers = []
  for (const t of ts) { try { t.cancel() } catch { /* already fired */ } }
}
// Armed from turn.complete only: no polling, nothing starts from a render.
function scheduleCache($: any) {
  cancelCacheTimers()
  try {
    const plan = driver?.cachePlan()
    if (!plan || plan.silent) return
    const arm = (ms: number, kind: 'warn' | 'expire') => {
      cacheTimers.push($.clock.after(Math.max(0, ms), async () => {
        try {
          const text = driver?.cacheNotice(kind)
          if (text) await $.ui.toast(text, { timeoutMs: 8000 })
          await sync($)
        } catch { /* optional */ }
      }))
    }
    if (plan.warnInMs != null && plan.warnInMs > 0) arm(plan.warnInMs, 'warn')
    if (plan.expireInMs > 0) arm(plan.expireInMs, 'expire')
  } catch { /* optional */ }
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
  let beat = 0
  const timer: Timer = $.clock.every(250, async () => {
    try {
      const up = (await $.ui.panes()).some((p: any) => p.id === PANE)
      if (!up) { if (tickTimer === timer) stopTick(); else timer.cancel(); return }
      const view = driver?.panel()
      beat++
      // The agent list is polled here only (every 2 s), and only while some agent runs.
      if (beat % 8 === 0 && driver?.agentsLive()) await driver.syncAgents()
      // The cache countdown on Home moves once a second: one tick in four.
      if (!view?.animated && !(view?.clockTicks && beat % 4 === 0)) return
      if (view?.animated) frame++
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
  // Pending toasts ("resumen listo"), shown once.
  try {
    for (const text of driver.drainNotices()) await $.ui.toast(text, { timeoutMs: 8000 })
  } catch { /* optional */ }
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

// The message field's Enter: the driver sends it (or asks first for an orchestrator's implementer).
async function onSend($: any, text: string): Promise<void> {
  try {
    if (!driver) return
    const sent = driver.submitMessage(text)
    await sync($)
    await sent
    await sync($)
  } catch (err) { await fail($, 'ui.input', err) }
}

// A Memoria / launcher field's Enter: the driver runs it (search, locate or launch).
async function onInput($: any, field: string, text: string): Promise<void> {
  try {
    if (!driver) return
    const run = driver.submitInput(field, text)
    await sync($)
    await run
    await sync($)
  } catch (err) { await fail($, 'ui.input', err) }
}

// A Features Select pick (base branch / existing branch): the driver validates it against the last list.
async function onPick($: any, pick: string, value: string): Promise<void> {
  try {
    if (!driver) return
    const run = pick === 'base' ? driver.pickBase(value) : driver.pickBranch(value)
    await sync($)
    await run
    await sync($)
  } catch (err) { await fail($, 'ui.input', err) }
}

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    try {
      headless = !e.isInteractive
      driver = createDriver(api($), { cwd: e.cwd, sessionId: await $.session.id(), headless })
      await driver.start()
      driver.setConfig(await readConfig($))
      try {
        const s: any = await $.settings.read()
        driver.setSettings(s)
      } catch { /* optional */ }
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

  // A subagent that nxy did not start: the book learns its id and model once the engine has started it.
  on('agent.spawn', async ($, e, next) => {
    const out = await next(e)
    try {
      const id = (out as any)?.agentId
      const own = /^nxy-orch:/.test(String((e as any).description ?? ''))
      if (id && driver && !own) {
        driver.noteSpawn({
          id, type: (e as any).subagentType, description: (e as any).description, prompt: (e as any).prompt,
          model: (out as any).model, spawnedBy: 'other', at: Date.now(),
        })
      }
    } catch { /* optional */ }
    return out
  }).catch(($, e, next) => next(e))

  // Only observes: it never steps nor spawns, so the engine does not skip it for the driver's agents.
  on('tool.call', { agentId: /./ }, async ($, e, next) => {
    try {
      // The event is flat: the tool's arguments sit beside the reserved keys.
      const { tool, tool_use_id: _id, consent: _c, agentId, ...args } = e as any
      driver?.noteTool(agentId, { tool, input: args, at: Date.now() })
    } catch { /* optional */ }
    return next(e)
  }).catch(($, e, next) => next(e))

  on('turn.complete', async ($, e, next) => {
    const ran = await next(e)
    try {
      const d = await ensure($)
      if (e.agentId) d.noteAgentTurn(e)
      if (e.agentId) await d.onAgentDone(e.agentId, e.answer)
      else {
        const u = await usageOf($)
        d.setUsage(u)
        d.noteTurn({ usage: e.usage, agentId: e.agentId, now: Date.now(), costUsd: u?.costUsd ?? null })
        scheduleCache($)
        await d.step('turn')
      }
      await sync($)
    } catch (err) { await fail($, 'turn.complete', err) }
    return ran
  })

  // The user's prompt marks the turn start (needed to infer the cache TTL). Never changes the prompt.
  on('prompt.submit', async ($, e, next) => {
    try { driver?.noteTurnStart(Date.now()) } catch { /* optional */ }
    return next(e)
  }).catch(($, e, next) => next(e))

  // The turn's effort, once per turn (the classic hook payload carries it).
  on('classic.Stop', async ($, e, next) => {
    try { driver?.noteEffort(e.effort?.level) } catch { /* optional */ }
    return next(e)
  }).catch(($, e, next) => next(e))

  on('classic.PreModelSwitch', async ($, e, next) => {
    try {
      const usd = e.estimated_cache_write_usd
      if (e.prompt_cache_warm && Number.isFinite(usd) && usd >= 0.01) {
        await $.ui.toast(`Cambiar a ${e.to_model} reescribe la cache: ≈ $${usd.toFixed(2)}`, { timeoutMs: 8000 })
      }
    } catch { /* optional */ }
    return next(e)
  }).catch(($, e, next) => next(e))

  on('classic.PostModelSwitch', async ($, e, next) => {
    try {
      // A resume or an automatic change without a warm cache lost nothing: leave the cache state alone.
      if (!((e.source === 'resume' || e.source === 'auto') && !e.prompt_cache_warm)) {
        driver?.noteModelSwitch({ toModel: e.to_model, tokens: e.context_tokens, usd: e.estimated_cache_write_usd, ttl: e.cache_ttl })
        scheduleCache($)
        await sync($)
      }
    } catch { /* optional */ }
    return next(e)
  }).catch(($, e, next) => next(e))

  on('ui.render', { component: 'Pane', requestId: PANE }, async ($, e) => {
    try {
      driver?.setColumns(e.props?.bodyColumns)
      driver?.setViewAgent(e.props?.view?.agentId)
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
  const { Box, Text, Button, Raster, Input, Select } = $.ui.resolve(e) as any
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
  // Horizontal bars: label (never cut), a gauge, the amount.
  const BarRows = (p: { rows: any[]; labelWidth: number; color?: string }) => {
    const gw = Math.max(4, Math.min(24, CW - p.labelWidth - 16))
    return (
      <Box flexDirection="column">
        {p.rows.map((r: any, i: number) => {
          const col = p.color ?? r.color ?? C.cyan
          return (
            <Box key={`br${i}`} flexDirection="row" columnGap={1}>
              <Box width={p.labelWidth + 1}><Text color={C.text}>{r.label}</Text></Box>
              <Raster key={`bg${i}`} columns={gw} rows={1} cells={gaugeCells(gw, r.ratio, [col, col])} />
              <Text color={C.muted}>{r.text}</Text>
            </Box>
          )
        })}
      </Box>
    )
  }
  const AgentLine =(p: { a: any; i: number }) => (
    <Text key={`ag${p.i}`} wrap="wrap">
      <Text color={C.blue}>{`${spin(p.i * 5)} `}</Text>
      <Text color={p.a.nxy ? C.green : C.muted}>{p.a.role}</Text>
      <Text color={C.muted}>{`${p.a.batch != null ? ` · lote ${p.a.batch}` : ''}${p.a.elapsedMs != null ? ` · ${durText(p.a.elapsedMs)}` : ''}`}</Text>
      {p.a.activity ? <Text color={C.muted}>{` · ${p.a.activity}`}</Text> : null}
    </Text>
  )

  // nxy roles in green, agents Claude Code started on its own in grey.
  const roleColor = (a: any) => (a?.nxy ? C.green : C.muted)
  const doneGlyph = (a: any) => (a?.tone === 'error' ? '✘' : a?.tone === 'dim' ? '■' : '✔')

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

  // Memoria / launcher helpers: a field that clears after each submit (its key changes), and path:line rows.
  const stateColor = (s: string) => (s === 'error' ? C.red : s === 'ok' ? C.text : C.muted)
  const inputOf = (inp: any) => (
    <Box marginTop={1}>
      <Input key={inp.key} value="" placeholder={inp.placeholder} submitLabel={inp.submitLabel}
        onSubmit={(text: string) => { if (!inp.disabled) void onInput($, inp.field, text) }} />
    </Box>
  )
  const hitList = (rowsIn: any[]) => (rowsIn ?? []).map((r: any, ri: number) => (
    <Text key={`ht${ri}`} wrap="wrap"><Text color={C.cyan}>{r.loc}</Text><Text color={C.muted}>{r.rest ? `  ${r.rest}` : ''}</Text></Text>
  ))

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
      case 'agents': {
        const c = b.counts ?? { running: 0, done: 0, failed: 0 }
        return (
          <Box key={`b${bi}`} flexDirection="column">
            <Heading title="Agentes" color={C.green} right={`${c.running} corriendo · ${c.done} terminaron · ${c.failed} fallaron${c.stopped ? ` · ${c.stopped} detenidos` : ''}`} />
            {b.agents.length ? b.agents.map((a: any, i: number) => (
              <Box key={`ar${a.id}`} flexDirection="column">
                <Box flexDirection="row" columnGap={1}>
                  <Text color={a.live ? C.blue : toneColor(a.tone)}>{a.live ? spin(i * 5) : doneGlyph(a)}</Text>
                  <Text color={roleColor(a)}>{a.role ?? ''}</Text>
                  <Button plain label={a.label} key={`agent:${a.id}`} onPress={press(`agent:${a.id}`)} />
                  <Text color={C.muted}>{a.modelEffort ?? ''}</Text>
                  <Box flexGrow={1} justifyContent="flex-end"><Text color={C.muted}>{[a.elapsedMs != null ? durText(a.elapsedMs) : '', a.costText].filter(Boolean).join(' · ')}</Text></Box>
                </Box>
                {a.activity ? <Box paddingLeft={2}><Text color={C.muted} wrap="wrap">{a.activity}</Text></Box> : null}
              </Box>
            )) : <Text color={C.muted}>ningún agente en esta sesión</Text>}
            {b.more ? <Box flexDirection="row"><Button plain label={b.more.label} key={b.more.id} onPress={press(b.more.id)} /></Box> : null}
          </Box>
        )
      }
      case 'watching':
        return (
          <Box key={`b${bi}`} flexDirection="column">
            <Heading title="Mirando" color={C.cyan} />
            <Box flexDirection="row" columnGap={1}>
              <Text color={b.card.live ? C.blue : toneColor(b.card.tone)}>{b.card.live ? spin() : doneGlyph(b.card)}</Text>
              <Text color={roleColor(b.card)}>{b.card.role ?? ''}</Text>
              <Text color={C.text}>{b.card.label}</Text>
              <Text color={C.muted}>{b.card.modelEffort ?? ''}</Text>
            </Box>
            {b.card.activity ? <Text color={C.muted} wrap="wrap">{b.card.activity}</Text> : null}
            <Box flexDirection="row"><Button plain label={b.open.label} key={b.open.id} onPress={press(b.open.id)} /></Box>
          </Box>
        )
      case 'agent': {
        const card = b.card ?? {}
        const lines = (title: string, ls: string[], color: string) => (ls?.length ? (
          <Box flexDirection="column" marginTop={1}>
            <Text color={C.muted}>{title}</Text>
            {ls.map((line: string, li: number) => <Text key={`${title}${li}`} color={color} wrap="wrap">{line}</Text>)}
          </Box>
        ) : null)
        return (
          <Box key={`b${bi}`} flexDirection="column">
            <Heading title={card.label ?? 'Agente'} color={C.green} right={[card.modelEffort, card.elapsedMs != null ? durText(card.elapsedMs) : '', card.costText].filter(Boolean).join(' · ')} />
            <Text wrap="wrap">
              <Text color={roleColor(card)} bold>{card.role ?? ''}</Text>
              <Text color={card.live ? C.blue : toneColor(card.tone)}>{`${card.role ? ' · ' : ''}${card.live ? `${spin()} ` : ''}${card.state ?? ''}${b.batch != null ? ` · lote ${b.batch}` : ''}`}</Text>
            </Text>
            {card.activity ? <Text color={C.muted} wrap="wrap">{card.activity}</Text> : null}
            {lines('Prompt', b.prompt, C.muted)}
            {b.tools?.length ? (
              <Box flexDirection="column" marginTop={1}>
                <Text color={C.muted}>Herramientas</Text>
                {b.tools.map((t: any, ti: number) => <Text key={`t${ti}`} color={C.cyan} wrap="wrap">{`· ${t.label}${t.ago ? `  ${t.ago}` : ''}`}</Text>)}
              </Box>
            ) : null}
            {lines('Resultado', b.result, C.text)}
            {lines('Error', b.error, C.red)}
            {b.sent?.length ? (
              <Box flexDirection="column" marginTop={1}>
                <Text color={C.muted}>Mensajes enviados</Text>
                {b.sent.map((s: any, si: number) => (
                  <Text key={`s${si}`} wrap="wrap"><Text color={C.text}>{s.text}</Text><Text color={s.state === 'recibido' ? C.green : s.state === 'en cola' ? C.amber : C.red}>{`  ${s.state}`}</Text></Text>
                ))}
              </Box>
            ) : null}
            {b.canSend && b.composer ? (
              <Box marginTop={1}>
                <Input key={b.composer.key} value="" placeholder={b.composer.placeholder} submitLabel={b.composer.submitLabel} onSubmit={(text: string) => { void onSend($, text) }} />
              </Box>
            ) : null}
            <Box flexDirection="row" marginTop={1}><Keycap id={b.back.id} label={b.back.label} hotkey={b.back.hotkey} /></Box>
          </Box>
        )
      }
      case 'memsearch':
        return (
          <Box key={`b${bi}`} flexDirection="column" width={CW}>
            <Heading title="Memoria" color={C.violet} right={b.query ? `«${b.query}»` : undefined} />
            {inputOf(b.input)}
            {b.state === 'ok' ? b.rows.map((r: any) => (
              <Box key={`mr:${r.id}`} flexDirection="column" marginTop={1}>
                <Button plain label={r.text} key={r.press} onPress={press(r.press)} />
                {r.sub ? <Box paddingLeft={2}><Text color={C.muted} wrap="wrap">{r.sub}</Text></Box> : null}
              </Box>
            )) : <Text color={stateColor(b.state)} wrap="wrap">{`${b.state === 'loading' ? `${spin()} ` : ''}${b.message}`}</Text>}
            {b.recent ? <Box flexDirection="row"><Button plain label={b.recent.label} key={b.recent.id} onPress={press(b.recent.id)} /></Box> : null}
          </Box>
        )
      case 'memdetail':
        return (
          <Box key={`b${bi}`} flexDirection="column" width={CW}>
            <Heading title="Memoria" color={C.violet} right={b.id} />
            {b.state === 'ok'
              ? b.lines.map((line: string, li: number) => <Text key={`md${li}`} color={C.text} wrap="wrap">{line}</Text>)
              : <Text color={stateColor(b.state)} wrap="wrap">{`${b.state === 'loading' ? `${spin()} ` : ''}${b.message}`}</Text>}
            <Box flexDirection="row" marginTop={1}><Keycap id={b.back.id} label={b.back.label} hotkey={b.back.hotkey} /></Box>
          </Box>
        )
      case 'handoff':
        return (
          <Box key={`b${bi}`} flexDirection="column" width={CW}>
            <Heading title="Handoff" color={C.cyan} />
            <Text color={stateColor(b.state)} wrap="wrap">{`${b.state === 'loading' ? `${spin()} ` : ''}${b.text}`}</Text>
            {b.lines.map((line: string, li: number) => <Text key={`ho${li}`} color={C.muted} wrap="wrap">{line}</Text>)}
            <Box flexDirection="row"><Button plain label={b.button.label} key={b.button.id} onPress={press(b.button.id)} /></Box>
          </Box>
        )
      case 'locate':
        return (
          <Box key={`b${bi}`} flexDirection="column" width={CW}>
            <Heading title="Ubicar" color={C.cyan} right={b.ms != null ? `${b.ms} ms` : undefined} />
            {inputOf(b.input)}
            {b.state === 'ok' ? hitList(b.rows) : <Text color={stateColor(b.state)} wrap="wrap">{`${b.state === 'loading' ? `${spin()} ` : ''}${b.message}`}</Text>}
            {b.state === 'ok' || b.state === 'empty' ? <Text color={C.muted} wrap="wrap">{[b.note, ...b.degraded].filter(Boolean).join(' · ')}</Text> : null}
            {b.ask ? (b.ask.disabled || !b.ask.id
              ? <Text color={C.muted} wrap="wrap">{b.ask.label}</Text>
              : <Box flexDirection="row"><Button plain label={b.ask.label} key={b.ask.id} onPress={press(b.ask.id)} /></Box>) : null}
            {b.askNote ? <Text color={C.muted} wrap="wrap">{b.askNote}</Text> : null}
          </Box>
        )
      case 'launch':
        return (
          <Box key={`b${bi}`} flexDirection="column" width={CW}>
            <Heading title={b.title} color={C.green} />
            <Box flexDirection="row" columnGap={1}>
              {b.kinds.map((k: any) => (k.active
                ? <Pill key={k.id} text={k.label} bg={C.violet} />
                : <Box key={k.id} paddingX={1}><Button plain label={k.label} dimColor key={`kind:${k.id}`} onPress={press(k.id)} /></Box>))}
            </Box>
            <Text color={C.muted} wrap="wrap">{b.hint}</Text>
            {inputOf(b.input)}
            {b.question ? <Text color={C.muted} wrap="wrap">{`«${b.question}»`}</Text> : null}
            {b.card ? (
              <Box flexDirection="row" columnGap={1}>
                <Text color={b.card.live ? C.blue : toneColor(b.card.tone)}>{b.card.live ? spin() : doneGlyph(b.card)}</Text>
                <Text color={roleColor(b.card)}>{b.card.role ?? ''}</Text>
                <Text color={C.muted} wrap="wrap">{[b.card.state, b.card.elapsedMs != null ? durText(b.card.elapsedMs) : '', b.card.costText].filter(Boolean).join(' · ')}</Text>
              </Box>
            ) : null}
            {b.error ? <Text color={C.red} wrap="wrap">{b.error}</Text> : null}
            {hitList(b.hits)}
            {b.mems.map((m: any) => (
              <Box key={`lm:${m.id}`} flexDirection="column">
                <Button plain label={m.text} key={m.press} onPress={press(m.press)} />
                {m.sub ? <Box paddingLeft={2}><Text color={C.muted} wrap="wrap">{m.sub}</Text></Box> : null}
              </Box>
            ))}
            {b.raw.map((line: string, li: number) => <Text key={`lw${li}`} color={C.text} wrap="wrap">{line}</Text>)}
            {b.open ? <Box flexDirection="row"><Button plain label={b.open.label} key={b.open.id} onPress={press(b.open.id)} /></Box> : null}
          </Box>
        )
      case 'worktrees':
        return (
          <Box key={`b${bi}`} flexDirection="column" width={CW}>
            <Heading title={b.title} color={C.cyan} />
            {b.message ? <Text color={b.state === 'error' ? C.red : C.muted} wrap="wrap">{b.message}</Text> : null}
            {b.modes.length ? (
              <Box flexDirection="row" flexWrap="wrap" columnGap={1}>
                {b.modes.map((m: any) => <Button key={m.id} plain label={m.label} onPress={press(m.id)} />)}
              </Box>
            ) : null}
            {inputOf(b.input)}
            {b.select ? (
              <Select key={b.select.key} label={b.select.label} options={b.select.options} value={b.select.value}
                onSelect={(v: string) => { void onPick($, b.select.pick, v) }} />
            ) : null}
            {b.info.map((t: string, ii: number) => <Text key={`wi${ii}`} color={C.muted} wrap="wrap">{t}</Text>)}
            {b.busy.map((t: string, ii: number) => <Text key={`wb${ii}`} color={C.muted} dimColor wrap="wrap">{t}</Text>)}
            {b.error ? <Text color={C.red} wrap="wrap">{b.error}</Text> : null}
            {b.rows.map((r: any) => (
              <Box key={`wt:${r.id}`} flexDirection="column" marginTop={1}>
                <Box flexDirection="row" flexWrap="wrap" columnGap={1}>
                  <Text color={C.text} bold wrap="wrap">{r.name}</Text>
                  <Text color={C.cyan} wrap="wrap">{r.branch}</Text>
                  {r.tags.map((t: string, ti: number) => <Text key={`t${ti}`} color={C.violet} wrap="wrap">{`[${t}]`}</Text>)}
                </Box>
                {r.lines.map((t: string, li: number) => <Text key={`l${li}`} color={C.muted} wrap="wrap">{t}</Text>)}
                <Box flexDirection="row" flexWrap="wrap" columnGap={1}>
                  {r.buttons.map((x: any) => (x.disabled
                    ? <Text key={x.id} color={C.muted} dimColor wrap="wrap">{`${x.label} (${x.reason})`}</Text>
                    : <Button key={x.id} plain label={x.label} onPress={press(x.id)} />))}
                </Box>
              </Box>
            ))}
            {b.note ? <Text color={C.muted} wrap="wrap">{b.note}</Text> : null}
          </Box>
        )
      case 'rows':
        return (
          <Box key={`b${bi}`} flexDirection="column">
            <Heading title={b.title} color={C.cyan} right={b.right} />
            {b.rows.map((r: any, ri: number) => (
              <Box key={`rw${ri}`} flexDirection="column">
                <Box flexDirection={stacked ? 'column' : 'row'} columnGap={1}>
                  <Box width={stacked ? undefined : b.labelWidth}><Text color={C.muted} wrap="wrap">{r.label}</Text></Box>
                  <Box flexDirection="row" flexWrap="wrap" columnGap={2} flexShrink={1}>
                    {r.cells.map((x: any, xi: number) => {
                      if (x.prev) {
                        return (
                          <Box key={`c${xi}`} flexDirection="row" columnGap={1}>
                            <Button plain label="‹" onPress={press(x.prev)} />
                            <Text color={toneColor(x.tone)} wrap="wrap">{x.text}</Text>
                            <Button plain label="›" onPress={press(x.next)} />
                          </Box>
                        )
                      }
                      if (x.press) {
                        return (
                          <Box key={`c${xi}`} flexDirection="row" columnGap={1}>
                            {typeof x.on === 'boolean' ? <Text color={x.on ? C.green : C.muted}>{x.on ? '●' : '○'}</Text> : null}
                            <Keycap id={x.press} label={x.text} hotkey={x.hotkey} />
                          </Box>
                        )
                      }
                      return <Text key={`c${xi}`} color={toneColor(x.tone)} wrap="wrap">{x.text}</Text>
                    })}
                  </Box>
                </Box>
                {r.hint ? <Text color={toneColor(r.hint.tone)} wrap="wrap">{r.hint.text}</Text> : null}
              </Box>
            ))}
          </Box>
        )
      case 'segments':
        return (
          <Box key={`b${bi}`} flexDirection="row" flexWrap="wrap" marginTop={1} columnGap={3}>
            {b.items.map((s: any) => (
              <Text key={s.id}><Text color={C.muted}>{`${s.label} `}</Text><Text color={toneColor(s.tone)}>{s.value}</Text></Text>
            ))}
          </Box>
        )
      case 'figures':
        return (
          <Box key={`b${bi}`} flexDirection="column">
            <Heading title={b.title} color={C.cyan} />
            <Box flexDirection="row" flexWrap="wrap" columnGap={3}>
              {b.items.map((s: any) => (
                <Text key={s.id}><Text color={C.muted}>{`${s.label} `}</Text><Text color={toneColor(s.tone)}>{s.value}</Text></Text>
              ))}
            </Box>
          </Box>
        )
      case 'bars':
        return (
          <Box key={`b${bi}`} flexDirection="column">
            <Heading title={b.title} color={C.cyan} />
            <BarRows rows={b.rows} labelWidth={b.labelWidth} />
          </Box>
        )
      case 'windows':
        return (
          <Box key={`b${bi}`} flexDirection="column">
            <Heading title="Ventanas" color={C.cyan} />
            {b.rows.map((w: any) => {
              const gw = Math.max(4, Math.min(30, CW - 28))
              const col = toneColor(w.tone)
              return (
                <Box key={`w${w.kind}`} flexDirection="row" columnGap={1}>
                  <Box width={6}><Text color={C.muted}>{w.label}</Text></Box>
                  <Raster key={`wg-${w.kind}`} columns={gw} rows={1} cells={gaugeCells(gw, w.ratio, [col, col])} />
                  <Text color={toneColor(w.tone)}>{`${Math.round(w.percentUsed)}%`}</Text>
                  <Text color={C.muted}>{w.resetsInMs != null ? `· reinicia en ${durText(w.resetsInMs)}` : ''}</Text>
                </Box>
              )
            })}
          </Box>
        )
      case 'selectors':
        return (
          <Box key={`b${bi}`} flexDirection="column">
            <Heading title={b.title} color={C.violet} />
            <Box flexDirection="row" flexWrap="wrap" columnGap={1}>
              {b.groups.flatMap((g: any) => g.options.map((o: any) => (
                <Keycap key={o.id} id={o.id} label={o.label} bg={o.active ? (o.color ?? C.violet) : C.keycap} />
              )))}
            </Box>
          </Box>
        )
      case 'chart': {
        const ch = b.chart
        const dimColor = '#' + mix([C.faint, ch.color], 0.6).toString(16).padStart(6, '0')
        const total = [ch.totalUsd != null ? `total ${money(ch.totalUsd)}` : '', ch.avoidableUsd ? `evitable ${money(ch.avoidableUsd)}` : '', ch.kind === 'rows' ? '' : ch.note ?? ''].filter(Boolean).join(' · ')
        return (
          <Box key={`b${bi}`} flexDirection="column">
            <Text color={C.muted} wrap="wrap">{ch.hint}</Text>
            {total ? <Text color={C.muted} wrap="wrap">{total}</Text> : null}
            {ch.kind === 'rows' ? (
              <Box flexDirection="column">
                {ch.note ? <Text color={C.muted} wrap="wrap">{ch.note}</Text> : null}
                <BarRows rows={ch.rows} labelWidth={ch.labelWidth} color={ch.color} />
              </Box>
            ) : (
              <Box flexDirection="row" marginTop={1}>
                {ch.columns.map((c: any, ci: number) => (
                  <Box key={`c${ci}`} flexDirection="column" width={COLUMN_WIDTH} alignItems="center">
                    <Text color={c.last ? ch.color : C.muted} bold={c.last}>{c.text}</Text>
                    {c.bars.map((cell: string, ri: number) => <Text key={`r${ri}`} color={c.last ? ch.color : dimColor}>{cell.repeat(Math.max(1, COLUMN_WIDTH - 2))}</Text>)}
                    <Text color={c.last ? ch.color : C.muted}>{c.label}</Text>
                  </Box>
                ))}
              </Box>
            )}
          </Box>
        )
      }
      case 'summary':
        return (
          <Box key={`b${bi}`} flexDirection="column" borderStyle="round" borderColor={C.green} paddingX={1} marginTop={1}>
            <Text bold color={C.green}>Resumen del feature</Text>
            <Text color={C.text} wrap="wrap">
              {`${b.costUsd != null ? money(b.costUsd) : '—'}${b.pct != null ? ` · ${b.pct}% de la sesión` : ''}${b.tokens ? ` · ${b.tokens}` : ''}`}
            </Text>
            <Box flexDirection="row" flexWrap="wrap" columnGap={3}>
              {b.figures.map((s: any) => (
                <Text key={s.id}><Text color={C.muted}>{`${s.label} `}</Text><Text color={toneColor(s.tone)}>{s.value}</Text></Text>
              ))}
            </Box>
            {b.roles.rows.length ? <BarRows rows={b.roles.rows} labelWidth={b.roles.labelWidth} /> : null}
            {b.batches.map((x: any) => (
              <Text key={`sb${x.n}`}><Text color={C.muted}>{`lote ${x.n}  `}</Text><Text color={C.text}>{x.text}</Text></Text>
            ))}
            <Box flexDirection="row" marginTop={1}><Keycap id={b.hide.id} label={b.hide.label} /></Box>
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
