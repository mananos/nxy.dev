# Configuración de nxy

Casi nada de esto hace falta tocarlo: los defaults están elegidos para que nxy funcione sin calibrar nada. Esta página es la referencia completa para cuando querés cambiar algo puntual, y dice dónde guarda nxy cada cosa.

## Para qué sirve

La config de nxy son dos archivos JSON opcionales (uno tuyo y uno del repo) que se combinan con los defaults que trae el binario. Ahí elegís qué modelo usa cada agente, si el filtro y los frenos están prendidos y cómo se ve la statusline. Si no existe ninguno, rige todo por defecto.

## Qué ves

La forma más cómoda es la pantalla **Config** de la TUI ([abrirla](tui.md)): muestra cada clave que se puede cambiar, su valor efectivo y de dónde viene, y lo cambia con teclas. Probala sin datos: `nxy tui --demo`.

Con `nxy config` (o `/nxy:config`) ves lo mismo en texto: cada clave, su valor y su origen entre paréntesis (`predeterminado`, `usuario`, `repo` o `env`), y al final los dos archivos con su estado (`existe`, `no existe` o `roto`).

## Qué significa

### Dónde vive

| Archivo | Alcance |
| --- | --- |
| `internal/platform/defaults.json` (va dentro del binario) | Los defaults. No se editan |
| `~/.nxy/config.json` | Vos, en todos tus proyectos. Es donde guardan `nxy config set` y la TUI si no les pedís otra cosa |
| `<repo>/.nxy/config.json` | El repo. Se elige a propósito (`--repo`, o el botón `r` de la TUI), y se puede commitear: quien lo clone hereda lo mismo |

Se combinan en ese orden con *deep-merge*: los objetos se mezclan; las listas y los valores sueltos se reemplazan. Después se aplican las variables `NXY_FILTER` y `NXY_ENGINE` (abajo), que ganan sobre todo.

`.nxy/` vive en la **raíz del repo git**, desde cualquier subcarpeta donde arranques un comando. Un `.nxy/config.json` dentro de una subcarpeta no se lee: movelo a la raíz. Fuera de un repo git se usa la carpeta donde estás.

Un cambio de config aplica «al próximo agente que se lance» y, para el filtro, en el comando siguiente. Si un archivo tiene un JSON roto, nxy lo ignora al leer y se niega a escribir en él (`<ruta> está roto; no se toca`): arreglalo a mano.

### Variables de entorno

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

Las variables que usan los tests o la TUI por dentro (`NXY_WRITE_GOLDEN`, `NXY_TUI_NONCE`, …) no son para vos.

### Los defaults

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

`scout.codegraph` no está en el archivo: se lee, y vale `true` si no está. Los roles tampoco: sin clave rige lo que dice el encabezado de cada agente.

### Cada sección

#### `modules`

`metrics` y `filter` se prenden y apagan por separado. Con `filter: false` los comandos pasan intactos; con `metrics: false` el filtro no registra una fila por comando. Más en [El filtro de comandos](rtk.md).

#### `filter`

| Clave | Qué hace |
| --- | --- |
| `engine` | `auto` (usa rtk si está), `rtk` o `off` |
| `excludeCommands` | Comandos que nunca se filtran (por el primer token, ej. `["kubectl"]`) |
| `onlyCommands` | Si no está vacía, sólo se filtran estos |
| `autoAllowWhenOriginalAllowed` | Si el comando original estaba permitido por tus reglas `permissions.allow`, el reescrito hereda el permiso y no aparece un prompt nuevo |

#### `metrics`

| Clave | Qué hace |
| --- | --- |
| `projectsDir` | Dónde están los transcripts de Claude Code (`null` = `~/.claude/projects`) |
| `subscription` | `true` muestra los montos como *equivalente* (`$1.20~`) |
| `cacheBreakThreshold` | Desde cuántos tokens una reescritura de cache cuenta como «reconstrucción» en `stats` y `trend` |

#### `metrics.statusline`

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

Los cambios se ven en el próximo refresco, sin reiniciar. Qué significa cada segmento: [Ver en qué se gasta](gasto.md).

#### `scout`

`codegraph: false` desactiva el índice de codegraph en `nxy locate` aunque esté instalado ([Buscar](buscar.md)).

#### `gate`

| Clave | Qué hace |
| --- | --- |
| `enabled` | `false` apaga el freno de escritura y también el de búsquedas amplias |
| `reads` | `false` deja el freno de búsquedas amplias del hilo principal sólo observando (por defecto `true`). También: `/nxy:gate reads off\|on` (guarda en tu usuario; con `--repo`, en el repo) |
| `contextTokens` | Desde cuánto contexto del hilo principal se frena una búsqueda amplia |
| `escapeMinutes` | Cuánto vale un `/nxy:gate once` o `/nxy:gate once search` antes de vencer solo (mínimo 1) |

Más en [El freno](gate.md).

#### `roles`

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

Los defaults salen del encabezado de cada `agents/*.md`. Valores que acepta la config: `roles.<rol>.model` = `haiku`, `sonnet`, `opus` o `fable`; `roles.<rol>.effort` = `low`, `medium`, `high`, `xhigh` o `max`. Sin clave queda lo del agente. Un valor inválido en la config (por ejemplo `"effort": "extreme"`) no se descarta en silencio: `nxy config show`, la pestaña Config y el despacho de ese rol lo avisan, y el rol sigue con lo de su agente. Si el despacho ya trae modelo o effort explícito, gana el del despacho. Editar los `.md` del plugin se pisa al actualizar: usá la config.

```
nxy config set roles.planner.model sonnet
nxy config set roles.planner.effort default --repo
```

#### Lentes de review

Cada `.md` en `<repo>/.nxy/lenses/` (o en `~/.nxy/lenses/` para todos tus repos) suma una lente propia. Con el mismo nombre que una de nxy la reemplaza, y con `enabled: false` en el frontmatter la apaga. Las lentes de nxy están en [`internal/review/lenses/`](../internal/review/lenses). Más en [Review](review.md).

#### Runners de tests afectados

`<repo>/.nxy/runners.json` suma filas a la tabla de runners que trae nxy, para que los tests afectados corran con tu propio comando ([Plan y lotes](plan-y-lotes.md)). Una fila que no estaba en el plan aprobado no se usa, y nxy lo avisa.

#### Claves de la 1.x que la v2 ignora

Ver [Si venís de la 1.x](si-venis-de-la-1x.md#claves-de-la-1x-que-la-v2-ignora): no hacen nada, pero no dan error.

## Qué hacer

Cómo se cambia, de lo más cómodo a lo más manual:

- **La pestaña Config de la TUI.** `e` cambia el modelo (o el valor de un módulo), `f` el effort, `r` guarda el valor en el repo en vez de en tu usuario.
- **`nxy config set <clave> <valor|default> [--repo]`** (o `/nxy:config set …` desde el chat): guarda en `~/.nxy/config.json`; con `--repo`, en `<repo>/.nxy/config.json`, conservando las otras claves. `default` borra la clave.
- **A mano**, en el JSON, para todo lo demás.

`nxy config set` sólo acepta las claves que lista `nxy config show`:

| Clave | Valores |
| --- | --- |
| `modules.filter` | `true` o `false` |
| `filter.engine` | `auto`, `rtk` u `off` |
| `roles.<rol>.model` | `haiku`, `sonnet`, `opus` o `fable` |
| `roles.<rol>.effort` | `low`, `medium`, `high`, `xhigh` o `max` |

con `<rol>` = `planner`, `implementer`, `reviewer`, `reviewer-quick`, `refuter`, `scout` o `documenter`. Con otra clave responde `campo desconocido: <clave>`, y con un valor que no está en la lista, `valor inválido para <clave>: <valor>`. Las demás claves de esta página (el `gate`, la `statusline`, `metrics`…) se editan a mano en el JSON; hay atajos para algunas: `/nxy:gate reads on|off`, `/nxy:filter on|off`.

Las recetas más pedidas:

| Querés | Hacé |
| --- | --- |
| Ver cada clave, su valor y de dónde viene | `/nxy:config` (o `nxy config show`) |
| Que el planner de este repo use Sonnet (más barato) | `nxy config set roles.planner.model sonnet --repo` |
| Apagar los dos frenos | En el JSON: `"gate": { "enabled": false }` |
| Apagar sólo el freno de búsquedas amplias | `/nxy:gate reads off` |
| Apagar el filtro | `/nxy:filter off` (o `nxy config set modules.filter false`) |
| Probar la cache de 1 hora | `nxy config cache-ttl 1h` |
| Lentes de review propias del repo | Un `.md` en `.nxy/lenses/` |
| Tests afectados con tu propio runner | Un `.nxy/runners.json` en el repo |

`nxy config cache-ttl 1h|off` no toca la config de nxy: escribe o quita `promptCacheTtl` en `~/.claude/settings.json` (deja un backup `settings.json.bak-nxy-<ms>` al lado; se conservan los 3 últimos), y Claude Code lo toma en la próxima sesión. Cuándo conviene: [Ver en qué se gasta](gasto.md).

## Dónde guarda nxy sus cosas

Igual que `.claude`, hay una carpeta tuya y una por repo. `NXY_HOME` mueve `~/.nxy` a otra carpeta.

| Carpeta o archivo | Qué tiene | ¿Se commitea? |
| --- | --- | --- |
| `~/.nxy/nxy.db` | La base SQLite: features, memoria, notas, resumen del filtro, costos por agente | No: es de tu máquina |
| `~/.nxy/config.json` | Tu config global | No |
| `~/.nxy/bin/<versión>/` | El binario de esa versión (el lanzador lo baja la primera vez) y, si instalaste la statusline o la fila del PATH, una copia estable del lanzador (`~/.nxy/bin/nxy.cmd`, unos 13 KB) | No |
| `~/.nxy/plans/` y `~/.nxy/reviews/` | El plan y la review de cada feature (`<id>.md`, 5 a 15 KB el plan), para **Ver completo**. Se borran al cerrar con éxito o al parar la feature | No |
| `~/.nxy/cache/` | Lo que nxy puede rehacer: el motor del filtro (`engine-v2.json`), la cache de reescrituras de rtk (`rewrite-v2.json`, hasta 500 comandos, unos 75 KB) y el estado de la statusline (`statusline-v2/`, unos 7 KB por sesión; los de más de 7 días se borran) | No |
| `~/.nxy/run/` | Una señal `tui-<pid>` (unos 50 bytes) mientras una TUI está abierta; se borra al cerrarla | No |
| `~/.nxy/lenses/` | Tus lentes de review | No |
| `<repo>/.nxy/config.json`, `<repo>/.nxy/lenses/`, `<repo>/.nxy/runners.json` | Lo que el equipo comparte: config, lentes y runners de tests | **Sí**, si querés compartirlos |
| `<repo>/.nxy/local/` | Lo de tu checkout: el índice de búsqueda (`index/`), el registro del freno (`metrics/gate.jsonl`, tope 256 KB), los permisos `once` (`gate-escape*.json`) y la marca de pausa de un worktree (`features/paused.json`) | No: ignoralo en tu `.gitignore`; nxy no lo agrega solo |

Regla: `.nxy/` se comparte, `.nxy/local/` es de tu checkout. Borrar `.nxy/local/` de un repo descarta sólo esos datos del checkout (el índice se rehace solo): el estado de las features vive en `nxy.db`, no ahí. Si codegraph está instalado, nxy también crea en cada repo donde lo necesita un `.codegraph/` (el índice de codegraph, ignorado en `.git/info/exclude`; no toca tu `.gitignore`).

Lo que viaja a otra máquina no es la base sino la memoria exportada (`/nxy:mem export`, [Memoria](memoria.md)). El costo por agente de cada feature vive sólo en tu máquina y se borra a los 90 días.

## Comandos y config

| Comando | Qué hace |
| --- | --- |
| `nxy config [show]` · `/nxy:config` | Cada clave que se puede cambiar, su valor efectivo y su origen, más los dos archivos |
| `nxy config set <clave> <valor\|default> [--repo]` | Cambia una clave; `default` la borra |
| `nxy config cache-ttl 1h\|off` | Escribe o quita `promptCacheTtl` en `~/.claude/settings.json` |
| `/nxy:filter [status\|on\|off] [--repo]` | Ver o prender/apagar el filtro ([rtk](rtk.md)) |
| `/nxy:gate reads on\|off [--repo]` | Prender o apagar el freno de búsquedas ([gate](gate.md)) |
| `/nxy:statusline [install\|remove]` | Mostrar, instalar o sacar la statusline; el bloque que escribe va a `~/.claude/settings.json` con backup |
| `/nxy:setup` | Estado de rtk, rg, codegraph y d2; instala lo que digas que sí |
| `nxy tui where` · `/nxy:tui` | El comando para abrir la TUI en otra terminal |

`/nxy:statusline install` además deja una copia estable del lanzador en `~/.nxy/bin/nxy.cmd`, que se actualiza al abrir sesión; `remove` la borra. `/nxy:setup` no instala nada solo: sin `--yes` muestra los pasos, con `--dry-run` los lista. El detalle de cada herramienta, por sistema, está en [Instalación](instalacion.md).

## Relacionados

- [La TUI](tui.md): la pantalla Config.
- [El filtro de comandos](rtk.md) · [El freno](gate.md) · [Ver en qué se gasta](gasto.md): las secciones que más se tocan.
- [Instalación](instalacion.md): rtk, rg, codegraph y d2, y la statusline.
- [Cómo funciona por dentro](como-funciona.md) · [Mensajes de nxy](mensajes.md)
