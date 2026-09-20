> Plan de diseño aprobado el 2026-09-16 y ejecutado en esta versión. Se conserva como registro de decisiones; el estado actual del plugin está en el README.

# nxy.dev — plugin Claude Code: calidad, velocidad, menos tokens, consumo visible — Fase 1

## Contexto

`nxy.dev` es un repo vacío (README + LICENSE) con tres repos de referencia clonados sin trackear
(`OmniRoute/`, `gentle-ai/`, `gentle-shell/`). Objetivo: un **plugin standalone de Claude Code,
agnóstico de lenguaje/framework**, con cuatro pilares: bajo gasto de tokens, calidad de código,
velocidad y visibilidad del consumo. Hoy el usuario usa `feature-dev` (Sonnet, effort alto, 1M ctx)
y paga por suscripción y por API.

**Fase 1 = dos módulos: `metrics` (visibilidad real del consumo: sesión, modelo, subagente, skill,
tendencia) y `filter` (filtrado determinístico de salidas de Bash forzado por hook, con RTK como motor).** Índice determinístico, scouts Haiku,
workflow por fases y review por riesgo quedan para fases posteriores; el layout les deja lugar.

### Decisiones tomadas por el usuario en esta sesión
- Plugin **monolítico único**; cada módulo con toggle en config para medir su aporte aislado.
- Empezar por **medición + filtrado**; después índice/scouts; después workflow por fases.
- Prompts internos en **inglés**; README/docs en **español**.
- Diseño agnóstico: nada en el core asume Java/Angular/Solid.
- Filtro: **reescritura transparente forzada por hook** (no sugerencia por prompt).
- Motor de filtrado: **RTK** (`rtk-ai/rtk`, Apache-2.0, binario Rust, Windows nativo desde v0.37.2).
  nxy lo envuelve con hook propio + métricas. **No** se corre `rtk init -g` (evita doble hook).
  Motor Node propio: incremento opcional al final, no en el camino crítico.
- Build Java: **Maven** (cubierto por RTK). Gradle no está en RTK → no se hace ahora.
- Instalación de RTK/ripgrep: **detectar y guiar** (`/nxy:filter status`), nunca instalar solo.
- **Requisito: funciona en Windows y Linux** (macOS de rebote). Sin binarios propios; Node cross-platform.
- Debe servir para un **repo existente y para arrancar de cero**. Fase 1 no mira el código, así que es
  indiferente; en fase 2 el índice sobre repo vacío devuelve "nada que indexar" y los scouts se saltan.
- Medición: el usuario **usa el plugin a conciencia y mira si el consumo baja** con el tiempo. No hay
  comparación formal con `feature-dev` ni módulo baseline. El eje es `/nxy:trend` (evolución por
  día/semana). Los datos son **reales**: Claude Code escribe en cada respuesta el `usage` que devuelve
  la API (lo que se factura); nxy sólo lee esos transcripts. No es `/usage` (cuota de suscripción):
  nxy muestra tokens/USD por sesión, modelo, subagente y skill; el statusline además muestra el % de
  5h/7d que Claude Code le pasa por stdin.
- Lenguaje: **JavaScript ESM (`.mjs`) con JSDoc + `// @ts-check`**, `tsc --noEmit` en CI. Sin build,
  sin deps de runtime (typescript/eslint sólo devDeps). Motivo: los hooks corren en cada Bash y el
  arranque cuenta; TS requeriría build o `--experimental-strip-types` (con flag en Node 22).
- Consumo de subagentes: visible en `/nxy:stats` (por tipo y por agente) y sumado en el statusline.

### Roadmap (qué viene después de fase 1)
- **Fase 2 — no explorar a ciegas**: índice determinístico (evaluar con datos `@colbymchenry/codegraph`
  [MCP, tercero] vs índice propio CLI+skill), scouts Haiku sólo para lo que el índice no responde,
  modelo+effort por fase en frontmatter de agentes.
- **Fase 3 — flujo**: brief → research → plan → apply → review con archivos en disco y subagente por
  fase; review escalado por riesgo determinístico; memoria de decisiones/convenciones.
- El usuario ve el consumo desde fase 1 (statusline + `/nxy:stats` + `/nxy:trend`).

### Posiciones tomadas en el debate (fases posteriores; no bloquean fase 1)
- Contexto limpio para implementar → **subagente por fase** con plan en disco, no `/clear`.
- Índice → preferencia inicial **CLI + skill** (0 tokens hasta usarse) sobre MCP, porque las tool
  descriptions de un MCP se pagan cada turno. Pero `@colbymchenry/codegraph` (lo que usa gentle-ai)
  se **evalúa con datos en fase 2**: si su ahorro en exploración supera su costo fijo, se adopta.
- Objetivos de ahorro (40%/50%) → no se fijan sin línea base. Dato honesto de OmniRoute: 60-90% en logs
  de tests/builds, **≈0% en lecturas limpias**. Si `feature-dev` gasta en lecturas y no en Bash, el
  filtro solo no alcanza y el ahorro grande vendrá de fase 2 (índice + no releer).
- "confidence: 0.94" → no; evidencia `path:line` + fragmento verificable por script.
- Superficie always-on del plugin **< 2K tokens** (gentle-ai gasta ~15K/turno: anti-ejemplo).

## Hechos verificados (condicionan el diseño)

- Claude Code **2.1.274**. Windows 11. **Sin Python** (stub de la Store). **Node 22.20**. `rtk` no
  está en PATH hoy. Todo script es Node ESM `.mjs` **sin dependencias npm**; tests con `node --test`.
- Transcripts `~/.claude/projects/<slug>/<session>.jsonl` (slug = `cwd.replace(/[^A-Za-z0-9]/g,'-')`).
  Líneas assistant: `requestId`, `message.id`, `message.model`, `effort`, `attributionSkill`, `cwd`,
  `gitBranch`, `message.usage.{input_tokens, cache_creation_input_tokens, cache_read_input_tokens,
  output_tokens, output_tokens_details.thinking_tokens, cache_creation.{ephemeral_1h_input_tokens,
  ephemeral_5m_input_tokens}, service_tier}`. Repetidas por streaming → dedupe por `requestId`
  quedándose con la de mayor `output_tokens`.
- Subagentes: `<slug>/<session>/subagents/agent-<id>.jsonl` + `agent-<id>.meta.json`
  `{agentType, description, toolUseId, spawnDepth}`; líneas con `isSidechain:true`, `agentId`.
- `toolUseResult` de Bash = `{stdout, stderr, interrupted}`; de Read = `{file:{filePath, numLines…}}`
  → "líneas devueltas por Bash" y "archivos leídos" son medibles desde el transcript.
- Statusline stdin (2.1.274): `model.{id,display_name}`, `session_id`, `transcript_path`, `cwd`,
  `effort.level`, `context_window.{used_percentage, context_window_size, total_input_tokens,
  total_output_tokens}`, `rate_limits.{five_hour,seven_day}.{used_percentage,resets_at}`,
  `prompt_cache.{hit_ratio,…}`, `version`. **No trae costo** → nxy lo calcula del transcript.
- Hooks: `PreToolUse` puede devolver `hookSpecificOutput.updatedInput` (reescribe el comando) y
  `permissionDecision`. `PostToolUse` sólo `additionalContext`. Docs locales:
  `~/.claude/plugins/marketplaces/claude-plugins-official/plugins/plugin-dev/skills/hook-development/SKILL.md`.
- Frontmatter de agentes: `model: haiku|sonnet|opus`, `effort: low|medium|high|xhigh|max`
  (Haiku sin effort; Sonnet sin xhigh). Reservado para fase 2.
- Plugins oficiales Apache-2.0 reutilizables:
  `~/.claude/plugins/marketplaces/claude-plugins-official/plugins/session-report/skills/session-report/analyze-sessions.mjs`
  (875 líneas: dedupe, `agentIdToType`, cache breaks, rollups por skill/subagente) → **fork** para
  `scripts/lib/transcripts.mjs`, con atribución en `NOTICE`.
- `feature-dev` (línea base): command de 125 líneas + 3 agentes Sonnet sin `effort`; lanza 2-3
  explorers, 2-3 architects, 3 reviewers en Sonnet y el principal relee "todos los archivos
  identificados". Ahí está el gasto.

## RTK — contrato de integración (verificado en `src/hooks/rewrite_cmd.rs`, `hook_cmd.rs`, README)
- `rtk rewrite "<cmd>"` imprime el comando reescrito. Exit `0` = reescrito+allow, `1` = sin
  equivalente, `2` = deny, `3` = reescrito pero preguntar. Maneja `&&`/`;`/pipes/prefijos env, salta
  heredocs y comandos ya prefijados. Es la entrada pensada para hooks de terceros.
- `rtk <cmd>` filtra; `rtk proxy <cmd>` passthrough con tracking; `rtk recall <hash>` devuelve la
  salida completa (`~/.local/share/rtk/recall.db`); `rtk gain --all --format json` exporta ahorros;
  `rtk discover` lista comandos que se escaparon. Config `~/.config/rtk/config.toml`
  (`[hooks] exclude_commands`, `[retriever] mode=sqlite`). Windows: winget `rtk-ai.rtk`; requiere `rg`.
- Cubre: ls/cat/grep/find/diff, git, gh, jest/vitest/playwright/pytest/go test/cargo test/rspec,
  eslint/biome/tsc/prettier/cargo/ruff/golangci-lint/sbt/**mvn/mvnd**, pnpm/uv/pip/bundle,
  docker/kubectl, curl/wget. No lista gradle.

## Qué se toma de cada referencia
- **OmniRoute** → sólo ideas para el motor builtin opcional (`open-sse/services/compression/engines/rtk/
  {lineFilter,smartTruncate,deduplicator,commandDetector,splitCompositeCommand,filterSchema}.ts`,
  `filters/*.json`, guardas `isDocumentLikeRead`, regex prioritario
  `/error|failed|exception|traceback|TS\d{4}|FAIL|✖/i`) y la fórmula de costo cache-aware de
  `src/lib/usage/costCalculator.ts`. No usar como proxy.
- **gentle-shell** → presentación: `model · effort ⟡ ctx ▰▰▰▰▱▱▱▱ 45% ⟡ $9.49`, umbrales 80/95,
  degradación por ancho, `?` cuando no hay dato, `formatTokens` (842/4.2k/412k/1.3M), usage ausente =
  `unavailable` nunca 0.
- **gentle-ai** → ideas para fase 2: preset modelo+effort con tabla de validez; skills doble cuerpo
  (`model-capable`/`model-small`); umbrales numéricos de delegación (1-3 archivos inline, 4+ → mapper);
  envelope de retorno fijo + "última acción = texto"; clasificador de riesgo por evidencia de paths
  (`auth|security|webhook|payments|.github/workflows|*.sh` → HIGH; 0/1/4 revisores). No copiar:
  CLAUDE.md de 46 KB, gate por escaneo de transcript, maquinaria RDD, SDD de 9 fases.

## Layout final

```
nxy.dev/
├─ .claude-plugin/plugin.json      manifest
├─ hooks/hooks.json                SessionStart, PreToolUse(Bash), PostToolUse(Bash)
├─ commands/
│  ├─ stats.md                     /nxy:stats → scripts/metrics/stats.mjs
│  ├─ trend.md                     /nxy:trend → scripts/metrics/trend.mjs
│  ├─ statusline.md                /nxy:statusline → snippet para settings.json
│  └─ filter.md                    /nxy:filter status|on|off
├─ skills/nxy-filter/SKILL.md      lazy: cómo recuperar salida completa (rtk recall / NXY_RAW=1)
├─ agents/README.md                nota sobre model/effort frontmatter (fase 2)
├─ scripts/
│  ├─ lib/
│  │  ├─ config.mjs                defaults ⊕ ~/.nxy/config.json ⊕ <proj>/.nxy/config.json ⊕ env
│  │  ├─ paths.mjs                 nxyDir, projectsDir, projectSlug, ensureDir
│  │  ├─ jsonl.mjs                 appendJsonl (atómico), readJsonl
│  │  ├─ shell.mjs                 splitTopLevel, isHeredoc, hasUnsafeConstruct
│  │  ├─ permissions.mjs           matchesAllowRule(cmd) sobre permissions.allow/deny de settings
│  │  ├─ transcripts.mjs           parser (fork de analyze-sessions.mjs)
│  │  ├─ pricing.mjs + pricing.json costFor(model, usage); precios desde skill claude-api
│  │  └─ format.mjs                fmtTokens, fmtUsd, table, gauge
│  ├─ hooks/
│  │  ├─ sessionstart.mjs          resuelve motor, cachea, no imprime nada
│  │  ├─ pretooluse-bash.mjs       decide + reescribe vía rtk
│  │  └─ posttooluse-bash.mjs      escribe fila de métricas
│  ├─ filter/
│  │  ├─ engine.mjs                resolveEngine → {engine, rtkPath, rtkVersion, rtkHookDetected}
│  │  ├─ decide.mjs                decide(cmd, ctx) → skip|rewrite (puro, testeado por tabla)
│  │  └─ rtk.mjs                   rtkRewrite(cmd), rtkGain()
│  └─ metrics/
│     ├─ stats.mjs                 --session last|<id> --since 7d --by model|agent|skill|filter --json
│     ├─ trend.mjs                 --since 30d --by day|week: evolución del consumo, columna tooling
│     └─ statusline.mjs            stdin JSON → una línea; cache incremental por tamaño del transcript
├─ tests/*.test.mjs + tests/fixtures/   node --test
├─ jsconfig.json                   `checkJs`, `strict`, `module: NodeNext` → `tsc --noEmit` en CI
├─ package.json                    sólo `scripts` (test, typecheck, lint) y devDeps; sin deps de runtime
├─ .github/workflows/ci.yml        node --test + tsc en ubuntu-latest y windows-latest
├─ nxy.config.json                 defaults documentados
├─ NOTICE                          atribuciones Apache-2.0 (Anthropic session-report, rtk-ai/rtk)
├─ .gitignore                      /OmniRoute/ /gentle-ai/ /gentle-shell/ .nxy/ node_modules/
├─ README.md (ES) y docs/ (ES)     instalación, módulos, comandos, cómo leer las métricas
```
Datos en runtime (no commiteados): `<proj>/.nxy/{config.json, metrics/filter.jsonl}`,
`~/.nxy/{config.json, cache/engine.json, cache/statusline-<session>.json}`.
Los repos de referencia se quedan donde están, ignorados por `.gitignore`.

## Manifests y config

`plugin.json`:
```json
{ "name": "nxy", "version": "0.1.0",
  "description": "Menos tokens y más control en Claude Code: métricas reales de consumo por sesión, modelo y subagente, y filtrado de salidas de comandos.",
  "author": { "name": "Matias Nicolas Añaños" }, "license": "Apache-2.0" }
```
commands/skills/agents/hooks se detectan por convención.

`hooks.json` (cada comando `node "${CLAUDE_PLUGIN_ROOT}/scripts/hooks/<x>.mjs"`, timeout 10):
`SessionStart` (sin matcher), `PreToolUse` matcher `Bash`, `PostToolUse` matcher `Bash`.
Todo hook: leer stdin completo → `try/catch` → nunca bloquear (fail-open, exit 0). Sólo imports de
`node:fs/path/child_process`; objetivo < 80 ms por hook.

`nxy.config.json` (defaults; override `~/.nxy/config.json` → `<proj>/.nxy/config.json`, deep-merge;
env `NXY_FILTER=0|1`, `NXY_ENGINE=rtk|off` ganan):
```json
{ "modules": { "metrics": true, "filter": true },
  "filter": { "engine": "auto", "excludeCommands": [], "onlyCommands": [],
              "autoAllowWhenOriginalAllowed": true },
  "metrics": { "projectsDir": null, "subscription": true,
               "statusline": { "cacheTtlMs": 2000, "color": true, "preset": "vivid", "layout": "line", "brand": "◆ nxy", "ctxWarnTokens": 100000,
                               "ctxCritTokens": 200000, "turnWarnUsd": 1, "turnCritUsd": 3, "promptCacheTtlMin": 5, "theme": {} }, "cacheBreakThreshold": 100000 } }
```
`engine: auto` = `rtk` si está en PATH, si no `off` con aviso único en stderr (nunca degradar en
silencio). `modules.filter=false` → PreToolUse sale sin output; PostToolUse sigue registrando filas
con `engine:"off"` (así `/nxy:trend` puede mostrar el volumen de Bash aun con el filtro apagado).

## Flujo del módulo `filter`

**SessionStart**: `loadConfig` → `resolveEngine()` (`spawnSync('rtk',['--version'])`; en win32 también
`rtk.exe`; detectar hook propio de RTK grepeando `rtk hook claude` en `~/.claude/settings.json` y
`<proj>/.claude/settings.json`) → escribir `~/.nxy/cache/engine.json`. Sin stdout.

**PreToolUse** (`pretooluse-bash.mjs`): stdin `{session_id, cwd, permission_mode, tool_input:{command}}`.
`decide()` en orden, puro y testeado por tabla (≥40 casos):
1. Escape → skip: empieza con `NXY_RAW=1 `, contiene `# raw`, primer token ∈ `excludeCommands`,
   o `onlyCommands` no vacío y primer token ∉.
2. Ya envuelto → skip: empieza con `rtk `/`rtk.exe `.
3. Nunca tocar → skip: heredoc, `cd` solo, `git commit|rebase|push|add -p`, interactivos
   (`vim|nano|less|top|ssh|docker exec -it|claude`), `sudo`, backgrounded `&`, `$(`/backticks o
   redirecciones `>`/`2>` a nivel top, lecturas de `.nxy/`.
4. `rtkHookDetected` → skip con razón `rtk-hook-present` (RTK ya reescribe; nxy sólo mide).
5. Motor rtk: `spawnSync(rtkPath, ['rewrite', cmd], {shell:false, timeout:2000})` (argv, sin shell →
   sin quoting en Windows). Exit 0 → `{command: stdout.trim(), allow: true}`; 3 → `allow:false`;
   1/2/timeout → skip.
Salida si rewrite:
```json
{ "hookSpecificOutput": { "hookEventName": "PreToolUse",
    "updatedInput": { "...tool_input", "command": "<rewritten>" },
    "permissionDecision": "allow", "permissionDecisionReason": "nxy: original matched an allow rule" },
  "suppressOutput": true }
```
`permissionDecision` sólo si `autoAllowWhenOriginalAllowed` y el comando **original** matchea una
regla `Bash(prefix:*)`/`Bash(exact)` de `permissions.allow` (user+project+local); cualquier `deny` → skip.

**PostToolUse** (`posttooluse-bash.mjs`): stdin `{tool_input:{command}, tool_response:{stdout,stderr}}`
→ fila en `<proj>/.nxy/metrics/filter.jsonl`:
`{ts, session_id, engine, command_kind, command_head (80 chars, secretos enmascarados), filtered_lines,
filtered_chars, raw_lines|null, raw_chars|null, est_tokens_saved|null, rtk_recall_hash|null, reason}`.
`command_kind` = primer token normalizado (`npm test`→`npm-test`, `mvn`→`maven`) por tabla de ~30.
En modo rtk, `raw_*` se completa en `stats` cruzando con `rtk gain --history --format json`
(timestamp+comando) y se reporta como "rtk-estimated". En modo off/skip: `raw = filtered`, saved = 0.

## Flujo del módulo `metrics`

**`lib/transcripts.mjs`** (fork de `analyze-sessions.mjs`; reusar dedupe uuid/requestId, detección de
prompts, cache breaks; sacar HTML/by_day; estado por llamada en vez de mapas globales):
```
projectSlug(cwd); listSessions({cwd, since}); resolveSession('last'|id|prefix)
parseSession(ref, {since, includeSubagents, pricing}) → SessionStats
iterCalls(lines) → Call{requestId, ts, model, usage, effort, agentId, agentType, skill, promptKey}
aggregate(calls, keyFn) → Map<key, UsageAgg>;  readIncremental(path, state) (tail-read para statusline)
UsageAgg   {calls, input, cacheWrite5m, cacheWrite1h, cacheRead, output, thinking, cacheHitPct, contextPeak, usd|null}
SessionStats {sessionId, project, wallMs, activeMs, turns, usage, byModel, byAgentType, bySkill, byPrompt,
              tools:{calls{}, filesRead[], bash:{calls, resultLines, resultChars}}, cacheBreaks[], agents[]}
```
Atribución de skill: `attributionSkill` → tool_use `Skill` → `<command-name>` en el prompt → null.
Tipo de agente: `agent-<id>.meta.json` → mapa `agentIdToType` desde tool_result → null.

**`pricing.json`**: USD/1M por modelo `{input, output, cache_write_5m, cache_write_1h, cache_read}` con
`aliases` (`-[1m]`, sufijo de fecha). **Valores `null` como placeholder: llenar desde el skill
`claude-api` al implementar, no inventar.** `costFor` = cache-aware, recalculado al leer, nunca
persistido. Etiqueta `USD-equiv` cuando `metrics.subscription`.

**`stats.mjs`**: default = proyecto actual, última sesión → bloque cabecera (turnos, wall/active,
llamadas, input dividido uncached/cache-write/cache-read + hit %, output+thinking, pico de contexto,
USD-equiv), tabla por modelo, **tabla por tipo de subagente y lista de agentes individuales**
(`agentType`, `description`, tokens, USD, duración), tabla por skill, bloque FILTER desde `filter.jsonl`
(filas, raw→filtered, tokens ahorrados, por `command_kind`), cache breaks. `--json` para tooling externo.

**`statusline.mjs`**: cache `~/.nxy/cache/statusline-<session>.json` `{offset, seen, usage, size,
agents:{[file]:{size, usage}}}`; si el tamaño del transcript principal y de los `subagents/*.jsonl`
(un `readdirSync` + `statSync`) no cambió y TTL vigente → render desde cache; si no, `readIncremental`
de lo que cambió. Render: `<model> · <effort> ⟡ ctx ▰▰▰▰▱▱▱▱ 45% ⟡ 412k/8.1k (+3 agents) ⟡ $1.23 ⟡
cache 91% ⟡ 5h 62%`. Los totales incluyen subagentes. Campos faltantes → `?`. Objetivo < 30 ms caliente.

**`trend.mjs`** (el eje de "¿mejora con el tiempo?"): `--since 30d --by day|week|session [--project|--all]`
→ tabla por período con: sesiones, tokens totales, input uncached / cache-read / output, USD-equiv,
cache hit %, % de tokens en subagentes, tokens por turno, líneas devueltas por Bash, tokens ahorrados
por el filtro, y columna `nxy` (sí/no: la sesión corrió con el plugin activo, detectado por las filas
de `filter.jsonl` y por `attributionSkill`). Todo sale de los transcripts ya existentes: las sesiones
anteriores a instalar nxy aparecen igual. `--json` para graficar después.

**Portabilidad Windows/Linux** (requisito): rutas con `node:path` + `os.homedir()`; detección de `rtk`
con y sin `.exe`; config de RTK en `~/.config/rtk/config.toml` (Linux) o su equivalente (Windows:
`%APPDATA%\rtk\config.toml`, macOS: `~/Library/Application Support/rtk/`) — leerla sólo para `status`;
`spawn` siempre por argv (`shell:false`); tests `node --test` corren en ambos SO (GitHub Actions
matrix `ubuntu-latest` + `windows-latest`); el spike del paso 1 se repite en Linux (WSL o máquina).

## Commands y skill (inglés; descripciones ≤ 25 tokens; always-on total ≈ 250 tokens)
- `/nxy:stats` — corre `stats.mjs $ARGUMENTS` y pega la salida; interpretación sólo si se pide.
- `/nxy:trend` — corre `trend.mjs $ARGUMENTS`; muestra la tabla por día/semana/sesión.
- `/nxy:statusline` — imprime el snippet `{"statusLine":{"type":"command","command":"node \"<abs>/scripts/metrics/statusline.mjs\""}}`;
  pide confirmación antes de mergear en `~/.claude/settings.json`.
- `/nxy:filter` — `status` (motor, versión rtk, `rg`, conflicto de hook, comando winget exacto si falta),
  `on|off` (toggle en `<proj>/.nxy/config.json`).
- `skills/nxy-filter/SKILL.md` (~150 tokens) — cuando una salida vino filtrada: usar `rtk recall <hash>`
  o Read/Grep sobre el archivo; `NXY_RAW=1 <cmd>` sólo si un Grep dirigido no alcanza; nunca apagar el
  filtro globalmente para ver una salida.

## Pasos de implementación (incrementos chicos, cada uno verificable)

| # | Incremento | Verificación |
|---|---|---|
| 0 | `.gitignore`, `NOTICE`, `plugin.json`, `hooks.json` vacío; instalar local (`/plugin marketplace add <path>` o `claude --plugin-dir`) | `claude --debug` muestra el plugin cargado |
| 1 | **Spike `updatedInput`**: PreToolUse que reescribe sólo `echo nxy-probe` → `echo nxy-probe-rewritten` con `permissionDecision:"allow"` | En Claude, `echo nxy-probe` imprime `nxy-probe-rewritten`, lanzado desde PowerShell y desde Git Bash. Manual: `echo '{"tool_name":"Bash","tool_input":{"command":"echo nxy-probe"}}' \| node scripts/hooks/pretooluse-bash.mjs`. **Si se ignora → parar y reevaluar** |
| 2 | `lib/config.mjs`, `paths.mjs`, `jsonl.mjs`, `shell.mjs` + tests | `node --test tests/` |
| 3 | `filter/decide.mjs` reglas 1-4 + tests por tabla (≥40 casos) | `node --test` |
| 4 | `filter/engine.mjs` + `sessionstart.mjs` | stdin `{}` → `~/.nxy/cache/engine.json`; hook < 100 ms en `claude --debug` |
| 5 | `filter/rtk.mjs` + rama rtk de `decide` + `lib/permissions.mjs` (tests con `rtk` falso en fixtures que imita exit codes) | Instalar RTK (`winget install rtk-ai.rtk`, verificar que su hook NO esté), en Claude `git status` sale filtrado con hash de recall; `/nxy:filter status` reporta rtk |
| 6 | `posttooluse-bash.mjs` | Filas en `.nxy/metrics/filter.jsonl` para rtk, off y skip |
| 7 | `lib/transcripts.mjs` + `pricing.mjs` + fixtures (mini transcript con requestIds duplicados, un subagente + meta.json) + tests de dedupe/agentType/skill/filesRead/bashLines | `node --test` |
| 8 | `metrics/stats.mjs` + `commands/stats.md` | `/nxy:stats` sobre la sesión actual; `--json` contra fixture |
| 9 | `metrics/statusline.mjs` + `commands/statusline.md` | `echo '<sample>' \| node statusline.mjs`; instalar; medir < 30 ms caliente |
| 10 | `metrics/trend.mjs` + `commands/trend.md` | `/nxy:trend --since 30d` sobre el historial real muestra las sesiones previas y las nuevas con nxy |
| 11 | `skills/nxy-filter`, `commands/filter.md`, README (ES), chequeo de presupuesto always-on; CI `node --test` + `tsc --noEmit` en ubuntu+windows; repetir spike del paso 1 en Linux | estimación chars/4 de descripciones < 2K tokens; CI verde en ambos SO |
| 12 | *(opcional, decidir con datos)* motor builtin Node portando OmniRoute (`pipeline/detect/catalog/run/test`, filtros JSON + `maven.json`) | `node scripts/filter/test.mjs` verde; `NXY_ENGINE=builtin` |

## Verificación end-to-end
1. Sesión nueva en un repo con tests (p. ej. `CursosSpring-Projects-taskmanager`): pedir "run the tests".
   Ver el comando reescrito a `rtk mvn test`, la salida filtrada, la fila en `filter.jsonl`, y el
   statusline moviéndose.
2. `/nxy:stats` muestra tokens por modelo/agente/skill y el bloque FILTER con ahorro.
3. `/nxy:filter off` → mismo pedido → la salida llega cruda y la fila dice `engine:"off"`.
4. `/nxy:trend --since 30d` sobre el historial real muestra las sesiones previas y las nuevas con nxy
   en la misma tabla, con tokens, USD-equiv y % en subagentes por período. Lanzar un subagente en la
   sesión y comprobar que `/nxy:stats` lo lista con su tipo y consumo y que el statusline suma `+1 agent`.
5. Mismo flujo en Linux (WSL o máquina): hooks, statusline y `rtk` detectado.

## Riesgos y preguntas abiertas
1. `updatedInput` en Bash con 2.1.274: el paso 1 lo verifica antes de todo. RTK depende de lo mismo
   (evidencia fuerte de que funciona).
2. Shell con que Claude Code corre los hooks en Windows (cmd vs Git Bash): `node "${CLAUDE_PLUGIN_ROOT}/…"`
   con barras normales funciona en ambos; confirmar en paso 1 desde ambos lanzadores.
3. Permisos tras reescritura: `rtk git status` ya no matchea `Bash(git status:*)` → nxy emite `allow`
   sólo si el original matcheaba. Verificar sintaxis de reglas contra docs.
4. Doble hook con RTK nativo: dos `updatedInput` en paralelo es indefinido → `rtkHookDetected` hace que
   nxy ceda y `/nxy:filter status` avise.
5. Raw vs filtered en modo rtk: sólo se mide filtered en PostToolUse; raw viene de `rtk gain --history`
   (campos a inspeccionar cuando RTK esté instalado). Reportar como estimado.
6. Statusline: schema puede cambiar entre versiones → lectura defensiva, `?` si falta.
7. Costo de arranque de Node ~40-70 ms × 2 hooks por Bash: aceptable; medir; si molesta, `"async": true`
   en PostToolUse (verificar que el campo exista en esta versión).
8. `spawnSync('rtk')` sin shell en Windows: libuv agrega `.exe`; si no, usar la ruta absoluta cacheada.
9. Precios: placeholders `null`, llenar desde skill `claude-api`.
