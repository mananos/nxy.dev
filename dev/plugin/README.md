# nxy 2.0 dev plugin

Dev only. Same hook events as the 1.x plugin, but each one runs the Go binary through the shim `bin/nxy.cmd`. The binary runs the F2.1–F2.5 flow (`/nxy:feature` or `/nxy:engineer` → planner → `nxy plan save` → approval → batches → affected tests → review) and the Bash filter from F1; other events answer nothing. It also runs the F3 utility commands (stats, trend, statusline, setup, filter, config, locate, worktrees, docs).

Try it:

```
go build -o <dir>/nxy.exe ./cmd/nxy        # no .exe suffix on Linux/macOS
NXY_BIN=<dir>/nxy.exe claude --plugin-dir dev/plugin
```

Outside a feature you should see nothing: no hook output, no blocked tool, no extra text in the session. A stderr line starting with `nxy:` means the shim could not find or fetch the binary.

Since F2.1 this plugin runs the feature flow and the filter itself, so it cannot sit beside the installed 1.x plugin: both would react to every event. Its name is `nxy` (needed for `/nxy:feature`), the same as 1.x, so disable the installed 1.x plugin while you load it.

Download limits: the shim fetches at most 8 s in total (binary 6 s, checksums 2 s), under the 10 s hook timeout. Only one hook downloads at a time (lock folder `nxy.exe.lock` beside the target); the others print one `nxy:` line and do nothing. A lock older than a minute and temp files older than a day, left by dead runs, are removed on the next start.

Known limit: the SHA-256 in `checksums.txt` comes from the same GitHub Release as the binary. It protects against corruption and truncation, not against a compromised release. A signature checked against a pinned key (cosign or minisign) is a later step.

## F1: the filter

PreToolUse on Bash/PowerShell now rewrites the command through rtk exactly like the 1.x plugin (same golden-tested rules). Disable the installed 1.x plugin while you load `dev/plugin`: both would rewrite the same command. Everything else still answers nothing.

Two differences with 1.x, both in how fast the answer comes, not in what it says:

- rtk's answer is remembered per exact command, so a command you already ran (`git status`, `go test ./...`) is rewritten without starting rtk: about 55 ms less per Bash call on Windows. The cache is one file, `~/.nxy/cache/rewrite-v2.json`: at most 500 commands (about 75 KB), each kept 7 days, the oldest dropped first. It is thrown away whole when rtk's path or version changes, or when rtk's own config (`rtk config` shows where) is created, edited or removed. Errors and timeouts are never kept. `NXY_REWRITE_CACHE=0` skips it.
- Changing `filter.engine` (config or `NXY_ENGINE`) or `NXY_RTK_PATH` takes effect on the next command. 1.x kept the old choice for up to 6 h.

Startup time: `nxy bench hook [-n 20] [-command "git status"] [-shim bin/nxy.cmd] [-bin <nxy>]` runs the real hook and prints min/median/p90/max in ms against the 50 ms threshold. Rows: `filter off` (process start and config only), `filter on, no cache` (what a command seen for the first time pays: rtk is started), `filter on` (a repeated command, answered from the cache) and, with `-shim`, the same through Git Bash with the filter off and on; `via shim, filter off` minus `filter off` is what the shell costs by itself. It always exits 0; CI prints the table in the job summary on Linux and Windows.

Golden files: regenerate with `node scripts/golden/filter.mjs` (add `--record` for new commands, needs rtk; the hook area needs `go`). `--check` only compares.

## F2.1: `/nxy:feature`

Primer pedazo del flujo nuevo: el binario lleva la máquina de estados, valida y guarda el plan, y verifica que apruebes vos.

Cómo probar: `claude --plugin-dir dev/plugin` con la 1.x desactivada (ambas se llaman `nxy`). Si la 1.x sigue activa, SessionStart te muestra un aviso: desactivala, si no las dos reaccionan al mismo comando.

Qué ves:

1. Escribís `/nxy:feature <pedido>`. nxy guarda el pedido literal y le da al hilo principal una instrucción que empieza con `nxy:` (despachar al planner). El planner arma el plan y lo guarda con `nxy plan save`; si el plan no valida, ve los problemas y lo corrige.
2. El hilo principal copia el checkpoint tal cual (criterios con su tipo de prueba y lotes con qué cubren, qué cambian y riesgo) y te pregunta: **Approve**, **Change** o **Ver completo**. Ver completo abre el plan entero en tu editor y Claude te confirma en el chat que lo abrió, con la ruta. Change te pide una nota y el planner replanifica con ella.
3. Si el planner tiene preguntas, las respondés antes del checkpoint.

Estados públicos (los de `nxy status`):

- **Trabajando**: nxy o un subagente está haciendo algo; no hace falta que hagas nada.
- **Verificando**: se está corriendo una prueba.
- **Necesita tu decisión**: te toca a vos (responder, aprobar, reintentar o parar).
- **Listo**: la feature terminó, o la paraste.

Después de aprobar, la feature ejecuta los lotes (ver F2.2 abajo). Mientras hay una feature sin terminar el hilo principal no puede editar archivos del repo (los de fuera, como `~/.claude`, sí). `nxy status` te dice dónde está (por ejemplo "Lote 1"). Para terminar: `nxy feature stop`; libera las ediciones del repo y borra el plan .md y la review .md.

Planner en segundo plano: si Claude lanza el planner sin esperarlo, nxy le dice "esperá" al hilo principal (no lo relanza). El plan te llega al final de ese turno o en tu próximo mensaje, una sola vez. Si hacés `/clear` con una feature pendiente, ves una línea "nxy: feature … pendiente": escribí cualquier cosa para seguir.

Dónde queda el plan: `~/.nxy/plans/<id>.md`, uno por feature, de unos 5 a 15 KB. Se pisa en cada Ver completo y se borra al parar.

## F2.2: los lotes

Al aprobar el plan, nxy ejecuta los lotes de a uno, solo: el hilo principal despacha un `nxy:implementer` por lote y nxy le escribe el prompt (el pedido, los criterios que cubre el lote, qué cambiar y cómo probarlo). No tenés que hacer nada mientras tanto.

Qué ves:

- **Trabajando / Verificando**: el implementer escribe la prueba y el código, y prueba con `nxy batch red` y `nxy batch green`. Esos comandos corren el `Accept` del plan por vos (con `sh`, o `cmd /c` en Windows sin `sh`) y nxy guarda el resultado; el implementer no corre el `Accept` por su cuenta. Un lote con regla `tdd` necesita un rojo (la prueba nueva falla) y después un verde. Si el `Accept` ya pasa antes de escribir código, el rojo se rechaza: la prueba no prueba nada.
- Cuando el implementer vuelve (en primer plano o en segundo plano), nxy decide verde o rojo en ese mismo momento. Si escribió algo después del verde, ese verde ya no vale y el lote queda en rojo.
- **Reintento automático**: si el lote queda en rojo, nxy lo despacha una vez más con la falla en el prompt. No te pregunta.
- **Lote fallido**: si falla de nuevo, ves "Lote n fallido" con la causa y la cola de la salida, y una pregunta con **Reintentar** (lo corre otra vez), **Seguir** (lo acepta así y pasa al siguiente) o **Parar** (termina la feature).
- Si el planner termina sin guardar un plan, la pregunta es **Reintentar** o **Parar**.
- Después del último lote verde la feature se detiene: sigue la corrida de los tests afectados (ver F2.3 abajo); después viene la review (ver F2.4 abajo). Para terminar: `nxy feature stop`.

Permisos:

- **Qué ves**: el checkpoint muestra el `Accept:` de cada lote. Qué significa: aprobar el plan aprueba esos comandos, que nxy corre por el implementer. Qué hacer: leelos antes de aprobar; si alguno no te gusta, pedí el cambio.
- **Qué ves**: los comandos propios de nxy (`nxy batch red|green`, `nxy plan save`) corren sin pedirte permiso, incluso en modo default. Qué hacer: nada. Los Edit/Write del implementer sobre los archivos que su lote nombra en el checkpoint ("Cambiar: calc.go:5 — …") pasan sin preguntar; un edit a cualquier otro archivo pregunta como siempre, así notás cuándo se sale del plan. Las demás llamadas de los agentes siguen preguntando como siempre, y el pedido de un agente en segundo plano aparece en tu sesión "from the nxy:implementer agent": respondelo ahí.

Qué cambia para el hilo principal:

- Tampoco puede escribir el repo por el shell: `echo x > archivo`, `sed -i`, `tee`, `cp`, `mv` y similares sobre archivos del proyecto se rechazan mientras hay una feature sin terminar. Fuera del repo y sobre `.git` o `.nxy` no pasa nada.
- Hay una sola feature sin terminar por repo. Si ya hay una, `/nxy:feature` te dice cuál es; usá `nxy feature stop` para liberarla. Si tenías duplicados de antes, la actualización deja viva sólo la más nueva.

**Stop hook error.** Qué ves: cuando un paso pendiente (un checkpoint) llega justo al terminar el turno, Claude Code lo rotula "Stop hook error". Qué significa: el rótulo es de Claude Code, no es un error; nxy usa ese camino para que Claude siga. Qué hacer: nada.

**Si nxy no corre.** Qué ves: un mensaje `nxy:` del shim con "nxy hooks do nothing until this is fixed". Qué significa: el binario no está o no se puede ejecutar, y los hooks no hacen nada. Además Claude se entera (al iniciar la sesión, con tu prompt o después de una herramienta) para frenar en vez de diagnosticar pasos de nxy. Qué hacer: bajalo o compilalo de nuevo, o apuntá `NXY_BIN` a un build válido. En Windows puede ser que Defender haya puesto el binario en cuarentena (por ejemplo Bearfoos.A!ml): recompilalo y excluí la carpeta del antivirus.

Límite conocido: el `Accept` lo escribe el planner para `sh`. En Windows sin `sh` se usa `cmd /c`, y un `Accept` tipo `test -f x` falla ahí; lo ves como una falla con la salida, no escondida.

## F2.3: tests afectados

Después del último lote verde, el hilo principal corre un solo comando, `nxy affected --feature <id>`, con timeout de 10 minutos y sin pedirte permiso. nxy calcula qué cambió desde que aprobaste el plan (la base es el HEAD de ese momento; los archivos ya sucios al aprobar también cuentan: más tests, nunca menos), elige los tests afectados, los corre y guarda verde, rojo o "sin verificar".

De dónde salen los tests (ves una línea "Afectados (fuente: …, N tests)"):

- **codegraph**: la lista sale de `codegraph affected` sobre los archivos cambiados. Es la fuente preferida.
- **nombres**: si codegraph no está o falla, nxy busca por nombre (`calc.go` → `calc_test.go`, `Calc.java` → `CalcTest`, etc.) y te dice por qué, con una de estas líneas: "afectados por nombre: codegraph no está instalado — instalalo con /nxy:setup", "afectados por nombre: sin índice de codegraph (no se pudo crear en 90 s; se reintenta en la próxima feature)", "afectados por nombre: el índice de codegraph está bloqueado", "afectados por nombre: codegraph no respondió (sync o consulta fallida)".
- **suite**: si no se encontró ningún test, o alguno no tiene runner conocido, y el plan trae `Suite:`, nxy corre ese comando entero.

Qué comando corre cada test sale de una tabla de runners (Go, Angular, vitest, jest, react-scripts, Maven, Gradle, sbt, pytest). Cada test va al marcador más cercano hacia arriba (`go.mod`, `package.json`, `pom.xml`…), así que un monorepo usa el runner de cada carpeta.

El índice de codegraph lo maneja nxy; no tenés que acordarte de nada:

- Si no hay índice, nxy lo crea (`codegraph init`) con un tope de 90 s. En el código Go de este repo (114 archivos) tardó 1,5 s y pesó 4,8 MB (medido). Para un repo de unos 10 mil archivos se estima ~35–60 s y cientos de MB (estimación, no medido). Si no llega en 90 s, nxy lo borra (`codegraph uninit -f`) para no dejar uno a medias y usa los nombres; la próxima feature lo reintenta.
- Agrega `.codegraph/` a `.git/info/exclude` (no toca tu `.gitignore`). Para borrar el índice: `codegraph uninit -f`; nxy nunca lo borra salvo cuando falla al crearlo.
- Siempre corre `codegraph sync` antes de consultar (tope 60 s). Si queda un lock viejo, hace `codegraph unlock` y reintenta una vez.
- Cuando lo crea ves: "índice de codegraph creado en N s (.codegraph/, ignorado en .git/info/exclude)".

Tu propia tabla: `.nxy/runners.json` en el repo agrega filas o reemplaza una del mismo `name` (las del repo van primero). Si el archivo es inválido, nxy avisa y usa la tabla embebida. Ejemplo:

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

Una fila con el mismo `name` que una embebida (por ejemplo `go`) la reemplaza. Las filas del repo aparecen en el checkpoint del plan y sólo corren las aprobadas: si cambiás `.nxy/runners.json` después de aprobar, nxy no usa las filas nuevas o cambiadas y lo avisa.

`item` es `path`, `dir` o `class`; `prefix` se antepone a cada test y `sep` los une; `detect` acepta `contains` para buscar un texto en el archivo marcador.

`Suite:` en el checkpoint: el planner puede proponer el comando que corre toda la suite (por ejemplo `npm test`). Qué significa: nxy lo usa sólo si no puede calcular los afectados. Qué hacer: leelo antes de aprobar, igual que los `Accept:`.

Resultados:

- **Verde**: pasa a la review (ver F2.4 abajo).
- **Rojo**: nxy despacha un `nxy:implementer` una vez para arreglar el código (cambia un test sólo si el test está mal, y lo dice) y vuelve a correr `nxy affected`. Durante el arreglo ese comando sólo informa, no registra nada. Si sigue rojo ves **Afectados fallidos** con la causa y la cola, y **Reintentar** (otro intento de arreglo), **Seguir** (aceptar así e ir a la review) o **Parar**.
- **Sin verificar**: no hay tests ni `Suite:`, o ningún runner conocido. Ves **Afectados sin verificar** con el motivo y **Reintentar** (corre `nxy affected` otra vez, por ejemplo después de agregar `.nxy/runners.json` o un suite), **Seguir** (sin verificar) o **Parar**. Nunca cuenta como verde.

Costo: un `sync` sin cambios tardó 0,85 s y la consulta 0,44 s en este repo (medido); el resto es el tiempo de tus tests. Tope total del comando: 9 min 30 s.

A validar en vivo: los comandos por defecto de cada stack (`./mvnw` y `./gradlew` necesitan `sh`; `python -m pytest` vs `python3`) y que en Maven multi-módulo el marcador más cercano gana (un `mvnw` en la raíz con `pom.xml` en el módulo usa `mvn` simple).

## F2.4: review

Después de los afectados en verde (o de elegir **Seguir**), la feature pasa por una review. Se hace siempre, también si el cambio es sólo docs o sólo tests.

**Riesgo subido por nxy.** Qué ves: en el checkpoint del plan, `Riesgo: alto (subido por nxy: lente security: internal/auth/x.go)`. Qué significa: nxy mira los archivos de cada lote y sube el riesgo que puso el planner: security, database o una migración → alto; API → medio. Nunca lo baja. Qué hacer: si no estás de acuerdo con la profundidad, pedí un cambio al plan; no hay nada que configurar.

**Profundidad por riesgo.**

- Bajo: un solo `nxy:reviewer-quick` con la lente base y los criterios del pedido, uno por uno.
- Medio: un `nxy:reviewer` por lente seleccionada, todos en paralelo.
- Alto: lo mismo, y además un `nxy:refuter` que intenta refutar los hallazgos de severidad alta leyendo el código.

Mientras corren no tenés nada que hacer; el checkpoint llega solo cuando vuelven todos.

**Lentes.** Vienen embebidas las de la 1.x (base, api, database, frontend, persistence, security). Para cambiarlas: `~/.nxy/lenses/<name>.md` (tuyas) o `<repo>/.nxy/lenses/<name>.md` (del repo, gana). Un archivo con el mismo `name` reemplaza a la lente; con `enabled: false` en el frontmatter la quita. Un archivo inválido no frena nada: ves "Aviso:" con el motivo y se usa la lente anterior.

**Filtro de precisión.** Qué significa: nxy descarta sin preguntarle a ningún modelo los hallazgos sin `path:línea` o sin un escenario `entrada → resultado`. Qué hacer: nada; los descartados figuran en "Ver completo".

**Checkpoint 2.** Qué ves: arriba la línea de criterios (`R1 ✔ · R2 ✘`), después los hallazgos con su escenario y su arreglo (H1, H2, …), los preexistentes sólo como información, y un conteo de descartados y refutados. Se elige con casillas (multiSelect), hasta 16 hallazgos en 4 preguntas de 4; un grupo de uno suma "Ninguno"; si hay más de 16, nxy te dice cuántos quedan fuera. Qué hacer: marcá los que querés arreglar o elegí "Ninguno". Los refutados y descartados no se ofrecen: están en **Ver completo**, `~/.nxy/reviews/<id>.md`.

**Arreglos.** Qué ves: un único `nxy:implementer` recibe todos los hallazgos elegidos y después nxy corre `nxy affected` de nuevo. Verde: la feature cierra. Rojo: un reintento; si sigue rojo (o queda sin verificar) ves **Arreglos fallidos** con la causa y **Reintentar**, **Seguir** (aceptar así) o **Parar**.

**Listo.** Qué ves: el cierre, que empieza con la palabra (`Listo · R1 ✔ <test> · …`), con `R1 ✔ <test>` por regla y los arreglos aplicados. Se muestra una sola vez. Si hubo un "Aviso:" de lentes inválidas, se repite acá. Si los afectados quedaron en rojo (aceptados con Seguir) o sin verificar, lo dice, y los arreglos figuran como "Arreglos pedidos (sin verificar)" en vez de "Arreglos aplicados". Si la review no corrió, los criterios quedan con `?`. Qué hacer: si ves "sin verificar", corré tus tests a mano.

**Qué se guarda.** Por lente, cuáles hallazgos elegiste y cuáles ignoraste, en la db (`~/.nxy`); se van a ver en `/nxy:trend` en F3.

**Costo** (estimación): N reviewers (uno por lente; típicamente 2–4 en riesgo medio o alto, 1 en bajo), cada uno con un paquete que incluye el diff cortado a 3000 líneas como máximo (un archivo nuevo sin trackear de más de 256 KB entra sólo con la cabecera, sin contenido; el reviewer lo lee por su cuenta); un diff de unas 1.000 líneas pesa ≈ 15k tokens por reviewer. El refutador se suma sólo en riesgo alto. La review completa son unos KB y se borra al cerrar con éxito o con `nxy feature stop`.

## F2.5: modo ingeniero

Para cambios donde querés decidir vos el diseño antes de que nadie planifique.

**Cómo entrás.** `/nxy:engineer <pedido>`, o la oferta **Pasar a engineer** que aparece entre las preguntas de `/nxy:feature` (la otra opción es **Responder acá**, que sigue como siempre). La oferta ocupa uno de los 4 lugares de la pantalla de preguntas; si el plan tiene 4, la cuarta llega en la vuelta siguiente. Si pasás a engineer, las preguntas del plan se vuelven las primeras bifurcaciones.

**Diseñando.** Qué ves: `nxy status` dice "Diseñando", y el chat muestra la agenda (`3 de 5 decididas`) con cada bifurcación B1, B2… y su estado. Antes, Claude hace un reconocimiento con `nxy:scout` y escribe un diagrama chico del estado actual, lo muestra como bloque de código `d2` y lo guarda con `nxy design today` (la TUI lo dibuja). Qué significa: todavía no hay plan ni se toca código; se debate. Qué hacer: respondé en el chat, proponé tu opción o pedí opciones.

**El debate.** Claude te refuta o te da 2–3 opciones con su trade-off, y su opinión es condicionada ("B, salvo que X"). Si ve un problema que no pediste mirar (N+1, índice, carrera, transacción, contrato, seguridad), lo dice con `path:línea` o lo marca como duda. No hay límite de vueltas ni desempate automático: cambia de opinión sólo con un argumento y lo dice, no cede por insistencia. Si cerrás con tu opción igual, queda registrado que Claude no estaba de acuerdo.

**Opciones de cada bifurcación.** Qué ves: al abrir una bifurcación, Claude guarda (`nxy design fork`) sus 2–4 opciones, cada una con resumen, pros, contras, qué toca y un diagrama D2 opcional, más su opinión; en el chat sigue viéndose el debate de siempre. Qué significa: la TUI de la F5 va a mostrar esas opciones como tarjetas, con la opinión de Claude detrás de una tecla (en el chat no aparece hasta que la pidas). Después de un `/clear` la guía vuelve con las etiquetas de la bifurcación en debate. Qué hacer: nada; si Claude reenvía la agenda, las opciones de las bifurcaciones abiertas se guardan de nuevo.

**Guardar cada decisión.** Qué ves: una pregunta `nxy feature <id>: ¿guardar la decisión D<n>?` con la decisión, el porqué y, si lo hay, "Claude mantiene su desacuerdo: …". **OK, guardar** la deja en la db en el acto; **Seguir debatiendo** (o escribir otra cosa) no guarda nada. Tus mensajes mientras diseñás se guardan tal cual como referencias para el planner.

**Cerrar.** Cuando están todas decididas, Claude propone cerrar: ves las decisiones, los criterios R1…Rn y el diagrama D2 como fuente. **Cerrar y planificar** aprueba los criterios y arranca el planner; **Seguir diseñando** vuelve. Si decís "decidí vos el resto", las bifurcaciones abiertas quedan delegadas al planner y el plan las marca como "las eligió el planner".

**/clear es seguro.** Las decisiones ya están en la db: después de un `/clear` la guía vuelve sola con la agenda, lo decidido y tus últimos 10 mensajes de este diseño (cada uno cortado si es largo). La propuesta de decisión y el resumen del cierre aparecen una sola vez en el chat.

**Planner y huecos.** El planner sólo baja tu diseño a lotes, copia los criterios tal cual y no hace preguntas. Si encuentra un hueco de diseño, frena con `nxy plan gaps` y volvés a Diseñando con una bifurcación nueva. El checkpoint del plan muestra sólo los lotes (los criterios ya los aprobaste) y el plan .md suma una sección `### Diseño`.

**Review.** El base pass explica el diff por decisión (`D1 → path:línea — cómo`), y cada hallazgo trae el porqué y una regla general para aprender de él.

**A validar en vivo:** `/clear` con el planner en background justo tras cerrar; el peso de las referencias en el prompt del planner (≈25 KB con 50 mensajes); que nxy no valida la sintaxis d2 (un bloque roto se ve recién al renderizarlo a mano).

**F5.2a: datos para la TUI.** Qué se guarda ahora: por cada subagente su modelo, tokens, costo y tiempo (tabla `agent_runs`, una fila por subagente, escrita una vez al terminar), y el inicio y el fin de cada lote. Qué significa: es sólo para que la TUI muestre costos y duraciones; vive en tu máquina, el bundle de memoria no la lleva y se borra a los 90 días. Pesa unos 4 KB por feature y unos 0,5 MB como mucho con 10 features por semana (estimación). Qué hacer: nada.

## F3.2: stats, trend y statusline

Los tres reportes de la 1.x ya corren en el binario de Go y imprimen lo mismo que la 1.x con las mismas entradas (texto y `--json` byte a byte, probado con golden generados por la 1.x). Todavía no hay slash commands ni instalación de la statusline: eso es F3.4.

**Cómo probarlos.** Con el binario compilado (`go build -o nxy ./cmd/nxy`):

- `nxy stats` (sesión actual; también `last`, un id, `--all`, `--no-agents`, `--json`).
- `nxy trend` (`--by day|week|session|model|agent`, `--here`, `--since 7d` o una fecha, `--json`).
- `echo '{"session_id":"x","cwd":"<tu repo>","model":{"id":"claude-sonnet-4-6"}}' | nxy statusline`, o `nxy statusline < payload.json` con un JSON de los que manda Claude Code.

Qué ves: las mismas tablas y la misma línea de la 1.x; lo que nxy no puede saber se muestra como `?`, nunca un número inventado. Qué hacer: nada que configurar, leen `metrics.*` de tu `config.json` como la 1.x.

**Lo que no es igual todavía.**

- La línea "broad searches in the main thread" de `nxy stats` (y `broadSearches.count` en `--json`) no sale hasta F3.3: hoy siempre es 0.
- Las sesiones del v2 muestran `FILTER …: 0` y la columna nxy de `trend` cuenta sólo skills `nxy:`: el filtro del v2 todavía no escribe filas (es F4).
- Si falta rtk, `trend --by day` sigue diciendo "install Node ≥ 22.13" (texto de la 1.x).
- La statusline no se instala sola ni la llama Claude Code todavía (F3.4): el shim `bin/nxy.cmd` sólo corre `hook`.

**Estado de la statusline.** Cada render lee sólo los bytes nuevos del transcript, así que guarda el avance por sesión:

- Dónde: `~/.nxy/cache/statusline-v2/<session>.json` (el id con todo lo que no sea `A-Za-z0-9_-` cambiado por `_`) y el último payload recibido en `~/.nxy/cache/statusline-v2-input.json` (para soporte: "¿por qué no veo X?"). `NXY_HOME` cambia `~/.nxy`.
- Cuánto pesa (estimación): ~7 KB por sesión; hasta ~130 KB con 30 subagentes. Con el borrado de abajo quedan ≤ ~70 archivos y ≤ 1 MB.
- Cuándo se borra: al crear el estado de una sesión nueva, nxy borra los archivos de esa carpeta con más de 7 días. Cuesta un listado y ≤ ~70 stats, una vez por sesión nueva. Una sesión que vuelve después de 7 días reconstruye su estado con los mismos números (relee el transcript una vez).
- Los archivos de la 1.x (`~/.nxy/cache/statusline-*.json`) no se leen ni se escriben: la primera vez de cada sesión se relee el transcript entero (estimado 20–150 ms con 5–20 MB).
- El camino caliente no lanza procesos, no abre la db de nxy y no llama al motor del filtro; si algo falla imprime `nxy ?` (con `NXY_DEBUG` ves la causa por stderr).

`NXY_BIN` wins over everything. To test the download path (binary under `~/.nxy/bin/2.0.0-dev/`, fetched from the GitHub release), unset it first: a leftover `NXY_BIN` silently runs a stale build.

## F3.4: comandos utilitarios

Los comandos utilitarios de la 1.x corren en el binario de Go y se llaman desde el plugin dev con slash commands. Todos usan el shim con argumentos: `bin/nxy.cmd <comando> …` corre ese comando del binario; si falla, escribe una línea en stderr y sale con 1. Los `!` de los slash commands necesitan bash/zsh o PowerShell (dash no pasa argumentos a `.`).

**Slash commands.** `/nxy:stats`, `/nxy:trend`, `/nxy:setup`, `/nxy:filter`, `/nxy:statusline`, `/nxy:config`, `/nxy:locate`, `/nxy:worktree` y `/nxy:mem`. Qué ves: la salida del comando correspondiente en el chat. Qué hacer: pasarles los mismos argumentos que al binario (`/nxy:trend --by week`).

### /nxy:statusline [install|remove]

Qué ves: sin argumento, el bloque `statusLine` que se escribiría en `~/.claude/settings.json` y si ya está instalada. `install` escribe `statusLine`, imprime la ruta del shim y el backup, y pide reiniciar Claude Code. `remove` la quita (sólo si es la de nxy).

- Copia estable del shim: `~/.nxy/bin/nxy.cmd` (~13 KB). Sólo existe si instalaste la statusline; SessionStart la pone al día con la versión del binario; `remove` la borra.
- Backup de `settings.json`: `bak-nxy-<ms>` junto al archivo, del tamaño del settings; se conservan los 3 últimos.

### /nxy:setup

Qué ves: el estado de rtk, rg, codegraph y d2 (instalados o no, de dónde, con pistas de instalación). Qué significa: nxy detecta, no instala solo. d2 es opcional: dibuja como texto, en la TUI, los diagramas del diseño. Qué hacer: `setup run <rtk|rg|codegraph|d2> --yes` instala con tu sí (sin `--yes` muestra los pasos y no hace nada; con `--dry-run` los lista sin correrlos). Un `sudo` sin terminal te devuelve el comando para correr a mano. No escribe archivos propios.

### /nxy:filter status|on|off

Qué ves: el motor del filtro (rtk o no), rg, lo excluido y el escape hatch. `on` / `off` escriben `modules.filter` en tu config de usuario (`~/.nxy/config.json`) por defecto y en el `.nxy/config.json` de este repo con `--repo`, conservando las otras claves y su orden (~100 B; es tuyo, no se borra solo), y refrescan el cache del motor. Si ese archivo de config está roto no se toca: lo dice (`<ruta> está roto; no se toca`) y sale con error; arreglalo a mano y repetí el comando.

### /nxy:config

- `show`: cada clave que la v2 lee, con su valor y de dónde viene (default, usuario, repo).
- `set <clave> <valor|default> [--repo]`: escribe `~/.nxy/config.json` por defecto; con `--repo`, `.nxy/config.json`. `default` borra la clave. Menos de 1 KB.
- `cache-ttl 1h|off`: escribe o quita `promptCacheTtl` en `~/.claude/settings.json`, con backup `bak-nxy-<ms>` (se conservan 3).

Roles: `roles.<rol>.model` (haiku, sonnet, opus, fable) y `roles.<rol>.effort` (low, medium, high) para planner, implementer, reviewer, reviewer-quick, refuter, scout y documenter se aplican al despachar cada agente de nxy. Si el despacho ya trae model o effort, gana el del despacho. Sin config no se toca nada y queda el frontmatter del agente. **Riesgo:** si Claude Code ignora `effort` en el despacho, el rol corre con el del frontmatter; a validar en vivo.

### /nxy:locate y el scout

`nxy locate "<consulta>"` busca sin modelo con el repo map, rg y codegraph; `--callers|--callees|--impact <símbolo>` pide relaciones, `--status` muestra el estado, `--index` reconstruye el índice y `--json` da salida de máquina. El scout (`nxy:scout`) corre ese comando, que nxy le inyecta, antes de leer archivos.

- Índice compartido con la 1.x: `.nxy/local/index/repomap.tsv` y `repomap.meta.json`. Pesa en proporción al repo, se reconstruye de forma incremental y se borra con `.nxy/local`.
- `/regex/i` ahora sí es regex (en la 1.x caía a substring).

### Worktrees: /nxy:worktree o `nxy feature list|new|pause|resume|close`

Qué ves: la lista de worktrees y, en cada acción, el comando git que corrió. `new --name <n> --base <rama> [--issue <n>]` (o `new --branch <b>`) crea el worktree con base explícita. nxy nunca commitea ni pushea.

- `pause` / `resume`: con `--feature <id>` (o la feature del `--path`) pausan o retoman la feature y la pausa libera sus checkouts; el worktree además lleva una marca en `.nxy/local/features/paused.json` (~100 B), que se borra al `resume` o al `close`. Si al retomar otra feature tomó el checkout, nxy lo dice.
- `close --path <wt>`: con cambios sin guardar pide `--force`; no cierra el worktree principal, el de esta sesión ni uno con una feature sin terminar. `close --feature <id>` cierra todos los worktrees de una feature terminada o pausada (ver "F4.5a: varios repos").
- Al pausar, Claude te pregunta (opcional) «¿Algo para la próxima sesión?»; si contestás, queda como nota temporal de ese worktree (ver "F4.3: notas y para retomar"). Al cerrar el worktree se borran las notas de su carpeta.

### /nxy:mem

Qué ves: la memoria del repo, guardada en la db global (`~/.nxy/nxy.db`). Qué significa: una regla (`K3`) es una decisión del repo y siempre dice la pregunta que resuelve; un aprendizaje (`A7`) es algo que notaste y todavía no es regla. Qué hacer:

- `show`: las reglas con `delivered N` (cuántas veces se entregó a un agente) y `cited M`; `to review` marca las que llevan 90 días sin citarse. Debajo, los aprendizajes con `seen N times`: los más vistos son candidatos a promover.
- `rule "<texto>" --question "<la pregunta que resuelve>" [--paths g1,g2]`: guarda una regla. Sin `--question` falla.
- `note "<texto>" --files a,b`: guarda un aprendizaje; repetirlo suma `seen`, no crea otro.
- `promote <A7> --question "<q>"`: lo convierte en regla y sale de la lista de aprendizajes.
- `rule "<texto>" --question "<q>" --global [--roles planner,reviewer]`: guarda una **regla global** (`G2`), tuya y de todos los repos. Sin `--roles` la reciben todos los roles; los roles son `planner`, `implementer`, `reviewer` y `main` (el hilo principal en modo ingeniero). `--roles` sin `--global` falla. `show` las lista debajo de las del repo, con `roles:`, `delivered` y `cited`.
- `edit <K3|G2|A7> [--text …] [--question …] [--paths …] [--roles …] [--files …]`: cambia lo que pasás y deja todo lo demás: el mismo número, las entregas y las citas. `--paths ""` o `--roles ""` los vacían. Una regla toma `--text`, `--question`, `--paths` (y `--roles` si es global); un aprendizaje, `--text` y `--files`. Sin ningún flag falla con «nothing to change».
- `rm <ref>` borra una regla o un aprendizaje (desaparece de `show`, de la búsqueda y de lo que reciben los agentes); `restore <ref>` lo trae de vuelta con el mismo número, que nunca se reutiliza. El borrado, la edición y la restauración viajan en el export/import como cualquier otro cambio: el más nuevo gana.
- `search <texto> [--all]` busca en la memoria del repo (con `--all`, en todos); `related <archivos…>` lista features cerradas y aprendizajes que tocaron esos archivos.
- `export [--out archivo] [--notes]` escribe un JSON versionado; `import <archivo>` sin `--apply` sólo muestra qué cambiaría (nuevo, cambiado, borrado, repetido) y no escribe; con `--apply` lo aplica en una sola transacción. El más nuevo gana y los borrados viajan. Las notas temporales no viajan por defecto: duran 30 días y su carpeta es de esta máquina; `--notes` las incluye.

**Reglas globales (`G2`).** Qué ves: en `nxy mem show`, una sección «Global rules» y otra «Global learnings». Qué significa: una `G` vale en todos tus repos; las `K` son de un repo. Llegan al planner, al implementer, al reviewer y al hilo principal del modo ingeniero (según `--roles`), después de las `K` y dentro del mismo tope de 3.000 caracteres; si algo no entra, se cortan primero las `G` (nunca una `K` por una `G`). Si hay alguna, el agente lee la línea «If a G rule contradicts a K rule, the K rule wins.». Nunca llegan al scout, al delegate, al refuter ni al documenter. Un reviewer puede citar una `G` (`"rule": "G2"`) como una `K`. Qué hacer: `nxy mem rule "<texto>" --question "<q>" --global`. Un aprendizaje global (de la 1.x) se vuelve `G` con `nxy mem promote A<n> --question "<q>"`.

Costo: una regla ~0,5 KB, un aprendizaje ~0,3 KB; la columna de roles suma ~10 bytes por regla; 100 reglas y 500 aprendizajes caben en ~1 MB de la db. El export se escribe sólo a pedido y no se borra solo.

**Notas temporales.** `nxy note save "<título>"` (cuerpo por stdin, hasta 2 KB), `nxy note list [--all]` y `nxy note done <id>`. Se ven desde la carpeta donde las guardaste y vencen a los 30 días. La pregunta al arrancar la sesión está en "F4.3: notas y para retomar".

### /nxy:mem bootstrap

Qué ves: en un repo sin reglas guardadas, la primera `/nxy:feature` (o `/nxy:engineer`) pregunta «Este repo no tiene reglas guardadas. ¿Las propongo ahora?» con **Sí** (recomendada) y **No**. Qué significa: **Sí** manda al scout a leer el build, los linters, el CI, el README y una muestra del código, y a proponer hasta 10 reglas que ninguna herramienta ya hace cumplir (lo que cubre un linter queda como «correr X antes de cerrar»); cada una dice la pregunta que resuelve y dónde la vio (`archivo:línea`). Después elegís cuáles guardar en preguntas de a 4 (`B1`, `B2`…); las elegidas quedan como reglas con origen `bootstrap`, y recién entonces arranca tu feature, así el planner ya las ve. **No** arranca la feature en el acto. Qué hacer: contestá; si querés hacerlo más tarde o de nuevo, corré `/nxy:mem bootstrap`.

Costo: ~30–60k tokens y 1–2 minutos, una sola vez por repo; no se vuelve a ofrecer (ni con Sí ni con No). El scout corre en primer plano: no lo mandes al fondo.

### Docs y documenter

Qué ves: al cerrar con éxito, si hay docs de usuario que nombran el cambio, una pregunta con **Actualizar docs** o **Dejar**. Qué significa: **Actualizar docs** despacha `nxy:documenter`, que sólo puede editar esos docs (y una página nueva en la carpeta wiki del CI); cualquier otro archivo se le niega. **Dejar** cierra. Sin docs que nombren el cambio no pregunta nada y cierra con "Listo".

Costo de la búsqueda: con rg, milisegundos; sin rg, un recorrido con tope de 5000 archivos / 1 MB. Una vez por feature. No escribe archivos fuera de los docs que apruebes.

## F4.2: la memoria en el flujo

Qué ves: las reglas del repo (`K3`) llegan solas a cada agente, sin que hagas nada. Los **criterios** del plan siguen llamándose R1…Rn (lo que tiene que cumplir esta feature); las **reglas** K son de todo el repo y viven entre features.

- **Qué recibe cada agente.** El planner recibe todas las reglas (las `K` y después tus `G` globales, ver `/nxy:mem`); el implementer, las del área que toca el lote (las que tienen `--paths` que coinciden, más las que no tienen) y los aprendizajes de esos archivos; el reviewer, las reglas del diff. Siempre con el texto completo y con un tope de 3.000 caracteres (~750 tokens): lo que no entra queda en una línea "not shown (budget)" y lo ves con `nxy mem show`. En modo ingeniero, el hilo principal recibe las reglas al empezar el diseño. El planner y el modo ingeniero también corren `nxy mem related <archivos>` para ver features cerradas y aprendizajes de esos archivos.
- **`delivered / cited / to review`** en `nxy mem show`: cuántas veces se entregó la regla a un agente, cuántas veces un reviewer la citó al encontrar que el cambio la rompía (un hallazgo que cita una regla sube a medium como mínimo) y si lleva 90 días sin citarse. Contar no cambia la fecha de la regla ni viaja distinto en el export.
- **Aprendizajes.** El implementer puede cerrar su respuesta con líneas `learned: <texto> | files: a, b` (hasta 5) y `false: A7` si comprobó que un aprendizaje ya no es cierto. El reviewer lo manda en el JSON de `review save` (`learnings` y `false`). Un aprendizaje repetido suma `seen`; uno falso, o cuyos archivos ya no existen, queda obsoleto (se marca, no se borra).
- **Pregunta del cierre.** Al terminar con éxito, si hay candidatas (las decisiones `repo` del plan o del diseño, lo que levantaste con `nxy feature rule` y los escapes), te pregunta qué guardar como regla del repo, de a 4 opciones por pregunta. Si una se parece a una regla existente, dice "reemplaza a K3": al elegirla la anterior queda reemplazada. Elegir "Ninguna" o parar la feature no guarda nada. La feature cerrada queda en la memoria con su título, decisiones, archivos y hallazgos.
- **Aviso por palabra.** Si con una feature activa escribís "siempre", "nunca" o "de ahora en adelante" (o always / never / from now on), nxy le pide a Claude que te pregunte si es una regla del repo; nunca la guarda sin que lo confirmes. Para marcarla a mano: `nxy feature rule "<texto>" --question "<qué resuelve>"` (queda como candidata para el cierre; sin feature activa usá `nxy mem rule`).

### /nxy:review

Qué ves: lo que la review dejó. Qué hacer:

- `show`: la review de la feature activa (o de la última); sin review dice "no review yet".
- `status`: por lente, en los últimos 30 días, los hallazgos que elegiste arreglar, los que ignoraste y los escapes. Qué significa: una lente con muchos ignorados y ningún escape probablemente sobra; una con escapes le falta algo.
- `escape "<texto>" --question "<qué resuelve>" [--lens base]`: avisás que la review dejó pasar un problema. Con una feature activa queda como candidata para la pregunta del cierre; sin feature activa (apareció en el PR) se guarda ya como regla con origen review y dice el K nuevo, o "similar to K3" si ya hay una parecida (no bloquea). Sin `--question` falla.

Costo: un escape es una fila de ~200 B; las candidatas viajan en el JSON de la feature (hasta ~6 KB). Nada se borra solo.

## F4.3: notas y para retomar

Qué ves: las notas temporales (`N3`) son lo que dejaste a medias en una carpeta; sirven para retomar en otra sesión sin releer el chat.

- **Al arrancar sin feature activa.** Si la carpeta tiene notas, tu primer mensaje dispara la pregunta «¿Con cuál seguimos?» (hasta 3 notas, con «hace 2 d», y «Ninguna, es otra cosa»). La elegida llega al contexto con su texto; «Ninguna» no inyecta nada. Si hacés `/clear` justo después de trabajar una nota, esa sale primera con «(recomendada)». Si hay más de 3, la descripción dice cuántas y `nxy note list` las muestra.
- **Con una feature activa** no se pregunta nada: al arrancar (inicio, `resume`, `/clear`, compact) Claude recibe el «para retomar» armado desde la db: el pedido, el paso, «lote N de M», lo último que falló, las decisiones y las notas de esa feature; si hay otras notas en la carpeta, una línea «Hay N notas más». `nxy status` muestra lo mismo (lote, última falla y decisiones).
- **Cómo se borran.** `nxy note done <id>` la borra; vencen a los 30 días sin escribirlas (elegir una no renueva el plazo); las de una feature se borran al terminarla o pararla; las de un worktree, al cerrarlo con `nxy feature close`.
- **Pausar un worktree.** `nxy feature pause --path <wt>` termina con una línea para Claude: te pregunta «¿Algo para la próxima sesión?» y, si contestás, la guarda con `nxy note save "<título>" --cwd <wt> [--feature <id>]` (texto por stdin, hasta 2 KB). Si no querés nada, decí que no.

Costo: leer las notas al arrancar es una consulta a la db (milisegundos); una nota pesa hasta 2 KB.

## F4.3: write gate y read gate

Qué te frena: sin feature activa, el hilo principal no edita archivos de código (los que el mapa del repo indexa más las extensiones de tus tests afectados, como `.kt`); docs y texto pasan, y el implementer nunca se frena. Lo mismo vale para una escritura por shell (`echo > x.go`). Con una feature activa el freno total no cambia. Además, con el contexto del hilo principal arriba de `gate.contextTokens` (100k por defecto), un Grep/Glob/`rg`/`find` amplio se frena y te manda al scout; archivos sueltos y `Read` con offset/limit nunca.

Qué hacer cuando frena: `/nxy:feature` para algo con forma, o el implementer para un cambio puntual. Si de verdad conviene hacerlo en el hilo principal: `/nxy:gate once` deja pasar una sola edición de código (`/nxy:gate once search`, una sola búsqueda); vence a los 5 minutos. `/nxy:gate reads off` apaga el freno de búsquedas (`reads on` lo vuelve a prender); se escribe en tu config de usuario (`~/.nxy/config.json`), o sólo para este repo con `--repo` (`.nxy/config.json`).

`/nxy:gate status` (o `nxy gate status`) muestra:
- `write gate` y `read gate`: si están prendidos y desde qué umbral frena.
- `one-shot edit` y `search escape`: si hay un `once` armado y cuántos minutos le quedan.
- `decisions`: ediciones frenadas de cuántas pasaron por el gate; `context at edit`: el contexto (p10, p50, p90, máximo) en que editaste.
- `searches`: búsquedas frenadas y dejadas pasar con `once search`.
- `broad searches`: búsquedas amplias de los últimos 30 días (`--since 7d`) que corrieron sobre el umbral.
`nxy stats` suma esas tres líneas del gate sólo si el repo ya tiene filas en el ledger.

Costo: cada edición del hilo principal escribe una fila de ~150 B en `.nxy/local/metrics/gate.jsonl` (tope 256 KB, 90 días).

## F4.5: varios repos

Qué ves: una feature puede tocar varios repos (por ejemplo `api` y `web`), abriendo la sesión dentro de uno o en una carpeta padre que no es repo. Los lotes de repos distintos corren en paralelo cuando sus `depends` se cumplen (ver "Lotes en paralelo").

- **Repos en el checkpoint.** El plan lleva una línea de repos (nombre, rol, ruta) y cada lote y decisión lleva `[repo]`. Los archivos se escriben `<nombre>/<ruta>`; el nombre es la carpeta del repo (si dos se llaman igual, nxy suma padres y te avisa). Un repo declarado sin git: el plan no se guarda. Un lote toca un solo repo.
- **Repo ocupado.** Hay una sola feature activa por checkout. Si el plan toca uno que ya tiene otra feature, el plan no llega a la aprobación y Claude te pregunta: «Pausar la feature X» (recomendada), «Sacar <repo> del plan» (con 2+ repos, salvo que todos estén ocupados), «Abrir en un worktree» (con un repo) o «Parar esta feature». Elegir pausar o abrir en worktree sigue con el mismo plan, sin llamar de nuevo al planner.
- **Rama y bases.** Al aprobar, Claude pregunta la rama (sugerencias según el prefijo más usado del repo, o escribila en Other) y la base de cada repo (la por defecto va recomendada). Una base que existe sólo en origin también se ofrece, y la rama se crea desde `origin/<base>`. Si git falla (por ejemplo cambios sin commitear que chocan con la rama), vuelve a preguntar con el mensaje de git; nxy nunca hace stash ni force.
- **Con un repo.** La rama se crea en la carpeta de la sesión (`git switch -c <rama> <base>`, o `git switch <rama>` si existe y está libre) y seguís trabajando ahí. Con 2+ repos, o con el checkout ocupado, nxy crea un worktree por repo en `<repo>.worktrees/<slug>`.
- **Worktrees.** nxy dice sus rutas y cómo abrirlos (otra ventana de tu editor o `cd`). Un worktree nuevo no trae dependencias (`node_modules`, build): nxy avisa cuáles faltan, no instala nada, y el primer Accept puede fallar hasta que las instales. La feature retiene sólo sus worktrees; tu sesión la sigue igual. Editar el checkout original de un repo que ya tiene worktree se rechaza («editá en <worktree>»).
- **Pausa que libera.** Pausar una feature suelta sus checkouts para que otra los use; `nxy feature resume --feature <id>` los retoma si siguen libres.
- **Cerrar.** `nxy feature close --feature <id>` cierra cada worktree de una feature terminada o pausada (`--force` sólo si lo pedís con cambios sin guardar), borra sus notas e imprime los comandos. Una feature que trabajó en la carpeta no tiene worktrees y lo dice. `nxy feature list` muestra las activas y pausadas con repos, rama y worktrees; también anda desde una carpeta padre que no es repo y lista sólo las features de esa carpeta o de sus repos.
- **Sesión reabierta.** Si reabrís una sesión en una carpeta cuya feature no tiene otra sesión viva, `/nxy:feature` responde «ya hay una feature en curso» en vez de arrancar una segunda.
- **Aprendizajes.** Con 2+ repos, un `false:` del implementer o del reviewer apunta al repo: `false: [api] A3` o `false: api:A3`. Las reglas se entregan por repo y una decisión `[repo]` se guarda en ese repo.

### Lotes en paralelo

- **Qué ves.** Cuando varios lotes están listos a la vez, Claude lanza varios implementers en un solo mensaje, uno por lote y cada uno en su repo («feature X batch 2», «feature X batch 3»). Mientras corren, nxy te avisa que hay que esperar; cada resultado llega cuando termina su implementer, en cualquier orden.
- **Qué decide cuándo.** Un lote espera a los lotes de su `depends`; sin `depends`, espera al lote anterior (así un plan viejo sigue en fila). Dentro de un mismo repo los lotes siempre van de a uno. Para tener paralelo, el plan pone primero el lote de contrato y luego cada repo con `depends: [1]`.
- **Qué hacer.** Nada: el implementer prueba su lote con `nxy batch red|green --feature <id> --batch <n>`. Si lo corrés a mano con 2+ lotes en curso, el `--batch` es obligatorio y nxy te lo pide.

### Si falla uno

Un lote que sigue rojo después del reintento queda como fallido. Los lotes de los otros repos siguen, y los que dependen del fallido esperan. Recién cuando no queda nada corriendo ni por arrancar, Claude te muestra «Lote N fallido» con Reintentar, Seguir o Parar. Reintentar vuelve a correr ese lote; Seguir lo acepta como está y libera a los que dependían de él; Parar termina la feature. Con 2+ lotes fallidos se pregunta de a uno.

### Lente «contrato entre repos»

Con código en 2+ repos, la review suma la lente `contract`: busca que lo que un repo expone (rutas, campos y tipos, códigos, eventos, schemas, claves de config) coincida con lo que el otro consume, y cita ambos lados (`api/...:línea` y `web/...:línea`). Un lado cambiado sin el otro es un hallazgo. La lente sube el riesgo de la review a media: con dos repos la review deja de ser rápida y corre la lente base más `contract` (más las que correspondan). Para apagarla, poné `enabled: false` en la lente `contract` de tu capa de usuario o de la carpeta de la sesión (`<carpeta>/.nxy/...`); si abrís la sesión en una carpeta padre, la capa de cada repo no se lee para esta lente, sólo la de usuario y la de esa carpeta.

Costo: N implementers a la vez son N veces el contexto de un implementer (dos lotes en paralelo ≈ el doble de tokens por minuto, el mismo total que en fila pero más rápido); la lente suma un reviewer más (una llamada con el diff de los dos repos). No se guarda nada nuevo en disco salvo unos pocos campos de la feature (pasos, pruebas por lote, lanes), unos cientos de bytes.

Costo de los worktrees: unas pocas filas en la db (`feature_holds`); cada worktree ocupa un checkout más en disco, y codegraph arma un índice por worktree (segundos).

## Datos de la 1.x

Qué ves: la primera vez que abrís un repo con la v2, un aviso único: `nxy: datos de la 1.x importados — 12 convenciones → aprendizajes A1..A12 · 3 handoffs vivos → notas · 41 archivados → features cerradas · 30 sesiones del filtro · 26 reviews, 88 elegidos`. Es una copia: la 1.x queda intacta y volver a ella funciona.

Qué significa cada conteo:
- **Convenciones (y decisiones, bugs, preferencias) → aprendizajes `A<n>`**, con los archivos a los que estaban atadas. No son reglas todavía.
- **Handoffs vivos → notas.** Entra el objetivo y el `## Next` primero, después lo demás hasta 2 KB; sin el plan. Si se cortó dice «(cortado al importar; completo en la 1.x)». Los que llevan más de 30 días sin escribirse no entran (se cuentan como vencidos).
- **Archivados → features cerradas**, con sus archivos, para que el planner las encuentre por archivo.
- **Sesiones del filtro:** el resumen por sesión que leen `nxy stats` y `nxy trend`. **Reviews y elegidos:** base de «elegidos vs ignorados por lente».
- **Memorias globales → aprendizajes globales.** Entran una sola vez por máquina (no por repo) y el aviso dice `3 globales → aprendizajes globales A13..A15`. No se entregan a ningún agente: `nxy mem promote A<n> --question "<q>"` vuelve `G` los que sigan valiendo. En una máquina cuyos repos ya se habían importado, la próxima apertura los trae igual (una vez).
- Lo que no entra se cuenta: reemplazadas por otra, relaciones entre memorias que no son de archivo, «N privadas sin importar» (las memorias `private` nunca se importan) y «N repetidos sin importar» (aprendizajes que ya estaban). El rango `A<n>` del aviso es el real, el de los aprendizajes que entraron.
- Si el repo no tiene datos de la 1.x, no sale ningún aviso.
- Si la importación falla, sale una línea con «probá `nxy legacy import`». La copia de `memory.db` se reintenta como mucho una vez por día; `nxy legacy status` muestra el último error.

Lo que la 1.x siga escribiendo en el filtro entra solo en el próximo arranque de la v2 (leyendo sólo lo nuevo); memorias y reviews se importan una sola vez por repo. Una sesión que ya escribió la v2 no se pisa con filas de la 1.x.

Qué hacer:
- `nxy legacy status`: marcas por repo y, por carpeta, cuántos bytes del filtro se leyeron de cuántos. `nxy legacy import [--dir <d>]` lo corre a mano; lo ya importado dice «ya importado el <fecha>».
- `nxy mem promote A<n> --question "<qué resuelve>"` vuelve regla una convención que sigue valiendo.
- `nxy note list` muestra los handoffs importados como notas; `nxy note done <id>` borra los que ya no sirven.

`nxy stats` y `nxy trend` abren `nxy.db` en sólo lectura: sin db o sin datos del filtro salen sin esas filas, y nunca la crean. La v2 también escribe su propio resumen del filtro (PostToolUse de Bash, sin el comando crudo); si la base está ocupada, el hook descarta la fila después de esperar 200 ms como mucho.

Costo: la primera vez por repo, menos de 200 ms (copia temporal de `memory.db`, ~2,5 MB, que se borra al terminar; en una máquina con repos ya importados, una copia más, una sola vez, para las globales; lectura de ~1 MB de jsonl); después ~1 ms por arranque, o 2 a 5 ms si la 1.x escribió algo. Las filas del filtro son unas 10 a 30 por sesión y las sesiones con más de 90 días se barren en el arranque.

## Prototipo de la TUI (F5.1)

### Qué ves

`nxy tui --demo` abre un prototipo navegable con las 11 pantallas (Inicio, Worktrees, Feature, Agentes, Decisión, Ingeniero, Plan, Archivo, Memoria, Gasto con Tendencia y Config) y datos de ejemplo embebidos en el binario. No lee tu db ni toca tu repo, y anda desde cualquier carpeta.

Arriba hay 4 secciones fijas: Inicio, Memoria, Gasto y Config. Lo demás se abre con `enter` y se vuelve con `esc`; el camino aparece debajo de la barra, por ejemplo `Inicio › F7K2 › Plan › nota-credito.component.ts`. El contenido usa todo el ancho de la terminal: en las pantallas de dos paneles la columna de la izquierda no crece más allá de lo que mide a 120 columnas y el panel de la derecha toma el resto. Colorear el código del visor de archivos suma unos 4,5 MB al binario (medido en Windows).

### Qué significa cada señal

- Etiqueta de estado de cada feature: Te necesita, Trabajando, Verificando o Listo.
- Etapa: Diseño, Plan, Lotes, Review y Cierre, con ✓ en las que ya pasaron.
- Bloque rojo en una feature: el lote que falló, con el test, el error y qué reglas recibió.
- `● N decisiones` arriba a la derecha: preguntas esperando tu respuesta. Aparecen en una ventana encima de la pantalla; si hay varias, `tab` pasa de una a otra y `esc` la deja para después.
- Memoria: sólo se marcan las cosas raras (sin citar, obsoleto, visto N veces). Lo demás se ve al abrir el detalle.
- Impacto de un lote: ✓ test que pasa, ✗ test que falla, ○ sin correr, ↔ contrato con otro repo.

### Qué hacer en cada pantalla

Las teclas disponibles siempre están en la barra de abajo; `?` muestra todas y `q` sale.

- Inicio: `↑↓` mueve, `enter` abre la feature, `d` abre la decisión, `w` los worktrees.
- Feature: `a` agentes, `p` plan, `d` decidir, `enter` sobre un lote va a su agente.
- Plan: `←→` cambia de lote, `↑↓` de archivo, `enter` abre el archivo. Si está por aprobar: `a` aprobar, `c` cambiar, `x` parar.
- Archivo: `n` / `N` saltan entre cambios, `v` alterna diff y archivo entero, `o` muestra el comando del editor.
- Ingeniero: `↑↓` opción, `enter` elige, `h` cómo es hoy, `c` qué elegiría Claude (no se muestra hasta que lo pedís, para no condicionarte).
- Memoria: `[ ]` cambia de repo o global, `/` busca (`tab` alterna este repo y todos), `e` edita, `x` borra (`y` confirma, `u` deshace durante 5 segundos), `+` crea una regla, `y` / `n` aceptan o descartan propuestas.
- Gasto: `tab` pasa a Tendencia, `s` elige otra sesión.
- Config: `↑↓` recorre, `e` / `f` ciclan modelo y effort del rol, `r` lo guarda en el repo, `i` instala, `c` copia el comando.

Las acciones no hacen nada: sólo muestran abajo el comando que correrían. Las que dicen "todavía no existe" (decidir desde la TUI) llegan con otros planes. Editar, borrar y restaurar memoria y las reglas globales ya existen en la línea de comandos (`nxy mem edit|rm|restore`, `--global`); la TUI todavía no los llama.

Cosas del demo: en Inicio F7K9 está en diseño y en su Plan aparece por aprobar (son dos momentos de la misma feature). `ctrl+n` simula que llega una decisión nueva, `ctrl+t` alterna tema claro/oscuro y `ctrl+g` los íconos. Una bifurcación del ingeniero que aparece en la ventana de decisión se lee ahí, pero se contesta en Ingeniero (`enter` te lleva).

Si ves cuadrados en lugar de íconos, forzá el set con `--icons unicode` o con la variable `NXY_ICONS=unicode` (`nerd` si tu fuente es una Nerd Font y no la detectó).

### Ver una pantalla sin abrir la TUI

`nxy tui --demo --dump <destino> --width 80 --plain` imprime la pantalla y sale; probá `--width 80`, `120` y `200`. `--feature <clave>` elige la feature, el repo de memoria o el archivo, y `--theme light` el tema. Destinos: `inicio`, `worktrees`, `feature`, `agentes`, `decision`, `ingeniero` (`ingeniero.hoy`, `ingeniero.claude`), `plan`, `archivo` (`archivo.entero`), `memoria` (`memoria.aprendizaje`, `memoria.buscar`, `memoria.editar`), `gasto`, `tendencia` (`tendencia.semana`) y `config`. Con un destino desconocido, el comando lista los válidos.

## `nxy view`: las pantallas con tus datos

Qué ves: `nxy view <pantalla> --json` imprime, como JSON, lo que mostraría esa pantalla de la TUI con los datos reales de tu repo (la db, los transcripts de Claude Code, tu config y tus herramientas). No cambia nada: sólo lee. Las pantallas son `cockpit` (Inicio), `agents`, `decision`, `memory`, `stats`, `trend` y `config`, más `plan`, `file` y `engineer` (abajo).

Opciones: `--feature <id>` (en `agents`, `decision`, `plan`, `file` y `engineer`; sin ella usa la feature activa de la carpeta), `--scope repo|global` (en `memory`), `--session <id>` (en `stats`; sin ella, la actual o la última del repo) y `--cwd <carpeta>`. Sin `--json` muestra el uso. `decision` de una feature que no espera nada imprime `null`.

Qué significa lo que ves:
- **Costo de una feature:** incluye el hilo principal (lo que se habló en la sesión desde que empezó la feature) más todos sus subagentes. Por eso es mayor que la suma de los agentes. Si Claude Code borró el transcript (a los 30 días), el costo de una feature vieja baja a lo que guardó la db de sus agentes.
- **Duración vacía (`elapsed_ms` en -1):** el lote no tiene hora de inicio, porque es de una feature guardada antes de que nxy anotara las horas. No se inventa una: la TUI no escribe duración. Un lote que todavía no arrancó tampoco la tiene.
- **Memoria, lo raro:** una regla «sin citar» es la que se entregó 10 veces o más y ningún agente la citó; un aprendizaje «visto» es el que se vio 2 veces o más; «obsoleto» es el que tiene archivos y ya no existe ninguno (se mira con stat al armar la pantalla). Son umbrales fijos, no hay nada que calibrar; el 10 es una estimación. De ahí salen las propuestas: borrar la regla sin citar, pasar a regla el aprendizaje visto o el global que vino de la 1.x. Tampoco se cuenta «en cuántas features» se vio un aprendizaje: son las veces que se vio, y la lista de features muestra sólo la primera.
- **«Pausada»:** en la línea de qué está pasando, la feature está pausada y no te pide nada. Si además te necesita (por ejemplo, un plan por aprobar), se ve «Te necesita: …» y no «Pausada».
- **«Lote N en curso»:** hay un solo lote corriendo y todavía no se sabe qué herramienta usa; cuando se sabe, se ve «Lote N · Edit archivo».
- **Agentes que corren:** se leen del final de su transcript. Un subagente que todavía no tiene fila en la db se ve «corriendo» sólo mientras la fase lo está esperando; después se ve como terminado, con sus números.
- **Decisión:** las respuestas muestran el comando `nxy action decide ...`, que todavía no existe (llega con F5.4); quedan marcadas como planeadas.

Costo: es una sola lectura. `nxy view cockpit` y `nxy view trend` leen enteros los transcripts principales de las features activas y de hoy o de las últimas 8 semanas: estimado de 0,1 a 1 s con muchas sesiones, sin medir. No se guarda nada en disco.

### `plan`, `file` y `engineer`

Se piden con `--feature <id>` (sin ella, la feature activa de la carpeta; si no hay, dice «no hay una feature activa»).

**`nxy view plan --json`.** Qué ves: los lotes con su estado y el intento, los criterios, las decisiones (marcadas las que vinieron del diseño) y el mapa de impacto: cada archivo que toca el plan como `edit` o `new`, quién lo usa y sus tests. Qué significa: `Source` dice de dónde sale el impacto. `codegraph` viene del índice; `name` es una adivinanza por nombre (busca el nombre del módulo con rg y empareja tests por nombre), puede sobrar o faltar; `none` es que no hubo con qué calcularlo y las listas quedan vacías. Si el plan toca varios repos, vale el más débil de todos. La fecha de aprobación se aproxima con el inicio del primer lote. Qué hacer: si ves `name`, instalá codegraph con `/nxy:setup`. Una feature que todavía no tiene plan imprime `null`.

**`nxy view file --path <ruta> --json`.** Qué ves: el diff de toda la feature contra el commit que había al aprobarla (3 líneas de contexto), «lo edita el lote N» o «lo editan los lotes 2 y 3», notas `fail` en las líneas `ruta:línea` que aparecen en la salida de la prueba que falló, y las reglas, aprendizajes y features (activas y cerradas) que tocan ese archivo. Qué significa: no hay un diff por lote, porque nxy no commitea por lote. Los fines de línea CRLF y LF no cuentan como cambio. El contenido del archivo se corta en 1 MB. Qué hacer: pasá la ruta como está en el plan (con el prefijo del repo si el plan toca varios). `--path` es obligatorio.

**`nxy view engineer --json`.** Qué ves: la agenda de bifurcaciones, la actual con sus opciones (pros, contras, qué toca y un diagrama) y el diagrama «hoy», que el hilo principal guarda con `nxy design today`. La opinión de Claude está en los datos y la TUI la esconde hasta que la pedís. Qué significa: los diagramas los dibuja d2 como texto. Sin d2 ves la fuente d2 y la pista de instalarlo desde Config. Qué hacer: instalá d2 desde `/nxy:setup` o Config (opcional). Una feature que no está diseñando imprime `null`.

Costo, estimado y sin medir: `plan` hace por archivo del plan una consulta a codegraph (0,1 a 1 s), una búsqueda con rg y un `git cat-file`; el resultado queda en memoria del proceso y no se escribe nada en disco. `engineer` dibuja de 2 a 5 diagramas con d2, de 100 a 300 ms cada uno.
