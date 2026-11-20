# Si venís de la 1.x

La v2 reemplaza a la 1.x y trae tus datos. Esta página dice qué cambia, qué se importa y cuándo, qué hacer con un plan de la 1.x a medias, y cómo volver.

## Para qué sirve

La v2 es otro programa: un binario en Go en vez de Node, con otro flujo (features con un plan que aprobás y lotes que verifica nxy, en vez del handoff y el orquestador). Para que el cambio no te cueste lo que ya construiste, la primera vez que abrís Claude Code en cada repo, nxy **copia** lo que la 1.x sabía de ese repo. Nunca escribe un archivo de la 1.x: volver a ella funciona.

## Qué ves

La primera sesión de cada repo que la 1.x conocía, un aviso único:

```
nxy: datos de la 1.x importados — 12 convenciones → aprendizajes A1..A12 · 3 handoffs vivos → notas · 41 archivados → features cerradas · 30 sesiones del filtro · 26 reviews, 88 elegidos
```

Si el repo no tiene datos de la 1.x, no sale ningún aviso. Si la copia falla, ves `nxy: la importación de la 1.x falló (…); probá nxy legacy import`. Y si la 1.x dejó un plan aprobado sin terminar, ves una pregunta ([más abajo](#una-feature-de-la-1x-a-medias)).

## Qué significa

### Qué cambia en el día a día

| Antes (1.x) | Ahora (v2) |
| --- | --- |
| Handoff y orquestador conducen la tarea | **Features**: un plan que aprobás, lotes que ejecuta un implementer y que verifica nxy corriendo el `Accept:` ([Una feature, paso a paso](feature.md)) |
| Agentes `tester` y `librarian` | No existen. Hay un `reviewer-quick` y un `refuter` nuevos, y la review corre por lentes ([Review](review.md)) |
| La skill `nxy-workflow` | No existe: el binario conduce el flujo y le dice a Claude cada paso |
| Memoria como memorias sueltas (convención, decisión, bug…) | **Reglas** del repo (`K3`), reglas globales (`G2`) y **aprendizajes** (`A7`), con roles, edición y borrado ([Memoria](memoria.md)) |
| — | Nuevo en la v2: la [TUI](tui.md) (y [contestar desde ella](decidir.md)), el [modo ingeniero](ingeniero.md) y los [varios repos y worktrees](varios-repos.md) |
| `/nxy:feature`, `filter`, `gate`, `locate`, `mem`, `review`, `setup`, `stats`, `statusline`, `trend` | Los mismos, y suman `/nxy:engineer`, `/nxy:worktree`, `/nxy:config` y `/nxy:tui` |
| Métricas, statusline, filtro con rtk, scout y freno | Reescritos en Go con los mismos resultados que la 1.x (probados contra los golden de la 1.x) |

El filtro, el freno y la statusline se comportan como antes: [El filtro](rtk.md), [El freno](gate.md) y [Ver en qué se gasta](gasto.md).

### Qué se importa, y cuándo

nxy lee lo que la 1.x dejó (`~/.nxy/memory/memory.db`, que lee sobre una copia temporal, y los archivos de `.nxy/local/` del repo) y lo convierte a la v2:

| De la 1.x | Pasa a | Cuándo |
| --- | --- | --- |
| Convenciones, decisiones, bugs y preferencias | **Aprendizajes** `A<n>`, con los archivos a los que estaban atadas. No son reglas todavía | Una vez por repo |
| Handoffs vivos | **Notas**: entra el objetivo y el `## Next` primero, después lo demás hasta 2 KB, sin el plan. Si se cortó dice «(cortado al importar; completo en la 1.x)» | Una vez por repo. Los que llevan más de 30 días sin escribirse no entran (se cuentan como vencidos) |
| Handoffs archivados | **Features cerradas**, con sus archivos, para que el planner las encuentre por archivo | Una vez por repo |
| Memorias globales | **Aprendizajes globales**. No se entregan a ningún agente hasta que las promovés | Una sola vez por máquina |
| Sesiones del filtro (`metrics/filter.jsonl`) | El resumen por sesión que leen `nxy stats` y `nxy trend` | Cada arranque, leyendo sólo lo nuevo. Una sesión que ya escribió la v2 no se pisa |
| Reviews (`review.jsonl`) y los hallazgos que elegiste | La base de «elegidos vs ignorados por lente» | Una vez por checkout |

Lo que **no** entra se cuenta en el mismo aviso: memorias reemplazadas por otra, relaciones entre memorias que no son de archivo, «N privadas sin importar» (las memorias marcadas `private` nunca se importan) y «N repetidos sin importar» (aprendizajes que ya estaban).

El aviso usa estas palabras: `convenciones`, `decisiones`, `bugs` y `preferencias` (→ `aprendizajes A1..A12`), `handoffs vivos` (→ `notas`), `archivados` (→ `features cerradas`), `sesiones del filtro`, `reviews` y `elegidos`, `globales` (→ `aprendizajes globales A13..A15`), `privadas`, `repetidos` y `vencidos`.

**Costo.** La primera vez por repo, menos de 200 ms (una copia temporal de `memory.db`, de unos 2,5 MB, que se borra al terminar); después, unos milisegundos por arranque. Si la copia de la memoria falla, el arranque la reintenta como mucho una vez por día; `nxy legacy import` la reintenta cuando quieras.

**Qué hacer con lo importado:**

- Los aprendizajes no son reglas: revisalos con `/nxy:mem show` y volvé regla los que sigan valiendo con `nxy mem promote A<n> --question "<qué resuelve>"` (para las globales, lo mismo: `promote` las vuelve `G`).
- Las notas importadas aparecen en «¿Con cuál seguimos?» al abrir la carpeta; `nxy note list` las muestra y `nxy note done <id>` borra las que ya no sirven.

### Una feature de la 1.x a medias

Si la 1.x dejó en ese repo un plan aprobado con lotes sin cerrar, y no hay una feature de la v2 activa, al abrir una sesión (o tras un `/clear`) ves un aviso con el objetivo del plan y la pregunta:

```
nxy: quedó a medias una feature de la 1.x en este repo: «<objetivo>» (faltan los lotes 2 y 3). Te pregunto si la planifico de nuevo con la v2; el plan viejo no se retoma.
```

«faltan los lotes 2 y 3» se lee «falta el lote 2» con uno solo, y «aprobada, sin lotes corridos» si ninguno había arrancado. La pregunta es «¿La planifico de nuevo con la v2?», con encabezado «Feature de la 1.x» y dos opciones:

- **Planificarla de nuevo**: arranca una feature de la v2 cuyo pedido es el objetivo de la 1.x, con su plan como referencia. El plan viejo **no se retoma tal cual**: el planner arma uno nuevo y no se toca nada de la 1.x.
- **Ignorarla**: no se vuelve a ofrecer; lo que faltaba queda sin hacer.

Si no contestás, se vuelve a preguntar en el próximo arranque. Cada plan se pregunta una sola vez. Si ya hay una feature de la v2 en curso, esperá: se ofrece más tarde. Por eso conviene **terminar las features de la 1.x antes de actualizar**.

### El aviso de «1.x activada»

Si además de esta instalación hay otra de nxy 1.x habilitada (por ejemplo, una de proyecto), ves:

```
nxy: la 1.x de nxy sigue activada: desactivala mientras probás la v2 (las dos responden a los mismos hooks)
```

Desactivala con `/plugin`: si no, las dos reaccionan al mismo comando y se pisan.

### Claves de la 1.x que la v2 ignora

Tus `config.json` de la 1.x siguen sirviendo: las claves que la v2 no usa no hacen nada, no se borran y no dan error.

| Clave de la 1.x | Qué pasó |
| --- | --- |
| `flow` (`plan`, `pauseAfterBatch`, `orchestrator`) | El flujo lo conduce el binario; ya no se configura |
| `ui` | La interfaz es la TUI |
| `docs` | La v2 no tiene `docs.paths`: nxy busca los docs que nombran lo que cambió y te pregunta si actualizarlos ([Review](review.md)) |
| `memory` (`mode`, `handoff`) | La memoria es otra ([Memoria](memoria.md)) |
| El rol `tester` y el rol que relacionaba memorias (`librarian`) | Los agentes ya no existen |
| `provider` dentro de un rol | No se usa |

Las claves que la v2 sí lee (`modules`, `filter`, `metrics`, `gate`, `scout.codegraph` y `roles.<rol>.model|effort`) están en [Configuración](configuracion.md).

### Volver a la 1.x

La 1.x queda en la rama `v1` del repo. Para volver: clonala, sacá el marketplace actual y agregá el del clon.

```
git clone --branch v1 https://github.com/mananos/nxy.dev
```

```
/plugin marketplace remove nxy-dev
/plugin marketplace add <carpeta del clon>
/plugin install nxy@nxy-dev
```

Lo que hiciste en la v2 (features, reglas, aprendizajes) no vuelve a la 1.x, y la 1.x encuentra sus datos como los dejó.

## Qué hacer

1. **Antes de actualizar**, terminá las features de la 1.x que tengas abiertas.
2. **Actualizá** con el mismo comando de siempre: `claude plugin marketplace update nxy-dev` y reinstalá ([Instalación](instalacion.md)).
3. **Abrí Claude Code en cada repo** que usabas: ahí ocurre la importación. Mirá el aviso.
4. **Revisá `/nxy:mem show`** y promové los aprendizajes que sigan valiendo.
5. Si te preguntan por una feature de la 1.x a medias, elegí **Planificarla de nuevo** o **Ignorarla**.

Para ver qué entró en cada repo, `nxy legacy status`:

```
memoria de <repo> — importado el 2026-11-18 — 12 convenciones → aprendizajes A1..A12
review de <checkout> — importado el 2026-11-18 — 26 reviews, 88 elegidos
feature de la 1.x 3f9a1c2d en <carpeta> — re-planificada con la v2 — el 2026-11-18
filtro de <carpeta> — leídos 18432 bytes de 18432 bytes (última lectura el 2026-11-18)
```

Cada línea dice qué se importó y cuándo; una feature de la 1.x aparece como «re-planificada con la v2» o «ignorada», y los planes que la 1.x ya había cerrado se resumen en `N planes de la 1.x ya cerrados en la 1.x`. Si la última copia de la memoria falló, ves `memoria de <repo> — último error el <fecha>: <causa> (nxy legacy import lo reintenta)`. Sin nada importado: `nada importado de la 1.x todavía`.

## Comandos y config

| Comando | Qué hace |
| --- | --- |
| `nxy legacy status` | Qué entró en cada repo y qué contestaste de las features a medias |
| `nxy legacy import [--dir <carpeta>]` | Corre la importación a mano, para la carpeta actual o la que indiques. Lo ya importado dice `ya importado el <fecha>`; lo nuevo, `importado ahora` |
| `nxy mem promote A<n> --question "<q>"` | Vuelve regla un aprendizaje importado |
| `nxy note list` · `nxy note done <id>` | Las notas importadas |

Con `nxy legacy import` en un repo sin datos de la 1.x: `nada que importar: la 1.x no dejó memoria ni archivos de review o filtro para <carpeta>`. No hay claves de config para la importación: ocurre sola, una vez por repo.

## Relacionados

- [Memoria](memoria.md): aprendizajes, reglas y notas, y cómo promover.
- [Una feature, paso a paso](feature.md): el flujo nuevo.
- [Configuración](configuracion.md): las claves que la v2 sí lee.
- [Instalación](instalacion.md): actualizar el plugin.
- [Mensajes de nxy](mensajes.md): todos los avisos de esta página.
