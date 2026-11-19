# Primer uso

Qué hace nxy por su cuenta apenas lo instalás, qué te va a preguntar la primera vez y qué conviene probar primero.

## Para qué sirve

No tenés que aprender nada para aprovechar la mayor parte de nxy. Esta página te cuenta lo que pasa solo, para que no te sorprenda, y te da un orden para ir probando lo demás.

## Qué ves

nxy se engancha en los eventos de Claude Code (los hooks de `hooks/hooks.json`: inicio de sesión, antes y después de usar una herramienta, cada mensaje tuyo, fin de un subagente y fin de sesión). Cuando no hay nada que decir, no ves nada. Cuando frena o te avisa, ves un mensaje que empieza con `nxy:`.

La TUI, si la abrís en otra terminal, muestra todo lo que nxy sabe de tu repo. Probala sin datos: `nxy tui --demo`.

## Qué significa

### Lo que pasa solo

- **Los comandos salen filtrados.** Un `./mvnw test` que devolvía 84 líneas le llega a Claude en 8 (con [rtk](https://github.com/rtk-ai/rtk) instalado; sin él, pasan enteros y no se rompe nada). Si Claude necesita la salida completa, sabe pedirla ([El filtro](rtk.md)).
- **La statusline te avisa cuándo cortar.** Naranja o rojo en `turno` o `ctx` quiere decir que cada mensaje ya cuesta caro. Cortar es barato: el estado de la feature vive en la base de nxy, no en el chat.
- **El hilo principal no edita código por su cuenta.** Sin una feature activa, Claude no escribe archivos de código en el hilo principal: o arrancás una feature, o delega el cambio en un subagente (el *implementer*), que lee lo justo, edita, prueba y se descarta. Así los archivos no se quedan en el contexto caro de la sesión principal. Docs y texto pasan.
- **Las búsquedas amplias se delegan.** Con el contexto del hilo principal en 100k tokens o más, un `Grep`, `Glob` o `rg` sin apuntar a un archivo se frena y Claude manda un scout.
- **Las reglas llegan solas.** Las reglas de tu repo y tus reglas globales se le entregan a cada agente que las necesita, sin que hagas nada (ver [Memoria](memoria.md)).
- **Claude sabe dónde quedaste.** Al abrir una sesión (también después de `/clear`) con una feature sin terminar, Claude recibe el «para retomar»: el pedido, el paso, «lote N de M», lo último que falló y las decisiones.

### La primera pregunta: las reglas del repo

La primera vez que arrancás una feature en un repo que no tiene reglas guardadas, nxy pregunta:

> Este repo no tiene reglas guardadas. ¿Las propongo ahora? (~30–60k tokens, 1–2 min, una sola vez)

- **Sí**: un scout lee el build, los linters y el CI, y te propone hasta 10 reglas para que elijas cuáles guardar. Cuesta lo que dice el mensaje y se hace una sola vez por repo.
- **No**: arranca la feature igual, sin reglas. No vuelve a preguntar; si más adelante las querés, `/nxy:mem bootstrap`.

Una regla es una decisión del repo que las tareas futuras tienen que seguir («los montos son `BigDecimal`»). Con reglas, el planner y los implementers las respetan sin que se las repitas.

### Según el tamaño de la tarea

| Tarea | Qué hacés | Qué pasa |
| --- | --- | --- |
| **Puntual**: un cambio que ya está claro, en uno o dos archivos | La pedís como siempre | El hilo principal no edita código solo: Claude despacha un implementer con el `path:line` y el criterio de aceptación. Si de verdad conviene que edite él, `/nxy:gate once` deja pasar una edición ([El freno](gate.md)) |
| **Buscar algo**: no sabés dónde está el código | `/nxy:locate <pregunta>` | Un scout encuentra el `path:line` sin llenar tu sesión de archivos |
| **Con forma**: varios archivos, algo nuevo, o más de un repo | `/nxy:feature <pedido>` | Plan → tu aprobación → lotes verificados → tests afectados → review → arreglos elegidos |
| **Con decisiones de diseño**: querés decidir vos antes de que nadie planifique | `/nxy:engineer <pedido>` | Debatís cada bifurcación con Claude, guardás cada decisión, y recién ahí se planifica |

### Cuando la sesión se pone cara

Cada mensaje reenvía todo el contexto acumulado: con 200k encima, cada paso cuesta el doble que con 100k. Cuando la statusline se pone roja, hacé `/clear` y seguí. Si había una feature en curso no perdés nada: al abrir la sesión nueva Claude recibe el «para retomar» y escribir cualquier cosa alcanza para continuar (vas a ver `nxy: feature <id> pendiente (…) — escribí cualquier cosa para seguir`). La sesión nueva arranca con unos miles de tokens en vez de cientos de miles.

## Qué hacer

Un orden razonable para la primera semana:

1. **Mirá la TUI en modo demo.** `nxy tui --demo` (o `/nxy:tui` para el comando exacto). No toca tu repo.
2. **Pedí un cambio chico y real.** Con `/nxy:feature <pedido>`: leé el plan antes de aprobar (los `Accept:` son comandos que nxy va a correr), mirá los lotes y elegí qué hallazgos de la review arreglar. Está explicado paso a paso en [Una feature](feature.md).
3. **Contestá la pregunta de reglas** y revisá lo que quedó guardado con `/nxy:mem show`.
4. **Buscá algo** con `/nxy:locate <pregunta>` cuando no sepas dónde está el código.
5. **Mirá el gasto** con `/nxy:stats` al final de una sesión larga, y la statusline durante.

Si algo frena y no entendés por qué, el mensaje le dice a Claude qué hacer y casi siempre se resuelve solo. Si querés cortar una feature: `nxy feature stop`.

## Comandos y config

| Comando | Para qué |
| --- | --- |
| `/nxy:feature <pedido>` | Una feature con plan y lotes |
| `/nxy:engineer <pedido>` | Lo mismo, decidiendo el diseño antes |
| `/nxy:locate <pregunta>` | Dónde está algo (`path:line`) |
| `/nxy:mem show` | Reglas y aprendizajes de este repo |
| `/nxy:gate status` | Estado de los frenos de escritura y de lectura |
| `/nxy:stats` | La sesión actual, al detalle |
| `nxy status` | Dónde está la feature de esta carpeta |
| `nxy feature stop` | Termina la feature en cualquier punto |
| `nxy tui --demo` | La TUI con datos de ejemplo |

Casi nadie necesita tocar la config: los defaults están elegidos para funcionar sin calibrar ([Configuración](configuracion.md)).

## Relacionados

- [Instalación](instalacion.md)
- [Una feature, paso a paso](feature.md)
- [Plan y lotes](plan-y-lotes.md)
- [Memoria](memoria.md)
- [La TUI](tui.md) · [Decidir desde la TUI](decidir.md)
- [Buscar](buscar.md) · [Ver en qué se gasta](gasto.md)
- [Modo ingeniero](ingeniero.md) · [Varios repos y worktrees](varios-repos.md)
- [El freno](gate.md) · [El filtro](rtk.md) · [Mensajes de nxy](mensajes.md)
- [Configuración](configuracion.md) · [Si venís de la 1.x](si-venis-de-la-1x.md)
