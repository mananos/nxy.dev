# El filtro de comandos (rtk)

nxy le pasa los comandos de consola de Claude por [rtk](https://github.com/rtk-ai/rtk), que recorta la salida a lo que sirve: unos cientos de caracteres en vez de miles.

## Para qué sirve

Cada comando que Claude corre devuelve su salida al contexto, y de ahí se reenvía en todas las llamadas que siguen ([Gasto](gasto.md)). Un `./mvnw test` imprime 84 líneas y 9.197 caracteres; filtrado, 8 líneas y 319 (−96 %, medido en un proyecto Spring Boot). El filtro recorta los tests, builds, `git`, logs, `ls`, `grep` y `find`, y deja intacto lo que no sabe recortar.

Todo esto lo decide código de nxy, no un modelo, y **rtk es opcional**: sin él los comandos pasan enteros y nada se rompe.

## Qué ves

Casi nada, a propósito: Claude corre `git status` y recibe la salida ya recortada, a veces con un pie como `[... rtk recall <hash>]`. Para ver el estado del filtro, `/nxy:filter status` (o `nxy filter status`):

```
módulo de filtros: activado (módulo de métricas: activado)
motor:         rtk (rtk-found; config=auto)
rtk:           rtk v0.48.0
ripgrep (rg):  disponible
hook de rtk:   no instalado (bien)
salida de emergencia: poné NXY_RAW=1 antes de un comando (o agregale "# raw") para correrlo sin filtrar
```

Si rtk está en una carpeta de instalación conocida pero no en el PATH de ese proceso, la línea `rtk:` lo aclara y avisa que una terminal nueva ya lo va a tener. Las líneas `excluidos:` y `solo:` aparecen únicamente si configuraste `filter.excludeCommands` o `filter.onlyCommands`. Si rtk no está, `status` dice `no está en el PATH ni en las carpetas de instalación conocidas` y suma una línea `instalar:` con el comando para tu sistema. Si `rg` no está, suma `instalar rg:`.

El ahorro se ve en la pantalla Gasto de la TUI y en `/nxy:stats` (`rtk saved`: los tokens que rtk recortó; sale del registro propio de rtk, porque nxy sólo ve la salida ya filtrada). Ver [Gasto](gasto.md).

## Qué significa

**Cómo decide nxy.** Antes de cada comando de Bash o PowerShell, el hook (`PreToolUse`) mira el comando y decide si se lo pregunta a rtk (`rtk rewrite "<comando>"`). Si rtk tiene un filtro para ese comando, el comando se reemplaza por `rtk <comando>`. Si no, pasa tal cual. Después de correrlo (`PostToolUse`), nxy suma una fila al resumen de filtro de la sesión en `~/.nxy/nxy.db`: motor, tipo de comando, caracteres y tokens devueltos, y el motivo cuando no reescribió. **No guarda el comando crudo.** Si la base está ocupada, el hook descarta la fila antes que demorarte.

**Qué nunca toca nxy** (el comando pasa sin reescribir; cada motivo queda anotado):

| Motivo | Cuándo |
| --- | --- |
| `escape-env` · `escape-comment` | El comando lleva `NXY_RAW=1` (al inicio o en cualquier segmento de una cadena; en PowerShell, `$env:NXY_RAW = "1"`) o un comentario `# raw` |
| `already-wrapped` | Ya empieza con `rtk` (o su ruta) |
| `excluded` · `not-in-only` | Está en `filter.excludeCommands`, o hay `filter.onlyCommands` y no está ahí (se compara el primer token de cada segmento) |
| `heredoc` · `substitution` · `redirect` · `background` | Tiene un heredoc, `$(…)` o backticks, una redirección a archivo (`> out.log`; `2>&1` y `>/dev/null` sí se permiten: descartan, no escriben) o un `&` al final |
| `interactive-or-stateful` | Es interactivo o cambia el estado: `vim`, `ssh`, `sudo`, `docker exec -it`, `node` o `python` sin script, `git commit\|push\|rebase\|merge\|cherry-pick\|stash\|tag\|reset\|checkout\|switch`, `git add -p`, o sólo `cd`/`export`/`source` |
| `opaque` | Una cadena que es sólo `cd` más scripts (`cd x && node -e "…"`): ni se le pregunta a rtk |
| `capped` | Termina en `\| head -N` o `\| tail -N` con N ≤ 20: la salida ya está acotada. Con N mayor, el límite se saca para preguntarle a rtk y se vuelve a poner |
| `nxy-internal` | Toca archivos de `.nxy/` |
| `powershell-script` | PowerShell con `foreach`, pipes, `if (…)`, `function` o variables: son scripts, no un comando |
| `rtk-hook-present` | rtk tiene **su propio** hook instalado (`rtk init -g`): nxy le cede la reescritura para no pisarse |
| `rtk-none` · `rtk-deny` · `rtk-unchanged` · `rtk-error` | rtk no tiene filtro para ese comando, lo negó, devolvió lo mismo o falló (2 segundos de tope) |
| `no-engine` · `engine-off` | No hay motor (rtk no está) o el filtro está apagado |

**Cadenas.** `cd api && export JAVA_HOME=… && ./mvnw test` pasa a `cd api && export JAVA_HOME=… && rtk mvn test`: los prefijos de estado quedan y se reescribe el último comando. Un segmento que rtk no conoce queda intacto, comillas incluidas. Ortografías que rtk no reconoce se normalizan sólo para preguntarle: `./mvnw.cmd` → `mvnw`, y `npm test`/`pnpm build` a su forma con `run`.

**Qué cubre rtk hoy** (v0.48): git, gh, ls/cat/grep/find, mvn/mvnw, gradle/gradlew, los scripts de npm y pnpm, bun, jest/vitest/playwright/pytest/cargo/go test, eslint/tsc/prettier/biome, next/vite, docker y kubectl. No cubre `ng` global, `yarn` ni la instalación de paquetes de npm. Lo que rtk agregue después también entra, sin cambios de nxy.

**Qué no se puede filtrar.** `Read`, `Grep` y `Glob` son herramientas internas de Claude Code: no pasan por Bash y ningún hook puede cambiar lo que devuelven. Para eso están el scout ([Buscar](buscar.md)) y el [freno](gate.md).

**Permisos.** El comando reescrito hereda lo que tu config diga del original:

- Si el original estaba permitido por tus reglas `permissions.allow` (por ejemplo `Bash(git status:*)`) y `filter.autoAllowWhenOriginalAllowed` es `true` (por defecto), el reescrito pasa sin pedirte nada.
- Si no había regla, Claude Code te pregunta por `rtk git status` como te habría preguntado por `git status`.
- Si el original está en `permissions.deny`, no se reescribe: la negativa se mantiene.
- Una cadena hereda el permiso sólo si **cada parte** está permitida (`git status && rm -rf build` te sigue preguntando), y un comando con `$(…)` o backticks nunca lo hereda.

**Auto mode.** Claude Code rechaza un comando cuyo input cambió sin una decisión de permiso, y nxy no aprueba nada por vos. Por eso, con `permission_mode` en auto y rtk activo, nxy **no reescribe**: al abrir la sesión le pide a Claude que empiece él mismo los comandos que rtk filtra con `rtk ` (o la ruta de rtk que fijaste con `NXY_RTK_PATH`). Si se olvida, ves como mucho un intento denegado que nombra el comando exacto:

```
nxy: auto mode — nxy filters this command through rtk but cannot rewrite it for you here; run exactly this instead: rtk git status
```

y Claude lo repite con `rtk`. En PowerShell, si la ruta lleva comillas, el comando sugerido empieza con `& `. Los comandos que ya coinciden con una regla `allow` tuya se reescriben y permiten como en los otros modos. No hay nada que hacer.

**El motor y su cache.** nxy resuelve el motor al abrir la sesión y antes de cada comando: si encuentra `rtk` (en `NXY_RTK_PATH`, el PATH o una carpeta de instalación conocida), qué versión y si tiene su propio hook. La respuesta queda 6 horas en `~/.nxy/cache/engine-v2.json` (2 minutos si no encontró rtk, para que una instalación se note enseguida) y se descarta sola si cambiás `filter.engine`, `NXY_ENGINE` o `NXY_RTK_PATH`. Además, la respuesta de rtk a cada comando exacto se recuerda en `~/.nxy/cache/rewrite-v2.json`: hasta 500 comandos (unos 75 KB), cada uno 7 días; se tira entera si cambia la ruta o la versión de rtk, o si se crea, edita o borra la config propia de rtk. Un comando que repetís (`git status`, `go test ./...`) se reescribe sin arrancar rtk, unos 55 ms menos por llamada en Windows. Los errores y los timeouts nunca se guardan.

**El aviso de rtk.** rtk imprime `[rtk] /!\ No hook installed — run rtk init -g` una vez por día; en Windows aparecía en cada comando, porque NTFS no actualiza la fecha de su archivo marcador. nxy toca ese archivo cuando tiene más de 23 horas, y el aviso deja de ocupar contexto. No corras `rtk init -g`: nxy ya instala su hook y los dos se pisarían.

## Qué hacer

Casi siempre, nada. Los casos:

- **Los comandos no salen filtrados.** `/nxy:filter status` dice por qué: rtk no está o está fuera del PATH, o hay un hook de rtk propio. Si lo instalaste con la terminal abierta, abrí una nueva. Para instalarlo, `/nxy:setup` muestra el comando exacto y lo corre cuando le decís que sí; **no instala nada solo**.
- **Claude necesita la salida completa.** Lo resuelve solo con la skill `nxy-filter`: primero `rtk recall <hash>` (imprime la salida original completa desde el registro local de rtk), y sólo como último recurso repite el comando con `NXY_RAW=1 ` adelante (o `# raw` al final). Vos podés hacer lo mismo a mano.
- **Apagar el filtro.** `/nxy:filter off` (o `nxy config set modules.filter false`); `on` lo vuelve a prender. Escribe en tu `~/.nxy/config.json`, o en el `.nxy/config.json` del repo con `--repo`, conservando las otras claves; si el archivo está roto no lo toca y dice `<ruta> está roto; no se toca`.
- **Excluir un comando.** Poné su nombre en `filter.excludeCommands` (por ejemplo `["kubectl"]`) a mano en la config.
- **Probar sin cache.** `NXY_REWRITE_CACHE=0` saltea la cache de reescrituras (para depurar o medir).
- **Medir cuánto tarda el hook.** `nxy bench hook [-n 20] [-command "git status"]` corre el hook real con el filtro apagado, prendido sin cache y prendido con cache, y muestra mínimo, mediana, p90 y máximo en milisegundos contra un umbral de 50 ms. Siempre sale con 0.

## Comandos y config

| Comando | Qué hace |
| --- | --- |
| `/nxy:filter [status]` · `nxy filter [status]` | Estado del filtro: motor, rtk, rg, hook de rtk, exclusiones |
| `/nxy:filter on\|off [--repo]` | Prende o apaga el módulo (`modules.filter`) |
| `/nxy:setup` | Detecta rtk, rg, codegraph y d2 y los instala si le decís que sí ([Instalación](instalacion.md)) |
| `nxy bench hook` | Mide el hook real |

| Clave o variable | Por defecto | Qué hace |
| --- | --- | --- |
| `modules.filter` | `true` | Prende o apaga el filtro |
| `modules.metrics` | `true` | Con `false`, el filtro no registra una fila por comando |
| `filter.engine` | `auto` | `auto` usa rtk si está; `rtk` también (si falta, el motor queda apagado con el motivo `rtk-missing` en `status`); `off` lo apaga |
| `filter.excludeCommands` | `[]` | Comandos que nunca se filtran (primer token) |
| `filter.onlyCommands` | `[]` | Si no está vacía, sólo se filtran estos |
| `filter.autoAllowWhenOriginalAllowed` | `true` | El comando reescrito hereda el permiso del original |
| `NXY_FILTER=0\|1` · `NXY_ENGINE=rtk\|off` | — | Pisan `modules.filter` y `filter.engine` |
| `NXY_RTK_PATH` · `NXY_REWRITE_CACHE=0` · `NXY_RAW=1` | — | Ruta a rtk, saltear la cache, no filtrar este comando |

Detalle de cada clave y de las variables: [Configuración](configuracion.md).

## Relacionados

- [Ver en qué se gasta](gasto.md): cuánto ahorró el filtro (`rtk saved`).
- [El freno](gate.md): lo que el filtro no puede tocar (`Read`, `Grep`, `Glob`).
- [Buscar](buscar.md): el scout, que lee afuera del contexto principal.
- [Instalación](instalacion.md): instalar rtk y qué hacer si choca con otro hook.
- [Mensajes de nxy](mensajes.md) · [Configuración](configuracion.md)
