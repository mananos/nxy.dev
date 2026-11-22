# Mensajes de nxy y qué hacer con cada uno

Cuando nxy frena algo o necesita algo tuyo, lo dice con un mensaje. Casi todos se resuelven solos: el texto le dice a Claude exactamente qué hacer. Esta página lista lo que vas a ver, qué significa y cuándo te toca actuar.

Los mensajes están citados tal cual salen. Los que empiezan con `nxy:` en inglés los lee Claude (llegan como rechazo de una herramienta o como contexto); los que están en español los ves vos. Cuando el texto lleva algo que cambia (`<id>`, `<archivo>`), va entre `<…>`.

## Dónde buscar

- [El freno](#el-freno)
- [El filtro de comandos](#el-filtro-de-comandos)
- [Una feature y sus preguntas](#una-feature-y-sus-preguntas)
- [Memoria y notas](#memoria-y-notas)
- [Contestar desde la TUI](#contestar-desde-la-tui)
- [Datos de la 1.x](#datos-de-la-1x)
- [Instalación y arranque](#instalación-y-arranque)
- [Comandos de terminal](#comandos-de-terminal)

## El freno

Más en [El freno](gate.md).

| Ves | Qué significa | Qué hacés |
| --- | --- | --- |
| `nxy: no feature is active — the main thread does not edit code <archivo> itself.` | Sin una feature activa, el hilo principal no edita código | Nada: Claude arranca una feature o despacha un implementer. Si de verdad conviene editar ahí: `/nxy:gate once` deja pasar **una** edición |
| `nxy: feature <id> is in progress — the main thread does not write code; …` | Hay una feature sin terminar: escribe el implementer, no el hilo principal (también por consola) | Nada. Para terminarla: `nxy feature stop` |
| `nxy: feature <id> works in a worktree — edit in <ruta>` | La feature trabaja en un worktree y se intentó editar el checkout original | Nada: Claude edita en el worktree |
| `nxy read gate: main thread context is 150k > 100k — a broad grep for <patrón> would pull its results into every later call.` | Una búsqueda amplia con la sesión cargada: la hace un scout | Nada. Para dejar pasar una: `/nxy:gate once search`; para apagar el freno de búsquedas: `/nxy:gate reads off` |
| `gate: la próxima edición de un archivo de código del hilo principal pasa sin bloquearse (vence en 5 min, <hora>).` | Armaste `/nxy:gate once` | Hacé la edición; se usa una sola vez |
| `gate: la próxima búsqueda amplia del hilo principal pasa sin frenarse (vence en 5 min, <hora>).` | Armaste `/nxy:gate once search` | Idem |
| `<ruta> está roto; no se toca` | El `config.json` donde iba a escribir (`gate reads`, `filter on/off`, `config set`) no es un JSON válido | Arreglalo a mano y repetí: nxy nunca reemplaza un archivo roto |

## El filtro de comandos

Más en [El filtro de comandos](rtk.md).

| Ves | Qué significa | Qué hacés |
| --- | --- | --- |
| `nxy: auto mode — nxy filters this command through rtk but cannot rewrite it for you here; run exactly this instead: rtk <comando>` | En auto mode nxy no puede reescribir el comando | Nada: Claude repite el comando con `rtk` adelante |
| `nxy: the permission mode is auto and nxy filters shell output through rtk. Start the Bash/PowerShell commands rtk filters …` | Al abrir una sesión en auto mode, nxy le pide a Claude que empiece esos comandos con `rtk` | Nada |
| `módulo de filtros: DESACTIVADO` | `modules.filter` está en `false` | `/nxy:filter on` si querés el filtro |
| `motor:  off (auto-no-rtk; config=auto)` | No hay rtk | `/nxy:setup` muestra el comando de instalación |
| `hook de rtk:   INSTALADO — nxy le cede la reescritura; corré rtk init --uninstall (o sacá el hook) …` | Corriste `rtk init -g` y rtk tiene su propio hook | Sacalo (`rtk init --uninstall`) para que lo maneje nxy |
| `[rtk] /!\ No hook installed — run rtk init -g` | Aviso de rtk, no de nxy | Ignoralo y **no** corras `rtk init -g`; nxy lo silencia solo |

## Una feature y sus preguntas

Más en [Una feature, paso a paso](feature.md), [Plan y lotes](plan-y-lotes.md) y [Review](review.md). Las preguntas aparecen en el chat o en la TUI ([Decidir desde la TUI](decidir.md)); el encabezado es la etiqueta corta que ves arriba de cada una.

| Ves | Qué significa | Qué hacés |
| --- | --- | --- |
| `feature <id> in progress (<estado>); next step …; to end it: nxy feature stop` | Ya hay una feature sin terminar en esta carpeta (hay una sola por checkout) | Seguí esa feature, o terminala con `nxy feature stop` |
| **¿Aprobás el plan `<id>` r`<n>` de nxy?** | Hay un plan esperando tu aprobación | Leelo (**Ver completo** lo abre entero) y elegí **Aprobar** o **Cambiar** |
| **nxy feature `<id>`: repo ocupado — ¿qué hacemos?** (encabezado «Repo ocupado») | Un repo que el plan necesita lo tiene otra feature | **Abrir en un worktree**, sacarlo del plan, o **Parar esta feature** |
| **nxy feature `<id>`: ¿en qué rama trabajamos?** / **¿desde qué base se crea la rama?** | Al aprobar, nxy pregunta la rama y su base de cada repo | Elegí; nxy crea la rama o el worktree |
| **Lote `<n>` fallido** (pregunta: «el lote `<n>` falló dos veces — ¿qué hacemos?») | El lote falló después del reintento automático | **Reintentar** si es del cambio; **Seguir** si ya fallaba antes; **Parar** para mirarlo vos |
| **Afectados fallidos** / **Afectados sin verificar** | Los tests afectados fallaron dos veces, o no hubo ninguno que correr | **Reintentar**; **Seguir** (queda marcado «sin verificar», nunca como verde); **Parar** |
| **nxy feature `<id>`: ¿qué hallazgos arreglamos?** | Hallazgos `H1`, `H2`… de la review | Marcá los que querés arreglar antes del PR, o ninguno. Los preexistentes son informativos |
| **Arreglos fallidos** | Los arreglos elegidos no quedaron en verde | **Reintentar**, **Seguir** o **Parar** |
| **nxy feature `<id>`: el planner no guardó un plan — ¿qué hacemos?** | El planner terminó sin guardar un plan | **Reintentar** o **Parar** |
| **nxy feature `<id>`: ¿actualizar los docs que nombran este cambio?** (**Actualizar docs** / **Dejar**) | Hay docs de usuario que nombran lo que cambió | **Actualizar docs** si querés que viajen en el mismo PR |
| **nxy feature `<id>`: ¿qué guardar como regla del repo? (1/2)** | Las decisiones `[repo]` del plan o del diseño pueden quedar como reglas | Marcá sólo las que valen para todo el repo; **Ninguna** no guarda nada |
| **nxy feature `<id>`: ¿pasar a engineer para decidir esto con vos?** (encabezado «Engineer») | El planner dejó preguntas abiertas | **Decidirlas con Claude** te pasa al [modo ingeniero](ingeniero.md), bifurcación por bifurcación; **Contestar las preguntas acá** las responde ahí mismo |
| **nxy feature `<id>`: ¿guardar la decisión D`<n>`?** / **¿cerrar el diseño y planificar?** | Preguntas del modo ingeniero | Contestá en el chat: estas dos no se contestan desde la TUI |
| **nxy feature `<id>`: ¿qué opción elegís para `<bifurcación>`?** | Una bifurcación del diseño | Elegí en el chat o en la pantalla Ingeniero de la TUI |
| `nxy: feature <id> pendiente (…) — escribí cualquier cosa para seguir` | Hiciste `/clear` con una feature a medias | Escribí cualquier cosa y nxy retoma |
| `nxy: feature <id> — los reviewers siguen corriendo; su resultado llega cuando terminan` (o `… el implementer sigue corriendo …`) | Un agente trabaja en segundo plano | Nada: el resultado llega solo, una sola vez |
| `nxy: feature <id> — 2 de 4 lentes listas; esperá al resto, nxy trae el resultado cuando terminen.` | La review va por lentes en paralelo | Nada |
| `nxy: the user stopped feature <id> from the TUI` | Paraste la feature con `x` en la TUI | Nada |
| `nxy: no arrancó ninguna feature. /nxy:feature (o /nxy:engineer) solo arranca si va al principio del mensaje …` | Escribiste el comando en una línea que no es la primera de tu mensaje | Mandalo de nuevo con el comando al principio |
| `Solo arranca tipeado por el usuario, no por un Skill. Decile … que escriba /nxy:feature <pedido> …` | Claude intentó arrancar la feature por su cuenta (Skill) y nxy lo rechazó | Escribí `/nxy:feature <pedido>` vos |
| **Aprobación escrita en el chat** | Si en vez de usar el diálogo respondés «aprobado» en el chat, nxy lo toma como aprobación del plan | Nada; si querés cambios, decilos en el chat |
| **Afectados fallidos**: una línea por paquete que falló | nxy muestra cada paquete de los tests afectados que falló, no solo el total | Leelas antes de elegir **Reintentar**, **Seguir** o **Parar** |
| `✔ arreglo` en la línea «Listo» | Un criterio que falló al principio y lo arregló un fix de la review | Nada: cuenta como verde |
| `nxy: no hay una feature activa en este repo` · `no hay una feature activa` | `nxy feature stop` (o `pause`) y `nxy status` sin una feature en la carpeta | Nada, o arrancá una con `/nxy:feature` |

Los estados que muestran `nxy status` y la TUI: **Trabajando** (un agente hace su parte), **Verificando** (corre un test o una review), **Necesita tu decisión** (te toca) y **Listo** (terminó, o la paraste).

Si un `Accept:` de un lote falla en Windows con algo como `test -f …`: el planner escribe esos comandos para `sh`; sin `sh` en el PATH se usa `cmd`, donde ese comando no existe. Lo ves como una falla con la salida, no escondida. Usá Git Bash (con `sh` en el PATH) o pedí cambiar el `Accept:` del plan.

## Memoria y notas

Más en [Memoria](memoria.md).

| Ves | Qué significa | Qué hacés |
| --- | --- | --- |
| **Este repo no tiene reglas guardadas. ¿Las propongo ahora? (~30–60k tokens, 1–2 min, una sola vez)** | Es la primera feature de un repo sin reglas | **Sí**: un scout lee el build, los linters y el CI y te propone hasta 10 reglas para elegir. **No**: sigue la feature sin reglas |
| **nxy: ¿Con cuál seguimos?** (encabezado «Notas») | Dejaste notas a medias en esta carpeta | Elegí una, o **Ninguna, es otra cosa** |
| `nxy: that message sounds like a repo rule. Ask the user whether to save it; …` | Con una feature activa escribiste «siempre», «nunca» o «de ahora en adelante» | Contestá la pregunta de Claude; nxy nunca guarda una regla sin que confirmes |
| **Stop hook error** (rótulo de Claude Code) | Claude Code lo pone cuando nxy te entrega un paso al terminar el turno (un checkpoint del planner o de un implementer en segundo plano) | No es un error y no hacés nada: es el rótulo de Claude Code y nxy no puede cambiarlo |
| `no hay nada que cambiar: pasá --text, --question, --paths o --roles` | `nxy mem edit` de una regla sin ningún cambio | Agregá lo que querés cambiar |

## Contestar desde la TUI

Más en [Decidir desde la TUI](decidir.md) y [La TUI](tui.md).

| Ves | Qué significa | Qué hacés |
| --- | --- | --- |
| `nxy: si tenés la TUI abierta, contestá ahí` (debajo del hook) y el título `● nxy: decisión pendiente` | Hay una pregunta de nxy y tenés la TUI abierta: se contesta ahí | Contestá en la TUI, o `t` para que vuelva al diálogo normal |
| `ya contestada` | La contestó otra TUI (o el diálogo) un instante antes | Nada: la pantalla se recarga |
| `No hay decisiones pendientes.` · `contestá en la terminal de Claude Code` · `ya no está esperando: contestá en la terminal de Claude Code` | La ventana de decisión no tiene nada, la pregunta no esperaba a la TUI o el hook ya la soltó | Ver [Decidir desde la TUI](decidir.md): si te manda a la terminal, contestá en el chat |
| `nxy action decide is the user's answer from the TUI; ask with AskUserQuestion` | Claude intentó contestarse solo una pregunta; nxy lo rechaza | Nada: la respuesta es siempre tuya |
| `nxy se actualizó: cerrá y volvé a abrir` (arriba en la TUI) | Actualizaste nxy con la TUI abierta | Cerrala y abrila de nuevo |
| `nxy tui: la db es de un nxy más nuevo: abrí la TUI de esa versión` | La base la escribió un nxy más nuevo que este binario | Actualizá nxy o abrí la TUI de la versión nueva |

## Datos de la 1.x

Más en [Si venís de la 1.x](si-venis-de-la-1x.md).

| Ves | Qué significa | Qué hacés |
| --- | --- | --- |
| `nxy: datos de la 1.x importados — 12 convenciones → aprendizajes A1..A12 · …` | Primera sesión de un repo con datos de la 1.x: nxy los copió | Nada; revisá los aprendizajes y promové los que sigan valiendo |
| `nxy: la importación de la 1.x falló (…); probá nxy legacy import` | La copia falló | `nxy legacy import` la reintenta; `nxy legacy status` muestra el último error |
| `nxy: quedó a medias una feature de la 1.x en este repo: «<objetivo>» (…)` | La 1.x dejó un plan aprobado sin terminar | Elegí **Planificarla de nuevo** o **Ignorarla** |
| `nxy: la 1.x de nxy sigue activada: desactivala mientras probás la v2 (las dos responden a los mismos hooks)` | Hay una instalación de nxy 1.x habilitada además de esta | Desactivala con `/plugin` |

## Instalación y arranque

Más en [Instalación](instalacion.md#si-algo-no-anda).

| Ves | Qué significa | Qué hacés |
| --- | --- | --- |
| `nxy: <causa> - nxy hooks do nothing until this is fixed` | El lanzador no pudo bajar o correr el binario | Mirá la causa en [Si algo no anda](instalacion.md#si-algo-no-anda) |
| `nxy: curl not found, cannot download the binary` · `nxy: download of nxy_… failed` | Sin `curl`, sin red, o el release todavía no existe | Instalá `curl` o revisá la red y abrí una sesión nueva; o compilalo vos |
| `nxy: checksum mismatch for …` | El archivo bajado no coincide con el SHA-256 del release | Probá de nuevo; si sigue, compilalo |
| `nxy: downloading its binary in another hook - this hook was skipped` | Otro hook se está bajando el binario; baja uno solo | Nada: es de una sola vez |
| `nxy: could not run <ruta> (exit …)` · `nxy: binary not found at <ruta>` | El binario no está o no corre; en Windows suele ser el antivirus | [Windows Defender](instalacion.md#windows-defender) |
| `nxy ?` en la statusline | La statusline no pudo leer algo | Reintentá; con `NXY_DEBUG=1` ves la causa por stderr |

## Comandos de terminal

| Ves | Qué significa | Qué hacés |
| --- | --- | --- |
| `no conozco la acción "<x>". Usá: status \| once [search] \| reads on\|off [--repo]` | Una acción de `nxy gate` que no existe | Usá una de la lista |
| `no conozco la acción "<x>". Usá: status \| on \| off` | Lo mismo para `nxy filter` | Idem |
| `uso: nxy config [show] \| set <clave> <valor\|default> [--repo] \| cache-ttl 1h\|off` | Argumentos de `nxy config` incorrectos | Mirá [Configuración](configuracion.md) |
| `campo desconocido: <clave>` · `valor inválido para <clave>: <valor>` | `nxy config set` con una clave o un valor que no acepta | `nxy config show` lista las claves válidas |
| `nxy: no encontré la sesión (<selector>) para <carpeta>. Probá con --all o --session <id>.` | `nxy stats` no halló esa sesión | Probá `--all` o pasá el id |
| `nxy: --by tiene que ser day \| week \| session \| model \| agent` | `nxy trend --by` con otro valor | Usá uno de los cinco |
| `nada que importar: la 1.x no dejó memoria ni archivos de review o filtro para <carpeta>` | `nxy legacy import` en un repo sin datos de la 1.x | Nada |

## Relacionados

- [Instalación](instalacion.md) · [El freno](gate.md) · [El filtro](rtk.md) · [Memoria](memoria.md)
- [Decidir desde la TUI](decidir.md) · [Si venís de la 1.x](si-venis-de-la-1x.md) · [Configuración](configuracion.md)
