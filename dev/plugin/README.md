# nxy 2.0 dev plugin

Dev only. Same hook events as the 1.x plugin, but each one runs the Go binary through the shim `bin/nxy.cmd`. The binary runs the F2.1 flow (`/nxy:feature` → planner → `nxy plan save` → approval) and the Bash filter from F1; other events answer nothing.

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

Después de aprobar, la feature ejecuta los lotes (ver F2.2 abajo). Mientras hay una feature sin terminar el hilo principal no puede editar archivos del repo (los de fuera, como `~/.claude`, sí). `nxy status` te dice dónde está (por ejemplo "Lote 1"). Para terminar: `nxy feature stop`; libera las ediciones del repo y borra el plan .md.

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
- Después del último lote verde la feature se detiene: sigue la corrida de los tests afectados (ver F2.3 abajo); la review llega en F2.4. Para terminar: `nxy feature stop`.

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

- **Verde**: pasa a la review (F2.4, todavía no existe: nxy te lo dice y la feature se detiene; `nxy feature stop` la termina).
- **Rojo**: nxy despacha un `nxy:implementer` una vez para arreglar el código (cambia un test sólo si el test está mal, y lo dice) y vuelve a correr `nxy affected`. Durante el arreglo ese comando sólo informa, no registra nada. Si sigue rojo ves **Afectados fallidos** con la causa y la cola, y **Reintentar** (otro intento de arreglo), **Seguir** (aceptar así e ir a la review) o **Parar**.
- **Sin verificar**: no hay tests ni `Suite:`, o ningún runner conocido. Ves **Afectados sin verificar** con el motivo y **Reintentar** (corre `nxy affected` otra vez, por ejemplo después de agregar `.nxy/runners.json` o un suite), **Seguir** (sin verificar) o **Parar**. Nunca cuenta como verde.

Costo: un `sync` sin cambios tardó 0,85 s y la consulta 0,44 s en este repo (medido); el resto es el tiempo de tus tests. Tope total del comando: 9 min 30 s.

A validar en vivo: los comandos por defecto de cada stack (`./mvnw` y `./gradlew` necesitan `sh`; `python -m pytest` vs `python3`) y que en Maven multi-módulo el marcador más cercano gana (un `mvnw` en la raíz con `pom.xml` en el módulo usa `mvn` simple).

`NXY_BIN` wins over everything. To test the download path (binary under `~/.nxy/bin/2.0.0-dev/`, fetched from the GitHub release), unset it first: a leftover `NXY_BIN` silently runs a stale build.
