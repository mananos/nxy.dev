# Cómo funciona nxy por dentro

El [README](../README.md) cuenta qué hace nxy y cómo usarlo. Esta página explica cómo decide cada pieza, para cuando querés saber por qué pasó algo.

- [La idea de fondo](#la-idea-de-fondo)
- [El filtro de comandos](#el-filtro-de-comandos)
- [Las métricas](#las-métricas)
- [El scout y el índice](#el-scout-y-el-índice)
- [El freno de escritura](#el-freno-de-escritura)
- [El handoff](#el-handoff)
- [La memoria y los punteros](#la-memoria-y-los-punteros)
- [El plan](#el-plan)
- [La verificación de cada lote](#la-verificación-de-cada-lote)
- [La review](#la-review)
- [Escapes, convenciones y docs](#escapes-convenciones-y-docs)
- [Los hooks](#los-hooks)
- [Estructura del código](#estructura-del-código)

## La idea de fondo

El costo de una sesión es **contexto por llamada × llamadas por turno**. Cada llamada a la API reenvía todo el contexto acumulado; casi todo se lee de la cache (a un décimo del precio), pero con 150k encima cada paso sigue costando mucho más que con 20k. En sesiones reales, las lecturas de cache son el 92–97 % de los tokens.

De ahí salen las piezas de nxy:

- lo que entra al contexto principal tiene que ser poco (filtro, scout, punteros de una línea);
- el trabajo que lee mucho (buscar, editar, correr tests, revisar) lo hace un subagente que arranca limpio, paga lo que lee una vez y se descarta;
- cortar la sesión tiene que ser barato (handoff), porque es lo único que baja el contexto por llamada.

Todo lo que nxy decide lo decide código (reglas sobre números, SQLite, el transcript), no un modelo. Los únicos lugares donde un modelo decide algo son los subagentes, y cada uno devuelve un resumen corto.

## El filtro de comandos

1. **Al abrir la sesión** (`SessionStart`), nxy resuelve el motor una vez: si está `rtk`, qué versión, y si rtk tiene su propio hook instalado. Lo guarda en `~/.nxy/cache/engine.json` para que el hook de cada comando no tenga que volver a preguntar.
2. **Antes de cada comando** (`PreToolUse` de Bash y PowerShell) decide si lo reescribe. **Nunca** toca:
   - comandos con `NXY_RAW=1` (al inicio o en cualquier segmento: `cd api && NXY_RAW=1 grep …`) o con `# raw`;
   - lo que ya empieza con `rtk`, heredocs, `$(...)`, redirecciones a archivo (`> out.log`; `2>&1` y `>/dev/null` sí se permiten: descartan, no escriben), `&` al final, `cd`/`export` solos;
   - interactivos (`vim`, `ssh`, `docker exec -it`, `node`/`python` sin argumentos…) y `sudo`;
   - `git commit|push|rebase|merge|checkout|stash|reset|tag`: nxy no ejecuta nada por su cuenta, sólo deja pasar.

   Lo demás va a `rtk rewrite "<cmd>"` y, si rtk tiene filtro para ese comando, se reemplaza por `rtk <cmd>`:
   - Cadenas: `cd api && export JAVA_HOME=… && ./mvnw test` → `cd api && export JAVA_HOME=… && rtk mvn test`. Un segmento que rtk no conoce queda intacto, comillas incluidas. Una cadena que es sólo `cd` + scripts (`cd x && node -e "…"`) ni se le pregunta a rtk (`opaque` en las métricas).
   - Ortografías que rtk no reconoce se normalizan sólo para preguntarle: `./mvnw.cmd` → `mvnw`, `npm test` → `npm run test`, `pnpm build` → `pnpm run build`.
   - Un `| head -N` / `| tail -n N` final con N ≤ 20 deja el comando sin filtrar (`capped`): la salida ya está acotada. Con N mayor (`mvn test 2>&1 | tail -n 150`) el límite se saca para preguntarle a rtk y se vuelve a poner.
   - PowerShell: `Set-Location …; $env:JAVA_HOME = …; .\mvnw.cmd test` → mismos prefijos + `rtk mvn test`. Scripts reales (`foreach`, pipes, variables) no se tocan.
   - Lo que rtk cubre hoy (v0.48): git, gh, ls/cat/grep/find, mvn/mvnw, gradle/gradlew, npm run/pnpm run/bun, jest/vitest/playwright/pytest/cargo/go test, eslint/tsc/prettier/biome, next/vite, docker, kubectl. No: `ng` global, `yarn`, `npm install`.
   - Medido en un proyecto Spring Boot: `./mvnw test` pasó de 84 líneas / 9.197 caracteres a 8 líneas / 319 (−96 %).
3. **Permisos.** Si el comando original estaba permitido por tus reglas `permissions.allow` (`Bash(git status:*)`), el reescrito hereda el permiso y no aparece un prompt nuevo. Si no había regla, Claude Code te pregunta por `rtk git status` como te habría preguntado por `git status`. Si el original está en `permissions.deny`, no se reescribe.
   - Una cadena hereda el permiso sólo si **cada parte** está permitida, igual que en Claude Code: con `Bash(git status:*)` permitido, `git status && rm -rf build` te sigue preguntando. Si alguna parte está denegada, la cadena no se reescribe. Un comando con `$(…)` o backticks nunca hereda el permiso.
4. **Después de cada comando** (`PostToolUse`) nxy registra una fila en `<repo>/.nxy/local/metrics/filter.jsonl`: motor, tipo de comando, líneas y caracteres devueltos, hash de `rtk recall`. Los secretos obvios se enmascaran antes de escribir.
5. **El aviso de rtk.** `[rtk] /!\ No hook installed — run rtk init -g` no aplica a nxy, y en Windows aparecía en cada comando: rtk lo limita a uno por día con la fecha de un archivo vacío, y NTFS no actualiza esa fecha al reescribir 0 bytes. nxy toca ese archivo cuando tiene más de 23 h (un `stat` por comando, una escritura por día) y el aviso deja de ocupar contexto.
6. **La salida completa.** La skill `nxy-filter` le indica a Claude `rtk recall <hash>` o, como último recurso, `NXY_RAW=1 <cmd>`.

**Qué no se puede filtrar.** `Read`, `Grep` y `Glob` son herramientas internas de Claude Code: no pasan por Bash y ningún hook puede cambiar lo que devuelven. Para eso están el scout (que lee afuera del contexto principal) y el freno de escritura.

## Las métricas

Cada respuesta de la API deja en el transcript (`~/.claude/projects/**/*.jsonl`) el `usage` que Anthropic factura: input, cache write (5 min y 1 h), cache read, output. nxy sólo lo lee. El único cálculo es el precio en USD, con la [tabla oficial](https://platform.claude.com/docs/en/about-claude/pricing) guardada en `core/pricing.json`.

- **La statusline** es incremental: guarda por sesión hasta dónde leyó cada transcript y en cada refresco lee sólo lo nuevo.
- **`stats` y `trend`** leen los transcripts enteros del período. Una línea `<synthetic>` (mensaje interno de Claude Code, usage en cero) no cuenta como llamada. Un modelo que no está en la tabla de precios se marca como parcial en vez de inventar un precio.
- **`rtk saved`** sale del registro propio de rtk (`history.db`), porque nxy sólo ve la salida ya filtrada. Necesita Node ≥ 22.13 (`node:sqlite`); si no, `trend --by day` usa los totales diarios de `rtk gain`.

**La forma de una sesión** (`shape` en `stats`, columnas en `trend`):

| Dato | Qué es |
| --- | --- |
| `calls/turn` | Llamadas a la API por mensaje tuyo. Cada herramienta que Claude usa es una llamada más |
| `main context avg` / `peak` | Contexto medio y máximo de cada llamada del agente principal |
| `subagents %` | Qué parte de los tokens gastaron subagentes |
| `files edited (by main)` | Archivos distintos escritos con `Edit`/`Write`; `by main`, los que editó el principal. Las ediciones vía Bash no se ven |

**Reconstrucciones de cache.** Una llamada que escribe en cache más de `cacheBreakThreshold` tokens (100k por default) es una reconstrucción, y nxy le asigna una causa mirando lo que pasó antes:

| Causa | Qué pasó | Qué la evita |
| --- | --- | --- |
| `idle` | Pasó más tiempo que la vida de la cache sin llamadas | Volver con handoff + `/clear` en vez de retomar una sesión grande; o TTL de 1 h |
| `subagent` | El principal esperó a un subagente más de lo que dura su cache | Lotes más cortos, o TTL de 1 h |
| `compact` | Una compactación reescribió el historial | Nada: es un prefijo nuevo |
| `start` | Primera llamada de la sesión | Nada |
| `other` | El prefijo cambió con la cache viva (cambio de modelo, instrucciones editadas) | Revisar qué cambió a mitad de sesión |

`avoidable by warming` suma `idle` + `subagent`: es el techo de lo que ahorraría una cache que no vence.

**¿Conviene la cache de 1 hora?** (`core/metrics/cache-ttl.mjs`). Claude Code elige el TTL por separado para la conversación principal (`promptCacheTtl`) y para los subagentes (`subagentPromptCacheTtl`). Para la principal, nxy recalcula cada llamada que escribió con 5 minutos: esa escritura se paga a la tarifa de 1 hora (1,6× la de 5 minutos) y, si la llamada vino después de una pausa de 5 a 60 minutos, el prefijo que había dejado la llamada anterior pasa a ser lectura (un décimo del input) y sólo se escribe lo nuevo. Una pausa de más de una hora o una compactación reescriben igual con cualquier TTL. Con eso compara los dos totales: si la hora ahorra 1 % o más dice **worth it** y la línea exacta para `~/.claude/settings.json`; si cuesta más, **not worth it here**; en el medio, **about the same**. El umbral del 1 % es fijo: no hay nada que calibrar.

## El scout y el índice

`/nxy:locate` despacha el agente `scout` (Haiku), que antes de leer nada corre `locate.mjs`, tres buscadores que no gastan tokens:

| Capa | Qué resuelve |
| --- | --- |
| Índice de nxy | Dónde está definido un símbolo |
| `rg` | Texto exacto: literales, rutas como string, nombres de bean |
| codegraph (opcional) | Relaciones: quién llama a qué, qué se rompe si toco esto |

Con esos candidatos lee 2 o 3 archivos con `offset`/`limit` y devuelve hasta 5 `path:line` en ~500 tokens.

**El índice** vive en `.nxy/local/index/`, respeta `.gitignore`, se arma solo la primera vez y después relee sólo los archivos que cambiaron (por fecha y tamaño). Extrae símbolos con expresiones regulares por lenguaje: JS/TS/JSX/TSX, Java, Scala, Python, Go y C#. Un lenguaje que no conoce no rompe nada: `rg` sigue funcionando.

**codegraph** se usa sólo por CLI y sólo desde adentro del scout, donde su salida muere con el subagente. Conectado como servidor MCP (`codegraph install`) sumaría la descripción de su herramienta a cada turno del principal.

## El freno de escritura

Antes de cada `Edit`/`Write` del principal, el hook lee el final del transcript (los últimos 256 KB) y toma el contexto de la última llamada. Por debajo de `gate.contextTokens` no escribe nada: cero tokens. Por encima, rechaza la edición y le dice a Claude que delegue en el implementer con el `path:line`, el cambio y un criterio de aceptación.

Nunca frena a un subagente (el implementer tiene que poder escribir), nunca frena si no hay implementer configurado, y ante cualquier dato que no puede leer deja pasar: un gate que falla cerrado te rompe el editor.

`/nxy:gate once` deja un permiso de una sola edición que vence a los `escapeMinutes`. `/nxy:gate status` muestra cuántas ediciones frenó y cómo se distribuye el contexto al editar (p10/p50/p90).

## El handoff

Uno por rama, en la memoria local, tipo `handoff`. Guardarlo de nuevo lo reemplaza. Tiene forma fija (~20 líneas):

```
route: implementer        # inline | implementer — quién escribe el código
## Done
## Next
## Files
## Decisions
```

Si hay un plan, va adentro del handoff (`## Plan`), y un guardado sin plan conserva el que había.

- **Al abrir sesión** (también después de `/clear` o una compactación) y en modo `assisted`, una línea de ~40 tokens si la rama tiene handoff. En `proactive`, el handoff entero; en `manual`, nada.
- **Obligatorio pasado el umbral.** Si la sesión pasó `gate.contextTokens` y la rama no tiene handoff, nxy frena la primera edición del principal o el primer despacho del implementer hasta que Claude lo guarde. El mensaje trae el comando y la plantilla. Se apaga con `memory.handoff.required: false`, o junto con el freno (`/nxy:gate off`).
- **Viaja al implementer.** El hook de `Agent` antepone al prompt del implementer un bloque `<nxy-context>` con el handoff, el avance del plan y las convenciones del repo (las del área de sus archivos primero, hasta ~1.600 caracteres). Al scout no se lo pasa: un buscador tiene que buscar sin que le digan dónde.
- **No queda viejo.** El hook `Stop`: si la rama tiene handoff y en esta sesión se editó algo después del último guardado, Claude no termina el turno sin actualizarlo. Una vez por estado, nunca dos seguidas, nunca en una tarea sin handoff.
- **`Progress:`** (*batch 1 ✔, 2 ✘ (Exit code 1), 3 pending · review 3fa9c1: 2 from the change, 1 chosen to fix*) la arma nxy con lo que registró; el modelo no la escribe.

## La memoria y los punteros

**Storage.** SQLite (`node:sqlite`, sin dependencias) en `~/.nxy/memory/memory.db`, con FTS5 y BM25 sobre título, cuerpo y keywords. La búsqueda ignora las palabras vacías en español e inglés.

**Proyecto.** Se identifica por el remote de git normalizado (leído de `.git/config`, sin ejecutar git). Sin remote, por la ruta, y te avisa que esas memorias no te van a seguir a otra PC.

**Intercambio.** `export` escribe un Markdown con frontmatter por memoria en `<repo>/.nxy/memory/`. `import` es idempotente, gana el `updated` más nuevo y nunca borra lo local. Lo `global` y lo `--private` no salen nunca.

**Punteros por mensaje** (`UserPromptSubmit`). Una memoria recibe un puntero cuando:

- está atada a un archivo que nombraste o que la sesión viene editando (nxy la ata sola cuando su cuerpo menciona una ruta que existe en el repo);
- tu mensaje contiene uno de sus `--keywords`;
- comparte dos palabras con tu mensaje y al menos una es de su título o keywords (si pegaste un log largo, las dos tienen que serlo).

Hasta 3 por mensaje, cada memoria una vez por sesión, las reemplazadas nunca. Un mensaje que empieza con `/` no dispara punteros.

**Relaciones.** Salen de cuatro lados: archivos compartidos, links `[[id]]` en el cuerpo, memorias que se cargan juntas, y el **librarian** (Haiku), que al guardar decide si la nueva *reemplaza*, *contradice* o *se relaciona* con las parecidas. Esa decisión se toma una vez y viaja con el export; las de archivos y links se recalculan al importar; las de "se cargan juntas" nunca salen de tu máquina. `CONFLICTS with` quiere decir que dos memorias se contradicen y alguien tiene que resolverlo.

`roles.librarian.provider` está pensado para un modelo externo (API compatible con OpenAI o local) que no gaste tokens de tu sesión; todavía no está implementado.

## El plan

El **planner** (Sonnet, effort high) lee las convenciones del repo, busca con `locate.mjs`, se fija qué existe para reutilizar y guarda el plan con `mem handoff plan`, que valida la forma y rechaza prosa:

```
## Plan
Goal: se pueden dar de alta clientes corporativos
### Batch 1 — api: validación
- `src/main/java/app/ClienteService.java:88` — llamar a CuitValidator antes de persistir
Accept: `./mvnw -q test -Dtest=ClienteServiceTest`
### Batch 2 — api: endpoint
- `src/main/java/app/ClienteController.java` — POST /clientes/corporativos
Accept: `./mvnw -q test -Dtest=ClienteControllerTest`
```

- **Hash.** Cada plan tiene un hash; si el plan cambia, cambia el hash y hay que aprobarlo de nuevo.
- **Aprobación.** La verifica nxy en el transcript: busca tu respuesta a la pregunta `Approve nxy plan <hash>?` sobre ese hash. Claude puede hacerte la pregunta, pero no contestarla por vos. Hasta entonces se deniega toda edición del principal y todo despacho del implementer.
- **Preguntas.** Si el borrador tiene `### Questions`, no se puede aprobar ni ejecutar. `mem handoff next` las imprime listas para preguntarte; con las respuestas el planner termina el plan y agrega `Decisions:`. En la misma pregunta del Approve se ofrece guardar esas decisiones como convenciones del repo.
- **Plan activo.** Con el plan aprobado el principal no edita: cada lote va a un implementer, que es lo que se verifica. `/nxy:gate once` es la salida para algo fuera del plan.
- **Dependencias.** `Depends: 1` (o `none`) permite lotes en paralelo; un lote en rojo frena sólo a los que dependen de él.

## La verificación de cada lote

- Cada `Accept:` es un comando en backticks: el test más chico que prueba el lote (una clase o un archivo, no la suite), o `manual — <qué mirar>`.
- El implementer lo corre después de su último cambio. Al terminar, el hook `SubagentStop` lee **el transcript del implementer** (no su resumen) y verifica que el comando haya corrido después del último `Edit`, tal como está escrito (sin pipe) y con salida 0.
- Si no lo corrió o falló, lo manda de vuelta **una vez** (exit 2) con el comando exacto. La segunda vez lo deja terminar y el lote queda en rojo.
- Los veredictos quedan en `.nxy/local/verify/<plan>/`, un archivo por lote reemplazado de forma atómica, para que dos lotes en paralelo que terminan juntos no se pisen. Una carpeta sin veredictos nuevos en 30 días se borra sola.
- Mientras un lote esté en rojo no se despachan los que dependen de él, salvo que elijas **Continue anyway**.
- Con todos en verde, nxy indica despachar el **tester** (Haiku) una vez: corre la suite completa de cada repo tocado (o la línea `Suite:` del plan) y devuelve sólo lo que falló. Es informativo: no frena nada.

## La review

- **Qué se revisa.** Sólo lo que cambió el plan. La primera vez que un lote toca un archivo, nxy guarda una copia (`.nxy/local/baseline/<rama>/`); el diff es esa copia contra el archivo actual. No usa git, así que cambios tuyos sin commitear en esos archivos no aparecen como del plan. Las copias se borran con `handoff done`.
- **Cuándo.** Una vez, al final, después del tester. Si sólo cambiaron docs o tests, no hay review.
- **Lentes**, elegidas por qué archivos cambiaron, nunca por cuántas líneas:

| Si cambió… | Lente |
| --- | --- |
| cualquier código | **base**: alcance vs. el plan, que el `Accept:` pruebe lo que dice, reutilización, convenciones, tests, restos de debug |
| entidades, repositorios, `@Entity`, `@Transactional` | **persistence**: N+1, lazy fuera de transacción, transacciones, consultas |
| controllers, rutas, DTOs | **api**: validación, contratos, errores, autorización |
| componentes Angular / React / Solid | **frontend**: reutilización, estado, suscripciones, render |
| SQL, migraciones, evolutions | **database**: índices, locks, orden y reversibilidad |
| rutas con auth, security, payment, webhook, token… | **security**: autorización, secretos, inyección, datos personales |

- **Del cambio o preexistente.** Un hallazgo es del cambio si apunta a una línea que el plan modificó; si no, es preexistente y se muestra como información. Lo decide nxy por código, no el reviewer.
- **Checkpoint 2.** Una pregunta de selección múltiple con los hallazgos del cambio. Cada uno elegido lo arregla un implementer y su `Accept:` se vuelve a verificar. Hay **una sola ronda**.

## Escapes, convenciones y docs

- **Convenciones al implementer.** Cada implementer recibe los títulos de las convenciones del repo en su `<nxy-context>` (primero las del área de sus archivos). Si hay más de las que entran, recibe el comando para verlas.
- **Escapes.** `/nxy:review escape "<regla>" --as convention|lens` registra algo que la review no vio y lo guarda como convención (la siguen planner e implementers) o como lente del repo en `.nxy/lenses/repo.md` (la mira toda review futura). En una rama ya revisada, Claude recibe una línea por sesión para ofrecértelo.
- **Docs.** Si algún `.md` del repo nombra un archivo o una clase que el plan cambió (lo busca nxy con `rg`, sin tokens), el checkpoint 2 suma **Update docs / Leave docs**. El **documenter** (Sonnet, effort low) edita sólo esos `.md` en la misma rama. Si tu CI publica una carpeta a una wiki (Wiki.js, Azure DevOps), nxy la detecta.
- `/nxy:review status` muestra por lente, en 30 días: hallazgos del cambio, elegidos para arreglar, preexistentes y escapes.

## Los hooks

| Evento | Qué hace | Tokens |
| --- | --- | --- |
| `SessionStart` | Resuelve el motor del filtro; una línea si la rama tiene handoff | ~40 si hay handoff |
| `UserPromptSubmit` | Punteros a memorias; recordatorio de escape en rama revisada | ~30 por puntero |
| `PreToolUse` Bash/PowerShell | Reescribe el comando por rtk | 0 |
| `PreToolUse` Edit/Write | Freno de escritura, handoff obligatorio, checkpoint del plan, copia para la review | 0 salvo que frene |
| `PreToolUse` Agent | Modelo por rol; handoff + avance + convenciones al implementer; orden de los lotes | El bloque del implementer (~20–60 líneas) |
| `PostToolUse` Bash/PowerShell | Una fila de métricas | 0 |
| `PostToolUse` Agent | Veredicto del lote, siguiente paso (tester, reviewer), sugerencia de corte | Una o dos líneas |
| `SubagentStop` | Verifica el `Accept:` del lote en el transcript del implementer | 0 salvo que lo mande de vuelta |
| `Stop` | Handoff viejo al terminar el turno | 0 salvo que frene |

Todos fallan abiertos: ante cualquier error dejan pasar lo que Claude iba a hacer. `NXY_DEBUG=1` muestra la causa en stderr.

## Estructura del código

```
core/                    # nada acá sabe qué es Claude Code
  config, paths, format, jsonl, shell, text, rg, pricing
  filter/                # decide qué comando pasa por rtk, y cómo; el ledger
  metrics/aggregate.mjs  # suma llamadas, las tarifa, sigue el pico de contexto (tipo Call)
  index/ scout/          # índice de símbolos, búsqueda sin modelo, codegraph
  memory/                # store SQLite, ámbitos, recall, grafo, handoff, intercambio, convenciones
  gate, plan, verify, review, diff, docs
hosts/claude-code/       # todo lo específico de Claude Code
  transcripts.mjs        # ~/.claude/projects/*.jsonl -> Call[]
  hooks/                 # un archivo por evento
  entries/               # los scripts detrás de cada /nxy:<comando>
  statusline, permissions, settings, roles, *-state (estado en .nxy/local/)
agents/ commands/ skills/ hooks/ lenses/ .claude-plugin/   # en la raíz porque Claude Code los busca ahí
```

El contrato entre los dos lados es el tipo `Call` (`core/metrics/aggregate.mjs`): un host adapta lo que su herramienta escribe en disco a `Call[]` y todo lo demás (sumar, tarifar, formatear, filtrar) ya funciona. **`core/` nunca importa de `hosts/`.**
