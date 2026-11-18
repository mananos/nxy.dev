# Configuración de nxy

Casi nada de esto hace falta tocarlo: los defaults están elegidos para que nxy funcione sin calibrar nada. Esta página es la referencia completa para cuando querés cambiar algo puntual.

## Dónde vive

| Archivo | Alcance |
| --- | --- |
| `internal/platform/defaults.json` (va dentro del binario) | Defaults. No se edita |
| `~/.nxy/config.json` | Vos, en todos tus proyectos. Es donde guarda `nxy config set` si no le pedís otra cosa |
| `<repo>/.nxy/config.json` | El repo. Se commitea: quien lo clone hereda lo mismo sin configurar nada |

Se combinan en ese orden con deep-merge (los objetos se mezclan; listas y valores sueltos se reemplazan). Después se aplican `NXY_FILTER` y `NXY_ENGINE` (ver abajo).

`.nxy/` vive en la raíz del repo git, desde cualquier subcarpeta donde arranques un comando. Un `.nxy/config.json` dentro de una subcarpeta ya no se lee: movelo a la raíz del repo. Fuera de un repo git se usa la carpeta donde estás.

### Cómo se cambia

- `nxy config` (o `nxy config show`): cada clave que se puede cambiar, su valor efectivo y de dónde viene (`predeterminado`, `usuario`, `repo` o `env`), más la ruta de los dos archivos y si existen o están rotos.
- `nxy config set <clave> <valor|default> [--repo]`: guarda en `~/.nxy/config.json`; con `--repo`, en `<repo>/.nxy/config.json`. `default` borra la clave. Si el archivo está roto no lo toca y te lo dice: arreglalo a mano y repetí.
- `nxy config cache-ttl 1h|off`: escribe o quita `promptCacheTtl` en `~/.claude/settings.json` (deja un backup `settings.json.bak-nxy-<ms>`, se conservan los 3 últimos). Claude Code lo toma en la próxima sesión.
- `/nxy:config`: lo mismo desde el chat, con los mismos argumentos.
- La pestaña **Config** de la TUI ([abrirla](#abrir-la-tui)): muestra lo mismo y lo cambia con botones.

`config set` sólo acepta las claves de `nxy config show`: `modules.filter` (`true` o `false`), `filter.engine` (`auto`, `rtk` o `off`) y `roles.<rol>.model` / `roles.<rol>.effort` (ver [roles](#roles)). Las demás claves de esta página se editan a mano en el JSON.

```
.nxy/
  config.json     # commiteado: la config del repo
  lenses/         # commiteado: lentes de review propias del repo
  local/          # en .gitignore: estado que cada clon regenera
```

Regla: `.nxy/` se comparte, `.nxy/local/` es de tu checkout.

## Variables de entorno

| Variable | Efecto |
| --- | --- |
| `NXY_FILTER=0\|1` | Apaga o prende el filtro (pisa `modules.filter`) |
| `NXY_ENGINE=rtk\|off` | Fuerza el motor del filtro (pisa `filter.engine`) |
| `NXY_RTK_PATH` | Ruta a `rtk` si no está en el PATH |
| `NXY_CODEGRAPH_PATH` | Ruta a `codegraph` si no está en el PATH |
| `NXY_D2_PATH` | Ruta a `d2` si no está en el PATH |
| `NXY_REWRITE_CACHE=0` | Apaga la cache de reescrituras del filtro (`~/.nxy/cache/rewrite-v2.json`) |
| `NXY_RAW=1` | Como prefijo de un comando (en cualquier segmento de una cadena), ese comando no se filtra |
| `NXY_DEBUG=1` | Si la statusline falla, imprime la causa en stderr en vez de sólo `nxy ?` |
| `NXY_ICONS=nerd\|unicode` | Fuerza los íconos de la TUI (por defecto lo adivina; `--icons` de `nxy tui` hace lo mismo) |
| `NXY_HOME` | Mueve `~/.nxy` a otra carpeta |
| `NXY_BIN` | Ruta a un binario de nxy para que el lanzador (`bin/nxy.cmd`) corra ése en vez de bajar el de la versión. Para desarrollo: un `NXY_BIN` olvidado corre un build viejo sin avisar |
| `NXY_RELEASE_URL` | Carpeta de la que el lanzador baja el binario y `checksums.txt` (por defecto, el release de GitHub de la versión) |

## Los defaults

```json
{
  "modules": { "filter": true, "metrics": true },
  "filter": { "engine": "auto", "excludeCommands": [], "onlyCommands": [], "autoAllowWhenOriginalAllowed": true },
  "metrics": {
    "projectsDir": null,
    "subscription": true,
    "statusline": {
      "color": true, "preset": "vivid", "layout": "line", "brand": "◆ nxy",
      "ctxWarnTokens": 100000, "ctxCritTokens": 200000, "turnWarnUsd": 1, "turnCritUsd": 3,
      "promptCacheTtlMin": 5, "theme": {}
    },
    "cacheBreakThreshold": 100000
  },
  "gate": { "enabled": true, "contextTokens": 100000, "escapeMinutes": 5, "reads": true }
}
```

`scout.codegraph` no está en el archivo: se lee, y vale `true` si no está.

## Cada sección

### `modules`

`metrics` y `filter` se prenden y apagan por separado. Con `filter: false` los comandos pasan intactos; con `metrics: false` el filtro no registra una fila por comando.

### `filter`

| Clave | Qué hace |
| --- | --- |
| `engine` | `auto` (usa rtk si está), `rtk` o `off` |
| `excludeCommands` | Comandos que nunca se filtran (por el primer token, ej. `["kubectl"]`) |
| `onlyCommands` | Si no está vacía, sólo se filtran estos |
| `autoAllowWhenOriginalAllowed` | Si el comando original estaba permitido por tus reglas `permissions.allow`, el reescrito hereda el permiso y no aparece un prompt nuevo |

`/nxy:filter` ([abajo](#nxyfilter)) prende y apaga el filtro sin tocar el JSON.

### `metrics`

| Clave | Qué hace |
| --- | --- |
| `projectsDir` | Dónde están los transcripts de Claude Code (`null` = `~/.claude/projects`) |
| `subscription` | `true` muestra los montos como *equivalente* (`$1.20~`) |
| `cacheBreakThreshold` | Desde cuántos tokens una reescritura de cache cuenta como "reconstrucción" en `stats` y `trend` |

### `metrics.statusline`

| Clave | Qué hace |
| --- | --- |
| `preset` | `vivid` (default: cada dato con su color), `classic` (sobrio) o `powerline` (bloques con fondo). Un nombre desconocido cae en `classic` |
| `layout` | `line` o `two-line` (la barra de contexto al doble de ancho) |
| `separator` | Reemplaza el separador entre datos |
| `color` | `false` = sin colores ANSI |
| `brand` | Texto inicial; `""` lo oculta |
| `ctxWarnTokens` / `ctxCritTokens` | Naranja y rojo del contexto, en tokens |
| `turnWarnUsd` / `turnCritUsd` | Naranja y rojo del turno, en USD |
| `promptCacheTtlMin` | Minutos de inactividad desde los que la statusline marca la cache como fría, cuando Claude Code no informa su estado |
| `theme` | Colores por rol (ver abajo) |

`theme` sobreescribe cualquier rol del preset: `brand`, `separator`, `path`, `branch`, `model`, `effort`, `where`, `ctx`, `turn`, `session`, `cache`, `limits`, `label`, `value`, `gauge`, `gaugeEmpty`, `warn`, `crit`. Cada valor son tokens separados por espacio: nombres (`red`, `cyan`, `muted`, `bold`, `dim`…), un índice 0–255 de la paleta, `#rrggbb`, o `bg:<color>` para el fondo.

```json
{ "metrics": { "statusline": { "theme": { "brand": "bold magenta", "gauge": "#ff79c6" } } } }
```

Los cambios se ven en el próximo refresco, sin reiniciar. La statusline la dibuja `nxy statusline`, el mismo binario; cómo instalarla, en [`/nxy:statusline`](#nxystatusline).

### `scout`

`codegraph: false` desactiva el índice de codegraph en `nxy locate` aunque esté instalado.

### `gate`

| Clave | Qué hace |
| --- | --- |
| `enabled` | `false` apaga el freno de escritura y también el de búsquedas amplias |
| `reads` | `false` deja el freno de búsquedas amplias del hilo principal apagado (por defecto `true`). También: `/nxy:gate reads off\|on` (guarda en tu usuario; con `--repo`, en el repo) |
| `contextTokens` | Desde cuánto contexto del hilo principal se delegan las ediciones y se frenan las búsquedas amplias |
| `escapeMinutes` | Cuánto vale un `/nxy:gate once` o `/nxy:gate once search` antes de vencer solo (mínimo 1) |

Las filas del freno quedan en un `metrics/gate.jsonl` del estado local, con tope de 256 KB: al pasarlo se descartan las de más de 90 días, y si aun así sobra quedan las más nuevas.

### `roles`

Qué modelo y effort usa cada rol de nxy. nxy lo aplica al despachar cada agente: `"planner": { "model": "sonnet", "effort": "medium" }` hace que el planner corra en Sonnet con effort medio (más barato que el default, Opus high).

| Rol | Modelo por defecto | Effort por defecto |
| --- | --- | --- |
| `planner` | opus | high |
| `implementer` | sonnet | medium |
| `reviewer` | sonnet | high |
| `reviewer-quick` | sonnet | medium |
| `refuter` | sonnet | high |
| `scout` | sonnet | medium |
| `documenter` | sonnet | low |

Los defaults salen del encabezado de cada `agents/*.md`. Valores que acepta la config: `roles.<rol>.model` = `haiku`, `sonnet`, `opus` o `fable`; `roles.<rol>.effort` = `low`, `medium` o `high`. Sin clave queda lo del agente. Si el despacho ya trae modelo o effort explícito, gana el del despacho.

```
nxy config set roles.planner.model sonnet
nxy config set roles.planner.effort default --repo
```

### Lentes de review

Cada `.md` en `<repo>/.nxy/lenses/` (o en `~/.nxy/lenses/` para todos tus repos) suma una lente propia. Con el mismo nombre que una de nxy la reemplaza, y con `enabled: false` en el frontmatter la apaga. Las lentes de nxy están en [`lenses/`](../lenses).

### Claves de la 1.x que la v2 ignora

Si venís de la 1.x y tus `config.json` todavía las tienen, no hacen nada (no se borran ni dan error): `flow` (`plan`, `pauseAfterBatch`, `orchestrator`), `ui`, `docs`, `memory` (`mode`, `handoff`), el rol `tester` y el que relacionaba memorias, y `provider` dentro de un rol.

## Comandos relacionados

### /nxy:statusline

Sin argumentos muestra el bloque `statusLine` que se escribiría en `~/.claude/settings.json` y si ya está instalada. `/nxy:statusline install` la escribe (con backup `bak-nxy-<ms>` junto al archivo; se conservan los 3 últimos), te dice la ruta del lanzador y te pide reiniciar Claude Code. `/nxy:statusline remove` la quita, sólo si es la de nxy. Sólo estos dos subcomandos escriben.

Una copia estable del lanzador queda en `~/.nxy/bin/nxy.cmd` (unos 13 KB) y se actualiza al abrir sesión con la versión del binario; `remove` la borra.

### /nxy:setup

Muestra el estado de las herramientas: rtk, rg, codegraph y d2 (instaladas o no, de dónde, y cómo instalarlas). Sólo detecta: no instala nada solo. Para instalar una, decí cuál y que sí: corre `nxy setup run <rtk|rg|codegraph|d2> --yes`. Sin `--yes` muestra los pasos y no hace nada; con `--dry-run` los lista sin correrlos. Dónde instala cada una:

| Herramienta | Windows | Linux | macOS |
| --- | --- | --- | --- |
| rtk | `winget install rtk-ai.rtk` (actualizar: `winget upgrade rtk-ai.rtk`) | `install.sh` de rtk-ai/rtk | `brew install rtk` |
| rg | `winget install BurntSushi.ripgrep.MSVC` | el gestor de paquetes que haya (`apt`, `dnf`, `pacman`, con `sudo -n`) | `brew install ripgrep` |
| codegraph | `install.ps1` de colbymchenry/codegraph | `install.sh` de colbymchenry/codegraph | ídem Linux |
| d2 (dibuja los diagramas del diseño) | `winget install Terrastruct.D2` | `install.sh` de d2lang.com con `--prefix "$HOME/.local"` | `brew install d2` |

Un `sudo` sin terminal falla sin colgarse y te devuelve el comando para correr a mano. Si hay un hook de rtk propio (`rtk init -g`) o un servidor MCP de codegraph, lo marca como choque y los pasos de reinstalación incluyen quitarlo cuando es posible; si no, te dice cómo hacerlo a mano (nxy no edita settings ajenos).

La fila «nxy en el PATH» (`nxy setup run path --yes`) hace que alcance con escribir `nxy tui`. En Windows suma `~/.nxy/bin` al `Path` del usuario; en Linux y macOS crea `~/.local/bin/nxy`, un script de 2 líneas que llama al lanzador (esa carpeta tiene que estar en tu PATH). Si cambiás `NXY_HOME`, repetilo. Para sacarlo: quitá la carpeta del PATH del usuario o borrá `~/.local/bin/nxy`.

### /nxy:filter

`/nxy:filter status` muestra el motor del filtro (rtk o no), rg, lo excluido y la salida de emergencia (`NXY_RAW=1`). `on` y `off` escriben `modules.filter` en `~/.nxy/config.json`, o en el `.nxy/config.json` del repo con `--repo`, conservando las otras claves, y refrescan la cache del motor. Si el archivo está roto no lo toca y sale con error.

### /nxy:config

Es `nxy config` ([cómo se cambia](#cómo-se-cambia)) desde el chat: `show`, `set <clave> <valor|default> [--repo]` y `cache-ttl 1h|off`.

### Abrir la TUI

La TUI necesita su propia terminal. `/nxy:tui` no la abre: te muestra el comando exacto (el de `nxy tui where`) para pegar en otra terminal. Si `nxy` ya está en el PATH es `nxy tui`; si no, es el lanzador, por ejemplo `& "<ruta>/nxy.cmd" tui` en PowerShell o `sh "<ruta>/nxy.cmd" tui` en Git Bash, Linux y macOS. Con `nxy tui --demo` abre el mismo programa con datos de ejemplo, sin tocar tu db ni tu repo.
