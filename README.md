<p align="center">
  <img src="resources/nxydev.png" alt="nxy.dev" width="320">
</p>

<p align="center">Plugin de Claude Code para gastar menos tokens sin perder calidad, y para ver en qué se va cada dólar.</p>

nxy hace cuatro cosas:

- **Te muestra lo que gastás.** Una statusline con el costo del turno y de la sesión, y reportes por sesión, modelo, subagente y día. Los números salen del `usage` real que Claude Code guarda en sus transcripts: no son estimaciones.
- **Achica lo que Claude lee.** Las salidas de tests, builds y `git` pasan filtradas por [RTK](https://github.com/rtk-ai/rtk). Cuando la sesión ya está cargada, las búsquedas y las ediciones las hacen subagentes baratos que se descartan al terminar.
- **Le pone método a los cambios grandes.** Primero un plan que aprobás vos, después cada lote verificado contra un test, una review al final y los arreglos que vos elijas.
- **Recuerda.** Las decisiones y las convenciones del repo, y el punto exacto de cada tarea (el *handoff*), sobreviven al `/clear` y se pueden compartir por git.

Funciona con cualquier lenguaje, en Windows, Linux/WSL y macOS, con Node 22.13+ y sin dependencias.

## Contenido

- [Instalación](#instalación)
- [Cómo se usa](#cómo-se-usa)
- [Mensajes de nxy y qué hacer con cada uno](#mensajes-de-nxy-y-qué-hacer-con-cada-uno)
- [Comandos](#comandos)
- [La statusline](#la-statusline)
- [Ver en qué se gasta](#ver-en-qué-se-gasta)
- [La memoria](#la-memoria)
- [Configuración](#configuración)
- [Si algo no anda](#si-algo-no-anda)
- [Qué le agrega nxy a tu sesión](#qué-le-agrega-nxy-a-tu-sesión)
- [Estado del proyecto](#estado-del-proyecto)
- [Desarrollo](#desarrollo)

Cómo funciona cada pieza por dentro: [`docs/como-funciona.md`](docs/como-funciona.md). Todas las opciones: [`docs/configuracion.md`](docs/configuracion.md).

## Instalación

### 1. Requisitos

Lo único obligatorio es **Node 22.13 o más nuevo** en el PATH (`node -v`). Con eso ya funcionan las métricas, la statusline, la memoria, el plan y la review. (La memoria, el handoff y el plan usan la base SQLite que trae Node desde 22.13; con una versión anterior esas partes no hacen nada, sin romper el resto.)

Opcionales, que suman ahorro:

| Herramienta | Para qué | Sin ella |
| --- | --- | --- |
| [rtk](https://github.com/rtk-ai/rtk) | Filtra la salida de los comandos (tests, builds, git, logs) | Los comandos pasan enteros; nada se rompe |
| [ripgrep](https://github.com/BurntSushi/ripgrep) (`rg`) | Búsqueda de texto del scout; rtk también lo usa | El scout busca más lento y rtk recorta menos |
| [codegraph](https://github.com/colbymchenry/codegraph) | "Quién llama a qué" para el scout | El scout usa el índice propio de nxy y `rg` |

```
# Windows
winget install rtk-ai.rtk
winget install BurntSushi.ripgrep.MSVC

# Linux / WSL (Debian/Ubuntu)
sudo apt install ripgrep
curl -fsSL https://raw.githubusercontent.com/rtk-ai/rtk/refs/heads/master/install.sh | sh

# macOS
brew install ripgrep rtk
```

En Linux, rtk queda en `~/.local/bin`; si no está en el PATH, nxy igual lo encuentra y te lo avisa. Verificá con `rtk --version` y `rg --version`.

codegraph (opcional) se instala así, y después se inicializa una vez en cada repo donde lo quieras:

```
# Windows (PowerShell)
irm https://raw.githubusercontent.com/colbymchenry/codegraph/main/install.ps1 | iex

# Linux / WSL / macOS
curl -fsSL https://raw.githubusercontent.com/colbymchenry/codegraph/main/install.sh | sh

# en la raíz de cada repo
codegraph init
```

> **No corras `rtk init -g` ni `codegraph install`.** nxy ya instala su propio hook y usa codegraph por CLI desde adentro de un subagente. Si los conectás por su lado, rtk y nxy se pisan, y codegraph le suma su descripción a *cada* turno de Claude.

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
/nxy:statusline --apply
```

La escribe en `~/.claude/settings.json` (con backup) y se actualiza sola con cada versión nueva del plugin. Reiniciá Claude Code para verla.

### 4. Verificá

| Comando | Tiene que decir |
| --- | --- |
| `/nxy:filter status` | `engine: rtk` (o por qué está apagado y cómo instalarlo) |
| `/nxy:locate --status` | Qué buscadores tenés: índice, `rg`, codegraph |
| `/nxy:mem status` | Dónde está la base y cómo identificó este proyecto |

## Cómo se usa

### Lo que pasa solo

No tenés que aprender nada para aprovechar la mayor parte. Con nxy instalado:

- **Los comandos salen filtrados.** Un `./mvnw test` que devolvía 84 líneas le llega a Claude en 8. Si Claude necesita la salida completa, sabe pedirla.
- **La statusline te avisa cuándo cortar.** Naranja o rojo en `turno` o `ctx` quiere decir que cada mensaje ya cuesta caro (ver [La statusline](#la-statusline)).
- **Con la sesión cargada, Claude delega.** Pasados los 100k tokens de contexto, Claude deja de editar él mismo y le pasa el cambio a un subagente (el *implementer*), que lee lo justo, edita, corre el test y se descarta. Así los archivos no se quedan en el contexto caro de la sesión principal.
- **Aparecen tus decisiones.** Si guardaste una decisión que tiene que ver con lo que pedís, Claude recibe un puntero (una línea) y la carga si le sirve.
- **Claude sabe dónde quedaste.** Al abrir una sesión en una rama con trabajo a medias, Claude recibe una línea con el *handoff* de esa rama.

### Según el tamaño de la tarea

| Tarea | Qué hacés | Qué pasa |
| --- | --- | --- |
| **Chica**: uno o dos archivos, está claro qué cambiar | La pedís como siempre | Claude edita directo mientras la sesión esté liviana |
| **Mediana**: algunos archivos, nada que decidir | La pedís como siempre. Si no sabés dónde está el código: `/nxy:locate <pregunta>` | Un scout (Haiku) encuentra el `path:line` y un implementer por cambio escribe |
| **Grande**: varios archivos, una decisión de diseño, algo nuevo o más de un repo | `/nxy:feature <pedido>` | Plan → tu aprobación → lotes verificados → suite → review → arreglos elegidos |

Si no estás seguro, usá `/nxy:feature`: Claude arranca diciendo qué tamaño le asignó y por qué, y sigue la receta de ese tamaño.

### Una tarea grande, paso a paso

```
/nxy:feature alta de clientes corporativos con validación de CUIT
```

1. **Claude decide el tamaño** y, si es grande, despacha al *planner* (Sonnet, effort high), que busca en el repo, reutiliza lo que existe y arma un plan por lotes, cada uno con su comando de verificación (`Accept:`).
2. **Preguntas (a veces).** Si el plan depende de algo que sólo vos sabés ("¿endpoint nuevo o extender el existente?"), te lo pregunta con opciones (`Plan Q1`, `Plan Q2`…). La primera es la recomendada.
3. **Aprobás el plan.** Claude te lo muestra y te pregunta **Approve** o **Change**. Hasta que elijas Approve no se escribe una línea de código. Si hubo preguntas, también te ofrece guardar tus respuestas como **convenciones del repo**, para que la próxima vez no pregunte.
4. **Lotes verificados.** Un implementer por lote. Después de cada uno ves `nxy verify: batch 1 ✔` o `✘`. Con un ✘ elegís **Retry**, **Continue anyway** o **Stop**. Los lotes que no dependen entre sí (un repo de back y uno de front) corren en paralelo. Si un agente corre en segundo plano, al lanzarlo sólo ves "lanzado en segundo plano": el veredicto llega **cuando el agente termina**, no al lanzarlo, y hasta entonces nxy no deja despachar lo que depende de él. No tenés que hacer nada: esperá el aviso.
5. **Suite completa.** Con todo en verde, un *tester* (Haiku) corre una vez la suite de cada repo tocado y te dice sólo lo que falló. Si la suite sale en rojo, un único implementer la arregla en un lugar propio (`Suite fix — <qué falló>`); nxy lo verifica volviendo a correr los comandos que fallaron y recién después pasa a la review. La review no arranca mientras la suite sigue corriendo.
6. **Review.** Un *reviewer* (Sonnet) mira únicamente lo que cambió el plan, con lentes elegidas por el tipo de archivo (persistencia, API, frontend, base de datos, seguridad). Ves los hallazgos numerados (`R1 high …`) y **elegís cuáles se arreglan**. Si el reviewer vuelve sin registrar la review, nxy lo manda de vuelta una vez y, si falla otra vez, te lo muestra como una falla. Para leer el cambio, el reviewer lo recibe como un archivo (`.nxy/local/baseline/<rama>/review-packet.md`, unos 130 KB para un diff de 2.000 líneas; se reemplaza en cada review y se borra con `mem handoff done`) y lo lee por partes; vos no tenés que abrirlo (`review.mjs packet --full` lo imprime entero si lo querés ver). Un archivo marcado `(git)` lo cambió el plan sin que nxy tuviera una copia: se compara contra HEAD, en modo lectura (nxy nunca escribe dentro de `.git`). La sección `## Not reviewed` nombra los archivos que ya tenías modificados antes de aprobar y no tienen copia: nxy no puede separar tu parte de la del plan, así que los nombra en vez de adivinar. Si te importan, mirá ese diff vos. Si algún documento para usuarios (README, un `.md` en la raíz, o lo que esté en `docs/`, `doc/`, `documentation/` o `wiki/`) nombra lo que cambió, te ofrece **Update docs**, con los 3 primeros y "+N más"; los prompts de agentes y skills nunca se ofrecen.
7. **Cierre.** Claude te resume qué hizo, qué se verificó y qué quedó. Vos mirás el diff, commiteás y abrís el PR: **nxy nunca commitea ni pushea**.
8. **Cuando la rama se mergea**, decile a Claude "cerrá el handoff" (`mem handoff done`): deja de aparecer al abrir sesiones, pero sigue buscable.

Los subagentes no pueden escribir archivos del proyecto por consola: un hook les deniega `sed -i`, las redirecciones, `tee` y los heredocs hacia archivos del proyecto, y el agente reintenta con Edit/Write. No ves nada ni tenés que hacer algo. El hilo principal no se ve afectado, y `cp`/`mv` y los archivos temporales siguen permitidos.

Qué te toca a vos en todo el proceso: contestar las preguntas, leer el plan como un diseño en un PR (¿reusa lo que existe?, ¿cada `Accept:` prueba lo correcto?) y elegir qué hallazgos arreglar.

### Una tarea en varios repos

Abrí Claude Code en un repo y nombrá en el pedido las rutas de los otros. El plan trae una sección `### Repos` con cada uno y su rol (api, web, lib compartida).

- **Qué ves.** Al aprobar, nxy anota **una sola vez por tarea** qué archivos ya estaban modificados en cada repo. Un plan revisado no repite la foto; sólo toma la de un repo que el plan nuevo suma. Se borra con `mem handoff done`. La review muestra los archivos como `<repo>/<ruta>`; si dos repos se llaman igual, nxy agrega carpetas hasta distinguirlos (`clientX/api`, `clientY/api`), sin configurar nada. Las convenciones, lentes y docs de cada repo tocado se aplican.
- **Qué significa.** Si se edita un repo que el plan no declaró, sale el aviso `<repo> is not in the plan; its prior uncommitted changes cannot be told apart` y sus cambios previos quedan en `## Not reviewed`.
- **Qué hacés.** Declaralo en `### Repos` y volvé a aprobar.

Regla del gate: sólo quedan libres los archivos fuera del proyecto y de todo repo git (el scratchpad, la memoria de Claude). Los de `.nxy/` siguen con gate porque son archivos tuyos que se commitean. `/nxy:gate once` ahora también deja pasar una edición después de `mem handoff done`.

### Cambios hechos fuera de Claude (scripts, generadores, proyectos sin git)

- **Qué ves.** Al aprobar, nxy copia los archivos que el plan nombra (hasta 200) y, en carpetas sin git, anota una lista liviana: ruta, tamaño, fecha y huella del contenido de cada archivo. Saltea `node_modules`, `target`, `dist`, `build`, `.git`, `.nxy` y similares; los archivos por encima del límite de copia sólo llevan tamaño y fecha. La lista se toma una vez por tarea, al primer plan aprobado; un plan revisado no la rehace. Con git no se hace inventario: git ya lo cubre.
- **Qué significa.** La review marca esos archivos `(inventory)`: los nuevos se revisan enteros; los modificados dicen "changed outside the plan's copies; no before" (no hay un antes: el revisor los lee y avisa si parecen fuera del plan). Un archivo que sólo cambió de fecha o se reescribió igual (`touch`, guardar sin cambios) no aparece. La sección `## Coverage` dice qué cubrió cada repo y qué no: carpetas salteadas, archivos grandes que cambian sin tocar tamaño ni fecha, más de 20.000 archivos (`too many files`, sólo copias).
- **Qué hacés.** Si algo importante queda sin cubrir, miralo a mano o nombrá el archivo en el plan.
- **Costo.** Al aprobar se lee cada archivo una vez (0,3 a 1 s para 5.000 archivos / ~30 MB); al revisar sólo se recorre y se lee lo que cambió de tamaño o fecha (0,1 a 0,3 s para 5.000 archivos). Unos 110 bytes por archivo. Se borra con `mem handoff done`.

### Cuando la sesión se pone cara

Cada mensaje reenvía todo el contexto acumulado: con 200k encima, cada paso cuesta el doble que con 100k. Cuando la statusline se pone roja:

1. Decile a Claude **"guardá el handoff"**: escribe en ~20 líneas qué se hizo, qué falta, qué archivos importan y qué se decidió.
2. `/clear`.
3. En la sesión nueva decí **"seguimos"**. Claude ya tiene la línea del handoff y lo carga.

La sesión nueva arranca con unos miles de tokens en vez de cientos de miles, sin perder el hilo. Si hay un plan en curso, el plan y los lotes verificados también pasan.

nxy te ayuda a no olvidarte: si la rama tiene handoff y Claude editó algo después de la última vez que se guardó, no termina el turno sin actualizarlo. Y en las pausas naturales (un lote en verde, la review hecha) te sugiere una vez cortar si el contexto pasó los 100k. Nunca corta solo.

### Buscar sin leer medio repo

```
/nxy:locate ¿dónde se decide si un comando pasa por rtk?
```

Un scout (Haiku) busca primero con herramientas que no gastan tokens (el índice de símbolos de nxy, `rg` y, si está, codegraph), confirma con dos o tres lecturas y devuelve hasta 5 `path:line` con tres líneas de código cada uno. Todo lo que leyó muere con él.

Usá el `path:line` directamente: si volvés a abrir los archivos enteros "para verificar", pagás lo que el comando evitó. Si no encuentra nada, te dice qué términos probó; suele significar que el código usa otras palabras que el dominio ("anular turno" vs. `cancelBooking`).

El índice se arma solo la primera vez, se actualiza sólo con lo que cambió y entiende JS/TS, Java, Scala, Python, Go y C#.

## Mensajes de nxy y qué hacer con cada uno

Cuando nxy frena algo, el mensaje le dice a Claude exactamente qué hacer, así que casi siempre se resuelve solo. Esto es lo que vas a ver y cuándo te toca actuar:

| Ves | Qué significa | Qué hacés |
| --- | --- | --- |
| `nxy: main thread context is 150k > 100k — do not edit … yourself` | La sesión está cargada; esa edición la hace un implementer | Nada. Si de verdad conviene editar ahí (una línea, sesión por cerrar): `/nxy:gate once` deja pasar la próxima edición |
| `… has no handoff yet. Save one before starting write work` | Sesión cargada y la rama no tiene handoff: Claude lo guarda antes de seguir | Nada; pasa una vez por rama |
| Pregunta `Plan Q1` / `Plan Q2` | El plan necesita una decisión tuya | Contestá. Si te da lo mismo, la recomendada |
| Pregunta **Approve nxy plan …?** | Hay un plan esperando tu aprobación | Leelo y elegí Approve o Change |
| **Save as repo conventions?** | Tus respuestas pueden quedar como reglas del repo. Con una sola decisión la pregunta es "Save as convention / Don't save"; con 2 a 4 se marcan las que valen; con más de 4 se reparten en varias preguntas de hasta 4 | Guardá sólo las que valen para todo el repo ("DTOs como records"), no las de esta tarea |
| `<repo> is not in the plan; its prior uncommitted changes cannot be told apart` | Se editó un repo que el plan no declaró: sus cambios previos quedan en `## Not reviewed` | Declaralo en `### Repos` del plan y volvé a aprobar |
| `warning: <ruta> does not exist` / `is not a git repo; its changes are not reviewed through git` | Un repo declarado en el plan no existe o no tiene git | Corregí la ruta; sin git, la revisión se apoya en copias e inventario |
| `copied the "before" of N file(s) the plan names` | Al aprobar, nxy copió los archivos que el plan nombra para poder compararlos después | Nada |
| Sección `## Coverage` en la review | Dice qué cubrió cada repo sin git y qué no (`too many files`: más de 20.000 archivos, sólo copias) | Si algo importante queda sin cubrir, miralo a mano o nombrá el archivo en el plan |
| `nxy verify: batch 2 ✘ … failed after the last edit` | El test del lote falló después del último cambio | **Retry** si es del cambio; **Continue anyway** si ya fallaba antes; **Stop** para mirarlo vos |
| `nxy verify: batch 3 – manual` | Ningún comando prueba ese lote (algo visual) | Miralo vos; el plan dice qué mirar |
| Pregunta **Review** con `R1`, `R2`… | Hallazgos de la review de lo que cambió el plan | Marcá los que querés arreglar antes del PR. Los *preexisting* son informativos |
| Pregunta **Docs** | Hay `.md` en el repo que nombran lo que cambió | **Update docs** si querés que viajen en el mismo PR |
| `plan … is approved, so its work goes through implementers` | Con plan aprobado, Claude no edita directo | Nada. Si querés algo fuera del plan ahí mismo: `/nxy:gate once` |
| `this session edited files after the handoff … was last saved` | Claude actualiza el handoff antes de terminar el turno | Nada; es una vez por cambio |
| `the main thread carries 160k tokens… a good point to cut` | Pausa natural con la sesión cara | Si te parece, "guardá el handoff" → `/clear` → "seguimos" |
| `nxy memory — possibly relevant to this message` | Hay decisiones guardadas que pueden aplicar | Nada; Claude carga la que le sirva |
| `this branch's plan was already reviewed` | Estás corrigiendo algo en una rama ya revisada | Si es algo que la review debió ver, dejá que Claude lo guarde como convención o lente (`/nxy:review escape`) |

## Comandos

| Comando | Para qué |
| --- | --- |
| `/nxy:feature <pedido>` | Arranca un cambio con la receta por tamaño; si es grande, con plan |
| `/nxy:locate <pregunta>` | Encuentra dónde está algo y devuelve `path:line`, sin llenar tu sesión de archivos. `--status`: qué buscadores hay |
| `/nxy:stats` | La sesión actual al detalle: tokens, costo, forma, por modelo, subagente y skill. `last`, `<id>`, `--all`, `--json` |
| `/nxy:trend` | Evolución por día, semana, sesión o modelo, de todos tus proyectos. `--since 30d`, `--by week\|session\|model`, `--here`, `--json` |
| `/nxy:mem …` | La memoria: `save`, `search`, `get`, `list`, `delete`, `export`, `import`, `status`, y `handoff` (`show`, `done`). Ver [La memoria](#la-memoria) |
| `/nxy:gate` | El freno de escritura: `status`, `once` (dejar pasar la próxima edición), `on`, `off` (por repo) |
| `/nxy:review` | `status` (hallazgos por lente en 30 días), `show` (la última review), `escape "<regla>" --as convention\|lens` (algo que la review no vio) |
| `/nxy:filter` | El filtro de comandos: `status`, `on`, `off` (por repo) |
| `/nxy:statusline` | Instala la statusline (`--apply`) o imprime el snippet para pegarlo a mano |

Para ver un reporte tal cual, sin que Claude lo retipee ni gaste tokens, usá el prefijo `!` de Claude Code: `! node "<ruta-al-plugin>/hosts/claude-code/entries/stats.mjs"`. También corren fuera de Claude: `node hosts/claude-code/entries/trend.mjs --by week --since 90d`.

## La statusline

Una línea siempre visible que responde tres preguntas: **¿cuánto me cuesta lo que Claude hace ahora?**, **¿cuándo conviene cortar?** y **¿qué pasa si dejo la sesión un rato?**

```
◆ nxy ⟡ my.app main ⟡ Opus 5 · high ⟡ ctx ▰▰▰▱▱▱▱▱ 41% · 82k ⟡ turno $0.42~ ⟡ sesión $34.74~ · 44M→241k · +4 agents ⟡ cache 98% · 52m ⟡ 5h 62% · 7d 31%
```

| Segmento | Qué es | Qué hacer con eso |
| --- | --- | --- |
| `my.app main` | Carpeta y rama | Confirmar que estás donde creés, sobre todo con varias sesiones abiertas |
| `Opus 5 · high` | Modelo y effort | Si es una tarea chica con Opus/high, cambialo al principio de la sesión: cambiar de modelo a mitad de camino reescribe toda la cache |
| `ctx … 41% · 82k` | Contexto que se reenvía en cada llamada, en **tokens reales**. Naranja desde 100k, rojo desde 200k | Naranja: no arranques nada grande. Rojo: handoff + `/clear`. Mirá los tokens, no el porcentaje: 41 % de 1M son 410k |
| `turno $0.42~` | Lo que va costando el turno actual, subagentes incluidos. Naranja desde $1, rojo desde $3 | **La señal más importante.** Un turno de $3 no es "trabajó mucho": es que cada llamada arrastra demasiado contexto |
| `sesión $34.74~ · 44M→241k · +4 agents` | El odómetro: costo total, tokens de entrada → salida, subagentes | Comparar con lo que hiciste. El detalle, en `/nxy:stats` |
| `cache 98% · 52m` | Cuánto de lo enviado ya estaba en cache (se paga a un décimo) y cuánto le queda de vida | Si baja de golpe, algo cambió en el contexto. Los minutos dicen cuánto podés ausentarte sin costo |
| `❄386k $3.86~` | La cache **venció**: el próximo mensaje reescribe 386k tokens y eso cuesta $3.86 antes de hacer nada | Buen momento para preguntarte si seguís o arrancás una sesión nueva con el handoff: cuesta lo mismo y arranca liviana |
| `5h 62% · 7d 31%` | Consumo de las ventanas de tu suscripción | Si vas al 90 % de la de 5 h, lo pesado puede esperar |

El `~` quiere decir *equivalente en USD*: con suscripción no pagás por token, pero es lo que ese uso costaría con API key.

Un `*` después de un monto (en la statusline, `/nxy:stats` y `/nxy:trend`) quiere decir que un modelo todavía no está en la tabla de precios de nxy y se calculó con la versión más cercana de la misma familia (por ejemplo, un futuro `claude-sonnet-5-6` con las tarifas de `claude-sonnet-5-5`); una línea de nota nombra el modelo y la base usada. Un `+?` quiere decir que el modelo es de una familia desconocida y no se pudo calcular. No tenés que hacer nada: el número se vuelve exacto cuando un release de nxy actualiza la tabla. Sonnet 5.5 ya tiene precio exacto.

Los umbrales son en tokens y no en porcentaje porque lo que encarece cada paso es cuánto se reenvía, no qué fracción de la ventana ocupa. Colores, presets (`vivid`, `classic`, `powerline`) y layout (`line`, `two-line`) se cambian en la [configuración](docs/configuracion.md#statusline) y se ven al instante.

## Ver en qué se gasta

`/nxy:stats` explica una sesión y `/nxy:trend` muestra cómo evolucionás. Tres cosas para mirar:

**1. La forma de la sesión.** El costo se explica con dos números: cuánto contexto lleva cada llamada y cuántas llamadas hace cada turno.

```
shape: 21 calls/turn (main 21) · main context avg 111k/call · peak 158k · subagents 0% of tokens
```

| Dato | Qué hacer si está alto |
| --- | --- |
| `calls/turn` | 20–30 es Claude explorando o editando solo. No es malo, pero 30 llamadas × 130k son 4M de tokens en un turno: pedí tareas más acotadas o delegá la exploración |
| `main context avg` | Por encima de ~100k cada paso cuesta el doble que al principio: es la señal de cortar y seguir |
| `subagents %` | En 0 % en sesiones grandes, el principal hace todo con el contexto a cuestas. Con nxy este número tiene que subir |

**2. Por qué se enfrió la cache.** Casi todo lo que pagás es contexto leído de la cache con 90 % de descuento. Lo caro es cuando vence y hay que reescribirlo:

```
cache rebuilds by cause: idle 6 · 1.8M · $17.33  |  subagent 2 · 420k · $4.10  |  start 1 · 182k · $1.14
cache TTL written: 5m ×3 · 1h ×6  ·  avoidable by warming (idle + subagent): $21.43 (6.4% of cost)
```

`idle` es una pausa más larga que la vida de la cache; `subagent`, el principal esperando a un subagente; `compact` y `start` son esperables. `cache TTL written` te dice si tu cuenta usa 5 minutos o 1 hora.

Si tu cuenta usa 5 minutos (API key, Bedrock, créditos extra), aparece una línea más con la cuenta hecha sobre tus propias sesiones:

```
cache TTL 1h what-if (main thread): $3.02 → $1.65 (-45.3%), 5 rebuilds would have been reads · worth it: set "promptCacheTtl": "1h" in ~/.claude/settings.json
```

Con una cache de 1 hora cada escritura cuesta más (2× el input en vez de 1,25×), pero las pausas de 5 a 60 minutos dejan de reescribir todo. nxy hace las dos cuentas y te dice cuál conviene: **worth it** (poné esa línea en `~/.claude/settings.json`; necesita Claude Code 2.1.242 o más nuevo), **about the same** o **not worth it here**. Con suscripción la sesión principal ya usa 1 hora y la línea no aparece.

**3. Cuánto ahorró el filtro.** `rtk saved` son los tokens que rtk recortó de las salidas de Bash. El ahorro grande está en tests, builds, `git`, `grep` y `ls`; leer código con `cat` no se recorta, a propósito.

## La memoria

Lo que decidiste en una sesión se va con el `/clear`. La memoria de nxy es una base local (SQLite, sin servidor) con búsqueda de texto completo, que se comparte por git.

```
/nxy:mem save "Los montos son BigDecimal" --body "Nunca double para dinero; ver PR #120." --type convention --keywords "plata importe precio"
/nxy:mem search "cómo guardamos los montos"
/nxy:mem list --type convention
/nxy:mem get <id>
/nxy:mem delete <id>
```

Guardá la decisión cuando la tomás: *qué se decidió y por qué*, no *qué hace el código* (eso ya está en el código). `--keywords` es para los sinónimos que vos usarías y el texto no tiene. Tipos: `decision`, `bug`, `convention`, `preference`, `handoff`.

**Tres ámbitos:**

| Ámbito | Para qué | Viaja |
| --- | --- | --- |
| `global` (`--scope global`) | Vos, en todos tus proyectos ("no commiteo desde el agente") | Nunca sale a un repo |
| `project` (default) | Este repo | Se commitea y lo hereda el equipo |
| `area` (`--area api/auth`) | Un subárbol del repo | Igual que `project`, pero sólo aparece cuando trabajás ahí |

El proyecto se identifica por el remote de git, así que el mismo repo es el mismo proyecto en otra carpeta, otra máquina o un worktree.

**Compartir con el equipo:**

```
/nxy:mem export            # escribe <repo>/.nxy/memory/*.md
git add .nxy/memory        # y commiteás como cualquier cambio
/nxy:mem import            # del otro lado, después de un pull
```

Son archivos Markdown, uno por memoria, para que una convención se pueda discutir en un PR. El import se puede correr siempre (no duplica), gana la versión más nueva y nunca borra nada tuyo. Lo `global` y lo marcado `--private` no se exportan.

**El handoff** es la memoria de la tarea: una por rama, ~20 líneas con qué se hizo, qué falta, archivos clave y decisiones. Lo guarda Claude (se lo pedís, o nxy se lo pide cuando la sesión ya está cargada), cada guardado reemplaza al anterior y no se exporta salvo que lo pidas. `/nxy:mem handoff` lo muestra; `/nxy:mem handoff done` lo archiva.

**Los punteros.** Antes de cada mensaje tuyo, nxy busca memorias relacionadas (por los archivos que nombrás o que se vienen editando, por keywords, por palabras compartidas) y le pasa a Claude hasta tres punteros de una línea. Decidir qué apuntar no gasta tokens; cada puntero cuesta ~30. Si una memoria reemplaza a otra, decilo al guardar (`--supersedes <id>`) o dejá que lo decida el *librarian*, un subagente Haiku que relaciona memorias nuevas con las parecidas y busca por significado cuando las palabras no coinciden.

`/nxy:mem status` muestra cuántos punteros se mostraron, cuántos usó Claude y cuánto costaron.

## Configuración

Casi nadie necesita tocar nada: los defaults están elegidos para que funcione sin calibrar. Hay tres niveles, y el de más abajo gana:

| Archivo | Alcance |
| --- | --- |
| `nxy.config.json` (en el plugin) | Defaults |
| `~/.nxy/config.json` | Vos, en todos tus proyectos |
| `<repo>/.nxy/config.json` | El repo; **se commitea** y lo hereda quien lo clone |

Lo que se cambia más seguido:

| Querés | Poné en `.nxy/config.json` |
| --- | --- |
| Plan obligatorio para todo cambio de más de un archivo | `{ "flow": { "plan": "always" } }` |
| Que el planner de este repo use Opus | `{ "roles": { "planner": { "model": "opus" } } }` |
| Apagar el freno de escritura | `{ "gate": { "enabled": false } }` (o `/nxy:gate off`) |
| Apagar el filtro | `{ "modules": { "filter": false } }` (o `/nxy:filter off`) |
| Que nxy no inyecte nada de memoria solo | `{ "memory": { "mode": "manual" } }` |
| Que el documenter busque docs en otra carpeta | `{ "docs": { "paths": ["wiki/"] } }` |
| Lentes de review propias del repo | Un `.md` en `.nxy/lenses/` (se commitea) |

### Modelo y effort de cada agente

| Agente | Qué hace | Modelo | Effort |
| --- | --- | --- | --- |
| `scout` | Encuentra dónde está algo (`/nxy:locate`) | Haiku | — |
| `planner` | Arma el plan por lotes | Sonnet | high |
| `implementer` | Escribe un cambio ya decidido y corre su test | Sonnet | medium |
| `tester` | Corre la suite completa una vez al final del plan | Haiku | — |
| `reviewer` | Revisa lo que cambió el plan | Sonnet | high |
| `documenter` | Actualiza los `.md` que nombran lo cambiado | Sonnet | low |
| `librarian` | Relaciona memorias y busca por significado | Haiku | — |

**El modelo se cambia por config**, para vos (`~/.nxy/config.json`) o para un repo (`.nxy/config.json`): `{ "roles": { "reviewer": { "model": "opus" } } }`. Valores: `haiku`, `sonnet`, `opus`, `fable`. nxy lo aplica en cada despacho. Si tu sesión principal usa un modelo de esa familia (por ejemplo Sonnet 4.6 1M), el agente corre en ese mismo modelo.

**El effort no se puede cambiar hoy.** Claude Code no permite elegirlo por despacho, así que vale el que trae cada agente en `agents/*.md`. Editar esos archivos funciona, pero se pisa al actualizar el plugin.

`.nxy/` se comparte; `.nxy/local/` es estado de tu checkout (ledgers, índice, copias para la review) y va en `.gitignore`. La referencia completa, con statusline y temas: [`docs/configuracion.md`](docs/configuracion.md).

## Si algo no anda

| Síntoma | Qué pasa | Qué hacer |
| --- | --- | --- |
| Los comandos no salen filtrados | rtk no está, está fuera del PATH, o tiene su propio hook | `/nxy:filter status` te dice cuál y cómo arreglarlo. Si lo instalaste con la terminal abierta, abrí una nueva |
| Aviso de que el hook de rtk está instalado | Corriste `rtk init -g` | Desinstalá el hook de rtk; nxy usa el suyo |
| Claude necesita la salida completa de un comando | El filtro recortó algo que hacía falta | Claude lo resuelve solo con `rtk recall <hash>`; como último recurso, `NXY_RAW=1 <comando>` |
| El freno de escritura frena todo | La sesión pasó los 100k de contexto; es lo esperado | Para una edición puntual, `/nxy:gate once`; para ese repo, `/nxy:gate off` |
| Mucho costo en `idle` o `subagent` | Tu cuenta usa cache de 5 minutos y cada pausa o subagente largo la enfría | Mirá la línea `cache TTL 1h what-if` de `/nxy:stats` o `/nxy:trend`: si dice **worth it**, poné `"promptCacheTtl": "1h"` en `~/.claude/settings.json` |
| Algún hook se comporta raro | Los hooks de nxy fallan abiertos: ante cualquier error dejan pasar | `NXY_DEBUG=1` muestra la causa en stderr (`claude --debug`) |
| Checkout compartido entre Windows y WSL con codegraph | El lock de SQLite no cruza filesystems | `CODEGRAPH_DIR=.codegraph-win` en Windows y el default en WSL |

## Qué le agrega nxy a tu sesión

nxy también cuesta algo, y está medido:

- **Contexto fijo:** ~1.000 tokens en cada llamada: las descripciones de 9 comandos, 7 agentes y 2 skills. A precio de cache es menos de un centavo cada 30 llamadas.
- **Tiempo por hook** (medido en Windows, Node 22): 40–50 ms típico; ~105 ms antes de cada comando Bash con rtk (incluye preguntarle a rtk) y ~40 ms después; ~130 ms al abrir la sesión.
- **Tokens de los hooks:** cero cuando no frenan nada. Cuando frenan, el mensaje. Los punteros de memoria, ~30 tokens cada uno, hasta tres por mensaje y cada memoria una vez por sesión.
- **Disco:** la base de memoria en `~/.nxy/memory/`, y por repo `.nxy/local/` (una línea por comando Bash, el índice, y durante un plan una copia de cada archivo que toca y una lista corta de los archivos que ya estaban modificados al aprobar, que se borran con `handoff done`).

## Estado del proyecto

| Parte | Estado |
| --- | --- |
| Versión actual | `1.0.1` |
| Métricas, statusline, filtro con rtk | Publicado desde v0.1.x y usado a diario |
| Scout, freno de escritura, memoria, handoff, plan, verificación, review | Publicado en `1.0.1`, probado en sesiones reales (Linux/WSL y Windows) durante las rc |
| Después | CLI `nxy` (`doctor`, `stats`, `trend`, `mem`) para operarlo sin abrir Claude Code |

## Desarrollo

```
npm install        # sólo devDependencies (typescript)
npm test           # node --test
npm run typecheck  # tsc sobre JSDoc (// @ts-check)
npm run check      # las dos
```

JavaScript ESM con JSDoc, sin build ni dependencias de runtime. El CI corre tests y typecheck en Ubuntu y Windows.

```
core/                # lo que no sabe nada de Claude Code: filtro, métricas, memoria, plan, verify, review, índice
hosts/claude-code/   # lo específico de Claude Code: transcripts, hooks, statusline, entries (los scripts de cada comando)
agents/              # scout, planner, implementer, tester, reviewer, documenter, librarian
commands/ skills/ hooks/ lenses/ .claude-plugin/
```

Regla: **`core/` nunca importa de `hosts/`.** Soportar otro agente es escribir un `hosts/<nombre>/`. Detalle en [`docs/como-funciona.md`](docs/como-funciona.md#estructura-del-código).

## Créditos

`hosts/claude-code/transcripts.mjs` deriva del plugin `session-report` de Anthropic (Apache-2.0). El filtrado lo hace [RTK](https://github.com/rtk-ai/rtk) (Apache-2.0), binario externo. Ver `NOTICE`.
