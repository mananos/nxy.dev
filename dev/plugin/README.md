# nxy 2.0 dev plugin

Dev only. Same hook events as the 1.x plugin, but each one runs the Go binary through the shim `bin/nxy.cmd`. The binary runs the F2.1–F2.5 flow (`/nxy:feature` or `/nxy:engineer` → planner → `nxy plan save` → approval → batches → affected tests → review) and the Bash filter from F1; other events answer nothing.

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
2. El hilo principal copia el checkpoint tal cual (reglas con su tipo de prueba y lotes con qué cubren, qué cambian y riesgo) y te pregunta: **Approve**, **Change** o **Ver completo**. Ver completo abre el plan entero en tu editor. Change te pide una nota y el planner replanifica con ella.
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

Al aprobar el plan, nxy ejecuta los lotes de a uno, solo: el hilo principal despacha un `nxy:implementer` por lote y nxy le escribe el prompt (el pedido, las reglas que cubre el lote, qué cambiar y cómo probarlo). No tenés que hacer nada mientras tanto.

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

Si ves una línea del shim con "nxy hooks do nothing until this is fixed" es que el binario no está o no se puede ejecutar: los hooks no hacen nada hasta que lo arregles (bajalo de nuevo o apuntá `NXY_BIN` a un build válido).

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

- Bajo: un solo `nxy:reviewer-quick` con la lente base y las reglas del pedido, una por una.
- Medio: un `nxy:reviewer` por lente seleccionada, todos en paralelo.
- Alto: lo mismo, y además un `nxy:refuter` que intenta refutar los hallazgos de severidad alta leyendo el código.

Mientras corren no tenés nada que hacer; el checkpoint llega solo cuando vuelven todos.

**Lentes.** Vienen embebidas las de la 1.x (base, api, database, frontend, persistence, security). Para cambiarlas: `~/.nxy/lenses/<name>.md` (tuyas) o `<repo>/.nxy/lenses/<name>.md` (del repo, gana). Un archivo con el mismo `name` reemplaza a la lente; con `enabled: false` en el frontmatter la quita. Un archivo inválido no frena nada: ves "Aviso:" con el motivo y se usa la lente anterior.

**Filtro de precisión.** Qué significa: nxy descarta sin preguntarle a ningún modelo los hallazgos sin `path:línea` o sin un escenario `entrada → resultado`. Qué hacer: nada; los descartados figuran en "Ver completo".

**Checkpoint 2.** Qué ves: arriba la línea de reglas (`R1 ✔ · R2 ✘`), después los hallazgos con su escenario y su arreglo (H1, H2, …), los preexistentes sólo como información, y un conteo de descartados y refutados. Se elige con casillas (multiSelect), hasta 16 hallazgos en 4 preguntas de 4; un grupo de uno suma "Ninguno"; si hay más de 16, nxy te dice cuántos quedan fuera. Qué hacer: marcá los que querés arreglar o elegí "Ninguno". Los refutados y descartados no se ofrecen: están en **Ver completo**, `~/.nxy/reviews/<id>.md`.

**Arreglos.** Qué ves: un único `nxy:implementer` recibe todos los hallazgos elegidos y después nxy corre `nxy affected` de nuevo. Verde: la feature cierra. Rojo: un reintento; si sigue rojo (o queda sin verificar) ves **Arreglos fallidos** con la causa y **Reintentar**, **Seguir** (aceptar así) o **Parar**.

**Listo.** Qué ves: el cierre con `R1 ✔ <test>` por regla y los arreglos aplicados. Se muestra una sola vez. Si hubo un "Aviso:" de lentes inválidas, se repite acá. Si los afectados quedaron en rojo (aceptados con Seguir) o sin verificar, lo dice, y los arreglos figuran como "Arreglos pedidos (sin verificar)" en vez de "Arreglos aplicados". Si la review no corrió, las reglas quedan con `?`. Qué hacer: si ves "sin verificar", corré tus tests a mano.

**Qué se guarda.** Por lente, cuáles hallazgos elegiste y cuáles ignoraste, en la db (`~/.nxy`); se van a ver en `/nxy:trend` en F3.

**Costo** (estimación): N reviewers (uno por lente; típicamente 2–4 en riesgo medio o alto, 1 en bajo), cada uno con un paquete que incluye el diff cortado a 3000 líneas como máximo (un archivo nuevo sin trackear de más de 256 KB entra sólo con la cabecera, sin contenido; el reviewer lo lee por su cuenta); un diff de unas 1.000 líneas pesa ≈ 15k tokens por reviewer. El refutador se suma sólo en riesgo alto. La review completa son unos KB y se borra al cerrar con éxito o con `nxy feature stop`.

## F2.5: modo ingeniero

Para cambios donde querés decidir vos el diseño antes de que nadie planifique.

**Cómo entrás.** `/nxy:engineer <pedido>`, o la oferta **Pasar a engineer** que aparece entre las preguntas de `/nxy:feature` (la otra opción es **Responder acá**, que sigue como siempre). La oferta ocupa uno de los 4 lugares de la pantalla de preguntas; si el plan tiene 4, la cuarta llega en la vuelta siguiente. Si pasás a engineer, las preguntas del plan se vuelven las primeras bifurcaciones.

**Diseñando.** Qué ves: `nxy status` dice "Diseñando", y el chat muestra la agenda (`3 de 5 decididas`) con cada bifurcación B1, B2… y su estado. Antes, Claude hace un reconocimiento con `nxy:scout` y te muestra un diagrama chico del estado actual como bloque de código `d2` (nxy no lo dibuja; es texto). Qué significa: todavía no hay plan ni se toca código; se debate. Qué hacer: respondé en el chat, proponé tu opción o pedí opciones.

**El debate.** Claude te refuta o te da 2–3 opciones con su trade-off, y su opinión es condicionada ("B, salvo que X"). Si ve un problema que no pediste mirar (N+1, índice, carrera, transacción, contrato, seguridad), lo dice con `path:línea` o lo marca como duda. No hay límite de vueltas ni desempate automático: cambia de opinión sólo con un argumento y lo dice, no cede por insistencia. Si cerrás con tu opción igual, queda registrado que Claude no estaba de acuerdo.

**Guardar cada decisión.** Qué ves: una pregunta `nxy feature <id>: ¿guardar la decisión D<n>?` con la decisión, el porqué y, si lo hay, "Claude mantiene su desacuerdo: …". **OK, guardar** la deja en la db en el acto; **Seguir debatiendo** (o escribir otra cosa) no guarda nada. Tus mensajes mientras diseñás se guardan tal cual como referencias para el planner.

**Cerrar.** Cuando están todas decididas, Claude propone cerrar: ves las decisiones, las reglas R1…Rn y el diagrama D2 como fuente. **Cerrar y planificar** aprueba las reglas y arranca el planner; **Seguir diseñando** vuelve. Si decís "decidí vos el resto", las bifurcaciones abiertas quedan delegadas al planner y el plan las marca como "las eligió el planner".

**/clear es seguro.** Las decisiones ya están en la db: después de un `/clear` la guía vuelve sola con la agenda y lo decidido.

**Planner y huecos.** El planner sólo baja tu diseño a lotes, copia las reglas tal cual y no hace preguntas. Si encuentra un hueco de diseño, frena con `nxy plan gaps` y volvés a Diseñando con una bifurcación nueva. El checkpoint del plan muestra sólo los lotes (las reglas ya las aprobaste) y el plan .md suma una sección `### Diseño`.

**Review.** El base pass explica el diff por decisión (`D1 → path:línea — cómo`), y cada hallazgo trae el porqué y una regla general para aprender de él.

**A validar en vivo:** `/clear` con el planner en background justo tras cerrar; el peso de las referencias en el prompt del planner (≈25 KB con 50 mensajes); que nxy no valida la sintaxis d2 (un bloque roto se ve recién al renderizarlo a mano).

`NXY_BIN` wins over everything. To test the download path (binary under `~/.nxy/bin/2.0.0-dev/`, fetched from the GitHub release), unset it first: a leftover `NXY_BIN` silently runs a stale build.
