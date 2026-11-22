# Cómo funciona nxy por dentro

Para quien quiere tocar el código: cómo viaja un evento de Claude Code hasta el binario, cómo está organizado el código y qué reglas hace cumplir un test, dónde vive el estado, cómo se arma una pantalla de la TUI, y cómo correr la suite y regenerar las capturas y diagramas de la documentación. Cómo usar cada cosa está en la documentación por feature, que arranca en el [README](../README.md).

## Para qué sirve

nxy decide con código, no con un modelo: reglas sobre números, SQLite y el transcript. Los únicos lugares donde un modelo decide algo son los subagentes, y cada uno devuelve un resumen corto. Esta página te ubica para encontrar *dónde* se decide cada cosa y *qué reglas* tenés que respetar al cambiarla.

## Qué ves

Un solo binario Go (`cmd/nxy`) más los archivos que Claude Code busca en la raíz del plugin:

```
.claude-plugin/          # plugin.json y marketplace.json
agents/  commands/  skills/  hooks/hooks.json   # lo que Claude Code carga del plugin
bin/nxy.cmd              # el lanzador (shim) que corre el binario
cmd/nxy/                 # el único lugar que conecta adaptadores con casos de uso: un archivo por comando
internal/                # todo el código, por capas (abajo)
testdata/                # golden files y datos de la demo de la TUI
docs/                    # esta documentación y docs/assets (capturas, diagramas, videos)
```

## Qué significa

### De un hook al binario

Todos los eventos de Claude Code pasan por el mismo camino:

1. `hooks/hooks.json` registra, para cada evento, el comando `. "${CLAUDE_PLUGIN_ROOT}/bin/nxy.cmd"; exit $LASTEXITCODE`.
2. **`bin/nxy.cmd`** es un archivo que sirve a la vez para PowerShell/cmd y para `sh`. La primera vez que corre, baja el binario de la versión desde el release de GitHub (con un tope de 8 s en total: 6 s el binario, 2 s los checksums, por debajo del timeout de 10 s de cada hook), verifica su SHA-256 y lo deja en `~/.nxy/bin/<versión>/`. Sólo un hook descarga a la vez (una carpeta `nxy.exe.lock` al lado del destino); los otros imprimen una línea `nxy:` y no hacen nada. Un lock de más de un minuto y los temporales de más de un día, de corridas muertas, se borran al próximo arranque. Con `NXY_BIN` corre un binario tuyo en vez de bajar uno. **Límite conocido:** el SHA-256 de `checksums.txt` sale del mismo release que el binario: protege de corrupción y truncado, no de un release comprometido.
3. El shim ejecuta **`nxy hook`** (`runHook` en `cmd/nxy/main.go`). `internal/host/claudecode` decodifica el JSON del evento a un `app.HookEvent`; `internal/app` (`Hooks.Handle`) decide y devuelve una `HookResponse` (`Deny`, `AllowReason`, `Rewrite`, `Notice`, `Context`, `Ask`, `Next`); y el adaptador la codifica de vuelta al formato de Claude Code.
4. **Falla abierto**: ante cualquier error (un panic, un JSON que no entiende, la base ocupada) `nxy hook` sale con 0 y sin nada en stdout, así que Claude hace lo que iba a hacer. La causa sale por stderr (la ves con `claude --debug`).

Fuera de una feature y sin nada que filtrar no ves nada: ni salida de hook, ni herramienta bloqueada, ni texto extra.

| Evento | Qué hace |
| --- | --- |
| `SessionStart` | Importa los datos de la 1.x una vez por repo; resuelve el motor del filtro; re-inyecta el paso de la feature activa (sobrevive a `/clear`); ofrece las notas de la carpeta; avisa si quedó una feature de la 1.x a medias o si hay una 1.x activa; en auto mode, le pide a Claude que anteponga `rtk` |
| `UserPromptSubmit` | Arranca la feature de `/nxy:feature` y `/nxy:engineer` (guarda tu pedido tal cual); guarda tus mensajes como referencias del diseño; entrega un paso pendiente; el aviso de «siempre / nunca» para proponer una regla; la pregunta de las notas |
| `PreToolUse` Bash/PowerShell | Reescribe el comando por rtk ([rtk](rtk.md)); frena las escrituras por consola y las búsquedas amplias del hilo principal ([gate](gate.md)); rechaza `nxy action decide` |
| `PreToolUse` Grep/Glob | El freno de lectura |
| `PreToolUse` Edit/Write/NotebookEdit/MultiEdit | El freno de escritura; cuenta las escrituras del implementer y verifica que su lote nombre el archivo |
| `PreToolUse` Agent/Task | Escribe el prompt del implementer, del reviewer, etc. a partir del plan guardado; aplica el modelo y el effort por rol; inyecta las reglas |
| `PreToolUse` Skill | En el hilo principal rechaza `nxy:feature` y `nxy:engineer`: sólo arrancan tipeados por vos |
| `PreToolUse` AskUserQuestion | Retiene la pregunta si hay una TUI viva (hasta 25 min); timeout de 1800 s ([decidir](decidir.md)) |
| `PostToolUse` Bash/PowerShell | Suma la llamada al resumen del filtro |
| `PostToolUse` Agent/Task | Decide el veredicto del lote cuando el subagente vuelve, y el paso siguiente |
| `PostToolUse` AskUserQuestion | Registra la respuesta (la aprobación del plan, las decisiones) |
| `SubagentStop` | El veredicto de un subagente en segundo plano, que se entrega una sola vez |
| `Stop` | Entrega un paso pendiente (un checkpoint) al terminar el turno; si trabajaste en una carpeta sin feature, ofrece dejar una nota |
| `SessionEnd` | Registra en la base cómo terminó la sesión |

Cuando un paso pendiente llega justo al terminar el turno, Claude Code lo rotula «Stop hook error»: el rótulo es de Claude Code, no es un error; nxy usa ese camino para que Claude siga.

### Las capas

```
cmd/nxy/                 # cablea adaptadores con casos de uso
internal/
  domain/ flow/ plan/    # el modelo (feature, plan, lote) y la máquina de estados del flujo
  review/ gate/ filter/  # review (lentes, riesgo, hallazgos), frenos, decisión del filtro
  affected/ design/ docs/ memory/ legacy/ ...  # tests afectados, modo ingeniero, docs, memoria, datos de la 1.x
  metrics/ pricing/ report/ promptcache/ format/  # costos y reportes de stats, trend y statusline
  scout/ repomap/ searchscope/ setup/ configedit/ worktree/ repos/ ask/  # búsqueda, instalación, config, worktrees
  app/                   # los casos de uso: hooks, stats, trend, memoria, review...
  host/claudecode/       # lo específico de Claude Code: transcripts, settings, formato de los hooks
  store/sqlite/          # la base ~/.nxy/nxy.db y sus migraciones
  platform/              # archivos, git, config, rutas, shell
  tools/                 # rtk, rg, codegraph, d2
  views/ tui/            # las pantallas como datos, y la TUI que las dibuja
  jscompat/              # semántica de JavaScript que el código portado de la 1.x necesita
  runner/ update/        # reservados para correr procesos y actualizar el binario
  archtest/              # el test que hace cumplir las reglas de abajo
```

**Reglas de capas** (`internal/archtest/rules.go`; las verifica `go test ./...`):

- Los paquetes «puros» (el modelo, `flow`, `plan`, `review`, `gate`, `filter`, `memory`, `affected`, `metrics`, `report`...) sólo pueden depender de otros puros y de la biblioteca estándar, y nunca de `os/exec`, `net`, `net/http` ni `database/sql`: no hacen entrada/salida.
- `internal/memory` además no puede depender de ningún host: no sabe qué es Claude Code.
- `internal/app` (los casos de uso) sólo importa paquetes puros y la biblioteca estándar: habla con el mundo a través de interfaces que declara en `ports.go`.
- Los adaptadores (`store/sqlite`, `host/claudecode`, `tools`, `tui`...) y los de apoyo (`platform`, `views`) no se importan entre sí: sólo `cmd/nxy` los conecta.
- `internal/tui` sólo puede importar `views` y paquetes puros que no sean de un host.

Consecuencia práctica: una regla de negocio nueva va en un paquete puro y se prueba sin tocar disco; la parte que toca archivos, git o la base va en un adaptador y se cablea en `cmd/nxy`. Soportar otro agente de código es escribir otro adaptador en `internal/host/`, sin tocar los casos de uso.

### La base y sus migraciones

El estado de nxy (features, plan, lotes, review, memoria, notas, resumen del filtro, costos por agente) vive en un solo archivo SQLite, `~/.nxy/nxy.db` (`NXY_HOME` lo mueve), con el driver `modernc.org/sqlite` (Go puro: no hace falta cgo). Se abre en modo WAL con espera de 5 s por bloqueo; los hooks que no deben demorar (el resumen del filtro en `PostToolUse`) usan una conexión con espera corta y descartan la fila antes que bloquear.

- **Migraciones.** `internal/store/sqlite/migrations/NNNN_nombre.sql`, numeradas, embebidas en el binario. Al abrir, si la base está en una versión vieja, nxy hace un backup (`nxy.db.bak`, con `VACUUM INTO`) y aplica las que faltan en una transacción. Para cambiar el esquema agregás la siguiente migración (nunca editás una ya publicada) y su test.
- **Base más nueva que el binario.** Si la base tiene una versión que este binario no conoce, se niega a tocarla: `la base de datos es de un nxy más nuevo que este binario`. `nxy tui` lo dice como «la db es de un nxy más nuevo: abrí la TUI de esa versión».
- **Lo que no está en la base.** El registro del freno y los permisos `once` (`<repo>/.nxy/local/`), la cache del filtro (`~/.nxy/cache/`), el plan y la review en markdown (`~/.nxy/plans/`, `~/.nxy/reviews/`). La lista completa, en [Configuración](configuracion.md#dónde-guarda-nxy-sus-cosas).

### El contrato de pantallas (views)

`internal/views` es el contrato entre los datos y la TUI: un struct por pantalla (`Cockpit`, `Agents`, `Decision`, `Memory`, `Stats`, `Trend`, `Config`, `Plan`, `File`, `Engineer`), con tags JSON en snake_case, y una interfaz `Source` que devuelve cada uno. Los constructores (`Build*`) son puros: reciben datos de dominio, memoria y reportes más la hora, y no leen base, archivos ni reloj.

- La fuente **real** vive en `cmd/nxy/viewsource.go` (y `viewplan.go`, `viewfile.go`, `viewengineer.go`): lee la base y arma cada pantalla.
- La fuente **de la demo** (`nxy tui --demo`) lee `testdata/views/*.json`, que se decodifican de forma estricta. Esos archivos son la base de los tests de la TUI: no se editan para hacer lucir una captura.
- `nxy view <pantalla> --json` imprime esa misma estructura con tus datos reales (sólo lee); sirve para depurar y para escribir tests. Pantallas: `cockpit`, `agents`, `decision`, `memory`, `stats`, `trend`, `config`, `plan`, `file` (con `--path`) y `engineer`.

### La TUI

`internal/tui` es una aplicación de [Bubble Tea](https://github.com/charmbracelet/bubbletea) (v2) con [Lip Gloss](https://github.com/charmbracelet/lipgloss) para el estilo. Una pantalla por archivo (`screen_*.go`); `app.go` maneja las teclas y las rutas; `load.go` pide los datos a la `Source` y `refresh.go` los renueva cuando cambia la base (mira cada medio segundo); `frame.go` dibuja el marco.

- **Sólo lee de `views`.** Nunca importa la base, el host ni `app`. Las acciones (pausar una feature, instalar una herramienta, contestar una decisión) corren como un subproceso del mismo `nxy`, en segundo plano; las que cambian algo fuera de la TUI piden `y`/`n` y dicen qué archivo tocan. Cada una se corta a los 10 minutos.
- **Contestar desde la TUI.** Una TUI viva escribe una señal `~/.nxy/run/tui-<pid>` cada 2 s; con ella el hook de `AskUserQuestion` retiene la pregunta en vez de mostrar el diálogo. El modelo no puede contestarse solo: `nxy action decide` sólo lo corre la TUI (con un nonce propio) y el hook de Bash y PowerShell rechaza ese comando. Detalle para usuarios: [Decidir desde la TUI](decidir.md).
- **Tests.** `nxy tui --dump <pantalla> --width 80 --plain` imprime una pantalla como texto y sale; los tests de `internal/tui` la comparan contra golden files.

### Costos, métricas y transcripts

Cada respuesta de la API deja en el transcript (`~/.claude/projects/**/*.jsonl`) el `usage` que Anthropic factura. nxy sólo lo lee: `internal/host/claudecode/transcript` lo parsea (deriva del plugin `session-report` de Anthropic), `internal/metrics` lo suma, `internal/pricing` le pone precio con una tabla embebida (`pricing.json`; nxy nunca sale a internet para actualizarla) y `internal/report` imprime `stats`, `trend` y la statusline. Un modelo que no está en la tabla se estima por familia y se marca con `*`; nunca se inventa un precio. Los números tal como los ve el usuario: [Ver en qué se gasta](gasto.md).

## Qué hacer

### Desarrollo

nxy es Go (la versión de `go.mod`: 1.27). El plugin vive en la raíz del repo.

```
go build ./... && go vet ./... && go test ./...
```

Esa es la suite. Para probar el plugin entero sin esperar un release:

```
go build -o <carpeta>/nxy.exe ./cmd/nxy        # sin el .exe en Linux y macOS
NXY_BIN=<carpeta>/nxy.exe claude --plugin-dir .
```

`NXY_BIN` le dice al lanzador que corra ese binario en lugar de bajar el de la versión (en PowerShell: `$env:NXY_BIN = "<carpeta>/nxy.exe"`). Ojo: un `NXY_BIN` olvidado corre un build viejo sin avisar; para probar el camino de la descarga, sacalo. Con la 1.x instalada, desactivala mientras cargás el repo con `--plugin-dir`: las dos responderían a los mismos hooks.

- **Golden files.** Muchos tests comparan contra archivos de `testdata/golden/` (y de los `testdata` de cada paquete). `NXY_WRITE_GOLDEN=1 go test ./...` reescribe los que cambiaron; revisá el diff antes de commitear.
- **Agregar un comando.** Una entrada en el mapa `commands` de `cmd/nxy/main.go`, un archivo `cmd/nxy/<nombre>.go` con su `run<Nombre>` y, si es un comando de Claude Code, `commands/<nombre>.md`. El comando de chat sólo corre `nxy <nombre> $ARGUMENTS` y le pide a Claude que muestre el resultado tal cual.
- **El hook.** `nxy bench hook [-n 20] [-command "git status"] [-shim bin/nxy.cmd] [-bin <nxy>]` corre el hook real y muestra mínimo, mediana, p90 y máximo en ms contra un umbral de 50 ms, con el filtro apagado, prendido sin cache y prendido con cache. Siempre sale con 0.
- **CI** (`.github/workflows/ci.yml`), en Ubuntu y Windows: `go vet ./...`, `go test ./...`, compila el binario en la carpeta versionada, imprime la tabla del bench en el resumen del job, y prueba que cada evento de hook, pasando por el shim (en Windows, también bajo Windows PowerShell), no responde nada ni imprime errores `nxy:`. Un job aparte corre GoReleaser en modo `--snapshot` y verifica que los nombres de los binarios coincidan con lo que descarga el shim.
- **Releases.** Un tag `v2.*` dispara `.github/workflows/release.yml`: verifica que el tag coincida con la versión fijada en `bin/nxy.cmd`, corre `go vet` y `go test` y publica con GoReleaser los binarios `nxy_<versión>_<os>_<arch>[.exe]` (Windows, Linux y macOS, amd64 y arm64) y `checksums.txt`. La versión del binario sale del tag (`internal/version`). El dueño del repo en GitHub es `mananos/nxy.dev`: el módulo de Go, GoReleaser y las URLs del shim tienen que decir lo mismo, y un test (`internal/archtest`) lo verifica.

### Regenerar las capturas, diagramas y videos de la documentación

Los visuales de `docs/` están en `docs/assets/` y se regeneran con un solo comando, que corre todo en Docker (no instala nada en tu máquina):

```
sh docs/assets/render.sh [--dry-run] [todo|capturas|diagramas|videos] [nombres...]
powershell -File docs/assets/render.ps1 [-DryRun] [todo|capturas|diagramas|videos] [nombres...]
```

Sin nombres regenera todo el grupo; con nombres, sólo esos (`sh docs/assets/render.sh capturas plan`). **Conviene regenerar por nombre**: cada PNG regenerado agrega su tamaño completo al historial de git, aunque no haya cambiado nada visible. `--dry-run` imprime cada comando sin correr Docker ni `go run`.

| Qué | Fuente | Salida | Se commitea | Imagen de Docker |
| --- | --- | --- | --- | --- |
| Capturas de la TUI (5) | `docs/assets/capturas/capturas.tsv` (el manifiesto), `nxy tui --demo --dump` y el marco `docs/assets/brand/captura-fondo.html` | `docs/assets/capturas/*.png` (2560 de ancho) | Sí (unos 1,2 MB en total) | `nxy-docs-video:0.9.10` |
| Portada | `docs/assets/brand/banner.html` | `docs/assets/brand/banner.png` (2560×1440) | Sí (unos 1,5 MB) | `nxy-docs-video:0.9.10` |
| Diagramas (4) | `docs/assets/diagrams/*.html` (cajas y flechas con `brand/marca.css`) | `docs/assets/diagrams/*.png` (2560×1440) | Sí (unos 1 MB en total) | `nxy-docs-video:0.9.10` |
| Videos (4) | `docs/assets/video/<nombre>/index.html` ([HyperFrames](https://hyperframes.heygen.com) 0.9.10) | `docs/assets/video/<nombre>.mp4` (unos 2 MB cada uno, 38–50 s) | Sí; además se suben a mano a GitHub y su URL de user-attachments va debajo del link `▶ Video`, para que se vea el reproductor; si se re-renderiza un video, hay que subirlo de nuevo y cambiar la URL. Antes de cada render se copia `docs/assets/brand` a `marca/` dentro de la carpeta del video y se borra al terminar; las fuentes vienen de Google Fonts | `nxy-docs-video:0.9.10`, que se construye con `docs/assets/video/Dockerfile` (node 22 + chromium + ffmpeg + fuentes Noto; unos 2 GB en disco) |

El script **nunca baja ni construye una imagen por su cuenta**: si falta una, imprime el comando exacto (`docker build -t nxy-docs-video:0.9.10 docs/assets/video`) con su tamaño y sale con código 2; si Docker no responde, te pide que lo inicies. Las capturas no necesitan un binario de Linux: los volcados (`nxy tui --demo --dump <pantalla> --width 120`, con color) corren con `go run` en tu máquina, igual en Windows y en Linux, y quedan en `docs/assets/.build/dumps/` (unos 60 KB, ignorados por git). Chromium los pasa a HTML (`ansiAHtml` en `captura.cjs`), los pone en el marco de marca con el título, el subtítulo y el chip de cada fila del manifiesto y comprueba que cada fila mida exactamente 120 celdas de JetBrains Mono: si un glifo cayera a otra fuente, la captura falla en vez de salir desalineada.

Un test de la raíz (`go test -run TestDoc .`) verifica sin Docker que cada enlace e imagen de `README.md` y `docs/*.md` apunte a un archivo que existe, que cada fila de `capturas.tsv` tenga su PNG (y al revés), que todas las capturas midan 2560 de ancho y la portada 2560×1440, que cada diagrama `.html` tenga su `.png` de 2560×1440 (y al revés), cada video su `index.html`, y que cada `/nxy:<comando>` nombrado exista en `commands/`.

## Comandos y config

| Comando | Qué hace |
| --- | --- |
| `go build ./... && go vet ./... && go test ./...` | La suite completa |
| `go test -run TestDoc .` | Sólo los enlaces, imágenes y comandos de la documentación |
| `NXY_WRITE_GOLDEN=1 go test ./...` | Reescribe los golden files que cambiaron |
| `nxy bench hook` | Mide cuánto tarda el hook real |
| `nxy view <pantalla> --json` | Una pantalla de la TUI como JSON, con tus datos |
| `nxy tui --dump <pantalla> --width 80 --plain` | Una pantalla de la TUI como texto |
| `sh docs/assets/render.sh [--dry-run] …` · `render.ps1 [-DryRun] …` | Regenera capturas, diagramas y videos |

Las variables que afectan al desarrollo (`NXY_HOME`, `NXY_BIN`, `NXY_RELEASE_URL`, `NXY_DEBUG`, `NXY_WRITE_GOLDEN`): [Configuración](configuracion.md#variables-de-entorno).

## Relacionados

- [Configuración](configuracion.md): claves, variables de entorno y dónde guarda nxy sus cosas.
- [La TUI](tui.md) · [Decidir desde la TUI](decidir.md): lo que ve el usuario de lo que describe esta página.
- [El freno](gate.md) · [El filtro](rtk.md) · [Memoria](memoria.md): las piezas que deciden dentro de los hooks.
- [Si venís de la 1.x](si-venis-de-la-1x.md): el código portado y los datos que importa.
