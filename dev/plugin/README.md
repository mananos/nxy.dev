# nxy 2.0 dev plugin

Dev only. Same hook events as the 1.x plugin, but each one runs the Go binary through the shim `bin/nxy.cmd`. The binary runs the F2.1 flow (`/nxy:feature` → planner → `nxy plan save` → approval) and the Bash filter from F1; other events answer nothing.

Try it:

```
go build -o <dir>/nxy.exe ./cmd/nxy        # no .exe suffix on Linux/macOS
NXY_BIN=<dir>/nxy.exe claude --plugin-dir dev/plugin
```

Outside a feature you should see nothing: no hook output, no blocked tool, no extra text in the session. A stderr line starting with `nxy:` means the shim could not find or fetch the binary.

It sits beside the installed 1.x plugin (`nxy`, version 1.x): both load, both hooks run on every event. The 1.x plugin does the real work; this one only proves the binary starts. Since F2.1 its name is `nxy` (needed for `/nxy:feature`), so disable the installed 1.x plugin while you load it.

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

Después de aprobar, F2.1 se detiene en **Lote 1**: la ejecución llega en F2.2. Mientras tanto el hilo principal no puede editar archivos del repo (los de fuera, como `~/.claude`, sí). `nxy status` te dice dónde está (por ejemplo "Lote 1"). Para terminar: `nxy feature stop`; libera las ediciones del repo y borra el plan .md.

Dónde queda el plan: `~/.nxy/plans/<id>.md`, uno por feature, de unos 5 a 15 KB. Se pisa en cada Ver completo y se borra al parar.

`NXY_BIN` wins over everything. To test the download path (binary under `~/.nxy/bin/2.0.0-dev/`, fetched from the GitHub release), unset it first: a leftover `NXY_BIN` silently runs a stale build.
