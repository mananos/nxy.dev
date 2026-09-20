<p align="center">
  <img src="resources/nxydev.png" alt="nxy.dev" width="320">
</p>

<p align="center">Plugin de Claude Code: calidad de código, velocidad, consumo de tokens bajo control y visibilidad de lo que se gasta.</p>

Sirve para cualquier lenguaje o framework, y tanto para un repo existente como para uno que arranca de cero. Funciona en Windows y Linux (macOS también), con Node 22+ y sin dependencias de runtime.

Se construye por fases, midiendo cada una antes de sumar la siguiente:

| Fase | Pilar principal | Estado |
| ---- | --------------- | ------ |
| 1 | **Visibilidad** del consumo real + **menos tokens** en salidas de comandos | actual · **v0.1.3** |
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

Statusline (opcional, recomendado): `/nxy:statusline --apply` la instala (hace backup de `~/.claude/settings.json`; Claude Code sólo la lee de ahí, por eso es un paso aparte). Reiniciá Claude Code y listo: se actualiza sola con cada versión nueva del plugin. Qué muestra y para qué sirve: [La statusline](#la-statusline).

## Comandos

| Comando           | Qué muestra                                                                                                           |
| ----------------- | --------------------------------------------------------------------------------------------------------------------- |
| `/nxy:stats`      | La sesión actual (`last` para la anterior, o un id): tokens (uncached / cache write / cache read / output / thinking), cache hit, pico de contexto, USD; la [forma de la sesión](#la-forma-de-una-sesión) (llamadas por turno, contexto medio del principal, % en subagentes); por modelo, por tipo de subagente, agente por agente, por skill; herramientas usadas, archivos leídos y editados (y cuántos editó el principal), líneas devueltas por Bash; filas del filtro y el ahorro real de RTK; cache breaks. `--session <id>`, `--all`, `--json`. |
| `/nxy:trend`      | Evolución por día / semana / sesión, **de todos tus proyectos**: tokens, uncached, cache-r, output, hit %, % en subagentes, tokens por turno, llamadas por turno, contexto por llamada, archivos editados por el principal, líneas de Bash, comandos filtrados y tokens que ahorró RTK, USD, y si la sesión corrió con nxy. `--since 30d`, `--by week`, `--by model` (una fila por modelo: cuánto va a Opus/Sonnet/Haiku), `--here` (sólo este proyecto). |
| `/nxy:filter`     | `status` (motor, versión de rtk, rg, conflicto de hook, comandos de instalación), `on`, `off` (por proyecto).          |
| `/nxy:statusline` | Instala la [statusline](#la-statusline) (`--apply`) o imprime el snippet para pegarlo a mano. |

Salida exacta, sin que el modelo la retipee y sin gastar tokens: dentro de Claude Code escribí `! node "<ruta-al-plugin>/scripts/metrics/stats.mjs"` (el prefijo `!` ejecuta el comando y muestra su salida tal cual). Los scripts también corren fuera de Claude: `node scripts/metrics/stats.mjs --all`, `node scripts/metrics/trend.mjs --by week --since 90d --json`.

## La statusline

Una línea, siempre visible, que responde tres preguntas: **¿cuánto me está costando lo que Claude hace ahora?**, **¿cuándo conviene cortar y empezar de nuevo?** y **¿qué pasa si dejo la sesión un rato?**

```
◆ nxy ⟡ my.app main ⟡ Opus 5 · high ⟡ ctx ▰▰▰▱▱▱▱▱ 41% · 82k ⟡ turno $0.42~ ⟡ sesión $34.74~ · 44M→241k · +4 agents ⟡ cache 98% · 52m ⟡ 5h 62% · 7d 31%
```

| Segmento | Qué es | Qué hacer con eso |
| --- | --- | --- |
| `my.app main` | Carpeta y branch en la que Claude está trabajando. | Confirmar que estás donde creés, sobre todo con varias sesiones abiertas. |
| `Opus 5 · high` | Modelo y nivel de esfuerzo de la sesión. | Si estás en una tarea chica con Opus/high, cambiarlo antes de seguir. |
| `ctx ▰▰▰▱▱▱▱▱ 41% · 82k` | Cuánto contexto lleva la conversación: barra, porcentaje y **tokens reales**. Cada llamada a la API reenvía todo eso, así que a más tokens, más caro cada paso. Se pone **naranja a partir de 100k** y **rojo a partir de 200k** (ahí los modelos de 1M duplican el precio). | En naranja: terminar lo que estás haciendo y no arrancar nada grande. En rojo: guardar un resumen y `/clear`. El porcentaje engaña (41 % de 200k es poco; 41 % de 1M son 410k): mirá los tokens. |
| `turno $0.42~` | Lo que va gastando **el turno actual**: desde tu último mensaje, incluyendo los subagentes que Claude lanzó. Naranja desde $1, rojo desde $3. | Es la señal más importante. Un turno de $3 no es "Claude trabajó mucho": es que cada llamada arrastra demasiado contexto. Cortar y seguir en una sesión nueva sale más barato que insistir. |
| `sesión $34.74~ · 44M→241k · +4 agents` | El odómetro: costo total de la sesión, tokens de entrada → salida, y cuántos subagentes corrieron. | Comparar con lo que hiciste. Sin umbral: una sesión larga y sana puede valer $30. El detalle está en `/nxy:stats`. |
| `cache 98% · 52m` | Qué tan bien está funcionando la prompt cache (98 % de lo que se envía ya estaba cacheado, y se paga a un décimo) y **cuánto le queda de vida** si no hacés nada. | Si el porcentaje baja mucho de golpe, algo cambió en el contexto (un archivo enorme, un `/compact`). Los minutos te dicen cuánto podés ausentarte sin costo. |
| `❄386k $3.86~` | La cache **se enfrió**: dejaste pasar el tiempo de vida (5 min o 1 h según tu cuenta). El próximo mensaje vuelve a cachear 386k tokens y eso cuesta $3.86 antes de que Claude haga nada. | Si volvés después de una pausa larga y ves esto, es el momento de preguntarte si seguís en esta sesión o arrancás una nueva con un resumen: cuesta lo mismo y arranca liviana. |
| `5h 62% · 7d 31%` | Cuánto llevás consumido de las ventanas de tu suscripción. | Planificar: si vas al 90 % de la ventana de 5 h, lo pesado puede esperar. |

**Cortar y seguir (handoff).** Cuando `turno` o `ctx` están en rojo, la sesión ya no se arregla: cada mensaje reenvía todo el contexto acumulado y el precio por paso sólo sube. Lo barato es cortar bien: (1) pedile a Claude un resumen de ~20 líneas — qué se hizo, qué falta, archivos clave, decisiones tomadas; (2) `/clear`; (3) pegá el resumen como primer mensaje. La sesión nueva arranca con unos miles de tokens en vez de cientos de miles y cada turno vuelve a costar centavos. Hoy es manual; nxy 0.3.x lo automatiza (memoria + handoff vivo). Turno = velocidad a la que gastás; sesión = odómetro. El que te dice "cortá" es el primero.

El `~` después de un monto significa *equivalente en USD*: pagás por suscripción, no por API, pero es lo que ese uso costaría a precio de API (y lo que efectivamente pagás si usás API key).

**Por qué los umbrales son en tokens y no en porcentaje.** Lo que encarece cada paso es la cantidad de tokens que se reenvían, no la fracción de la ventana. Con un modelo de 1M de contexto, 20 % son 200k tokens y ya estás pagando doble; con una ventana de 200k, 20 % son 40k y está todo bien. Por eso `ctx` muestra el número y colorea por número.

**Apariencia.** Tres presets (`vivid` por defecto: cada dato con su color; `classic`: sobrio, etiquetas apagadas; `powerline`: bloques con fondo) y dos layouts (`line` o `two-line`, con la barra de contexto al doble de ancho). Se cambian en la [configuración](#configuración) y se ven al instante, sin reiniciar. Cualquier color se puede ajustar por rol.

**Cómo se mantiene actualizada.** `--apply` no apunta a una versión del plugin sino a un pequeño lanzador en `~/.nxy/statusline.mjs` que en cada arranque usa la versión de nxy que Claude Code tiene instalada. Actualizás el plugin y la statusline se actualiza sola. Para desarrollo: `node scripts/metrics/statusline-setup.mjs --apply` desde un clon la apunta a ese clon; `NXY_STATUSLINE=<ruta>` fuerza un script.

## La forma de una sesión

El costo de una sesión se explica con dos números: **cuánto contexto lleva cada llamada** y **cuántas llamadas hace cada turno**. Todo lo demás (modelo, cache, subagentes) modula esos dos. `/nxy:stats` los muestra en la línea `shape`, y `/nxy:trend` como columnas para ver cómo evolucionan:

```
shape: 21 calls/turn (main 21) · main context avg 111k/call · peak 158k · subagents 0% of tokens
files read (distinct): 12 · files edited: 14 (by main 14) · Bash calls 52 → 3.7k lines / 177k chars back
```

| Dato | Qué es | Qué hacer si está alto |
| --- | --- | --- |
| `calls/turn` | Llamadas a la API por cada mensaje tuyo (cada herramienta que Claude usa es una llamada más). 2–5 es un turno de pregunta y respuesta; 20–30 es Claude explorando o editando solo. | No es malo en sí — es trabajo hecho — pero cada llamada reenvía todo el contexto: 30 llamadas × 130k de contexto son 4M de tokens en un turno. Si el turno sale caro, pedí tareas más acotadas o que use subagentes para explorar. |
| `main context avg` | Contexto medio que carga cada llamada del agente principal (lo que se reenvía en cada paso). `peak` es el máximo. | Por encima de ~100k cada paso cuesta el doble que al principio de la sesión: es la señal de [cortar y seguir](#la-statusline). |
| `subagents %` | Qué parte de los tokens gastaron subagentes. Un subagente arranca con contexto limpio y devuelve un resumen: explorar ahí es mucho más barato que en el principal. | Si es 0 % en sesiones grandes, el principal está haciendo todo el trabajo con el contexto a cuestas. Fase 2 apunta a subir este número con scouts baratos. |
| `files edited (by main)` | Archivos distintos que Claude escribió con `Edit`/`Write` (ediciones vía Bash no se ven). `by main` cuenta las que hizo el agente principal. | Es el número de partida para 0.2.x (implementer + freno de escritura: que el principal orqueste en vez de editar). Hoy sólo se mide. |
| `rtk saved` | Tokens que RTK recortó de las salidas de Bash: lo que el comando imprimió menos lo que Claude leyó. Sale del registro propio de RTK (`history.db`), porque nxy sólo ve la salida ya filtrada. | Si es bajo con muchos comandos filtrados, mirá `command kind`: `cat`/`read` de código no se recortan (RTK devuelve el archivo entero, por diseño); el ahorro grande está en tests, builds, `git`, `grep`, `ls`. Necesita Node ≥ 22.13 (`node:sqlite`); si no, `trend --by day` usa los totales diarios de `rtk gain`. |

## Cómo funciona el filtro

1. `SessionStart` resuelve el motor una vez por sesión (¿está `rtk`? ¿tiene su propio hook instalado?) y lo cachea en `~/.nxy/cache/engine.json`.
2. `PreToolUse(Bash|PowerShell)` decide si reescribe. **Nunca** toca: comandos con `NXY_RAW=1` (al inicio o en cualquier segmento: `cd api && NXY_RAW=1 grep …`) o `# raw`, ya envueltos en `rtk`, heredocs, `$(...)`, redirecciones a archivo (`> out.log`; `2>&1` y cualquier `>/dev/null` sí se permiten: descartan, no escriben), `&` al final, `cd`/`export` solos, interactivos (`vim`, `ssh`, `docker exec -it`, `node`/`python` sin argumentos…), `sudo`, ni `git commit|push|rebase|merge|checkout|stash|reset|tag` — el plugin no ejecuta nada por su cuenta, sólo deja pasar. Lo demás va a `rtk rewrite "<cmd>"` y, si RTK tiene filtro, se reemplaza por `rtk <cmd>`.
   - Cadenas: `cd api && export JAVA_HOME=… && ./mvnw test` → `cd api && export JAVA_HOME=… && rtk mvn test` (RTK maneja `&&`, `;` y prefijos `VAR=valor`). Un segmento que RTK no conoce queda intacto, comillas incluidas: `node -e "…" && grep -rn foo .` → `node -e "…" && rtk grep -rn foo .`. Si la cadena es sólo `cd` + scripts (`cd x && node -e "…"`) ni se le pregunta a RTK (`opaque` en las métricas).
   - Ortografías que RTK no reconoce se normalizan sólo para preguntarle: `./mvnw.cmd`, `.gradlew.bat` → sin `./`; `npm test` → `npm run test`; `pnpm build` → `pnpm run build`. Un `| head -N` / `| tail -n N` final con N ≤ 20 deja el comando sin filtrar (`capped`): la salida ya está acotada y RTK sólo le cambiaría la forma (su encabezado se comería parte de las N líneas). Con N mayor (`mvn test 2>&1 | tail -n 150`) el límite se quita para preguntar (RTK rechaza pipes) y se vuelve a poner: `rtk mvn test 2>&1 | tail -n 150`. Si RTK no reescribe, el comando original corre intacto.
   - PowerShell: `Set-Location …; $env:JAVA_HOME = …; .mvnw.cmd test` → mismos prefijos + `rtk mvn test`. Scripts reales (`foreach`, pipes, variables) no se tocan.
   - Lo que RTK cubre hoy (v0.48): git, gh, ls/cat/grep/find, mvn/mvnw, gradle/gradlew, npm run/pnpm run/bun, jest/vitest/playwright/pytest/cargo/go test, eslint/tsc/prettier/biome, next/vite, docker, kubectl. No: `ng` global, `yarn`, `npm install`.
   - Medido en un proyecto Spring Boot: `./mvnw test` pasó de 84 líneas / 9.197 chars a 8 líneas / 319 chars (−96 %).
3. Si el comando original ya estaba permitido por tus reglas `permissions.allow` (`Bash(git status:*)`), el reescrito hereda ese permiso: no aparecen prompts nuevos. Si no había regla, Claude Code te pregunta por `rtk git status` como te habría preguntado por `git status` (y podés dejarlo permitido). Si el original está en `permissions.deny`, no se reescribe.
4. `PostToolUse(Bash|PowerShell)` (síncrono, ~100 ms por comando) registra una fila en `<proyecto>/.nxy/metrics/filter.jsonl`: motor, tipo de comando, líneas/chars devueltos, hash de `rtk recall`. Los secretos obvios se enmascaran antes de escribir.
   - El aviso `[rtk] /!\ No hook installed — run rtk init -g` que RTK imprime en stderr no aplica a nxy (instala su propio hook) y en Windows aparecía en **cada** comando: RTK limita el aviso a uno por día con la fecha de un archivo vacío, y NTFS no actualiza la fecha al reescribir 0 bytes. nxy toca ese archivo (`.hook_warn_last`, en el directorio de datos de RTK) cuando tiene más de 23 h — un `stat` por comando, una escritura por día — y el aviso deja de ocupar contexto.
5. Cuando Claude necesita la salida completa, el skill `nxy-filter` le indica `rtk recall <hash>` o, como último recurso, `NXY_RAW=1 <cmd>`.

## Configuración

Defaults en `nxy.config.json`; overrides en `~/.nxy/config.json` y `<proyecto>/.nxy/config.json` (deep-merge). Env: `NXY_FILTER=0|1`, `NXY_ENGINE=rtk|off`, `NXY_RTK_PATH`, `NXY_DEBUG=1` (los hooks son fail-open: ante cualquier error dejan pasar el comando; con `NXY_DEBUG` imprimen la causa en stderr).

```json
{
  "modules": { "metrics": true, "filter": true },
  "filter": { "engine": "auto", "excludeCommands": [], "onlyCommands": [], "autoAllowWhenOriginalAllowed": true },
  "metrics": { "projectsDir": null, "subscription": true, "statusline": { "cacheTtlMs": 2000, "color": true, "preset": "vivid", "layout": "line", "brand": "◆ nxy", "ctxWarnTokens": 100000, "ctxCritTokens": 200000, "turnWarnUsd": 1, "turnCritUsd": 3, "promptCacheTtlMin": 5, "theme": {} }, "cacheBreakThreshold": 100000 }
}
```

`statusline`: `preset` (`classic` | `vivid` | `powerline`), `layout` (`line` | `two-line`), `separator` (reemplaza el `⟡`), `color: false` (sin ANSI), `brand` (texto inicial; `""` lo oculta), `ctxWarnTokens`/`ctxCritTokens` (naranja/rojo del contexto, en tokens), `turnWarnUsd`/`turnCritUsd` (naranja/rojo del turno, en USD), `promptCacheTtlMin` (sólo para versiones de Claude Code anteriores a 2.1.251, que no reportan el estado de la cache). `theme` sobreescribe cualquier rol del preset — `brand`, `separator`, `path`, `branch`, `model`, `effort`, `where`, `ctx`, `turn`, `session`, `cache`, `limits`, `label`, `value`, `gauge`, `gaugeEmpty`, `warn`, `crit` — con tokens separados por espacio: nombres (`red`, `cyan`, `muted`, `bold`, `dim`…), índice 0–255 de la paleta, `#rrggbb` o `bg:<color>` para el fondo. Ej.: `"theme": { "brand": "bold magenta", "gauge": "#ff79c6" }`. Los cambios se ven en el próximo refresco, sin reiniciar.

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

Una versión por paso: cada una se instala y se prueba en repos reales antes de la siguiente, y cada una deja algo usable.

| Versión | Qué vas a poder hacer |
| --- | --- |
| 0.1.2 | Statusline completa: dónde estás, cuánto cuesta el turno y la sesión, cuándo cortar, qué pasa si la cache se enfría. |
| **0.1.3** (actual) | Ver la [forma de la sesión](#la-forma-de-una-sesión): llamadas por turno, contexto por llamada, archivos que editó el principal, % en subagentes y el ahorro real de RTK. Más comandos filtrados: `2>/dev/null` y cadenas con `node -e`/`python` ya pasan por RTK; `NXY_RAW=1` escapa en cualquier segmento; los `\| head/tail -N` se respetan; el aviso de RTK ya no ocupa contexto. |
| 0.2.x | **Velocidad**: un scout barato localiza (`/nxy:locate`) para que el principal no lea 40 archivos; implementer + freno de escritura para que el principal orqueste en vez de editar. Config compartible por repo (`.nxy/`). |
| 0.3.x | **Memoria**: sobrevive al `/clear`, handoff vivo para retomar sin reconstruir el plan, contexto obligatorio para cada subagente. |
| 0.4.x | **Calidad**: planner con checkpoint, tester, reviewer con lentes, tiers por tamaño, protección contra cerrar con estado pendiente. |
| 0.5.0 | CLI `nxy` (`doctor`, `stats`, `trend`, `config`, `mem`) para operarlo sin abrir Claude Code. |

La medición de fase 1 es lo que permite saber si cada paso aporta o no.

## Créditos

`scripts/lib/transcripts.mjs` deriva del plugin `session-report` de Anthropic (Apache-2.0). El filtrado lo hace [RTK](https://github.com/rtk-ai/rtk) (Apache-2.0), binario externo. Ver `NOTICE`.
