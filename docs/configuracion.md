# Configuración de nxy

Casi nada de esto hace falta tocarlo: los defaults están elegidos para que nxy funcione sin calibrar nada. Esta página es la referencia completa para cuando querés cambiar algo puntual.

## Dónde vive

| Archivo | Alcance |
| --- | --- |
| `nxy.config.json` (en la raíz del plugin) | Defaults. No lo edites: se pisa al actualizar |
| `~/.nxy/config.json` | Vos, en todos tus proyectos |
| `<repo>/.nxy/config.json` | El repo. **Se commitea**: quien lo clone hereda el mismo filtro, umbrales, roles y flujo sin configurar nada |

Se combinan en ese orden con deep-merge (los objetos se mezclan; listas y valores sueltos se reemplazan). Después se aplican las variables de entorno.

```
.nxy/
  config.json     # commiteado: la config del repo
  memory/         # commiteado: las memorias exportadas (/nxy:mem export)
  lenses/         # commiteado: lentes de review propias del repo
  local/          # en .gitignore: estado que cada clon regenera
```

Regla: **`.nxy/` se comparte, `.nxy/local/` es de tu checkout.**

## Variables de entorno

| Variable | Efecto |
| --- | --- |
| `NXY_FILTER=0\|1` | Apaga o prende el filtro |
| `NXY_ENGINE=rtk\|off` | Fuerza el motor del filtro |
| `NXY_RTK_PATH` | Ruta a `rtk` si no está en el PATH |
| `NXY_RAW=1` | Como prefijo de un comando (en cualquier segmento de una cadena), ese comando no se filtra |
| `NXY_DEBUG=1` | Los hooks imprimen en stderr por qué fallaron (siempre fallan abiertos) |
| `NXY_HOME` | Mueve `~/.nxy` a otra carpeta (lo usan los tests) |
| `NXY_STATUSLINE` | Fuerza el script de statusline que usa el lanzador |
| `CODEGRAPH_DIR` | Carpeta del índice de codegraph (ver [Windows + WSL](../README.md#si-algo-no-anda)) |

## Los defaults

```json
{
  "modules": { "metrics": true, "filter": true },
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
  "scout": { "codegraph": true },
  "gate": { "enabled": true, "contextTokens": 100000, "escapeMinutes": 5 },
  "roles": {
    "scout":       { "model": "haiku",  "provider": "claude" },
    "planner":     { "model": "sonnet", "provider": "claude" },
    "implementer": { "model": "sonnet", "provider": "claude" },
    "tester":      { "model": "haiku",  "provider": "claude" },
    "reviewer":    { "model": "sonnet", "provider": "claude" },
    "documenter":  { "model": "sonnet", "provider": "claude" },
    "librarian":   { "model": "haiku",  "provider": "claude" }
  },
  "memory": { "mode": "assisted", "handoff": { "required": true } }
}
```

## Cada sección

### `modules`

`metrics` y `filter` se prenden y apagan por separado. Sin `metrics` no se registra una fila por comando Bash; sin `filter` los comandos pasan intactos.

### `filter`

| Clave | Qué hace |
| --- | --- |
| `engine` | `auto` (usa rtk si está), `rtk` o `off` |
| `excludeCommands` | Comandos que nunca se filtran (por el primer token, ej. `["kubectl"]`) |
| `onlyCommands` | Si no está vacía, sólo se filtran estos |
| `autoAllowWhenOriginalAllowed` | Si el comando original estaba permitido por tus reglas `permissions.allow`, el reescrito hereda el permiso y no aparece un prompt nuevo |

### `metrics`

| Clave | Qué hace |
| --- | --- |
| `projectsDir` | Dónde están los transcripts de Claude Code (`null` = `~/.claude/projects`) |
| `subscription` | `true` muestra los montos como *equivalente* (`$1.20~`) |
| `cacheBreakThreshold` | Desde cuántos tokens una reescritura de cache cuenta como "reconstrucción" en `stats` y `trend` |

### `statusline`

| Clave | Qué hace |
| --- | --- |
| `preset` | `vivid` (default: cada dato con su color), `classic` (sobrio) o `powerline` (bloques con fondo) |
| `layout` | `line` o `two-line` (la barra de contexto al doble de ancho) |
| `separator` | Reemplaza el `⟡` |
| `color` | `false` = sin colores ANSI |
| `brand` | Texto inicial; `""` lo oculta |
| `ctxWarnTokens` / `ctxCritTokens` | Naranja y rojo del contexto, en tokens |
| `turnWarnUsd` / `turnCritUsd` | Naranja y rojo del turno, en USD |
| `promptCacheTtlMin` | Sólo para Claude Code anterior a 2.1.251, que no informa el estado de la cache |
| `theme` | Colores por rol (ver abajo) |

`theme` sobreescribe cualquier rol del preset: `brand`, `separator`, `path`, `branch`, `model`, `effort`, `where`, `ctx`, `turn`, `session`, `cache`, `limits`, `label`, `value`, `gauge`, `gaugeEmpty`, `warn`, `crit`. Cada valor son tokens separados por espacio: nombres (`red`, `cyan`, `muted`, `bold`, `dim`…), un índice 0–255 de la paleta, `#rrggbb`, o `bg:<color>` para el fondo.

```json
{ "metrics": { "statusline": { "theme": { "brand": "bold magenta", "gauge": "#ff79c6" } } } }
```

Los cambios se ven en el próximo refresco, sin reiniciar.

**Cómo se mantiene actualizada.** `/nxy:statusline --apply` no apunta a una versión del plugin sino a un lanzador en `~/.nxy/statusline.mjs`, que en cada arranque usa la versión de nxy que Claude Code tiene instalada. Para desarrollo, `node hosts/claude-code/entries/statusline-setup.mjs --apply` desde un clon la apunta a ese clon.

### `scout`

`codegraph: false` desactiva esa capa aunque esté instalada.

### `gate`

| Clave | Qué hace |
| --- | --- |
| `enabled` | `false` apaga el freno de escritura y, con él, la exigencia de guardar un handoff antes de editar (usan el mismo umbral) |
| `contextTokens` | Desde cuánto contexto del principal se delegan las ediciones |
| `escapeMinutes` | Cuánto vale un `/nxy:gate once` antes de vencer solo |

El gate nunca frena a un subagente, nunca frena si no hay implementer disponible y ante cualquier duda deja pasar.

### `roles`

Qué modelo usa cada rol. nxy lo aplica en cada despacho: `"planner": { "model": "opus" }` hace que el planner de ese repo corra en Opus. Valores: `sonnet`, `opus`, `haiku`, `fable`. Si Claude pide un modelo explícito para un despacho, gana el de Claude.

- El **effort** no está en la config: Claude Code no permite cambiarlo por despacho, así que vale el del archivo de cada agente (`agents/*.md`; ver la tabla en el [README](../README.md#modelo-y-effort-de-cada-agente)).
- `provider` todavía no hace nada (hay un solo proveedor); está declarado para que sumar otro sea un cambio de config.
- Borrar un rol lo apaga: sin `implementer` el gate no frena (no hay a quién delegar); sin `librarian` las memorias se guardan sin relacionarse; sin `documenter` nunca se ofrece actualizar docs.

### `flow`

`"plan": "always"`: en ese repo, todo cambio que toca un segundo archivo pasa primero por el planner. Sin ese valor decide Claude con la receta por tamaño.

### `docs`

`"paths": ["wiki/"]` fija dónde busca docs el documenter. Sin eso busca los `.md` del repo que nombran lo que cambió y la carpeta que tu CI publique a una wiki.

### `memory`

| Clave | Qué hace |
| --- | --- |
| `mode` | `assisted` (default): una línea por handoff al abrir sesión y punteros por mensaje. `manual`: nada automático, la memoria aparece sólo cuando la pedís. `proactive`: el handoff entero en cada sesión de esa rama |
| `handoff.required` | `false` deja de exigir el handoff cuando la sesión pasa el umbral del gate (sigue disponible a mano) |

### Lentes de review

Cada `.md` en `<repo>/.nxy/lenses/` suma una lente propia del repo. Con el mismo nombre que una de nxy la reemplaza, y con `enabled: false` en el frontmatter la apaga. `/nxy:review escape "<regla>" --as lens` agrega reglas a `.nxy/lenses/repo.md`. Las lentes de nxy están en [`lenses/`](../lenses).
