# Instalación

Cómo dejar nxy andando en Claude Code, qué herramientas opcionales suman ahorro y qué hacer si algo no arranca.

## Para qué sirve

nxy es un plugin de Claude Code con un solo binario escrito en Go: no necesitás Node ni nada más. Esta página te lleva del cero a «nxy responde» en cinco pasos, y al final tiene lo que hay que hacer si el binario no baja o no corre.

## Qué ves

Una vez instalado, lo primero visible es la statusline (paso 3) y los comandos `/nxy:…` en Claude Code. La TUI (paso 4) es una consola que abrís aparte. Probala sin datos: `nxy tui --demo`.

## Qué significa

### 1. Requisitos

No hay nada obligatorio salvo **`curl`** (viene con Windows 10 y 11 y con macOS; en Linux, instalalo con tu gestor de paquetes si falta). La primera vez que Claude Code corre un hook, el lanzador de nxy (`bin/nxy.cmd`) baja el binario de la versión desde el release de GitHub, verifica su SHA-256 y lo deja en `~/.nxy/bin/<versión>/`. Tarda unos segundos, una sola vez por versión. Hay binarios para Windows, Linux y macOS, en amd64 y arm64.

Opcionales, que suman ahorro:

| Herramienta | Para qué | Sin ella |
| --- | --- | --- |
| [rtk](https://github.com/rtk-ai/rtk) | Filtra la salida de los comandos (tests, builds, git, logs); [cómo decide](rtk.md) | Los comandos pasan enteros; nada se rompe |
| [ripgrep](https://github.com/BurntSushi/ripgrep) (`rg`) | Búsqueda de texto del scout; rtk también lo usa | El scout busca más lento y rtk recorta menos |
| [codegraph](https://github.com/colbymchenry/codegraph) | «Quién llama a qué» para el scout, y qué tests afecta un cambio | El scout usa el índice propio de nxy y `rg`; los tests afectados se buscan por nombre |
| [d2](https://d2lang.com) | Dibuja los diagramas del modo ingeniero en la TUI | El diagrama se muestra como texto fuente |

No hace falta pegar nada a mano: `/nxy:setup` detecta qué te falta, te muestra el comando exacto para tu sistema y lo corre **sólo si le decís que sí**. Elige el gestor que encuentra: `winget`, `brew`, o `apt-get`, `dnf` o `pacman` (con `sudo`: si no puede usarlo, te imprime el comando para que lo pegues vos en una terminal). rtk y codegraph se instalan con el script de su proyecto (`install.sh`; en Windows, `install.ps1` para codegraph); d2, con `https://d2lang.com/install.sh`. Si winget dice que no hay una versión más nueva (ya estaba instalado, por ejemplo d2 en `C:\Program Files\D2`), nxy lo toma como instalado, termina con exit 0 y vuelve a detectar: imprime dónde lo encontró o que no está en el PATH (abrí una terminal nueva).

Si preferís instalar a mano:

```
# Windows
winget install rtk-ai.rtk
winget install BurntSushi.ripgrep.MSVC

# Linux (Debian/Ubuntu)
sudo apt install ripgrep
curl -fsSL https://raw.githubusercontent.com/rtk-ai/rtk/refs/heads/master/install.sh | sh

# macOS
brew install ripgrep rtk
```

En Linux, rtk queda en `~/.local/bin`; si no está en el PATH, nxy igual lo encuentra. Verificá con `rtk --version` y `rg --version`. Si lo instalaste con la terminal abierta, abrí una nueva.

> **No corras `rtk init -g` ni `codegraph install`.** nxy ya instala su propio hook y usa codegraph por CLI desde adentro de un subagente. Si los conectás por su lado, rtk y nxy se pisan, y codegraph le suma su descripción a *cada* turno de Claude. `/nxy:setup` marca el choque («hay un hook que choca») y te dice cómo sacarlo.

### 2. El plugin

Desde GitHub (el repo es a la vez un marketplace de Claude Code):

```
/plugin marketplace add mananos/nxy.dev
/plugin install nxy@nxy-dev
```

Desde un clon local (para probar una rama que todavía no está publicada):

```
/plugin marketplace add /ruta/al/clon/nxy.dev
/plugin install nxy@nxy-dev
```

o sin instalar, sólo para esa sesión: `claude --plugin-dir /ruta/al/clon/nxy.dev`.

Queda instalado para tu usuario en todos los proyectos. `/plugin` lo lista y lo deshabilita. Para actualizar: `claude plugin marketplace update nxy-dev` y reinstalar.

### 3. La statusline (recomendado)

```
/nxy:statusline install
```

La escribe en `~/.claude/settings.json` (con un backup `settings.json.bak-nxy-<ms>` al lado (<ms> es un timestamp en milisegundos); se conservan los 3 últimos) y te pide reiniciar Claude Code para verla. Sin argumentos sólo te muestra el bloque que se escribiría y si ya está instalada; `/nxy:statusline remove` la quita, y sólo si es la de nxy. Una statusline que no es de nxy no se toca nunca.
### 4. La TUI (opcional)

La TUI es una consola de control que abrís en **otra terminal**, al lado de Claude Code: tus features, el plan, los agentes, la memoria, el gasto y la config. `/nxy:tui` te muestra el comando exacto para abrirla (es lo mismo que `nxy tui where`); nxy no abre terminales solo.

Para que alcance con escribir `nxy tui`, hace falta que `nxy` esté en el PATH. `nxy setup status` te dice si lo está (`nxy en el PATH: sí/no`) y, si no, el comando que lo deja: `nxy setup run path --yes`. En Windows suma `~/.nxy/bin` al PATH del usuario, y nxy ve el Path nuevo (el de usuario y el de sistema del registro) sin reiniciar la terminal ni la TUI; en Linux y macOS crea `~/.local/bin/nxy`, un script de 2 líneas (esa carpeta tiene que estar en tu PATH). Para sacarlo, quitá esa carpeta del PATH o borrá ese archivo.

`nxy tui --demo` abre el mismo programa con datos de ejemplo, sin tocar tu repo ni tu base: sirve para mirar antes de usarla.

### 5. Verificá

| Comando | Tiene que decir |
| --- | --- |
| `/nxy:filter status` | El motor del filtro: `rtk` (o por qué está apagado y cómo instalarlo) |
| `/nxy:locate --status` | Qué buscadores tenés: índice, `rg`, codegraph |
| `/nxy:mem show` | Las reglas y aprendizajes de este repo (vacío si todavía no hay) |
| `/nxy:gate status` | Si el freno de escritura y el de lectura están prendidos, y desde qué umbral frenan |
| `/nxy:setup` | Por herramienta (rtk, rg, codegraph, d2): si está, de dónde, si hay un hook que choca, y el comando exacto para instalarla; no instala nada solo |

## Qué hacer

Si todo dio bien, seguí con [Primer uso](primer-uso.md). Si algo falla, buscá el síntoma acá.

### Windows Defender

El binario de nxy no está firmado, y en Windows el antivirus puede ponerlo en cuarentena (por ejemplo con una detección genérica de aprendizaje automático).

- **Qué ves.** Un mensaje de nxy con `nxy: could not run <ruta> (exit …)` o `nxy: binary not found at <ruta>`, seguido de «nxy hooks do nothing until this is fixed» y de «Si en Windows el binario no corre o desaparece, puede que un antivirus (Windows Defender) lo haya puesto en cuarentena: volvé a compilarlo o bajarlo, o excluí su carpeta del antivirus». Claude también se entera y frena en vez de diagnosticar pasos de nxy.
- **Qué significa.** El binario no está o no se puede ejecutar, así que los hooks no hacen nada: nxy no filtra, no frena y no conduce features hasta que lo arregles. Tu código no corre ningún riesgo.
- **Qué hacés.** Una de estas tres:
  1. **Restauralo.** En Seguridad de Windows › Protección antivirus y contra amenazas › Historial de protección, buscá el elemento de nxy y permitilo.
  2. **Excluí la carpeta** `%USERPROFILE%\.nxy\bin` (o `%NXY_HOME%\bin` si moviste nxy) en Seguridad de Windows › Protección antivirus y contra amenazas › Administrar la configuración › Exclusiones, y dejá que nxy baje el binario de nuevo. Desde un PowerShell de administrador: `Add-MpPreference -ExclusionPath "$env:USERPROFILE\.nxy\bin"`.
  3. **Compilalo vos.** Hace falta [Go](https://go.dev/dl/) (la versión que dice `go.mod`).

     *Instalación remota* (sin clonar; funciona una vez que el tag `v2.1.0` está publicado):

     ```
     go install github.com/mananos/nxy.dev/v2/cmd/nxy@v2.1.0
     ```

     El binario queda en `GOBIN` si la tenés definida, y si no en `$(go env GOPATH)/bin` (`nxy.exe` en Windows). Para que el lanzador lo use, apuntá `NXY_BIN` a ese archivo (en PowerShell: `$env:NXY_BIN = "…\nxy.exe"`), o copialo a `$HOME/.nxy/bin/2.1.0/nxy.exe` (`$NXY_HOME/bin/…` si moviste nxy). Ojo: un `NXY_BIN` olvidado corre un build viejo sin avisar.

     *Desde el clon.* Clonás el repo en el tag de la versión y lo compilás en la carpeta exacta donde lo busca el lanzador, así no baja nada:

     ```
     git clone --branch v2.1.0 https://github.com/mananos/nxy.dev
     cd nxy.dev
     mkdir -p "$HOME/.nxy/bin/2.1.0"
     go build -o "$HOME/.nxy/bin/2.1.0/nxy.exe" ./cmd/nxy
     ```

     (En PowerShell, `mkdir -p` se reemplaza por `New-Item -ItemType Directory -Force "$HOME/.nxy/bin/2.1.0"`. En Linux y macOS el archivo se llama `nxy`, sin `.exe`.) La carpeta lleva la versión del plugin que instalaste.

### Si algo no anda

| Síntoma | Qué pasa | Qué hacer |
| --- | --- | --- |
| Ves `nxy: curl not found, cannot download the binary` o `nxy: download of nxy_… failed` | El lanzador no pudo bajar el binario (sin `curl`, sin red, o el release todavía no existe) | Instalá `curl` o revisá la red, y abrí una sesión nueva; o compilalo vos (arriba, «Compilalo vos») |
| Ves `nxy: checksum mismatch for …` | El archivo bajado no coincide con el SHA-256 del release | Probá de nuevo; si sigue, compilalo vos |
| Ves `nxy: downloading its binary in another hook - this hook was skipped` | Otro hook se está bajando el binario; sólo uno descarga a la vez | Nada: es de una sola vez |
| Los comandos no salen filtrados | rtk no está, está fuera del PATH, o tiene su propio hook | `/nxy:filter status` te dice cuál y cómo arreglarlo. Si lo instalaste con la terminal abierta, abrí una nueva |
| Aviso de que el hook de rtk está instalado | Corriste `rtk init -g` | Desinstalá el hook de rtk (`/nxy:setup` te dice cómo); nxy usa el suyo |
| Claude necesita la salida completa de un comando | El filtro recortó algo que hacía falta | Claude lo resuelve solo con `rtk recall <hash>`; como último recurso, `NXY_RAW=1 <comando>` |
| El [freno](gate.md) de escritura frena todo | No hay feature activa, así que el hilo principal no edita código; es lo esperado (no depende del contexto) | Para una edición puntual, `/nxy:gate once`; para ese repo, poné `"gate": { "enabled": false }` en `.nxy/config.json` |
| Un `Accept:` de un lote falla en Windows con `test -f …` | El planner escribe los `Accept:` para `sh`; en Windows sin `sh` se usa `cmd /c`, y ese comando no existe ahí | Usá Git Bash (con `sh` en el PATH) o pedí el cambio del `Accept:` |
| Algún hook se comporta raro | Los hooks de nxy fallan abiertos: ante cualquier error dejan pasar | Corré `claude --debug`: el error del hook sale por stderr (`NXY_DEBUG` sólo afecta a la statusline) |
| Checkout compartido entre Windows y WSL con codegraph | El lock de SQLite no cruza filesystems | `CODEGRAPH_DIR=.codegraph-win` en Windows y el default en WSL |

## Comandos y config

| Comando | Qué hace |
| --- | --- |
| `/nxy:setup` | Detecta rtk, rg, codegraph y d2; instala uno sólo si le decís que sí |
| `nxy setup status [--json]` | Lo mismo desde la terminal, más la fila `nxy en el PATH` |
| `nxy setup run <rtk\|rg\|codegraph\|d2> --yes [--dry-run]` | Instala esa herramienta; `--dry-run` sólo muestra el comando |
| `nxy setup run path --yes` | Deja `nxy` en el PATH |
| `/nxy:statusline [install\|remove]` | Muestra, instala o saca la statusline |
| `/nxy:tui` · `nxy tui where` | El comando para abrir la TUI en otra terminal |
| `NXY_HOME`, `NXY_BIN`, `NXY_DEBUG` | Variables de entorno: carpeta de nxy, binario a usar, diagnóstico de la statusline |

## Relacionados

- [Primer uso](primer-uso.md): qué pasa solo y qué probar primero.
- [La TUI](tui.md): la consola que abrís en otra terminal.
- [Ver en qué se gasta](gasto.md): qué muestra la statusline.
- [Buscar](buscar.md): para qué sirven `rg` y codegraph.
- [El filtro (rtk)](rtk.md) · [El freno](gate.md): lo que `/nxy:filter status` y `/nxy:gate status` muestran.
- [Mensajes de nxy](mensajes.md): qué hacer con cada aviso.
- [Si venís de la 1.x](si-venis-de-la-1x.md): actualizar sin perder datos.
- [Configuración](configuracion.md): todas las claves y variables.
- [Cómo funciona](como-funciona.md): hooks, lanzador y binario por dentro.
