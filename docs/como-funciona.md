# Cómo funciona nxy por dentro

El [README](../README.md) cuenta qué hace nxy y cómo usarlo, y [configuración](configuracion.md) lista cada clave de config, las variables de entorno y los comandos de instalación. Esta página explica cómo decide cada pieza, para cuando querés saber por qué pasó algo.

- [La idea de fondo](#la-idea-de-fondo)
- [El filtro de comandos](#el-filtro-de-comandos)
- [Las métricas](#las-métricas)
- [El scout y el índice](#el-scout-y-el-índice)
- [El freno de escritura y de lectura](#el-freno-de-escritura-y-de-lectura)
- [Features y notas](#features-y-notas)
- [La memoria](#la-memoria)
- [El plan](#el-plan)
- [Los lotes y su verificación](#los-lotes-y-su-verificación)
- [Los tests afectados](#los-tests-afectados)
- [La review](#la-review)
- [Docs y documenter](#docs-y-documenter)
- [El modo ingeniero](#el-modo-ingeniero)
- [Varios repos y worktrees](#varios-repos-y-worktrees)
- [La TUI](#la-tui)
- [Los hooks](#los-hooks)
- [Estructura del código](#estructura-del-código)
- [Datos de la 1.x](#datos-de-la-1x)

## La idea de fondo

El costo de una sesión es **contexto por llamada × llamadas por turno**. Cada llamada a la API reenvía todo el contexto acumulado; casi todo se lee de la cache (a un décimo del precio), pero con 150k encima cada paso sigue costando mucho más que con 20k. En sesiones reales, las lecturas de cache son el 92–97 % de los tokens.

De ahí salen las piezas de nxy:

- lo que entra al contexto principal tiene que ser poco (filtro, scout, reglas de una línea);
- el trabajo que lee mucho (buscar, editar, correr tests, revisar) lo hace un subagente que arranca limpio, paga lo que lee una vez y se descarta;
- cortar la sesión tiene que ser barato: el estado de la feature vive en la base de nxy, no en el chat, así que un `/clear` no pierde nada.

Todo lo que nxy decide lo decide código (reglas sobre números, SQLite, el transcript), no un modelo. Los únicos lugares donde un modelo decide algo son los subagentes, y cada uno devuelve un resumen corto.

## El filtro de comandos

1. **Al abrir la sesión** (`SessionStart`) y antes de cada comando, nxy resuelve el motor: si está `rtk`, qué versión y si rtk tiene su propio hook instalado. El resultado queda en `~/.nxy/cache/engine-v2.json` para que el hook de cada comando no tenga que volver a preguntar. Si cambiás `filter.engine` (o `NXY_ENGINE`) o `NXY_RTK_PATH`, el cambio rige en el comando siguiente.
2. **Antes de cada comando** (`PreToolUse` de Bash y PowerShell) decide si lo reescribe. **Nunca** toca:
   - comandos con `NXY_RAW=1` (al inicio o en cualquier segmento: `cd api && NXY_RAW=1 grep …`) o con `# raw`;
   - lo que ya empieza con `rtk`, heredocs, `$(...)`, redirecciones a archivo (`> out.log`; `2>&1` y `>/dev/null` sí se permiten: descartan, no escriben), `&` al final, `cd`/`export` solos;
   - interactivos (`vim`, `ssh`, `docker exec -it`, `node`/`python` sin argumentos…) y `sudo`;
   - `git commit|push|rebase|merge|checkout|stash|reset|tag`: nxy no ejecuta nada por su cuenta, sólo deja pasar.

   Lo demás va a `rtk rewrite "<cmd>"` y, si rtk tiene filtro para ese comando, se reemplaza por `rtk <cmd>`:
   - Cadenas: `cd api && export JAVA_HOME=… && ./mvnw test` → `cd api && export JAVA_HOME=… && rtk mvn test`. Un segmento que rtk no conoce queda intacto, comillas incluidas. Una cadena que es sólo `cd` + scripts (`cd x && node -e "…"`) ni se le pregunta a rtk (`opaque` en las métricas).
   - Ortografías que rtk no reconoce se normalizan sólo para preguntarle: `./mvnw.cmd` → `mvnw`, y los scripts de npm y pnpm (`test`, `build`) a su forma con `run`.
   - Un `| head -N` / `| tail -n N` final con N ≤ 20 deja el comando sin filtrar (`capped`): la salida ya está acotada. Con N mayor (`mvn test 2>&1 | tail -n 150`) el límite se saca para preguntarle a rtk y se vuelve a poner.
   - PowerShell: `Set-Location …; $env:JAVA_HOME = …; .\mvnw.cmd test` → mismos prefijos + `rtk mvn test`. Scripts reales (`foreach`, pipes, variables) no se tocan.
   - Lo que rtk cubre hoy (v0.48): git, gh, ls/cat/grep/find, mvn/mvnw, gradle/gradlew, los scripts de npm y pnpm, bun, jest/vitest/playwright/pytest/cargo/go test, eslint/tsc/prettier/biome, next/vite, docker, kubectl. No: `ng` global, `yarn` ni la instalación de paquetes de npm.
   - Medido en un proyecto Spring Boot: `./mvnw test` pasó de 84 líneas / 9.197 caracteres a 8 líneas / 319 (−96 %).
3. **La respuesta de rtk se recuerda por comando exacto.** Un comando que ya corriste (`git status`, `go test ./...`) se reescribe sin arrancar rtk: unos 55 ms menos por llamada a Bash en Windows. La cache es un solo archivo, `~/.nxy/cache/rewrite-v2.json`: hasta 500 comandos (unos 75 KB), cada uno 7 días, y se descartan primero los más viejos. Se tira entera si cambia la ruta o la versión de rtk, o si se crea, edita o borra la config propia de rtk (`rtk config` dice dónde está). Los errores y los timeouts nunca se guardan. `NXY_REWRITE_CACHE=0` la saltea.
4. **Permisos.** Si el comando original estaba permitido por tus reglas `permissions.allow` (`Bash(git status:*)`), el reescrito hereda el permiso y no aparece un prompt nuevo. Si no había regla, Claude Code te pregunta por `rtk git status` como te habría preguntado por `git status`. Si el original está en `permissions.deny`, no se reescribe.
   - **En auto mode.** Claude Code rechaza un comando cuyo input cambió sin una decisión de permiso, y nxy no aprueba nada por vos, así que no reescribe: `SessionStart` le pide a Claude que escriba él mismo `rtk git status` (o la ruta de rtk que fijaste). Si se olvida, ves como mucho un intento denegado que nombra el comando exacto, y Claude lo repite con `rtk`. Los comandos que ya matchean una regla `allow` se reescriben y permiten como siempre. No hay nada que hacer.
   - Una cadena hereda el permiso sólo si **cada parte** está permitida, igual que en Claude Code: con `Bash(git status:*)` permitido, `git status && rm -rf build` te sigue preguntando. Si alguna parte está denegada, la cadena no se reescribe. Un comando con `$(…)` o backticks nunca hereda el permiso.
5. **Después de cada comando** (`PostToolUse`) nxy suma una llamada al resumen de filtro de su sesión en `~/.nxy/nxy.db`: motor, tipo de comando, caracteres y tokens devueltos, y el motivo cuando no reescribió. No guarda el comando crudo. Si la base está ocupada, el hook descarta la fila después de esperar 200 ms como mucho. Las sesiones de más de 90 días se barren.
6. **El aviso de rtk.** `[rtk] /!\ No hook installed — run rtk init -g` no aplica a nxy, y en Windows aparecía en cada comando: rtk lo limita a uno por día con la fecha de un archivo vacío, y NTFS no actualiza esa fecha al reescribir 0 bytes. nxy toca ese archivo cuando tiene más de 23 h y el aviso deja de ocupar contexto.
7. **La salida completa.** La skill `nxy-filter` le indica a Claude `rtk recall <hash>` o, como último recurso, `NXY_RAW=1 <cmd>`.

`nxy bench hook [-n 20] [-command "git status"]` mide cuánto tarda el hook real (mínimo, mediana, p90 y máximo en ms, contra un umbral de 50 ms) con el filtro apagado, prendido sin cache y prendido con cache. Siempre sale con 0.

**Qué no se puede filtrar.** `Read`, `Grep` y `Glob` son herramientas internas de Claude Code: no pasan por Bash y ningún hook puede cambiar lo que devuelven. Para eso están el scout (que lee afuera del contexto principal) y los frenos.

## Las métricas

Cada respuesta de la API deja en el transcript (`~/.claude/projects/**/*.jsonl`) el `usage` que Anthropic factura: input, cache write (5 min y 1 h), cache read, output. nxy sólo lo lee: lo suma `internal/metrics` y lo imprimen `internal/report` y los comandos `nxy stats`, `nxy trend` y `nxy statusline` (`internal/app/stats.go`, `trend.go`, `statusline.go`).

El único cálculo es el precio en USD, con la [tabla oficial](https://platform.claude.com/docs/en/about-claude/pricing) embebida en el binario (`internal/pricing/pricing.json`); nxy nunca sale a internet para actualizarla. Los modelos que se cobran según el largo del prompt (hoy Haiku 5.5) traen en la tabla un tramo opcional `above` con su `threshold`; nxy elige el tramo por pedido (largo = entrada + escrituras + lecturas de cache de ese pedido, estrictamente mayor que el umbral). Un modelo que no está en la tabla se estima por familia y se marca con `*`, o se marca como parcial; nunca se inventa un precio.

- **La statusline** es incremental: guarda por sesión hasta dónde leyó cada transcript y en cada refresco lee sólo lo nuevo (`~/.nxy/cache/statusline-v2/<sesión>.json`, unos 7 KB por sesión; al crear el estado de una sesión nueva se borran los de más de 7 días). Si algo falla imprime `nxy ?`.
- **`stats` y `trend`** leen los transcripts enteros del período. Una línea `<synthetic>` (mensaje interno de Claude Code, usage en cero) no cuenta como llamada. Abren `nxy.db` sólo en lectura: nunca la crean.
- **`rtk saved`** sale del registro propio de rtk, porque nxy sólo ve la salida ya filtrada.

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
| `idle` | Pasó más tiempo que la vida de la cache sin llamadas | Volver con `/clear` en vez de retomar una sesión grande (la feature sigue en la base); o TTL de 1 h |
| `subagent` | El principal esperó a un subagente más de lo que dura su cache | Lotes más cortos, o TTL de 1 h |
| `compact` | Una compactación reescribió el historial | Nada: es un prefijo nuevo |
| `start` | Primera llamada de la sesión | Nada |
| `other` | El prefijo cambió con la cache viva (cambio de modelo, instrucciones editadas) | Revisar qué cambió a mitad de sesión |

`avoidable by warming` suma `idle` + `subagent`: es el techo de lo que ahorraría una cache que no vence.

**¿Conviene la cache de 1 hora?** Claude Code elige el TTL por separado para la conversación principal (`promptCacheTtl`) y para los subagentes (`subagentPromptCacheTtl`). Para la principal, nxy recalcula cada llamada que escribió con 5 minutos: esa escritura se paga a la tarifa de 1 hora (1,6× la de 5 minutos) y, si la llamada vino después de una pausa de 5 a 60 minutos, el prefijo que había dejado la llamada anterior pasa a ser lectura (un décimo del input) y sólo se escribe lo nuevo. Una pausa de más de una hora o una compactación reescriben igual con cualquier TTL. Con eso compara los dos totales: si la hora ahorra 1 % o más dice `conviene` y la línea exacta para `~/.claude/settings.json`; si cuesta más, `acá no conviene`; en el medio, `da casi igual`. El umbral del 1 % es fijo: no hay nada que calibrar. `nxy config cache-ttl 1h|off` escribe o quita esa clave ([configuración](configuracion.md)).

**Tiempo por subagente.** `nxy stats` imprime `SUBAGENT TIME` y `nxy trend --by agent` hace lo mismo sobre todo el período: para cada tipo de subagente (implementer, planner, scout...) dice cuántas corridas hubo, la mediana, el p90 y el máximo, y en qué se fue el tiempo. Sale de los transcripts de los subagentes, sin medir nada aparte:

| Columna | Qué es |
| --- | --- |
| `model %` | El modelo generando (de la última cosa que recibió hasta su último fragmento de respuesta) |
| `bash %` | Comandos de Bash/PowerShell, del pedido a su resultado |
| `tools %` | Las demás herramientas (Read, Grep, Edit...) |
| `waiting %` | Todo lo demás: tiempo sin nada en curso |
| `locate` | `X/N`: en cuántas de las N corridas se usó `nxy locate` |

Los cuatro porcentajes suman el 100 % del tiempo de pared. Si un comando corre mientras el modelo sigue escribiendo, se cuenta una sola vez, como comando. Debajo van los comandos más lentos. Cuando el servicio frena por límite de uso, esa pausa no es trabajo del agente: se excluye del tiempo y se cuenta aparte en `usage-limit stops excluded: N`, que sólo aparece si N > 0. Qué hacer: si domina `bash %`, mirá los comandos lentos; si domina `waiting %`, hay agentes esperando algo ajeno al modelo.

**Lo que se guarda por feature.** Por cada subagente nxy anota modelo, tokens, costo y tiempo (tabla `agent_runs`, una fila al terminar) y el inicio y el fin de cada lote. Es lo que muestra la TUI; vive en tu máquina, el export de memoria no lo lleva y se borra a los 90 días.

## El scout y el índice

`/nxy:locate` despacha el agente `scout`, que antes de leer nada corre `nxy locate`, tres buscadores que no gastan tokens (`cmd/nxy/locate.go`):

| Capa | Qué resuelve |
| --- | --- |
| Índice de nxy | Dónde está definido un símbolo |
| `rg` | Texto exacto: literales, rutas como string, nombres de bean |
| codegraph (opcional) | Relaciones: quién llama a qué, qué se rompe si toco esto |

Con esos candidatos lee 2 o 3 archivos con `offset`/`limit` y devuelve hasta 5 `path:line`. También podés correrlo vos: `nxy locate "<consulta>"`; `--callers|--callees|--impact <símbolo>` pide relaciones, `--status` muestra el estado, `--index` reconstruye el índice y `--json` da salida de máquina. Una consulta `/regex/i` se toma como regex.

**El índice** vive en `.nxy/local/index/` (`repomap.tsv` y `repomap.meta.json`), respeta `.gitignore`, se arma solo la primera vez y después relee sólo los archivos que cambiaron. Extrae símbolos con expresiones regulares por lenguaje: JS/TS/JSX/TSX, Java, Scala, Python, Go y C#. Un lenguaje que no conoce no rompe nada: `rg` sigue funcionando. Se borra junto con `.nxy/local`.

**codegraph** (`internal/tools/codegraph.go`) se usa sólo por CLI y sólo desde adentro del scout y de los tests afectados, donde su salida muere con el subagente. Conectado como servidor MCP sumaría la descripción de su herramienta a cada turno del principal. Con `scout.codegraph: false` el scout no lo usa ([configuración](configuracion.md)).

## El freno de escritura y de lectura

**Escritura.** Sin una feature activa, el hilo principal no edita archivos de código: ni con `Edit`/`Write` ni por shell (`echo > x.go`, `sed -i`, `tee`, `cp`, `mv`). Cuenta como código lo que el mapa del repo indexa más las extensiones de los tests de tus runners (por ejemplo `.kt`); los docs y el texto pasan, lo de afuera del repo pasa, y el implementer nunca se frena. El rechazo manda a `/nxy:feature` o al implementer. Con una feature activa el freno es total: el principal no escribe el repo, escribe el implementer.

**Lectura.** Con `gate.reads` (por defecto `true`) y el contexto del hilo principal en `gate.contextTokens` (100k) o más, una búsqueda amplia del principal se frena y te manda a un scout: `Grep`/`Glob` sin ruta de archivo, o `rg`, `grep -r`, `find`, `fd`, `gci -r`, `sls` sin ruta de archivo. Nunca frena `Read`, subagentes ni búsquedas sobre un archivo; ante un dato desconocido deja pasar. El rechazo empieza con `nxy read gate:`, y la primera vez de cada sesión agrega cómo apagarlo. La clasificación es sólo de strings: una búsqueda angosta sale sin leer config ni transcript. Límite conocido: un directorio con un punto en el nombre (`v1.2`) se toma por archivo y su búsqueda no se frena.

**Escapes.** `/nxy:gate once` deja pasar una sola edición de código y `/nxy:gate once search`, una sola búsqueda; cada permiso vence a los `gate.escapeMinutes` (5) y es independiente del otro. `/nxy:gate reads off|on` apaga o prende el freno de búsquedas: escribe `gate.reads` en tu config de usuario, o en la del repo con `--repo`. Para apagar los dos frenos, `nxy config set gate.enabled false` ([configuración](configuracion.md)).

Ante cualquier dato que no puede leer, el gate deja pasar: un gate que falla cerrado te rompe el editor.

**Qué registra.** Cada edición del principal dentro del repo y cada búsqueda frenada o permitida por un escape suma una fila de unos 150 bytes a `.nxy/local/metrics/gate.jsonl` (tope de 256 KB; al pasarlo se descartan las filas de más de 90 días). `/nxy:gate status` muestra:

- `write gate` y `read gate`: si están prendidos y desde qué umbral frenan;
- `one-shot edit` y `search escape`: si hay un `once` armado y cuántos minutos le quedan;
- `decisions` y `context at edit`: ediciones frenadas de cuántas pasaron, y el contexto (p10, p50, p90, máximo) en que editaste;
- `searches`: búsquedas frenadas y dejadas pasar con `once search`;
- `broad searches`: las búsquedas amplias de los últimos 30 días (`--since 7d`) hechas con un contexto sobre el umbral.

Las `broad searches` y la línea por sesión de `nxy stats` salen de los transcripts, no del ledger, así que cubren también sesiones pasadas y cuestan 0 mientras trabajás. Cuenta como amplia un `Grep` que no apunta a un solo archivo, cualquier `Glob`, un `Read` de archivo entero (sin `offset` ni `limit`) y sus equivalentes por Bash/PowerShell. Esa suma es una cota superior de lo que costó cada llamada, no los tokens que un scout habría ahorrado. Si es alta, delegá la búsqueda con `/nxy:locate`.

## Features y notas

La unidad de trabajo es la **feature**: nace con `/nxy:feature <pedido>` o `/nxy:engineer <pedido>` y vive en `~/.nxy/nxy.db`, no en el chat. Guarda el pedido, el plan, los lotes con su veredicto, la review y el paso en que está. Por eso un `/clear`, una compactación o abrir otra sesión no la pierden: al arrancar, Claude recibe el «para retomar» armado desde la base (el pedido, el paso, «lote N de M», lo último que falló, las decisiones y las notas de la feature). `nxy status` muestra lo mismo.

Los estados públicos de `nxy status`:

- **Trabajando**: nxy o un subagente está haciendo algo; no hace falta que hagas nada.
- **Verificando**: se está corriendo una prueba.
- **Necesita tu decisión**: te toca a vos (responder, aprobar, reintentar o parar).
- **Listo**: la feature terminó, o la paraste.

Hay **una sola feature sin terminar por checkout**. Si ya hay una, `/nxy:feature` te dice cuál es; `nxy feature stop` la libera (y borra su plan `.md` y su review `.md`). Si reabrís una sesión en una carpeta cuya feature no tiene otra sesión viva, `/nxy:feature` responde «ya hay una feature en curso» en vez de arrancar una segunda.

**Planner en segundo plano.** Si Claude lanza el planner sin esperarlo, nxy le dice «esperá» al hilo principal. El plan te llega al final de ese turno o en tu próximo mensaje, una sola vez.

**Stop hook error.** Cuando un paso pendiente (un checkpoint) llega justo al terminar el turno, Claude Code lo rotula «Stop hook error». El rótulo es de Claude Code, no es un error: nxy usa ese camino para que Claude siga. No hay que hacer nada.

**Notas.** Una nota (`N3`) es algo que dejaste a medias en una carpeta, para retomar en otra sesión sin releer el chat: `nxy note save "<título>"` (cuerpo por stdin, hasta 2 KB), `nxy note list [--all]` y `nxy note done <id>`.

- Sin feature activa y con notas en la carpeta, tu primer mensaje dispara la pregunta «¿Con cuál seguimos?» (hasta 3 notas, con «hace 2 d», y «Ninguna, es otra cosa»). La elegida llega al contexto con su texto; «Ninguna» no inyecta nada. Si hay más de 3, `nxy note list` las muestra.
- Con una feature activa no se pregunta nada: las notas de la feature viajan en el «para retomar».
- Vencen a los 30 días sin escribirlas; las de una feature se borran al terminarla o pararla; las de un worktree, al cerrarlo. `nxy note done <id>` borra una a mano.
- Al pausar un worktree, Claude te pregunta (opcional) «¿Algo para la próxima sesión?» y, si contestás, queda como nota de ese worktree.

## La memoria

Vive en la base global (`~/.nxy/nxy.db`) y se identifica por repo con el remote de git normalizado (sin remote, por la ruta). Tiene tres cosas distintas:

- **Reglas (`K3`)**: una decisión del repo que una tarea futura tiene que seguir. Siempre dice la pregunta que resuelve.
- **Reglas globales (`G2`)**: tuyas y de todos tus repos. Con `--roles` elegís qué roles las reciben (`planner`, `implementer`, `reviewer` y `main`, el hilo principal en modo ingeniero); sin `--roles` las reciben todos.
- **Aprendizajes (`A7`)**: algo que notaste y todavía no es regla. Repetirlo suma `seen`, no crea otro.

Comandos (`nxy mem …`, o `/nxy:mem`):

- `show`: las reglas con `delivered N` (cuántas veces se entregó a un agente) y `cited M` (cuántas veces un reviewer la citó al encontrar que el cambio la rompía); `to review` marca las que llevan 90 días sin citarse. Debajo, las globales y los aprendizajes con `seen N times`: los más vistos son candidatos a promover.
- `rule "<texto>" --question "<la pregunta que resuelve>" [--paths g1,g2] [--global [--roles planner,reviewer]]`. Sin `--question` falla; `--roles` sin `--global` también.
- `note "<texto>" --files a,b` guarda un aprendizaje; `promote <A7> --question "<q>"` lo convierte en regla.
- `edit <K3|G2|A7> [--text …] [--question …] [--paths …] [--roles …] [--files …]` cambia lo que pasás y deja todo lo demás (el número, las entregas, las citas). `--paths ""` o `--roles ""` los vacían. Sin ningún flag falla con «nothing to change».
- `rm <ref>` borra (desaparece de `show`, de la búsqueda y de lo que reciben los agentes) y `restore <ref>` lo trae de vuelta con el mismo número, que nunca se reutiliza. `keep <ref>` marca una propuesta como «dejarla»: nxy no la vuelve a proponer.
- `search <texto> [--all]` y `related <archivos…>` (features cerradas y aprendizajes que tocaron esos archivos).
- `export [--out archivo] [--notes]` y `import <archivo> [--apply]`: `import` sin `--apply` sólo muestra qué cambiaría; con `--apply` lo aplica en una sola transacción. Gana el más nuevo y los borrados viajan. Las notas temporales sólo viajan con `--notes`.

**Qué recibe cada agente.** El planner recibe todas las reglas; el implementer, las del área que toca el lote (las que tienen `--paths` que coinciden, más las que no tienen) y los aprendizajes de esos archivos; el reviewer, las reglas del diff; el hilo principal del modo ingeniero, las reglas al empezar el diseño. Siempre con el texto completo y con un tope de 3.000 caracteres (unos 750 tokens): lo que no entra queda en una línea «not shown (budget)» y lo ves con `nxy mem show`. Las `G` van después de las `K` y se cortan primero; si hay alguna, el agente lee que ante una contradicción gana la `K`. Nunca llegan al scout, al refuter ni al documenter. Un hallazgo que cita una regla sube a medium como mínimo. El planner y el modo ingeniero también corren `nxy mem related <archivos>`.

**Cómo se ofrecen.** Al terminar con éxito, si hay candidatas (las decisiones `[repo]` del plan o del diseño, lo que levantaste con `nxy feature rule` y los escapes de la review), te pregunta qué guardar como regla del repo, de a 4 opciones por pregunta. Si una se parece a una existente dice «reemplaza a K3». «Ninguna» o parar la feature no guarda nada. Si con una feature activa escribís «siempre», «nunca» o «de ahora en adelante» (o always / never / from now on), nxy le pide a Claude que te pregunte si es una regla del repo; nunca la guarda sin que lo confirmes. En un repo sin reglas, la primera feature pregunta «Este repo no tiene reglas guardadas. ¿Las propongo ahora?»: **Sí** manda al scout a leer el build, los linters, el CI, el README y una muestra del código y propone hasta 10 reglas que ninguna herramienta ya hace cumplir (unos 30–60k tokens, una sola vez por repo; también con `/nxy:mem bootstrap`).

**Aprendizajes automáticos.** El implementer puede cerrar su respuesta con `learned: <texto> | files: a, b` (hasta 5) y `false: A7` si comprobó que un aprendizaje ya no es cierto. Un aprendizaje falso, o cuyos archivos ya no existen, queda obsoleto (se marca, no se borra).

**Cómo saber si sirve.** En la TUI, la pantalla Memoria marca sólo lo raro: una regla «sin citar» (entregada 10 veces o más sin que nadie la cite), un aprendizaje «visto» (2 veces o más), uno «obsoleto». Son umbrales fijos; el 10 es una estimación.

## El plan

El **planner** (Opus, effort high) lee las reglas del repo, busca con `nxy locate`, se fija qué existe para reutilizar y guarda el plan con `nxy plan save`: un JSON que nxy valida y rechaza si está mal (el planner ve los problemas y lo corrige). Tiene criterios `R1…Rn` (lo que tiene que cumplir esta feature, cada uno con su tipo de prueba), lotes y decisiones, y opcionalmente preguntas, riesgos y una línea `Suite:` (`internal/plan/prompt.go`):

```json
{
  "title": "Greet by name",
  "criteria": [{"id": "R1", "text": "greet(name) returns \"Hello, <name>\"", "test": "tdd"}],
  "batches": [{
    "n": 1, "title": "Greeting function", "covers": ["R1"],
    "change": [{"at": "internal/greet/greet.go:12", "what": "add greet(name)"}],
    "test": {"kind": "new", "name": "TestGreet", "accept": "go test ./internal/greet/"},
    "risk": "low"
  }],
  "suite": "go test ./..."
}
```

- **Preguntas.** Si el planner tiene preguntas, las respondés antes del checkpoint.
- **Checkpoint.** El hilo principal copia el plan tal cual: criterios con su tipo de prueba y lotes con qué cubren, qué cambian y riesgo. Después te pregunta **«¿Aprobás el plan <id> r<n> de nxy?»** con **Aprobar**, **Cambiar** o **Ver completo**. **Ver completo** abre el plan entero en tu editor (`~/.nxy/plans/<id>.md`, unos 5 a 15 KB) y Claude te confirma en el chat que lo abrió, con la ruta. **Cambiar** te pide una nota y el planner replanifica con ella; cada replanificación sube la revisión `r<n>` y hay que aprobarla de nuevo.
- **La aprobación es tuya.** La verifica nxy contra tu respuesta a esa pregunta; Claude puede hacerla, no contestarla por vos. Hasta que aprobás no se despacha ningún lote.
- **Los `Accept:` son comandos que corre nxy.** Aprobar el plan aprueba esos comandos: leelos antes de aprobar y, si alguno no te gusta, pedí el cambio. Lo mismo vale para `Suite:`.
- **Permisos.** Los comandos propios de nxy (`nxy batch red|green`, `nxy plan save`) corren sin pedirte permiso, también en modo default. Los `Edit`/`Write` del implementer sobre los archivos que su lote nombra pasan sin preguntar; un edit a cualquier otro archivo pregunta como siempre, así notás cuándo se sale del plan. Las demás llamadas de los agentes siguen preguntando, y el pedido de un agente en segundo plano aparece en tu sesión «from the nxy:implementer agent»: respondelo ahí.
- **Riesgo subido por nxy.** En el checkpoint ves, por ejemplo, `Riesgo: alto (subido por nxy: lente security: internal/auth/x.go)`. nxy mira los archivos de cada lote y sube el riesgo que puso el planner: security, database o una migración → alto; API o contrato entre repos → medio. Nunca lo baja. Si no estás de acuerdo con la profundidad de la review, pedí un cambio al plan.
- **Plan sin guardar.** Si el planner termina sin guardar un plan, ves **Reintentar** o **Parar**.
- **Parar.** `nxy feature stop` termina la feature en cualquier punto.

## Los lotes y su verificación

Aprobado el plan, nxy ejecuta los lotes solo: el hilo principal despacha un `nxy:implementer` por lote y nxy le escribe el prompt (el pedido, los criterios que cubre, qué cambiar, cómo probarlo, las reglas que le tocan y, en un reintento, la falla anterior). No tenés que hacer nada mientras tanto.

**`nxy batch red` y `nxy batch green`.** Quien corre el `Accept` es el binario, no el implementer: lo ejecuta con `sh -c` si hay `sh` y, si no, con `cmd /d /s /c` en Windows, y guarda el exit y las últimas 40 líneas. Un lote con regla `tdd` necesita un rojo (la prueba nueva tiene que fallar; si el `Accept` ya pasa antes de escribir código, el rojo se rechaza: la prueba no prueba nada) y después un verde. Un lote con prueba existente o de build pide sólo el verde. Los lotes `manual` sin `Accept` no piden nada. Un verde sólo vale si el subagente no escribió nada después. Límite conocido: el `Accept` lo escribe el planner para `sh`; en Windows sin `sh`, algo como `test -f x` falla y lo ves como una falla con la salida, no escondida.

**Decide el código, en el momento.** Cuando el implementer vuelve (en primer plano o en segundo plano) nxy decide verde o rojo en ese mismo momento. Si escribió algo después del verde, ese verde no vale y el lote queda en rojo. Un implementer en segundo plano nunca se manda de vuelta: su parada es final, y el veredicto te llega una sola vez, por el hook `Stop`, tu próximo mensaje o la próxima herramienta.

**Reintento y lote fallido.** Un lote en rojo se despacha una vez más con la falla en el prompt, sin preguntarte. Si falla de nuevo ves «Lote n fallido» con la causa y la cola de la salida, y **Reintentar**, **Seguir** (lo acepta así y pasa al siguiente) o **Parar**.

**Lotes en paralelo.** Cada lote declara `depends`; un lote espera a los de su `depends` y, sin `depends`, al lote anterior (así un plan simple va en fila). Cuando varios están listos a la vez, Claude lanza varios implementers en un solo mensaje, uno por lote; cada resultado llega cuando termina el suyo. Dentro de un mismo repo los lotes siempre van de a uno. Para tener paralelo, el plan pone primero el lote de contrato y luego cada repo con `depends: [1]`. Si corrés `nxy batch` a mano con 2 o más lotes en curso, `--batch` es obligatorio.

**Si falla uno.** Un lote que sigue rojo después del reintento queda como fallido; los lotes de otros repos siguen y los que dependen del fallido esperan. Recién cuando no queda nada corriendo ni por arrancar, ves «Lote N fallido» con Reintentar, Seguir o Parar. Con 2 o más fallidos se pregunta de a uno.

**Qué pasa con el hilo principal.** Mientras hay una feature sin terminar el principal no escribe el repo, ni con `Edit` ni por shell. Fuera del repo y sobre `.git` o `.nxy` no pasa nada.

**Si nxy no corre.** Si ves un mensaje `nxy:` del shim con «nxy hooks do nothing until this is fixed», el binario no está o no se puede ejecutar y los hooks no hacen nada; Claude también se entera para frenar en vez de diagnosticar. Bajalo o compilalo de nuevo, o apuntá `NXY_BIN` a un build válido ([configuración](configuracion.md)). En Windows puede ser Defender: el [README](../README.md) explica cómo restaurarlo.

## Los tests afectados

Después del último lote verde, el hilo principal corre un solo comando, `nxy affected --feature <id>` (`internal/affected`, `internal/app/affectedrun.go`), con timeout de 10 minutos y sin pedirte permiso. nxy calcula qué cambió desde que aprobaste el plan (la base es el HEAD de ese momento; los archivos ya sucios al aprobar también cuentan: más tests, nunca menos), elige los tests afectados, los corre y guarda verde, rojo o «sin verificar». Ves una línea «Afectados (fuente: …, N tests)».

De dónde salen los tests:

- **codegraph**: la lista sale de `codegraph affected` sobre los archivos cambiados. Es la fuente preferida.
- **nombres**: si codegraph no está o falla, nxy busca por nombre (`calc.go` → `calc_test.go`, `Calc.java` → `CalcTest`, etc.) y te dice por qué: codegraph no instalado (instalalo con `/nxy:setup`), sin índice, índice bloqueado o sin respuesta.
- **suite**: si no se encontró ningún test, o alguno no tiene runner conocido, y el plan trae `Suite:`, nxy corre ese comando entero.

Qué comando corre cada test sale de una tabla de runners (Go, Angular, vitest, jest, react-scripts, Maven, Gradle, sbt, pytest). Cada test va al marcador más cercano hacia arriba (`go.mod`, `package.json`, `pom.xml`…), así que un monorepo usa el runner de cada carpeta. Tu propia tabla: `.nxy/runners.json` en el repo agrega filas o reemplaza una del mismo `name`. Las filas del repo aparecen en el checkpoint y sólo corren las aprobadas: si cambiás el archivo después de aprobar, nxy no usa las filas nuevas y lo avisa. Un archivo inválido se ignora con un aviso. Ejemplo:

```json
[
  {
    "name": "rake",
    "detect": [{"file": "Gemfile"}],
    "tests": ["*_test.rb"],
    "stems": ["{name}_test"],
    "same_dir": true,
    "command": "bundle exec rake test {tests}",
    "item": "path",
    "sep": " "
  }
]
```

`item` es `path`, `dir` o `class`; `prefix` se antepone a cada test y `sep` los une; `detect` acepta `contains` para buscar un texto en el archivo marcador.

**El índice de codegraph lo maneja nxy.** Si no hay índice, lo crea (`codegraph init`) con un tope de 90 s; si no llega, lo borra (`codegraph uninit -f`) para no dejar uno a medias y usa los nombres, y la próxima feature lo reintenta. Agrega `.codegraph/` a `.git/info/exclude` (no toca tu `.gitignore`). Siempre corre `codegraph sync` antes de consultar (tope 60 s). Para borrar el índice a mano: `codegraph uninit -f`.

**Resultados:**

- **Verde**: pasa a la review.
- **Rojo**: nxy despacha un `nxy:implementer` una vez para arreglar el código (cambia un test sólo si el test está mal, y lo dice) y vuelve a correr `nxy affected`. Si sigue rojo ves **Afectados fallidos** con la causa y la cola, y **Reintentar**, **Seguir** (aceptar así e ir a la review) o **Parar**.
- **Sin verificar**: no hay tests ni `Suite:`, o ningún runner conocido. Ves **Afectados sin verificar** con el motivo y **Reintentar**, **Seguir** o **Parar**. Nunca cuenta como verde.

## La review

Después de los afectados en verde (o de elegir **Seguir**) la feature pasa por una review, siempre, también si el cambio es sólo docs o tests. Mientras corre no tenés nada que hacer: el checkpoint llega solo cuando vuelven todos los revisores.

**Profundidad por riesgo.**

- Bajo: un solo `nxy:reviewer-quick` con la lente base y los criterios del pedido, uno por uno.
- Medio: un `nxy:reviewer` por lente seleccionada, todos en paralelo.
- Alto: lo mismo, y además un `nxy:refuter` que intenta refutar los hallazgos de severidad alta leyendo el código.

**Qué se revisa.** El diff de toda la feature contra el commit que había al aprobar el plan; nxy no commitea por lote, así que no hay un diff por lote. Los archivos ya sucios al aprobar se tratan aparte para no mezclar lo tuyo con lo del plan. Un paquete de review corta el diff a 3.000 líneas como máximo (un archivo nuevo de más de 256 KB entra sólo con la cabecera); un diff de unas 1.000 líneas pesa unos 15k tokens por reviewer. La review completa se guarda en `~/.nxy/reviews/<id>.md` y se borra al cerrar con éxito o con `nxy feature stop`.

**Lentes**, elegidas por qué archivos cambiaron, nunca por cuántas líneas (las embebidas están en `internal/review/lenses/`):

| Si cambió… | Lente |
| --- | --- |
| cualquier código | **base**: alcance vs. el plan, que el `Accept:` pruebe lo que dice, reutilización, convenciones, tests, restos de debug |
| entidades, repositorios, `@Entity`, `@Transactional` | **persistence**: N+1, lazy fuera de transacción, transacciones, consultas |
| controllers, rutas, DTOs | **api**: validación, contratos, errores, autorización |
| componentes Angular / React / Solid | **frontend**: reutilización, estado, suscripciones, render |
| SQL, migraciones, evolutions | **database**: índices, locks, orden y reversibilidad |
| rutas con auth, security, payment, webhook, token… | **security**: autorización, secretos, inyección, datos personales |
| código en 2 o más repos | **contract**: lo que un repo expone coincide con lo que el otro consume, citando ambos lados (`api/...:línea` y `web/...:línea`); sube el riesgo a medio |

Para cambiarlas: `~/.nxy/lenses/<name>.md` (tuyas) o `<repo>/.nxy/lenses/<name>.md` (del repo, gana). Un archivo con el mismo `name` reemplaza a la lente; con `enabled: false` en el frontmatter la quita. Un archivo inválido no frena nada: ves «Aviso:» con el motivo y se usa la lente anterior.

**Filtro de precisión.** nxy descarta, sin preguntarle a ningún modelo, los hallazgos sin `path:línea` o sin un escenario `entrada → resultado`. Los descartados figuran en «Ver completo».

**Del cambio o preexistente.** Un hallazgo es del cambio si apunta a una línea que el plan modificó; si no, es preexistente y se muestra como información. Lo decide nxy por código, no el reviewer.

**Checkpoint de la review.** Arriba la línea de criterios (`R1 ✔ · R2 ✘`), después los hallazgos con su escenario y su arreglo (H1, H2, …), los preexistentes sólo como información, y un conteo de descartados y refutados. Se elige con casillas, hasta 16 hallazgos en 4 preguntas de 4; un grupo de uno suma «Ninguno»; si hay más de 16, nxy te dice cuántos quedan fuera. Marcá los que querés arreglar o elegí «Ninguno».

**Arreglos.** Un único `nxy:implementer` recibe todos los hallazgos elegidos y después nxy corre `nxy affected` de nuevo. Verde: la feature cierra. Rojo: un reintento; si sigue rojo (o queda sin verificar) ves **Arreglos fallidos** con la causa y **Reintentar**, **Seguir** o **Parar**.

**Listo.** El cierre empieza con la palabra (`Listo · R1 ✔ <test> · …`), con `R1 ✔ <test>` por regla y los arreglos aplicados. Se muestra una sola vez. Si los afectados quedaron en rojo (aceptados con Seguir) o sin verificar, lo dice, y los arreglos figuran como «Arreglos pedidos (sin verificar)». Si la review no corrió, los criterios quedan con `?`. Si ves «sin verificar», corré tus tests a mano.

**Escapes.** Si la review dejó pasar algo, `/nxy:review escape "<texto>" --question "<qué resuelve>" [--lens base]` lo registra: con una feature activa queda como candidata para la pregunta del cierre; sin feature activa (apareció en el PR) se guarda ya como regla con origen review. `/nxy:review show` muestra la review de la feature activa (o de la última) y `/nxy:review status` cuenta por lente, en 30 días, los hallazgos que elegiste arreglar, los que ignoraste y los escapes: una lente con muchos ignorados y ningún escape probablemente sobra; una con escapes le falta algo.

## Docs y documenter

Al cerrar con éxito, si hay docs de usuario que nombran un archivo o una clase que el plan cambió (la búsqueda es con `rg`, sin tokens; sin `rg`, un recorrido con tope de 5.000 archivos / 1 MB), ves una pregunta con **Actualizar docs** o **Dejar**. Sólo cuentan los documentos para usuarios: `.md` en la raíz, `README*`, y lo que esté bajo `docs/`, `doc/`, `documentation/` o `wiki/`; `agents/`, `skills/`, `commands/`, `lenses/` y las carpetas con punto no cuentan. **Actualizar docs** despacha `nxy:documenter` (Sonnet, effort low), que sólo puede editar esos docs (y una página nueva en la carpeta wiki si tu CI publica una); cualquier otro archivo se le niega. **Dejar** cierra. Sin docs que nombren el cambio no pregunta nada y cierra con «Listo».

## El modo ingeniero

Para cambios donde querés decidir vos el diseño antes de que nadie planifique.

**Cómo entrás.** `/nxy:engineer <pedido>`, o la oferta **Pasar a engineer** que aparece entre las preguntas de `/nxy:feature` (la otra opción es **Responder acá**). Si pasás a engineer, las preguntas del plan se vuelven las primeras bifurcaciones.

**Diseñando.** `nxy status` dice «Diseñando» y el chat muestra la agenda (`3 de 5 decididas`) con cada bifurcación B1, B2… y su estado. Antes, Claude hace un reconocimiento con `nxy:scout` y guarda un diagrama chico del estado actual (`nxy design today`, en formato d2; la TUI lo dibuja si tenés d2). Todavía no hay plan ni se toca código: se debate. Respondé en el chat, proponé tu opción o pedí opciones.

**El debate.** Claude te refuta o te da 2–3 opciones con su trade-off, y su opinión es condicionada («B, salvo que X»). Si ve un problema que no pediste mirar (N+1, índice, carrera, transacción, contrato, seguridad), lo dice con `path:línea` o lo marca como duda. No hay límite de vueltas ni desempate automático: cambia de opinión sólo con un argumento y lo dice, no cede por insistencia. Si cerrás con tu opción igual, queda registrado que Claude no estaba de acuerdo.

**Opciones de cada bifurcación.** Al abrir una, Claude guarda (`nxy design fork`) sus 2–4 opciones, cada una con resumen, pros, contras, qué toca y un diagrama opcional, más su opinión. La TUI las muestra como tarjetas, con la opinión de Claude detrás de una tecla (no aparece hasta que la pedís, para no condicionarte). Cuando querés elegir desde la TUI, Claude hace la pregunta `nxy feature <id>: ¿qué opción elegís para <fork>?` con esas etiquetas; con texto libre en el chat, el debate sigue ahí.

**Guardar cada decisión.** Ves una pregunta `nxy feature <id>: ¿guardar la decisión D<n>?` con la decisión, el porqué y, si lo hay, «Claude mantiene su desacuerdo: …». **OK, guardar** la deja en la base en el acto; **Seguir debatiendo** no guarda nada. Tus mensajes mientras diseñás se guardan tal cual como referencias para el planner.

**Cerrar.** Cuando están todas decididas, Claude propone cerrar: ves las decisiones, los criterios R1…Rn y el diagrama como fuente. **Cerrar y planificar** aprueba los criterios y arranca el planner; **Seguir diseñando** vuelve. Si decís «decidí vos el resto», las bifurcaciones abiertas quedan delegadas al planner y el plan las marca como «las eligió el planner».

**`/clear` es seguro.** Las decisiones ya están en la base: la guía vuelve sola con la agenda, lo decidido y tus últimos 10 mensajes de este diseño.

**Planner y huecos.** El planner sólo baja tu diseño a lotes y copia los criterios tal cual. Si encuentra un hueco de diseño, frena con `nxy plan gaps` y volvés a Diseñando con una bifurcación nueva. El checkpoint muestra sólo los lotes (los criterios ya los aprobaste) y el plan `.md` suma una sección `### Diseño`. En la review, el pase base explica el diff por decisión (`D1 → path:línea — cómo`) y cada hallazgo trae el porqué y una regla general para aprender de él.

nxy no valida la sintaxis d2: un bloque roto se ve recién al renderizarlo.

## Varios repos y worktrees

Una feature puede tocar varios repos (por ejemplo `api` y `web`), abriendo la sesión dentro de uno o en una carpeta padre que no es repo.

- **Repos en el checkpoint.** El plan lleva una línea de repos (nombre, rol, ruta) y cada lote y decisión lleva `[repo]`. Los archivos se escriben `<nombre>/<ruta>`; el nombre es la carpeta del repo (si dos se llaman igual, nxy suma padres y te avisa). Un repo declarado sin git: el plan no se guarda. Un lote toca un solo repo.
- **Repo ocupado.** Hay una sola feature activa por checkout. Si el plan toca uno que ya tiene otra feature, el plan no llega a la aprobación y Claude te pregunta: «Pausar la feature X» (recomendada), «Sacar <repo> del plan» (con 2 o más repos), «Abrir en un worktree» (con un repo) o «Parar esta feature». Pausar o abrir en worktree sigue con el mismo plan, sin llamar de nuevo al planner.
- **Rama y bases.** Al aprobar, Claude pregunta la rama (sugerencias según el prefijo más usado del repo, o escribila en Other) y la base de cada repo (la por defecto va recomendada). Una base que existe sólo en origin también se ofrece y la rama se crea desde `origin/<base>`. Si git falla (por ejemplo cambios sin commitear que chocan con la rama), vuelve a preguntar con el mensaje de git; nxy nunca hace stash ni force, y nunca commitea ni pushea.
- **Con un repo** la rama se crea en la carpeta de la sesión (`git switch -c <rama> <base>`, o `git switch <rama>` si existe y está libre). **Con 2 o más repos**, o con el checkout ocupado, nxy crea un worktree por repo en `<repo>.worktrees/<slug>` y te dice cómo abrirlos (otra ventana de tu editor o `cd`). Un worktree nuevo no trae dependencias (`node_modules`, build): nxy avisa cuáles faltan y no instala nada. Editar el checkout original de un repo que ya tiene worktree se rechaza («editá en <worktree>»).
- **Aprendizajes.** Con 2 o más repos, un `false:` apunta al repo: `false: [api] A3` o `false: api:A3`. Las reglas se entregan por repo y una decisión `[repo]` se guarda en ese repo.

**Comandos** (`/nxy:worktree` o `nxy feature …`): `list` muestra las features activas y pausadas con repos, rama y worktrees (también desde una carpeta padre que no es repo); `new --name <n> --base <rama> [--issue <n>]` (o `new --branch <b>`) crea un worktree con base explícita y muestra el comando git que corrió; `pause` y `resume` (`--feature <id>` o `--path`) pausan o retoman una feature y la pausa suelta sus checkouts para que otra los use, y el worktree lleva una marca en `.nxy/local/features/paused.json`; `close --path <wt>` o `close --feature <id>` cierra worktrees: con cambios sin guardar pide `--force`, y no cierra el principal, el de esta sesión ni uno con una feature sin terminar.

Costo: cada worktree ocupa un checkout más en disco y codegraph arma un índice por worktree (segundos). N implementers a la vez son N veces el contexto de un implementer: el mismo total que en fila, pero más rápido.

## La TUI

`nxy tui` abre una consola de control en tu terminal, con tus datos reales: la base, los transcripts, tu config y tus herramientas. No es un panel de Claude Code ni se instala como Mod: es otro proceso, y Claude Code sigue en su terminal. `nxy tui --demo` abre el mismo programa con datos de ejemplo y no toca tu repo. Como `nxy` casi nunca está en el PATH, `/nxy:tui` imprime el comando exacto para pegar en otra terminal (es lo mismo que `nxy tui where`); para que alcance con escribir `nxy tui`, usá la fila «nxy en el PATH» de `/nxy:setup` ([configuración](configuracion.md)).

**Qué ves.** Cuatro secciones fijas arriba: Inicio, Memoria, Gasto y Config. Lo demás se abre con `enter` y se vuelve con `esc`; el camino aparece debajo de la barra (por ejemplo `Inicio › F7K2 › Plan › calc.go`). Las 11 pantallas son Inicio, Worktrees, Feature, Agentes, Decisión, Ingeniero, Plan, Archivo, Memoria, Gasto (con Tendencia) y Config. Cada una carga sus datos al entrar; si una falla ves un bloque rojo con el error y las demás siguen andando. Se refresca sola cuando nxy escribe en su base, y cada segundo mientras un agente trabaja. Si nxy se actualiza con la TUI abierta aparece «nxy se actualizó: cerrá y volvé a abrir» y las acciones se apagan.

**Qué significa cada señal.**

- Etiqueta de estado de cada feature: Te necesita, Trabajando, Verificando o Listo.
- Etapa: Diseño, Plan, Lotes, Review y Cierre, con ✓ en las que ya pasaron.
- Bloque rojo en una feature: el lote que falló, con el test, el error y qué reglas recibió.
- `● N decisiones` arriba a la derecha: preguntas esperando tu respuesta, en una ventana encima de la pantalla.
- Memoria: sólo se marcan las cosas raras (sin citar, obsoleto, visto N veces).
- Impacto de un lote: ✓ test que pasa, ✗ test que falla, ○ sin correr, ↔ contrato con otro repo.

**Qué hacer en cada pantalla.** Las teclas disponibles están siempre en la barra de abajo; `?` muestra todas y `q` sale.

- Inicio: `↑↓` mueve, `enter` abre la feature, `d` la decisión, `w` los worktrees, `space` pausa o reanuda la feature elegida.
- Feature: `space` pausa o reanuda, `a` agentes, `p` plan, `d` decidir, `enter` sobre un lote va a su agente.
- Plan: `←→` cambia de lote, `↑↓` de archivo, `enter` abre el archivo. Si está por aprobar: `a` aprobar, `c` cambiar, `x` parar la feature (pide `y`/`n`).
- Archivo: `n` / `N` saltan entre cambios, `v` alterna diff y archivo entero, `o` abre el archivo en tu editor (`$VISUAL` o `$EDITOR`), `r` recalcula.
- Ingeniero: `↑↓` opción, `enter` elige (sólo si Claude ya hizo la pregunta de la bifurcación), `h` cómo es hoy, `c` qué elegiría Claude.
- Memoria: `[ ]` cambia de repo o global, `/` busca, `e` edita, `x` borra (`y` confirma, `u` deshace durante 5 segundos), `+` crea una regla o un aprendizaje, `y` / `n` aceptan o descartan propuestas.
- Gasto: `tab` pasa a Tendencia, `s` elige otra sesión.
- Config: `↑↓` recorre, `e` / `f` ciclan modelo y effort del rol, `r` lo guarda en el repo, `i` instala o quita la herramienta, `c` copia el comando.

Las acciones son reales: corren como un subproceso del mismo `nxy`, en segundo plano. Las que cambian algo fuera de la TUI piden `y`/`n` en el pie y dicen qué archivo tocan; si fallan, un aviso rojo con la primera línea del error. Cada acción se corta a los 10 minutos.

**Contestar desde la TUI.** Cuando Claude hace una pregunta de nxy (aprobar el plan, el resultado de una review, etc.) y tenés la TUI abierta, la terminal de Claude Code muestra «nxy: si tenés la TUI abierta, contestá ahí» y el título dice «● nxy: decisión pendiente»; en la TUI aparece la ventana de Decisión.

Cómo funciona (`internal/app/askwait.go`): una TUI viva escribe una señal `~/.nxy/run/tui-<pid>` que renueva cada 2 s y borra al cerrar. Con ella, el hook de `AskUserQuestion` retiene la pregunta del hilo principal en vez de mostrar el diálogo, siempre que todas las preguntas de la llamada sean de nxy y la TUI sepa mostrarlas (guardar una decisión D<n> y cerrar el diseño se contestan en el chat). Lo que elegís en la TUI vuelve a Claude como si lo hubieras contestado en el diálogo. Sin TUI abierta el hook sale al instante y no cambia nada.

- Elegí con `↑↓` y `enter`. Con varias preguntas en una llamada la ventana dice «pregunta 1 de 3» y se contesta de a una; en las de varias opciones `space` marca. Se envía todo junto al contestar la última.
- `t` en la ventana dice «contestar en la terminal»: el hook suelta la pregunta y aparece el diálogo normal. Lo mismo pasa si cerrás la TUI o pasan 25 minutos sin respuesta.
- Con dos TUI abiertas gana la primera que contesta; la otra ve «ya contestada» y se recarga.
- `x` en el plan por aprobar para la feature (pide `y`/`n`): corre `nxy feature stop --feature <id>` y el hook niega la pregunta. La pregunta de aprobación no tiene «Parar», por eso es una acción aparte.
- Mientras espera queda vivo un proceso de nxy. Si el hook deja de renovar una pregunta retenida por más de 10 s (por ejemplo, se cerró Claude Code), desaparece de la TUI.

El modelo no puede contestarse solo: `nxy action decide` sólo lo corre la TUI (con un nonce propio) y el hook de Bash y PowerShell rechaza ese comando.

**`nxy view`: las pantallas como JSON.** `nxy view <pantalla> --json` (`cmd/nxy/view.go`) imprime lo que mostraría esa pantalla con los datos reales de tu repo. No cambia nada: sólo lee. Las pantallas son `cockpit` (Inicio), `agents`, `decision`, `memory`, `stats`, `trend`, `config`, `plan`, `file` (con `--path <ruta>`) y `engineer`. Opciones: `--feature <id>` (sin ella, la feature activa de la carpeta), `--scope repo|global` (memory), `--session <id>` (stats) y `--cwd <carpeta>`. Un `decision`, `plan` o `engineer` de una feature que no tiene eso imprime `null`. Y `nxy tui --dump <pantalla> --width 80 --plain` imprime la pantalla como texto y sale.

Cosas que conviene saber al leerlo:

- **Costo de una feature:** incluye el hilo principal (lo que se habló en la sesión desde que empezó la feature) más todos sus subagentes, por eso es mayor que la suma de los agentes. Si Claude Code borró el transcript (a los 30 días), el costo de una feature vieja baja a lo que guardó la base.
- **`elapsed_ms` en -1:** el lote no tiene hora de inicio (todavía no arrancó, o es de una feature guardada antes de que nxy anotara las horas). No se inventa una.
- **Impacto del plan** (`view plan`): `Source` dice de dónde sale. `codegraph` viene del índice; `name` es una adivinanza por nombre y puede sobrar o faltar; `none` es que no hubo con qué calcularlo. Si ves `name`, instalá codegraph con `/nxy:setup`.
- **`view file`:** el diff de toda la feature contra el commit de la aprobación, qué lotes lo editan, y las reglas, aprendizajes y features que tocan ese archivo. Los fines de línea CRLF y LF no cuentan como cambio.
- **Diagramas:** los dibuja d2 como texto. Sin d2 ves la fuente d2 y la pista de instalarlo desde Config (opcional).

## Los hooks

Todos los eventos pasan por el shim `bin/nxy.cmd`, que corre `nxy hook` (el binario Go, que el shim baja y verifica la primera vez). Fuera de una feature y sin nada que filtrar no ves nada: ni salida de hook, ni herramienta bloqueada, ni texto extra.

| Evento | Qué hace |
| --- | --- |
| `SessionStart` | Importa los datos de la 1.x una vez por repo; resuelve el motor del filtro; re-inyecta el paso de la feature activa (sobrevive a `/clear`); ofrece las notas de la carpeta; avisa si quedó una feature de la 1.x a medias o si hay una 1.x instalada y activa; en auto mode, le pide a Claude que anteponga `rtk` |
| `UserPromptSubmit` | Arranca la feature de `/nxy:feature` y `/nxy:engineer` (guarda tu pedido tal cual); guarda tus mensajes como referencias del diseño; entrega un paso pendiente; el aviso de «siempre / nunca» para proponer una regla; la pregunta de las notas |
| `PreToolUse` Bash/PowerShell | Reescribe el comando por rtk; frena las escrituras por shell del hilo principal; frena una búsqueda amplia con el contexto pasado; rechaza `nxy action decide` |
| `PreToolUse` Grep/Glob | El freno de lectura |
| `PreToolUse` Edit/Write/NotebookEdit/MultiEdit | El freno de escritura; cuenta las escrituras del implementer y verifica que su lote nombre el archivo |
| `PreToolUse` Agent/Task | Escribe el prompt del implementer, del reviewer, etc. a partir del plan guardado; aplica el modelo y el effort por rol; inyecta las reglas |
| `PreToolUse` AskUserQuestion | Retiene la pregunta si hay una TUI viva (hasta 25 min); timeout de 1800 s |
| `PostToolUse` Bash/PowerShell | Suma la llamada al resumen del filtro |
| `PostToolUse` Agent/Task | Decide el veredicto del lote cuando el subagente vuelve, y el paso siguiente |
| `PostToolUse` AskUserQuestion | Registra la respuesta (la aprobación del plan, las decisiones) |
| `SubagentStop` | El veredicto de un subagente en segundo plano, que se entrega una sola vez |
| `Stop` | Entrega un paso pendiente (un checkpoint) al terminar el turno; si trabajaste en una carpeta sin feature, ofrece dejar una nota |
| `SessionEnd` | Registra en la base cómo terminó la sesión |

Todos fallan abiertos: ante cualquier error dejan pasar lo que Claude iba a hacer. `NXY_DEBUG=1` muestra la causa en stderr. Cada hook tiene un timeout de 10 s (salvo `AskUserQuestion`); el shim baja el binario en 8 s como máximo.

## Estructura del código

nxy es un solo binario Go (`cmd/nxy`) más los archivos que Claude Code busca en la raíz del plugin (`agents/`, `commands/`, `skills/`, `hooks/`, `bin/nxy.cmd`, `.claude-plugin/`).

```
cmd/nxy/                 # el único lugar que conecta adaptadores con casos de uso: un archivo por comando
internal/
  domain/ flow/ plan/    # el modelo (feature, plan, lote) y la máquina de estados del flujo
  review/ gate/ filter/  # review (lentes, riesgo, hallazgos), frenos, decisión del filtro
  affected/ design/ docs/ memory/ legacy/ ...  # tests afectados, modo ingeniero, docs, memoria, datos de la 1.x
  metrics/ pricing/ report/ promptcache/ format/  # costos, reportes de stats/trend/statusline
  scout/ repomap/ searchscope/ setup/ configedit/ worktree/ repos/ ask/  # búsqueda, instalación, config, worktrees
  app/                   # los casos de uso: hooks, stats, trend, memoria, review...
  host/claudecode/       # lo específico de Claude Code: transcripts, settings, formato de los hooks
  store/sqlite/          # la base ~/.nxy/nxy.db y sus migraciones
  platform/              # archivos, git, config, rutas, shell
  tools/                 # rtk, rg, codegraph, d2
  views/ tui/            # las pantallas como datos, y la TUI que las dibuja
  runner/ update/        # reservados para correr procesos y actualizar el binario
  archtest/              # el test que hace cumplir las reglas de abajo
```

**Reglas de capas** (`internal/archtest/rules.go`, las verifica un test):

- Los paquetes «puros» (el modelo, `flow`, `plan`, `review`, `gate`, `filter`, `memory`, `affected`...) sólo pueden depender de otros puros y de la biblioteca estándar, y nunca de `os/exec`, `net`, `net/http` ni `database/sql`: no hacen entrada/salida.
- `internal/memory` además no puede depender de ningún host: no sabe qué es Claude Code.
- `internal/app` (los casos de uso) sólo importa paquetes puros y la biblioteca estándar: habla con el mundo a través de interfaces.
- Los adaptadores (`store/sqlite`, `host/claudecode`, `tools`, `tui`...) y los de apoyo (`platform`, `views`) no se importan entre sí: sólo `cmd/nxy` los conecta.
- `internal/tui` sólo puede importar `views` y paquetes puros que no sean de un host.

## Datos de la 1.x

Qué ves: la primera vez que abrís un repo con la v2, si la 1.x tenía datos de ese repo, un aviso único: `nxy: datos de la 1.x importados — 12 convenciones → aprendizajes A1..A12 · 3 handoffs vivos → notas · 41 archivados → features cerradas · 30 sesiones del filtro · 26 reviews, 88 elegidos`. Es una copia (`internal/app/legacyimport.go`): nxy nunca escribe un archivo de la 1.x, así que volver a ella funciona. Si el repo no tiene datos de la 1.x, no sale ningún aviso.

Qué significa cada conteo:

- **Convenciones (y decisiones, bugs, preferencias) → aprendizajes `A<n>`**, con los archivos a los que estaban atadas. No son reglas todavía.
- **Handoffs vivos → notas.** Entra el objetivo y el `## Next` primero, después lo demás hasta 2 KB, sin el plan. Si se cortó dice «(cortado al importar; completo en la 1.x)». Los que llevan más de 30 días sin escribirse no entran (se cuentan como vencidos).
- **Archivados → features cerradas**, con sus archivos, para que el planner las encuentre por archivo.
- **Sesiones del filtro:** el resumen por sesión que leen `nxy stats` y `nxy trend`. **Reviews y elegidos:** la base de «elegidos vs ignorados por lente».
- **Memorias globales → aprendizajes globales.** Entran una sola vez por máquina (no por repo) y el aviso dice `3 globales → aprendizajes globales A13..A15`. No se entregan a ningún agente: `nxy mem promote A<n> --question "<q>"` vuelve `G` los que sigan valiendo.
- Lo que no entra se cuenta: reemplazadas por otra, relaciones entre memorias que no son de archivo, «N privadas sin importar» (las memorias `private` nunca se importan) y «N repetidos sin importar» (aprendizajes que ya estaban).

Memoria y reviews se importan una sola vez por repo; lo que la 1.x siga escribiendo en el filtro entra solo en el próximo arranque (leyendo sólo lo nuevo). Una sesión que ya escribió la v2 no se pisa con filas de la 1.x.

Si la importación falla, sale una línea con «probá `nxy legacy import`». La copia de la memoria se reintenta como mucho una vez por día en el arranque.

**Una feature de la 1.x a medias.** Si la 1.x dejó en ese repo un plan aprobado con lotes sin cerrar (y no hay una feature de la v2 activa), ves un aviso con el objetivo del plan («faltan los lotes 2 y 3» o «aprobada, sin lotes corridos») y la pregunta «¿La planifico de nuevo con la v2?». El plan viejo no se retoma: **Planificarla de nuevo** arranca una feature de la v2 cuyo pedido es el objetivo de la 1.x con su plan como referencia, e **Ignorarla** la descarta. Si no contestás, se vuelve a preguntar en el próximo arranque. Cada plan se pregunta una sola vez.

Qué hacer:

- `nxy legacy status` muestra qué entró en cada repo y qué se contestó de las features a medias. `nxy legacy import [--dir <d>]` lo corre a mano.
- `nxy mem promote A<n> --question "<qué resuelve>"` vuelve regla una convención que sigue valiendo.
- `nxy note list` muestra los handoffs importados como notas; `nxy note done <id>` borra los que ya no sirven.

Costo: la primera vez por repo, menos de 200 ms (una copia temporal de `memory.db`, de unos 2,5 MB, que se borra al terminar); después, unos milisegundos por arranque. Las sesiones del filtro con más de 90 días se barren.
