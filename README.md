<p align="center">
  <img src="resources/nxydev.png" alt="nxy.dev" width="320">
</p>

<p align="center">Plugin de Claude Code: calidad de código, velocidad, consumo de tokens bajo control y visibilidad de lo que se gasta.</p>

Sirve para cualquier lenguaje o framework, y tanto para un repo existente como para uno que arranca de cero. Funciona en Windows y Linux (macOS también), con Node 22+ y sin dependencias de runtime.

Se construye por fases, midiendo cada una antes de sumar la siguiente:

| Fase | Pilar principal | Estado |
| ---- | --------------- | ------ |
| 1 | **Visibilidad** del consumo real + **menos tokens** en salidas de comandos | actual · **v0.1.1** |
| 2 | **Velocidad**: no explorar a ciegas (índice determinístico del repo, scouts baratos, modelo y esfuerzo por fase) | próxima |
| 3 | **Calidad**: flujo por fases con contexto limpio, review escalado por riesgo, memoria de decisiones | después |

Fase 1 trae dos módulos:

| Módulo    | Qué hace                                                                                                                     |
| --------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `metrics` | Lee los transcripts que Claude Code ya escribe y muestra el consumo **real** por sesión, modelo, subagente y skill, la tendencia en el tiempo y un statusline en vivo. |
| `filter`  | Reescribe los comandos Bash a través de [RTK](https://github.com/rtk-ai/rtk) para que el modelo reciba la salida filtrada (tests, builds, git, logs) en vez del log completo. |

Los números no son estimaciones: cada respuesta de la API deja en el transcript el `usage` que Anthropic factura (input, cache write/read, output). nxy sólo lo lee. El único cálculo es el costo en USD (tokens × [tabla oficial de precios](https://platform.claude.com/docs/en/about-claude/pricing)); si pagás por suscripción se muestra como *equivalente*.

## Instalación

Requisitos: Node ≥ 22 en el PATH. Para el filtro, además `rtk` (y `rg`):

```
# Windows
winget install rtk-ai.rtk
winget install BurntSushi.ripgrep.MSVC
# Linux / macOS: https://github.com/rtk-ai/rtk#installation
```

**No corras `rtk init -g`**: nxy instala su propio hook y se lo pasa a `rtk`; si RTK instala el suyo los dos se pisan y nxy se retira (`/nxy:filter status` lo avisa).

Instalar el plugin (el repo es a la vez un marketplace de Claude Code):

```
# desde GitHub, dentro de Claude Code
/plugin marketplace add mananos/nxy.dev
/plugin install nxy@nxy-dev

# lo mismo desde la terminal
claude plugin marketplace add mananos/nxy.dev
claude plugin install nxy@nxy-dev
```

Queda instalado para tu usuario en todos los proyectos; `/plugin` lo lista y permite deshabilitarlo. Para actualizar: `claude plugin marketplace update nxy-dev` y reinstalar. Para verlo sin instalar (o para desarrollarlo): `claude --plugin-dir /ruta/al/clon`.

Ver qué agrega al contexto antes de instalar: `claude plugin details nxy@nxy-dev` (hoy: ~180 tokens always-on según la estimación de Claude Code).

Statusline (opcional; Claude Code sólo lo lee desde `~/.claude/settings.json`, por eso es un paso manual): `/nxy:statusline` imprime el snippet; `/nxy:statusline --apply` lo mergea con backup.

## Comandos

| Comando           | Qué muestra                                                                                                           |
| ----------------- | --------------------------------------------------------------------------------------------------------------------- |
| `/nxy:stats`      | La sesión actual (`last` para la anterior, o un id): tokens (uncached / cache write / cache read / output / thinking), cache hit, pico de contexto, USD; por modelo, por tipo de subagente, agente por agente, por skill; herramientas usadas, archivos leídos, líneas devueltas por Bash; filas del filtro; cache breaks. `--session <id>`, `--all`, `--json`. |
| `/nxy:trend`      | Evolución por día / semana / sesión, **de todos tus proyectos**: tokens, uncached, cache-r, output, hit %, % en subagentes, tokens por turno, líneas de Bash, comandos filtrados, USD, y si la sesión corrió con nxy. `--since 30d`, `--by week`, `--by model` (una fila por modelo: cuánto va a Opus/Sonnet/Haiku), `--here` (sólo este proyecto). |
| `/nxy:filter`     | `status` (motor, versión de rtk, rg, conflicto de hook, comandos de instalación), `on`, `off` (por proyecto).          |
| `/nxy:statusline` | Snippet para el statusline: `Opus 5 · high ⟡ ctx ▰▰▰▱▱▱▱▱ 41% ⟡ 44M→241k (+4 agents) ⟡ $34.74~ ⟡ cache 98% ⟡ 5h 62% · 7d 31%`. |

Salida exacta, sin que el modelo la retipee y sin gastar tokens: dentro de Claude Code escribí `! node "<ruta-al-plugin>/scripts/metrics/stats.mjs"` (el prefijo `!` ejecuta el comando y muestra su salida tal cual). Los scripts también corren fuera de Claude: `node scripts/metrics/stats.mjs --all`, `node scripts/metrics/trend.mjs --by week --since 90d --json`.

## Cómo funciona el filtro

1. `SessionStart` resuelve el motor una vez por sesión (¿está `rtk`? ¿tiene su propio hook instalado?) y lo cachea en `~/.nxy/cache/engine.json`.
2. `PreToolUse(Bash|PowerShell)` decide si reescribe. **Nunca** toca: comandos con `NXY_RAW=1` o `# raw`, ya envueltos en `rtk`, heredocs, `$(...)`, redirecciones a archivo (`> out.log`; `2>&1` sí se permite), `&` al final, `cd`/`export` solos, interactivos (`vim`, `ssh`, `docker exec -it`…), `sudo`, ni `git commit|push|rebase|merge|checkout|stash|reset|tag` — el plugin no ejecuta nada por su cuenta, sólo deja pasar. Lo demás va a `rtk rewrite "<cmd>"` y, si RTK tiene filtro, se reemplaza por `rtk <cmd>`.
   - Cadenas: `cd api && export JAVA_HOME=… && ./mvnw test` → `cd api && export JAVA_HOME=… && rtk mvn test` (RTK maneja `&&`, `;` y prefijos `VAR=valor`).
   - Ortografías que RTK no reconoce se normalizan sólo para preguntarle: `./mvnw.cmd`, `.gradlew.bat` → sin `./`; `npm test` → `npm run test`; `pnpm build` → `pnpm run build`. Un `| tail -n N` final se descarta al preguntar (la salida de RTK ya viene recortada). Si RTK no reescribe, el comando original corre intacto.
   - PowerShell: `Set-Location …; $env:JAVA_HOME = …; .mvnw.cmd test` → mismos prefijos + `rtk mvn test`. Scripts reales (`foreach`, pipes, variables) no se tocan.
   - Lo que RTK cubre hoy (v0.48): git, gh, ls/cat/grep/find, mvn/mvnw, gradle/gradlew, npm run/pnpm run/bun, jest/vitest/playwright/pytest/cargo/go test, eslint/tsc/prettier/biome, next/vite, docker, kubectl. No: `ng` global, `yarn`, `npm install`.
   - Medido en un proyecto Spring Boot: `./mvnw test` pasó de 84 líneas / 9.197 chars a 8 líneas / 319 chars (−96 %).
3. Si el comando original ya estaba permitido por tus reglas `permissions.allow` (`Bash(git status:*)`), el reescrito hereda ese permiso: no aparecen prompts nuevos. Si no había regla, Claude Code te pregunta por `rtk git status` como te habría preguntado por `git status` (y podés dejarlo permitido). Si el original está en `permissions.deny`, no se reescribe.
4. `PostToolUse(Bash|PowerShell)` (síncrono, ~100 ms por comando) registra una fila en `<proyecto>/.nxy/metrics/filter.jsonl`: motor, tipo de comando, líneas/chars devueltos, hash de `rtk recall`. Los secretos obvios se enmascaran antes de escribir.
5. Cuando Claude necesita la salida completa, el skill `nxy-filter` le indica `rtk recall <hash>` o, como último recurso, `NXY_RAW=1 <cmd>`.

## Configuración

Defaults en `nxy.config.json`; overrides en `~/.nxy/config.json` y `<proyecto>/.nxy/config.json` (deep-merge). Env: `NXY_FILTER=0|1`, `NXY_ENGINE=rtk|off`, `NXY_RTK_PATH`, `NXY_DEBUG=1` (los hooks son fail-open: ante cualquier error dejan pasar el comando; con `NXY_DEBUG` imprimen la causa en stderr).

```json
{
  "modules": { "metrics": true, "filter": true },
  "filter": { "engine": "auto", "excludeCommands": [], "onlyCommands": [], "autoAllowWhenOriginalAllowed": true },
  "metrics": { "projectsDir": null, "subscription": true, "statusline": { "cacheTtlMs": 2000 }, "cacheBreakThreshold": 100000 }
}
```

## Qué NO filtra

`Read`, `Grep` y `Glob` son herramientas internas de Claude Code: no pasan por Bash y ningún hook puede modificar lo que devuelven. Fase 1 las **mide** (`/nxy:stats`: archivos leídos, cantidad de `Read`/`Grep`, pico de contexto); reducirlas es el trabajo de la fase 2 (índice del repo para no explorar a ciegas).

## Presupuesto de contexto

Lo único que el plugin agrega a cada turno son las descripciones de 4 comandos y 1 skill: **~90–180 tokens** (estimación propia chars/4 vs. `claude plugin details`). No inyecta nada en CLAUDE.md ni define agentes en esta fase.

## Desarrollo

```
npm install        # sólo devDependencies (typescript)
npm test           # node --test
npm run typecheck  # tsc sobre JSDoc (// @ts-check)
```

Todo es JavaScript ESM con JSDoc; sin build. CI corre tests y typecheck en Ubuntu y Windows.

## Roadmap

- **Fase 2 — velocidad, no explorar a ciegas**: índice determinístico del repo (símbolos, endpoints, mapa fuente→test), scouts Haiku sólo para lo que el índice no responde, modelo y `effort` por fase.
- **Fase 3 — calidad, flujo de trabajo**: brief → research → plan → apply → review con archivos en disco y subagente por fase; review escalado por riesgo determinístico; memoria de decisiones y convenciones.

La medición de fase 1 es lo que permite saber si las fases siguientes aportan o no.

## Créditos

`scripts/lib/transcripts.mjs` deriva del plugin `session-report` de Anthropic (Apache-2.0). El filtrado lo hace [RTK](https://github.com/rtk-ai/rtk) (Apache-2.0), binario externo. Ver `NOTICE`.
